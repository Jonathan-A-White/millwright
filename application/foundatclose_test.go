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

// A finding is the bead already filed when a bead not closed, labelled
// found-at-close, names the same file; and the files key is the finding's own
// (mw-gq6.1068).
func TestAFindingNamingTheFileOfAnOpenFoundAtCloseBeadIsNotFiledAgain(t *testing.T) {
	cases := []struct {
		name, title, description, status string
		labels                           []string
		filed                            bool
	}{
		{"the file and line in the title, held", "[bug] gofmt lists cmd/mw/member.go:1", "", apptest.StatusDeferred, []string{"bug", "found-at-close"}, false},
		{"the file with a line, in the description", "[bug] unformatted", "Reworded: cmd/mw/member.go:1 is not gofmt-clean.", apptest.StatusOpen, []string{"bug", "found-at-close"}, false},
		{"closed", "[bug] gofmt lists cmd/mw/member.go:1", "", apptest.StatusClosed, []string{"bug", "found-at-close"}, true},
		{"not found at close", "[bug] gofmt lists cmd/mw/member.go:1", "", apptest.StatusOpen, []string{"bug"}, true},
		{"another file", "[bug] gofmt lists cmd/mw/other.go", "", apptest.StatusOpen, []string{"bug", "found-at-close"}, true},
		{"the finder's own title does not count", "[bug] x", "mw-z.9 (fix cmd/mw/member.go) landed, and its closing comment lists this finding under 'Found, not fixed:':\n\n> unrelated\n", apptest.StatusOpen, []string{"bug", "found-at-close"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tracker, rules := aBatchEpicToTest(t)
			ctx := context.Background()
			tracker.AddStory("mw-e", domain.Story{ID: "mw-e.9", Title: c.title})
			mustDo(t, tracker.SetDescription("mw-e.9", c.description))
			mustDo(t, tracker.SetLabels("mw-e.9", c.labels...))
			mustDo(t, tracker.SetStatus("mw-e.9", c.status))
			mustDo(t, tracker.CommentOnStory(ctx, "mw-e.1", "Done.\nFound, not fixed:\nA formatting fault in cmd/mw/member.go:1\nFor the rig memory: nothing"))
			before := tracker.Stories()

			aBatchLandedWithTesters(t, application.TesterTrial{}, tracker, rules, "mw-e.1", "mw-e.2")

			filed := theTestersFiled(t, tracker, before)
			if c.filed != (len(filed) == 1) {
				t.Fatalf("expected filed=%v, got %d filed: %+v", c.filed, len(filed), filed)
			}
			if c.filed && filed[0].Merged().Files != "cmd/mw/member.go" {
				t.Errorf("expected the files key of the file the finding names, got %q", filed[0].Merged().Files)
			}
		})
	}
}

// A finding is the bead already filed only when that bead names the same fault:
// the same file:line, or the same symbol and the same fault words; a shared file
// or function alone is a near match, which is filed and named in the mail
// (mw-gq6.1103).
func TestAFindingSharingOnlyAFileWithAnOpenFoundAtCloseBeadIsFiledAndNamesItAsNear(t *testing.T) {
	cases := []struct {
		name, title, description, finding string
		filed                             bool
	}{
		{
			"the import finding against the docs budget bug",
			"[bug] docs/module-map.md and docs/overview.md are within 22 and 20 bytes of their budgets", "",
			"src/bulk/cut.ts:5 imports ../capture/captureStill from inside the capture module, which docs/module-map.md says never to do",
			true,
		},
		{
			"the same file and another line",
			"[bug] the parser drops a trailing line (application/parse.go:12)", "",
			"The loader reads the file twice (application/parse.go:40)",
			true,
		},
		{
			"the same symbol and other fault words",
			"[bug] `Job` is never reset between runs, so a second run reads the first run's output", "",
			"`Job` is not exported to the dispatcher package",
			true,
		},
		{
			"the same file:line, worded differently",
			"[bug] gofmt lists cmd/mw/member.go (cmd/mw/member.go:1)", "",
			"A formatting fault: cmd/mw/member.go:1 is not gofmt-clean",
			false,
		},
		{
			"the same symbol and the same fault words",
			"[bug] `Job` is never reset between runs, so a second run reads the first run's output", "",
			"A second run reads the first run's output because `Job` is never reset between runs",
			false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tracker, rules := aBatchEpicToTest(t)
			ctx := context.Background()
			tracker.AddStory("mw-e", domain.Story{ID: "mw-e.9", Title: c.title})
			mustDo(t, tracker.SetDescription("mw-e.9", c.description))
			mustDo(t, tracker.SetLabels("mw-e.9", "bug", "found-at-close"))
			mustDo(t, tracker.SetStatus("mw-e.9", apptest.StatusDeferred))
			mustDo(t, tracker.CommentOnStory(ctx, "mw-e.1", "Done.\nFound, not fixed:\n"+c.finding+"\nFor the rig memory: nothing"))
			before := tracker.Stories()

			aBatchLandedWithTesters(t, application.TesterTrial{}, tracker, rules, "mw-e.1", "mw-e.2")

			filed := theTestersFiled(t, tracker, before)
			if c.filed != (len(filed) == 1) {
				t.Fatalf("expected filed=%v, got %d filed: %+v", c.filed, len(filed), filed)
			}
			if c.filed && (filed[0].Story.Title != "[bug] "+c.finding || filed[0].Status != apptest.StatusDeferred ||
				!slices.Contains(filed[0].Labels, "found-at-close")) {
				t.Errorf("expected a held found-at-close bug of the finding, got %+v", filed[0])
			}
		})
	}
}
