package doctor_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// churnRun is the beads-churn check over a counter the test moves and a clock
// the test sets, keeping its sample in a real doctor state directory.
type churnRun struct {
	t     *testing.T
	count int64
	err   error
	now   time.Time
	check *doctor.BeadsChurn
}

func newChurnRun(t *testing.T) *churnRun {
	r := &churnRun{t: t, count: 1_000_000, now: time.Date(2026, 10, 18, 6, 0, 0, 0, time.UTC)}
	r.check = doctor.NewBeadsChurn(
		func(context.Context) (int64, error) { return r.count, r.err },
		doctor.New(t.TempDir()),
	)
	r.check.Now = func() time.Time { return r.now }
	return r
}

// probe moves the clock by minutes and the counter by connections, then
// probes.
func (r *churnRun) probe(minutes float64, connections int64) (application.Verdict, string) {
	r.now = r.now.Add(time.Duration(minutes * float64(time.Minute)))
	r.count += connections
	return r.check.Probe(context.Background())
}

func TestBeadsChurnIsNamedBeadsChurn(t *testing.T) {
	if got := newChurnRun(t).check.Name(); got != "beads-churn" {
		t.Fatalf("expected the check to be called beads-churn, got %q", got)
	}
}

// The first run only takes a sample: a rate needs two.
func TestBeadsChurnFirstRunOnlyTakesASample(t *testing.T) {
	r := newChurnRun(t)
	if verdict, reason := r.probe(0, 0); verdict != application.DoctorOK {
		t.Fatalf("expected ok on the first run, got %s (%s)", verdict, reason)
	}
}

func TestBeadsChurnIsOKUnderTheLimit(t *testing.T) {
	r := newChurnRun(t)
	r.probe(0, 0)
	// 5 minutes, 500 connections: 100 a minute, the busiest the follower was
	// allowed to be.
	if verdict, reason := r.probe(5, 500); verdict != application.DoctorOK {
		t.Fatalf("expected ok at 100 a minute, got %s (%s)", verdict, reason)
	}
}

// 514 a minute is what the home's beads server answered on 2026-10-18
// (mw-gq6.596).
func TestBeadsChurnAlarmsOverTheLimitNamingTheRate(t *testing.T) {
	r := newChurnRun(t)
	r.probe(0, 0)
	verdict, reason := r.probe(5, 2570)
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty at 514 a minute, got %s (%s)", verdict, reason)
	}
	for _, want := range []string{"514", "120"} {
		if !strings.Contains(reason, want) {
			t.Fatalf("expected the reason to name %s, got %q", want, reason)
		}
	}
}

// Each run is judged against the one before it, not against the first: a
// burst that has ended is not an alarm that goes on.
func TestBeadsChurnJudgesTheLastStretchOnly(t *testing.T) {
	r := newChurnRun(t)
	r.probe(0, 0)
	if verdict, _ := r.probe(5, 2570); verdict != application.DoctorFaulty {
		t.Fatal("expected the burst to alarm")
	}
	if verdict, reason := r.probe(5, 100); verdict != application.DoctorOK {
		t.Fatalf("expected the quiet stretch after it to be ok, got %s (%s)", verdict, reason)
	}
}

// A server that restarted counts from zero again: that is no negative rate and
// no alarm.
func TestBeadsChurnReadsARestartedServerAsAFreshStart(t *testing.T) {
	r := newChurnRun(t)
	r.probe(0, 0)
	r.count = 40
	if verdict, reason := r.probe(5, 0); verdict != application.DoctorOK {
		t.Fatalf("expected ok after a restart, got %s (%s)", verdict, reason)
	}
}

// A sample from the day before says nothing of now.
func TestBeadsChurnIgnoresASampleOlderThanAnHour(t *testing.T) {
	r := newChurnRun(t)
	r.probe(0, 0)
	if verdict, reason := r.probe(24*60, 10_000_000); verdict != application.DoctorOK {
		t.Fatalf("expected a day-old sample not to be judged, got %s (%s)", verdict, reason)
	}
}

func TestBeadsChurnCannotTellWhenTheServerCannotBeAsked(t *testing.T) {
	r := newChurnRun(t)
	r.err = errors.New("connection refused")
	verdict, reason := r.probe(0, 0)
	if verdict != application.DoctorCannotTell || !strings.Contains(reason, "connection refused") {
		t.Fatalf("expected cannot-tell naming the failure, got %s (%s)", verdict, reason)
	}
}
