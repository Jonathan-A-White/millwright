package grist_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/grist"
)

func TestTheCursorStartsAtNothingAndIsKept(t *testing.T) {
	ctx, dir := context.Background(), filepath.Join(t.TempDir(), "grist")
	state := grist.New(dir)
	if seq, err := state.Cursor(ctx); err != nil || seq != 0 {
		t.Fatalf("expected 0 before the first pass, got %d %v", seq, err)
	}
	if err := state.SetCursor(ctx, 42); err != nil {
		t.Fatal(err)
	}
	if seq, err := grist.New(dir).Cursor(ctx); err != nil || seq != 42 {
		t.Fatalf("expected 42, got %d %v", seq, err)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("expected the state directory 0700, got %v %v", info, err)
	}
}

// The record is one JSON line per grist, appended, 0600, and read back.
func TestTheRecordIsAppendedAndReadBack(t *testing.T) {
	ctx, dir := context.Background(), t.TempDir()
	state := grist.New(dir)
	at := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	for _, txid := range []string{"direct:a", "direct:b"} {
		if err := state.Append(ctx, application.GrindLine{Time: at, Txid: txid, Sender: "1848 4c0e 494b bb6e", Status: application.GristAnswered, Delivered: true}); err != nil {
			t.Fatal(err)
		}
	}
	lines, err := state.Lines(ctx)
	if err != nil || len(lines) != 2 || lines[0].Txid != "direct:a" || lines[1].Txid != "direct:b" || !lines[1].Time.Equal(at) {
		t.Fatalf("expected both lines back in order, got %+v %v", lines, err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, grist.RecordFile))
	if strings.Count(string(raw), "\n") != 2 {
		t.Fatalf("expected one line each, got %q", raw)
	}
	if info, _ := os.Stat(filepath.Join(dir, grist.RecordFile)); info.Mode().Perm() != 0o600 {
		t.Fatalf("expected the record 0600, got %v", info.Mode())
	}
}

// A line that is not one of the record's stops the read, rather than being
// skipped and its grist answered twice.
func TestARecordLineThatIsNotOneIsAnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, grist.RecordFile), []byte("{\"txid\":\"direct:a\"}\nnot json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := grist.New(dir).Lines(context.Background()); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("expected line 2 named, got %v", err)
	}
}

func TestAnUndeliveredAnswerIsKeptUntilDelivered(t *testing.T) {
	ctx, dir := context.Background(), t.TempDir()
	state := grist.New(dir)
	kept := application.GristUndelivered{Txid: "direct:abc", Payload: []byte(`{"v":1}`), Blobs: []string{"9f86"}}
	if err := state.KeepUndelivered(ctx, kept); err != nil {
		t.Fatal(err)
	}
	got, err := state.Undelivered(ctx)
	if err != nil || len(got) != 1 || got[0].Txid != kept.Txid || string(got[0].Payload) != `{"v":1}` || got[0].Blobs[0] != "9f86" {
		t.Fatalf("expected the answer kept, got %+v %v", got, err)
	}
	if err := state.Delivered(ctx, "direct:abc"); err != nil {
		t.Fatal(err)
	}
	if err := state.Delivered(ctx, "direct:abc"); err != nil {
		t.Fatalf("forgetting an answer already forgotten: %v", err)
	}
	if got, _ := state.Undelivered(ctx); len(got) != 0 {
		t.Fatalf("expected nothing kept once delivered, got %+v", got)
	}
}
