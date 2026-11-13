package application_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1137: the Builder's harness (Claude Code on Bun) segfaulted on cloud
// boxes, at session start (mw-indx1x6.6, mw-82lb97i.11) and mid-session
// (mw-fnvjof.17.101, whose worktree held uncommitted work). Each left an empty
// result.json and a pane ending in Bun's crash banner, and mw next stopped the
// story as no-result, claim kept, for the Mayor's hand retry.

// bunCrashPane is the tail of a pane after the harness segfaulted, the way Bun
// prints its crash report.
const bunCrashPane = `● Bash(go test ./application/...)
  ⎿  ok  github.com/Jonathan-A-White/millwright/application  41.2s
============================================================
Bun v1.4.4 (5f3a1c0b) Linux x64 (baseline)
Linux Kernel v6.8.0 | glibc v2.39
CPU: sse42 popcnt avx avx2
Args: "claude" "--print"
Elapsed: 5210ms | User: 2130ms | Sys: 410ms
panic(main thread): Segmentation fault at address 0x0
oh no: Bun has crashed. This indicates a bug in Bun, not your code.
`

// crashedRun is one close-out of mw-gq6.9 at attempt attempts, its result file
// empty and its pane showing a crash, with the vault's runs holding the files
// kept names (a crash an earlier attempt of the story already had).
type crashedRun struct {
	report   application.NextReport
	err      error
	tracker  *apptest.FakeTracker
	lands    *fakeRetryLanding
	files    *apptest.FakeVaultFiles
	mail     *apptest.FakeMailbox
	dispatch application.Dispatch
	runner   *apptest.FakeRunner
	session  string
	vault    string
	now      time.Time
}

func aCrashedCloseOut(t *testing.T, attempts int, pane string, kept []string, lands *fakeRetryLanding) *crashedRun {
	t.Helper()
	return aCloseOutWithAResult(t, attempts, pane, kept, lands, true)
}

// aCloseOutWithAResult is aCrashedCloseOut, the result file written empty when
// writeResult is true and never written at all when it is false.
func aCloseOutWithAResult(t *testing.T, attempts int, pane string, kept []string, lands *fakeRetryLanding, writeResult bool) *crashedRun {
	t.Helper()
	return aCloseOutWithAResultFile(t, attempts, pane, kept, lands, func(path string) {
		if writeResult {
			if err := os.WriteFile(path, nil, 0o644); err != nil {
				t.Fatalf("writing the empty result: %v", err)
			}
		}
	})
}

// aCloseOutWithAResultFile is aCloseOutWithAResult, the result file made by
// makeResult, which is given its path (and may leave it unmade).
func aCloseOutWithAResultFile(t *testing.T, attempts int, pane string, kept []string, lands *fakeRetryLanding, makeResult func(path string)) *crashedRun {
	t.Helper()
	ctx := context.Background()
	dispatch, tracker, _, runner, vaultDir := aFactory(t)
	now := time.Date(2026, 11, 12, 19, 46, 34, 0, time.UTC)
	dispatch.Now = func() time.Time { return now }
	tracker.Clock = func() time.Time { return now }
	dispatch.Memory = tracker
	claimedStory(t, tracker, "mw-gq6.9", attempts)
	name := startOldSession(t, runner, "mw-gq6.9")
	runner.Write(name, pane)
	dir := filepath.Join(vaultDir, application.RunsDir, "mw-gq6.9")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making the run directory: %v", err)
	}
	makeResult(filepath.Join(dir, application.ResultFileNameForAttempt(attempts)))
	for _, file := range kept {
		if err := os.WriteFile(filepath.Join(dir, file), []byte("kept\n"), 0o644); err != nil {
			t.Fatalf("writing %s: %v", file, err)
		}
	}
	if lands == nil {
		lands = &fakeRetryLanding{}
	}
	run := &crashedRun{
		tracker: tracker, lands: lands, files: &apptest.FakeVaultFiles{}, mail: apptest.NewFakeMailbox(),
		dispatch: dispatch, runner: runner, session: name, vault: vaultDir, now: now,
	}
	run.report, run.err = application.Next{
		Tracker: tracker, Vault: dispatch.Boot.Vault, Worktrees: lands, Landing: lands,
		Files: run.files, Mailbox: run.mail, Memory: tracker, RetryFromBranch: true,
		Checks: checkStub{}, Slot: slotStub{}, Runner: runner,
		Seat: "builder", Host: "cloud4",
		Rigs: dispatch.Rigs,
		Now:  func() time.Time { return now },
		Out:  io.Discard, Err: io.Discard,
	}.Run(ctx, "mw-gq6.9")
	_ = ctx
	return run
}

