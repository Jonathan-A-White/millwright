package main

import (
	"context"

	"bytes"
	"fmt"
	"github.com/Jonathan-A-White/millwright/application"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These run mw seat reap against a tmux server that is not there: the socket
// is named for this test alone, so nothing here can reach the default server,
// which holds the person's windows.
func reapCmd(t *testing.T, vault string, args ...string) (string, error) {
	t.Helper()
	mwConfig(t, "vault = \""+vault+"\"\nhost = \"laptop\"\n")
	t.Setenv(TmuxSocketEnv, fmt.Sprintf("mw-test-%d-%d", os.Getpid(), time.Now().UnixNano()))
	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"seat", "reap", "mayor"}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestSeatReapNamesAWindowOrIsRefused(t *testing.T) {
	vault := t.TempDir()
	out, err := reapCmd(t, vault)
	if err == nil || !strings.Contains(err.Error(), "window") {
		t.Fatalf("expected a refusal naming the window, got %q, %v", out, err)
	}
	if _, statErr := os.Stat(filepath.Join(vault, ".mayor-reaper.log")); statErr == nil {
		t.Errorf("expected a refused watch to log nothing")
	}
}

func TestSeatReapLogsAWindowThatIsAlreadyGone(t *testing.T) {
	vault := t.TempDir()
	out, err := reapCmd(t, vault, "--window", "@9", "--interval", "10ms", "--limit", "5s")
	if err != nil {
		t.Fatalf("expected a window that is gone to end the watch cleanly, got %v", err)
	}
	if !strings.Contains(out, "reap @9: the window is already gone") {
		t.Errorf("expected the watch to say the window is gone, got %q", out)
	}
	logged, err := os.ReadFile(filepath.Join(vault, ".mayor-reaper.log"))
	if err != nil {
		t.Fatalf("reading the reaper log: %v", err)
	}
	if lines := strings.Split(strings.TrimSpace(string(logged)), "\n"); len(lines) != 2 {
		t.Errorf("expected an armed line and an outcome line, got:\n%s", logged)
	}
}

func TestSeatReapGivesUpWithAnErrorSoTheCommandExitsNonZero(t *testing.T) {
	// The server is not there, so the window is gone at the first look; to give
	// up the watch must be out of time before it looks, which a limit shorter
	// than an interval does.
	vault := t.TempDir()
	out, err := reapCmd(t, vault, "--window", "@9", "--interval", "50ms", "--limit", "1ns")
	if err == nil || !strings.Contains(err.Error(), "gave up") {
		t.Fatalf("expected the watch to give up, got %q, %v", out, err)
	}
}

func actingOf(t *testing.T, files interface {
	SeatStart(ctx context.Context, seat, host string) (application.SeatStart, error)
}, seat, host string) string {
	t.Helper()
	start, err := files.SeatStart(context.Background(), seat, host)
	if err != nil {
		t.Fatalf("reading the %s seat's start: %v", seat, err)
	}
	return start.Acting
}

func writeActing(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSeatReapReadsTheMillhandsPerHostActingFile(t *testing.T) {
	dir := t.TempDir()
	writeActing(t, dir, ".millhand-acting", "the shared file")
	writeActing(t, dir, ".millhand-laptop-acting", "millhand-7 on the laptop")
	files, err := seatReapFiles(dir, "millhand", "laptop", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := actingOf(t, files, "millhand", "laptop"); got != "millhand-7 on the laptop" {
		t.Errorf("expected the per-host acting file for the millhand seat, got %q", got)
	}
}

func TestSeatReapActingFlagOverridesTheActingFile(t *testing.T) {
	dir := t.TempDir()
	writeActing(t, dir, ".millhand-laptop-acting", "millhand-7 on the laptop")
	writeActing(t, dir, ".other-acting", "named by the flag")
	files, err := seatReapFiles(dir, "millhand", "laptop", ".other-acting")
	if err != nil {
		t.Fatal(err)
	}
	if got := actingOf(t, files, "millhand", "laptop"); got != "named by the flag" {
		t.Errorf("expected --acting to override, got %q", got)
	}
	files, err = seatReapFiles(dir, "mayor", "laptop", filepath.Join(dir, ".other-acting"))
	if err != nil {
		t.Fatal(err)
	}
	if got := actingOf(t, files, "mayor", "laptop"); got != "named by the flag" {
		t.Errorf("expected --acting as a full path to override, got %q", got)
	}
}

func TestSeatReapOtherSeatsKeepTheirOwnActingFile(t *testing.T) {
	dir := t.TempDir()
	writeActing(t, dir, ".mayor-acting", "mayor-3")
	writeActing(t, dir, ".mayor-laptop-acting", "not this one")
	files, err := seatReapFiles(dir, "mayor", "laptop", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := actingOf(t, files, "mayor", "laptop"); got != "mayor-3" {
		t.Errorf("expected the mayor to keep .mayor-acting, got %q", got)
	}
}

func TestSeatReapActingFlagIsRefusedWhenItNamesADirectoryUp(t *testing.T) {
	if _, err := seatReapFiles(t.TempDir(), "mayor", "laptop", "../x"); err == nil {
		t.Errorf("expected a refusal of an acting file outside the vault")
	}
}
