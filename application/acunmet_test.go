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

// A bug filed for an unmet criterion runs on the factory's default route, not
// on the finder's: a finder pathed fable at xhigh does not give its model and
// effort to a small bug (mw-gq6.1549, folded into mw-gq6.1640).
func TestAnUnmetACBugGetsItsOwnModelAndEffortNotTheFinders(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: domain.ModelFable, Effort: domain.EffortXHigh, Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", theSourceStory())
	mustDo(t, tracker.ClaimStory(ctx, "mw-x.1"))
	mustDo(t, tracker.SetStoryState(ctx, "mw-x.1", application.RunState, application.RunLanded, "landed on main"))
	mustDo(t, tracker.CommentOnStory(ctx, "mw-x.1", "Done, but AC 2 NOT met: the test does not run yet.\nFor the rig memory: nothing"))
	before := tracker.Stories()
	lands := &fakeRetryLanding{}
	_, err := application.Next{
		Tracker: tracker, Vault: newFakeVault(), Worktrees: lands, Landing: lands,
		Checks: checkStub{}, Slot: slotStub{}, Mailbox: apptest.NewFakeMailbox(),
		Seat: "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(ctx, "mw-x.1")
	mustDo(t, err)

	var bug []application.StoryDetail
	for _, d := range theTestersFiled(t, tracker, before) {
		if strings.Contains(d.Story.Title, "AC") {
			bug = append(bug, d)
		}
	}
	if len(bug) != 1 {
		t.Fatalf("expected 1 unmet-AC bug filed, got %d: %+v", len(bug), bug)
	}
	if got := bug[0].Merged(); got.Model != domain.ModelSonnet || got.Effort != domain.EffortHigh {
		t.Errorf("expected the unmet-AC bug on sonnet at high, got %s at %s", got.Model, got.Effort)
	}
}

// A criterion that is optional, 'Optional and preferred if small', is met by a
// closing comment that says which was done, so saying one was NOT done is not a
// refusal; the same words on a criterion that is not optional still are
// (mw-gq6.1602).
func TestAnOptionalCriterionSaidNotDoneIsNotUnmet(t *testing.T) {
	acceptance := "1. The detector reads the comment. go test passes.\n" +
		"2. Optional and preferred if small: the sweep is done; say in the closing comment which was done.\n" +
		"3. Fix the wording too if small.\n" +
		"4. make build passes, optional checks aside."
	cases := []struct {
		name    string
		comment string
		unmet   int
	}{
		{"optional criterion not done", "AC 2 (the sweep) was NOT done: it was bigger than small.", 0},
		{"if small criterion not run", "AC3 was not run.", 0},
		{"optional criterion, other phrases", "AC 2 not verified; AC 2 not met.", 0},
		{"the same words on a required criterion", "AC 1 was NOT done: it was bigger than small.", 1},
		{"a required and an optional one in one sentence", "AC 1 and AC 2 were NOT done.", 1},
		{"an optional one and a required one in two lines", "AC 2 was NOT done.\nAC 1 was NOT done.", 1},
		{"the optional word in a criterion that is not the one named", "AC 1 was NOT done, though AC 2 is optional.", 1},
		{"a criterion the acceptance does not have", "AC 9 was NOT done.", 1},
		{"no number to look up", "The acceptance criteria were not met.", 1},
		{"the quoted number is not named", "AC 1 NOT done; the line 'AC 2 not done' is quoted.", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			unmet, _ := application.UnmetCriteriaAfterHandoff(c.comment, acceptance)
			if len(unmet) != c.unmet {
				t.Fatalf("unmet = %q, want %d of them", unmet, c.unmet)
			}
		})
	}
}

// A landing files no bug for an optional criterion its closing comment says was
// NOT done, and still files one for a criterion that is not optional
// (mw-gq6.1602).
func TestALandingFilesNoBugForAnOptionalCriterionNotDone(t *testing.T) {
	for _, c := range []struct {
		name, comment string
		bugs          int
	}{
		{"optional", "Done.\nAC 2 was NOT done: bigger than small.\nFor the rig memory: nothing", 0},
		{"required", "Done.\nAC 1 was NOT done.\nFor the rig memory: nothing", 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			tracker := apptest.NewFakeTracker()
			tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
			tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A landed story"})
			mustDo(t, tracker.SetAcceptance("mw-x.1", "1. One.\n2. Optional and preferred if small: Two."))
			mustDo(t, tracker.ClaimStory(ctx, "mw-x.1"))
			mustDo(t, tracker.SetStoryState(ctx, "mw-x.1", application.RunState, application.RunLanded, "landed on main"))
			mustDo(t, tracker.CommentOnStory(ctx, "mw-x.1", c.comment))
			before := tracker.Stories()
			lands := &fakeRetryLanding{}
			_, err := application.Next{
				Tracker: tracker, Vault: newFakeVault(), Worktrees: lands, Landing: lands,
				Checks: checkStub{}, Slot: slotStub{}, Mailbox: apptest.NewFakeMailbox(),
				Seat: "builder", Host: "laptop",
				Rigs: map[string]string{"millwright": "/rigs/millwright"},
				Out:  io.Discard, Err: io.Discard,
			}.Run(ctx, "mw-x.1")
			mustDo(t, err)
			var bugs int
			for _, d := range theTestersFiled(t, tracker, before) {
				if strings.Contains(d.Story.Title, "landed without its AC") {
					bugs++
				}
			}
			if bugs != c.bugs {
				t.Errorf("expected %d unmet-AC bugs filed, got %d", c.bugs, bugs)
			}
		})
	}
}
