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
		{"nothing then a blank line ends the section", "Found, not fixed: nothing\n\nNot used: no subagents.", true, nil},
		{"nothing on its own line then a blank line ends the section", "Found, not fixed:\nNothing.\n\nNot used: no subagents, no Fable advice.", true, nil},
		{"an empty heading, a blank line, then a finding", "Found, not fixed:\n\n- a finding (a.go:1)", true, []string{"a finding (a.go:1)"}},
		{"nothing directly followed by a sentence reads the sentence", "Found, not fixed: nothing\nNot used: no subagents.", true, []string{"Not used: no subagents."}},
		{"ends at the next heading", "Found, not fixed:\nA (a.go:1)\nFor common errors:\nx: y", true, []string{"A (a.go:1)"}},
		{"ends at what's new", "Found, not fixed:\nA (a.go:1)\nWhat's new: none", true, []string{"A (a.go:1)"}},
		{"a line twice counts once", "Found, not fixed:\nA (a.go:1)\nA (a.go:1)", true, []string{"A (a.go:1)"}},
		{"the last heading is the section", "Found, not fixed:\nOld\n\nFound, not fixed:\nNew", true, []string{"New"}},
		{"a trailing note in parentheses is no finding (mw-6ww.175.45)",
			"Found, not fixed:\nFiled: A is wrong (a.go:1). AC: go test ./a passes.\nFiled: B is wrong (b.go:2). AC: go test ./b passes.\nFiled: C is wrong (c.go:3). AC: go test ./c passes.\n(I did not run the tests named in these AC lines; the files named are my guess at where each belongs.)\n\nFor the rig memory: nothing",
			true, []string{"Filed: A is wrong (a.go:1). AC: go test ./a passes.", "Filed: B is wrong (b.go:2). AC: go test ./b passes.", "Filed: C is wrong (c.go:3). AC: go test ./c passes."}},
		{"a bulleted note in parentheses is no finding", "Found, not fixed:\n- A (a.go:1)\n- (note: B is by design.)", true, []string{"A (a.go:1)"}},
		{"a note with a parenthesis inside is no finding", "Found, not fixed:\nA (a.go:1)\n(I guessed (a.go) for each.)", true, []string{"A (a.go:1)"}},
		{"a note alone is no finding and says nothing", "Found, not fixed:\n(none seen)\nA (a.go:1)", true, []string{"A (a.go:1)"}},
		{"a note after nothing", "Found, not fixed: nothing\n(I ran no tests.)", true, nil},
		{"a finding that contains a parenthesis is a finding", "Found, not fixed:\nA is wrong (a.go:1) and B too", true, []string{"A is wrong (a.go:1) and B too"}},
		{"a finding that starts with a parenthesis and goes on is a finding", "Found, not fixed:\n(a.go:1) is wrong. AC: go test passes (here)", true, []string{"(a.go:1) is wrong. AC: go test passes (here)"}},
		{"a finding that starts with a parenthesis and ends with one after more text", "Found, not fixed:\n(a.go) is wrong (see b.go)", true, []string{"(a.go) is wrong (see b.go)"}},
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
// found-at-close, names the same file:line; and the files key is the finding's own
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
func TestAFindingIsTheBeadAlreadyFiledOnlyWhenItNamesTheSameFault(t *testing.T) {
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
			if c.filed && (!strings.HasPrefix(filed[0].Story.Title, "[bug] "+c.finding[:min(len(c.finding), 50)]) || !strings.Contains(filed[0].Description, "> "+c.finding) || filed[0].Status != apptest.StatusDeferred ||
				!slices.Contains(filed[0].Labels, "found-at-close")) {
				t.Errorf("expected a held found-at-close bug of the finding, got %+v", filed[0])
			}
		})
	}
}

// A finding that carries its own AC, Sweep and size is filed with them: the AC and
// Sweep in the acceptance, the size in the path, the title the finding alone
// (mw-gq6.1124).
func TestAFindingWithAnACAndASweepIsFiledWithThemInItsAcceptanceAndItsSizeInItsPath(t *testing.T) {
	tracker, rules := aBatchEpicToTest(t)
	ctx := context.Background()
	mustDo(t, tracker.CommentOnStory(ctx, "mw-e.1", "Done.\nFound, not fixed:\n"+
		"The parser drops a trailing line (application/parse.go:12). AC: go test ./application -run TestParseKeepsTheLastLine passes. "+
		"Sweep: every other reader of the lines in application/ is checked for the same drop. Size: S. By design? no\n"+
		"For the rig memory: nothing"))
	before := tracker.Stories()

	aBatchLandedWithTesters(t, application.TesterTrial{}, tracker, rules, "mw-e.1", "mw-e.2")

	filed := theTestersFiled(t, tracker, before)
	if len(filed) != 1 {
		t.Fatalf("expected 1 bug filed, got %d: %+v", len(filed), filed)
	}
	d := filed[0]
	if d.Story.Title != "[bug] The parser drops a trailing line (application/parse.go:12)" {
		t.Errorf("expected the title to be the finding alone, got %q", d.Story.Title)
	}
	for _, want := range []string{"go test ./application -run TestParseKeepsTheLastLine passes", "Sweep: every other reader of the lines in application/ is checked for the same drop"} {
		if !strings.Contains(d.Acceptance, want) {
			t.Errorf("expected the acceptance to hold %q, got %q", want, d.Acceptance)
		}
	}
	if d.Merged().Size != domain.SizeS {
		t.Errorf("expected the size S from the finding, got %q", d.Merged().Size)
	}
	if d.Merged().Files != "application/parse.go" {
		t.Errorf("expected the files key of the file the finding names, got %q", d.Merged().Files)
	}
	if strings.Contains(d.Description, "AC:") {
		t.Errorf("expected the description to quote the finding, not its AC: %q", d.Description)
	}
	comments, err := tracker.StoryComments(ctx, d.Story.ID)
	mustDo(t, err)
	for _, c := range comments {
		if application.IsHoldComment(c.Text) {
			t.Errorf("expected no hold reason on a bug with an AC, got %q", c.Text)
		}
	}
}

