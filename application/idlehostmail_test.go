package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// anIdleHost is a dispatch on a host with room: the first tick starts
// mw-gq6.1 on src/Reader.tsx, and every tick after it starts nothing, for
// mw-gq6.2 shares that file, mw-gq6.3 waits on the Governor and mw-gq6.4 is
// parked behind an event. tick runs the dispatch at an offset from 09:00Z.
func anIdleHost(t *testing.T, mail bool) (dispatch *application.Dispatch, tracker *apptest.FakeTracker, mailbox *apptest.FakeMailbox, tick func(time.Duration)) {
	t.Helper()
	built, tracker, _, _, _ := aFactory(t)
	dispatch = &built
	mailbox = apptest.NewFakeMailbox()
	dispatch.Cap, dispatch.Memory, dispatch.Mailbox = 4, tracker, mailbox
	dispatch.IdleHostMail = mail
	first := time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)
	clock := first
	dispatch.Now = func() time.Time { return clock }
	storyChanging(t, tracker.AddStory, "mw-gq6.1", "src/Reader.tsx")
	storyChanging(t, tracker.AddStory, "mw-gq6.2", "src/Preface.tsx,src/Reader.tsx")
	storyChanging(t, tracker.AddStory, "mw-gq6.3", "src/Cover.tsx")
	storyChanging(t, tracker.AddStory, "mw-gq6.4", "src/Index.tsx")
	mustDo(t, tracker.SetLabels("mw-gq6.3", application.LabelHitl))
	mustDo(t, tracker.SetLabels("mw-gq6.4", "held-for-jonah"))

	tick = func(after time.Duration) {
		t.Helper()
		clock = first.Add(after)
		if _, err := dispatch.Run(context.Background()); err != nil {
			t.Fatalf("dispatching at +%s: %v", after, err)
		}
	}
	tick(0) // starts mw-gq6.1: the host is busy, not idle
	return dispatch, tracker, mailbox, tick
}

func TestDispatchIdleHostMailSendsOneLineOfCountsOncePerSpell(t *testing.T) {
	_, tracker, mailbox, tick := anIdleHost(t, true)

	tick(1 * time.Minute) // room, nothing started: the spell begins
	tick(15 * time.Minute)
	if inbox := mayorMail(t, mailbox); len(inbox) != 0 {
		t.Fatalf("14 minutes into the spell the Mayor was mailed %d times, want none: %+v", len(inbox), inbox)
	}
	tick(16 * time.Minute)
	inbox := mayorMail(t, mailbox)
	if len(inbox) != 1 {
		t.Fatalf("15 minutes into the spell the Mayor was mailed %d times, want once: %+v", len(inbox), inbox)
	}
	body := strings.TrimSpace(inbox[0].Body)
	if strings.Contains(body, "\n") {
		t.Errorf("the body is more than one line: %q", body)
	}
	for _, want := range []string{"vps", "1 held on files", "src/Reader.tsx", "mw-gq6.1", "1 waiting on him", "1 thawable"} {
		if !strings.Contains(body, want) {
			t.Errorf("the line %q lacks %q", body, want)
		}
	}
	if !strings.Contains(inbox[0].Subject, "vps") {
		t.Errorf("the subject %q does not name the host", inbox[0].Subject)
	}

	// The same spell, an hour on: nothing more is sent.
	tick(75 * time.Minute)
	if inbox := mayorMail(t, mailbox); len(inbox) != 1 {
		t.Fatalf("the same spell mailed the Mayor %d times in all, want once", len(inbox))
	}

	// A story starts, which ends the spell; a later one that waits on a file
	// is a new spell with its own mail.
	if err := tracker.SetStoryMetadata(context.Background(), "mw-gq6.2", map[string]string{"files": "src/Preface.tsx"}); err != nil {
		t.Fatalf("narrowing the files: %v", err)
	}
	tick(80 * time.Minute) // mw-gq6.2 starts
	storyChanging(t, tracker.AddStory, "mw-gq6.5", "src/Preface.tsx")
	tick(81 * time.Minute)
	tick(100 * time.Minute)
	inbox = mayorMail(t, mailbox)
	if len(inbox) != 2 {
		t.Fatalf("a second spell should be mailed once more: the Mayor has %d mails, want 2", len(inbox))
	}
	if !strings.Contains(inbox[1].Body, "mw-gq6.2") || !strings.Contains(inbox[1].Body, "src/Preface.tsx") {
		t.Errorf("the second spell's line does not name the file and its holder: %q", inbox[1].Body)
	}
}

