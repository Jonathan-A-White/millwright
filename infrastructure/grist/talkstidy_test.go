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

func touch(t *testing.T, path string, at time.Time) {
	t.Helper()
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

// mw-gq6.982, on real directories: the instructions files and scratch
// directories of cwd/<app> 30 days old are deleted, one a second younger stays,
// and nothing else in a talk directory is listed.
func TestTalkDirectoryItemsAreDeletedThirtyDaysAfterTheyWereWrittenAndNotBefore(t *testing.T) {
	ctx, dir, now := context.Background(), t.TempDir(), time.Now()
	talks := grist.NewTalks(dir)
	app := filepath.Join(dir, grist.TalkCwdDir, "lampas")
	for _, d := range []string{"grist-1", "grist-2", "grist-3"} {
		if err := os.MkdirAll(filepath.Join(app, d), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(app, d, "photo.jpg"), []byte("12345"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"system-prompt-aaaa.md", "system-prompt-bbbb.md", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(app, f), []byte("text"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old, young := now.Add(-application.GristKeepAge), now.Add(-application.GristKeepAge+time.Minute)
	for name, at := range map[string]time.Time{"grist-1": old, "grist-2": young, "grist-3": young, "system-prompt-aaaa.md": old, "system-prompt-bbbb.md": young, "notes.txt": old.Add(-time.Hour)} {
		if name == "grist-1" || name == "grist-2" || name == "grist-3" {
			touch(t, filepath.Join(app, name, "photo.jpg"), at)
		}
		touch(t, filepath.Join(app, name), at)
	}
	// A young photo inside an old directory keeps the directory.
	touch(t, filepath.Join(app, "grist-1", "photo.jpg"), young)
	touch(t, filepath.Join(app, "grist-1"), old)

	report, err := application.GristTidy{Cwd: talks, Now: func() time.Time { return now }}.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Acts) != 1 || report.Acts[0].Txid != "lampas/system-prompt-aaaa.md" {
		t.Fatalf("expected only the old instructions file, got %+v", report.Acts)
	}
	touch(t, filepath.Join(app, "grist-1", "photo.jpg"), old)
	report, err = application.GristTidy{Cwd: talks, Now: func() time.Time { return now }}.Run(ctx)
	if err != nil || len(report.Acts) != 1 || report.Acts[0].Txid != "lampas/grist-1" || report.Acts[0].Files != 1 || report.Acts[0].Bytes != 5 {
		t.Fatalf("expected the old scratch directory, got %v %+v", err, report.Acts)
	}
	for _, kept := range []string{"grist-2", "grist-3", "system-prompt-bbbb.md", "notes.txt"} {
		if _, err := os.Stat(filepath.Join(app, kept)); err != nil {
			t.Fatalf("expected %s kept: %v", kept, err)
		}
	}
	for _, gone := range []string{"grist-1", "system-prompt-aaaa.md"} {
		if _, err := os.Stat(filepath.Join(app, gone)); !os.IsNotExist(err) {
			t.Fatalf("expected %s gone, got %v", gone, err)
		}
	}
}

// A name that is not an app's own item is never deleted.
func TestADropOfAnythingButATalkDirectoryItemIsRefused(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, grist.TalkCwdDir, "a"), 0o700); err != nil {
		t.Fatal(err)
	}
	talks := grist.NewTalks(dir)
	for _, name := range []string{"", "a", "a/b", "a/notes.txt", "../a/grist-1", "a/../grist-1", "a/system-prompt-x.txt", "/a/grist-1"} {
		if err := talks.DropKept(context.Background(), name); err == nil {
			t.Fatalf("expected %q refused", name)
		}
	}
}

func writeRecord(t *testing.T, dir string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, grist.RecordFile), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// mw-gq6.982: lines of grinds.jsonl 30 days old go, one a second younger stays;
// the rest stay byte for byte in order, a line that is not JSON among them.
func TestTheRecordLosesLinesThirtyDaysOldAndNotYounger(t *testing.T) {
	ctx, dir := context.Background(), t.TempDir()
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	line := func(at time.Time, txid string) string {
		return `{"time":"` + at.Format(time.RFC3339Nano) + `","txid":"` + txid + `","sender":"s","billing":"","status":"answered","photos":0}`
	}
	keep1 := line(now.Add(-application.GristKeepAge+time.Second), "almost")
	keep2 := line(now.Add(-time.Hour), "new")
	writeRecord(t, dir,
		line(now.Add(-40*24*time.Hour), "old"),
		line(now.Add(-application.GristKeepAge), "exactly"),
		keep1, "not json", keep2)
	state := grist.New(dir)
	report, err := application.GristTidy{Record: state, Now: func() time.Time { return now }}.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Acts) != 1 || report.Acts[0].Files != 2 {
		t.Fatalf("expected two lines cut, got %+v", report.Acts)
	}
	got, err := os.ReadFile(filepath.Join(dir, grist.RecordFile))
	if err != nil {
		t.Fatal(err)
	}
	if want := keep1 + "\nnot json\n" + keep2 + "\n"; string(got) != want {
		t.Fatalf("expected\n%s\ngot\n%s", want, got)
	}
	if info, _ := os.Stat(filepath.Join(dir, grist.RecordFile)); info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600, got %v", info.Mode())
	}
	// A second pass has nothing to cut, and a line appended afterwards lands in the new file.
	report, _ = application.GristTidy{Record: state, Now: func() time.Time { return now }}.Run(ctx)
	if len(report.Acts) != 0 {
		t.Fatalf("expected nothing more, got %+v", report.Acts)
	}
	if err := state.Append(ctx, application.GrindLine{Time: now, Txid: "after", Sender: "s", Status: "answered"}); err != nil {
		t.Fatal(err)
	}
	lines, err := os.ReadFile(filepath.Join(dir, grist.RecordFile))
	if err != nil || !strings.Contains(string(lines), `"txid":"after"`) || !strings.Contains(string(lines), "not json") {
		t.Fatalf("expected the new line at the end, got %q %v", lines, err)
	}
}

// A dry run counts the lines and cuts none; no record is nothing to cut.
func TestADryRunCutsNoRecordAndNoRecordIsNothing(t *testing.T) {
	ctx, dir := context.Background(), t.TempDir()
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	state := grist.New(dir)
	if report, err := (application.GristTidy{Record: state, Now: func() time.Time { return now }}).Run(ctx); err != nil || len(report.Acts) != 0 {
		t.Fatalf("expected nothing, got %v %+v", err, report)
	}
	writeRecord(t, dir, `{"time":"2026-01-01T00:00:00Z","txid":"old"}`)
	report, err := application.GristTidy{Record: state, DryRun: true, Now: func() time.Time { return now }}.Run(ctx)
	if err != nil || len(report.Acts) != 1 {
		t.Fatalf("expected one act, got %v %+v", err, report)
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, grist.RecordFile)); !strings.Contains(string(raw), "old") {
		t.Fatalf("expected the record untouched, got %q", raw)
	}
}
