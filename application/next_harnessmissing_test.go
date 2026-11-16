package application_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-gq6.1281: mw-gq6.1247 was started on cloud8 while the home's cloud check was
// replacing the box's Claude Code, and ended no-result with the shell's "nice:
// 'claude': No such file or directory" and an empty result. The story's code was
// never run.
const claudeMissingPane = `nice: 'claude': No such file or directory
`

func TestASessionThatFoundNoClaudeIsPutBackUnattemptedWithOneLineSayingWhy(t *testing.T) {
	run := aCrashedCloseOut(t, 1, claudeMissingPane, nil, nil)

	if string(run.report.Reason) == string(application.ReasonNoResult) {
		t.Fatalf("expected the story put back, not stopped as no-result: %s", run.report.Why)
	}
	if !slices.Contains(run.tracker.Asked(), "ReleaseClaim") {
		t.Errorf("expected the claim given back, asked %v", run.tracker.Asked())
	}
	detail, err := run.tracker.ShowStory(t.Context(), "mw-gq6.9")
	if err != nil {
		t.Fatalf("showing the story: %v", err)
	}
	if detail.Attempts != 1 {
		t.Errorf("expected the attempt count unchanged at 1, got %d", detail.Attempts)
	}
	if own, uncounted := application.OwnAttempts(detail); own != 0 || len(uncounted) != 1 || uncounted[0].Cause != application.CauseTooling {
		t.Errorf("expected no own attempt and the session's one uncounted as tooling, got %d and %v", own, uncounted)
	}
	if !strings.Contains(run.comments(), "No such file or directory") || !strings.Contains(run.comments(), "no claude to start") {
		t.Errorf("expected a comment saying why, got:\n%s", run.comments())
	}
	if inbox, _ := run.mail.Inbox(t.Context(), application.MayorMailbox); len(inbox) != 0 {
		t.Errorf("expected no mail to the Mayor for a story put back, got %v", inbox)
	}
}

func TestAMissingClaudeIsPutBackOnceAndTheSecondStopsAsNoResult(t *testing.T) {
	run := aCrashedCloseOut(t, 2, claudeMissingPane, []string{application.HarnessMissingFileNameForAttempt(1)}, nil)

	if run.report.Reason != application.ReasonNoResult {
		t.Fatalf("expected %s on the second, got %q: %s", application.ReasonNoResult, run.report.Reason, run.report.Why)
	}
	if slices.Contains(run.tracker.Asked(), "ReleaseClaim") {
		t.Errorf("expected the claim kept, asked %v", run.tracker.Asked())
	}
}

func TestANoSuchFileLineThatIsNotClaudeStaysNoResult(t *testing.T) {
	run := aCrashedCloseOut(t, 1, "cat: notes.txt: No such file or directory\n", nil, nil)

	if run.report.Reason != application.ReasonNoResult {
		t.Fatalf("expected %s, got %q: %s", application.ReasonNoResult, run.report.Reason, run.report.Why)
	}
}

// mw-gq6.1357: a Codex story's pane says "No such file or directory" for any file
// of its own; only the missing claude program is a missing harness.
func TestHarnessMissingIgnoresAnotherProgramsMissingFile(t *testing.T) {
	for _, pane := range []string{
		"cat: /home/jwhite/.claude/notes.txt: No such file or directory\n",
		"bash: ./claude-notes.sh: No such file or directory\n",
		"cat: docs/claude.md: No such file or directory\n",
	} {
		run := aCrashedCloseOut(t, 1, pane, nil, nil)
		if run.report.Reason != application.ReasonNoResult {
			t.Errorf("pane %q: expected %s, got %q: %s", pane, application.ReasonNoResult, run.report.Reason, run.report.Why)
		}
	}
}

func TestHarnessMissingStillReadsClaudeByPath(t *testing.T) {
	for _, pane := range []string{
		"bash: claude: No such file or directory\n",
		"bash: /home/jwhite/.local/bin/claude: No such file or directory\n",
	} {
		run := aCrashedCloseOut(t, 1, pane, nil, nil)
		if run.report.Reason == application.ReasonNoResult {
			t.Errorf("pane %q: expected the story put back, got no-result: %s", pane, run.report.Why)
		}
	}
}
