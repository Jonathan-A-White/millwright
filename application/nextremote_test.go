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

// mw-gq6.1622: cloud9 refused mw-gq6.1619 and the Mayor, on the Laptop, ran mw
// next --no-dispatch --rescoped-by-mayor: it stopped 'its branch is not in
// laptop's checkout, it was worked on cloud9: run mw next on cloud9', and the
// seat has no ssh name for a cloud box. The Mayor's word is answered on the host
// that worked the story, which the home reaches as it reaches a box for mw retry.
type mayorAnswerRun struct {
	report  application.NextReport
	err     error
	remote  *apptest.FakeMayorAnswers
	tracker *apptest.FakeTracker
	files   *apptest.FakeVaultFiles
	mail    *apptest.FakeMailbox
	ledger  []string
}

// aMayorAnswerOnTheHome runs mw next on the laptop for mw-gq6.9, claimed on
// cloud9, with next set to say what the Mayor's flags are. noBranch leaves the
// story's branch out of the laptop's checkout and its run record in the vault:
// the story as cloud9's refusal and a sync leave it; without it there is no run
// record here.
func aMayorAnswerOnTheHome(t *testing.T, noBranch bool, flags func(*application.Next)) *mayorAnswerRun {
	t.Helper()
	ctx := t.Context()
	dispatch, tracker, _, runner, vaultDir := aFactory(t)
	now := time.Date(2026, 11, 21, 23, 52, 0, 0, time.UTC)
	tracker.Clock = func() time.Time { return now }
	claimedStory(t, tracker, "mw-gq6.9", 1)
	if err := tracker.ClaimAs("mw-gq6.9", "builder@cloud9", now.Add(time.Hour)); err != nil {
		t.Fatalf("claiming as builder@cloud9: %v", err)
	}
	dir := filepath.Join(vaultDir, application.RunsDir, "mw-gq6.9")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making the run directory: %v", err)
	}
	if noBranch {
		if err := os.WriteFile(filepath.Join(dir, application.ResultFileNameForAttempt(1)), nil, 0o644); err != nil {
			t.Fatalf("writing the empty result: %v", err)
		}
	}
	run := &mayorAnswerRun{
		remote: apptest.NewFakeMayorAnswers(), tracker: tracker, files: &apptest.FakeVaultFiles{}, mail: apptest.NewFakeMailbox(),
	}
	run.remote.Prints("cloud9", "next on cloud9: mw-gq6.9\n  landed  on main\n")
	lands := &fakeRetryLanding{NoBranch: noBranch}
	next := application.Next{
		Tracker: tracker, Vault: dispatch.Boot.Vault, Worktrees: lands, Landing: lands,
		Files: run.files, Mailbox: run.mail, Memory: tracker,
		Checks: checkStub{}, Slot: slotStub{}, Runner: runner,
		Seat: "builder", Host: "laptop",
		Rigs:    dispatch.Rigs,
		Answers: run.remote,
		Now:     func() time.Time { return now },
		Out:     io.Discard, Err: io.Discard,
	}
	flags(&next)
	run.report, run.err = next.Run(ctx, "mw-gq6.9")
	run.ledger, _ = dispatch.Boot.Vault.ReadLedger(ctx, "builder")
	return run
}

func (r *mayorAnswerRun) wantRunOnCloud9(t *testing.T, flag, word string) {
	t.Helper()
	if r.err != nil {
		t.Fatalf("expected the Mayor's word answered on cloud9, got: %v", r.err)
	}
	calls := r.remote.Calls()
	if len(calls) != 1 || calls[0].Host != "cloud9" || calls[0].Story != "mw-gq6.9" {
		t.Fatalf("expected one mw next of mw-gq6.9 on cloud9, got %+v", calls)
	}
	joined := strings.Join(calls[0].Flags, "\x00")
	if want := "--no-dispatch\x00" + flag + "\x00" + word; joined != want {
		t.Errorf("expected the flags %q, got %q", want, joined)
	}
	if r.report.DelegatedTo != "cloud9" || !strings.Contains(r.report.String(), "landed  on main") {
		t.Errorf("expected the report to say cloud9 ran it and what it printed, got: %s", r.report.String())
	}
	if len(r.ledger) != 0 || len(r.files.Commits()) != 0 || r.mail.Writes() != 0 || len(r.tracker.Comments("mw-gq6.9")) != 0 {
		t.Errorf("expected nothing written on the home (ledger %q, commits %v, %d mails, comments %q)",
			r.ledger, r.files.Commits(), r.mail.Writes(), r.tracker.Comments("mw-gq6.9"))
	}
}

