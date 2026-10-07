package grist_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/grist"
)

func aRun(txid string, received time.Time, status string) application.GristRun {
	return application.GristRun{
		Txid:        txid,
		Input:       json.RawMessage(`{"target_text":"the cat sat"}`),
		Attachments: []application.GristRunAttachment{{Ext: ".webm", Data: []byte("audio")}},
		Answer:      application.GristRunAnswer{Status: status},
		Timing:      application.GristRunTiming{Txid: txid, Kind: "reading", Model: "opus", Received: received, Seconds: 12.5},
	}
}

// A run is five files and its attachments in runs/<txid>/, private, and a run
// with nothing scored still has a scorers.json that is a list.
func TestARunIsKeptPrivatelyInItsOwnDirectory(t *testing.T) {
	ctx, dir := context.Background(), t.TempDir()
	if err := grist.NewRuns(dir).Keep(ctx, aRun("direct:abc", time.Now(), "answered")); err != nil {
		t.Fatal(err)
	}
	run := filepath.Join(dir, "runs", "direct:abc")
	for _, name := range []string{"input.json", "attachment-1.webm", "scorers.json", "answer.json", "timing.json"} {
		info, err := os.Stat(filepath.Join(run, name))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("expected %s kept 0600, got %v %v", name, info, err)
		}
	}
	if info, _ := os.Stat(run); info.Mode().Perm() != 0o700 {
		t.Fatalf("expected the run's directory 0700, got %v", info.Mode())
	}
	if raw, _ := os.ReadFile(filepath.Join(run, "scorers.json")); string(raw) != "[]\n" {
		t.Fatalf("expected an empty list in scorers.json, got %q", raw)
	}
}

// A txid that could climb out of runs/ is kept under a name of its own.
func TestATxidCannotNameAPlaceOutsideRuns(t *testing.T) {
	ctx, dir := context.Background(), t.TempDir()
	if err := grist.NewRuns(dir).Keep(ctx, aRun("../../etc", time.Now(), "answered")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "runs"))
	if err != nil || len(entries) != 1 || entries[0].Name() == ".." || entries[0].Name() == "../../etc" {
		t.Fatalf("expected one run kept inside runs/, got %v %v", entries, err)
	}
}

// The list is oldest first, holds only the runs since the time given, and
// says how each ended.
func TestRunsAreListedOldestFirstSinceATime(t *testing.T) {
	ctx, dir := context.Background(), t.TempDir()
	runs := grist.NewRuns(dir)
	at := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	for _, r := range []application.GristRun{
		aRun("direct:late", at.Add(2*time.Hour), "failed"),
		aRun("direct:early", at, "answered"),
	} {
		if err := runs.Keep(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	all, err := runs.List(ctx, time.Time{})
	if err != nil || len(all) != 2 || all[0].Txid != "direct:early" || all[1].Status != "failed" || all[0].Seconds != 12.5 || all[0].Kind != "reading" || all[0].Model != "opus" {
		t.Fatalf("expected both runs, oldest first, got %+v %v", all, err)
	}
	later, err := runs.List(ctx, at.Add(time.Hour))
	if err != nil || len(later) != 1 || later[0].Txid != "direct:late" {
		t.Fatalf("expected only the late run, got %+v %v", later, err)
	}
	if none, err := grist.NewRuns(t.TempDir()).List(ctx, time.Time{}); err != nil || len(none) != 0 {
		t.Fatalf("expected no runs where none were kept, got %+v %v", none, err)
	}
}
