package application_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// A springRig is an EventSpring with the dispatch and the Millhand's tick as
// its jobs, each run counted and held until the test lets it finish.
type springRig struct {
	log    *apptest.FakeEventLog
	spring *application.EventSpring
	now    time.Time

	mu      sync.Mutex
	runs    map[string]int
	release map[string]chan struct{}
	fail    map[string]error
	ran     chan string
}

func newSpringRig(t *testing.T) *springRig {
	t.Helper()
	r := &springRig{
		log:     &apptest.FakeEventLog{},
		now:     time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC),
		runs:    map[string]int{},
		release: map[string]chan struct{}{"dispatch": make(chan struct{}, 8), "millhand-tick": make(chan struct{}, 8)},
		fail:    map[string]error{},
		ran:     make(chan string, 32),
	}
	run := func(name string) func(context.Context) error {
		return func(context.Context) error {
			r.mu.Lock()
			r.runs[name]++
			r.mu.Unlock()
			r.ran <- name
			<-r.release[name]
			r.mu.Lock()
			defer r.mu.Unlock()
			return r.fail[name]
		}
	}
	r.spring = &application.EventSpring{
		Log:  r.log,
		Host: "laptop",
		Jobs: []application.SpringJob{
			application.DispatchJob(time.Hour, run("dispatch")),
			application.MillhandTickJob(time.Hour, run("millhand-tick")),
		},
		Now: func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.now },
	}
	return r
}

func (r *springRig) advance(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.now = r.now.Add(d)
}

func (r *springRig) count(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runs[name]
}