func (r *crashedRun) comments() string {
	return strings.Join(r.tracker.Comments("mw-gq6.9"), "\n---\n")
}

func TestAHarnessCrashIsNamedToolingItsWorkBundledAndTheClaimGivenBackOnce(t *testing.T) {
	// A crash mid-session: the worktree held uncommitted work and two commits.
	lands := &fakeRetryLanding{UncommittedPaths: []string{"application/next.go"}, LeftoversSHA: "abc", AheadCount: 2, BundleSHA: "def"}
	run := aCrashedCloseOut(t, 1, bunCrashPane, nil, lands)

	if run.report.Reason != application.ReasonHarnessCrash || run.report.Landed {
		t.Fatalf("expected a %s stop that landed nothing, got %q: %s (%v)", application.ReasonHarnessCrash, run.report.Reason, run.report.Why, run.err)
	}
	if !slices.Contains(run.tracker.Asked(), "ReleaseClaim") {
		t.Errorf("expected the claim given back without the Mayor, asked %v", run.tracker.Asked())
	}
	meta := run.tracker.Metadata("mw-gq6.9")
	if meta[application.CauseField] != application.CauseTooling || meta[application.AttemptCausesField] != "1:tooling" {
		t.Errorf("expected attempt 1 named tooling, got %v", meta)
	}
	if len(lands.leftoverMessages) != 1 || !strings.Contains(lands.leftoverMessages[0], "attempt 1") {
		t.Errorf("expected the worktree's uncommitted work committed, got %v", lands.leftoverMessages)
	}
	if !lands.bundled {
		t.Errorf("expected the worktree's commits bundled")
	}
	bundled, crashKept := false, false
	for _, commit := range run.files.Commits() {
		bundled = bundled || slices.Contains(commit.Paths, application.BundleRecord("mw-gq6.9", 1))
		crashKept = crashKept || slices.Contains(commit.Paths, "runs/mw-gq6.9/"+application.HarnessCrashFileNameForAttempt(1))
	}
	if !bundled || !crashKept {
		t.Errorf("expected the bundle and the crash record committed in the vault, got %v", run.files.Commits())
	}
	if kept, err := os.ReadFile(filepath.Join(run.vault, application.RunsDir, "mw-gq6.9", application.HarnessCrashFileNameForAttempt(1))); err != nil || !strings.Contains(string(kept), "Bun v1.4.4") {
		t.Errorf("expected the crash kept in the run with its banner, got %q (%v)", kept, err)
	}
	for _, want := range []string{"Bun v1.4.4", "attempt-1.bundle"} {
		if !strings.Contains(run.comments(), want) {
			t.Errorf("expected the story's comment to say %q, got:\n%s", want, run.comments())
		}
	}
	if inbox, _ := run.mail.Inbox(context.Background(), application.MayorMailbox); len(inbox) != 0 {
		t.Errorf("expected no mail to the Mayor for a crash given back, got %v", inbox)
	}
	if note, _ := run.tracker.Note(context.Background(), application.ContinueKey("mw-gq6.9")); note != "1" {
		t.Errorf("expected the next attempt to continue the branch, got note %q", note)
	}
}

func TestACrashedStoryIsDispatchedAgainAsTheNextAttemptAndNotCountedAgainstIt(t *testing.T) {
	run := aCrashedCloseOut(t, 1, bunCrashPane, nil, nil)
	run.runner.Exit(run.session, 0)

	report, err := run.dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("expected the dispatch to run cleanly, got %v", err)
	}
	if len(report.Started) != 1 || report.Started[0].Attempt != 2 {
		t.Fatalf("expected the story started again as attempt 2, got %+v", report)
	}
	detail, err := run.tracker.ShowStory(context.Background(), "mw-gq6.9")
	if err != nil {
		t.Fatalf("showing the story: %v", err)
	}
	if own, uncounted := application.OwnAttempts(detail); own != 1 || len(uncounted) != 1 || uncounted[0].Cause != application.CauseTooling {
		t.Errorf("expected one own attempt and the crashed one uncounted as tooling, got %d and %v", own, uncounted)
	}
}

