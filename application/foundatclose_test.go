package application_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

func TestParseFoundNotFixed(t *testing.T) {
	cases := []struct {
		name, comment string
		found         bool
		lines         []string
	}{
		{"two lines", "Done.\nFound, not fixed:\nA is wrong (a.go:1)\nB is wrong (b.go:2)\nFor the rig memory: nothing", true, []string{"A is wrong (a.go:1)", "B is wrong (b.go:2)"}},
		{"bullets and numbers", "Found, not fixed:\n- A (a.go:1)\n* B (b.go:2)\n3. C (c.go:3)", true, []string{"A (a.go:1)", "B (b.go:2)", "C (c.go:3)"}},
		{"the first on the heading's line", "Found, not fixed: A (a.go:1)\nB (b.go:2)", true, []string{"A (a.go:1)", "B (b.go:2)"}},
		{"nothing", "Found, not fixed: nothing\nFor the rig memory: nothing", true, nil},
		{"nothing on its own line", "Found, not fixed:\nNothing.\n", true, nil},
		{"no section", "Done.\nFor the rig memory: nothing", false, nil},
		{"in a sentence", "The step asks for a section headed Found, not fixed: and I wrote one.\nA is wrong", false, nil},
		{"in single quotes", "'Found, not fixed:' is the heading.\nA is wrong", false, nil},
		{"in double quotes", "\"Found, not fixed:\" is the heading.\nA is wrong", false, nil},
		{"in backticks", "`Found, not fixed:` is the heading.\nA is wrong", false, nil},
		{"quoted behind a >", "> Found, not fixed:\n> A is wrong", false, nil},
		{"bold heading", "**Found, not fixed:**\nA (a.go:1)", true, []string{"A (a.go:1)"}},
		{"ends at a blank line after a finding", "Found, not fixed:\nA (a.go:1)\n\nSome prose after.", true, []string{"A (a.go:1)"}},
		{"a blank line before the first finding", "Found, not fixed:\n\nA (a.go:1)", true, []string{"A (a.go:1)"}},
		{"ends at the next heading", "Found, not fixed:\nA (a.go:1)\nFor common errors:\nx: y", true, []string{"A (a.go:1)"}},
		{"ends at what's new", "Found, not fixed:\nA (a.go:1)\nWhat's new: none", true, []string{"A (a.go:1)"}},
		{"a line twice counts once", "Found, not fixed:\nA (a.go:1)\nA (a.go:1)", true, []string{"A (a.go:1)"}},
		{"the last heading is the section", "Found, not fixed:\nOld\n\nFound, not fixed:\nNew", true, []string{"New"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := application.ParseFoundNotFixed(c.comment)
			if got.Found != c.found || !slices.Equal(got.Lines, c.lines) {
				t.Errorf("expected found=%v lines %q, got found=%v lines %q", c.found, c.lines, got.Found, got.Lines)
			}
		})
	}
}

// The section changes nothing the other two sections read, written before them
// or, wrongly, after (mw-t5shinh.24).
func TestTheFoundNotFixedSectionChangesWhatTheFactAndCommonErrorReadersRead(t *testing.T) {
	rig := "For the rig memory:\nmw memory add millwright --kind gotcha --subject s --source mw-x.1 \"A sentence.\""
	errs := "For common errors:\nthe-class: the symptom"
	found := "Found, not fixed:\nA is wrong (a.go:1)\nB is wrong (b.go:2)"
	plain := application.ParseFactProposals("Done.\n" + errs + "\n" + rig)
	plainErrs := application.ParseCommonErrors("Done.\n" + errs + "\n" + rig)
	if !plain.Found || len(plain.Lines) != 1 || !plainErrs.Found || len(plainErrs.Lines) != 1 {
		t.Fatalf("the fixture is wrong: %+v %+v", plain, plainErrs)
	}
	for name, comment := range map[string]string{
		"before common errors": "Done.\n" + found + "\n" + errs + "\n" + rig,
		"after the rig memory": "Done.\n" + errs + "\n" + rig + "\n" + found,
		"between the two":      "Done.\n" + errs + "\n" + found + "\n" + rig,
	} {
		if got := application.ParseFactProposals(comment); !equalProposals(got, plain) {
			t.Errorf("%s: ParseFactProposals read %+v, expected %+v", name, got, plain)
		}
		if got := application.ParseCommonErrors(comment); !equalErrors(got, plainErrs) {
			t.Errorf("%s: ParseCommonErrors read %+v, expected %+v", name, got, plainErrs)
		}
	}
}

