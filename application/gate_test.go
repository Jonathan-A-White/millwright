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
