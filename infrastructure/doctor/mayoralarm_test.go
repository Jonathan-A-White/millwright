package doctor_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// mw-gq6.1647: a Mayor cure cut by the run's end still sends its alarm, under a
// ctx that has not ended: the Governor is told what the cure did and could not do.

// alarmSeen is what an Alarm stand-in saw: whether its ctx had ended when it was
// called, and the text it was given.
type alarmSeen struct {
	ended bool
	text  string
}

func recordAlarm(into *[]alarmSeen) func(context.Context, string) (uint64, error) {
	return func(ctx context.Context, text string) (uint64, error) {
		*into = append(*into, alarmSeen{ended: ctx.Err() != nil, text: text})
		return 301, nil
	}
}

func slowMayorUp(r *staleRig) {
	mayorUp := filepath.Join(r.vault, "bin", "mayor-up")
	r.write(mayorUp, fmt.Sprintf("#!/bin/sh\necho started >> '%s'\nexec sleep 30\n", mayorUpStarted(r)))
	r.chmod(mayorUp)
}

// mayorUpStarted is the file slowMayorUp's bin/mayor-up writes once it runs.
func mayorUpStarted(r *staleRig) string {
	return filepath.Join(filepath.Dir(r.upRuns), "mayor-up-started")
}

// cutWhenMayorUpStarts is a ctx that ends once slowMayorUp's bin/mayor-up is
// running: the cut the cure tests mean to make, waited on as a condition. A
// clock short enough to cut bin/mayor-up is short enough, on a loaded host, to
// cut the tmux calls before it (mw-gq6.1753); the minute is only a backstop.
func cutWhenMayorUpStarts(t *testing.T, r *staleRig) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	go func() {
		for ctx.Err() == nil {
			if _, err := os.Stat(mayorUpStarted(r)); err == nil {
				cancel()
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	return ctx
}

func TestMayorAlarmAfterCutOfStaleIsSentOnALiveCtx(t *testing.T) {
	r := newStaleRig(t)
	slowMayorUp(r)
	var seen []alarmSeen
	r.check.Alarm = recordAlarm(&seen)
	r.run(0)
	r.now = r.now.Add(20 * time.Minute)

	ctx := cutWhenMayorUpStarts(t, r)
	curesSayCut(t, r.check.Cure(ctx))
	if len(seen) != 1 {
		t.Fatalf("expected the one alarm after the cut, got %d", len(seen))
	}
	if seen[0].ended {
		t.Errorf("the alarm was sent with a ctx that had ended")
	}
	if !strings.Contains(seen[0].text, "could not start") {
		t.Errorf("expected the alarm to say no new Mayor was started, got %q", seen[0].text)
	}
}

func TestMayorAlarmAfterCutOfStuckIsSentOnALiveCtx(t *testing.T) {
	r := newStaleRig(t)
	slowMayorUp(r)
	loadedTmux(t, r, 400*time.Millisecond)
	var seen []alarmSeen
	stuck := &doctor.MayorStuck{
		Vault: r.vault, Tmux: r.check.Tmux, State: r.check.State,
		Now:   func() time.Time { return r.now },
		Alarm: recordAlarm(&seen),
	}

	ctx := cutWhenMayorUpStarts(t, r)
	curesSayCut(t, stuck.Cure(ctx))
	if len(seen) != 1 {
		t.Fatalf("expected the one alarm after the cut, got %d", len(seen))
	}
	if seen[0].ended {
		t.Errorf("the alarm was sent with a ctx that had ended")
	}
}

// TestMayorAlarmAfterCutKeepsADeadlineOfItsOwn: the alarm is not left to hang
// for ever on a ctx that no longer ends.
func TestMayorAlarmAfterCutKeepsADeadlineOfItsOwn(t *testing.T) {
	r := newStaleRig(t)
	slowMayorUp(r)
	var hasDeadline bool
	r.check.Alarm = func(ctx context.Context, _ string) (uint64, error) {
		_, hasDeadline = ctx.Deadline()
		return 0, nil
	}
	r.run(0)
	r.now = r.now.Add(20 * time.Minute)

	ctx := cutWhenMayorUpStarts(t, r)
	_ = r.check.Cure(ctx)
	if !hasDeadline {
		t.Errorf("expected the alarm's ctx to carry a deadline of its own")
	}
}

// loadedTmux makes every answer of the rig's tmux take delay, as it does on a
// host loaded past its cores (mw-gq6.1753): a test whose ctx is meant to end
// during bin/mayor-up must not end it during the window search before.
func loadedTmux(t *testing.T, r *staleRig, delay time.Duration) {
	t.Helper()
	real := r.check.Tmux
	slow := real + "-loaded"
	r.write(slow, fmt.Sprintf("#!/bin/sh\nsleep %f\nexec '%s' \"$@\"\n", delay.Seconds(), real))
	r.chmod(slow)
	r.check.Tmux = slow
}
