package application

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// perDirCheckout is a checkout that moves every directory once, safe to call
// from several levels at once.
type perDirCheckout struct {
	mu    sync.Mutex
	moved map[string]bool
}

func (*perDirCheckout) Fetch(context.Context, string) error { return nil }
func (*perDirCheckout) Tip(context.Context, string, string, string) (string, error) {
	return "new", nil
}
func (c *perDirCheckout) Head(_ context.Context, dir string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.moved[dir] {
		return "new", nil
	}
	return "old", nil
}
func (*perDirCheckout) Uncommitted(context.Context, string) ([]string, error) { return nil, nil }
func (c *perDirCheckout) Advance(_ context.Context, dir, _, _ string) (Advanced, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.moved == nil {
		c.moved = map[string]bool{}
	}
	if c.moved[dir] {
		return Advanced{Level: true}, nil
	}
	c.moved[dir] = true
	return Advanced{Moved: true}, nil
}

// slowSmoker holds the app named slow until release is closed; every other app
// finds well at once. It records the apps that finished.
type slowSmoker struct {
	slow    string
	release chan struct{}
	mu      sync.Mutex
	done    []string
}

func (s *slowSmoker) Run(ctx context.Context, app, _ string) (GristSmokeReport, error) {
	if app == s.slow {
		select {
		case <-s.release:
		case <-ctx.Done():
		}
	}
	s.mu.Lock()
	s.done = append(s.done, app)
	s.mu.Unlock()
	return GristSmokeReport{App: app, Examples: 1}, nil
}

func (s *slowSmoker) finished() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.done...)
}

// mw-gq6.816: a slow app's smoke (lampas, up to an hour) does not hold back the
// levelling of the apps after it by name: they are levelled and smoked while it
// still runs.
func TestASlowAppsLevelDoesNotHoldBackAnotherApps(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	apps := map[string]string{"alpha": "/home/alpha", "zeta": "/home/zeta"}
	smoker := &slowSmoker{slow: "alpha", release: make(chan struct{})}
	checkout := &perDirCheckout{}
	var out lockedBuffer
	level := GristLevel{
		Apps: apps, Rigs: apps, Checkout: checkout,
		Smoke: GristSmokeAfter{
			Touches: touchesOnly{GristSmokeGrindsPath: true}, Smoke: smoker, Apps: apps, Rigs: apps,
			Book: GristSmokeBook{Notes: &lockedNotes{notes: smokeNoteStore{}}, Host: "desktop"},
		},
		Host: "desktop", Out: &out,
	}
	ran := make(chan error, 1)
	go func() { ran <- level.Run(ctx) }()

	// zeta sorts after alpha, so a serial pass would still be inside alpha's smoke.
	for deadline := time.Now().Add(10 * time.Second); len(smoker.finished()) == 0; {
		if time.Now().After(deadline) {
			t.Fatalf("zeta was not smoked while alpha's smoke ran; log:\n%s", out.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := fmt.Sprint(smoker.finished()); got != "[zeta]" {
		t.Fatalf("expected only zeta done while alpha is held, got %s", got)
	}
	select {
	case err := <-ran:
		t.Fatalf("Run ended (%v) before the slow smoke was released", err)
	default:
	}
	close(smoker.release)
	if err := <-ran; err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := fmt.Sprint(smoker.finished()); got != "[zeta alpha]" {
		t.Fatalf("expected both smoked, got %s", got)
	}
	if !strings.Contains(out.String(), "alpha checkout") || !strings.Contains(out.String(), "zeta checkout") {
		t.Fatalf("expected both checkouts said to have moved, got:\n%s", out.String())
	}
}

// A level that fails does not stop the others, and the pass names each failure
// in the order of the apps' names.
func TestAFailedLevelDoesNotStopTheOthersAndIsNamedInOrder(t *testing.T) {
	apps := map[string]string{"alpha": "/home/alpha", "beta": "/home/beta", "gamma": "/home/gamma"}
	checkout := &failingFetch{bad: map[string]bool{"/home/alpha": true, "/home/gamma": true}, perDirCheckout: &perDirCheckout{}}
	level := GristLevel{Apps: apps, Rigs: apps, Checkout: checkout, Host: "desktop"}
	err := level.Run(context.Background())
	if err == nil {
		t.Fatal("expected the pass to fail")
	}
	want := "the checkout of alpha: fetching: no network; gamma: fetching: no network could not be brought level"
	if err.Error() != want {
		t.Fatalf("got %q, want %q", err.Error(), want)
	}
	if !checkout.moved["/home/beta"] {
		t.Fatal("beta was not levelled after alpha failed")
	}
}

type failingFetch struct {
	*perDirCheckout
	bad map[string]bool
}

func (f *failingFetch) Fetch(_ context.Context, dir string) error {
	if f.bad[dir] {
		return fmt.Errorf("no network")
	}
	return nil
}

// lockedBuffer is a strings.Builder that several levels may write to.
type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// lockedNotes is smokeNoteStore for levels that write notes side by side.
type lockedNotes struct {
	mu    sync.Mutex
	notes smokeNoteStore
}

func (l *lockedNotes) Note(ctx context.Context, k string) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.notes.Note(ctx, k)
}
func (l *lockedNotes) SetNote(ctx context.Context, k, v string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.notes.SetNote(ctx, k, v)
}
func (l *lockedNotes) ClearNote(ctx context.Context, k string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.notes.ClearNote(ctx, k)
}
func (l *lockedNotes) NotesWithPrefix(ctx context.Context, p string) (map[string]string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.notes.NotesWithPrefix(ctx, p)
}
