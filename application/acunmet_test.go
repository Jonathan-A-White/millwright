package application_test

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

func TestReadACNotMet(t *testing.T) {
	cases := []struct {
		name    string
		comment string
		acs     []int
		subject string
	}{
		{"one", "Done.\nAC 3 NOT met: why.", []int{3}, "Landed, AC 3 NOT met"},
		{"done is not met too", "AC 2 NOT done (the harness refused the step)", []int{2}, "Landed, AC 2 NOT met"},
		{"only the one that is not met", "AC 1, 3 and 4 done; AC 2 NOT done", []int{2}, "Landed, AC 2 NOT met"},
		{"two", "AC 2 and 4 NOT met", []int{2, 4}, "Landed, AC 2 and 4 NOT met"},
		{"three", "AC 1, 2 and 5 NOT met.", []int{1, 2, 5}, "Landed, AC 1, 2 and 5 NOT met"},
		{"oxford comma", "AC 1, 2, and 5 NOT met.", []int{1, 2, 5}, "Landed, AC 1, 2 and 5 NOT met"},
		{"on two lines", "AC 4 NOT met.\nAC 2 NOT done.", []int{2, 4}, "Landed, AC 2 and 4 NOT met"},
		{"colon and verb", "AC 3: NOT met\nAC 5 is NOT met", []int{3, 5}, "Landed, AC 3 and 5 NOT met"},
		{"lower case is prose", "AC 3 was not met by my first try.", nil, ""},
		{"no criterion named", "The work is NOT met by the old tests.", nil, ""},
		{"all done", "AC 1, 2 and 3 done.", nil, ""},
		{"mw-gq6.720's sweep line", "Sweep: acunmet.go reads 'AC n NOT met' on any line, so a quoted 'AC 3 NOT met' counts. Low risk.", nil, ""},
		{"single quotes", "The mail says 'AC 3 NOT met' when it should not.", nil, ""},
		{"double quotes", `The mail says "AC 3 NOT met" when it should not.`, nil, ""},
		{"curly quotes", "The mail says \u201cAC 3 NOT met\u201d when it should not.", nil, ""},
		{"backticks", "The mail says `AC 3 NOT met` when it should not.", nil, ""},
		{"quoted and said on one line", "A quoted 'AC 3 NOT met' counts; AC 4 NOT met.", []int{4}, "Landed, AC 4 NOT met"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := application.ReadACNotMet(c.comment)
			if !reflect.DeepEqual(got.ACs, c.acs) {
				t.Fatalf("ACs = %v, want %v", got.ACs, c.acs)
			}
			if got.Found() != (c.acs != nil) {
				t.Fatalf("Found() = %v for %v", got.Found(), c.acs)
			}
			if c.acs != nil && got.Subject() != c.subject {
				t.Fatalf("Subject() = %q, want %q", got.Subject(), c.subject)
			}
		})
	}
}

// A bug already filed for an unmet AC is said in the close-out's log with the
// rule that matched, its title (mw-gq6.1107).
func TestAnUnmetACJoinedToAFiledBugIsWrittenInTheCloseOutsLog(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A landed story"})
	mustDo(t, tracker.SetAcceptance("mw-x.1", "1. One.\n2. Two."))
	title := "[bug] mw-x.1 landed without its AC 2: Two."
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.9", Title: title})
	mustDo(t, tracker.ClaimStory(ctx, "mw-x.1"))
	mustDo(t, tracker.SetStoryState(ctx, "mw-x.1", application.RunState, application.RunLanded, "landed on main"))
	mustDo(t, tracker.CommentOnStory(ctx, "mw-x.1", "Done.\nAC 2 NOT met: it did not run.\nFor the rig memory: nothing"))
	vault := newFakeVault()
	vault.written["mw-x.1/next-1.log"] = "close-out started 2026-11-13T10:00:00Z\n"
	lands := &fakeRetryLanding{}
	_, err := application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands, Landing: lands,
		Checks: checkStub{}, Slot: slotStub{}, Mailbox: apptest.NewFakeMailbox(),
		Seat: "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(ctx, "mw-x.1")
	mustDo(t, err)

	log := vault.written["mw-x.1/next-1.log"]
	if !strings.HasPrefix(log, "close-out started 2026-11-13T10:00:00Z\n") {
		t.Errorf("expected what the log held kept, got %q", log)
	}
	var line string
	for _, l := range strings.Split(log, "\n") {
		if strings.Contains(l, "mw-x.9") {
			line = l
		}
	}
	for _, want := range []string{"AC 2", "mw-x.9", "same title", title} {
		if !strings.Contains(line, want) {
			t.Errorf("expected the line to say %q, got %q (log %q)", want, line, log)
		}
	}
}
