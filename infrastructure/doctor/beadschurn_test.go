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
	// 5 minutes, 1250 connections: 250 a minute, a busy host's normal
	// (mw-gq6.825).
	if verdict, reason := r.probe(5, 1250); verdict != application.DoctorOK {
		t.Fatalf("expected ok at 250 a minute, got %s (%s)", verdict, reason)
	}
}

// The limit is the host's to set ([doctor] beads_churn_per_minute); a Limit
// of 100 alarms at 250 a minute, and the default does not.
func TestBeadsChurnLimitIsTheOneItWasGiven(t *testing.T) {
	r := newChurnRun(t)
	r.check.Limit = 100
	r.probe(0, 0)
	verdict, reason := r.probe(5, 1250)
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "past 100") {
		t.Fatalf("expected faulty past 100, got %s (%s)", verdict, reason)
	}
}

// The fault names the parents of the bd processes the sampler saw, the five
// most common, as the Millhand found them by hand (mw-gq6.825).
func TestBeadsChurnFaultNamesTheTopClients(t *testing.T) {
	r := newChurnRun(t)
	r.check.Clients = func(context.Context) ([]doctor.ChurnClient, error) {
		return []doctor.ChurnClient{
			{Command: "mw dispatch", Count: 66}, {Command: "mw postern bead", Count: 24},
			{Command: "mw seat heartbeat", Count: 12}, {Command: "mw next", Count: 9},
			{Command: "mw status", Count: 5}, {Command: "mw land", Count: 2}, {Command: "cron", Count: 1},
		}, nil
	}
	r.probe(0, 0)
	verdict, reason := r.probe(5, 2000)
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
	}
	for _, want := range []string{"mw dispatch 66, mw postern bead 24, mw seat heartbeat 12, mw next 9, mw status 5"} {
		if !strings.Contains(reason, want) {
			t.Fatalf("expected the reason to name %q, got %q", want, reason)
		}
	}
	if strings.Contains(reason, "mw land") || strings.Contains(reason, "cron") {
		t.Fatalf("expected only the top five, got %q", reason)
	}
	if strings.Contains(reason, "no single client") {
		t.Fatalf("expected a lead of 66 of 119 to be a client, got %q", reason)
	}
}

// With no client ahead of the rest, or none seen, the fault says so.
func TestBeadsChurnFaultSaysWhenThereIsNoSingleClient(t *testing.T) {
	spread := []doctor.ChurnClient{{Command: "a", Count: 10}, {Command: "b", Count: 10}, {Command: "c", Count: 10}}
	for name, sampled := range map[string][]doctor.ChurnClient{"spread": spread, "none": nil} {
		r := newChurnRun(t)
		r.check.Clients = func(context.Context) ([]doctor.ChurnClient, error) { return sampled, nil }
		r.probe(0, 0)
		verdict, reason := r.probe(5, 2000)
		if verdict != application.DoctorFaulty || !strings.Contains(reason, "no single client") {
			t.Fatalf("%s: expected faulty with no single client, got %s (%s)", name, verdict, reason)
		}
	}
}

// A sampler that fails does not hide the fault.
func TestBeadsChurnFaultStandsWhenTheSamplerFails(t *testing.T) {
	r := newChurnRun(t)
	r.check.Clients = func(context.Context) ([]doctor.ChurnClient, error) { return nil, errors.New("ps: not found") }
	r.probe(0, 0)
	verdict, reason := r.probe(5, 2000)
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "ps: not found") {
		t.Fatalf("expected faulty naming the sampler's failure, got %s (%s)", verdict, reason)
	}
}

// ps lists a bd by its parent: the sampler counts each bd pid once over the
// window and names the parent's command.
func TestBdParentsCountsEachBdOnceByItsParentsCommand(t *testing.T) {
	const parents = "10 1 /usr/bin/mw dispatch --vault /v\n20 1 /bin/bash /opt/seat-loop.sh\n"
	rounds := []string{
		parents + "11 10 bd -C /v list\n12 10 bd -C /v show x\n21 20 /usr/local/bin/bd ready\n",
		parents + "11 10 bd -C /v list\n13 10 bd -C /v list\n",
	}
	round := 0
	ps := func(context.Context) (string, error) {
		out := rounds[round]
		round++
		return out, nil
	}
	sampler := &doctor.BdParents{Ps: ps, Rounds: 2, Wait: func(context.Context, time.Duration) {}}
	got, err := sampler.Sample(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []doctor.ChurnClient{{Command: "mw dispatch", Count: 3}, {Command: "seat-loop.sh", Count: 1}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("expected %v, got %v", want, got)
	}
}
