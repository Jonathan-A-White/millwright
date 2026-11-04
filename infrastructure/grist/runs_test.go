package grist_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// Timings reads every kept timing.json back, oldest first, with the sent time
// and each scorer's seconds, and skips a directory that is no run.
func TestTimingsAreReadBackWithSentAndTheScorersSeconds(t *testing.T) {
	ctx, dir := context.Background(), t.TempDir()
	runs := grist.NewRuns(dir)
	at := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	sent := at.Add(-3 * time.Second)
	later, earlier := aRun("direct:later", at.Add(time.Minute), "answered"), aRun("direct:earlier", at, "answered")
	earlier.Timing.Sent = &sent
	earlier.Timing.Scorers = map[string]float64{"local": 1.5, "azure": 2.5}
	for _, run := range []application.GristRun{later, earlier} {
		if err := runs.Keep(ctx, run); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "runs", "not-a-run"), 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := runs.Timings(ctx)
	if err != nil || len(got) != 2 || got[0].Txid != "direct:earlier" || got[1].Txid != "direct:later" {
		t.Fatalf("expected the two runs, oldest first, got %+v %v", got, err)
	}
	if got[0].Sent == nil || !got[0].Sent.Equal(sent) || got[0].Scorers["local"] != 1.5 || got[0].Scorers["azure"] != 2.5 {
		t.Errorf("expected sent and scorer seconds read back, got %+v", got[0])
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "runs", "direct:later", "timing.json"))
	if strings.Contains(string(raw), `"sent"`) || strings.Contains(string(raw), `"scorers"`) {
		t.Errorf("expected a run with neither to omit both, got %s", raw)
	}
}

// Only a forward run that kept its sender's key is a forwarded grist: a run of
// another kind, one with no sender kept, and a txid never run are not.
func TestOnlyAForwardRunThatKeptItsSenderIsForwarded(t *testing.T) {
	ctx, dir := context.Background(), t.TempDir()
	runs := grist.NewRuns(dir)
	forward := aRun("direct:fwd", time.Now(), "answered")
	forward.Timing.Kind, forward.Timing.App, forward.Sender = application.GristForwardKind, "cairn", "02sender"
	oldForward := aRun("direct:old", time.Now(), "answered")
	oldForward.Timing.Kind = application.GristForwardKind
	for _, run := range []application.GristRun{forward, oldForward, aRun("direct:ground", time.Now(), "answered")} {
		if err := runs.Keep(ctx, run); err != nil {
			t.Fatal(err)
		}
	}
	got, ok, err := runs.Forwarded(ctx, "direct:fwd")
	if err != nil || !ok || got != (application.GristForwarded{Txid: "direct:fwd", App: "cairn", Sender: "02sender"}) {
		t.Fatalf("expected the forward run with its sender, got %+v %v %v", got, ok, err)
	}
	if info, err := os.Stat(filepath.Join(dir, "runs", "direct:fwd", "sender.json")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("expected sender.json kept 0600, got %v %v", info, err)
	}
	for _, txid := range []string{"direct:old", "direct:ground", "direct:never"} {
		if _, ok, err := runs.Forwarded(ctx, txid); ok || err != nil {
			t.Fatalf("expected %s not a forwarded grist, got %v %v", txid, ok, err)
		}
	}
}

