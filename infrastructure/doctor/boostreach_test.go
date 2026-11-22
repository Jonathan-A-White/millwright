package doctor_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
	// gaveBack is each Boost the check had its claims given back, in order;
	// giveBackErr is what the give-back fails with, when set.
	gaveBack    []string
	giveBackErr error
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
	check.GiveBack = func(_ context.Context, boost string) error {
		r.gaveBack = append(r.gaveBack, boost)
		return r.giveBackErr
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

// A real ssh whose remote command ends with the probe's write-failed status is
// the "cannot write" alarm; any other failure stays "has not answered".
func TestBoostRealSshExitOf73IsACannotWriteAlarm(t *testing.T) {
	for status, want := range map[string]string{"73": "answers ssh but cannot write its disk", "255": "has not answered ssh"} {
		dir := t.TempDir()
		stub := filepath.Join(dir, "ssh")
		if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit "+status+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		now := time.Date(2026, 10, 3, 3, 22, 0, 0, time.UTC)
		store := doctor.New(dir)
		var alarms []string
		run := func(at time.Time) {
			check := doctor.NewBoostReach(&apptest.FakeHomeFile{Text: "laptop 2026-09-29T00:10:00Z mw@laptop"}, "laptop", map[string]string{"desktop": stub + " desktop"}, store)
			check.Now = func() time.Time { return at }
			check.Alarm = func(_ context.Context, text string) (uint64, error) {
				alarms = append(alarms, text)
				return 1, nil
			}
			check.Probe(context.Background())
			if err := check.Cure(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
		run(now)
		run(now.Add(31 * time.Minute))
		if len(alarms) != 1 || !strings.Contains(alarms[0], want) {
			t.Errorf("exit %s: expected one alarm saying %q, got %q", status, want, alarms)
		}
	}
}

// mw-6ww.163.1: a Boost the Governor retired is not asked, and so not alarmed.
func TestBoostReachSkipsARetiredBoostWithoutAskingIt(t *testing.T) {
	r := newBoostRig(t)
	asked := false
	check := doctor.NewBoostReach(&apptest.FakeHomeFile{Text: "laptop 2026-09-29T00:10:00Z mw@laptop"}, "laptop", map[string]string{"desktop": "ssh desktop"}, r.store)
	check.Retired = []string{"desktop"}
	check.Ssh = func(context.Context, []string) error { asked = true; return errors.New("ssh: timed out") }
	check.Alarm = func(context.Context, string) (uint64, error) { r.alarms = append(r.alarms, "alarm"); return 1, nil }

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK || !strings.Contains(reason, "retired") {
		t.Fatalf("expected ok, saying the desktop is retired, got %s (%s)", verdict, reason)
	}
	if asked || len(r.alarms) != 0 {
		t.Fatalf("expected no ssh and no alarm for a retired Boost, asked %v, alarms %v", asked, r.alarms)
	}
}

// mw-6ww.185.10: the Boost's claims are given back once per outage, when the
// alarm that it has not answered for 30 minutes goes out.
func TestBoostDownGivesItsClaimsBackOncePerOutage(t *testing.T) {
	r := newBoostRig(t)
	r.up = false
	r.run(0)
	r.run(29)
	if len(r.gaveBack) != 0 {
		t.Fatalf("expected no give-back before the 30 minutes are up, got %v", r.gaveBack)
	}
	r.run(2)
	if len(r.alarms) != 1 || len(r.gaveBack) != 1 || r.gaveBack[0] != "desktop" {
		t.Fatalf("expected one give-back for desktop with the alarm, got alarms %q give-backs %v", r.alarms, r.gaveBack)
	}
	for i := 0; i < 5; i++ {
		r.run(10)
	}
	if len(r.gaveBack) != 1 {
		t.Errorf("expected the latch to keep the give-back to one for the outage, got %v", r.gaveBack)
	}

	// The Boost answers, goes down again, and that is a second outage.
	r.up = true
	r.run(5)
	r.up = false
	r.run(0)
	r.run(31)
	if len(r.gaveBack) != 2 {
		t.Errorf("expected the next outage to give back once more, got %v", r.gaveBack)
	}
}

func TestAGiveBackThatFailsDoesNotUnsendTheAlarmNorSendItAgain(t *testing.T) {
	r := newBoostRig(t)
	r.giveBackErr = errors.New("the tracker is down")
	r.up = false
	r.run(0)
	res := r.run(31)
	if len(r.alarms) != 1 || len(r.gaveBack) != 1 {
		t.Fatalf("expected the alarm sent and the give-back tried, got %q %v", r.alarms, r.gaveBack)
	}
	if !strings.Contains(res.Reason, "tracker is down") {
		t.Errorf("expected the failure said, got %+v", res)
	}
	r.run(2)
	if len(r.alarms) != 1 {
		t.Errorf("expected no second alarm, got %q", r.alarms)
	}
}

// The "down" alarm only tells the Governor: the Boost still does not answer, so
// the result is faulty, never cured. The "answers again" event follows a Boost
// that really is back, and is rightly cured (mw-6ww.185.33).
func TestBoostDownAlarmIsNeverCuredButAnswersAgainIs(t *testing.T) {
	r := newBoostRig(t)
	r.up = false
	r.run(0)
	down := r.run(31)
	if len(r.alarms) != 1 {
		t.Fatalf("expected the down alarm, got %q", r.alarms)
	}
	if down.Verdict != "faulty" && down.Verdict != "damped" {
		t.Errorf("the down alarm left the Boost down: expected faulty or damped, got %q (%s)", down.Verdict, down.String())
	}
	if strings.Contains(down.String(), "cured") {
		t.Errorf("the report says cured: %s", down.String())
	}

	r.up = true
	back := r.run(5)
	if len(r.clears) != 1 {
		t.Fatalf("expected the answers-again event, got %+v", r.clears)
	}
	if back.Verdict != "cured" {
		t.Errorf("a Boost that answers again is put right: expected cured, got %q (%s)", back.Verdict, back.String())
	}
}
