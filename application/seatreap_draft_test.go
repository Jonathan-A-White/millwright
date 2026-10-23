package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// The 2026-10-22 Deputy 757 wrote its handoff and stopped with an unsent boot
// prompt on its input line; the reaper never saw an idle pane and gave up after
// three hours (mw-gq6.709).
func TestAHandedOffWindowHoldingOnlyUnsentTextIsClosed(t *testing.T) {
	r := newDeputyReaper(t, nil)
	r.reap.Mail = nil
	if err := r.windows.Pane("@3", application.PaneDraft); err != nil {
		t.Fatal(err)
	}

	report, err := r.reap.Run(context.Background())

	if err != nil || report.Outcome != application.ReapClosed {
		t.Fatalf("expected the window closed, it ended %q with %v", report.Outcome, err)
	}
	if !strings.Contains(report.Said, "held only unsent text") {
		t.Errorf("expected the log to say the pane held only unsent text, got %q", report.Said)
	}
	if typed := r.windows.Typed("@3"); len(typed) != 0 {
		t.Errorf("expected nothing typed over the text, got %q", typed)
	}
}

func TestAHandedOffWindowWithATurnRunningIsNotClosed(t *testing.T) {
	r := newDeputyReaper(t, nil)
	r.reap.Mail = nil
	if err := r.windows.Pane("@3", application.PaneWorking); err != nil {
		t.Fatal(err)
	}

	report, err := r.reap.Run(context.Background())

	if err == nil || report.Outcome != application.ReapGaveUp {
		t.Fatalf("expected the watch to give up, it ended %q with %v", report.Outcome, err)
	}
	if got := r.windows.ClosedIDs(); len(got) != 0 {
		t.Errorf("expected nothing closed, got %v", got)
	}
}

// Unsent text is not a reason to type a nudge over it: the mail waits for the
// next session, which `mw deputy` starts.
func TestAHandedOffWindowHoldingUnsentTextIsNotNudgedOverUnreadMail(t *testing.T) {
	r := newDeputyReaper(t, aBoxWith(t, 1))
	if err := r.windows.Pane("@3", application.PaneDraft); err != nil {
		t.Fatal(err)
	}

	report, err := r.reap.Run(context.Background())

	if err != nil || report.Outcome != application.ReapClosed {
		t.Fatalf("expected the window closed, it ended %q with %v", report.Outcome, err)
	}
	if typed := r.windows.Typed("@3"); len(typed) != 0 {
		t.Errorf("expected nothing typed over the text, got %q", typed)
	}
}

// A window not handed off is not closed over text on its line, as before.
func TestAWindowWithNoHandoffHoldingTextIsNotClosed(t *testing.T) {
	r := newDeputyReaper(t, nil)
	r.reap.Mail = nil
	r.files.start = aSeatStart(func(s *application.SeatStart) {
		s.Handoffs = nil
	})
	if err := r.windows.Pane("@3", application.PaneDraft); err != nil {
		t.Fatal(err)
	}
	r.reap.Limit = 10 * time.Minute

	report, err := r.reap.Run(context.Background())

	if err == nil || report.Outcome != application.ReapGaveUp || len(r.windows.ClosedIDs()) != 0 {
		t.Fatalf("expected the watch to give up with nothing closed, it ended %q with %v, closed %v", report.Outcome, err, r.windows.ClosedIDs())
	}
}

func TestPaneHoldsOnlyDraft(t *testing.T) {
	const rule = "────────────────────────────────────────\n"
	for name, tc := range map[string]struct {
		screen string
		want   bool
	}{
		"text on the input line":           {rule + "❯ Woken for: Mayor 550 jobs 23-26\n" + rule, true},
		"text after a no-break space":      {"❯ half a sente\n", true},
		"an empty input line":              {"❯ \n", false},
		"a dim suggestion":                 {"❯ \x1b[2mrun the tests\x1b[0m\n", false},
		"a turn running":                   {"✻ Pondering… (41s · esc to interrupt)\n❯ half a sente\n", false},
		"a menu's selected choice":         {"Do you want to proceed?\n❯ 1. Yes\n  2. No\n", false},
		"no prompt at all":                 {"working...\n", false},
		"an earlier prompt in the history": {"❯ old words\nreply\n❯ \n", false},
		"the usage limit menu": {"  ⎿  You've hit your weekly limit · resets Oct 16, 7am (UTC)\n" +
			" │ ❯ 1. Stop and wait for limit to reset\n │   2. Switch to usage credits\n", false},
	} {
		if got := application.PaneHoldsOnlyDraft(tc.screen); got != tc.want {
			t.Errorf("%s: PaneHoldsOnlyDraft = %v, want %v", name, got, tc.want)
		}
	}
}
