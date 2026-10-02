package peekremote

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// standIn puts an ssh on PATH that records its arguments and prints one line.
func standIn(t *testing.T, exit int) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + log + "\necho host: desktop\necho oops >&2\nexit " + string(rune('0'+exit)) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func TestPeekRunsMwPeekHereOnTheHost(t *testing.T) {
	log := standIn(t, 0)
	got, err := Remote{Reach: map[string]string{"desktop": "ssh desktop"}}.Peek(context.Background(), "desktop", "mw-3evcnk.1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "host: desktop\n" {
		t.Fatalf("printed %q", got)
	}
	args, _ := os.ReadFile(log)
	want := "-o\nBatchMode=yes\ndesktop\nbash -lc 'mw peek --here '\\''mw-3evcnk.1'\\'''\n"
	if string(args) != want {
		t.Fatalf("ssh got\n%s\nwant\n%s", args, want)
	}
}

func TestPeekSaysWhenAHostCannotBeReached(t *testing.T) {
	if _, err := (Remote{}).Peek(context.Background(), "desktop", "mw-1"); err == nil || !strings.Contains(err.Error(), "[hands_hosts]") {
		t.Fatalf("expected a refusal naming [hands_hosts], got %v", err)
	}
	standIn(t, 1)
	_, err := Remote{Reach: map[string]string{"desktop": "ssh desktop"}}.Peek(context.Background(), "desktop", "mw-1")
	if err == nil || !strings.Contains(err.Error(), "oops") {
		t.Fatalf("expected ssh's stderr in the error, got %v", err)
	}
}
