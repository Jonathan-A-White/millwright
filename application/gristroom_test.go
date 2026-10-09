package application_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

var roomNow = time.Date(2026, 10, 9, 20, 0, 0, 0, time.UTC)

// grindEnding is a kept grind of kind that was answered ago before roomNow,
// waited queued seconds and ran run seconds.
func grindEnding(kind string, ago time.Duration, queued, run float64) application.GristRunTiming {
	answered := roomNow.Add(-ago)
	received := answered.Add(-time.Duration(run * float64(time.Second)))
	sent := received.Add(-time.Duration(queued * float64(time.Second)))
	return application.GristRunTiming{
		Txid: fmt.Sprintf("direct:%s-%d", kind, answered.UnixNano()), Kind: kind,
		Sent: &sent, Received: received, Answered: answered,
		QueuedSeconds: &queued, RunSeconds: run, Seconds: run,
	}
}

type roomRuns struct{ timings []application.GristRunTiming }

func (r roomRuns) Keep(context.Context, application.GristRun) error { return nil }
func (r roomRuns) List(context.Context, time.Time) ([]application.GristRunLine, error) {
	return nil, nil
}
func (r roomRuns) Timings(context.Context) ([]application.GristRunTiming, error) {
	return r.timings, nil
}

func roomOf(runs roomRuns, locks ...application.GristLock) application.GristRoom {
	return application.GristRoom{
		Runs: runs, Grinding: locks, Now: func() time.Time { return roomNow },
		Limits: application.GristRoomLimits{Cap: 4, RecentSeconds: 600, SlowFactor: 1.5},
	}
}

func pulseOf(t *testing.T, room application.GristRoom, hostCap int) application.GristPulse {
	t.Helper()
	pulse, err := room.Pulse(context.Background(), hostCap)
	if err != nil {
		t.Fatal(err)
	}
	return pulse
}

// steady is n grinds of kind, the earliest ending at start ago and one every
// minute after, each taking e2e seconds end to end (2 of them waiting).
func steady(kind string, n int, start time.Duration, e2e float64) []application.GristRunTiming {
	var out []application.GristRunTiming
	for i := 0; i < n; i++ {
		out = append(out, grindEnding(kind, start-time.Duration(i)*time.Minute, 2, e2e-2))
	}
	return out
}

func TestGristInFlightCapsNewStartsAtTheGristCap(t *testing.T) {
	lock := &apptest.FakeGristLock{}
	lock.Hold()
	pulse := pulseOf(t, roomOf(roomRuns{}, lock), 6)
	if !pulse.InFlight || !pulse.Active {
		t.Fatalf("a grind holding a slot is grist in flight: %+v", pulse)
	}
	if pulse.Cap != 4 {
		t.Errorf("the cap while grist is active is grist_cap 4, got %d", pulse.Cap)
	}
	if pulse.Slow != "" {
		t.Errorf("nothing is slow: %q", pulse.Slow)
	}
}

func TestGristFinishedWithinTheRecentWindowStillCaps(t *testing.T) {
	runs := roomRuns{steady("tutor-turn", 3, 9*time.Minute, 10)}
	pulse := pulseOf(t, roomOf(runs, &apptest.FakeGristLock{}), 6)
	if !pulse.Active || pulse.InFlight || pulse.Recent != 3 || pulse.Cap != 4 {
		t.Fatalf("three grinds ended in the last 10 minutes: %+v", pulse)
	}
}

func TestNoGristInTheRecentWindowLiftsTheCap(t *testing.T) {
	runs := roomRuns{steady("tutor-turn", 3, 20*time.Minute, 10)}
	pulse := pulseOf(t, roomOf(runs, &apptest.FakeGristLock{}), 6)
	if pulse.Active || pulse.Cap != 6 || pulse.Slow != "" {
		t.Fatalf("grist older than grist_recent_s lifts the cap back to the host's 6: %+v", pulse)
	}
}

func TestTheGristCapDefaultsToTheHostCapLessTwoButOne(t *testing.T) {
	for _, tt := range []struct{ hostCap, want int }{{6, 4}, {3, 1}, {2, 1}, {1, 1}} {
		room := roomOf(roomRuns{steady("tutor-turn", 1, time.Minute, 10)})
		room.Limits.Cap = 0
		if pulse := pulseOf(t, room, tt.hostCap); pulse.Cap != tt.want {
			t.Errorf("host cap %d: the grist cap is %d, want %d", tt.hostCap, pulse.Cap, tt.want)
		}
	}
	room := roomOf(roomRuns{steady("tutor-turn", 1, time.Minute, 10)})
	room.Limits.Cap = 9
	if pulse := pulseOf(t, room, 6); pulse.Cap != 6 {
		t.Errorf("a grist cap above the host's cap never raises it: %d", pulse.Cap)
	}
}

func TestSlowGristBlocksStartsAndRecoveryLiftsIt(t *testing.T) {
	// Forty-five grinds at 10 s, then five at 30 s: the median of the last
	// five is 30 s, three times the par of 10 s.
	old := steady("tutor-turn", 45, 100*time.Minute, 10)
	slow := steady("tutor-turn", 5, 9*time.Minute, 30)
	runs := roomRuns{append(append([]application.GristRunTiming{}, old...), slow...)}
	pulse := pulseOf(t, roomOf(runs), 6)
	if pulse.Slow == "" {
		t.Fatalf("answers at 3 x par start no story: %+v", pulse)
	}
	for _, want := range []string{"tutor-turn", "30 s", "10 s"} {
		if !strings.Contains(pulse.Slow, want) {
			t.Errorf("the reason %q should say %q", pulse.Slow, want)
		}
	}
	// Three quick answers join: the last five are 30, 30, 10, 10, 10.
	recovered := append(append([]application.GristRunTiming{}, runs.timings...), steady("tutor-turn", 3, 3*time.Minute, 10)...)
	if pulse := pulseOf(t, roomOf(roomRuns{recovered}), 6); pulse.Slow != "" {
		t.Errorf("answers back near par lift the block: %q", pulse.Slow)
	}
}

