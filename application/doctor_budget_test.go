package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1199: mw-doctor.service was stopped by systemd at its 2-minute start
// limit on a loaded laptop (a bd read taking 38 s), and mailed 'unit failed
// twice' though no check had found a fault. A check that outlasts its budget is
// cannot-tell, and the run goes on and ends as it would have.

// slowCheck is a check whose probe does not come back until released, and does
// not look at its context, as a probe waiting on a bd that is hung does not.
type slowCheck struct {
	apptest.FakeDoctorCheck
	release chan struct{}
}

func (s *slowCheck) Probe(context.Context) (application.Verdict, string) {
	<-s.release
	return application.DoctorOK, ""
}

func TestACheckPastItsBudgetIsCannotTellAndTheRunGoesOn(t *testing.T) {
	slow := &slowCheck{release: make(chan struct{})}
	defer close(slow.release)
	slow.CheckName = "slowpoke"
	after := &apptest.FakeDoctorCheck{CheckName: "after", Verdict: application.DoctorOK}

	state := apptest.NewFakeDoctorState()
	log := &apptest.FakeDoctorLog{}
	now := doctorNow
	d := aDoctor(state, log, &now, slow, after)
	d.CheckBudget = 50 * time.Millisecond
	d.Load = func() string { return "41.2" }
	var out strings.Builder
	d.Out = &out

	report, err := d.Run(context.Background(), "", false)
	if err != nil {
		t.Fatalf("a check past its budget is no failure of the run, got %v", err)
	}
	if report.Faulty() {
		t.Errorf("a check past its budget is not left faulty: %v", report.FaultyNames())
	}
	if len(report.Results) != 2 {
		t.Fatalf("the run goes on to the next check: %d results", len(report.Results))
	}
	got := report.Results[0]
	if got.Verdict != "cannot-tell" || got.Reason != "took over 0.05s at load 41.2" {
		t.Errorf("slowpoke = %q %q, want cannot-tell (took over 0.05s at load 41.2)", got.Verdict, got.Reason)
	}
	if report.Results[1].Verdict != "ok" {
		t.Errorf("the check after is worked: %q", report.Results[1].Verdict)
	}
	if !strings.Contains(out.String(), "slowpoke: cannot-tell (took over 0.05s at load 41.2)") {
		t.Errorf("the report says it:\n%s", out.String())
	}
}

func TestACheckWithinItsBudgetIsUntouched(t *testing.T) {
	now := doctorNow
	check := &apptest.FakeDoctorCheck{CheckName: "widget", Verdict: application.DoctorOK}
	d := aDoctor(apptest.NewFakeDoctorState(), &apptest.FakeDoctorLog{}, &now, check)
	d.CheckBudget = time.Minute
	report, err := d.Run(context.Background(), "", false)
	if err != nil || report.Results[0].Verdict != "ok" {
		t.Fatalf("got %v, %v", report.Results, err)
	}
}

// blockedDampers is a damper read that does not come back until released.
type blockedDampers struct{ release chan struct{} }

func (b blockedDampers) DamperCounts(context.Context) (application.DampersReport, error) {
	<-b.release
	return application.DampersReport{}, nil
}

func TestTheDamperReadPastItsBudgetIsSaidAndTheRunEnds(t *testing.T) {
	now := doctorNow
	d := aDoctor(apptest.NewFakeDoctorState(), &apptest.FakeDoctorLog{}, &now,
		&apptest.FakeDoctorCheck{CheckName: "widget", Verdict: application.DoctorOK})
	d.CheckBudget = 50 * time.Millisecond
	d.Load = func() string { return "54.0" }
	blocked := blockedDampers{release: make(chan struct{})}
	defer close(blocked.release)
	d.Dampers = blocked
	var out strings.Builder
	d.Out = &out

	if _, err := d.Run(context.Background(), "", false); err != nil {
		t.Fatalf("a slow damper read is no failure, got %v", err)
	}
	if !strings.Contains(out.String(), "dampers: could not be read (took over 0.05s at load 54.0)") {
		t.Errorf("the line says why:\n%s", out.String())
	}
}

// mw-gq6.1287: each check was held to CheckBudget but the run was not held to
// anything, so many slow checks, each within its budget, ran past the unit's 4
// minutes. The run has a budget of its own, read off the injected Now.

// advancingCheck is a check whose probe takes took on the injected clock.
type advancingCheck struct {
	apptest.FakeDoctorCheck
	now  *time.Time
	took time.Duration
}

func (a *advancingCheck) Probe(ctx context.Context) (application.Verdict, string) {
	*a.now = a.now.Add(a.took)
	return a.FakeDoctorCheck.Probe(ctx)
}

func slowOne(name string, now *time.Time, verdict application.Verdict) *advancingCheck {
	return &advancingCheck{
		FakeDoctorCheck: apptest.FakeDoctorCheck{CheckName: name, Verdict: verdict, Wait: time.Hour, Cap: 3},
		now:             now,
		took:            2 * time.Minute,
	}
}

const spentReason = "took over 180s: the run's budget is spent"

