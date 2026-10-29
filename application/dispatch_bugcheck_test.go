package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

const (
	teachSpec    = "tests/e2e/teach.spec.ts"
	settingsSpec = "tests/e2e/settings.spec.ts"
)

// A bug names its check in one of four ways. Each one that passes on main closes
// the bug with no session; each one that fails dispatches it, saying what it ran.
func TestADispatchRechecksABugOnMainWhicheverWayItNamesItsCheck(t *testing.T) {
	for name, bug := range map[string]struct {
		title, description, files string
		labels                    []string
		wantSpec                  string
	}{
		"a suite spec": {
			title: "[bug] lampas main: " + teachSpec + " fails on main", files: teachSpec,
			labels: []string{application.LabelBug, application.LabelSuite}, wantSpec: teachSpec,
		},
		"a Check line": {
			title:       "[bug] lampas: the Words button is found twice",
			description: "It fails since mw-5r3p30.624.\n\nCheck: npx playwright test " + teachSpec + " --retries=0\n",
			files:       "src/Words.tsx", labels: []string{application.LabelBug}, wantSpec: teachSpec,
		},
		"a spec path in the title": {
			title: "[bug] lampas main: " + settingsSpec + " 'Quiz goal at phone width' fails in the nightly suite",
			files: settingsSpec + ",src/settings/controls/quizGoal.tsx", labels: []string{application.LabelBug}, wantSpec: settingsSpec,
		},
		"a title that starts [bug] with no label": {
			title: "[bug] lampas main: " + settingsSpec + " fails", files: "src/a.ts", wantSpec: settingsSpec,
		},
	} {
		t.Run(name+" passes", func(t *testing.T) {
			dispatch, tracker, worktrees, runner, _ := aFactory(t)
			tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: bug.title, Overrides: domain.Path{Files: bug.files}})
			tracker.SetDescription("mw-gq6.1", bug.description)
			if err := tracker.SetLabels("mw-gq6.1", bug.labels...); err != nil {
				t.Fatal(err)
			}
			recheck := &stubRechecks{answer: application.SpecRecheck{Spec: bug.wantSpec, Sha: "9f3c2ab41d0e", Passed: true, Tests: 2, Tail: "  2 passed (3s)\n"}}
			dispatch.Rechecks = recheck

			report, err := dispatch.Run(context.Background())
			if err != nil {
				t.Fatalf("dispatching: %v", err)
			}
			if len(recheck.asked) != 1 || recheck.asked[0] != "millwright "+bug.wantSpec {
				t.Fatalf("the re-run was asked for %v, want %s", recheck.asked, bug.wantSpec)
			}
			want := "already fixed on main at 9f3c2ab41: " + bug.wantSpec + " passes (2 tests)"
			if got := tracker.CloseReason("mw-gq6.1"); got != want {
				t.Errorf("close reason:\n got %q\nwant %q", got, want)
			}
			if said := strings.Join(tracker.Comments("mw-gq6.1"), "\n"); !strings.Contains(said, "2 passed (3s)") {
				t.Errorf("the comment should carry the run's output, got %q", said)
			}
			if len(report.Started) != 0 || len(worktrees.added) != 0 || len(runner.Started()) != 0 {
				t.Errorf("nothing should be dispatched: %+v", report.Started)
			}
		})
		t.Run(name+" fails", func(t *testing.T) {
			dispatch, tracker, _, _, _ := aFactory(t)
			tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: bug.title, Overrides: domain.Path{Files: bug.files}})
			tracker.SetDescription("mw-gq6.1", bug.description)
			if err := tracker.SetLabels("mw-gq6.1", bug.labels...); err != nil {
				t.Fatal(err)
			}
			dispatch.Rechecks = &stubRechecks{answer: application.SpecRecheck{Spec: bug.wantSpec, Sha: "9f3c2ab41d0e", Passed: false}}

			report, err := dispatch.Run(context.Background())
			if err != nil {
				t.Fatalf("dispatching: %v", err)
			}
			if ids := startedIDs(report); len(ids) != 1 {
				t.Fatalf("started %v, want the bug dispatched", ids)
			}
			if tracker.CloseReason("mw-gq6.1") != "" {
				t.Errorf("a failing check must not close the bug")
			}
			if got := report.Started[0].Recheck; !strings.Contains(got, bug.wantSpec) || !strings.Contains(got, "still fails at 9f3c2ab41") {
				t.Errorf("the line should name the check and its result, got %q", got)
			}
		})
	}
}

