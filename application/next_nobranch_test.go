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
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.1564: mw-gq6.1540 was worked on cloud1, where its branch mw/<id> is.
// mw next --no-dispatch --met-by-mayor on the Laptop wrote the MET-BY-MAYOR
// comment, then stopped on a raw 'git rev-list ... exit status 128', with a
// 'not landed' ledger line and a Refused mail. A host whose checkout of the rig
// lacks the story's branch refuses first, naming the host that worked it.

const anUnmetClosingComment = "AC 1 is not met on the box it ran on.\n\nFor the rig memory:\nnothing"

// aMetByMayorCloseOut runs mw next --met-by-mayor on host for mw-gq6.9, claimed
// as assignee, with its result file in this vault (so the run record is not what
// refuses) and a closing comment that says a criterion is not met. noBranch
// says the rig's checkout here has no mw/mw-gq6.9.
func aMetByMayorCloseOut(t *testing.T, host, assignee string, noBranch bool) *offHostRun {
	t.Helper()
	ctx := t.Context()
	dispatch, tracker, _, runner, vaultDir := aFactory(t)
	now := time.Date(2026, 11, 20, 9, 6, 0, 0, time.UTC)
	tracker.Clock = func() time.Time { return now }
	claimedStory(t, tracker, "mw-gq6.9", 1)
	if err := tracker.ClaimAs("mw-gq6.9", assignee, now.Add(time.Hour)); err != nil {
		t.Fatalf("claiming as %s: %v", assignee, err)
	}
	mustDo(t, tracker.CommentOnStoryAt("mw-gq6.9", anUnmetClosingComment, now.Add(-time.Hour)))
	runner.Exit(startOldSession(t, runner, "mw-gq6.9"), 0)
	dir := filepath.Join(vaultDir, application.RunsDir, "mw-gq6.9")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making the run directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, application.ResultFileNameForAttempt(1)), []byte(`{"subtype":"success"}`), 0o644); err != nil {
		t.Fatalf("writing the result: %v", err)
	}
	run := &offHostRun{
		tracker: tracker, files: &apptest.FakeVaultFiles{}, mail: apptest.NewFakeMailbox(),
		lands: &fakeRetryLanding{AheadCount: 1, NoBranch: noBranch},
	}
	run.report, run.err = application.Next{
		Tracker: tracker, Vault: dispatch.Boot.Vault, Worktrees: run.lands, Landing: run.lands,
		Files: run.files, Mailbox: run.mail, Memory: tracker,
		Checks: checkStub{}, Slot: slotStub{}, Runner: runner,
		Seat: "builder", Host: host,
		Rigs:            dispatch.Rigs,
		MetByMayor:      "ran AC 1 on the laptop: it passed",
		MetByMayorGiven: true,
		Now:             func() time.Time { return now },
		Out:             io.Discard, Err: io.Discard,
	}.Run(ctx, "mw-gq6.9")
	run.ledger, _ = dispatch.Boot.Vault.ReadLedger(ctx, "builder")
	return run
}

func TestNextOnAHostWithoutTheStorysBranchRefusesBeforeWritingAnything(t *testing.T) {
	for _, assignee := range []string{"mw@cloud1", "builder@cloud1"} {
		t.Run(assignee, func(t *testing.T) {
			run := aMetByMayorCloseOut(t, "laptop", assignee, true)

			if run.err == nil {
				t.Fatalf("expected a refusal, got none; report: %s", run.report.String())
			}
			for _, want := range []string{"mw/mw-gq6.9", "cloud1", "laptop", "mw next mw-gq6.9"} {
				if !strings.Contains(run.err.Error(), want) {
					t.Errorf("expected %q in the refusal, got: %v", want, run.err)
				}
			}
			if strings.Contains(run.err.Error(), "\n") || strings.Contains(run.err.Error(), "rev-list") {
				t.Errorf("expected a one-line refusal that is no raw git error, got: %q", run.err)
			}
			if len(run.ledger) != 0 {
				t.Errorf("expected no ledger line, got %q", run.ledger)
			}
			if commits := run.files.Commits(); len(commits) != 0 {
				t.Errorf("expected no vault commit, got %v", commits)
			}
			if comments := run.tracker.Comments("mw-gq6.9"); len(comments) != 1 {
				t.Errorf("expected only the closing comment on the story, got %q", comments)
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

// A story whose claim names no host is still refused, naming the host its path
// names (the fixture's stories are for vps).
func TestNextWithoutTheStorysBranchAndNoClaimHostNamesTheHostOfItsPath(t *testing.T) {
	run := aMetByMayorCloseOut(t, "laptop", "someone", true)

	if run.err == nil || !strings.Contains(run.err.Error(), "mw/mw-gq6.9") || !strings.Contains(run.err.Error(), "mw next mw-gq6.9` on vps") {
		t.Fatalf("expected a refusal naming the branch and vps, got: %v", run.err)
	}
	if comments := run.tracker.Comments("mw-gq6.9"); len(comments) != 1 {
		t.Errorf("expected only the closing comment on the story, got %q", comments)
	}
}

// With the branch in the checkout, the Mayor's word is written as it always was.
func TestNextWithTheStorysBranchHereWritesTheMayorsWordAsBefore(t *testing.T) {
	run := aMetByMayorCloseOut(t, "cloud1", "mw@cloud1", false)

	if run.err != nil && strings.Contains(run.err.Error(), "is not in") {
		t.Fatalf("expected no refusal with the branch here, got: %v", run.err)
	}
	said := strings.Join(run.tracker.Comments("mw-gq6.9"), "\n")
	if !strings.Contains(said, application.MetByMayorMark) {
		t.Errorf("expected the MET-BY-MAYOR comment written, got:\n%s", said)
	}
}

// mw check reads the same branch in the same checkout: without it, it refuses
// up front, naming the host that worked the story, instead of running the gate
// on nothing.
func TestCheckOnAHostWithoutTheStorysBranchRefusesNamingTheHost(t *testing.T) {
	for host, want := range map[string]string{
		"cloud1": "run `mw check mw-gq6.9` on cloud1",
		"laptop": "though the story names this host",
	} {
		t.Run(host, func(t *testing.T) {
			tracker := apptest.NewFakeTracker()
			tracker.AddEpic("mw-gq6", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: host})
			tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.9", Title: "A story"})
			var out strings.Builder
			_, err := application.Check{
				Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1, NoBranch: true}, Checks: &scriptedChecks{passes: []bool{true}},
				Host: "laptop", Remote: "origin",
				Rigs: map[string]string{"millwright": "/rigs/millwright"},
				Out:  io.MultiWriter(io.Discard, &out),
			}.Run(t.Context(), "mw-gq6.9")

			if err == nil {
				t.Fatalf("expected a refusal, got none")
			}
			for _, w := range []string{"mw/mw-gq6.9", "laptop", want} {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("expected %q in the refusal, got: %v", w, err)
				}
			}
			if strings.Contains(err.Error(), "\n") {
				t.Errorf("expected a one-line refusal, got %q", err)
			}
			if comments := tracker.Comments("mw-gq6.9"); len(comments) != 0 {
				t.Errorf("expected no comment, got %q", comments)
			}
		})
	}
}