func TestSlowGristDoesNotBlockOnceNoneIsRecent(t *testing.T) {
	old := steady("tutor-turn", 45, 200*time.Minute, 10)
	slow := steady("tutor-turn", 5, 60*time.Minute, 30)
	runs := roomRuns{append(append([]application.GristRunTiming{}, old...), slow...)}
	if pulse := pulseOf(t, roomOf(runs), 6); pulse.Slow != "" || pulse.Active {
		t.Errorf("slow answers an hour ago are not the Governor waiting now: %+v", pulse)
	}
}

func TestSlowIsJudgedAtTheFactorTimesPar(t *testing.T) {
	old := steady("tutor-turn", 45, 100*time.Minute, 10)
	for factor, slow := range map[float64]bool{14.9: false, 15.1: true} {
		last := steady("tutor-turn", 5, 5*time.Minute, factor)
		runs := roomRuns{append(append([]application.GristRunTiming{}, old...), last...)}
		// The median of 50 with five high ones is still 10 s.
		if got := pulseOf(t, roomOf(runs), 6).Slow != ""; got != slow {
			t.Errorf("last five at %.1f s against a par of 10 s and a factor of 1.5: slow = %v, want %v", factor, got, slow)
		}
	}
}

func TestAKindWithTooFewGrindsFallsBackToAllGristForItsPar(t *testing.T) {
	// Forty fast grinds of one kind set the par of all grist at 10 s; a new
	// kind has five slow grinds of its own, too few to have a par.
	fast := steady("tutor-turn", 40, 100*time.Minute, 10)
	slow := steady("spell", 5, 5*time.Minute, 30)
	runs := roomRuns{append(append([]application.GristRunTiming{}, fast...), slow...)}
	pulse := pulseOf(t, roomOf(runs), 6)
	if pulse.Slow == "" || !strings.Contains(pulse.Slow, "spell") {
		t.Fatalf("the new kind is judged against all grist's par: %+v", pulse)
	}
	// With a par of its own (ten grinds, 30 s each) the same five are not slow.
	own := append(steady("spell", 10, 150*time.Minute, 30), slow...)
	runs = roomRuns{append(append([]application.GristRunTiming{}, fast...), own...)}
	if pulse := pulseOf(t, roomOf(runs), 6); pulse.Slow != "" {
		t.Errorf("a kind that usually takes 30 s is not slow at 30 s: %q", pulse.Slow)
	}
}

func TestTooFewGrindsAnywhereHaveNoParSoNothingIsSlow(t *testing.T) {
	runs := roomRuns{steady("tutor-turn", 5, 5*time.Minute, 300)}
	pulse := pulseOf(t, roomOf(runs), 6)
	if pulse.Slow != "" {
		t.Errorf("five grinds are no par: %q", pulse.Slow)
	}
	if !pulse.Active {
		t.Errorf("but they are recent: %+v", pulse)
	}
}

func TestAForwardedGristIsNotATutor(t *testing.T) {
	runs := roomRuns{steady(application.GristForwardKind, 3, time.Minute, 1)}
	pulse := pulseOf(t, roomOf(runs), 6)
	if pulse.Active || pulse.Cap != 6 {
		t.Errorf("a photo forwarded to the Mayor lowers no cap: %+v", pulse)
	}
}

func TestOlderRecordsWithoutQueuedAndRunSecondsAreJudgedByTheirTimes(t *testing.T) {
	var timings []application.GristRunTiming
	for _, t := range steady("tutor-turn", 50, 100*time.Minute, 10) {
		t.QueuedSeconds, t.RunSeconds = nil, 0
		timings = append(timings, t)
	}
	for _, t := range steady("tutor-turn", 5, 5*time.Minute, 30) {
		t.QueuedSeconds, t.RunSeconds = nil, 0
		timings = append(timings, t)
	}
	if pulse := pulseOf(t, roomOf(roomRuns{timings}), 6); pulse.Slow == "" {
		t.Errorf("sent to answered is the end to end time when queued_s and run_s are not there: %+v", pulse)
	}
}

func TestThePulseNamesTheMedianAndParForTheStatusLine(t *testing.T) {
	old := steady("tutor-turn", 45, 100*time.Minute, 11)
	last := steady("tutor-turn", 3, 5*time.Minute, 14)
	pulse := pulseOf(t, roomOf(roomRuns{append(old, last...)}), 6)
	if got, want := pulse.Line(), "grist: active (3 in 10 min), median 14 s (par 11 s)"; got != want {
		t.Errorf("line = %q, want %q", got, want)
	}
	if line := (application.GristPulse{}).Line(); line != "" {
		t.Errorf("a pulse that read nothing says nothing: %q", line)
	}
}

func TestAHostThatCannotReadItsRunsIsNotHeldBack(t *testing.T) {
	room := roomOf(roomRuns{})
	room.Runs = nil
	pulse, err := room.Pulse(context.Background(), 6)
	if err != nil || pulse.Active || pulse.Cap != 6 {
		t.Errorf("with nowhere kept, grist is not active: %+v %v", pulse, err)
	}
}
