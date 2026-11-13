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
