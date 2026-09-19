package tmux_test

// This test drives a real tmux and closes a real window in it. Like the other
// integration tests here it never touches tmux's default server: every command
// goes to a private server of this test's own (tmux.WithSocket, from
// privateWindows), which is killed when the test ends, and what runs in a
// window is a stand-in script that prints what Claude Code's screen would, never
// a real claude. Nothing here may be changed to run without that.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// aScreen is a stand-in for a session: it prints one line of what Claude Code's
// screen shows, as printf's octal escapes so that no shell reads the bytes, and
// waits to be closed.
func aScreen(t *testing.T, dir, name, line string) string {
	t.Helper()
	program := filepath.Join(dir, name)
	if err := os.WriteFile(program, []byte("#!/bin/sh\nprintf '"+line+"\\n'\nsleep 60\n"), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return program
}

// The three lines a stand-in prints: an empty input line (the prompt mark ❯ and
// a no-break space), the same with a sentence begun, and a working session.
const (
	emptyInput  = `\342\235\257\302\240`
	typedInput  = `\342\235\257\302\240half a sente`
	workingLine = `esc to interrupt`
)

// openStandIn opens a window running a stand-in and returns its id.
func openStandIn(t *testing.T, windows *tmux.Windows, dir, name, line string) string {
	t.Helper()
	ctx := context.Background()
	spec := application.WindowSpec{Name: name, Dir: dir, Command: []string{aScreen(t, dir, name+".sh", line)}}
	if err := windows.Open(ctx, spec); err != nil {
		t.Fatalf("opening %s: %v", name, err)
	}
	return idOf(t, windows, name)
}

func idOf(t *testing.T, windows *tmux.Windows, name string) string {
	t.Helper()
	open, err := windows.OpenWindows(context.Background())
	if err != nil {
		t.Fatalf("listing every window: %v", err)
	}
	for _, window := range open {
		if window.Name == name {
			return window.ID
		}
	}
	t.Fatalf("expected the window %s to be open, got %+v", name, open)
	return ""
}

// waitForPane waits for a pane to show what a stand-in prints.
func waitForPane(t *testing.T, windows *tmux.Windows, id string, want application.PaneState) {
	t.Helper()
	waitFor(t, fmt.Sprintf("the pane of %s to be %s", id, want), func() bool {
		state, err := windows.PaneState(context.Background(), id)
		return err == nil && state == want
	})
}

func TestPaneStateReadsWhatTheScreenSays(t *testing.T) {
	windows, _ := privateWindows(t)
	dir := t.TempDir()

	idle := openStandIn(t, windows, dir, "idle", emptyInput)
	typed := openStandIn(t, windows, dir, "typed", typedInput)
	working := openStandIn(t, windows, dir, "working", workingLine)

	waitForPane(t, windows, idle, application.PaneIdle)
	waitForPane(t, windows, typed, application.PaneInput)
	waitForPane(t, windows, working, application.PaneWorking)
}

func TestOpenWindowsSeesEveryWindowAndDatesIt(t *testing.T) {
	windows, socket := privateWindows(t)
	dir := t.TempDir()

	// Nothing on the server yet: no windows, which is not a failure.
	open, err := windows.OpenWindows(context.Background())
	if err != nil || len(open) != 0 {
		t.Fatalf("expected no windows and no error before a server is running, got %+v, %v", open, err)
	}

	opened := time.Now()
	id := openStandIn(t, windows, dir, "mayor-2026-09-19-12", emptyInput)
	// A window of another session on the same server is seen too, and a name
	// holding the separator is kept whole.
	if out, err := exec.Command(tmux.Program, "-L", socket, "new-session", "-d", "-s", "person", "-n", "a|b", "sleep", "60").CombinedOutput(); err != nil {
		t.Fatalf("opening a person's session: %v\n%s", err, out)
	}

	if open, err = windows.OpenWindows(context.Background()); err != nil {
		t.Fatalf("listing every window: %v", err)
	}
	var names []string
	for _, window := range open {
		names = append(names, window.Name)
		if window.ID == id && (window.Opened.Before(opened.Add(-5*time.Second)) || window.Opened.After(time.Now().Add(time.Second))) {
			t.Errorf("expected %s to be dated around %s, got %s", window.Name, opened.Format(time.RFC3339), window.Opened.Format(time.RFC3339))
		}
	}
	if !strings.Contains(strings.Join(names, ","), "a|b") || !strings.Contains(strings.Join(names, ","), "mayor-2026-09-19-12") {
		t.Errorf("expected the windows of both sessions, got %v", names)
	}
}

func TestThisWindowIsTheWindowOfThePaneTheProcessRunsIn(t *testing.T) {
	windows, socket := privateWindows(t)
	dir := t.TempDir()
	id := openStandIn(t, windows, dir, "mayor-2026-09-19-12", emptyInput)

	t.Setenv(tmux.PaneEnv, "")
	if _, in, err := windows.ThisWindow(context.Background()); err != nil || in {
		t.Fatalf("expected no window outside tmux, got %v, %v", in, err)
	}

	pane, err := exec.Command(tmux.Program, "-L", socket, "list-panes", "-t", id, "-F", "#{pane_id}").Output()
	if err != nil {
		t.Fatalf("asking for the pane of %s: %v", id, err)
	}
	t.Setenv(tmux.PaneEnv, strings.TrimSpace(string(pane)))
	here, in, err := windows.ThisWindow(context.Background())
	if err != nil || !in {
		t.Fatalf("expected to be in a window, got %v, %v", in, err)
	}
	if here.ID != id || here.Name != "mayor-2026-09-19-12" {
		t.Errorf("expected to be in %s (mayor-2026-09-19-12), got %+v", id, here)
	}
}

// reaperOn is a SeatReap on the real terminal and a real vault, pacing itself
// in milliseconds. Its sleep says how many looks were made, and runs what a
// test wants to happen just before one.
func reaperOn(t *testing.T, windows *tmux.Windows, window string, limit time.Duration, before func(look int)) (application.SeatReap, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "seats", "mayor", "handoffs"), 0o755); err != nil {
		t.Fatal(err)
	}
	looks := 0
	files := vault.New(dir)
	return application.SeatReap{
		Seats:    files,
		Terminal: windows,
		Log:      files,
		Seat:     "mayor",
		Host:     "laptop",
		Window:   window,
		Interval: 100 * time.Millisecond,
		Limit:    limit,
		Sleep: func(ctx context.Context, d time.Duration) error {
			time.Sleep(d)
			looks++
			if before != nil {
				before(looks)
			}
			return nil
		},
	}, dir
}