// A story filed from a finding is a different story from the finder: the
// finder's callers_ok is a reason about the finder's files, and its size and
// bump are its own, so none is copied, as mw path set --from copies none
// (mw-gq6.1195).
func TestAFoundStoryFiledAtCloseOutCarriesNoCallersOK(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A landed story", Overrides: domain.Path{
		CallersOK: "adds one new branch; the 44 callers are unchanged",
		Size:      domain.SizeM,
		Bump:      "minor",
		Files:     "application/next.go",
	}})
	mustDo(t, tracker.ClaimStory(ctx, "mw-x.1"))
	mustDo(t, tracker.SetStoryState(ctx, "mw-x.1", application.RunState, application.RunLanded, "landed on main"))
	mustDo(t, tracker.CommentOnStory(ctx, "mw-x.1", "Done.\nFound, not fixed:\n"+
		"The parser drops a trailing line (application/parse.go:12). AC: go test ./application -run TestParseKeepsTheLastLine passes. Sweep: the other readers. By design? no\n"+
		"For the rig memory: nothing"))
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

	filed := theTestersFiled(t, tracker, before)
	if len(filed) != 1 {
		t.Fatalf("expected 1 bug filed, got %d: %+v", len(filed), filed)
	}
	path := filed[0].Merged()
	if path.CallersOK != "" {
		t.Errorf("expected no callers_ok on the filed story, got %q", path.CallersOK)
	}
	if path.Size != "" {
		t.Errorf("expected no size copied from the finder, got %q", path.Size)
	}
	if path.Bump != "" {
		t.Errorf("expected no bump copied from the finder, got %q", path.Bump)
	}
	if path.Files != "application/parse.go" {
		t.Errorf("expected the files key of the file the finding names, got %q", path.Files)
	}
}

// A bug filed from a finding keeps whole a file name with several dots, and is
// not pinned to the finder's host: the host is about the finder's work
// (mw-gq6.1255, seen on mw-6ww.175.35 and mw-gq6.1248).
func TestAFoundStoryKeepsADottedFileNameWholeAndIsNotPinnedToTheFindersHost(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A landed story", Overrides: domain.Path{
		Host:  "desktop",
		Files: "application/next.go",
	}})
	mustDo(t, tracker.ClaimStory(ctx, "mw-x.1"))
	mustDo(t, tracker.SetStoryState(ctx, "mw-x.1", application.RunState, application.RunLanded, "landed on main"))
	mustDo(t, tracker.CommentOnStory(ctx, "mw-x.1", "Done.\nFound, not fixed:\n"+
		"The viewer spec misses a case (tests/e2e/attachment-viewer.spec.ts:12) and the helper test (src/a.test.tsx). AC: go test ./application passes. By design? no\n"+
		"For the rig memory: nothing"))
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

	filed := theTestersFiled(t, tracker, before)
	if len(filed) != 1 {
		t.Fatalf("expected 1 bug filed, got %d: %+v", len(filed), filed)
	}
	path := filed[0].Merged()
	if path.Files != "tests/e2e/attachment-viewer.spec.ts,src/a.test.tsx" {
		t.Errorf("expected both dotted file names whole in the files key, got %q", path.Files)
	}
	if path.Host != domain.HostAuto {
		t.Errorf("expected the host auto, not the finder's, got %q", path.Host)
	}
}

// aSingleLandingSaying lands mw-x.1 with the closing comment given and returns
// what it filed and the Landed mail's body.
func aSingleLandingSaying(t *testing.T, comment string) ([]application.StoryDetail, string) {
	t.Helper()
	return aSingleLandingSayingAll(t, comment)
}

// aSingleLandingSayingAll is aSingleLandingSaying for a story with several
// comments, the first written first.
func aSingleLandingSayingAll(t *testing.T, comments ...string) ([]application.StoryDetail, string) {
	t.Helper()
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	for _, comment := range comments {
		mustDo(t, tracker.CommentOnStory(ctx, "mw-x.1", comment))
	}
	before := tracker.Stories()
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	lands := &aLandingLanding{fakeRetryLanding{AheadCount: 1}}
	mailbox := apptest.NewFakeMailbox()
	report, err := application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands, Landing: lands,
		Checks: &scriptedChecks{passes: []bool{true}}, Slot: aSlot{}, Mailbox: mailbox,
		RetryWait: func(context.Context, time.Duration) error { return nil },
		Seat:      "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(ctx, "mw-x.1")
	if err != nil || report.Refused || !report.Landed {
		t.Fatalf("expected the story landed, got err %v and %+v", err, report)
	}
	mail, _ := mailbox.Inbox(ctx, application.MayorMailbox)
	body := ""
	for _, m := range mail {
		if strings.HasPrefix(m.Subject, application.MailLanded) {
			body = m.Body
		}
	}
	return theTestersFiled(t, tracker, before), body
}

// A finding is filed at P3: the Mayor raises what he wants worked first.
func TestFoundNotFixedFilesAFindingAtP3(t *testing.T) {
	filed, _ := aSingleLandingSaying(t, "Done.\nFound, not fixed:\nA is wrong (a.go:1). AC: go test ./x passes. By design? no\nFor the rig memory: nothing")
	if len(filed) != 1 || filed[0].Priority != 3 {
		t.Fatalf("expected 1 bug at P3, got %+v", filed)
	}
}

// A Mayor's comment after the Builder's closing comment, with a 'For the rig
// memory:' line and no 'Found, not fixed:' heading, does not withdraw the
// Builder's findings (mw-gq6.1547).
func TestFoundNotFixedOfTheBuildersCommentIsFiledAfterAMayorsCommentWithoutTheSection(t *testing.T) {
	filed, _ := aSingleLandingSayingAll(t,
		"Done.\nFound, not fixed:\nA is wrong (a.go:1). AC: go test ./x passes. By design? no\nB is wrong (b.go:2). AC: go test ./y passes. By design? no\nFor the rig memory: nothing",
		"Re-scoped: the acceptance is met.\nFor the rig memory: as the Builder's comment proposes")
	if len(filed) != 2 {
		t.Fatalf("expected the Builder's 2 findings filed as 2 bugs, got %d: %+v", len(filed), filed)
	}
}

