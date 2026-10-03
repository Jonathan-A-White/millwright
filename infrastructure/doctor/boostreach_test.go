package doctor_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// clearCall is one call of a check's Clear: its text and the seq it clears.
type clearCall struct {
	text   string
	clears uint64
}

// boostRig is the boost-reach check over a real Store in a temp dir. Each
// run builds the check afresh, as each timer run of mw doctor does, so what
// carries over is only what the Store kept.
type boostRig struct {
	t      *testing.T
	store  *doctor.Store
	now    time.Time
	up     bool
	nextEm uint64
	alarms []string
	clears []clearCall
}

func newBoostRig(t *testing.T) *boostRig {
	t.Helper()
	return &boostRig{t: t, store: doctor.New(t.TempDir()), now: time.Date(2026, 10, 3, 3, 22, 0, 0, time.UTC), nextEm: 8160}
}

// run lets minutes go by and runs a fresh boost-reach check once.
func (r *boostRig) run(minutes int) application.DoctorResult {
	r.t.Helper()
	r.now = r.now.Add(time.Duration(minutes) * time.Minute)
	check := doctor.NewBoostReach(&apptest.FakeHomeFile{Text: "laptop 2026-09-29T00:10:00Z mw@laptop"}, "laptop", map[string]string{"desktop": "ssh desktop"}, r.store)
	check.Now = func() time.Time { return r.now }
	check.Ssh = func(context.Context, []string) error {
		if r.up {
			return nil
		}
		return errors.New("ssh: timed out")
	}
	check.Alarm = func(_ context.Context, text string) (uint64, error) {
		r.alarms = append(r.alarms, text)
		r.nextEm++
		return r.nextEm, nil
	}
	check.Clear = func(_ context.Context, text string, clears uint64) error {
		r.clears = append(r.clears, clearCall{text, clears})
		return nil
	}
	report, err := application.Doctor{
		Checks: application.DoctorChecks{check}, State: r.store, Log: r.store, Host: "laptop",
		Now: func() time.Time { return r.now },
	}.Run(context.Background(), "", false)
	if err != nil {
		if _, fault := application.DoctorFaults(err); !fault {
			r.t.Fatalf("doctor run: %v", err)
		}
	}
	if len(report.Results) != 1 {
		r.t.Fatalf("expected one result, got %d", len(report.Results))
	}
	return report.Results[0]
}

// goDown runs the check until it has alarmed the Boost is down.
func (r *boostRig) goDown() {
	r.t.Helper()
	r.up = false
	r.run(0)
	r.run(31)
	if len(r.alarms) != 1 {
		r.t.Fatalf("expected the down alarm, got %q", r.alarms)
	}
}

func TestBoostBackClearsTheSeqOfTheEmergencyItAnswers(t *testing.T) {
	r := newBoostRig(t)
	r.goDown()

	r.up = true
	r.run(5)
	if len(r.clears) != 1 || r.clears[0].clears != 8161 || !strings.Contains(r.clears[0].text, "answers again (down 2026-10-03 03:22 to 2026-10-03 03:58 UTC)") {
		t.Fatalf("expected one clear of 8161 saying the Boost answers again, got %+v", r.clears)
	}
	if len(r.alarms) != 1 {
		t.Errorf("the back must not raise a second emergency, got %q", r.alarms)
	}

	r.run(5)
	if len(r.clears) != 1 || len(r.alarms) != 1 {
		t.Errorf("the outage is over: expected no more events, got alarms %q, clears %+v", r.alarms, r.clears)
	}
}

func TestBoostRecordedSeqSurvivesAFreshDoctorRun(t *testing.T) {
	r := newBoostRig(t)
	r.goDown()
	// The Boost stays down for more runs, each a fresh check over the same state.
	r.run(10)
	r.run(10)

	r.up = true
	r.run(1)
	if len(r.clears) != 1 || r.clears[0].clears != 8161 {
		t.Fatalf("expected the clear to name 8161 after fresh runs, got %+v", r.clears)
	}
}

func TestBoostBackWithNoRecordedEmergencyClearsNothing(t *testing.T) {
	r := newBoostRig(t)
	r.goDown()
	// A latch from before this change kept no seq; so does an alarm whose
	// emergency event could not be written (seq 0).
	err := r.store.Save(context.Background(), doctor.BoostReachSeenStateName, application.DoctorEpisode{FirstFaulty: r.now, SeenPaths: []string{doctor.BoostReachAlarmed}})
	if err != nil {
		t.Fatal(err)
	}

	r.up = true
	r.run(5)
	if len(r.clears) != 1 || r.clears[0].clears != 0 || !strings.Contains(r.clears[0].text, "answers again") {
		t.Fatalf("expected the back said with clears 0, got %+v", r.clears)
	}
}
