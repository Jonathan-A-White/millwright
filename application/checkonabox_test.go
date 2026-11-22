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

// mw-gq6.1623: mw check run on the home for a story a cloud box worked stopped
// 'run `mw check <id>` on cloud9' and the seat has no ssh name for a box. The
// check is run on the host that worked the story, which the home reaches as it
// reaches a box for mw retry and mw next --met-by-mayor.
type checkOnABoxRun struct {
	out    strings.Builder
	report application.CheckReport
	err    error
	remote *apptest.FakeChecksElsewhere
	checks *scriptedChecks
}

// aCheckOnTheHome runs mw check on the laptop for mw-x.1, which cloud9 worked.
// claimed says the story is still claimed as mw@cloud9; without it only its
// path names cloud9 and its branch is not in the laptop's checkout.
func aCheckOnTheHome(t *testing.T, claimed bool, mutate func(*application.Check)) *checkOnABoxRun {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "cloud9"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	if claimed {
		if err := tracker.ClaimAs("mw-x.1", "mw@cloud9", time.Now().Add(time.Hour)); err != nil {
			t.Fatalf("claiming as mw@cloud9: %v", err)
		}
	}
	rigDir := filepath.Join(t.TempDir(), "rigs", "millwright")
	if err := os.MkdirAll(rigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	run := &checkOnABoxRun{remote: apptest.NewFakeChecksElsewhere(), checks: &scriptedChecks{passes: []bool{true}}}
	run.remote.Prints("cloud9", "check on cloud9: mw-x.1\n  ok      the rig's tests pass\n")
	k := application.Check{
		Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1, NoBranch: true}, Checks: run.checks,
		Host: "laptop", Remote: "origin",
		Rigs:  map[string]string{"millwright": rigDir},
		There: run.remote,
		Out:   &run.out,
	}
	if mutate != nil {
		mutate(&k)
	}
	run.report, run.err = k.Run(t.Context(), "mw-x.1")
	return run
}

func (r *checkOnABoxRun) wantRunOnCloud9(t *testing.T, flags ...string) {
	t.Helper()
	if r.err != nil {
		t.Fatalf("expected the check run on cloud9, got: %v", r.err)
	}
	calls := r.remote.Calls()
	if len(calls) != 1 || calls[0].Host != "cloud9" || calls[0].Story != "mw-x.1" {
		t.Fatalf("expected one mw check of mw-x.1 on cloud9, got %+v", calls)
	}
	if got, want := strings.Join(calls[0].Flags, " "), strings.Join(flags, " "); got != want {
		t.Errorf("expected the flags %q, got %q", want, got)
	}
	if r.report.DelegatedTo != "cloud9" || !strings.Contains(r.out.String(), "the rig's tests pass") {
		t.Errorf("expected the output to say cloud9 ran it and what it printed, got: %s", r.out.String())
	}
	if r.checks.runs != 0 {
		t.Errorf("expected no tests run on the home, got %d", r.checks.runs)
	}
}

func TestCheckOnABoxStillClaimedThereIsRunThere(t *testing.T) {
	aCheckOnTheHome(t, true, nil).wantRunOnCloud9(t)
}

func TestCheckOnABoxWhoseBranchIsNotHereIsRunThere(t *testing.T) {
	aCheckOnTheHome(t, false, nil).wantRunOnCloud9(t)
}

func TestCheckOnABoxCarriesItsFlags(t *testing.T) {
	aCheckOnTheHome(t, true, func(k *application.Check) {
		k.ThereFlags = []string{"--part", "2", "--wait", "40m"}
	}).wantRunOnCloud9(t, "--part", "2", "--wait", "40m")
}

// A box that cannot be reached, or whose check fails, is an error that says so,
// with what the box printed: the check failed there, not here.
func TestCheckOnABoxSaysWhyTheBoxDidNotPass(t *testing.T) {
	run := aCheckOnTheHome(t, true, func(k *application.Check) {
		fails := apptest.NewFakeChecksElsewhere()
		fails.Prints("cloud9", "check on cloud9: mw-x.1\n  REFUSED the tests fail\n")
		fails.Fails("cloud9", io.ErrUnexpectedEOF)
		k.There = fails
	})
	if run.err == nil {
		t.Fatalf("expected an error, got none")
	}
	for _, want := range []string{"cloud9", "unexpected EOF", "mw-x.1"} {
		if !strings.Contains(run.err.Error(), want) {
			t.Errorf("expected %q in the error, got: %v", want, run.err)
		}
	}
	if !strings.Contains(run.out.String(), "REFUSED the tests fail") {
		t.Errorf("expected what cloud9 printed in the output, got: %s", run.out.String())
	}
}

// Without a way to reach a host the refusal stands and names the command.
func TestCheckOnABoxWithNoWayThereRefuses(t *testing.T) {
	for name, claimed := range map[string]bool{"claimed there": true, "branch not here": false} {
		t.Run(name, func(t *testing.T) {
			run := aCheckOnTheHome(t, claimed, func(k *application.Check) { k.There = nil })
			if run.err == nil || !strings.Contains(run.err.Error(), "run `mw check mw-x.1` on cloud9") {
				t.Fatalf("expected the refusal naming mw check on cloud9, got: %v", run.err)
			}
		})
	}
}

// On the host that worked the story nothing is run elsewhere: the refusal stands.
func TestCheckOnTheStorysOwnHostIsNotRunElsewhere(t *testing.T) {
	run := aCheckOnTheHome(t, false, func(k *application.Check) { k.Host = "cloud9" })
	if len(run.remote.Calls()) != 0 {
		t.Fatalf("expected no host asked, got %+v", run.remote.Calls())
	}
	if run.err == nil {
		t.Fatalf("expected a refusal")
	}
}
