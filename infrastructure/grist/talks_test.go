package grist_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/grist"
)

// A talk's record is kept under talks/, private, read back as it was written,
// and listed; another sender's talk of the same name is another record.
func TestTalksKeepARecordPerAppSenderAndTalk(t *testing.T) {
	dir := t.TempDir()
	talks := &grist.Talks{Dir: dir, Projects: filepath.Join(dir, "projects")}
	ctx := context.Background()
	key := application.GristTalkKey{App: "lampas", Sender: "abcd ef01 2345 6789", Talk: "rom.8@2026-10-21T12:00:00Z"}
	if _, found, err := talks.Load(ctx, key); err != nil || found {
		t.Fatalf("an empty store found %v, %v", found, err)
	}
	when := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	record := application.GristTalk{GristTalkKey: key, Session: "s-1", Turns: 2, LastUsed: when, Ended: []string{"s-0"}}
	if err := talks.Save(ctx, record); err != nil {
		t.Fatal(err)
	}
	other := key
	other.Sender = "ffff ffff ffff ffff"
	if err := talks.Save(ctx, application.GristTalk{GristTalkKey: other, Session: "s-9"}); err != nil {
		t.Fatal(err)
	}
	got, found, err := talks.Load(ctx, key)
	if err != nil || !found || got.Session != "s-1" || got.Turns != 2 || !got.LastUsed.Equal(when) || got.Ended[0] != "s-0" {
		t.Fatalf("read back %+v, %v, %v", got, found, err)
	}
	all, err := talks.List(ctx)
	if err != nil || len(all) != 2 {
		t.Fatalf("listed %d records, %v", len(all), err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, grist.TalksDir))
	for _, e := range entries {
		info, _ := e.Info()
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s is %v, not 0600", e.Name(), info.Mode().Perm())
		}
	}
}

// Each app's talks run from one directory under cwd/, made when missing; a
// name that would leave it is refused.
func TestTalksCwdIsOneDirectoryPerApp(t *testing.T) {
	dir := t.TempDir()
	talks := &grist.Talks{Dir: dir}
	cwd, err := talks.Cwd("lampas")
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := filepath.Abs(filepath.Join(dir, grist.TalkCwdDir, "lampas")); cwd != want {
		t.Fatalf("lampas's talk directory is %s, not %s", cwd, want)
	}
	if info, err := os.Stat(cwd); err != nil || !info.IsDir() {
		t.Fatalf("the directory was not made: %v", err)
	}
	for _, bad := range []string{"", "..", "a/b"} {
		if _, err := talks.Cwd(bad); err == nil {
			t.Fatalf("the app %q was given a directory", bad)
		}
	}
}

// Claude Code keeps a session run from /home/x/.local/state at
// projects/-home-x--local-state/<id>.jsonl: the transcript is looked for there.
func TestTalksFindTheTranscriptWhereClaudeKeepsIt(t *testing.T) {
	projects := t.TempDir()
	talks := &grist.Talks{Projects: projects}
	path := grist.TranscriptPath(projects, "/home/x/.local/state", "s-1")
	if want := filepath.Join(projects, "-home-x--local-state", "s-1.jsonl"); path != want {
		t.Fatalf("the transcript is looked for at %s, not %s", path, want)
	}
	if kept, err := talks.Transcript("/home/x/.local/state", "s-1"); err != nil || kept {
		t.Fatalf("a missing transcript was found: %v, %v", kept, err)
	}
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.WriteFile(path, []byte("{}\n"), 0o600)
	if kept, err := talks.Transcript("/home/x/.local/state", "s-1"); err != nil || !kept {
		t.Fatalf("the transcript was not found: %v, %v", kept, err)
	}
}

// A transcript is deleted where Claude Code keeps it; one already gone is no
// error, and a session name that is a path is refused.
func TestTalksRemoveATranscript(t *testing.T) {
	projects := t.TempDir()
	talks := &grist.Talks{Projects: projects}
	path := grist.TranscriptPath(projects, "/home/x/.local/state", "s-1")
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.WriteFile(path, []byte("{}\n"), 0o600)
	if err := talks.RemoveTranscript("/home/x/.local/state", "s-1"); err != nil {
		t.Fatal(err)
	}
	if kept, _ := talks.Transcript("/home/x/.local/state", "s-1"); kept {
		t.Fatal("the transcript is still there")
	}
	if err := talks.RemoveTranscript("/home/x/.local/state", "s-1"); err != nil {
		t.Fatalf("a transcript already gone is an error: %v", err)
	}
	for _, session := range []string{"", "../s-1", "a/b", ".."} {
		if err := talks.RemoveTranscript("/home/x/.local/state", session); err == nil {
			t.Errorf("the session %q was accepted", session)
		}
	}
}

// Remove deletes one talk's record and no other, and a record already gone is
// no error (the eval and smoke end their talks with it).
func TestTalksRemoveDeletesOneRecord(t *testing.T) {
	dir := t.TempDir()
	talks := &grist.Talks{Dir: dir}
	ctx := context.Background()
	key := application.GristTalkKey{App: "lampas", Sender: "abcd", Talk: "eval:x:00000000"}
	other := application.GristTalkKey{App: "lampas", Sender: "abcd", Talk: "rom.8@1"}
	for _, k := range []application.GristTalkKey{key, other} {
		if err := talks.Save(ctx, application.GristTalk{GristTalkKey: k, Session: "s-1"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := talks.Remove(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err := talks.Remove(ctx, key); err != nil {
		t.Fatalf("removing a record twice: %v", err)
	}
	all, err := talks.List(ctx)
	if err != nil || len(all) != 1 || all[0].Talk != "rom.8@1" {
		t.Fatalf("expected only the other talk left, got %+v, %v", all, err)
	}
}