// Four findings file two bugs and one that lists the other two.
func TestFoundAtCloseFilesTwoBugsAndOneListingTheRest(t *testing.T) {
	filed, _ := aSingleLandingSaying(t, "Done.\nFound, not fixed:\nA is wrong (a.go:1)\nB is wrong (b.go:2)\nC is wrong (c.go:3)\nD is wrong (d.go:4)\nFor the rig memory: nothing")
	if len(filed) != 3 {
		t.Fatalf("expected 2 bugs and 1 listing the rest, got %d: %+v", len(filed), filed)
	}
	var titles []string
	for _, d := range filed {
		titles = append(titles, d.Story.Title)
	}
	slices.Sort(titles)
	want := "[bug] mw-x.1: 2 more findings not fixed, past the first 2"
	if !slices.Contains(titles, want) {
		t.Errorf("expected a bead titled %q, got %q", want, titles)
	}
	last := filed[2]
	if !strings.Contains(last.Description, "C is wrong") || !strings.Contains(last.Description, "D is wrong") {
		t.Errorf("expected the third bug to quote C and D, got %q", last.Description)
	}
}

// A finding the Builder says is by design files nothing, and the Landed mail
// says so.
func TestFoundNotFixedByDesignFilesNothingAndTheMailSaysSo(t *testing.T) {
	filed, mail := aSingleLandingSaying(t, "Done.\nFound, not fixed:\nThe loader reads the file twice (application/load.go:40). AC: go test ./application -run TestLoadReadsOnce passes. By design? yes\nFor the rig memory: nothing")
	if len(filed) != 0 {
		t.Fatalf("expected no bug filed for a by-design finding, got %+v", filed)
	}
	if !strings.Contains(mail, "By design, not filed:") || !strings.Contains(mail, "The loader reads the file twice") {
		t.Errorf("expected the mail to name the by-design finding as not filed, got %q", mail)
	}
}

// A by-design finding does not use up a place of the cap.
func TestFoundNotFixedByDesignFindingsDoNotCountPastTheCap(t *testing.T) {
	filed, _ := aSingleLandingSaying(t, "Done.\nFound, not fixed:\nA is wrong (a.go:1). By design? yes\nB is wrong (b.go:2)\nC is wrong (c.go:3)\nFor the rig memory: nothing")
	if len(filed) != 2 {
		t.Fatalf("expected the 2 other findings filed as 2 bugs, got %+v", filed)
	}
}

// A finding with no AC is still filed, held, with the reason 'no AC from the
// Builder' written as a HELD comment, so nothing is lost.
func TestAFindingWithNoACIsFiledHeldWithTheReasonNoACFromTheBuilder(t *testing.T) {
	tracker, rules := aBatchEpicToTest(t)
	ctx := context.Background()
	mustDo(t, tracker.CommentOnStory(ctx, "mw-e.1", "Done.\nFound, not fixed:\nThe parser drops a trailing line (application/parse.go:12)\nFor the rig memory: nothing"))
	before := tracker.Stories()

	aBatchLandedWithTesters(t, application.TesterTrial{}, tracker, rules, "mw-e.1", "mw-e.2")

	filed := theTestersFiled(t, tracker, before)
	if len(filed) != 1 || filed[0].Status != apptest.StatusDeferred || filed[0].Acceptance != "" {
		t.Fatalf("expected 1 held bug with no acceptance, got %+v", filed)
	}
	comments, err := tracker.StoryComments(ctx, filed[0].Story.ID)
	mustDo(t, err)
	if len(comments) != 1 || comments[0].Text != application.HeldCommentPrefix+" no AC from the Builder" {
		t.Errorf("expected the hold reason 'HELD: no AC from the Builder', got %+v", comments)
	}
}

func TestParseFoundItem(t *testing.T) {
	cases := []struct {
		name, line, what, ac, sweep string
		size                        domain.Size
		byDesign                    string
	}{
		{"bare", "A is wrong (a.go:1)", "A is wrong (a.go:1)", "", "", "", ""},
		{"all four", "A is wrong (a.go:1). AC: go test ./x passes. Sweep: grep the rest. Size: m. By design? yes", "A is wrong (a.go:1)", "go test ./x passes", "grep the rest", domain.SizeM, "yes"},
		{"by design with a colon", "A (a.go:1) AC: go vet ./... exits 0; By design?: no", "A (a.go:1)", "go vet ./... exits 0", "", "", "no"},
		{"an unknown size is none", "A (a.go:1) AC: x. Size: huge", "A (a.go:1)", "x", "", "", ""},
		{"a word ending in ac is no marker", "The mac: address is wrong (a.go:1)", "The mac: address is wrong (a.go:1)", "", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := application.ParseFoundItem(c.line)
			if got.What != c.what || got.AC != c.ac || got.Sweep != c.sweep || got.Size != c.size || got.ByDesign != c.byDesign {
				t.Errorf("got %+v", got)
			}
		})
	}
}

// aStoryLandedWithAFinding lands mw-x.1 with a closing comment holding finding
// under 'Found, not fixed:' while mw-x.9, a held found-at-close bead, is open,
// and gives the vault the close-out wrote into.
func aStoryLandedWithAFinding(t *testing.T, finding, otherTitle, otherDescription string) *fakeVault {
	t.Helper()
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A landed story", Overrides: domain.Path{Files: "application/next.go"}})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.9", Title: otherTitle})
	mustDo(t, tracker.SetDescription("mw-x.9", otherDescription))
	mustDo(t, tracker.SetLabels("mw-x.9", "bug", "found-at-close"))
	mustDo(t, tracker.SetStatus("mw-x.9", apptest.StatusDeferred))
	mustDo(t, tracker.ClaimStory(ctx, "mw-x.1"))
	mustDo(t, tracker.SetStoryState(ctx, "mw-x.1", application.RunState, application.RunLanded, "landed on main"))
	mustDo(t, tracker.CommentOnStory(ctx, "mw-x.1", "Done.\nFound, not fixed:\n"+finding+"\nFor the rig memory: nothing"))
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
	return vault
}