func TestADispatchStartsABugThatNamesNoCheckAndSaysSo(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "[bug] lampas: the card covers the text", Overrides: domain.Path{Files: "src/Card.tsx"}})
	tracker.SetDescription("mw-gq6.1", "Check: look at it on the phone\nIt is wrong.")
	if err := tracker.SetLabels("mw-gq6.1", application.LabelBug); err != nil {
		t.Fatal(err)
	}
	recheck := &stubRechecks{answer: application.SpecRecheck{Passed: true}}
	dispatch.Rechecks = recheck

	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if ids := startedIDs(report); len(ids) != 1 {
		t.Fatalf("started %v, want the bug dispatched", ids)
	}
	if len(recheck.asked) != 0 {
		t.Errorf("nothing should be re-run, got %v", recheck.asked)
	}
	if !strings.Contains(report.Started[0].Recheck, "no check named") || !strings.Contains(report.String(), "no check named") {
		t.Errorf("the line should say no check was named, got %q", report.Started[0].Recheck)
	}
}

// A story that is not a bug is never re-run and its line says nothing of it.
func TestADispatchNeverRechecksAStoryThatIsNotABugEvenWhenItNamesASpec(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "[taste] lampas: " + settingsSpec + " card stays", Overrides: domain.Path{Files: "src/a.ts"}})
	tracker.SetDescription("mw-gq6.1", "Check: "+settingsSpec)
	recheck := &stubRechecks{answer: application.SpecRecheck{Passed: true}}
	dispatch.Rechecks = recheck

	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(recheck.asked) != 0 || tracker.CloseReason("mw-gq6.1") != "" {
		t.Errorf("a story that is not a bug was re-run (%v) or closed", recheck.asked)
	}
	if ids := startedIDs(report); len(ids) != 1 || report.Started[0].Recheck != "" {
		t.Errorf("started %v with the line %q, want the story dispatched as before", ids, report.Started[0].Recheck)
	}
}

// The check is found in this order: the suite spec field, a Check: line, a
// grist eval line, a spec path in the title.
func TestADispatchTakesTheFirstCheckInTheOrderTheBugNamesThem(t *testing.T) {
	for name, bug := range map[string]struct {
		title, description, files string
		labels                    []string
		want                      string
	}{
		"the suite spec before a Check line": {
			title: "[bug] x " + settingsSpec, description: "Check: " + teachSpec, files: "tests/e2e/a.spec.ts",
			labels: []string{application.LabelBug, application.LabelSuite}, want: "millwright tests/e2e/a.spec.ts",
		},
		"a Check line before the title": {
			title: "[bug] x " + settingsSpec, description: "Check: " + teachSpec, files: "src/a.ts",
			labels: []string{application.LabelBug}, want: "millwright " + teachSpec,
		},
		"the description's Check line before the acceptance's": {
			title: "[bug] x", description: "Check: " + teachSpec, files: "src/a.ts",
			labels: []string{application.LabelBug}, want: "millwright " + teachSpec,
		},
	} {
		t.Run(name, func(t *testing.T) {
			dispatch, tracker, _, _, _ := aFactory(t)
			tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: bug.title, Overrides: domain.Path{Files: bug.files}})
			tracker.SetDescription("mw-gq6.1", bug.description)
			tracker.SetAcceptance("mw-gq6.1", "Check: "+settingsSpec)
			if err := tracker.SetLabels("mw-gq6.1", bug.labels...); err != nil {
				t.Fatal(err)
			}
			recheck := &stubRechecks{answer: application.SpecRecheck{Passed: false}}
			dispatch.Rechecks = recheck
			if _, err := dispatch.Run(context.Background()); err != nil {
				t.Fatalf("dispatching: %v", err)
			}
			if len(recheck.asked) != 1 || recheck.asked[0] != bug.want {
				t.Errorf("asked %v, want %q", recheck.asked, bug.want)
			}
		})
	}
}