// started waits for a run of name to begin.
func (r *springRig) started(t *testing.T, name string) {
	t.Helper()
	select {
	case got := <-r.ran:
		if got != name {
			t.Fatalf("%s began, want %s", got, name)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not begin", name)
	}
}

// finish lets the held run of name end and waits for every run to be over.
func (r *springRig) finish(name string) {
	r.release[name] <- struct{}{}
	r.spring.Wait()
}

func (r *springRig) append(t *testing.T, evs ...events.Event) {
	t.Helper()
	for i := range evs {
		evs[i].Ts = r.now
		evs[i].Lane = events.LaneNormal
	}
	if _, err := r.log.Append(context.Background(), evs); err != nil {
		t.Fatal(err)
	}
}

func beadMoved(bead, from, to string) events.Event {
	return events.Event{Kind: events.KindBeadChanged, Bead: bead, Actor: "mayor", From: from, To: to, Detail: "status"}
}

// jobWords is the job events in the log, as actor from>to.
func (r *springRig) jobWords() []string {
	var out []string
	for _, e := range r.log.All() {
		if e.Kind == events.KindJob {
			out = append(out, e.Actor+" "+e.From+">"+e.To)
		}
	}
	return out
}

func (r *springRig) spin(t *testing.T) {
	t.Helper()
	if err := r.spring.Spring(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSpringHistoryIsNotReplayedAndAnIdleLogRunsNothing(t *testing.T) {
	r := newSpringRig(t)
	r.append(t, beadMoved("mw-a", events.Start, events.BeadOpen))
	r.spin(t) // the first look: where the log stands, no more
	r.spin(t)
	r.spring.Wait()
	if r.count("dispatch") != 0 || r.count("millhand-tick") != 0 {
		t.Fatalf("ran %d dispatches and %d ticks for history", r.count("dispatch"), r.count("millhand-tick"))
	}
}

func TestSpringABeadOpenedRunsOneDispatchAndASecondEventDuringItDoesNotStartAnother(t *testing.T) {
	r := newSpringRig(t)
	r.spin(t)
	r.append(t, beadMoved("mw-a", events.Start, events.BeadOpen))
	r.spin(t)
	r.started(t, "dispatch")

	// A second bead opens while the pass is in flight: nothing starts.
	r.append(t, beadMoved("mw-b", events.BeadHeld, events.BeadOpen))
	r.spin(t)
	r.spin(t)
	if got := r.count("dispatch"); got != 1 {
		t.Fatalf("%d dispatch passes began while one was in flight, want 1", got)
	}

	// It is not lost: one pass more follows the first, never beside it.
	r.release["dispatch"] <- struct{}{}
	r.started(t, "dispatch")
	r.finish("dispatch")
	if got := r.count("dispatch"); got != 2 {
		t.Fatalf("%d dispatch passes in all, want 2 (the second event's, after the first)", got)
	}
}

func TestSpringALandedBeadRunsADispatchButAClaimOrAStepBeadDoesNot(t *testing.T) {
	r := newSpringRig(t)
	r.spin(t)
	r.append(t,
		beadMoved("mw-a", events.BeadOpen, events.BeadClaimed),
		beadMoved("mw-mol-xyz", events.Start, events.BeadOpen),
		events.Event{Kind: events.KindBeadChanged, Bead: "mw-a", Actor: "mayor", From: events.BeadOpen, To: events.BeadOpen, Detail: "comment"},
	)
	r.spin(t)
	r.spring.Wait()
	if got := r.count("dispatch"); got != 0 {
		t.Fatalf("a claim, a step bead and a comment ran %d dispatches", got)
	}
	r.append(t, beadMoved("mw-a", events.BeadRunning, events.BeadLanded))
	r.spin(t)
	r.started(t, "dispatch")
	r.finish("dispatch")
}

func TestSpringAnAlarmRunsTheMillhandTickAndNotTheDispatch(t *testing.T) {
	r := newSpringRig(t)
	r.spin(t)
	// An event in the normal lane is no alarm; the emergency lane is.
	alarm := events.Event{Kind: events.KindMessage, Actor: "mayor-stale@laptop", Detail: "the Mayor is stale"}
	r.append(t, alarm)
	r.spin(t)
	r.spring.Wait()
	if r.count("millhand-tick") != 0 {
		t.Fatal("a normal-lane event ran the tick")
	}
	emergency := alarm
	emergency.Ts, emergency.Lane = r.now, events.LaneEmergency
	if _, err := r.log.Append(context.Background(), []events.Event{emergency}); err != nil {
		t.Fatal(err)
	}
	r.spin(t)
	r.started(t, "millhand-tick")
	r.finish("millhand-tick")
	if r.count("dispatch") != 0 {
		t.Fatalf("an alarm ran %d dispatches", r.count("dispatch"))
	}
}

func TestSpringMailForTheMillhandAndADoctorFailureRunTheTick(t *testing.T) {
	r := newSpringRig(t)
	r.spin(t)
	r.append(t, events.Event{Kind: events.KindMail, Bead: "mw-m1", Actor: "mayor", Detail: "mayor"})
	r.spin(t)
	r.spring.Wait()
	if r.count("millhand-tick") != 0 {
		t.Fatal("mail for the mayor ran the Millhand's tick")
	}
	r.append(t, events.Event{Kind: events.KindMail, Bead: "mw-m2", Actor: "mayor", Detail: "millhand"})
	r.spin(t)
	r.started(t, "millhand-tick")
	r.finish("millhand-tick")
	r.append(t, events.Event{Kind: events.KindJob, Actor: "doctor@laptop", From: events.Start, To: events.JobScheduled},
		events.Event{Kind: events.KindJob, Actor: "doctor@laptop", From: events.JobScheduled, To: events.JobRunning},
		events.Event{Kind: events.KindJob, Actor: "doctor@laptop", From: events.JobRunning, To: events.JobFailed, Detail: "a check failed"})
	r.spin(t)
	r.started(t, "millhand-tick")
	r.finish("millhand-tick")
}

func TestSpringTheHeartbeatRunsEachJobWhenNothingHasForAnHour(t *testing.T) {
	r := newSpringRig(t)
	r.spin(t)
	r.advance(59 * time.Minute)
	r.spin(t)
	r.spring.Wait()
	if r.count("dispatch")+r.count("millhand-tick") != 0 {
		t.Fatal("a pass ran before the hour was up")
	}
	r.advance(2 * time.Minute)
	r.spin(t)
	got := []string{<-r.ran, <-r.ran}
	if got[0] == got[1] {
		t.Fatalf("the heartbeat began %v, want one of each", got)
	}
	r.release["dispatch"] <- struct{}{}
	r.release["millhand-tick"] <- struct{}{}
	r.spring.Wait()

	// The clock starts again from a pass: an hour of nothing, not before.
	r.advance(30 * time.Minute)
	r.spin(t)
	r.spring.Wait()
	if r.count("dispatch") != 1 {
		t.Fatalf("%d dispatches half an hour after a heartbeat, want still 1", r.count("dispatch"))
	}
	for _, e := range r.log.All() {
		if e.Kind == events.KindJob && e.To == events.JobScheduled && e.Detail != "heartbeat" {
			t.Fatalf("a heartbeat pass was scheduled for %q", e.Detail)
		}
	}
}

func TestSpringEveryPassSaysScheduledRunningAndDoneOrFailed(t *testing.T) {
	r := newSpringRig(t)
	r.spin(t)
	r.append(t, beadMoved("mw-a", events.Start, events.BeadOpen))
	r.spin(t)
	r.started(t, "dispatch")
	r.finish("dispatch")

	r.mu.Lock()
	r.fail["dispatch"] = errors.New("exit status 1")
	r.mu.Unlock()
	r.append(t, beadMoved("mw-a", events.BeadRunning, events.BeadLanded))
	r.spin(t)
	r.started(t, "dispatch")
	r.finish("dispatch")

	want := []string{
		"dispatch@laptop >scheduled", "dispatch@laptop scheduled>running", "dispatch@laptop running>done",
		"dispatch@laptop done>scheduled", "dispatch@laptop scheduled>running", "dispatch@laptop running>failed",
	}
	got := r.jobWords()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("job events:\n got %q\nwant %q", got, want)
	}
	var scheduled []string
	for _, e := range r.log.All() {
		if e.Kind == events.KindJob && e.To == events.JobScheduled {
			scheduled = append(scheduled, e.Detail)
		}
	}
	if strings.Join(scheduled, "|") != "bead mw-a opened|bead mw-a landed" {
		t.Fatalf("the passes were scheduled for %q", scheduled)
	}
	last := r.log.All()[len(r.log.All())-1]
	if last.To != events.JobFailed || last.Detail != "exit status 1" {
		t.Fatalf("the last job event is %s with detail %q", last.To, last.Detail)
	}
}

func TestSpringAClockJobRunsOnItsOwnClockAndOnNoEvent(t *testing.T) {
	r := newSpringRig(t)
	runs := 0
	r.spring.Jobs = []application.SpringJob{application.ClockJob("mail-notify", 5*time.Minute, func(context.Context) error { runs++; return nil })}
	r.spin(t)
	r.append(t, beadMoved("mw-a", events.Start, events.BeadOpen))
	r.spin(t)
	r.spring.Wait()
	if runs != 0 {
		t.Fatal("an event ran the clock job")
	}
	r.advance(5 * time.Minute)
	r.spin(t)
	r.spring.Wait()
	if runs != 1 {
		t.Fatalf("the clock job ran %d times after its five minutes, want 1", runs)
	}
}

func TestSpringAFailureToWriteAJobEventIsSaidAndTheJobStillRuns(t *testing.T) {
	r := newSpringRig(t)
	var errs strings.Builder
	r.spring.Err = &errs
	r.spin(t)
	r.append(t, beadMoved("mw-a", events.Start, events.BeadOpen))
	r.log.FailNext(errors.New("disk full"))
	r.spin(t)
	r.started(t, "dispatch")
	r.finish("dispatch")
	if !strings.Contains(errs.String(), "disk full") {
		t.Fatalf("the failure was not said: %q", errs.String())
	}
}

// The follower is restarted after a millwright landing, and the systemctl it
// had running to start the dispatch unit is terminated with it (mw-gq6.242):
// that is a pass cut short, not a failed job.
const terminatedByRestart = "starting mw-dispatch.service: signal: terminated"

func (r *springRig) failWith(name string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fail[name] = err
}

func (r *springRig) lastJobEvent(t *testing.T) events.Event {
	t.Helper()
	var last events.Event
	for _, e := range r.log.All() {
		if e.Kind == events.KindJob {
			last = e
		}
	}
	return last
}

func TestSpringAPassTerminatedByTheFollowersRestartIsCutShortNotFailed(t *testing.T) {
	r := newSpringRig(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	if err := r.spring.Spring(ctx); err != nil {
		t.Fatal(err)
	}
	r.append(t, beadMoved("mw-a", events.BeadRunning, events.BeadLanded))
	if err := r.spring.Spring(ctx); err != nil {
		t.Fatal(err)
	}
	r.started(t, "dispatch")
	r.failWith("dispatch", errors.New(terminatedByRestart))
	stop() // the follower is told to stop, and its systemctl ends with it
	r.finish("dispatch")

	for _, e := range r.log.All() {
		if e.Kind == events.KindJob && e.To == events.JobFailed {
			t.Fatalf("a restart wrote a failed job: %q", e.Detail)
		}
	}
	last := r.lastJobEvent(t)
	if last.To != events.JobDone || last.Detail != application.SpringInterruptedDetail {
		t.Fatalf("the last job event is %s with detail %q, want done and the interruption", last.To, last.Detail)
	}
}

func TestSpringAPassThatEndsAFewMomentsBeforeTheStopIsSeenIsStillCutShort(t *testing.T) {
	r := newSpringRig(t)
	r.spring.Settle = 5 * time.Second
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	if err := r.spring.Spring(ctx); err != nil {
		t.Fatal(err)
	}
	r.append(t, beadMoved("mw-a", events.BeadRunning, events.BeadLanded))
	if err := r.spring.Spring(ctx); err != nil {
		t.Fatal(err)
	}
	r.started(t, "dispatch")
	// systemd terminates the systemctl a beat before the follower hears SIGTERM.
	r.failWith("dispatch", errors.New(terminatedByRestart))
	time.AfterFunc(50*time.Millisecond, stop)
	r.finish("dispatch")

	if last := r.lastJobEvent(t); last.To != events.JobDone || last.Detail != application.SpringInterruptedDetail {
		t.Fatalf("the last job event is %s with detail %q, want done and the interruption", last.To, last.Detail)
	}
}

func TestSpringAPassThatFailsWhileTheFollowerKeepsRunningStillFails(t *testing.T) {
	r := newSpringRig(t)
	r.spring.Settle = 20 * time.Millisecond
	r.spin(t)
	r.append(t, beadMoved("mw-a", events.BeadRunning, events.BeadLanded))
	r.spin(t)
	r.started(t, "dispatch")
	r.failWith("dispatch", errors.New("exit status 1"))
	r.finish("dispatch")

	if last := r.lastJobEvent(t); last.To != events.JobFailed || last.Detail != "exit status 1" {
		t.Fatalf("the last job event is %s with detail %q, want failed", last.To, last.Detail)
	}
}

// successor is the follower the restart brings up: a new EventSpring over the
// same log and jobs.
func (r *springRig) successor() *application.EventSpring {
	return &application.EventSpring{Log: r.log, Host: "laptop", Jobs: r.spring.Jobs, Now: r.spring.Now}
}

func TestSpringTheFollowerThatComesBackRunsAgainThePassItsPredecessorWasCutShortInOnce(t *testing.T) {
	r := newSpringRig(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	r.spring.Spring(ctx)
	r.append(t, beadMoved("mw-a", events.BeadRunning, events.BeadLanded))
	r.spring.Spring(ctx)
	r.started(t, "dispatch")
	r.failWith("dispatch", errors.New(terminatedByRestart))
	stop()
	r.finish("dispatch")
	r.failWith("dispatch", nil)

	next := r.successor()
	if err := next.Spring(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.started(t, "dispatch")
	r.release["dispatch"] <- struct{}{}
	next.Wait()
	if err := next.Spring(context.Background()); err != nil {
		t.Fatal(err)
	}
	next.Wait()

	if got := r.count("dispatch"); got != 2 {
		t.Fatalf("the dispatch ran %d times, want the cut-short pass and one more", got)
	}
	var scheduled []string
	for _, e := range r.log.All() {
		if e.Kind == events.KindJob && e.To == events.JobScheduled {
			scheduled = append(scheduled, e.Detail)
		}
	}
	if len(scheduled) != 2 || scheduled[1] != application.SpringRestartedReason {
		t.Fatalf("the passes were scheduled for %q", scheduled)
	}
	if last := r.lastJobEvent(t); last.To != events.JobDone || last.Detail != "" {
		t.Fatalf("the last job event is %s with detail %q, want a plain done", last.To, last.Detail)
	}
}

func TestSpringTheFollowerThatComesBackRunsNothingAfterAPassThatEndedOrFailed(t *testing.T) {
	r := newSpringRig(t)
	r.spin(t)
	r.append(t, beadMoved("mw-a", events.BeadRunning, events.BeadLanded))
	r.spin(t)
	r.started(t, "dispatch")
	r.failWith("dispatch", errors.New("exit status 1"))
	r.finish("dispatch")

	if err := r.successor().Spring(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case name := <-r.ran:
		t.Fatalf("%s was run for a failure the log already says", name)
	case <-time.After(100 * time.Millisecond):
	}
}
