package doctor_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

func TestAnEpisodeRoundTripsThroughItsStateFile(t *testing.T) {
	store := doctor.New(t.TempDir())
	ctx := context.Background()
	want := application.DoctorEpisode{
		FirstFaulty: time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC),
		Cures:       2,
		LastCure:    time.Date(2026, 9, 23, 10, 20, 0, 0, time.UTC),
	}

	if err := store.Save(ctx, "daemon-reload", want); err != nil {
		t.Fatalf("saving: %v", err)
	}
	got, err := store.Load(ctx, "daemon-reload")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if !got.FirstFaulty.Equal(want.FirstFaulty) || got.Cures != want.Cures || !got.LastCure.Equal(want.LastCure) {
		t.Fatalf("expected %+v back, got %+v", want, got)
	}
}

func TestAnEpisodeNeverSavedIsTheZeroValue(t *testing.T) {
	store := doctor.New(t.TempDir())
	got, err := store.Load(context.Background(), "daemon-reload")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if !got.FirstFaulty.IsZero() || got.Cures != 0 || !got.LastCure.IsZero() {
		t.Fatalf("expected the zero episode, got %+v", got)
	}
}

func TestResetForgetsAnEpisode(t *testing.T) {
	store := doctor.New(t.TempDir())
	ctx := context.Background()
	episode := application.DoctorEpisode{Cures: 3, LastCure: time.Now()}
	if err := store.Save(ctx, "daemon-reload", episode); err != nil {
		t.Fatalf("saving: %v", err)
	}

	if err := store.Reset(ctx, "daemon-reload"); err != nil {
		t.Fatalf("resetting: %v", err)
	}
	got, err := store.Load(ctx, "daemon-reload")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if got.Cures != 0 {
		t.Fatalf("expected the episode forgotten, got %+v", got)
	}
}

func TestResettingAnEpisodeNeverSavedIsNotAnError(t *testing.T) {
	store := doctor.New(t.TempDir())
	if err := store.Reset(context.Background(), "daemon-reload"); err != nil {
		t.Fatalf("expected resetting nothing to be fine, got %v", err)
	}
}

func TestAppendAddsOneLineAtATime(t *testing.T) {
	dir := t.TempDir()
	store := doctor.New(dir)
	ctx := context.Background()

	if err := store.Append(ctx, "2026-09-23T10:00:00Z daemon-reload ok"); err != nil {
		t.Fatalf("appending: %v", err)
	}
	if err := store.Append(ctx, "2026-09-23T10:05:00Z daemon-reload ok"); err != nil {
		t.Fatalf("appending: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, doctor.LogFile))
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	want := "2026-09-23T10:00:00Z daemon-reload ok\n2026-09-23T10:05:00Z daemon-reload ok\n"
	if string(data) != want {
		t.Fatalf("expected %q, got %q", want, string(data))
	}
}