// A test named in a title is often where the symptom shows, not the bug's check
// (mw-gq6.841): when the acceptance's numbered criterion runs a command that is
// not that spec, the bug is dispatched; one that runs only that spec, or names no
// command, is still rechecked from the title.
func TestADispatchLeavesTheTitleSpecAloneWhenTheAcceptanceNamesAnotherCheck(t *testing.T) {
	const titleSpec = "tests/unit/x.test.ts"
	title := "[bug] lampas: a grind is over " + titleSpec + "'s ceiling"
	for name, bug := range map[string]struct {
		acceptance string
		rechecked  bool
	}{
		"a backticked command that is not the spec": {
			acceptance: "1. `npx vitest related --run grinds/a.md` runs " + titleSpec + "\n2. go test ./... passes",
		},
		"a numbered line that begins with a runner": {
			acceptance: "1. npx vitest related --run grinds/a.md prints a test file\n",
		},
		"a backticked command that runs the spec": {
			acceptance: "1. `npx vitest --run " + titleSpec + "` passes", rechecked: true,
		},
		"no command at all": {
			acceptance: "1. The grind stays under the ceiling.\n2. The spec " + titleSpec + " passes.", rechecked: true,
		},
		"a command in an unnumbered line": {
			acceptance: "Run `npx vitest related --run grinds/a.md`.", rechecked: true,
		},
		"no acceptance": {rechecked: true},
	} {
		t.Run(name, func(t *testing.T) {
			dispatch, tracker, _, _, _ := aFactory(t)
			tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: title, Overrides: domain.Path{Files: "src/a.ts"}})
			tracker.SetAcceptance("mw-gq6.1", bug.acceptance)
			if err := tracker.SetLabels("mw-gq6.1", application.LabelBug); err != nil {
				t.Fatal(err)
			}
			recheck := &stubRechecks{answer: application.SpecRecheck{Spec: titleSpec, Sha: "9f3c2ab41d0e", Passed: true, Tests: 2}}
			dispatch.Rechecks = recheck

			report, err := dispatch.Run(context.Background())
			if err != nil {
				t.Fatalf("dispatching: %v", err)
			}
			if bug.rechecked {
				if len(recheck.asked) != 1 || tracker.CloseReason("mw-gq6.1") == "" {
					t.Errorf("asked %v, close %q: want the title spec rechecked and the bug closed", recheck.asked, tracker.CloseReason("mw-gq6.1"))
				}
				return
			}
			if len(recheck.asked) != 0 || tracker.CloseReason("mw-gq6.1") != "" {
				t.Errorf("asked %v, close %q: want no recheck and the bug open", recheck.asked, tracker.CloseReason("mw-gq6.1"))
			}
			if ids := startedIDs(report); len(ids) != 1 || !strings.Contains(report.Started[0].Recheck, "no check named") {
				t.Errorf("started %v, want the bug dispatched saying no check was named", ids)
			}
		})
	}
}

// stubExamples is the ExampleRechecker a dispatch test gives: it answers what it
// was told to and remembers what it was asked.
type stubExamples struct {
	answer application.SpecRecheck
	err    error
	asked  []string
}

func (s *stubExamples) RecheckExamples(_ context.Context, app, kind string, examples []string) (application.SpecRecheck, error) {
	s.asked = append(s.asked, app+" "+kind+" "+strings.Join(examples, ","))
	return s.answer, s.err
}

func aGristBug(t *testing.T, dispatch *application.Dispatch, tracker interface {
	AddStory(string, domain.Story)
	SetDescription(string, string) error
	SetLabels(string, ...string) error
}, where, line string) {
	t.Helper()
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "[bug] lampas grist smoke: audible-position/romans-8 had no answer", Overrides: domain.Path{Files: "grinds/audible-position.json"}})
	tracker.SetDescription("mw-gq6.1", where+line)
	if err := tracker.SetLabels("mw-gq6.1", application.LabelBug); err != nil {
		t.Fatal(err)
	}
}

