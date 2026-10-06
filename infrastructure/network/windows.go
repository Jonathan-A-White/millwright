// Package network is how mw learns whether the network is metered: Windows'
// own connection cost, asked from WSL, and the host's memory of the answer.
package network

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// PowerShell is where WSL mounts Windows' PowerShell. It is not on the seats'
// PATH, so it is named in full.
const PowerShell = "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe"

// Timeout is how long the call may take: it takes a few seconds, and a stuck one
// must not hold a tick.
const Timeout = 10 * time.Second

// script prints the current connection profile's name and cost on one line,
// `<profile> | cost=<NetworkCostType> roaming=... overLimit=... approaching=...`,
// or `none` when Windows has no connection profile.
const script = `$p=[Windows.Networking.Connectivity.NetworkInformation,Windows.Networking.Connectivity,ContentType=WindowsRuntime]::GetInternetConnectionProfile(); ` +
	`if($p -eq $null){'none'}else{$c=$p.GetConnectionCost(); ` +
	`"$($p.ProfileName) | cost=$($c.NetworkCostType) roaming=$($c.Roaming) overLimit=$($c.OverDataLimit) approaching=$($c.ApproachingDataLimit)"}`

// Run is how a program is run and its output read: the seam a test replaces.
type Run func(ctx context.Context, name string, args ...string) ([]byte, error)

// Windows asks Windows for the cost of its connection.
type Windows struct {
	// Path is the PowerShell to run; empty is PowerShell.
	Path string
	// Run runs it; nil runs it for real.
	Run Run
	// Timeout is how long it may take; zero is Timeout.
	Timeout time.Duration
}

var _ application.NetworkProbe = Windows{}

// Probe implements application.NetworkProbe.
func (w Windows) Probe(ctx context.Context) (application.NetworkCondition, error) {
	path, run, limit := w.Path, w.Run, w.Timeout
	if path == "" {
		path = PowerShell
	}
	if run == nil {
		run = execRun
	}
	if limit <= 0 {
		limit = Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()

	out, err := run(ctx, path, "-NoProfile", "-Command", script)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return application.NetworkCondition{}, fmt.Errorf("powershell.exe took longer than %s", limit)
		}
		return application.NetworkCondition{}, err
	}
	return parse(string(out))
}

// parse reads the script's one line.
func parse(out string) (application.NetworkCondition, error) {
	line := strings.TrimSpace(out)
	if i := strings.LastIndex(line, "\n"); i >= 0 {
		line = strings.TrimSpace(line[i+1:])
	}
	if line == "none" {
		return application.NetworkCondition{}, nil
	}
	profile, rest, found := strings.Cut(line, " | cost=")
	if !found {
		return application.NetworkCondition{}, fmt.Errorf("powershell.exe said %q, not a connection profile and its cost", clip(line))
	}
	cost, _, _ := strings.Cut(rest, " ")
	if cost == "" {
		return application.NetworkCondition{}, fmt.Errorf("powershell.exe said %q, with no cost", clip(line))
	}
	return application.NetworkCondition{Profile: strings.TrimSpace(profile), Cost: cost}, nil
}

func clip(s string) string {
	if r := []rune(s); len(r) > 80 {
		return string(r[:79]) + "…"
	}
	return s
}

func execRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}
