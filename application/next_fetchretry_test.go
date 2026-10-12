package application_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// A flakyLanding is a landing whose git fails the first fetchFails fetches and
// the first pushFails pushes with err, and works after that.
type flakyLanding struct {
	aLandingLanding
	fetchFails, pushFails int
	err                   error

	fetches, pushes int
}

func (f *flakyLanding) Fetch(context.Context, string) error {
	f.fetches++
	if f.fetches <= f.fetchFails {
		return f.err
	}
	return nil
}

func (f *flakyLanding) Push(context.Context, string, string, string) error {
	f.pushes++
	if f.pushes <= f.pushFails {
		return f.err
	}
	return nil
}

// A passingDNSFault is the failure mw-neuq78.2's landing met.
func passingDNSFault() error {
	return &application.TransientFault{
		Why: "could not resolve host",
		Err: errors.New("git fetch origin in /rigs/millwright: exit status 128: fatal: unable to access 'https://github.com/x/y.git/': Could not resolve host: github.com"),
	}
}

type fetchRetryRun struct {
	report application.NextReport
	err    error
	waits  []time.Duration
	lands  *flakyLanding
}

func aCloseOutOverFlakyGit(t *testing.T, lands *flakyLanding, crossHeld bool) *fetchRetryRun {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	lands.AheadCount = 1
	run := &fetchRetryRun{lands: lands}
	run.report, run.err = application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands, Landing: lands,
		Checks: &scriptedChecks{passes: []bool{true}}, Slot: aSlot{},
		PushWait: 20 * time.Second,
		RetryWait: func(_ context.Context, d time.Duration) error {
			run.waits = append(run.waits, d)
			return nil
		},
		Seat: "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(context.Background(), "mw-x.1")
	return run
}

func notesNaming(report application.NextReport, words string) []string {
	var found []string
	for _, note := range report.Notes {
		if strings.Contains(note, words) {
			found = append(found, note)
		}
	}
	return found
}

func TestAFetchThatMeetsAPassingDNSFaultTwiceIsRetriedAndTheStoryLands(t *testing.T) {
	run := aCloseOutOverFlakyGit(t, &flakyLanding{fetchFails: 2, err: passingDNSFault()}, false)

	if run.err != nil || !run.report.Landed {
		t.Fatalf("expected the story landed on the third fetch, got err %v and %+v", run.err, run.report)
	}
	if run.lands.fetches != 3 {
		t.Fatalf("expected 3 fetches, got %d", run.lands.fetches)
	}
	if want := []time.Duration{10 * time.Second, 20 * time.Second}; fmt.Sprint(run.waits) != fmt.Sprint(want) {
		t.Fatalf("expected the waits to grow, %v, got %v", want, run.waits)
	}
	notes := notesNaming(run.report, "fetch failed")
	want := []string{
		"fetch failed (could not resolve host), retried in 10s",
		"fetch failed (could not resolve host), retried in 20s",
	}
	if fmt.Sprint(notes) != fmt.Sprint(want) {
		t.Fatalf("expected one note per retry, %q, got %q", want, notes)
	}
}

func TestAFetchThatKeepsMeetingTheFaultStopsAsGitFailedAfterThreeRetriesOverAboutAMinute(t *testing.T) {
	run := aCloseOutOverFlakyGit(t, &flakyLanding{fetchFails: 100, err: passingDNSFault()}, false)

	if run.err == nil || run.report.Landed || run.report.Reason != application.ReasonGitFailed {
		t.Fatalf("expected a git-failed stop, got err %v and %+v", run.err, run.report)
	}
	if run.lands.fetches != 4 {
		t.Fatalf("expected the fetch tried 4 times (3 retries), got %d", run.lands.fetches)
	}
	var total time.Duration
	for _, d := range run.waits {
		total += d
	}
	if len(run.waits) != 3 || total != 60*time.Second {
		t.Fatalf("expected 3 waits of 60s in all, got %v", run.waits)
	}
}

func TestAFetchThatFailsForAnotherReasonIsNotRetried(t *testing.T) {
	run := aCloseOutOverFlakyGit(t, &flakyLanding{fetchFails: 100, err: errors.New("git fetch origin: exit status 128: fatal: couldn't find remote ref")}, false)

	if run.err == nil || run.report.Reason != application.ReasonGitFailed {
		t.Fatalf("expected a git-failed stop, got err %v and %+v", run.err, run.report)
	}
	if run.lands.fetches != 1 || len(run.waits) != 0 {
		t.Fatalf("expected one fetch and no wait, got %d fetches and waits %v", run.lands.fetches, run.waits)
	}
	if notes := notesNaming(run.report, "retried"); len(notes) != 0 {
		t.Fatalf("expected no retry note, got %q", notes)
	}
}

func TestAPushThatMeetsAPassingFaultTwiceIsRetriedWithNotesAndLands(t *testing.T) {
	fault := &application.TransientFault{Why: "connection timed out", Err: fmt.Errorf("%w: connection timed out", application.ErrPushTransient)}
	run := aCloseOutOverFlakyGit(t, &flakyLanding{pushFails: 2, err: fault}, false)

	if run.err != nil || !run.report.Landed {
		t.Fatalf("expected the story landed on the third push, got err %v and %+v", run.err, run.report)
	}
	if run.lands.pushes != 3 {
		t.Fatalf("expected 3 pushes, got %d", run.lands.pushes)
	}
	want := []string{
		"push failed (connection timed out), retried in 10s",
		"push failed (connection timed out), retried in 20s",
	}
	if notes := notesNaming(run.report, "push failed"); fmt.Sprint(notes) != fmt.Sprint(want) {
		t.Fatalf("expected one note per retry, %q, got %q", want, notes)
	}
}

func TestAPushThatIsRefusedIsNotRetried(t *testing.T) {
	run := aCloseOutOverFlakyGit(t, &flakyLanding{pushFails: 100, err: errors.New("git push origin in /w: exit status 1: ! [remote rejected] HEAD -> main (protected branch hook declined)")}, false)

	if run.err == nil || run.report.Landed {
		t.Fatalf("expected the landing to fail, got err %v and %+v", run.err, run.report)
	}
	if run.lands.pushes != 1 || len(run.waits) != 0 {
		t.Fatalf("expected one push and no wait, got %d pushes and waits %v", run.lands.pushes, run.waits)
	}
}
