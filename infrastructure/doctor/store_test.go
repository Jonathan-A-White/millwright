package doctor_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

func TestAnEpisodeRoundTripsThroughItsStateFile(t *testing.T) {
	store := doctor.New(t.TempDir())
	ctx := context.Background()
	want := application.DoctorEpisode{
		FirstFaulty:          time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC),
		Cures:                2,
		LastCure:             time.Date(2026, 9, 23, 10, 20, 0, 0, time.UTC),
		LastCannotTellReason: "powershell not found: this host is not Windows-backed",
		LastCureFailed:       true,
		DampedNoted:          true,
		SeenPaths:            []string{"seats/a.md", "plans/b.md"},
	}

	if err := store.Save(ctx, "daemon-reload", want); err != nil {
		t.Fatalf("saving: %v", err)
	}
	got, err := store.Load(ctx, "daemon-reload")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if !got.FirstFaulty.Equal(want.FirstFaulty) || got.Cures != want.Cures || !got.LastCure.Equal(want.LastCure) || got.LastCannotTellReason != want.LastCannotTellReason || got.LastCureFailed != want.LastCureFailed || got.DampedNoted != want.DampedNoted || !slices.Equal(got.SeenPaths, want.SeenPaths) {
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

// mw-gq6.982: the log is rotated once it passes its size: the full one becomes
// log.1 (the one before it is gone), Read still gives both oldest first, and
// the two never hold more than a rotation's worth each.
func TestTheLogIsRotatedOnceItPassesItsSize(t *testing.T) {
	dir := t.TempDir()
	store := doctor.New(dir)
	store.MaxLogBytes = 40
	ctx := context.Background()
	var written []string
	for i := 0; i < 12; i++ {
		line := "2026-11-07T00:00:00Z check-" + string(rune('a'+i)) + " ok"
		written = append(written, line)
		if err := store.Append(ctx, line); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{doctor.LogFile, doctor.LogFile + ".1"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.Size() > 40+int64(len(written[0]))+1 {
			t.Fatalf("expected %s kept within its size, got %v %v", name, info, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, doctor.LogFile+".2")); !os.IsNotExist(err) {
		t.Fatalf("expected no third log, got %v", err)
	}
	got, err := store.Read(ctx)
	if err != nil || len(got) == 0 || len(got) >= len(written) {
		t.Fatalf("expected some of the oldest lines gone, got %d of %d, %v", len(got), len(written), err)
	}
	if !slices.Equal(got, written[len(written)-len(got):]) {
		t.Fatalf("expected the newest lines in order, got %v", got)
	}
}

// A log under its size is never rotated.
func TestALogUnderItsSizeIsNotRotated(t *testing.T) {
	dir := t.TempDir()
	store := doctor.New(dir)
	for i := 0; i < 5; i++ {
		if err := store.Append(context.Background(), "a line"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, doctor.LogFile+".1")); !os.IsNotExist(err) {
		t.Fatalf("expected no rotated log, got %v", err)
	}
}
