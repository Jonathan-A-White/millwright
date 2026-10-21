package application_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// matchLog is a stand-in for a run of mw verify match: it records the txids of
// each run, and holds each run until the test lets it return.
type matchLog struct {
	mu      sync.Mutex
	runs    [][]string
	entered chan struct{}
	release chan struct{}
	err     error
}

func newMatchLog() *matchLog {
	return &matchLog{entered: make(chan struct{}, 8), release: make(chan struct{}, 8)}
}

func (m *matchLog) match(ctx context.Context, txids []string) (application.VerifyResult, error) {
	m.mu.Lock()
	m.runs = append(m.runs, slices.Clone(txids))
	m.mu.Unlock()
	m.entered <- struct{}{}
	select {
	case <-m.release:
	case <-ctx.Done():
		return application.VerifyResult{}, ctx.Err()
	}
	return application.VerifyResult{Proposed: make([]application.VerifyProposal, len(txids))}, m.err
}

func (m *matchLog) seen() [][]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.runs)
}

// postPictures posts a Governor message of text with one png to the general
// thread under txid.
func (f *manyFiles) postPicture(t *testing.T, txid, text string) {
	t.Helper()
	f.post(t, txid, map[string]any{"text": text, "attachments": []any{f.add(t, "image/png", pngBytes)}})
}

// Two messages with pictures applied in one pass start one match over both
// txids, and a message without a picture starts none; the pass is over before
// the match returns (mw-4xdnis.4).
func TestFollowerPassMatchesThePicturesItSavedInOneRunApartFromThePass(t *testing.T) {
	f := newManyFiles(t)
	f.postPicture(t, "pic-one", "the green tick")
	f.postPicture(t, "pic-two", "and the blue one")
	f.post(t, "words-only", map[string]any{"text": "just words"})
	log := newMatchLog()
	said := &bytes.Buffer{}
	hook := &application.VerifyHook{Match: log.match, Err: said}
	inbox := f.inbox(&bytes.Buffer{})
	inbox.Pictures = hook
	taps := &application.PosternTaps{Inbox: inbox}

	if err := taps.Tap(context.Background()); err != nil {
		t.Fatalf("the pass: %v", err)
	}
	// The pass has returned while the match is still held.
	select {
	case <-log.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the match never began")
	}
	if got := log.seen(); len(got) != 1 || !slices.Equal(got[0], []string{"pic-one", "pic-two"}) {
		t.Fatalf("expected one run over pic-one and pic-two, got %v", got)
	}
	if note, _ := f.tracker.Note(context.Background(), application.PosternAppliedKey("words-only")); note != "" {
		t.Errorf("expected the words-only message left alone, it was applied: %q", note)
	}
	log.release <- struct{}{}
	hook.Wait()
	if !strings.Contains(said.String(), "verify match pic-one pic-two: 2 proposed") {
		t.Errorf("expected the run said with its count, got %q", said.String())
	}
}

// A picture saved while a run is in flight waits for the next run, which names
// it with the rest; two runs never overlap.
func TestAPictureSavedWhileAMatchRunsIsMatchedInTheNextRun(t *testing.T) {
	log := newMatchLog()
	hook := &application.VerifyHook{Match: log.match}
	ctx := context.Background()

	hook.Saved("a")
	hook.Flush(ctx)
	<-log.entered
	hook.Saved("b")
	hook.Saved("c")
	hook.Saved("b")
	hook.Flush(ctx)
	if got := log.seen(); len(got) != 1 {
		t.Fatalf("expected the second run to wait for the first, got %v", got)
	}
	log.release <- struct{}{}
	<-log.entered
	log.release <- struct{}{}
	hook.Wait()
	got := log.seen()
	if len(got) != 2 || !slices.Equal(got[0], []string{"a"}) || !slices.Equal(got[1], []string{"b", "c"}) {
		t.Fatalf("expected runs over [a] then [b c], got %v", got)
	}
}

// A match that errors leaves the pass's result as it was, and is said once, in one line.
func TestAMatchThatFailsLeavesThePassAloneAndIsSaidInOneLine(t *testing.T) {
	f := newManyFiles(t)
	f.postPicture(t, "pic-fail", "a shot")
	log := newMatchLog()
	log.err = errors.New("the agent could not run")
	log.release <- struct{}{}
	said := &bytes.Buffer{}
	hook := &application.VerifyHook{Match: log.match, Err: said}
	inbox := f.inbox(&bytes.Buffer{})
	inbox.Pictures = hook

	lines, err := inbox.Apply(context.Background())
	hook.Wait()
	if err != nil || len(lines) != 1 {
		t.Fatalf("expected the pass to apply the one message and not fail, got %v (%v)", lines, err)
	}
	if note, _ := f.tracker.Note(context.Background(), application.PosternAppliedKey("pic-fail")); note == "" {
		t.Error("expected the message marked applied whatever the match did")
	}
	if got := strings.Count(said.String(), "\n"); got != 1 || !strings.Contains(said.String(), "verify match pic-fail: the agent could not run") {
		t.Errorf("expected one line naming the txid and the error, got %q", said.String())
	}
}