func TestReaperClosesARealIdleWindowOnceTheSeatIsHandedOver(t *testing.T) {
	windows, _ := privateWindows(t)
	scratch := t.TempDir()
	old := openStandIn(t, windows, scratch, "mayor-2026-09-19-12", emptyInput)
	openStandIn(t, windows, scratch, "mayor-2026-09-19-13", emptyInput)
	waitForPane(t, windows, old, application.PaneIdle)

	var vaultDir string
	reap, dir := reaperOn(t, windows, old, 30*time.Second, func(look int) {
		// The successor has the seat from the second look on.
		if look == 2 {
			acting := "Mayor after handoff 12 (tmux window 4 'mayor-2026-09-19-13'), since now\n"
			if err := os.WriteFile(filepath.Join(vaultDir, application.ActingFileName("mayor")), []byte(acting), 0o644); err != nil {
				t.Errorf("writing the acting file: %v", err)
			}
		}
	})
	vaultDir = dir
	before := "Mayor after handoff 11 (tmux window 3 'mayor-2026-09-19-12'), since earlier\n"
	if err := os.WriteFile(filepath.Join(dir, application.ActingFileName("mayor")), []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := reap.Run(context.Background())
	if err != nil {
		t.Fatalf("expected the watch to close the window, got %v (%q)", err, report.Said)
	}
	if report.Outcome != application.ReapClosed {
		t.Errorf("expected the window to be closed, the watch ended %q", report.Outcome)
	}

	open, err := windows.OpenWindows(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, window := range open {
		names = append(names, window.Name)
	}
	if strings.Contains(strings.Join(names, ","), "mayor-2026-09-19-12") {
		t.Errorf("expected the old window to be closed, still open: %v", names)
	}
	if !strings.Contains(strings.Join(names, ","), "mayor-2026-09-19-13") {
		t.Errorf("expected the successor's window to be left open, open: %v", names)
	}

	logged, err := os.ReadFile(filepath.Join(dir, application.ReapLogFileName("mayor")))
	if err != nil {
		t.Fatalf("reading the reaper log: %v", err)
	}
	if lines := strings.Split(strings.TrimSpace(string(logged)), "\n"); len(lines) != 2 || !strings.Contains(lines[1], "closed") {
		t.Errorf("expected an armed line and a closed line, got:\n%s", logged)
	}
}

func TestReaperNeverClosesARealWindowWithTextOnItsInputLine(t *testing.T) {
	windows, _ := privateWindows(t)
	typed := openStandIn(t, windows, t.TempDir(), "mayor-2026-09-19-12", typedInput)
	waitForPane(t, windows, typed, application.PaneInput)

	reap, dir := reaperOn(t, windows, typed, 1500*time.Millisecond, nil)
	reap.WhenIdle = true
	handoff := filepath.Join(dir, "seats", "mayor", "handoffs", "2026-09-19-12.md")
	if err := os.WriteFile(handoff, []byte("left for the next\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := reap.Run(context.Background())
	if err == nil || report.Outcome != application.ReapGaveUp {
		t.Fatalf("expected the watch to give up, it ended %q with %v", report.Outcome, err)
	}
	if _, err := windows.PaneState(context.Background(), typed); err != nil {
		t.Errorf("expected the window with text on its input line to be left open: %v", err)
	}
}