func TestDoctorRunBudgetLeavesTheChecksAfterItCannotTell(t *testing.T) {
	now := doctorNow
	a, b := slowOne("a", &now, application.DoctorOK), slowOne("b", &now, application.DoctorOK)
	c, d := slowOne("c", &now, application.DoctorOK), slowOne("d", &now, application.DoctorOK)
	log := &apptest.FakeDoctorLog{}
	doc := aDoctor(apptest.NewFakeDoctorState(), log, &now, a, b, c, d)
	doc.CheckBudget = time.Minute * 10
	doc.RunBudget = 3 * time.Minute
	var out strings.Builder
	doc.Out = &out

	report, err := doc.Run(context.Background(), "", false)
	if err != nil {
		t.Fatalf("a run past its budget is no failure of the run, got %v", err)
	}
	if len(report.Results) != 4 {
		t.Fatalf("every check has a result: %d", len(report.Results))
	}
	for i, want := range []string{"ok", "ok", "cannot-tell", "cannot-tell"} {
		if got := report.Results[i].Verdict; got != want {
			t.Errorf("result %d (%s) = %q, want %q", i, report.Results[i].Check, got, want)
		}
	}
	for _, i := range []int{2, 3} {
		if got := report.Results[i].Reason; got != spentReason {
			t.Errorf("%s reason = %q, want %q", report.Results[i].Check, got, spentReason)
		}
	}
	if c.Probes() != 0 || d.Probes() != 0 {
		t.Errorf("a check after the budget is not probed: c %d, d %d", c.Probes(), d.Probes())
	}
	if !strings.Contains(out.String(), "c: cannot-tell ("+spentReason+")") {
		t.Errorf("the report says it:\n%s", out.String())
	}
	for _, line := range log.Lines() {
		if strings.Contains(line, "cannot-tell") {
			t.Errorf("a check past the budget is not logged: %s", line)
		}
	}
}

func TestDoctorRunBudgetEndsAsTheChecksThatRanDecide(t *testing.T) {
	now := doctorNow
	state := apptest.NewFakeDoctorState()
	// a is faulty and held by its damper: left faulty and uncured, so exit 6.
	_ = state.Save(context.Background(), "a", application.DoctorEpisode{Cures: 1, LastCure: now})
	a := slowOne("a", &now, application.DoctorFaulty)
	b := slowOne("b", &now, application.DoctorOK)
	c := slowOne("c", &now, application.DoctorFaulty)
	doc := aDoctor(state, &apptest.FakeDoctorLog{}, &now, a, b, c)
	doc.RunBudget = 3 * time.Minute

	report, err := doc.Run(context.Background(), "", false)
	fault, ok := application.DoctorFaults(err)
	if !ok || fault.Line != "a" {
		t.Fatalf("the run ends faulty by a, which ran: %v", err)
	}
	if got := report.Results[2]; got.Verdict != "cannot-tell" || got.Faulty {
		t.Errorf("c = %+v, want cannot-tell and not faulty", got)
	}
	if c.Probes() != 0 {
		t.Errorf("c is not probed once the budget is spent")
	}
	if application.ExitStatus(err) != application.DoctorFaultExit {
		t.Errorf("exit = %d, want %d", application.ExitStatus(err), application.DoctorFaultExit)
	}
}

func TestDoctorRunBudgetEndsOKWhenTheChecksThatRanAreOK(t *testing.T) {
	now := doctorNow
	a, b := slowOne("a", &now, application.DoctorOK), slowOne("b", &now, application.DoctorOK)
	c := slowOne("c", &now, application.DoctorFaulty)
	doc := aDoctor(apptest.NewFakeDoctorState(), &apptest.FakeDoctorLog{}, &now, a, b, c)
	doc.RunBudget = 3 * time.Minute
	_, err := doc.Run(context.Background(), "", false)
	if err != nil || application.ExitStatus(err) != 0 {
		t.Fatalf("the run ends ok, got %v", err)
	}
}

func TestDoctorRunBudgetIsReadFromTheInjectedClockAndSetsNoneByDefault(t *testing.T) {
	now := doctorNow
	a := slowOne("a", &now, application.DoctorOK)
	b := slowOne("b", &now, application.DoctorOK)
	doc := aDoctor(apptest.NewFakeDoctorState(), &apptest.FakeDoctorLog{}, &now, a, b)
	report, err := doc.Run(context.Background(), "", false)
	if err != nil || b.Probes() != 1 || report.Results[1].Verdict != "ok" {
		t.Fatalf("no RunBudget leaves every check worked: %v %v", report.Results, err)
	}
}

func TestDoctorRunBudgetSpentSaysSoOnTheDampersLineAndReadsNothing(t *testing.T) {
	now := doctorNow
	a := slowOne("a", &now, application.DoctorOK)
	doc := aDoctor(apptest.NewFakeDoctorState(), &apptest.FakeDoctorLog{}, &now, a)
	doc.RunBudget = time.Minute
	blocked := blockedDampers{release: make(chan struct{})}
	defer close(blocked.release)
	doc.Dampers = blocked
	var out strings.Builder
	doc.Out = &out
	if _, err := doc.Run(context.Background(), "", false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "dampers: could not be read (took over 60s: the run's budget is spent)") {
		t.Errorf("the dampers line says why:\n%s", out.String())
	}
}
