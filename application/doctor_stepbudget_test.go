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
)

// mw-gq6.1330: RunBudget held the probes and the dampers read, but a check's
// cure, the notes written, cleared and listed, and the verdict left are bd
// calls it did not hold, so a hung bd there still ran the run into the unit's
// timeout. StepBudget is how long after RunBudget those may go on.

// hungCure is a faulty check whose cure does not come back until released, as a
// cure waiting on a hung bd does not.
type hungCure struct {
	apptest.FakeDoctorCheck
	release chan struct{}
}

func (h *hungCure) Cure(ctx context.Context) error {
	_ = h.FakeDoctorCheck.Cure(ctx)
	<-h.release
	return nil
}

// hungNotes is a notes port whose chosen calls do not come back until released.
type hungNotes struct {
	*apptest.FakeDoctorNotes
	release                        chan struct{}
	hangSet, hangClear, hangList   bool
	hangNote                       bool
	mu                             sync.Mutex
	setCalls, clearCalls, listCall int
}

func (h *hungNotes) SetNote(ctx context.Context, key, value string) error {
	h.mu.Lock()
	h.setCalls++
	h.mu.Unlock()
	if h.hangSet {
		<-h.release
	}
	return h.FakeDoctorNotes.SetNote(ctx, key, value)
}

func (h *hungNotes) ClearNote(ctx context.Context, key string) error {
	h.mu.Lock()
	h.clearCalls++
	h.mu.Unlock()
	if h.hangClear {
		<-h.release
	}
	return h.FakeDoctorNotes.ClearNote(ctx, key)
}

func (h *hungNotes) NotesWithPrefix(ctx context.Context, prefix string) (map[string]string, error) {
	h.mu.Lock()
	h.listCall++
	h.mu.Unlock()
	if h.hangList {
		<-h.release
	}
	return h.FakeDoctorNotes.NotesWithPrefix(ctx, prefix)
}

func (h *hungNotes) Note(ctx context.Context, key string) (string, error) {
	if h.hangNote {
		<-h.release
	}
	return h.FakeDoctorNotes.Note(ctx, key)
}

func newHungNotes() *hungNotes {
	return &hungNotes{FakeDoctorNotes: apptest.NewFakeDoctorNotes(), release: make(chan struct{})}
}

// runEnds runs the doctor and fails the test when it does not end: the run a
// hung step left to systemd's timeout.
func runEnds(t *testing.T, d application.Doctor) (application.DoctorReport, error) {
	t.Helper()
	type ended struct {
		report application.DoctorReport
		err    error
	}
	done := make(chan ended, 1)
	go func() {
		r, err := d.Run(context.Background(), "", false)
		done <- ended{r, err}
	}()
	select {
	case e := <-done:
		return e.report, e.err
	case <-time.After(5 * time.Second):
		t.Fatal("the run did not end: a step past the run's budget was left to systemd's timeout")
		return application.DoctorReport{}, nil
	}
}

func stepDoctor(now *time.Time, checks ...application.DoctorCheck) (application.Doctor, *apptest.FakeDoctorLog, *apptest.FakeDoctorState) {
	log := &apptest.FakeDoctorLog{}
	state := apptest.NewFakeDoctorState()
	d := aDoctor(state, log, now, checks...)
	d.Host = "laptop"
	d.RunBudget = 50 * time.Millisecond
	d.StepBudget = 50 * time.Millisecond
	return d, log, state
}

func logHolds(log *apptest.FakeDoctorLog, parts ...string) bool {
	for _, line := range log.Lines() {
		all := true
		for _, p := range parts {
			all = all && strings.Contains(line, p)
		}
		if all {
			return true
		}
	}
	return false
}

