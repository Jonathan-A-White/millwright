package application_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// A batch's fixer ran forty hand-made burners for 40 minutes on the 20-core home
// and nobody was told but by the Millhand (mw-gq6.1529). The check names a tree
// of a Builder's processes that keeps more of them running than the host has
// cores, and tells the Mayor once it has done so for over ten minutes.

const (
	burnWorktrees = "/home/jonathan/.mw-worktrees"
	burnHome      = "/home/jonathan"
)

// fakeProcessTable is the host's process table at a time the test moves.
type fakeProcessTable struct {
	at    time.Time
	procs []application.RunnableProcess
	err   error
}

func (f *fakeProcessTable) Snapshot(context.Context) (application.RunnableSnapshot, error) {
	return application.RunnableSnapshot{At: f.at, Processes: append([]application.RunnableProcess(nil), f.procs...)}, f.err
}

// aSessionWithBurners is a Builder's session (pid 100, in its worktree) with a
// shell under it and n burners under that, each running or not.
func aSessionWithBurners(story string, n int, running bool) []application.RunnableProcess {
	cwd := burnWorktrees + "/" + story
	procs := []application.RunnableProcess{
		{Pid: 100, Ppid: 1, Cwd: cwd, Argv: []string{"claude"}},
		{Pid: 101, Ppid: 100, Cwd: cwd, Argv: []string{"bash"}},
	}
	for i := 0; i < n; i++ {
		// The burners' own working directory is anywhere: it is the parent that
		// ties them to the Builder.
		procs = append(procs, application.RunnableProcess{Pid: 200 + i, Ppid: 101, Cwd: "/tmp",
			Argv: []string{"sh", "/tmp/burn575.sh"}, Running: running})
	}
	return procs
}

type burnFixture struct {
	table *fakeProcessTable
	state *apptest.FakeDoctorState
	mails []application.NewMessage
	cores int
	check *application.BuilderBurnCheck
}

func aBurnFixture(procs []application.RunnableProcess) *burnFixture {
	f := &burnFixture{
		table: &fakeProcessTable{at: time.Date(2026, 11, 19, 5, 0, 0, 0, time.UTC), procs: procs},
		state: apptest.NewFakeDoctorState(),
		cores: 20,
	}
	f.check = &application.BuilderBurnCheck{
		Host:         "laptop",
		WorktreeDirs: []string{burnWorktrees},
		LandingDirs:  []string{burnHome},
		Table:        f.table,
		Cores:        func(context.Context) (int, error) { return f.cores, nil },
		State:        f.state,
		Mail:         func(_ context.Context, m application.NewMessage) error { f.mails = append(f.mails, m); return nil },
	}
	return f
}

// probeEvery probes n times, moving the table's clock on by step after each, and
// returns the last verdict and reason.
func (f *burnFixture) probeEvery(step time.Duration, n int) (application.Verdict, string) {
	var verdict application.Verdict
	var reason string
	for i := 0; i < n; i++ {
		verdict, reason = f.check.Probe(context.Background())
		f.table.at = f.table.at.Add(step)
	}
	return verdict, reason
}

func TestBuilderBurnNamesATreeThatKeepsMoreProcessesRunningThanTheHostHasCoresForOverTenMinutes(t *testing.T) {
	f := aBurnFixture(aSessionWithBurners("mw-gq6.575", 40, true))

	// Five minutes over is not yet ten.
	if verdict, reason := f.probeEvery(5*time.Minute, 2); verdict != application.DoctorOK {
		t.Fatalf("five minutes over the cores is not a fault yet, got %v: %s", verdict, reason)
	}
	verdict, reason := f.probeEvery(5*time.Minute, 2)
	if verdict != application.DoctorFaulty {
		t.Fatalf("ten and more minutes over the cores should be faulty, got %v: %s", verdict, reason)
	}
	for _, want := range []string{"mw-gq6.575", "20 cores", "10 minutes"} {
		if !strings.Contains(reason, want) {
			t.Errorf("expected the finding to name %q, got %q", want, reason)
		}
	}
}

func TestBuilderBurnNamesALandingTreeToo(t *testing.T) {
	procs := aSessionWithBurners("x", 30, true)
	for i := range procs[:2] {
		procs[i].Cwd = burnHome + "/" + application.LandingPrefix + "lampas-575"
	}
	f := aBurnFixture(procs)
	verdict, reason := f.probeEvery(6*time.Minute, 3)
	if verdict != application.DoctorFaulty || !strings.Contains(reason, application.LandingPrefix+"lampas-575") {
		t.Fatalf("a batch fixer's burners should be named by its landing, got %v: %s", verdict, reason)
	}
}