func TestMayorAnswerOnABoxMetByMayorIsRunThere(t *testing.T) {
	for name, noBranch := range map[string]bool{"no run record here": false, "branch not here": true} {
		t.Run(name, func(t *testing.T) {
			run := aMayorAnswerOnTheHome(t, noBranch, func(n *application.Next) {
				n.MetByMayor, n.MetByMayorGiven = "ran AC 1 on the laptop: 1.68 s", true
			})
			run.wantRunOnCloud9(t, "--met-by-mayor", "ran AC 1 on the laptop: 1.68 s")
		})
	}
}

func TestMayorAnswerOnABoxRescopedByMayorIsRunThere(t *testing.T) {
	for name, noBranch := range map[string]bool{"no run record here": false, "branch not here": true} {
		t.Run(name, func(t *testing.T) {
			run := aMayorAnswerOnTheHome(t, noBranch, func(n *application.Next) {
				n.RescopedByMayor = "narrowed AC 1"
			})
			run.wantRunOnCloud9(t, "--rescoped-by-mayor", "narrowed AC 1")
		})
	}
}

// A box that cannot be reached, or whose mw next refuses, is the Mayor's error,
// with what the box printed in the report: nothing is written here.
func TestMayorAnswerOnABoxSaysWhyTheBoxDidNotAnswer(t *testing.T) {
	run := aMayorAnswerOnTheHome(t, true, func(n *application.Next) {
		n.RescopedByMayor = "narrowed AC 1"
		fails := apptest.NewFakeMayorAnswers()
		fails.Prints("cloud9", "next on cloud9: mw-gq6.9\n  STOPPED the tests fail\n")
		fails.Fails("cloud9", io.ErrUnexpectedEOF)
		n.Answers = fails
	})
	if run.err == nil {
		t.Fatalf("expected an error, got none")
	}
	for _, want := range []string{"cloud9", "unexpected EOF", "mw-gq6.9"} {
		if !strings.Contains(run.err.Error(), want) {
			t.Errorf("expected %q in the error, got: %v", want, run.err)
		}
	}
	if !strings.Contains(run.report.String(), "STOPPED the tests fail") {
		t.Errorf("expected what cloud9 printed in the report, got: %s", run.report.String())
	}
	if len(run.ledger) != 0 || run.mail.Writes() != 0 {
		t.Errorf("expected nothing written on the home, got ledger %q and %d mails", run.ledger, run.mail.Writes())
	}
}

// Without the Mayor's word, or on the host that worked the story, nothing is
// run elsewhere: the refusal stands and names the command that works from here.
func TestMayorAnswerOnABoxIsNotRunWithoutTheFlags(t *testing.T) {
	run := aMayorAnswerOnTheHome(t, true, func(n *application.Next) {})
	if len(run.remote.Calls()) != 0 {
		t.Fatalf("expected no host asked without a flag, got %+v", run.remote.Calls())
	}
	if run.err == nil {
		t.Fatalf("expected a refusal")
	}
	for _, want := range []string{"cloud9", "mw next mw-gq6.9 --no-dispatch --met-by-mayor", "--rescoped-by-mayor", "over ssh"} {
		if !strings.Contains(run.err.Error(), want) {
			t.Errorf("expected %q in the refusal, got: %v", want, run.err)
		}
	}
	if strings.Contains(run.err.Error(), "\n") {
		t.Errorf("expected the refusal to be one line, got: %q", run.err)
	}
}
