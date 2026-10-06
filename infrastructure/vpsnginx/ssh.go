// Package vpsnginx reads the VPS's nginx site file and mw binary over ssh, for
// the guard of mw-gq6.274. It only reads: one `cat`, one `mw version`.
package vpsnginx

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

var _ application.VPSProbe = (*SSH)(nil)

// Timeout bounds one ssh call, a margin over ssh's own ConnectTimeout, so a
// connection that opens and then stalls is a failure too.
const Timeout = 30 * time.Second

// SSH is the VPS reached by `ssh -o BatchMode=yes root@<vps>`.
type SSH struct {
	// Target is the ssh destination, `root@host`.
	Target string
	// Conf is the VPS's nginx site file and Mw its mw binary.
	Conf, Mw string
	// RigDir is this host's checkout of the factory rig, where a revision the
	// VPS's binary names is looked up. Empty leaves the binary's age untold.
	RigDir string

	// Run runs one command and gives its output. Nil runs the real one.
	Run func(ctx context.Context, argv []string) ([]byte, error)
}

// New is the VPS at target.
func New(target, conf, mw, rigDir string) *SSH {
	return &SSH{Target: target, Conf: conf, Mw: mw, RigDir: rigDir}
}

// NginxSite implements application.VPSProbe: the site file, read, not changed.
func (s *SSH) NginxSite(ctx context.Context) (string, error) {
	out, err := s.ssh(ctx, "cat", s.Conf)
	if err != nil {
		return "", fmt.Errorf("reading %s on the VPS: %w", s.Conf, err)
	}
	return string(out), nil
}

// BinaryLacks implements application.VPSProbe: the VPS's `mw version` names the
// revision its binary was built from, and git here says whether commit is an
// ancestor of it. A binary with no revision stamped, or a revision this
// checkout does not know, is an error: it cannot be told.
func (s *SSH) BinaryLacks(ctx context.Context, commit string) (bool, error) {
	if s.RigDir == "" {
		return false, errors.New("no checkout of the factory rig to look the revision up in")
	}
	out, err := s.ssh(ctx, s.Mw, "version")
	if err != nil {
		return false, fmt.Errorf("asking the VPS's mw its version: %w", err)
	}
	revision := RevisionIn(string(out))
	if revision == "" {
		return false, errors.New("the VPS's mw version names no revision")
	}
	_, err = s.run(ctx, []string{"git", "-C", s.RigDir, "merge-base", "--is-ancestor", commit, revision})
	var exit *exec.ExitError
	switch {
	case err == nil:
		return false, nil
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		return true, nil
	}
	return false, fmt.Errorf("telling whether %s is in %s: %w", commit, revision, err)
}

// RevisionIn is the revision `mw version`'s first line names, "mw 0.1.0-dev
// (8248967)" or "(8248967, modified)", empty when it names none.
func RevisionIn(version string) string {
	line, _, _ := strings.Cut(version, "\n")
	start, end := strings.LastIndex(line, "("), strings.LastIndex(line, ")")
	if start < 0 || end < start {
		return ""
	}
	revision, _, _ := strings.Cut(line[start+1:end], ",")
	return strings.TrimSpace(revision)
}

func (s *SSH) ssh(ctx context.Context, command ...string) ([]byte, error) {
	argv := []string{"ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", s.Target}
	return s.run(ctx, append(argv, command...))
}

func (s *SSH) run(ctx context.Context, argv []string) ([]byte, error) {
	if s.Run != nil {
		return s.Run(ctx, argv)
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if text := strings.TrimSpace(stderr.String()); text != "" {
			return nil, fmt.Errorf("%w: %s", err, text)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}