// A finding joined to an open bead, or filed as a near match of one, is said in
// the close-out's log, runs/<story>/next-<n>.log: the finding, the bead and the
// rule that matched, so that a wrong join can be found afterwards (mw-gq6.1107).
func TestAJoinOfAFindingToAnOpenBeadIsWrittenInTheCloseOutsLog(t *testing.T) {
	cases := []struct {
		name, finding, title, description string
		wants                             []string
	}{
		{
			"the same file and line",
			"A formatting fault: cmd/mw/member.go:1 is not gofmt-clean",
			"[bug] gofmt lists cmd/mw/member.go (cmd/mw/member.go:1)", "",
			[]string{"A formatting fault: cmd/mw/member.go:1 is not gofmt-clean", "mw-x.9", "cmd/mw/member.go:1"},
		},
		{
			"the same symbol and fault words",
			"A second run reads the first run's output because `Job` is never reset between runs",
			"[bug] `Job` is never reset between runs, so a second run reads the first run's output", "",
			[]string{"`Job` is never reset between runs", "mw-x.9", "Job", "second", "output"},
		},
		{
			"a near match",
			"The loader reads the file twice (application/parse.go:40)",
			"[bug] the parser drops a trailing line (application/parse.go:12)", "",
			[]string{"The loader reads the file twice", "mw-x.9", "near", "application/parse.go"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			vault := aStoryLandedWithAFinding(t, c.finding, c.title, c.description)
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
			if line == "" {
				t.Fatalf("expected one line of the log to name mw-x.9, got %q", log)
			}
			for _, want := range c.wants {
				if !strings.Contains(line, want) {
					t.Errorf("expected the line to say %q, got %q", want, line)
				}
			}
			if strings.Count(log, "mw-x.9") != 1 {
				t.Errorf("expected exactly one line naming mw-x.9, got %q", log)
			}
		})
	}
}

// mw-gq6.1288: a bug filed from a finding under an epic that says why its host
// stays is worked on that host, not on any host.
func TestAFoundStoryOfAnEpicWithAHostWhyKeepsTheEpicsHost(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature",
		Host: "laptop", HostWhy: "the Morpheus cache is made on the Laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A landed story", Overrides: domain.Path{Files: "application/next.go"}})
	mustDo(t, tracker.ClaimStory(ctx, "mw-x.1"))
	mustDo(t, tracker.SetStoryState(ctx, "mw-x.1", application.RunState, application.RunLanded, "landed on main"))
	mustDo(t, tracker.CommentOnStory(ctx, "mw-x.1", "Done.\nFound, not fixed:\n"+
		"The parser drops a line (application/next.go:12). AC: go test ./application passes. By design? no\n"+
		"For the rig memory: nothing"))
	before := tracker.Stories()
	lands := &fakeRetryLanding{}
	_, err := application.Next{
		Tracker: tracker, Vault: newFakeVault(), Worktrees: lands, Landing: lands,
		Checks: checkStub{}, Slot: slotStub{}, Mailbox: apptest.NewFakeMailbox(),
		Seat: "builder", Host: "cloud2",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(ctx, "mw-x.1")
	mustDo(t, err)
	filed := theTestersFiled(t, tracker, before)
	if len(filed) != 1 {
		t.Fatalf("expected 1 bug filed, got %d", len(filed))
	}
	if got := filed[0].Merged(); got.Host != "laptop" || got.HostWhy == "" {
		t.Errorf("expected the epic's host=laptop and its why, got host=%q host_why=%q", got.Host, got.HostWhy)
	}
}

