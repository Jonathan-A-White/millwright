package application_test

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// aRelatedLine is a rig's related-tests line as a runner that picks tests by
// import graph runs it: the files the branch changed select nothing that fails,
// so the run fails only when it is given a test file in Also, as a source-scanning
// test the branch never touched would be.
type aRelatedLine struct {
	spans []application.RelatedSpan
	// scans is the test file that fails whenever the line is given it.
	scans string
}

func (a *aRelatedLine) RunRelated(_ context.Context, _, _ string, span application.RelatedSpan) (application.RelatedRun, error) {
	a.spans = append(a.spans, span)
	run := application.RelatedRun{Files: append([]string{"src/changed.ts"}, span.Also...), Ran: true}
	run.Command = "npx vitest related --run src/changed.ts"
	run.Passed = true
	for _, file := range span.Also {
		if file == a.scans {
			run.Passed = false
			run.Output = " FAIL  " + file + " > scans the source > finds nothing\n"
		}
	}
	return run, nil
}

// aPartRecord is what mw check --part keeps of a part: the commit it ran at,
// what it came to, and the tail of what it printed.
func aPartRecord(result, tail string) string { return "tip 0123abc\n" + result + "\n" + tail }

const aVitestFailure = "\n ⎯⎯⎯ Failed Tests 1 ⎯⎯⎯\n\n FAIL  tests/unit/one-scheme.test.ts > every sound comes from the scheme > finds no rule outside it\nAssertionError: expected [ Array(1) ] to deeply equal []\n"

// aCloseOutWithRelated closes out a finished story of a rig that names a related-tests line, the
// vault holding the run files given.
func aCloseOutWithRelated(t *testing.T, related *aRelatedLine, files map[string]string) (application.NextReport, error, *fakeVault) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	for name, text := range files {
		vault.written["mw-x.1/"+name] = text
	}
	lands := &aLandingLanding{fakeRetryLanding{AheadCount: 1}}
	report, err := application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands, Landing: lands,
		Checks: &scriptedChecks{passes: []bool{true}}, RelatedChecks: related, Slot: aSlot{}, Mailbox: apptest.NewFakeMailbox(),
		Seat: "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(context.Background(), "mw-x.1")
	return report, err, vault
}

func TestAStoryWhoseOwnCheckFailedATestItsFilesDoNotSelectIsGatedOnThatTestToo(t *testing.T) {
	related := &aRelatedLine{scans: "tests/unit/one-scheme.test.ts"}
	report, err, _ := aCloseOutWithRelated(t, related, map[string]string{
		"check-1-part-1.log": aPartRecord("passed", "ok\n"),
		"check-1-part-2.log": aPartRecord("failed", aVitestFailure),
	})

	if len(related.spans) == 0 || !reflect.DeepEqual(related.spans[0].Also, []string{"tests/unit/one-scheme.test.ts"}) {
		t.Fatalf("expected the related run given the test file the own check failed, got %+v", related.spans)
	}
	if err == nil || !report.Refused || report.Reason != application.ReasonTestsFail {
		t.Fatalf("expected the close-out refused for %s, got err %v and %+v", application.ReasonTestsFail, err, report)
	}
	if !strings.Contains(err.Error(), "tests/unit/one-scheme.test.ts") {
		t.Errorf("expected the refusal to name the test, got %q", err)
	}
}

func TestAWholeCheckLogThatNamesAFailedTestFileGivesItToTheRelatedRunToo(t *testing.T) {
	related := &aRelatedLine{scans: "tests/unit/one-scheme.test.ts"}
	report, _, _ := aCloseOutWithRelated(t, related, map[string]string{"check-1.log": "check on laptop: mw-x.1\n  tests failed\n" + aVitestFailure})

	if !report.Refused || report.Reason != application.ReasonTestsFail {
		t.Fatalf("expected the close-out refused for failing tests, got %+v", report)
	}
}