// A run kept before timings carried model_id lists and times as it did
// (mw-2zh33g.11).
func TestARunWithNoModelIDStillLists(t *testing.T) {
	ctx, dir := context.Background(), t.TempDir()
	run := filepath.Join(dir, "runs", "direct:old")
	if err := os.MkdirAll(run, 0o700); err != nil {
		t.Fatal(err)
	}
	old := `{"txid":"direct:old","app":"lampas","kind":"tips","model":"haiku","effort":"low","received":"2026-10-01T09:00:00Z","harness_started":"2026-10-01T09:00:00Z","answered":"2026-10-01T09:00:03Z","scoring_seconds":0,"harness_seconds":3,"seconds":3.4,"run_s":3.4}`
	for name, body := range map[string]string{"timing.json": old, "answer.json": `{"status":"answered"}`} {
		if err := os.WriteFile(filepath.Join(run, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runs := grist.NewRuns(dir)
	listed, err := runs.List(ctx, time.Time{})
	if err != nil || len(listed) != 1 || listed[0].Model != "haiku" || listed[0].Kind != "tips" || listed[0].Seconds != 3.4 || listed[0].Status != "answered" {
		t.Fatalf("expected the old run listed as it was, got %+v %v", listed, err)
	}
	timings, err := runs.Timings(ctx)
	if err != nil || len(timings) != 1 || timings[0].ModelID != "" || timings[0].Model != "haiku" || timings[0].HarnessSeconds != 3 {
		t.Fatalf("expected the old timing read with no model id, got %+v %v", timings, err)
	}
}

// mw-gq6.692: the sweep reads a run's attachments and drops them, and nothing
// else of the run.
func TestDroppingARunsMediaKeepsItsRecord(t *testing.T) {
	ctx, dir := context.Background(), t.TempDir()
	runs := grist.NewRuns(dir)
	received := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	if err := runs.Keep(ctx, aRun("direct:old", received, "answered")); err != nil {
		t.Fatal(err)
	}
	if err := runs.Keep(ctx, aRun("direct:newer", received.Add(time.Hour), "answered")); err != nil {
		t.Fatal(err)
	}
	media, err := runs.RunMedia(ctx)
	if err != nil || len(media) != 2 || media[0].Txid != "direct:old" || media[0].Files != 1 || media[0].Bytes != 5 || !media[0].Received.Equal(received) {
		t.Fatalf("expected the two runs' media, oldest first, got %+v %v", media, err)
	}
	if err := runs.DropMedia(ctx, media[0].Name); err != nil {
		t.Fatal(err)
	}
	run := filepath.Join(dir, "runs", "direct:old")
	if _, err := os.Stat(filepath.Join(run, "attachment-1.webm")); !os.IsNotExist(err) {
		t.Fatalf("expected the attachment gone, got %v", err)
	}
	for _, name := range []string{"input.json", "scorers.json", "answer.json", "timing.json"} {
		if _, err := os.Stat(filepath.Join(run, name)); err != nil {
			t.Fatalf("expected %s kept: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "runs", "direct:newer", "attachment-1.webm")); err != nil {
		t.Fatalf("expected the other run's attachment kept: %v", err)
	}
	// The record still lists, and a run with no media is not listed again.
	if lines, err := runs.List(ctx, time.Time{}); err != nil || len(lines) != 2 {
		t.Fatalf("expected both runs still listed, got %v %v", lines, err)
	}
	if media, _ := runs.RunMedia(ctx); len(media) != 1 || media[0].Txid != "direct:newer" {
		t.Fatalf("expected one run with media left, got %+v", media)
	}
}

func TestAMediaNameThatClimbsIsRefused(t *testing.T) {
	runs := grist.NewRuns(t.TempDir())
	for _, name := range []string{"", ".", "..", "../x", "a/b"} {
		if err := runs.DropMedia(context.Background(), name); err == nil {
			t.Fatalf("expected %q refused", name)
		}
	}
}

// mw-gq6.692, end to end on real directories: a run 30 days old loses its
// media, one a second younger does not.
func TestTheThirtyDayBoundaryOnRealRuns(t *testing.T) {
	ctx, dir := context.Background(), t.TempDir()
	runs := grist.NewRuns(dir)
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	if err := runs.Keep(ctx, aRun("direct:thirty", now.Add(-application.GristKeepAge), "answered")); err != nil {
		t.Fatal(err)
	}
	if err := runs.Keep(ctx, aRun("direct:young", now.Add(-application.GristKeepAge+time.Second), "answered")); err != nil {
		t.Fatal(err)
	}
	report, err := application.GristTidy{Runs: runs, Now: func() time.Time { return now }}.Run(ctx)
	if err != nil || len(report.Acts) != 1 || report.Acts[0].Txid != "direct:thirty" {
		t.Fatalf("expected the 30-day run alone swept, got %+v %v", report, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "runs", "direct:thirty", "attachment-1.webm")); !os.IsNotExist(err) {
		t.Fatalf("expected the 30-day run's media gone, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "runs", "direct:young", "attachment-1.webm")); err != nil {
		t.Fatalf("expected the younger run's media kept: %v", err)
	}
}
