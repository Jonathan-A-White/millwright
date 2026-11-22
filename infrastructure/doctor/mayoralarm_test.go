package doctor_test

import (
	"context"
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
	r.write(mayorUp, "#!/bin/sh\nexec sleep 30\n")
	r.chmod(mayorUp)
}

func TestMayorAlarmAfterCutOfStaleIsSentOnALiveCtx(t *testing.T) {
	r := newStaleRig(t)
	slowMayorUp(r)
	var seen []alarmSeen
	r.check.Alarm = recordAlarm(&seen)
	r.run(0)
	r.now = r.now.Add(20 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
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
	var seen []alarmSeen
	stuck := &doctor.MayorStuck{
		Vault: r.vault, Tmux: r.check.Tmux, State: r.check.State,
		Now:   func() time.Time { return r.now },
		Alarm: recordAlarm(&seen),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
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

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = r.check.Cure(ctx)
	if !hasDeadline {
		t.Errorf("expected the alarm's ctx to carry a deadline of its own")
	}
}
