package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1364: StepBudget held a cure, the notes and the verdict, but the doctor
// state's Load, Save and Reset and the log's Append and Read were still called
// with no end, so a port behind them that hung would run the run into the unit's
// timeout.

// hungState is a state port whose chosen calls do not come back until released.
type hungState struct {
	*apptest.FakeDoctorState
	release   chan struct{}
	hangLoad  bool
	hangSave  bool
	hangReset bool
}

func (h *hungState) Load(ctx context.Context, check string) (application.DoctorEpisode, error) {
	if h.hangLoad {
		<-h.release
	}
	return h.FakeDoctorState.Load(ctx, check)
}

func (h *hungState) Save(ctx context.Context, check string, episode application.DoctorEpisode) error {
	if h.hangSave {
		<-h.release
	}
	return h.FakeDoctorState.Save(ctx, check, episode)
}

func (h *hungState) Reset(ctx context.Context, check string) error {
	if h.hangReset {
		<-h.release
	}
	return h.FakeDoctorState.Reset(ctx, check)
}

// hungLog is a log port whose chosen calls do not come back until released.
type hungLog struct {
	*apptest.FakeDoctorLog
	release            chan struct{}
	hangAppend, hangRd bool
}

func (h *hungLog) Append(ctx context.Context, line string) error {
	if h.hangAppend {
		<-h.release
	}
	return h.FakeDoctorLog.Append(ctx, line)
}

func (h *hungLog) Read(ctx context.Context) ([]string, error) {
	if h.hangRd {
		<-h.release
	}
	return h.FakeDoctorLog.Read(ctx)
}

func TestDoctorStateAndLogPastTheRunEnd(t *testing.T) {
	const spent = "took over 0.1s: the run's budget is spent"

	setup := func(checks ...application.DoctorCheck) (application.Doctor, *hungState, *hungLog) {
		now := doctorNow
		d, _, _ := stepDoctor(&now, checks...)
		state := &hungState{FakeDoctorState: apptest.NewFakeDoctorState(), release: make(chan struct{})}
		log := &hungLog{FakeDoctorLog: &apptest.FakeDoctorLog{}, release: make(chan struct{})}
		d.State, d.Log = state, log
		return d, state, log
	}
	unsure := func() application.DoctorCheck {
		return &apptest.FakeDoctorCheck{CheckName: "unsure", Verdict: application.DoctorCannotTell, Reason: "no idea"}
	}

	t.Run("a state save that outlasts it is cut and the run ends saying so", func(t *testing.T) {
		d, state, _ := setup(unsure())
		defer close(state.release)
		state.hangSave = true

		_, err := runEnds(t, d)
		if err == nil || !strings.Contains(err.Error(), "saving unsure's doctor state") || !strings.Contains(err.Error(), spent) {
			t.Errorf("the save cut is said by the run's error: %v", err)
		}
	})

	t.Run("a state load that outlasts it is cut and the run ends saying so", func(t *testing.T) {
		d, state, _ := setup(unsure())
		defer close(state.release)
		state.hangLoad = true

		_, err := runEnds(t, d)
		if err == nil || !strings.Contains(err.Error(), "reading unsure's doctor state") || !strings.Contains(err.Error(), spent) {
			t.Errorf("the load cut is said by the run's error: %v", err)
		}
	})

	t.Run("a state reset that outlasts it is cut and the run ends saying so", func(t *testing.T) {
		d, state, _ := setup(&apptest.FakeDoctorCheck{CheckName: "fine", Verdict: application.DoctorOK})
		defer close(state.release)
		state.hangReset = true

		_, err := runEnds(t, d)
		if err == nil || !strings.Contains(err.Error(), "resetting fine's doctor state") || !strings.Contains(err.Error(), spent) {
			t.Errorf("the reset cut is said by the run's error: %v", err)
		}
	})

	t.Run("a log append that outlasts it is cut and the run ends saying so", func(t *testing.T) {
		d, _, log := setup(unsure())
		defer close(log.release)
		log.hangAppend = true

		_, err := runEnds(t, d)
		if err == nil || !strings.Contains(err.Error(), "appending to the doctor log") || !strings.Contains(err.Error(), spent) {
			t.Errorf("the append cut is said by the run's error: %v", err)
		}
	})

	t.Run("a log read that outlasts it leaves the note written with no log lines", func(t *testing.T) {
		d, _, log := setup(unsure())
		defer close(log.release)
		log.hangRd = true
		notes := apptest.NewFakeDoctorNotes()
		d.Notes = notes

		if _, err := runEnds(t, d); err != nil {
			t.Fatalf("a log read cut is no failure of the run: %v", err)
		}
		note, _ := notes.Note(context.Background(), application.DoctorNoteKey("laptop", "unsure"))
		if !strings.Contains(note, "cannot-tell no idea") {
			t.Errorf("the note is still written: %q", note)
		}
	})

	t.Run("a hung log is not waited on again for each note said", func(t *testing.T) {
		d, _, log := setup(unsure(), &apptest.FakeDoctorCheck{CheckName: "second", Verdict: application.DoctorOK})
		defer close(log.release)
		log.hangAppend = true
		d.Notes = newHungNotes()

		start := time.Now()
		if _, err := runEnds(t, d); err == nil {
			t.Errorf("the append cut ends the run with an error")
		}
		if took := time.Since(start); took > 2*time.Second {
			t.Errorf("the run took %v: the hung log was waited on again and again", took)
		}
	})
}
