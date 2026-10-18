package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// jobEventsOf is the job events of actor in the log, oldest first.
func jobEventsOf(log *apptest.FakeEventLog, actor string) []events.Event {
	var out []events.Event
	for _, e := range log.All() {
		if e.Kind == events.KindJob && e.Actor == actor {
			out = append(out, e)
		}
	}
	return out
}

// mw-gq6.590: the pass of 2026-10-17 was cut short by the follower stopping,
// and the pass run again by the follower that came back went running->done in
// no time and made nothing, because the first pass had marked the day begun.
func TestASleepPassCutShortByTheFollowerIsRunInFullByTheOneThatComesBack(t *testing.T) {
	notes := &sleepNotes{kv: map[string]string{}}
	log := &apptest.FakeEventLog{}
	now := time.Date(2026, 10, 17, 7, 30, 11, 0, time.UTC)
	made := 0
	newSpring := func(run func(context.Context) error) *application.EventSpring {
		nightly := &application.SleepNightly{
			Hour: 7, Minute: 30, Notes: notes, Run: run,
			Home: func(context.Context) (bool, error) { return true, nil },
			Now:  func() time.Time { return now },
		}
		return &application.EventSpring{Log: log, Host: "laptop", Jobs: []application.SpringJob{nightly.Job()}, Now: func() time.Time { return now }}
	}

	// The first follower: the pass begins, and the follower is stopped in it.
	began := make(chan struct{})
	first := newSpring(func(ctx context.Context) error {
		close(began)
		<-ctx.Done()
		return ctx.Err()
	})
	ctx, stop := context.WithCancel(context.Background())
	if err := first.Spring(ctx); err != nil { // takes its bearings
		t.Fatal(err)
	}
	if err := first.Spring(ctx); err != nil { // the pass is due
		t.Fatal(err)
	}
	<-began
	stop()
	first.Wait()
	cut := jobEventsOf(log, "sleep@laptop")
	if last := cut[len(cut)-1]; last.To != events.JobDone || last.Detail != application.SpringInterruptedDetail {
		t.Fatalf("the cut-short pass ended %s %q, want done and the interruption", last.To, last.Detail)
	}

	// The follower that comes back runs it again, and the pass is made.
	second := newSpring(func(context.Context) error { made++; return nil })
	if err := second.Spring(context.Background()); err != nil {
		t.Fatal(err)
	}
	second.Wait()
	if made != 1 {
		t.Fatalf("the re-run after the restart made the pass %d times, want 1", made)
	}
}

// A pass that ends without making anything says why on its done event.
func TestAJobThatIsNotDueWhenRunSaysWhyOnItsDoneEvent(t *testing.T) {
	notes := &sleepNotes{kv: map[string]string{application.SleepDayKey: "2026-10-17"}}
	log := &apptest.FakeEventLog{}
	now := time.Date(2026, 10, 17, 7, 30, 11, 0, time.UTC)
	nightly := &application.SleepNightly{
		Hour: 7, Minute: 30, Notes: notes,
		Run:  func(context.Context) error { t.Error("the pass ran though the day is marked"); return nil },
		Home: func(context.Context) (bool, error) { return true, nil },
		Now:  func() time.Time { return now },
	}
	// The log says the last pass was cut short, so the first Spring runs it again.
	log.Append(context.Background(), []events.Event{{Kind: events.KindJob, Actor: "sleep@laptop", From: events.JobRunning, To: events.JobDone, Detail: application.SpringInterruptedDetail, Ts: now, Lane: events.LaneNormal}})
	spring := &application.EventSpring{Log: log, Host: "laptop", Jobs: []application.SpringJob{nightly.Job()}, Now: func() time.Time { return now }}
	if err := spring.Spring(context.Background()); err != nil {
		t.Fatal(err)
	}
	spring.Wait()
	evs := jobEventsOf(log, "sleep@laptop")
	last := evs[len(evs)-1]
	if last.To != events.JobDone || strings.TrimSpace(last.Detail) == "" {
		t.Fatalf("a pass that made nothing ended %s with detail %q, want done and the reason", last.To, last.Detail)
	}
}

// The weekly digest marks its day the same way, and is run again in full after a
// stop that cut it short; a pass that finds the day marked says so.
func TestADigestCutShortByTheFollowerIsSentByTheOneThatComesBack(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	notes := apptest.NewFakeTracker()
	now := time.Date(2026, 10, 19, 11, 5, 0, 0, time.UTC) // a Monday
	sends := 0
	digest := weeklyDigest(notes, &now, true, &sends)
	digest.Send = func(ctx context.Context) error { stop(); return ctx.Err() }
	job := digest.Job()
	if err := job.Run(ctx); err == nil {
		t.Fatal("want the cut-short error")
	}
	digest.Send = func(context.Context) error { sends++; return nil }
	if err := job.Run(context.Background()); err != nil || sends != 1 {
		t.Fatalf("the re-run: %v, sends = %d, want the digest sent", err, sends)
	}
	err := job.Run(context.Background())
	var skipped *application.SkippedPassError
	if !errors.As(err, &skipped) || sends != 1 {
		t.Fatalf("a third run the same day: %v, sends = %d, want it skipped with a reason", err, sends)
	}
}

// madeOrPassedOver is err from a once-a-day job's Run, with the error of a pass
// that found it was not due (and said why) taken as no error.
func madeOrPassedOver(err error) error {
	var skipped *application.SkippedPassError
	if errors.As(err, &skipped) {
		return nil
	}
	return err
}