// Without the hook, the follower's pass leaves a picture message for the inbox,
// as it always did.
func TestFollowerPassLeavesAPictureMessageAloneWithoutTheHook(t *testing.T) {
	f := newManyFiles(t)
	f.postPicture(t, "pic-left", "a shot")
	taps := &application.PosternTaps{Inbox: f.inbox(&bytes.Buffer{})}
	if err := taps.Tap(context.Background()); err != nil {
		t.Fatalf("the pass: %v", err)
	}
	if note, _ := f.tracker.Note(context.Background(), application.PosternAppliedKey("pic-left")); note != "" {
		t.Errorf("expected nothing applied without the hook, got %q", note)
	}
}

// The --apply pass is a short process: it returns only when the match it began
// has ended, so that the match outlives nothing (mw-4xdnis.6).
func TestApplyPassWaitsForTheMatchItBegan(t *testing.T) {
	f := newManyFiles(t)
	f.postPicture(t, "pic-apply", "the green tick")
	log := newMatchLog()
	hook := &application.VerifyHook{Match: log.match}
	inbox := f.inbox(&bytes.Buffer{})
	inbox.Pictures = hook

	returned := make(chan error, 1)
	go func() {
		_, err := inbox.Apply(context.Background())
		returned <- err
	}()
	select {
	case <-log.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the match never began")
	}
	select {
	case err := <-returned:
		t.Fatalf("the pass returned (%v) while the match was still running", err)
	case <-time.After(100 * time.Millisecond):
	}
	log.release <- struct{}{}
	select {
	case err := <-returned:
		if err != nil {
			t.Fatalf("the pass: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the pass never returned after the match ended")
	}
	if got := log.seen(); len(got) != 1 || !slices.Equal(got[0], []string{"pic-apply"}) {
		t.Fatalf("expected one run over pic-apply, got %v", got)
	}
}

// A txid the --apply pass matched is not matched again by the follower's pass,
// and a message without a picture starts no match on either route.
func TestATxidTheApplyPassMatchedIsNotMatchedAgainByTheFollower(t *testing.T) {
	f := newManyFiles(t)
	f.postPicture(t, "pic-once", "the green tick")
	f.post(t, "words-only", map[string]any{"text": "just words"})
	log := newMatchLog()
	log.release <- struct{}{}
	log.release <- struct{}{}
	hook := &application.VerifyHook{Match: log.match}
	inbox := f.inbox(&bytes.Buffer{})
	inbox.Pictures = hook

	if _, err := inbox.Apply(context.Background()); err != nil {
		t.Fatalf("the apply pass: %v", err)
	}
	taps := &application.PosternTaps{Inbox: inbox}
	if err := taps.Tap(context.Background()); err != nil {
		t.Fatalf("the follower's pass: %v", err)
	}
	hook.Wait()
	if got := log.seen(); len(got) != 1 || !slices.Equal(got[0], []string{"pic-once"}) {
		t.Fatalf("expected one run over pic-once alone, got %v", got)
	}
}

// With no hook (realtime = false gives none) the --apply pass starts no match.
func TestApplyPassWithoutTheHookStartsNoMatch(t *testing.T) {
	f := newManyFiles(t)
	f.postPicture(t, "pic-off", "a shot")
	lines, err := f.inbox(&bytes.Buffer{}).Apply(context.Background())
	if err != nil || len(lines) != 1 {
		t.Fatalf("expected the message applied, got %v (%v)", lines, err)
	}
}

// A match that runs past its bound is cut off, said in one line, and leaves the
// --apply pass's result and error as they were.
func TestAMatchPastItsBoundLeavesTheApplyPassAlone(t *testing.T) {
	f := newManyFiles(t)
	f.postPicture(t, "pic-slow", "a shot")
	log := newMatchLog()
	said := &bytes.Buffer{}
	hook := &application.VerifyHook{Match: log.match, Err: said, Timeout: 50 * time.Millisecond, Who: "mw postern inbox --apply"}
	inbox := f.inbox(&bytes.Buffer{})
	inbox.Pictures = hook

	lines, err := inbox.Apply(context.Background())
	if err != nil || len(lines) != 1 {
		t.Fatalf("expected the pass to apply the one message and not fail, got %v (%v)", lines, err)
	}
	if got := strings.Count(said.String(), "\n"); got != 1 || !strings.HasPrefix(said.String(), "mw postern inbox --apply: verify match pic-slow: ") {
		t.Errorf("expected one line from the apply route naming the txid, got %q", said.String())
	}
}