func TestDoctorStepsPastTheRunBudget(t *testing.T) {
	const spent = "took over 0.1s: the run's budget is spent"

	t.Run("a cure that outlasts it is cut and said cure-failed, and the next check is worked", func(t *testing.T) {
		now := doctorNow
		hung := &hungCure{FakeDoctorCheck: apptest.FakeDoctorCheck{CheckName: "hung", Verdict: application.DoctorFaulty, Wait: time.Hour, Cap: 3}, release: make(chan struct{})}
		defer close(hung.release)
		after := &apptest.FakeDoctorCheck{CheckName: "after", Verdict: application.DoctorOK}
		d, log, state := stepDoctor(&now, hung, after)

		report, err := runEnds(t, d)
		fault, ok := application.DoctorFaults(err)
		if !ok || fault.Line != "hung" {
			t.Fatalf("a cure cut is left faulty and the run ends by it: %v", err)
		}
		got := report.Results[0]
		if got.Verdict != "cure-failed" || got.Reason != "the cure "+spent {
			t.Errorf("hung = %q %q, want cure-failed (the cure %s)", got.Verdict, got.Reason, spent)
		}
		if !logHolds(log, "hung cure-failed", spent) {
			t.Errorf("the cure cut is logged: %v", log.Lines())
		}
		if ep := state.Episode("hung"); ep.Cures != 1 || !ep.LastCureFailed {
			t.Errorf("the cure that began is counted by the damper: %+v", ep)
		}
		if len(report.Results) != 2 || report.Results[1].Verdict != "ok" && report.Results[1].Verdict != "cannot-tell" {
			t.Errorf("the run goes on to the next check: %v", report.Results)
		}
	})

	t.Run("a cure that would begin after the end is not begun", func(t *testing.T) {
		now := doctorNow
		late := &advancingCheck{
			FakeDoctorCheck: apptest.FakeDoctorCheck{CheckName: "late", Verdict: application.DoctorFaulty, Wait: time.Hour, Cap: 3},
			now:             &now,
			took:            time.Minute,
		}
		d, log, _ := stepDoctor(&now, late)
		d.RunBudget, d.StepBudget = 3*time.Minute, 30*time.Second
		d.CheckBudget = 10 * time.Minute
		late.took = 3*time.Minute + 31*time.Second

		report, err := runEnds(t, d)
		if _, ok := application.DoctorFaults(err); !ok {
			t.Fatalf("a cure not begun leaves the check faulty: %v", err)
		}
		if late.Cures() != 0 {
			t.Errorf("a cure is not begun past the end: %d", late.Cures())
		}
		if got := report.Results[0]; got.Verdict != "cure-failed" || got.Reason != "the cure took over 210s: the run's budget is spent" {
			t.Errorf("late = %q %q", got.Verdict, got.Reason)
		}
		if !logHolds(log, "late cure-failed") {
			t.Errorf("it is logged: %v", log.Lines())
		}
	})

	t.Run("a note written that outlasts it is cut and logged note-failed, and the run goes on", func(t *testing.T) {
		now := doctorNow
		cannot := &apptest.FakeDoctorCheck{CheckName: "unsure", Verdict: application.DoctorCannotTell, Reason: "no idea"}
		after := &apptest.FakeDoctorCheck{CheckName: "after", Verdict: application.DoctorOK}
		d, log, _ := stepDoctor(&now, cannot, after)
		notes := newHungNotes()
		defer close(notes.release)
		notes.hangSet = true
		d.Notes = notes

		report, err := runEnds(t, d)
		if err != nil {
			t.Fatalf("a note cut is no failure of the run: %v", err)
		}
		if report.Results[0].Verdict != "cannot-tell" {
			t.Errorf("the check keeps its verdict: %v", report.Results)
		}
		if !logHolds(log, "unsure note-failed", spent) {
			t.Errorf("the note cut is said in the log: %v", log.Lines())
		}
	})

	t.Run("a note cleared that outlasts it is cut and logged note-failed", func(t *testing.T) {
		now := doctorNow
		ok := &apptest.FakeDoctorCheck{CheckName: "fine", Verdict: application.DoctorOK}
		d, log, _ := stepDoctor(&now, ok)
		notes := newHungNotes()
		defer close(notes.release)
		notes.hangClear = true
		_ = notes.FakeDoctorNotes.SetNote(context.Background(), application.DoctorNoteKey("laptop", "fine"), "old")
		d.Notes = notes

		if _, err := runEnds(t, d); err != nil {
			t.Fatalf("a clear cut is no failure of the run: %v", err)
		}
		if !logHolds(log, "fine note-failed", spent) {
			t.Errorf("the clear cut is said in the log: %v", log.Lines())
		}
	})

	t.Run("a listing of the notes that outlasts it is given up and every ok check clears its note", func(t *testing.T) {
		now := doctorNow
		ok := &apptest.FakeDoctorCheck{CheckName: "fine", Verdict: application.DoctorOK}
		d, _, _ := stepDoctor(&now, ok)
		notes := newHungNotes()
		defer close(notes.release)
		notes.hangList = true
		d.Notes = notes

		if _, err := runEnds(t, d); err != nil {
			t.Fatalf("a listing cut is no failure of the run: %v", err)
		}
		if notes.clearCalls != 1 {
			t.Errorf("with no listing every ok check clears its note: %d clears", notes.clearCalls)
		}
	})

	t.Run("a verdict left that outlasts it is cut and logged note-failed", func(t *testing.T) {
		now := doctorNow
		ok := &apptest.FakeDoctorCheck{CheckName: "fine", Verdict: application.DoctorOK}
		d, log, _ := stepDoctor(&now, ok)
		verdicts := newHungNotes()
		defer close(verdicts.release)
		verdicts.hangNote, verdicts.hangSet = true, true
		d.Verdicts = verdicts

		if _, err := runEnds(t, d); err != nil {
			t.Fatalf("a verdict cut is no failure of the run: %v", err)
		}
		if !logHolds(log, "doctor note-failed", spent) {
			t.Errorf("the verdict cut is said in the log: %v", log.Lines())
		}
	})

	t.Run("with no StepBudget the steps are not held, as before", func(t *testing.T) {
		now := doctorNow
		cure := &apptest.FakeDoctorCheck{CheckName: "cure", Verdict: application.DoctorFaulty, Wait: time.Hour, Cap: 3, CureErr: errors.New("boom")}
		d, _, _ := stepDoctor(&now, cure)
		d.StepBudget = 0
		report, _ := runEnds(t, d)
		if got := report.Results[0]; got.Verdict != "cure-failed" || got.Reason != "boom" {
			t.Errorf("cure = %q %q", got.Verdict, got.Reason)
		}
	})
}
