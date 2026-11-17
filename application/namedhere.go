package application

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// NamedHere is the far end of NamedElsewhere (mw-gq6.1395): this host runs the
// tests another host's main failed by name, in a throwaway worktree of the target
// as this host has it, and prints the verdict for the asking host to read.
type NamedHere struct {
	Worktrees   Worktrees
	Landing     Landing
	NamedChecks NamedChecks
	Rigs        map[string]string
	Out         io.Writer
}

// The first line the far end prints: how the run came out. The rest is what the
// tests printed.
const (
	NamedHerePassed      = "named: passed"
	NamedHereFailed      = "named: failed"
	NamedHereNotRun      = "named: not run"
	NamedHereNoCommand   = "named: no command"
	namedHereCommandLine = "command: "
)

// Run runs tests, by name, in a landing of ref in the rig here, and prints the
// verdict. A rig not checked out here is an error, which the asking host reads
// as a host that could not be asked.
func (h NamedHere) Run(ctx context.Context, rigName, ref string, tests []string) error {
	rigDir, ok := h.Rigs[rigName]
	if !ok {
		return fmt.Errorf("the rig %s is not checked out on this host", rigName)
	}
	if len(tests) == 0 {
		return fmt.Errorf("no tests were named")
	}
	// A fetch that failed is not fatal: the rig's view of the remote stands.
	_ = h.Worktrees.Fetch(ctx, rigDir)
	dir, err := h.Landing.OpenLanding(ctx, rigDir, ref)
	if err != nil {
		return fmt.Errorf("a landing of %s at %s could not be opened: %w", rigName, ref, err)
	}
	defer func() { _ = h.Landing.CloseLanding(ctx, rigDir, dir) }()
	run, err := h.NamedChecks.RunNamed(ctx, rigName, dir, tests)
	if err != nil {
		return err
	}
	switch {
	case !run.Ran:
		fmt.Fprintln(h.Out, NamedHereNoCommand)
	case run.NotRun:
		fmt.Fprintln(h.Out, NamedHereNotRun)
	case run.Passed:
		fmt.Fprintln(h.Out, NamedHerePassed)
	default:
		fmt.Fprintln(h.Out, NamedHereFailed)
	}
	fmt.Fprintln(h.Out, namedHereCommandLine+strings.ReplaceAll(run.Command, "\n", " "))
	fmt.Fprint(h.Out, run.Output)
	return nil
}

// ParseNamedHere reads what a far end printed. Text with no verdict line is an
// error, not a pass: a host that printed something else (a login banner) has not
// said its tests passed.
func ParseNamedHere(text string) (NamedRun, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.SplitAfter(text, "\n")
	for i, line := range lines {
		var run NamedRun
		switch strings.TrimRight(line, "\n") {
		case NamedHerePassed:
			run = NamedRun{Ran: true, Checked: Checked{Passed: true}}
		case NamedHereFailed:
			run = NamedRun{Ran: true}
		case NamedHereNotRun:
			run = NamedRun{Ran: true, Checked: Checked{NotRun: true}}
		case NamedHereNoCommand:
			run = NamedRun{}
		default:
			continue
		}
		rest := lines[i+1:]
		if len(rest) > 0 && strings.HasPrefix(rest[0], namedHereCommandLine) {
			run.Command = strings.TrimRight(strings.TrimPrefix(rest[0], namedHereCommandLine), "\n")
			rest = rest[1:]
		}
		run.Output = strings.Join(rest, "")
		return run, nil
	}
	return NamedRun{}, fmt.Errorf("the host did not say how the tests came out: %q", strings.TrimSpace(text))
}
