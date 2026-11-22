package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// longCheck takes 150 ms to probe, over the 50 ms budget a check gets.
type longCheck struct {
	apptest.FakeDoctorCheck
	budget time.Duration
}

func (l *longCheck) Probe(context.Context) (application.Verdict, string) {
	time.Sleep(150 * time.Millisecond)
	return application.DoctorOK, "sampled"
}

type longProbeCheck struct{ longCheck }

func (l *longProbeCheck) ProbeBudget() time.Duration { return l.budget }

// mw-6ww.185.12: the clock-steps sample takes 90 s, over a check's minute.
func TestACheckThatAsksForALongerProbeIsGivenIt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		check application.DoctorCheck
		want  string
	}{
		{"plain", &longCheck{FakeDoctorCheck: apptest.FakeDoctorCheck{CheckName: "plain"}}, "cannot-tell"},
		{"long", &longProbeCheck{longCheck{FakeDoctorCheck: apptest.FakeDoctorCheck{CheckName: "long"}, budget: 5 * time.Second}}, "ok"},
	} {
		now := doctorNow
		d := aDoctor(apptest.NewFakeDoctorState(), &apptest.FakeDoctorLog{}, &now, tc.check)
		d.CheckBudget = 50 * time.Millisecond
		report, err := d.Run(context.Background(), "", false)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := report.Results[0].Verdict; got != tc.want {
			t.Errorf("%s: verdict %q, want %q", tc.name, got, tc.want)
		}
	}
}