func equalProposals(a, b application.FactProposals) bool {
	return a.Found == b.Found && slices.Equal(a.Lines, b.Lines)
}

func equalErrors(a, b application.CommonErrorProposals) bool {
	return a.Found == b.Found && slices.Equal(a.Lines, b.Lines)
}

// A story landed by a batch files its lines once, as a single landing does.
func TestAStoryLandedByABatchFilesTheLinesOfItsFoundNotFixedSectionOnce(t *testing.T) {
	tracker, rules := aBatchEpicToTest(t)
	ctx := context.Background()
	mustDo(t, tracker.CommentOnStory(ctx, "mw-e.1", "Done.\nFound, not fixed:\nA is wrong (a.go:1)\nB is wrong (b.go:2)\nFor the rig memory: nothing"))
	before := tracker.Stories()

	aBatchLandedWithTesters(t, application.TesterTrial{}, tracker, rules, "mw-e.1", "mw-e.2")

	filed := theTestersFiled(t, tracker, before)
	if len(filed) != 2 {
		t.Fatalf("expected the 2 lines filed as 2 bugs, got %d: %+v", len(filed), filed)
	}
	for i, line := range []string{"A is wrong (a.go:1)", "B is wrong (b.go:2)"} {
		d := filed[i]
		if d.Story.Title != "[bug] "+line || d.Status != apptest.StatusDeferred || d.EpicID != "mw-e" ||
			!slices.Contains(d.Labels, "bug") || !slices.Contains(d.Labels, "found-at-close") ||
			!strings.Contains(d.Description, "> "+line) || !strings.Contains(d.Description, "Seen by: mw-e.1") {
			t.Errorf("expected a held bug under mw-e quoting %q, got %+v", line, d)
		}
	}
}

// failingToFileBugs is a tracker that cannot file a story.
type failingToFileBugs struct{ *apptest.FakeTracker }

func (failingToFileBugs) CreateStory(context.Context, application.NewStory) (string, error) {
	return "", errors.New("the tracker is away")
}

func TestATrackerThatRefusesTheCreateStillLandsTheStoryAndMailsALostWrite(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	mustDo(t, tracker.CommentOnStory(ctx, "mw-x.1", "Done.\nFound, not fixed:\nA is wrong (a.go:1)\nB is wrong (b.go:2)\nFor the rig memory: nothing"))
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	lands := &aLandingLanding{fakeRetryLanding{AheadCount: 1}}
	mailbox := apptest.NewFakeMailbox()

	report, err := application.Next{
		Tracker: failingToFileBugs{tracker}, Vault: vault, Worktrees: lands, Landing: lands,
		Checks: &scriptedChecks{passes: []bool{true}}, Slot: aSlot{}, Mailbox: mailbox,
		RetryWait: func(context.Context, time.Duration) error { return nil },
		Seat:      "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(ctx, "mw-x.1")

	if err != nil || report.Refused || !report.Landed {
		t.Fatalf("expected the story landed in spite of the bugs, got err %v and %+v", err, report)
	}
	mail, _ := mailbox.Inbox(ctx, application.MayorMailbox)
	var lost, landed []application.Message
	for _, m := range mail {
		switch {
		case strings.HasPrefix(m.Subject, "lost write"):
			lost = append(lost, m)
		case strings.HasPrefix(m.Subject, application.MailLanded):
			landed = append(landed, m)
		}
	}
	if len(lost) != 1 || len(landed) != 1 {
		t.Fatalf("expected one lost write mail and one Landed mail, got %+v", mail)
	}
	for _, want := range []string{"mw-x.1", "A is wrong (a.go:1)", "B is wrong (b.go:2)", "the tracker is away", "found-at-close"} {
		if !strings.Contains(lost[0].Subject+lost[0].Body, want) {
			t.Errorf("expected the lost write mail to name %q, got %+v", want, lost[0])
		}
	}
	if strings.Contains(landed[0].Body, "Filed: ") {
		t.Errorf("expected the Landed mail not to claim a bug filed, got %s", landed[0].Body)
	}
	if d, _ := tracker.ShowStory(ctx, "mw-x.1"); d.Status != apptest.StatusClosed {
		t.Errorf("expected mw-x.1 closed, got %q", d.Status)
	}
}