func TestADispatchSendsTheGristExampleABugNamesToMainNotTheSpecRunner(t *testing.T) {
	for name, line := range map[string]string{
		"a Check line":      "Check: " + "mw grist eval lampas audible-position --example romans-8\n",
		"a grist eval line": "The failing example: `mw grist eval lampas audible-position --example romans-8`.\n",
	} {
		t.Run(name+" passes", func(t *testing.T) {
			dispatch, tracker, worktrees, runner, _ := aFactory(t)
			aGristBug(t, &dispatch, tracker, "Seen in the smoke.\n", line)
			specs := &stubRechecks{answer: application.SpecRecheck{Passed: true}}
			examples := &stubExamples{answer: application.SpecRecheck{Sha: "c0ffee1234567", Passed: true, Tail: "grist eval: lampas/audible-position: 1 examples passed"}}
			dispatch.Rechecks, dispatch.ExampleRechecks = specs, examples

			report, err := dispatch.Run(context.Background())
			if err != nil {
				t.Fatalf("dispatching: %v", err)
			}
			if len(specs.asked) != 0 {
				t.Errorf("the spec runner was asked for %v, want none", specs.asked)
			}
			if len(examples.asked) != 1 || examples.asked[0] != "lampas audible-position romans-8" {
				t.Fatalf("the examples were asked for %v", examples.asked)
			}
			want := "already fixed on main at c0ffee123: mw grist eval lampas audible-position --example romans-8 passes"
			if got := tracker.CloseReason("mw-gq6.1"); got != want {
				t.Errorf("close reason:\n got %q\nwant %q", got, want)
			}
			if len(report.Started) != 0 || len(worktrees.added) != 0 || len(runner.Started()) != 0 {
				t.Errorf("nothing should be dispatched: %+v", report.Started)
			}
		})
		t.Run(name+" fails", func(t *testing.T) {
			dispatch, tracker, _, _, _ := aFactory(t)
			aGristBug(t, &dispatch, tracker, "", line)
			dispatch.ExampleRechecks = &stubExamples{answer: application.SpecRecheck{Sha: "c0ffee1234567", Passed: false}}

			report, err := dispatch.Run(context.Background())
			if err != nil {
				t.Fatalf("dispatching: %v", err)
			}
			if ids := startedIDs(report); len(ids) != 1 {
				t.Fatalf("started %v, want the bug dispatched", ids)
			}
			if got := report.Started[0].Recheck; !strings.Contains(got, "mw grist eval lampas audible-position --example romans-8") || !strings.Contains(got, "still fails at c0ffee123") {
				t.Errorf("the line should name the check and its result, got %q", got)
			}
		})
	}
}

func TestADispatchDispatchesAGristBugFromAHostWithoutTheKeyAndSaysSo(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	aGristBug(t, &dispatch, tracker, "", "Check: mw grist eval lampas audible-position --example romans-8\n")
	dispatch.ExampleRechecks = &stubExamples{err: application.ErrNoGristKey}

	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if ids := startedIDs(report); len(ids) != 1 {
		t.Fatalf("started %v, want the bug dispatched", ids)
	}
	if got := report.Started[0].Recheck; got != "no key on this host: dispatching" {
		t.Errorf("the line should say there is no key, got %q", got)
	}
	if tracker.CloseReason("mw-gq6.1") != "" {
		t.Errorf("a check that was not run must not close the bug")
	}
}

func TestADispatchReadsAGristEvalLineForItsAppKindAndEveryExample(t *testing.T) {
	for line, want := range map[string]string{
		"mw grist eval lampas tutor --example greets":                         "lampas tutor greets",
		"`mw grist eval lampas tutor --example a --example b --wait 2m`":      "lampas tutor a,b",
		"mw grist eval lampas tutor --example=greets.":                        "lampas tutor greets",
		"mw grist eval lampas tutor --example greets --ref mw/story":          "",
		"mw grist eval --grind rigs/lampas/grinds/tutor.json --photos /tmp/p": "",
		"mw grist eval lampas tutor":                                          "",
	} {
		dispatch, tracker, _, _, _ := aFactory(t)
		aGristBug(t, &dispatch, tracker, "", line+"\n")
		examples := &stubExamples{answer: application.SpecRecheck{Passed: false}}
		dispatch.ExampleRechecks = examples
		if _, err := dispatch.Run(context.Background()); err != nil {
			t.Fatalf("dispatching: %v", err)
		}
		have := strings.Join(examples.asked, ";")
		if have != want {
			t.Errorf("%q: asked %q, want %q", line, have, want)
		}
	}
}

