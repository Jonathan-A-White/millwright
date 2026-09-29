package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// fakeHandsPush swaps the sender mw hands add pushes through for a fake, so
// no test reaches a postern backend.
func fakeHandsPush(t *testing.T, err error) *apptest.FakePosternSender {
	t.Helper()
	fake := &apptest.FakePosternSender{Err: err}
	real := newHandsPush
	newHandsPush = func(*beads.Gateway) application.PosternSender { return fake }
	t.Cleanup(func() { newHandsPush = real })
	return fake
}

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
	fakeHandsPush(t, nil)
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

const handsAddBead = `[{"id": "mw-f758y.8", "title": "Linger", "status": "open", "issue_type": "task"}]`

func handsAddArgs(extra ...string) []string {
	args := []string{"hands", "add", "mw-f758y.8", "--id", "linger", "--host", "desktop", "--as", "root"}
	return append(append(args, extra...), "--", "loginctl enable-linger jwhite")
}

// mw hands add sends one push, of a class that opens Needs you, on the bead's
// thread.
func TestHandsAddSendsOnePushOnTheBeadsThread(t *testing.T) {
	posternHome(t, "http://unused", "", "")
	push := fakeHandsPush(t, nil)
	bdRecording(t, handsAddBead)

	if out, err := runMw(t, handsAddArgs()...); err != nil {
		t.Fatalf("mw hands add failed: %v\n%s", err, out)
	}
	sent := push.Sent()
	if len(sent) != 1 {
		t.Fatalf("expected one push, got %+v", sent)
	}
	if sent[0].Class != "decision-needed" || sent[0].Thread != "mw-f758y.8" || sent[0].Text != "New hands step on mw-f758y.8: Linger" {
		t.Errorf("unexpected push %+v", sent[0])
	}
}

// A push that fails is said on the output and on the bead, and the step is
// still kept: exit 0.
func TestHandsAddKeepsTheStepWhenThePushFails(t *testing.T) {
	posternHome(t, "http://unused", "", "")
	fakeHandsPush(t, fmt.Errorf("the backend is down"))
	log := bdRecording(t, handsAddBead)

	out, err := runMw(t, handsAddArgs()...)
	if err != nil {
		t.Fatalf("expected exit 0 with a failed push, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "push to the Governor failed") || !strings.Contains(out, "the backend is down") {
		t.Errorf("expected the failed push said, got %q", out)
	}
	calls, _ := os.ReadFile(log)
	for _, want := range []string{"kv set hands.mw-f758y.8 ", "comment mw-f758y.8 PUSH FAILED"} {
		if !strings.Contains(string(calls), want) {
			t.Errorf("expected bd to be asked %q, got:\n%s", want, calls)
		}
	}
}

func TestHandsAddNoPushSendsNothing(t *testing.T) {
	posternHome(t, "http://unused", "", "")
	push := fakeHandsPush(t, nil)
	bdRecording(t, handsAddBead)

	if out, err := runMw(t, handsAddArgs("--no-push")...); err != nil {
		t.Fatalf("mw hands add failed: %v\n%s", err, out)
	}
	if sent := push.Sent(); len(sent) != 0 {
		t.Fatalf("expected no push, got %+v", sent)
	}
}
