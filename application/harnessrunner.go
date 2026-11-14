package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// The wait a session's start makes for claude to answer when HarnessReadyRunner
// says nothing of it: 24 asks, 5 seconds apart, two minutes in all, longer than
// the install of a version of Claude Code takes to replace the binary.
const (
	DefaultHarnessTries = 24
	DefaultHarnessPoll  = 5 * time.Second
)

// HarnessReadyRunner is a Runner that starts a session of claude only once
// claude answers on this host (mw-gq6.1281). The home's cloud check replaces a
// box's Claude Code while the box's own dispatch ticks, and mw-gq6.1247, started
// in between, died with "nice: 'claude': No such file or directory". Ready is the
// host's `claude --version`; a Start whose command does not run claude, or a
// Runner with no Ready, starts at once. Ready is asked up to Tries times, Poll
// apart (zero are the defaults), waiting with Sleep (nil is a real wait). A claude
// that never answers is a failed Start, which gives the story's claim back with
// no attempt counted, because the session never ran. Every other method is the
// wrapped Runner's.
type HarnessReadyRunner struct {
	Runner
	Ready func(ctx context.Context) error
	Tries int
	Poll  time.Duration
	Sleep func(ctx context.Context, d time.Duration) error
	Host  string
	Out   io.Writer
}

// runsClaude says whether a session's command starts claude.
func runsClaude(spec SessionSpec) bool {
	return strings.Contains(strings.Join(spec.Command, " "), "claude")
}

// Start implements Runner: it waits for claude to answer, then starts the session.
func (r HarnessReadyRunner) Start(ctx context.Context, spec SessionSpec) error {
	if err := r.awaitClaude(ctx, spec); err != nil {
		return err
	}
	return r.Runner.Start(ctx, spec)
}

func (r HarnessReadyRunner) awaitClaude(ctx context.Context, spec SessionSpec) error {
	if r.Ready == nil || !runsClaude(spec) {
		return nil
	}
	tries, poll, wait := r.Tries, r.Poll, r.Sleep
	if tries < 1 {
		tries = DefaultHarnessTries
	}
	if poll <= 0 {
		poll = DefaultHarnessPoll
	}
	if wait == nil {
		wait = sleepFor
	}
	var err error
	for try := 1; try <= tries; try++ {
		if err = r.Ready(ctx); err == nil {
			return nil
		}
		if try == tries {
			break
		}
		if try == 1 && r.Out != nil {
			fmt.Fprintf(r.Out, "  claude does not answer on %s (%s): waiting for it, up to %d asks %s apart\n", r.Host, oneLine(err.Error()), tries, poll)
		}
		if werr := wait(ctx, poll); werr != nil {
			return werr
		}
	}
	return fmt.Errorf("claude --version did not answer on %s in %d asks %s apart: %w", r.Host, tries, poll, err)
}