func TestAGristExampleRecheckSendsToMainOnAHostWithTheKeyAndNothingWithout(t *testing.T) {
	// main answers "main"; an example that expects it passes on main, one that
	// expects what only a branch says does not.
	mainOK := `{"schemaVersion": "1", "request": {"question": "hi"}, "expect": {"answer": {"matches": "main"}}}`
	sender := &textSender{}
	smoke, grinds := evalRig(t, sender)
	grinds.SetFile("/rigs/lampas", "c1", "grinds/examples/tutor/main-ok.json", []byte(mainOK))

	none := application.GristExampleRecheck{Smoke: smoke, HasKey: func() bool { return false }}
	if _, err := none.RecheckExamples(context.Background(), "lampas", "tutor", []string{"main-ok"}); err != application.ErrNoGristKey {
		t.Fatalf("a host with no key should get ErrNoGristKey, got %v", err)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("nothing should be sent without the key, got %d", len(sender.sent))
	}

	held := application.GristExampleRecheck{Smoke: smoke, HasKey: func() bool { return true }}
	got, err := held.RecheckExamples(context.Background(), "lampas", "tutor", []string{"main-ok"})
	if err != nil || !got.Passed || got.Sha != "c1" {
		t.Fatalf("an example that meets its expect on main: got %+v, %v", got, err)
	}
	if len(sender.sent) != 1 || sender.sent[0].GrindOverride != nil {
		t.Errorf("the example should be sent once, against main (no override), got %+v", sender.sent)
	}
	got, err = held.RecheckExamples(context.Background(), "lampas", "tutor", []string{"greets"})
	if err != nil || got.Passed {
		t.Errorf("an example that misses its expect on main is an answer, not a pass: got %+v, %v", got, err)
	}
	if _, err = held.RecheckExamples(context.Background(), "lampas", "tutor", []string{"no-such"}); err == nil {
		t.Errorf("an example that does not exist is a check that could not be run")
	}
}

// One pass proves nothing about a flake: a bug that says it flakes, or whose
// acceptance asks for more than one run, is dispatched and not closed by the one
// eval that passes (mw-gq6.784).
func TestADispatchDoesNotCloseAFlakeBugOnOnePassingEval(t *testing.T) {
	line := "Check: mw grist eval lampas verse-ask --example bridge-quite-a-bit\n"
	for name, bug := range map[string]struct{ title, description, acceptance string }{
		"flakes in the title":        {"[bug] lampas: verse-ask bridge-quite-a-bit flakes in a smoke", line, ""},
		"flaky in the description":   {"[bug] lampas: verse-ask bridge-quite-a-bit", "The example is flaky.\n" + line, ""},
		"N of M in the acceptance":   {"[bug] lampas: verse-ask bridge-quite-a-bit failed once", line, "1. The example passes 3 of 3 runs."},
		"in a row in the acceptance": {"[bug] lampas: verse-ask bridge-quite-a-bit failed once", line, "1. Passes five in a row."},
	} {
		t.Run(name, func(t *testing.T) {
			dispatch, tracker, _, _, _ := aFactory(t)
			tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: bug.title, Overrides: domain.Path{Files: "grinds/verse-ask.json"}})
			tracker.SetDescription("mw-gq6.1", bug.description)
			if bug.acceptance != "" {
				tracker.SetAcceptance("mw-gq6.1", bug.acceptance)
			}
			if err := tracker.SetLabels("mw-gq6.1", application.LabelBug); err != nil {
				t.Fatal(err)
			}
			examples := &stubExamples{answer: application.SpecRecheck{Sha: "c0ffee1234567", Passed: true}}
			dispatch.Rechecks, dispatch.ExampleRechecks = &stubRechecks{answer: application.SpecRecheck{Passed: true}}, examples

			report, err := dispatch.Run(context.Background())
			if err != nil {
				t.Fatalf("dispatching: %v", err)
			}
			if len(examples.asked) != 0 || tracker.CloseReason("mw-gq6.1") != "" {
				t.Errorf("a flake bug was re-run (%v) or closed (%q)", examples.asked, tracker.CloseReason("mw-gq6.1"))
			}
			if ids := startedIDs(report); len(ids) != 1 {
				t.Errorf("started %v, want the bug dispatched", ids)
			}
		})
	}
}