func TestASecondCrashOnTheSameStoryStopsAsNoResultWithTheClaimKept(t *testing.T) {
	run := aCrashedCloseOut(t, 2, bunCrashPane, []string{application.HarnessCrashFileNameForAttempt(1)}, nil)

	if run.report.Reason != application.ReasonNoResult {
		t.Fatalf("expected %s on the second crash, got %q: %s", application.ReasonNoResult, run.report.Reason, run.report.Why)
	}
	if slices.Contains(run.tracker.Asked(), "ReleaseClaim") {
		t.Errorf("expected the claim kept, asked %v", run.tracker.Asked())
	}
	if !strings.Contains(run.comments(), "Bun v1.4.4") {
		t.Errorf("expected the stop to carry the pane with the banner, got:\n%s", run.comments())
	}
}

func TestAnEmptyResultWithNoCrashBannerInThePaneStaysNoResult(t *testing.T) {
	run := aCrashedCloseOut(t, 1, "working\nthe notes say Bun v1.4.4 is the runtime, no more\n", nil, nil)

	if run.report.Reason != application.ReasonNoResult {
		t.Fatalf("expected %s, got %q: %s", application.ReasonNoResult, run.report.Reason, run.report.Why)
	}
	if slices.Contains(run.tracker.Asked(), "ReleaseClaim") {
		t.Errorf("expected the claim kept, asked %v", run.tracker.Asked())
	}
}

// mw-gq6.1193: a result file that was never written is the same stop as an
// empty one, and its pane is the same evidence.
func TestAMissingResultStopCarriesThePaneTail(t *testing.T) {
	run := aCloseOutWithAResult(t, 1, "working\nthe last thing the session printed\n", nil, nil, false)

	if run.report.Reason != application.ReasonNoResult {
		t.Fatalf("expected %s, got %q: %s (%v)", application.ReasonNoResult, run.report.Reason, run.report.Why, run.err)
	}
	if !strings.Contains(run.report.Why, "left no result") {
		t.Fatalf("expected the stop for a missing result file, got: %s", run.report.Why)
	}
	if !strings.Contains(run.comments(), "the last thing the session printed") {
		t.Errorf("expected the stop's comment to carry the pane's tail, got:\n%s", run.comments())
	}
}

// mw-gq6.1225: the two stops for a result that is there but cannot be read
// (the file does not open; it opens and is not a result) carry the pane's tail
// too.
func TestAReadErrorStopCarriesThePaneTail(t *testing.T) {
	cases := map[string]func(t *testing.T) func(path string){
		"the file cannot be read": func(t *testing.T) func(string) {
			return func(path string) {
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatalf("making a directory where the result is: %v", err)
				}
			}
		},
		"the file is not a result": func(t *testing.T) func(string) {
			return func(path string) {
				if err := os.WriteFile(path, []byte("this is not json\n"), 0o644); err != nil {
					t.Fatalf("writing the result: %v", err)
				}
			}
		},
	}
	for name, makeResult := range cases {
		t.Run(name, func(t *testing.T) {
			run := aCloseOutWithAResultFile(t, 1, "working\nthe last thing the session printed\n", nil, nil, makeResult(t))

			if run.report.Reason != application.ReasonNoResult {
				t.Fatalf("expected %s, got %q: %s (%v)", application.ReasonNoResult, run.report.Reason, run.report.Why, run.err)
			}
			if !strings.Contains(run.report.Why, "could not be read") {
				t.Fatalf("expected the stop for a result that could not be read, got: %s", run.report.Why)
			}
			if !strings.Contains(run.comments(), "the last thing the session printed") {
				t.Errorf("expected the stop's comment to carry the pane's tail, got:\n%s", run.comments())
			}
		})
	}
}

func TestACrashWhoseWorkCannotBeBundledKeepsTheClaimAsEvidence(t *testing.T) {
	lands := &fakeRetryLanding{AheadCount: 1, BundleErr: io.ErrClosedPipe}
	run := aCrashedCloseOut(t, 1, bunCrashPane, nil, lands)

	if run.report.Reason != application.ReasonNoResult {
		t.Fatalf("expected %s when the work could not be kept, got %q: %s", application.ReasonNoResult, run.report.Reason, run.report.Why)
	}
	if slices.Contains(run.tracker.Asked(), "ReleaseClaim") {
		t.Errorf("expected the claim kept, asked %v", run.tracker.Asked())
	}
	if !strings.Contains(run.comments(), "bundl") {
		t.Errorf("expected the stop to say the bundle failed, got:\n%s", run.comments())
	}
}
