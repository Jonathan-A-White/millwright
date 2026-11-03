package application

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// CheckTestsKeptFileName is the name of the record of a whole check's passed
// tests, in the story's run directory, one for the attempt (mw-gq6.918).
func CheckTestsKeptFileName(attempt int) string {
	if attempt < 1 {
		attempt = 1
	}
	return fmt.Sprintf("check-%d-tests.rec", attempt)
}

// A whole mw check that has run the rig's tests and passed them, and is then
// handed back because the changed specs' slot or gate lock is held past the
// check's one deadline, keeps that the tests passed, and at which commit and with
// which command. The check run again at the same commit does not run them again,
// and so waits for no slot, gate lock or calm host for them: it goes straight to
// the changed specs. Nothing else is kept: a run that failed is not (the run
// again is what the next call makes first, and only a failure on a calm host
// counts), and a pass at another commit, or with the worktree holding
// uncommitted work, says nothing of this one (mw-gq6.918).

// lastChecks remembers the rig's last tests that ran.
type lastChecks struct {
	inner Checks
	mu    sync.Mutex
	ran   Checked
}

func (l *lastChecks) Run(ctx context.Context, rig, dir string) (Checked, error) {
	checked, err := l.inner.Run(ctx, rig, dir)
	l.mu.Lock()
	l.ran = checked
	l.mu.Unlock()
	return checked, err
}

func (l *lastChecks) last() Checked {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ran
}

// keptTestsTipWord begins the line of a kept record that names the commit.
const keptTestsTipWord = "tip"

// keepTests keeps that the tests passed at tip, unless they did not, the tip is
// not known, or the branch has moved since it was read.
func (k Check) keepTests(ctx context.Context, c *closeOut, attempt int, tip string, ran Checked) {
	if k.Records == nil || tip == "" || !ran.Passed || ran.NotRun {
		return
	}
	if now, err := k.Landing.RefHead(ctx, c.rigDir, c.branch); err != nil || now != tip {
		return
	}
	_, _ = k.Records.PutRunFile(ctx, c.id, CheckTestsKeptFileName(attempt),
		fmt.Sprintf("%s %s\n%s\n", keptTestsTipWord, tip, strings.ReplaceAll(strings.TrimSpace(ran.Command), "\n", " ")))
}

// keptTests is the command of the tests an earlier call of this attempt's check
// passed at tip, or "" when none was kept for it.
func (k Check) keptTests(ctx context.Context, storyID string, attempt int, tip string) string {
	if k.Records == nil || tip == "" {
		return ""
	}
	text, err := k.Records.ReadRunFile(ctx, storyID, CheckTestsKeptFileName(attempt))
	if err != nil {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) < 2 {
		return ""
	}
	word, at, _ := strings.Cut(strings.TrimSpace(lines[0]), " ")
	if word != keptTestsTipWord || at != tip {
		return ""
	}
	if command := strings.TrimSpace(lines[1]); command != "" {
		return command
	}
	return "the rig's tests"
}

// keptChecks stands in for the rig's tests that an earlier call passed.
type keptChecks struct{ command string }

func (k keptChecks) Run(context.Context, string, string) (Checked, error) {
	return Checked{Command: k.command, Passed: true, Output: "kept from an earlier call of this check: passed at this commit\n"}, nil
}
