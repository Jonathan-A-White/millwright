package tmux_test

// A tmux command that never answers must not hold the follower (mw-gq6.884):
// on 2026-11-02 one `tmux send-keys` sat in poll for 32 minutes, and with it
// the follower's beads, mail and nudges. These tests put a stand-in for tmux
// in the follower's place that hangs, and see each call end at its deadline.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"
	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"
)

// hangDeadline is the deadline the tests give a call; longWait is the longest
// a test waits for a call that should have ended at it.
const (
	hangDeadline = 200 * time.Millisecond
	longWait     = 5 * time.Second
	// slowHangDeadline is for a test whose calls that answer must not be read as
	// hung on a loaded host: a fake tmux is a shell script, and a host running
	// other gates can take more than hangDeadline to start one.
	slowHangDeadline = 2 * time.Second
)

// fakeTmux writes a stand-in for tmux that answers list-windows with one idle
// seat window and capture-pane with an empty input line, and runs hang for any
// other command. hang is shell, with $dir the directory the script lives in.
func fakeTmux(t *testing.T, hang string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "tmux")
	body := "#!/bin/sh\ndir=" + dir + "\n" +
		"while [ \"$1\" = -L ]; do shift 2; done\n" +
		"case \"$1\" in\n" +
		"list-windows) echo '@7||deputy-2026-11-02-01';;\n" +
		"capture-pane) printf '❯ \\n';;\n" +
		"*) " + hang + ";;\n" +
		"esac\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

// within runs f and fails the test if it has not returned within longWait.
func within(t *testing.T, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); f() }()
	select {
	case <-done:
	case <-time.After(longWait):
		t.Fatalf("%s did not return within %s of a %s deadline", what, longWait, hangDeadline)
	}
}

func TestEveryTmuxCallEndsAtItsDeadlineWhenTmuxNeverAnswers(t *testing.T) {
	program := fakeTmux(t, "exec sleep 60")
	windows := tmux.NewWindows(tmux.WithProgram(program), tmux.WithCallDeadline(hangDeadline))
	runner := tmux.New(tmux.WithProgram(program), tmux.WithCallDeadline(hangDeadline))
	terminal := tmux.NewTerminal(tmux.WithProgram(program), tmux.WithCallDeadline(hangDeadline))
	ctx := context.Background()
	calls := map[string]func() error{
		"Windows.Type":   func() error { return windows.Type(ctx, "@7", "hello") },
		"Windows.Enter":  func() error { return windows.Enter(ctx, "@7") },
		"Windows.Close":  func() error { return windows.Close(ctx, "@7") },
		"Windows.Rename": func() error { return windows.Rename(ctx, "@7", "x") },
		"Runner.Close":   func() error { return runner.Close(ctx, "mw-gq6_4") },
		"Runner.Status":  func() error { _, err := runner.Status(ctx, "mw-gq6_4"); return err },
		"Terminal.Type":  func() error { return terminal.Type(ctx, "probe", "hello") },
		"Terminal.Press": func() error { return terminal.Press(ctx, "probe", "Enter") },
	}
	for name, call := range calls {
		var err error
		start := time.Now()
		within(t, name, func() { err = call() })
		if err == nil {
			t.Errorf("%s: a tmux that never answered gave no error", name)
		} else if !strings.Contains(err.Error(), "did not finish") {
			t.Errorf("%s: the error does not say the call ran out of time: %v", name, err)
		}
		if took := time.Since(start); took > 3*time.Second {
			t.Errorf("%s: took %s against a %s deadline", name, took, hangDeadline)
		}
	}
}

