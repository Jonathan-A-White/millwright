package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func runMw(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

// mw hands add keeps the step in the bead's hands note, comments it, and
// labels the bead hitl — each one bd call — printing the step's hash.
func TestHandsAddWritesTheStepCommentsAndLabels(t *testing.T) {
	posternHome(t, "http://unused", "", "")
	log := bdRecording(t, `[{"id": "mw-f758y.8", "title": "Linger", "status": "open", "issue_type": "task"}]`)

	out, err := runMw(t, "hands", "add", "mw-f758y.8", "--id", "linger", "--host", "desktop", "--as", "root",
		"--way-back", "loginctl disable-linger jwhite", "--", "loginctl enable-linger jwhite")
	if err != nil {
		t.Fatalf("mw hands add failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "sha256 ") {
		t.Fatalf("expected the step's hash printed, got %q", out)
	}
	calls, _ := os.ReadFile(log)
	for _, want := range []string{
		`kv set hands.mw-f758y.8 [{"id":"linger","host":"desktop","as":"root","run":"loginctl enable-linger jwhite","way_back":"loginctl disable-linger jwhite","added_at":"`,
		"comment mw-f758y.8 HANDS STEP linger on desktop as root:",
		"update mw-f758y.8 --add-label hitl",
	} {
		if !strings.Contains(string(calls), want) {
			t.Errorf("expected bd to be asked %q, got:\n%s", want, calls)
		}
	}
}

// The commands go after --, as one argument: anything else is refused before
// bd is asked anything.
func TestHandsAddTakesTheCommandsAsOneArgument(t *testing.T) {
	posternHome(t, "http://unused", "", "")
	log := bdRecording(t, `[]`)

	if out, err := runMw(t, "hands", "add", "mw-f758y.8", "--id", "x", "--host", "desktop", "--as", "user", "--", "sudo", "reboot"); err == nil {
		t.Fatalf("expected an unquoted command refused, got %q", out)
	}
	if calls, _ := os.ReadFile(log); len(calls) != 0 {
		t.Fatalf("expected no bd call, got %s", calls)
	}
}
