package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

func heldLog(t *testing.T, lines ...string) *apptest.FakeTickLog {
	t.Helper()
	log := &apptest.FakeTickLog{}
	for _, line := range lines {
		if err := log.Append(context.Background(), line); err != nil {
			t.Fatalf("appending %q: %v", line, err)
		}
	}
	return log
}

func TestDispatchOutcomeReadsWhatDispatchWrites(t *testing.T) {
	at := time.Date(2026, 9, 21, 12, 15, 0, 0, time.UTC)
	stamp := at.Format(time.RFC3339) + " "
	for line, want := range map[string]application.TickOutcome{
		stamp + "ok: 2 started":                       application.TickGood,
		stamp + "ok: 0 started":                       application.TickGood,
		stamp + "ok: nothing ready":                   application.TickGood,
		stamp + "local network fault":                 application.TickFault,
		stamp + "failed: claiming mw-x.1: bd said no": application.TickFailed,
		stamp + "a line in words nobody has written":  application.TickUnread,
		"no date " + "ok: 1 started":                  application.TickUnread,
		"":                                            application.TickUnread,
	} {
		gotAt, got := application.DispatchOutcome(line)
		if got != want {
			t.Errorf("%q: expected %s, got %s", line, want, got)
		}
		if want != application.TickUnread && !gotAt.Equal(at) {
			t.Errorf("%q: expected the time %s, got %s", line, at, gotAt)
		}
	}
}

func TestMillhandTickOutcomeReadsWhatTheTickWrites(t *testing.T) {
	stamp := "2026-09-21T12:15:00Z "
	for line, want := range map[string]application.TickOutcome{
		stamp + "quiet":                                         application.TickGood,
		stamp + "already up (millhand-1)":                       application.TickGood,
		stamp + "woke the Millhand: 1 unread message: \"Look\"": application.TickGood,
		stamp + "quiet; sync did only its beads half: the vault holds uncommitted changes to a.md": application.TickGood,
		stamp + "quiet; sync failed: could not resolve host":                                       application.TickFailed,
		stamp + "woke the Millhand: 1 unread message: \"x\"; sync failed: no":                      application.TickFailed,
		stamp + "quiet; local-fault: this host's network is down, so it did not sync":              application.TickFault,
		stamp + "wake failed: the seat has no charter":                                             application.TickFailed,
		stamp + "could not tell whether the Millhand is needed; mail could not be read: x":         application.TickFailed,
		stamp + "could not look for the Millhand's window: tmux is not running":                    application.TickFailed,
		// A watch that could not be run in a tick that wakes for something else, or
		// finds nothing to say, is not a failed tick by itself.
		stamp + "quiet; watch: unreachable ssh": application.TickGood,
		"junk":                                  application.TickUnread,
	} {
		if _, got := application.MillhandTickOutcome(line); got != want {
			t.Errorf("%q: expected %s, got %s", line, want, got)
		}
	}
}

func TestCountTicksCountsWhatFollowsTheLastGoodRun(t *testing.T) {
	log := heldLog(t,
		"2026-09-21T09:00:00Z ok: nothing ready",
		"2026-09-21T09:15:00Z ok: 1 started",
		"2026-09-21T09:30:00Z failed: no",
		"2026-09-21T09:45:00Z local network fault",
		"2026-09-21T10:00:00Z failed: no",
		"a line that is not a run",
		"2026-09-21T10:15:00Z local network fault",
	)
	count := application.CountTicks(context.Background(), log, application.DispatchOutcome)

	if !count.Known {
		t.Fatal("expected a log of runs to be known")
	}
	if want := time.Date(2026, 9, 21, 9, 15, 0, 0, time.UTC); !count.LastGood.Equal(want) {
		t.Errorf("expected the last good run at %s, got %s", want, count.LastGood)
	}
	if count.Failed != 2 || count.Faults != 2 {
		t.Errorf("expected 2 failed and 2 local network faults, got %+v", count)
	}
}

func TestACountResetsWhenARunIsGood(t *testing.T) {
	log := heldLog(t,
		"2026-09-21T09:00:00Z failed: no",
		"2026-09-21T09:15:00Z local network fault",
		"2026-09-21T09:30:00Z ok: 1 started",
	)
	count := application.CountTicks(context.Background(), log, application.DispatchOutcome)
	if count.Failed != 0 || count.Faults != 0 || count.LastGood.IsZero() {
		t.Fatalf("expected a good run to leave nothing counted, got %+v", count)
	}
}

func TestACountWithNoGoodRunHasNoLastGood(t *testing.T) {
	log := heldLog(t, "2026-09-21T09:00:00Z failed: no", "2026-09-21T09:15:00Z failed: no")
	count := application.CountTicks(context.Background(), log, application.DispatchOutcome)
	if !count.Known || !count.LastGood.IsZero() || count.Failed != 2 {
		t.Fatalf("expected 2 failed and no good run, got %+v", count)
	}
}

func TestNoLogIsNotKnown(t *testing.T) {
	ctx := context.Background()
	if count := application.CountTicks(ctx, nil, application.DispatchOutcome); count.Known {
		t.Errorf("expected no log to be unknown, got %+v", count)
	}
	if count := application.CountTicks(ctx, &apptest.FakeTickLog{}, application.DispatchOutcome); count.Known {
		t.Errorf("expected an empty log to be unknown, got %+v", count)
	}
	if count := application.CountTicks(ctx, heldLog(t, "nothing dated here"), application.DispatchOutcome); count.Known {
		t.Errorf("expected a log of no runs to be unknown, got %+v", count)
	}
	unreadable := &apptest.FakeTickLog{ReadErr: context.DeadlineExceeded}
	if count := application.CountTicks(ctx, unreadable, application.DispatchOutcome); count.Known {
		t.Errorf("expected a log that cannot be read to be unknown, got %+v", count)
	}
}

func TestTheNoteOfTheCountsReadsBackAsTheyWere(t *testing.T) {
	ctx := context.Background()
	logs := application.TickLogs{
		Dispatch: heldLog(t, "2026-09-21T09:00:00Z ok: 1 started", "2026-09-21T09:15:00Z failed: no", "2026-09-21T09:30:00Z local network fault"),
		Millhand: heldLog(t, "2026-09-21T09:05:00Z wake failed: x"),
	}
	held := application.ReadHostTicks(ctx, logs)
	note := held.Note()
	if strings.Contains(note, "\n") {
		t.Fatalf("expected a note of one line, got %q", note)
	}
	back := application.ParseHostTicks(note)

	if back != held {
		t.Fatalf("expected the note %q to read back as %+v, got %+v", note, held, back)
	}
	if !back.Dispatch.Known || back.Dispatch.Failed != 1 || back.Dispatch.Faults != 1 {
		t.Errorf("expected the dispatch counts to survive, got %+v", back.Dispatch)
	}
}

func TestANoteWithNoLogsIsEmptyAndReadsBackAsNone(t *testing.T) {
	held := application.ReadHostTicks(context.Background(), application.TickLogs{})
	if held.Known() || held.Note() != "" {
		t.Fatalf("expected nothing to say without logs, got %+v %q", held, held.Note())
	}
	for _, note := range []string{"", "garbage", "dispatch good=notatime failed=x faults=1"} {
		if application.ParseHostTicks(note).Known() {
			t.Errorf("expected %q to read as no counts", note)
		}
	}
}