func TestBuilderBurnLeavesATreeAtOrUnderTheCoresAlone(t *testing.T) {
	f := aBurnFixture(aSessionWithBurners("mw-gq6.575", 20, true))
	if verdict, reason := f.probeEvery(6*time.Minute, 5); verdict != application.DoctorOK {
		t.Fatalf("exactly the cores running is not over them, got %v: %s", verdict, reason)
	}
}

func TestBuilderBurnLeavesProcessesThatAreNotRunningAlone(t *testing.T) {
	f := aBurnFixture(aSessionWithBurners("mw-gq6.575", 60, false))
	if verdict, reason := f.probeEvery(6*time.Minute, 5); verdict != application.DoctorOK {
		t.Fatalf("sleeping processes put no load on the host, got %v: %s", verdict, reason)
	}
}

func TestBuilderBurnLeavesProcessesOutsideEveryBuildersDirectoryAlone(t *testing.T) {
	procs := aSessionWithBurners("mw-gq6.575", 40, true)
	for i := range procs {
		procs[i].Cwd = "/home/jonathan/sermon-index"
	}
	f := aBurnFixture(procs)
	if verdict, reason := f.probeEvery(6*time.Minute, 5); verdict != application.DoctorOK {
		t.Fatalf("a tree no Builder owns is another's to watch, got %v: %s", verdict, reason)
	}
}

func TestBuilderBurnStartsTheTenMinutesAgainWhenTheTreeFallsUnderTheCores(t *testing.T) {
	f := aBurnFixture(aSessionWithBurners("mw-gq6.575", 40, true))
	f.probeEvery(5*time.Minute, 2)
	// The burners stop for one look.
	f.table.procs = aSessionWithBurners("mw-gq6.575", 40, false)
	f.check.Probe(context.Background())
	// And start again: ten minutes are counted from here.
	f.table.at = f.table.at.Add(5 * time.Minute)
	f.table.procs = aSessionWithBurners("mw-gq6.575", 40, true)
	if verdict, reason := f.check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("the count should have started again, got %v: %s", verdict, reason)
	}
}

func TestBuilderBurnDoesNotCountAGapOfHoursAsTenMinutesOver(t *testing.T) {
	f := aBurnFixture(aSessionWithBurners("mw-gq6.575", 40, true))
	f.check.Probe(context.Background())
	// mw doctor was not running; the host may have been calm in between.
	f.table.at = f.table.at.Add(5 * time.Hour)
	if verdict, reason := f.check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("a look hours later proves nothing about the time between, got %v: %s", verdict, reason)
	}
}

func TestBuilderBurnCannotTellWhenTheTableOrTheCoresCannotBeRead(t *testing.T) {
	f := aBurnFixture(nil)
	f.table.err = errors.New("no /proc")
	if verdict, _ := f.check.Probe(context.Background()); verdict != application.DoctorCannotTell {
		t.Fatalf("an unreadable table is cannot-tell, got %v", verdict)
	}
	f = aBurnFixture(nil)
	f.check.Cores = func(context.Context) (int, error) { return 0, errors.New("no cores") }
	if verdict, _ := f.check.Probe(context.Background()); verdict != application.DoctorCannotTell {
		t.Fatalf("unreadable cores are cannot-tell, got %v", verdict)
	}
}

func TestBuilderBurnCureMailsTheMayorAndStopsNothing(t *testing.T) {
	f := aBurnFixture(aSessionWithBurners("mw-gq6.575", 40, true))
	if verdict, _ := f.probeEvery(5*time.Minute, 4); verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty")
	}
	if err := f.check.Cure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.mails) != 1 {
		t.Fatalf("expected one mail, got %d", len(f.mails))
	}
	m := f.mails[0]
	if m.To != application.MayorMailbox || !strings.Contains(m.Subject, "laptop") {
		t.Errorf("the mail should go to the Mayor and name the host, got %+v", m)
	}
	for _, want := range []string{"mw-gq6.575", "pid 100", "contrib/mw-under-load", fmt.Sprint(f.cores)} {
		if !strings.Contains(m.Body, want) {
			t.Errorf("expected the mail to say %q, got %q", want, m.Body)
		}
	}
	if !strings.Contains(f.check.WayBack(), "mail") {
		t.Errorf("the way back should say only a mail was sent, got %q", f.check.WayBack())
	}
}