// A bug filed from a grist smoke flake carries smoke_examples, and a 1-in-3 miss
// passes one send two times in three: so it is a flake bug and is dispatched, while
// the same bug without smoke_examples is still rechecked (mw-gq6.812).
func TestADispatchDoesNotRecheckABugThatCarriesSmokeExamples(t *testing.T) {
	line := "Check: mw grist eval lampas sentence-diagram --example col-3-1-greek\n"
	for name, smoke := range map[string]bool{"with smoke_examples": true, "without smoke_examples": false} {
		t.Run(name, func(t *testing.T) {
			dispatch, tracker, _, _, _ := aFactory(t)
			tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "[bug] Lampas sentence-diagram grind answers outside its schema on col-3-1-greek", Overrides: domain.Path{Files: "grinds/sentence-diagram.json"}})
			tracker.SetDescription("mw-gq6.1", line)
			if err := tracker.SetLabels("mw-gq6.1", application.LabelBug); err != nil {
				t.Fatal(err)
			}
			if smoke {
				if err := tracker.SetStoryMetadata(context.Background(), "mw-gq6.1", map[string]string{application.SmokeExamplesField: "lampas/sentence-diagram/col-3-1-greek"}); err != nil {
					t.Fatal(err)
				}
			}
			examples := &stubExamples{answer: application.SpecRecheck{Sha: "c0ffee1234567", Passed: true}}
			dispatch.Rechecks, dispatch.ExampleRechecks = &stubRechecks{answer: application.SpecRecheck{Passed: true}}, examples

			report, err := dispatch.Run(context.Background())
			if err != nil {
				t.Fatalf("dispatching: %v", err)
			}
			if smoke {
				if len(examples.asked) != 0 || tracker.CloseReason("mw-gq6.1") != "" {
					t.Errorf("a smoke flake bug was re-run (%v) or closed (%q)", examples.asked, tracker.CloseReason("mw-gq6.1"))
				}
				if ids := startedIDs(report); len(ids) != 1 {
					t.Errorf("started %v, want the bug dispatched", ids)
				}
				return
			}
			if len(examples.asked) != 1 || tracker.CloseReason("mw-gq6.1") == "" {
				t.Errorf("a plain bug should still be re-run and closed: asked %v, closed %q", examples.asked, tracker.CloseReason("mw-gq6.1"))
			}
		})
	}
}

// "about 1 in 3" and "6 runs in 6" count runs like "3 of 3" does; a number in a
// reference that is not a count of runs ("Psalm 23 in 3 John", "verse 3 in John")
// does not (mw-gq6.812).
func TestADispatchTakesNInNAsARunCount(t *testing.T) {
	line := "Check: mw grist eval lampas sentence-diagram --example col-3-1-greek\n"
	for name, bug := range map[string]struct {
		title, acceptance string
		flake             bool
	}{
		"about 1 in 3 in the title":     {"[bug] lampas: sentence-diagram answers outside its schema about 1 in 3", "", true},
		"6 runs in 6 in the acceptance": {"[bug] lampas: sentence-diagram answers outside its schema", "1. It passes 6 runs in 6.", true},
		"6 in 6 in the acceptance":      {"[bug] lampas: sentence-diagram answers outside its schema", "1. It passes 6 in 6.", true},
		"a verse reference":             {"[bug] lampas: Psalm 23 in 3 John answers outside its schema", "", false},
		"a chapter in a book":           {"[bug] lampas: verse 3 in John answers outside its schema", "1. It reads chapter 12 in John 3.", false},
	} {
		t.Run(name, func(t *testing.T) {
			dispatch, tracker, _, _, _ := aFactory(t)
			tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: bug.title, Overrides: domain.Path{Files: "grinds/sentence-diagram.json"}})
			tracker.SetDescription("mw-gq6.1", line)
			if bug.acceptance != "" {
				tracker.SetAcceptance("mw-gq6.1", bug.acceptance)
			}
			if err := tracker.SetLabels("mw-gq6.1", application.LabelBug); err != nil {
				t.Fatal(err)
			}
			examples := &stubExamples{answer: application.SpecRecheck{Sha: "c0ffee1234567", Passed: true}}
			dispatch.Rechecks, dispatch.ExampleRechecks = &stubRechecks{answer: application.SpecRecheck{Passed: true}}, examples

			if _, err := dispatch.Run(context.Background()); err != nil {
				t.Fatalf("dispatching: %v", err)
			}
			if asked := len(examples.asked) != 0; asked == bug.flake {
				t.Errorf("flake bug %v but the example was re-run: %v", bug.flake, examples.asked)
			}
		})
	}
}