func TestAStoryWhoseOwnCheckPassedOrLeftNoLogIsGatedOnItsChangedFilesOnly(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"no check log":      nil,
		"a passing part":    {"check-1-part-1.log": aPartRecord("passed", "ok\n")},
		"a passed check":    {"check-1.log": "check on laptop: mw-x.1\n  all checks passed\n"},
		"another attempt's": {"check-2-part-1.log": aPartRecord("failed", aVitestFailure)},
	} {
		t.Run(name, func(t *testing.T) {
			related := &aRelatedLine{scans: "tests/unit/one-scheme.test.ts"}
			report, err, _ := aCloseOutWithRelated(t, related, files)

			if len(related.spans) == 0 || len(related.spans[0].Also) != 0 {
				t.Fatalf("expected the related run given no failed test file, got %+v", related.spans)
			}
			if err != nil || !report.Landed {
				t.Fatalf("expected the story landed, got err %v and %+v", err, report)
			}
		})
	}
}

func TestFailingTestFilesAreTheFilesAFailingRunNamesEachOnce(t *testing.T) {
	output := " FAIL  tests/unit/a.test.ts > one\n FAIL  tests/unit/a.test.ts > two\n" +
		" FAIL  src/b.spec.tsx:12:3 > three\n✘  1 [chromium] › tests/e2e/c.spec.ts:9:5 › four (2.1s)\n" +
		"--- FAIL: TestGo (0.00s)\nFAIL\tgithub.com/x/y\t0.4s\n × tests/unit/a.test.ts > five\n"
	want := []string{"tests/unit/a.test.ts", "src/b.spec.tsx", "tests/e2e/c.spec.ts"}
	if got := application.FailingTestFiles(output); !reflect.DeepEqual(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestABatchesRelatedGateIsGivenTheTestFilesEveryMembersOwnCheckFailed(t *testing.T) {
	related := &aRelatedLine{scans: "tests/unit/one-scheme.test.ts"}
	vault := newFakeVault()
	vault.written["mw-x.1/check-1.log"] = "check on laptop: mw-x.1\n  all checks passed\n"
	vault.written["mw-x.2/check-1-part-1.log"] = aPartRecord("passed", "ok\n")
	vault.written["mw-x.2/check-1-part-2.log"] = aPartRecord("failed", aVitestFailure)
	vault.written["mw-x.3/check-1-part-1.log"] = aPartRecord("failed", " FAIL  tests/unit/other.test.ts > a\n")
	land := application.BatchLand{Next: application.Next{
		Tracker: apptest.NewFakeTracker(), Vault: vault, Checks: &scriptedChecks{passes: []bool{true}}, RelatedChecks: related,
		Host: "laptop", Seat: "builder", Out: io.Discard, Err: io.Discard,
	}}
	report := &application.BatchReport{
		Rig: "millwright", Target: "main", Base: "origin/main",
		Merged: []application.BatchStory{{ID: "mw-x.1"}, {ID: "mw-x.2"}, {ID: "mw-x.3"}},
	}

	gate := land.GateTheBatch(context.Background(), t.TempDir(), report)

	if len(related.spans) != 1 {
		t.Fatalf("expected the related line run once, got %+v", related.spans)
	}
	want := []string{"tests/unit/one-scheme.test.ts", "tests/unit/other.test.ts"}
	if got := related.spans[0].Also; !reflect.DeepEqual(got, want) {
		t.Errorf("expected the batch's gate given the files %v, got %v", want, got)
	}
	if gate.Passed {
		t.Errorf("expected the batch's gate to fail on the source-scanning test, got %+v", gate)
	}
}

// The rule of mw-gq6.903, from the desktop's close-outs of 2026-10-26 to 11-03:
// a gate is run again only when its failure was read far over the cores (twice
// them), a gate is not run while the host stays far over after the wait, and a
// failure that ends with the host far over is the host's, handed back without an
// attempt.

func halfOver() application.LoadReading { return application.LoadReading{Load: 30, Cores: 20} }

func TestALoadOfTwiceTheCoresIsFarOverAndOneJustUnderItIsNot(t *testing.T) {
	for _, c := range []struct {
		reading application.LoadReading
		want    bool
	}{
		{application.LoadReading{Load: 40, Cores: 20}, true},
		{application.LoadReading{Load: 39.9, Cores: 20}, false},
		{application.LoadReading{Load: 20, Cores: 20}, false},
		{application.LoadReading{Load: 90, Cores: 0}, false},
	} {
		if got := c.reading.FarOver(); got != c.want {
			t.Errorf("load %.1f of %d cores: expected FarOver %v, got %v", c.reading.Load, c.reading.Cores, c.want, got)
		}
	}
}

func TestAGateThatFailsBetweenOnceAndTwiceTheCoresCountsAtOnceAndIsNotRunAgain(t *testing.T) {
	run, checks := aCloseOutWhoseTestsGo(t, []bool{false, true}, []application.LoadReading{calm(), halfOver(), calm()}, 15*time.Minute)

	if run.err == nil || run.report.Landed || run.report.Reason != application.ReasonTestsFail {
		t.Fatalf("expected a tests-fail refusal, got err %v and %+v", run.err, run.report)
	}
	if checks.runs != 1 {
		t.Errorf("expected one run, as the failure was not read far over the cores, got %d", checks.runs)
	}
	if strings.Contains(run.report.Why, "twice") {
		t.Errorf("expected no second run to be named, got %q", run.report.Why)
	}
}

func TestAGateWhoseWaitEndsFarOverTheCoresRunsNothingAndIsHandedBackWithNoAttempt(t *testing.T) {
	run, checks := aCloseOutWhoseTestsGo(t, []bool{true}, []application.LoadReading{farOver()}, 2*time.Minute)

	if checks.runs != 0 {
		t.Errorf("expected no run of the gate far over the cores, got %d", checks.runs)
	}
	assertHandedBackForTheHost(t, run)
	if !strings.Contains(run.err.Error(), "load 45.0 of 20 cores") {
		t.Errorf("expected the hand-back to name the load, got %q", run.err)
	}
}

func TestAGateWhoseRetryWaitEndsFarOverTheCoresIsNotRunAgainAndIsHandedBack(t *testing.T) {
	// calm before the first run, far over after it and for every read after.
	run, checks := aCloseOutWhoseTestsGo(t, []bool{false, true}, []application.LoadReading{calm(), farOver()}, 2*time.Minute)

	if checks.runs != 1 {
		t.Errorf("expected the one run made at a calm host, and none far over, got %d", checks.runs)
	}
	assertHandedBackForTheHost(t, run)
}

func TestASecondRunThatFailsWithTheHostFarOverAfterItIsTheHostsAndIsHandedBack(t *testing.T) {
	// calm, failed far over, calm for the second run, far over again after it.
	run, checks := aCloseOutWhoseTestsGo(t, []bool{false}, []application.LoadReading{calm(), farOver(), calm(), farOver()}, 15*time.Minute)

	if checks.runs != 2 {
		t.Errorf("expected the gate run twice, got %d", checks.runs)
	}
	assertHandedBackForTheHost(t, run)
}

func TestASecondRunThatFailsOnACalmHostIsTheStorysAndRefuses(t *testing.T) {
	run, checks := aCloseOutWhoseTestsGo(t, []bool{false}, []application.LoadReading{calm(), farOver(), calm()}, 15*time.Minute)

	if checks.runs != 2 || run.report.Reason != application.ReasonTestsFail || run.report.HandedBack {
		t.Fatalf("expected a tests-fail refusal after two runs, got %d runs and %+v", checks.runs, run.report)
	}
}

// assertHandedBackForTheHost says the close-out was handed back for the host's
// load: its close-out is to be run again alone, nothing is refused, mailed as a
// failure or counted as an attempt.
func assertHandedBackForTheHost(t *testing.T, run *gateRun) {
	t.Helper()
	if run.err == nil || run.report.Landed || run.report.Refused || !run.report.HandedBack || run.report.Reason != application.ReasonHostBusy {
		t.Fatalf("expected a hand-back for %s, got err %v and %+v", application.ReasonHostBusy, run.err, run.report)
	}
	state, err := run.tracker.StoryState(context.Background(), "mw-x.1", application.RunState)
	if err != nil || state != application.RunCloseOutAgain {
		t.Errorf("expected the run state %q, got %q (%v)", application.RunCloseOutAgain, state, err)
	}
	if d, _ := run.tracker.ShowStory(context.Background(), "mw-x.1"); d.Attempts > 1 {
		t.Errorf("expected no attempt spent, got %d", d.Attempts)
	}
	if len(run.vault.ledger) != 1 || !strings.Contains(run.vault.ledger[0], "host-busy") {
		t.Errorf("expected one ledger line naming host-busy, got %q", run.vault.ledger)
	}
}