// The follower's loop goes on past a nudge whose tmux hangs: the next pass
// reads the beads' head again and publishes the view.
func TestTheFollowerReadsTheBeadsAgainAfterANudgeWhoseTmuxHangs(t *testing.T) {
	program := fakeTmux(t, "exec sleep 60")
	w := newNudgeWorld(program, hangDeadline)
	appendEvents(t, w.log, events.Event{Kind: events.KindMail, Bead: "mw-m1", Detail: "deputy"})
	if err := w.nudge.Nudge(context.Background()); err != nil { // the seat starts at the head
		t.Fatal(err)
	}
	appendEvents(t, w.log, events.Event{Kind: events.KindMail, Bead: "mw-m2", Detail: "deputy"})

	ctx, stop := context.WithTimeout(context.Background(), longWait)
	defer stop()
	head := &apptest.FakeHead{Heads: []string{"a", "b", "c"}}
	var published atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- application.EventFollow{
			Head:    head,
			Log:     w.log,
			Feed:    apptest.NewFakeTracker(),
			Cursors: &apptest.FakeFollowCursors{},
			Nudger:  w.nudge,
			Publish: func(context.Context) error { published.Add(1); return nil },
			Sleep: func(ctx context.Context, _ time.Duration) error {
				if published.Load() >= 3 {
					stop()
				}
				return ctx.Err()
			},
			Err: &w.err,
		}.Run(ctx)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(longWait + time.Second):
		t.Fatal("the follower is held by a tmux that never answers")
	}
	if got := published.Load(); got < 3 {
		t.Fatalf("the follower published %d views over three heads, said %q", got, w.err.String())
	}
}

// A nudge that times out is said on the log and is told again at the next
// nudge: the mail is not marked told and no event is lost.
func TestANudgeThatTimedOutIsSaidAndRetriedAtTheNextNudge(t *testing.T) {
	// The first send-keys hangs, the ones after it work.
	program := fakeTmux(t, `if [ -e "$dir/hung-once" ]; then echo "$@" >> "$dir/typed"; else touch "$dir/hung-once"; exec sleep 60; fi`)
	w := newNudgeWorld(program, slowHangDeadline)
	ctx := context.Background()
	appendEvents(t, w.log, events.Event{Kind: events.KindMail, Bead: "mw-m1", Detail: "deputy"})
	if err := w.nudge.Nudge(ctx); err != nil {
		t.Fatal(err)
	}
	appendEvents(t, w.log, events.Event{Kind: events.KindMail, Bead: "mw-m2", Detail: "deputy"})

	within(t, "the nudge", func() {
		if err := w.nudge.Nudge(ctx); err != nil {
			t.Error(err)
		}
	})
	if said := w.err.String(); !strings.Contains(said, "typing the nudge into deputy-2026-11-02-01") || !strings.Contains(said, "did not finish") {
		t.Fatalf("the timed-out nudge was not said on the log: %q", said)
	}
	if cur, _ := w.cursors.Load(ctx); cur["deputy"] != 1 {
		t.Fatalf("the seat's cursor moved to %d after a nudge that did not get typed, want it left at 1", cur["deputy"])
	}

	within(t, "the next nudge", func() {
		if err := w.nudge.Nudge(ctx); err != nil {
			t.Error(err)
		}
	})
	typed, _ := os.ReadFile(filepath.Join(filepath.Dir(program), "typed"))
	if !strings.Contains(string(typed), "New mail for deputy: 1 message(s): mw-m2.") {
		t.Fatalf("the next nudge did not tell the mail again: typed %q", typed)
	}
	if cur, _ := w.cursors.Load(ctx); cur["deputy"] != 2 {
		t.Fatalf("the cursor is %d after the retry went through, want 2", cur["deputy"])
	}
}

type nudgeWorldT struct {
	log     *apptest.FakeEventLog
	cursors *apptest.FakeNudgeCursors
	err     bytes.Buffer
	nudge   *application.EventNudge
}

// newNudgeWorld is an EventNudge for the deputy over the real Windows adapter
// run on program, in place of tmux.
func newNudgeWorld(program string, deadline time.Duration) *nudgeWorldT {
	w := &nudgeWorldT{log: &apptest.FakeEventLog{}, cursors: &apptest.FakeNudgeCursors{}}
	subs := &apptest.FakeSubscribeFiles{}
	subs.Set("deputy", `kinds = ["mail"]`)
	w.nudge = &application.EventNudge{
		Log:      w.log,
		Subs:     subs,
		Cursors:  w.cursors,
		Terminal: tmux.NewWindows(tmux.WithProgram(program), tmux.WithCallDeadline(deadline)),
		Err:      &w.err,
	}
	return w
}

func appendEvents(t *testing.T, log *apptest.FakeEventLog, evs ...events.Event) {
	t.Helper()
	for _, e := range evs {
		e.Ts = time.Date(2026, 11, 2, 13, 0, 0, 0, time.UTC)
		e.Lane = events.LaneNormal
		if _, err := log.Append(context.Background(), []events.Event{e}); err != nil {
			t.Fatal(err)
		}
	}
}