// aFindingOfAnotherRigRun files the findings text said by a story of lampas
// (rig lampas, branch main, formula tdd-feature, files package.json) and
// returns the bugs filed. A story of cairn sits in the tracker with its own
// branch, formula and model, as the only place the rig's defaults are read from.
func aFindingOfAnotherRigRun(t *testing.T, finding string) []domain.Path {
	t.Helper()
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-c", domain.Path{Rig: "cairn", Branch: "trunk", Harness: domain.HarnessClaude, Model: "opus", Effort: "medium", Formula: "plain", Host: "laptop"})
	tracker.AddStory("mw-c", domain.Story{ID: "mw-c.1", Title: "A cairn story", Overrides: domain.Path{Files: "src/a.ts"}})
	tracker.AddEpic("mw-x", domain.Path{Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A landed story", Overrides: domain.Path{Files: "package.json"}})
	mustDo(t, tracker.ClaimStory(ctx, "mw-x.1"))
	mustDo(t, tracker.SetStoryState(ctx, "mw-x.1", application.RunState, application.RunLanded, "landed on main"))
	mustDo(t, tracker.CommentOnStory(ctx, "mw-x.1", "Done.\nFound, not fixed:\n"+finding+"\nFor the rig memory: nothing"))
	before := tracker.Stories()
	lands := &fakeRetryLanding{}
	_, err := application.Next{
		Tracker: tracker, Vault: newFakeVault(), Worktrees: lands, Landing: lands,
		Checks: checkStub{}, Slot: slotStub{}, Mailbox: apptest.NewFakeMailbox(),
		Seat: "builder", Host: "laptop",
		Rigs: map[string]string{"lampas": "/rigs/lampas", "cairn": "/rigs/Cairn", "legend": "/rigs/legend"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(ctx, "mw-x.1")
	mustDo(t, err)
	var paths []domain.Path
	for _, d := range theTestersFiled(t, tracker, before) {
		paths = append(paths, d.Merged())
	}
	return paths
}

// A finding that names a file of another rig by that rig's directory is filed
// in that rig, with that rig's defaults and the file without the prefix, not
// with the reporting story's rig (mw-6ww.175.72, mw-gq6.1317).
func TestFoundAtCloseFindingNamingAnotherRigsFileIsFiledInThatRig(t *testing.T) {
	filed := aFindingOfAnotherRigRun(t, "Cairn carries the same fault (Cairn/vite.config.ts:19). AC: go test ./application passes. By design? no")
	if len(filed) != 1 {
		t.Fatalf("expected 1 bug filed, got %d", len(filed))
	}
	got := filed[0]
	if got.Rig != "cairn" || got.Files != "vite.config.ts" {
		t.Errorf("expected rig=cairn files=vite.config.ts, got rig=%q files=%q", got.Rig, got.Files)
	}
	if got.Branch != "trunk" || got.Formula != "plain" || got.Model != "opus" || got.Effort != "medium" {
		t.Errorf("expected the defaults of cairn's own story (branch trunk, formula plain, opus, medium), got %s", got.Summary())
	}
	if got.Host != domain.HostAuto {
		t.Errorf("expected host auto, got %q", got.Host)
	}
}

// A finding of another rig named in its words, its files bare, is that rig's.
func TestFoundAtCloseFindingNamingAnotherRigInItsWordsIsFiledInThatRig(t *testing.T) {
	filed := aFindingOfAnotherRigRun(t, "In legend the build config drops the base (vite.config.ts:31). AC: go test ./application passes. By design? no")
	if len(filed) != 1 {
		t.Fatalf("expected 1 bug filed, got %d", len(filed))
	}
	if got := filed[0]; got.Rig != "legend" || got.Files != "vite.config.ts" {
		t.Errorf("expected rig=legend files=vite.config.ts, got rig=%q files=%q", got.Rig, got.Files)
	}
}

// A rig named like a common word (legend) in the prose of a finding about the
// finder's own rig, with no file of another rig, does not move the bug to that
// rig (mw-gq6.1350).
func TestFoundAtCloseFindingUsingARigNameAsACommonWordKeepsItsRig(t *testing.T) {
	for name, finding := range map[string]string{
		"the":  "The parser drops the legend of the table (package.json:3). AC: go test ./application passes. By design? no",
		"a":    "The config is a legend for the build (package.json:3). AC: go test ./application passes. By design? no",
		"this": "This legend is dropped by the config (package.json:3). AC: go test ./application passes. By design? no",
	} {
		t.Run(name, func(t *testing.T) {
			filed := aFindingOfAnotherRigRun(t, finding)
			if len(filed) != 1 {
				t.Fatalf("expected 1 bug filed, got %d", len(filed))
			}
			if got := filed[0]; got.Rig != "lampas" || got.Files != "package.json" {
				t.Errorf("expected rig=lampas files=package.json, got %s", got.Summary())
			}
		})
	}
}

// A finding whose rig cannot be told is filed with no rig, never the reporting
// story's: its files name two other rigs, or one of them and one of its own.
func TestFoundAtCloseFindingWhoseRigCannotBeToldIsFiledWithNoRig(t *testing.T) {
	for name, finding := range map[string]string{
		"two other rigs":          "Both carry it (Cairn/vite.config.ts:19 and legend/vite.config.ts:31). AC: go test ./application passes. By design? no",
		"another rig and its own": "The fault is in Cairn/vite.config.ts:19 and in package.json:3. AC: go test ./application passes. By design? no",
	} {
		t.Run(name, func(t *testing.T) {
			filed := aFindingOfAnotherRigRun(t, finding)
			if len(filed) != 1 {
				t.Fatalf("expected 1 bug filed, got %d", len(filed))
			}
			if got := filed[0]; got.Rig != "" {
				t.Errorf("expected no rig, got rig=%q (%s)", got.Rig, got.Summary())
			}
		})
	}
}

// A finding that names only the reporting rig's own files is filed as before.
func TestFoundAtCloseFindingNamingOnlyTheReportingRigsFilesKeepsItsRig(t *testing.T) {
	filed := aFindingOfAnotherRigRun(t, "The config drops the base (package.json:3). AC: go test ./application passes. By design? no")
	if len(filed) != 1 {
		t.Fatalf("expected 1 bug filed, got %d", len(filed))
	}
	if got := filed[0]; got.Rig != "lampas" || got.Files != "package.json" || got.Branch != "main" {
		t.Errorf("expected rig=lampas files=package.json branch=main, got %s", got.Summary())
	}
}

// A bare file name that a path with a folder in the same finding ends with is
// that path's own second mention, so the files key keeps the path alone; a bare
// name named alone stays (mw-gq6.1415, seen on mw-indx1x6.92).
func TestFoundAtCloseFilesDropsABareNameItsFullPathNames(t *testing.T) {
	cases := []struct {
		name, finding, want string
	}{
		{
			"the path then its bare name",
			"The step reads the wrong table (features/steps/sermons.steps.tsx:87) and again in sermons.steps.tsx:246. AC: go test ./application passes. By design? no",
			"features/steps/sermons.steps.tsx",
		},
		{
			"the bare name first",
			"The step in sermons.steps.tsx:246 matches twice, as in features/steps/sermons.steps.tsx:87. AC: go test ./application passes. By design? no",
			"features/steps/sermons.steps.tsx",
		},
		{
			"a bare name alone",
			"The reader drops a row (Reader.tsx:12). AC: go test ./application passes. By design? no",
			"Reader.tsx",
		},
		{
			"a bare name of another file stays",
			"The step reads the wrong table (features/steps/sermons.steps.tsx:87) and Reader.tsx:12 too. AC: go test ./application passes. By design? no",
			"features/steps/sermons.steps.tsx,Reader.tsx",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			filed := aFindingOfAnotherRigRun(t, c.finding)
			if len(filed) != 1 {
				t.Fatalf("expected 1 bug filed, got %d", len(filed))
			}
			if got := filed[0].Files; got != c.want {
				t.Errorf("expected the files key %q, got %q", c.want, got)
			}
		})
	}
}

// The rig a finding is about is read from the same list of files, so a bare name
// beside the full path of another rig's file does not make the finding's files
// mixed (mw-gq6.1415).
func TestFoundAtCloseRigReadingDropsABareNameItsFullPathNames(t *testing.T) {
	filed := aFindingOfAnotherRigRun(t, "Cairn drops a row (Cairn/src/sermons.steps.tsx:87) and again in sermons.steps.tsx:246. AC: go test ./application passes. By design? no")
	if len(filed) != 1 {
		t.Fatalf("expected 1 bug filed, got %d", len(filed))
	}
	if got := filed[0]; got.Rig != "cairn" || got.Files != "src/sermons.steps.tsx" {
		t.Errorf("expected rig=cairn files=src/sermons.steps.tsx, got rig=%q files=%q", got.Rig, got.Files)
	}
}

// aFindingMeetingAStoryRun files the finding said by a landed story of lampas
// and returns what was filed and the tracker, with one other story in the
// tracker: in the epic of rig, with status and title and description as given.
func aFindingMeetingAStoryRun(t *testing.T, finding, rig, status, title, description string) ([]application.StoryDetail, *apptest.FakeTracker) {
	t.Helper()
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-c", domain.Path{Rig: "cairn", Branch: "trunk", Harness: domain.HarnessClaude, Model: "opus", Effort: "medium", Formula: "plain", Host: "laptop"})
	tracker.AddEpic("mw-x", domain.Path{Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A landed story", Overrides: domain.Path{Files: "package.json"}})
	mustDo(t, tracker.ClaimStory(ctx, "mw-x.1"))
	mustDo(t, tracker.SetStoryState(ctx, "mw-x.1", application.RunState, application.RunLanded, "landed on main"))
	mustDo(t, tracker.CommentOnStory(ctx, "mw-x.1", "Done.\nFound, not fixed:\n"+finding+"\nFor the rig memory: nothing"))
	epic := map[string]string{"lampas": "mw-x", "cairn": "mw-c"}[rig]
	tracker.AddStory(epic, domain.Story{ID: epic + ".9", Title: title})
	mustDo(t, tracker.SetDescription(epic+".9", description))
	mustDo(t, tracker.SetStatus(epic+".9", status))
	before := tracker.Stories()
	lands := &fakeRetryLanding{}
	_, err := application.Next{
		Tracker: tracker, Vault: newFakeVault(), Worktrees: lands, Landing: lands,
		Checks: checkStub{}, Slot: slotStub{}, Mailbox: apptest.NewFakeMailbox(),
		Seat: "builder", Host: "laptop",
		Rigs: map[string]string{"lampas": "/rigs/lampas", "cairn": "/rigs/Cairn"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(ctx, "mw-x.1")
	mustDo(t, err)
	return theTestersFiled(t, tracker, before), tracker
}

const aFindingOfTheSectionsLeftOut = "Several sections are left out of data/sections.json although their title prints a text. AC: go test ./application passes. By design? no"

// A bug filed for a finding that a story of the same rig, being worked now,
// names the same file and fault words of, waits on that story and says it may
// duplicate it; it is not released (mw-gq6.1440).
func TestFoundNotFixedNamingAnInProgressStoryWaitsOnIt(t *testing.T) {
	filed, tracker := aFindingMeetingAStoryRun(t, aFindingOfTheSectionsLeftOut, "lampas", apptest.StatusInProgress,
		"Add the sections left out of data/sections.json", "The sections whose title prints a text are left out of data/sections.json; add them.")
	if len(filed) != 1 {
		t.Fatalf("expected 1 bug filed, got %d", len(filed))
	}
	bug := filed[0]
	if bug.Status != apptest.StatusDeferred {
		t.Errorf("expected the bug held, got status %q", bug.Status)
	}
	if !slices.Contains(bug.Needs, "mw-x.9") {
		t.Errorf("expected the bug to wait on mw-x.9, got needs %v", bug.Needs)
	}
	if !slices.ContainsFunc(tracker.Comments(bug.Story.ID), func(c string) bool { return strings.Contains(c, "may duplicate mw-x.9") }) {
		t.Errorf("expected a 'may duplicate mw-x.9' note, got %v", tracker.Comments(bug.Story.ID))
	}
}

// A story of another rig, a closed one, one not being worked, or one that
// shares only a word or only the file is not named a possible duplicate and is
// not waited on.
func TestFoundNotFixedMeetingNoStoryInFlightOfItsRigWaitsOnNothing(t *testing.T) {
	cases := []struct{ name, rig, status, title, description string }{
		{"another rig", "cairn", apptest.StatusInProgress, "Add the sections left out of data/sections.json", "The sections whose title prints a text are left out of data/sections.json."},
		{"closed", "lampas", apptest.StatusClosed, "Add the sections left out of data/sections.json", "The sections whose title prints a text are left out of data/sections.json."},
		{"not claimed", "lampas", apptest.StatusOpen, "Add the sections left out of data/sections.json", "The sections whose title prints a text are left out of data/sections.json."},
		{"one shared word", "lampas", apptest.StatusInProgress, "Pair the sections in the Lampas LXX list", "The pairing drops a verse."},
		{"only the file", "lampas", apptest.StatusInProgress, "Sort data/sections.json by date", "The file is sorted by the year."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			filed, tracker := aFindingMeetingAStoryRun(t, aFindingOfTheSectionsLeftOut, c.rig, c.status, c.title, c.description)
			if len(filed) != 1 {
				t.Fatalf("expected 1 bug filed, got %d", len(filed))
			}
			if len(filed[0].Needs) != 0 {
				t.Errorf("expected no wait, got needs %v", filed[0].Needs)
			}
			for _, note := range tracker.Comments(filed[0].Story.ID) {
				if strings.Contains(note, "may duplicate") {
					t.Errorf("expected no duplicate note, got %q", note)
				}
			}
		})
	}
}

// A bug filed from a finding runs on the rig's default route, sonnet at high
// effort, and not on the finder's model and effort: the finder's are about its
// own work, and a dearer model needs its reason written. The rig, branch,
// harness, formula and host are copied as before, and the description names the
// finder's model and effort so the Mayor may raise the bug with a reason
// (mw-gq6.1426).
func TestAFoundStoryRunsOnSonnetHighNotOnTheFindersFableXHighAndSaysWhatTheFinderRanOn(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "fable", Effort: "xhigh", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A landed story", Overrides: domain.Path{Files: "application/next.go"}})
	mustDo(t, tracker.ClaimStory(ctx, "mw-x.1"))
	mustDo(t, tracker.SetStoryState(ctx, "mw-x.1", application.RunState, application.RunLanded, "landed on main"))
	mustDo(t, tracker.CommentOnStory(ctx, "mw-x.1", "Done.\nFound, not fixed:\n"+
		"The parser drops a trailing line (application/parse.go:12). AC: go test ./application -run TestParseKeepsTheLastLine passes. By design? no\n"+
		"For the rig memory: nothing"))
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

	filed := theTestersFiled(t, tracker, before)
	if len(filed) != 1 {
		t.Fatalf("expected 1 bug filed, got %d: %+v", len(filed), filed)
	}
	got := filed[0].Merged()
	if got.Model != domain.ModelSonnet || got.Effort != domain.EffortHigh {
		t.Errorf("expected sonnet/high, got %s/%s", got.Model, got.Effort)
	}
	if got.Rig != "millwright" || got.Branch != "main" || got.Harness != domain.HarnessClaude || got.Formula != "tdd-feature" || got.Host != domain.HostAuto {
		t.Errorf("expected rig, branch, harness, formula copied and host auto, got %s", got.Summary())
	}
	line := ""
	for _, l := range strings.Split(filed[0].Description, "\n") {
		if strings.HasPrefix(l, "Finder mw-x.1 ran on ") {
			line = l
		}
	}
	if !strings.Contains(line, "fable/xhigh") || !strings.Contains(line, "sonnet/high") {
		t.Errorf("expected a line naming the finder's fable/xhigh and the bug's sonnet/high, got %q in:\n%s", line, filed[0].Description)
	}
}

// A bug of another rig whose newest story is not known gets the default route
// too, not the finder's model and effort (mw-gq6.1426).
func TestAFoundStoryOfAnotherRigWithNoKnownStoryRunsOnSonnetHigh(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude, Model: "fable", Effort: "xhigh", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A landed story", Overrides: domain.Path{Files: "package.json"}})
	mustDo(t, tracker.ClaimStory(ctx, "mw-x.1"))
	mustDo(t, tracker.SetStoryState(ctx, "mw-x.1", application.RunState, application.RunLanded, "landed on main"))
	mustDo(t, tracker.CommentOnStory(ctx, "mw-x.1", "Done.\nFound, not fixed:\n"+
		"Both carry it (Cairn/vite.config.ts:19 and legend/vite.config.ts:31). AC: go test ./application passes. By design? no\nFor the rig memory: nothing"))
	before := tracker.Stories()
	lands := &fakeRetryLanding{}
	_, err := application.Next{
		Tracker: tracker, Vault: newFakeVault(), Worktrees: lands, Landing: lands,
		Checks: checkStub{}, Slot: slotStub{}, Mailbox: apptest.NewFakeMailbox(),
		Seat: "builder", Host: "laptop",
		Rigs: map[string]string{"lampas": "/rigs/lampas", "cairn": "/rigs/Cairn", "legend": "/rigs/legend"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(ctx, "mw-x.1")
	mustDo(t, err)
	filed := theTestersFiled(t, tracker, before)
	if len(filed) != 1 {
		t.Fatalf("expected 1 bug filed, got %d", len(filed))
	}
	if got := filed[0].Merged(); got.Model != domain.ModelSonnet || got.Effort != domain.EffortHigh {
		t.Errorf("expected sonnet/high, got %s/%s", got.Model, got.Effort)
	}
	if !strings.Contains(filed[0].Description, "Finder mw-x.1 ran on fable/xhigh") {
		t.Errorf("expected the finder's route named, got:\n%s", filed[0].Description)
	}
}

// The bead that lists the findings past the cap carries the Builder's AC and
// Sweep of the one past it, the Sweep on a line of its own, so dispatch does not
// refuse it for want of a sweep (mw-gq6.1627).
func TestFoundAtCloseOverflowBeadHoldsTheACAndSweepOfTheThirdFinding(t *testing.T) {
	filed, _ := aSingleLandingSaying(t, "Done.\nFound, not fixed:\nA is wrong (a.go:1)\nB is wrong (b.go:2)\nC is wrong (c.go:3). AC: go test ./x -run TestC passes. Sweep: grep the other readers. Size: S. By design? no\nFor the rig memory: nothing")
	if len(filed) != 3 {
		t.Fatalf("expected 3 bugs, got %d: %+v", len(filed), filed)
	}
	over := filed[2]
	if !strings.Contains(over.Story.Title, "1 more findings not fixed") {
		t.Fatalf("expected the overflow bead last, got %q", over.Story.Title)
	}
	if !strings.Contains(over.Acceptance, "go test ./x -run TestC passes") {
		t.Errorf("expected the AC of the third finding in the acceptance, got %q", over.Acceptance)
	}
	if !slices.Contains(strings.Split(over.Acceptance, "\n"), "Sweep: grep the other readers") {
		t.Errorf("expected the Sweep on a line of its own, got %q", over.Acceptance)
	}
	if domain.SweepOf(over.Acceptance) != domain.SweepGiven {
		t.Errorf("expected the acceptance to pass the sweep rule, got %q", over.Acceptance)
	}
}

// With two findings past the cap, each one's AC is in the acceptance, labelled
// with its finding; one with no AC leaves nothing of its own.
func TestFoundAtCloseOverflowBeadHoldsEachFindingsACLabelled(t *testing.T) {
	filed, _ := aSingleLandingSaying(t, "Done.\nFound, not fixed:\nA is wrong (a.go:1)\nB is wrong (b.go:2)\nC is wrong (c.go:3). AC: go test ./c passes. Sweep: look at c\nD is wrong (d.go:4). AC: go test ./d passes. Sweep: look at d\nFor the rig memory: nothing")
	if len(filed) != 3 {
		t.Fatalf("expected 3 bugs, got %d: %+v", len(filed), filed)
	}
	acc := filed[2].Acceptance
	for _, want := range []string{"C is wrong (c.go:3)", "go test ./c passes", "Sweep: look at c", "D is wrong (d.go:4)", "go test ./d passes", "Sweep: look at d"} {
		if !strings.Contains(acc, want) {
			t.Errorf("expected the acceptance to hold %q, got %q", want, acc)
		}
	}
	cIdx, dIdx := strings.Index(acc, "go test ./c"), strings.Index(acc, "go test ./d")
	if strings.Index(acc, "D is wrong") < cIdx || dIdx < strings.Index(acc, "Sweep: look at c") {
		t.Errorf("expected each AC beside its own finding, got %q", acc)
	}
}

// Findings past the cap that give no AC leave the overflow bead held, as before.
func TestFoundAtCloseOverflowBeadWithNoACIsHeldForIt(t *testing.T) {
	filed, _ := aSingleLandingSaying(t, "Done.\nFound, not fixed:\nA is wrong (a.go:1)\nB is wrong (b.go:2)\nC is wrong (c.go:3)\nFor the rig memory: nothing")
	if len(filed) != 3 || filed[2].Acceptance != "" || filed[2].Status != apptest.StatusDeferred {
		t.Fatalf("expected a held overflow bead with no acceptance, got %+v", filed)
	}
}

// A finding whose fault a bead closed since the finder's branch forked holds
// (landed, or closed as superseded naming the bead that carries it) is not filed
// again: the finder's branch saw the fault unfixed, and it is not now. A bead
// closed before the branch forked is no such bead, and the finding is filed
// (mw-gq6.1638).
func TestAFindingWhoseFaultAClosedBeadHoldsIsNotFiledAgain(t *testing.T) {
	forked := time.Date(2026, 11, 22, 4, 0, 0, 0, time.UTC)
	const finding = "`CureLeavesFault` leaves the claude login fault standing after the cure (infrastructure/doctor/claudelogin.go:40)"
	const fixed = "Make the claude login cure change what it names: `CureLeavesFault` left the claude login fault standing after the cure."
	cases := []struct {
		name      string
		closedAt  time.Time
		reason    string
		carrier   bool
		wantFiled bool
		wantID    string
	}{
		{"landed since the branch forked", forked.Add(48 * time.Minute), "landed", false, false, "mw-x.18"},
		{"superseded since the branch forked, by a bead that carries it", forked.Add(50 * time.Minute), "Superseded by mw-x.17", true, false, "mw-x.17"},
		{"closed before the branch forked", forked.Add(-time.Hour), "landed", false, true, ""},
		{"closed with no time known", time.Time{}, "landed", false, true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			tracker := apptest.NewFakeTracker()
			tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
			tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A landed story", Overrides: domain.Path{Files: "application/next.go"}})
			tracker.AddStory("mw-x", domain.Story{ID: "mw-x.18", Title: "Make the claude login cure change what it names"})
			mustDo(t, tracker.SetDescription("mw-x.18", fixed))
			if c.carrier {
				tracker.AddStory("mw-x", domain.Story{ID: "mw-x.17", Title: "Fold the claude login and battery cures"})
				mustDo(t, tracker.SetDescription("mw-x.17", fixed))
				mustDo(t, tracker.SetStatus("mw-x.17", apptest.StatusClosed))
				mustDo(t, tracker.SetClosedAt("mw-x.17", c.closedAt))
			}
			mustDo(t, tracker.CloseStory(ctx, "mw-x.18", c.reason))
			mustDo(t, tracker.SetClosedAt("mw-x.18", c.closedAt))
			mustDo(t, tracker.ClaimStory(ctx, "mw-x.1"))
			mustDo(t, tracker.SetStarted("mw-x.1", forked))
			mustDo(t, tracker.SetStoryState(ctx, "mw-x.1", application.RunState, application.RunLanded, "landed on main"))
			mustDo(t, tracker.CommentOnStory(ctx, "mw-x.1", "Done.\nFound, not fixed:\n"+finding+"\nFor the rig memory: nothing"))
			before := tracker.Stories()
			vault := newFakeVault()
			vault.written["mw-x.1/next-1.log"] = "close-out started 2026-11-22T04:57:00Z\n"
			lands := &fakeRetryLanding{}

			report, err := application.Next{
				Tracker: tracker, Vault: vault, Worktrees: lands, Landing: lands,
				Checks: checkStub{}, Slot: slotStub{}, Mailbox: apptest.NewFakeMailbox(),
				Seat: "builder", Host: "laptop",
				Rigs: map[string]string{"millwright": "/rigs/millwright"},
				Out:  io.Discard, Err: io.Discard,
			}.Run(ctx, "mw-x.1")
			mustDo(t, err)

			filed := theTestersFiled(t, tracker, before)
			if c.wantFiled != (len(filed) == 1) {
				t.Fatalf("expected filed=%v, got %d filed: %+v", c.wantFiled, len(filed), filed)
			}
			if c.wantFiled {
				return
			}
			want := "already filed as " + c.wantID
			if !slices.ContainsFunc(report.Notes, func(n string) bool { return strings.Contains(n, want) }) {
				t.Errorf("expected a note %q, got %v", want, report.Notes)
			}
			if log := vault.written["mw-x.1/next-1.log"]; !strings.Contains(log, c.wantID) || !strings.Contains(log, "closed since its branch forked") {
				t.Errorf("expected the close-out's log to say the join to %s, got %q", c.wantID, log)
			}
		})
	}
}
