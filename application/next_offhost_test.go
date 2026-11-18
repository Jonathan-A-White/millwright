package application_test

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1481: mw-gq6.1461 ran on cloud1 and its result.json is on cloud1. A
// mw next for it on the Laptop wrote a false 'not landed (no-result)' ledger
// line, committed it, wrote a run note and mailed 'Blocked:' before it said
// no-result. A story claimed on another host, whose run record is not here,
// is refused at the start.
type offHostRun struct {
	report  application.NextReport
	err     error
	tracker *apptest.FakeTracker
	files   *apptest.FakeVaultFiles
	mail    *apptest.FakeMailbox
	ledger  []string
	lands   *fakeRetryLanding
}

// aCloseOutClaimedOn runs mw next on host for mw-gq6.9, claimed by assignee,
// whose result file is empty when writeResult is set and never written when not.
func aCloseOutClaimedOn(t *testing.T, host, assignee string, writeResult bool) *offHostRun {
	t.Helper()
	ctx := t.Context()
	dispatch, tracker, _, runner, vaultDir := aFactory(t)
	now := time.Date(2026, 11, 18, 5, 47, 0, 0, time.UTC)
	tracker.Clock = func() time.Time { return now }
	claimedStory(t, tracker, "mw-gq6.9", 1)
	if err := tracker.ClaimAs("mw-gq6.9", assignee, now.Add(time.Hour)); err != nil {
		t.Fatalf("claiming as %s: %v", assignee, err)
	}
	name := startOldSession(t, runner, "mw-gq6.9")
	runner.Write(name, "working\n")
	dir := filepath.Join(vaultDir, application.RunsDir, "mw-gq6.9")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making the run directory: %v", err)
	}
	if writeResult {
		if err := os.WriteFile(filepath.Join(dir, application.ResultFileNameForAttempt(1)), nil, 0o644); err != nil {
			t.Fatalf("writing the empty result: %v", err)
		}
	}
	run := &offHostRun{
		tracker: tracker, files: &apptest.FakeVaultFiles{}, mail: apptest.NewFakeMailbox(), lands: &fakeRetryLanding{},
	}
	run.report, run.err = application.Next{
		Tracker: tracker, Vault: dispatch.Boot.Vault, Worktrees: run.lands, Landing: run.lands,
		Files: run.files, Mailbox: run.mail, Memory: tracker,
		Checks: checkStub{}, Slot: slotStub{}, Runner: runner,
		Seat: "builder", Host: host,
		Rigs: dispatch.Rigs,
		Now:  func() time.Time { return now },
		Out:  io.Discard, Err: io.Discard,
	}.Run(ctx, "mw-gq6.9")
	run.ledger, _ = dispatch.Boot.Vault.ReadLedger(ctx, "builder")
	return run
}

func TestNextOnAnotherHostRefusesBeforeWritingAnything(t *testing.T) {
	for _, assignee := range []string{"mw@cloud1", "builder@cloud1"} {
		t.Run(assignee, func(t *testing.T) {
			run := aCloseOutClaimedOn(t, "laptop", assignee, false)

			if run.err == nil {
				t.Fatalf("expected a refusal, got none; report: %s", run.report.String())
			}
			for _, want := range []string{"cloud1", "laptop", "mw next mw-gq6.9"} {
				if !strings.Contains(run.err.Error(), want) {
					t.Errorf("expected %q in the refusal, got: %v", want, run.err)
				}
			}
			if strings.Contains(run.err.Error(), "\n") {
				t.Errorf("expected the refusal to be one line, got: %q", run.err)
			}
			if len(run.ledger) != 0 {
				t.Errorf("expected no ledger line, got %q", run.ledger)
			}
			if commits := run.files.Commits(); len(commits) != 0 {
				t.Errorf("expected no vault commit, got %v", commits)
			}
			if comments := run.tracker.Comments("mw-gq6.9"); len(comments) != 0 {
				t.Errorf("expected no comment on the story, got %q", comments)
			}
			if run.mail.Writes() != 0 {
				t.Errorf("expected no mail, %d were written", run.mail.Writes())
			}
			if run.report.Reason != "" {
				t.Errorf("expected no stop reason, got %q", run.report.Reason)
			}
		})
	}
}

// On the host the claim was made on, mw next goes on as it always did: a
// session that left no result is the no-result stop, ledgered and mailed.
func TestNextOnTheClaimingHostStopsNoResultAsBefore(t *testing.T) {
	for _, assignee := range []string{"mw@cloud1", "builder@cloud1"} {
		t.Run(assignee, func(t *testing.T) {
			run := aCloseOutClaimedOn(t, "cloud1", assignee, false)

			if run.report.Reason != application.ReasonNoResult {
				t.Fatalf("expected %s, got %q (%v)", application.ReasonNoResult, run.report.Reason, run.err)
			}
			if len(run.ledger) == 0 {
				t.Errorf("expected the stop's ledger line, got none")
			}
			if run.mail.Writes() == 0 {
				t.Errorf("expected the Mayor mailed of the stop, nothing was")
			}
		})
	}
}

// A run record that is here is read wherever mw next runs: the story's result
// is judged, not refused for the claim's host.
func TestNextOnAnotherHostGoesOnWhenTheRunRecordIsHere(t *testing.T) {
	run := aCloseOutClaimedOn(t, "laptop", "mw@cloud1", true)

	if run.err != nil && strings.Contains(run.err.Error(), "run `mw next") {
		t.Fatalf("expected the close-out to go on with a run record here, got: %v", run.err)
	}
	if run.report.Reason != application.ReasonNoResult {
		t.Errorf("expected the empty result judged as %s, got %q (%v)", application.ReasonNoResult, run.report.Reason, run.err)
	}
}
