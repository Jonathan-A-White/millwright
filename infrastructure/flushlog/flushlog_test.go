package flushlog_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/flushlog"
)

func TestNoFileIsNoRecord(t *testing.T) {
	last, err := flushlog.New(filepath.Join(t.TempDir(), "beads-flush")).Last(context.Background())

	if err != nil || !last.At.IsZero() {
		t.Fatalf("expected the zero record, got %+v (%v)", last, err)
	}
}

func TestARecordComesBackAsItWentIn(t *testing.T) {
	log := flushlog.New(filepath.Join(t.TempDir(), "state", "beads-flush"))
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	for _, want := range []application.BeadsFlushRecord{{At: at}, {At: at, Failed: "server unreachable at 127.0.0.1"}} {
		if err := log.Record(context.Background(), want); err != nil {
			t.Fatal(err)
		}
		got, err := log.Last(context.Background())
		if err != nil || !got.At.Equal(want.At) || got.Failed != want.Failed {
			t.Fatalf("wrote %+v, read %+v (%v)", want, got, err)
		}
	}
}

func TestAFileThatIsNotARecordIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "beads-flush")
	if err := os.WriteFile(path, []byte("garbage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := flushlog.New(path).Last(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
}