func TestDispatchIdleHostMailIsOffWhenThawableIsFalse(t *testing.T) {
	_, _, mailbox, tick := anIdleHost(t, false)

	tick(1 * time.Minute)
	tick(16 * time.Minute)
	tick(75 * time.Minute)
	if inbox := mayorMail(t, mailbox); len(inbox) != 0 {
		t.Fatalf("with the mail off the Mayor was mailed %d times: %+v", len(inbox), inbox)
	}
}

func TestDispatchIdleHostMailCountsRoomTheWayDispatchDoes(t *testing.T) {
	dispatch, _, mailbox, tick := anIdleHost(t, true)

	// A host with no room is not idle: the spell does not begin, however long
	// the stories wait.
	dispatch.Load = &apptest.FakeHostLoad{Reading: application.LoadReading{Load: 40, Cores: 4}}
	dispatch.Room = application.RoomLimits{LoadPerCore: 2}
	tick(1 * time.Minute)
	tick(30 * time.Minute)
	tick(60 * time.Minute)
	if inbox := mayorMail(t, mailbox); len(inbox) != 0 {
		t.Fatalf("a host with no room mailed the Mayor %d times: %+v", len(inbox), inbox)
	}

	// Room again: the spell begins now, and is mailed 15 minutes on, not at once.
	dispatch.Load = &apptest.FakeHostLoad{Reading: application.LoadReading{Load: 0.1, Cores: 4}}
	tick(61 * time.Minute)
	if inbox := mayorMail(t, mailbox); len(inbox) != 0 {
		t.Fatalf("the spell mailed the Mayor the moment room came back: %+v", inbox)
	}
	tick(77 * time.Minute)
	if inbox := mayorMail(t, mailbox); len(inbox) != 1 {
		t.Fatalf("15 minutes after room came back the Mayor has %d mails, want 1", len(inbox))
	}
}

func TestDispatchIdleHostMailSaysNothingForAHostWithNothingToCount(t *testing.T) {
	_, tracker, mailbox, tick := anIdleHost(t, true)
	mustDo(t, tracker.SetLabels("mw-gq6.3"))
	mustDo(t, tracker.SetLabels("mw-gq6.4"))
	if err := tracker.SetStoryMetadata(context.Background(), "mw-gq6.2", map[string]string{"files": "src/Preface.tsx"}); err != nil {
		t.Fatalf("narrowing the files: %v", err)
	}

	tick(1 * time.Minute) // everything left starts: a busy host
	tick(30 * time.Minute)
	tick(60 * time.Minute)
	if inbox := mayorMail(t, mailbox); len(inbox) != 0 {
		t.Fatalf("a host with nothing held mailed the Mayor: %+v", inbox)
	}
}

func TestDispatchIdleHostMailWritesNothingOnADryRun(t *testing.T) {
	dispatch, tracker, mailbox, tick := anIdleHost(t, true)
	dispatch.DryRun = true

	tick(1 * time.Minute)
	tick(30 * time.Minute)
	if inbox := mayorMail(t, mailbox); len(inbox) != 0 {
		t.Fatalf("a dry run mailed the Mayor: %+v", inbox)
	}
	if notes, _ := tracker.NotesWithPrefix(context.Background(), "host.vps.idle"); len(notes) != 0 {
		t.Fatalf("a dry run kept a note of the idle spell: %v", notes)
	}
}
