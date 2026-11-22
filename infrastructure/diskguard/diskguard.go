// Package diskguard holds the host's side of the disk guard (mw-6ww.185.7): what
// frees space (the mw commands that do) and what holds the space under the home
// directory (du).
package diskguard

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// StepTimeout is how long one step of freeing may run.
const StepTimeout = 15 * time.Minute

// Freer frees space by running this mw's own commands: mw host sweep, mw host
// tidy go-cache and mw host tidy tmp. Each runs as a process of its own, so that
// a step that fails or hangs costs the guard that step and nothing else.
type Freer struct {
	// Mw is the mw binary to run.
	Mw string
}

var _ application.DiskFreeUp = Freer{}

// steps are the arguments of each command, and how it is named in the mail.
var steps = []struct {
	name string
	args []string
}{
	{"host sweep", []string{"host", "sweep"}},
	{"host tidy go-cache", []string{"host", "tidy", "go-cache"}},
	{"host tidy tmp", []string{"host", "tidy", "tmp"}},
}

// FreeUp implements application.DiskFreeUp.
func (f Freer) FreeUp(ctx context.Context) []string {
	var lines []string
	for _, step := range steps {
		stepCtx, cancel := context.WithTimeout(ctx, StepTimeout)
		cmd := exec.CommandContext(stepCtx, f.Mw, step.args...)
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		err := cmd.Run()
		cancel()
		said := lastLine(out.String())
		switch {
		case err != nil && said != "":
			lines = append(lines, fmt.Sprintf("mw %s failed (%v): %s", step.name, err, said))
		case err != nil:
			lines = append(lines, fmt.Sprintf("mw %s failed: %v", step.name, err))
		case said == "":
			lines = append(lines, fmt.Sprintf("mw %s: done", step.name))
		default:
			lines = append(lines, fmt.Sprintf("mw %s: %s", step.name, said))
		}
	}
	return lines
}

// lastLine is the last non-empty line of out, which for the host's tidies is the
// one that says what was done.
func lastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// Holders reads what holds the space under Home with du.
type Holders struct {
	Home string
	// Timeout bounds the read; zero is StepTimeout.
	Timeout time.Duration
}

var _ application.DiskHolders = Holders{}

// Largest implements application.DiskHolders: du's one level under the home
// directory, on its own filesystem, the n largest.
func (h Holders) Largest(ctx context.Context, n int) ([]application.DiskHolder, error) {
	timeout := h.Timeout
	if timeout <= 0 {
		timeout = StepTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, "du", "-x", "-d", "1", "-B1", h.Home)
	cmd.Stdout = &out
	// du exits non-zero for a file it cannot read, and still says the rest.
	if err := cmd.Run(); err != nil && out.Len() == 0 {
		return nil, fmt.Errorf("du %s: %w", h.Home, err)
	}
	return largest(out.String(), h.Home, n), nil
}

// largest parses du's output (bytes, a tab, a path), leaves out the home
// directory's own total, and returns the n largest.
func largest(out, home string, n int) []application.DiskHolder {
	var held []application.DiskHolder
	for _, line := range strings.Split(out, "\n") {
		size, path, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		bytes, err := strconv.ParseInt(strings.TrimSpace(size), 10, 64)
		if err != nil || strings.TrimRight(path, "/") == strings.TrimRight(home, "/") {
			continue
		}
		held = append(held, application.DiskHolder{Path: path, Bytes: bytes})
	}
	sort.SliceStable(held, func(i, j int) bool { return held[i].Bytes > held[j].Bytes })
	if len(held) > n {
		held = held[:n]
	}
	return held
}
