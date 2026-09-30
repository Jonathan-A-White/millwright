package main

import (
	"bytes"
	"context"
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

// fakeHandsView is the view mw hands add publishes, counting its runs.
type fakeHandsView struct{ runs int }

func (v *fakeHandsView) Run(context.Context) (application.PosternViewDoc, error) {
	v.runs++
	return application.PosternViewDoc{}, nil
}

// freeViewLock is the notifier's lock with nobody holding it.
type freeViewLock struct{}

func (freeViewLock) TryTake(context.Context) (func(), bool, error) { return func() {}, true, nil }

// swapHandsView replaces the view and the notifier's lock mw hands add uses,
// so no test writes the real view or takes the real lock.
func swapHandsView(t *testing.T) *fakeHandsView {
	t.Helper()
	fake := &fakeHandsView{}
	realView, realLock := newHandsView, newHandsViewLock
	newHandsView = func(*beads.Gateway, string) application.PosternViewPublisher { return fake }
	newHandsViewLock = func() application.ViewLock { return freeViewLock{} }
	t.Cleanup(func() { newHandsView, newHandsViewLock = realView, realLock })
	return fake
}

// mw hands add publishes the live view once the step is kept, and --no-view
// opts out.
func TestHandsAddPublishesTheViewUnlessNoView(t *testing.T) {
	posternHome(t, "http://unused", "", "")
	fakeHandsPush(t, nil)
	view := swapHandsView(t)
	bdRecording(t, handsAddBead)

	if out, err := runMw(t, handsAddArgs()...); err != nil {
		t.Fatalf("mw hands add failed: %v\n%s", err, out)
	}
	if view.runs != 1 {
		t.Fatalf("expected the view published once, got %d", view.runs)
	}
	if out, err := runMw(t, handsAddArgs("--replace", "--no-view")...); err != nil {
		t.Fatalf("mw hands add --no-view failed: %v\n%s", err, out)
	}
	if view.runs != 1 {
		t.Fatalf("expected --no-view to publish nothing more, got %d runs", view.runs)
	}
}

// --after makes each named bead block the step's bead before the step is
// kept, and the push says what the step waits on.
func TestHandsAddAfterBlocksTheBeadFirst(t *testing.T) {
	posternHome(t, "http://unused", "", "")
	push := fakeHandsPush(t, nil)
	swapHandsView(t)
	log := bdRecording(t, `[{"id": "mw-f758y.8", "title": "Linger", "status": "open", "issue_type": "task"},`+
		`{"id": "mw-f758y.7", "title": "Fetch the key", "status": "open", "issue_type": "task"}]`)

	if out, err := runMw(t, handsAddArgs("--after", "mw-f758y.7")...); err != nil {
		t.Fatalf("mw hands add --after failed: %v\n%s", err, out)
	}
	calls, _ := os.ReadFile(log)
	dep := strings.Index(string(calls), "dep add mw-f758y.8 mw-f758y.7")
	kept := strings.Index(string(calls), "kv set hands.mw-f758y.8 ")
	if dep < 0 || kept < 0 || dep > kept {
		t.Errorf("expected bd dep add before the step is kept, got:\n%s", calls)
	}
	if sent := push.Sent(); len(sent) != 1 || sent[0].Text != "New hands step on mw-f758y.8: Linger (waits on Fetch the key)" {
		t.Errorf("expected the push to say what the step waits on, got %+v", sent)
	}
}

func TestHandsAddHelpListsAfterAndNoView(t *testing.T) {
	out, err := runMw(t, "hands", "add", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--after", "--no-view"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected mw hands add --help to list %s, got:\n%s", want, out)
		}
	}
}
