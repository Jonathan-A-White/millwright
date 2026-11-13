package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.1171: every reader of a host's per-name notes leaves out a note
// older than the box's make time, and a note of a box the book no longer holds,
// through CloudState.NoteOfAnOlderBox.

func TestNoteOfAnOlderBoxSaysBothCases(t *testing.T) {
	made := statusNow.Add(-time.Hour)
	book := application.CloudState{Boxes: []application.CloudBox{{Name: "cloud2", Up: made}}}
	plan := application.CloudPlan{Prefix: "cloud"}
	for _, c := range []struct {
		name string
		host string
		at   time.Time
		want bool
	}{
		{"a note before the box was made", "cloud2", made.Add(-time.Minute), true},
		{"no note time at all is older than the make", "cloud2", time.Time{}, true},
		{"a note at the make", "cloud2", made, false},
		{"a note after the make", "cloud2", made.Add(time.Minute), false},
		{"a plan box the book holds none of", "cloud1", statusNow, true},
		{"a host that is no box", "laptop", time.Time{}, false},
		{"a name like a box's but no plan box", "cloudy", time.Time{}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := book.NoteOfAnOlderBox(plan, c.host, c.at); got != c.want {
				t.Fatalf("NoteOfAnOlderBox(%s, %s) = %v, want %v", c.host, c.at, got, c.want)
			}
		})
	}
}

// reusedBoxWithNotes is cloud4 made 17 minutes ago under the name of a box
// destroyed before it, which left every note a host leaves, written 22 minutes
// back (or, with after, 5 minutes back, written by the new box itself).
func reusedBoxWithNotes(t *testing.T, after bool) (*apptest.FakeTracker, *apptest.FakeCloudBook) {
	t.Helper()
	ctx := context.Background()
	tracker, book := reusedCloud4(t, 17*time.Minute, 22*time.Minute, false)
	ago := 22 * time.Minute
	if after {
		ago = 5 * time.Minute
		syncedAt(t, tracker, "cloud4", ago)
	}
	at := statusNow.Add(-ago)
	set := func(key, value string) {
		t.Helper()
		if err := tracker.SetNote(ctx, key, value); err != nil {
			t.Fatalf("writing %s: %v", key, err)
		}
	}
	set(application.SyncHaltKey("cloud4"), application.FormatSyncHalt(application.SyncHaltInfo{At: at, Said: "a merge conflict"}))
	set(application.TicksKey("cloud4"), application.HostTicks{Dispatch: application.TickCount{Known: true, LastGood: at}}.Note())
	set(application.MwCommitKey("cloud4"), "abc1234")
	set(application.DoctorVerdictKey("cloud4"), application.DoctorVerdict{At: at, Faulty: []application.DoctorFinding{{Check: "disk", Reason: "full"}}}.Note())
	set(application.HostPassedKey("cloud4"), application.FormatHostPassed([]application.HostPassed{{
		Story: "mw-gq6.30", Since: at, Gate: "metered-network", Why: "the network is metered"}}))
	set(application.GiveWayKey("cloud4"), application.GiveWayNote{Since: at, By: "someone", Seen: at}.String())
	return tracker, book
}

func hostWorkOf(t *testing.T, report application.StatusReport, host string) application.HostWork {
	t.Helper()
	for _, w := range report.Others {
		if w.Host == host {
			return w
		}
	}
	t.Fatalf("expected %s among the other hosts, got %+v", host, report.Others)
	return application.HostWork{}
}

// unreadBook is a cloud book that cannot be read.
type unreadBook struct{ *apptest.FakeCloudBook }

func (unreadBook) Read(context.Context) (application.CloudState, error) {
	return application.CloudState{}, context.DeadlineExceeded
}

func statusOfBook(t *testing.T, tracker *apptest.FakeTracker, book application.CloudBook) application.StatusReport {
	t.Helper()
	report, err := application.Status{
		Tracker: tracker, Notes: tracker, Host: "vps", Seat: "builder",
		HostSilence: 2 * time.Hour, MainCommit: "fff0000",
		Cloud: book, CloudPlan: application.CloudPlan{Prefix: "cloud"},
		Now: func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	return report
}

// The notes of the box destroyed before cloud4 was made again are none of the
// new box's: status reads none of them.
func TestStatusReadsNoNotesOfABoxMadeAfterThem(t *testing.T) {
	tracker, book := reusedBoxWithNotes(t, false)

	w := hostWorkOf(t, statusOfBook(t, tracker, book), "cloud4")

	if w.Halt != nil {
		t.Errorf("expected no sync halt, got %+v", w.Halt)
	}
	if w.Ticks.Known() {
		t.Errorf("expected no ticks, got %+v", w.Ticks)
	}
	if w.MwCommit != "" {
		t.Errorf("expected no mw commit, got %q", w.MwCommit)
	}
	if w.DoctorSaid != "" || len(w.Doctor.Faulty) != 0 || w.DoctorUnreadable {
		t.Errorf("expected no doctor verdict, got %q", w.DoctorSaid)
	}
	if len(w.Passed) != 0 {
		t.Errorf("expected nothing passed, got %+v", w.Passed)
	}
	if w.GivesWay.Stands {
		t.Errorf("expected no give-way, got %+v", w.GivesWay)
	}
}

// The control: notes written after the box was made are its own.
func TestStatusReadsTheNotesOfABoxWrittenAfterItWasMade(t *testing.T) {
	tracker, book := reusedBoxWithNotes(t, true)

	w := hostWorkOf(t, statusOfBook(t, tracker, book), "cloud4")

	if w.Halt == nil || !w.Ticks.Known() || w.MwCommit != "abc1234" || w.DoctorSaid == "" || len(w.Passed) != 1 || !w.GivesWay.Stands {
		t.Errorf("expected every note of the new box read, got %+v", w)
	}
}

// A book that cannot be read leaves every note as it was.
func TestStatusReadsEveryNoteWhenTheBookCannotBeRead(t *testing.T) {
	tracker, book := reusedBoxWithNotes(t, false)

	w := hostWorkOf(t, statusOfBook(t, tracker, unreadBook{book}), "cloud4")

	if w.Halt == nil || !w.Ticks.Known() || w.MwCommit != "abc1234" || w.DoctorSaid == "" || len(w.Passed) != 1 || !w.GivesWay.Stands {
		t.Errorf("expected every note read with no book, got %+v", w)
	}
}

func TestNoHostTakesLeavesOutADestroyedBox(t *testing.T) {
	ctx := context.Background()
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11"}}
	run := func(t *testing.T, book *apptest.FakeCloudBook, cloud1Passes string) []string {
		tracker := aTrackerPathedToVPS(t)
		storyOn(t, tracker, "mw-gq6.50", "A ready story", "auto")
		for _, host := range []string{"vps", "laptop", "desktop"} {
			passedNote(t, tracker, host, "mw-gq6.50")
		}
		passedNote(t, tracker, "cloud1", cloud1Passes)
		syncedAt(t, tracker, "cloud1", time.Hour)
		report, err := application.Status{
			Tracker: tracker, Notes: tracker, Host: "vps", Seat: "builder", HostSilence: 2 * time.Hour,
			Cloud: book, CloudPlan: application.CloudPlan{Prefix: "cloud"},
			Now: func() time.Time { return statusNow },
		}.Run(ctx)
		if err != nil {
			t.Fatalf("reading status: %v", err)
		}
		return passedHosts(report, "mw-gq6.50")
	}

	t.Run("a box that passed the story is not named", func(t *testing.T) {
		got := run(t, book, "mw-gq6.50")
		if strings.Join(got, ",") != "desktop,laptop,vps" {
			t.Fatalf("expected the live hosts alone named, got %v", got)
		}
	})
	t.Run("a box that passed another story does not stop the story being passed by all", func(t *testing.T) {
		got := run(t, book, "mw-gq6.99")
		if strings.Join(got, ",") != "desktop,laptop,vps" {
			t.Fatalf("expected the story passed by the live hosts, got %v", got)
		}
	})
	t.Run("a box the book holds is still counted", func(t *testing.T) {
		held := &apptest.FakeCloudBook{State: application.CloudState{Boxes: []application.CloudBox{{Name: "cloud1", Up: statusNow.Add(-5 * time.Hour)}}}}
		got := run(t, held, "mw-gq6.99")
		if len(got) != 0 {
			t.Fatalf("expected a live box that has not passed to take the story, got %v", got)
		}
	})
}

func TestFilesHeldSinceLeavesOutADestroyedBox(t *testing.T) {
	ctx := context.Background()
	run := func(t *testing.T, book *apptest.FakeCloudBook) []string {
		tracker := aTrackerPathedToVPS(t)
		aClaimOn(t, tracker, "mw-gq6.754", "vps", "docs/paradigms.md", true)
		anyHost := domain.Story{ID: "mw-gq6.1003", Title: "A story"}
		mustDo(t, anyHost.Overrides.Set("host", "auto"))
		mustDo(t, anyHost.Overrides.Set("files", "docs/paradigms.md"))
		tracker.AddStory("mw-gq6", anyHost)
		syncedAt(t, tracker, "cloud1", time.Hour)
		held := "mw-gq6.1003 " + statusNow.Add(-2*time.Hour).Format(time.RFC3339)
		if err := tracker.SetNote(ctx, application.FilesHeldKey("cloud1"), held); err != nil {
			t.Fatal(err)
		}
		report, err := application.Status{
			Tracker: tracker, Notes: tracker, Host: "vps", Seat: "builder", HostSilence: 2 * time.Hour,
			Cloud: book, CloudPlan: application.CloudPlan{Prefix: "cloud"},
			Now: func() time.Time { return statusNow },
		}.Run(ctx)
		if err != nil {
			t.Fatalf("reading status: %v", err)
		}
		return damperLines(report, "mw-gq6.1003")
	}

	if lines := run(t, &apptest.FakeCloudBook{}); len(lines) != 0 {
		t.Errorf("expected a destroyed box's files_held note to be none of the story's since, got %q", lines)
	}
	live := &apptest.FakeCloudBook{State: application.CloudState{Boxes: []application.CloudBox{{Name: "cloud1", Up: statusNow.Add(-5 * time.Hour)}}}}
	if lines := run(t, live); len(lines) != 1 {
		t.Errorf("expected a live box's files_held note read as it was, got %q", lines)
	}
}

// A box made again under a destroyed box's name holds back a rig, and the
// ticks of the box before it (no good dispatch run) do not say it is not
// passing; its own, written after its sync, do.
func TestAStarvedBoxIsJudgedByItsOwnTicksNotThoseOfABoxBeforeIt(t *testing.T) {
	ctx := context.Background()
	failing := application.HostTicks{Dispatch: application.TickCount{Known: true, Failed: 3}}.Note()
	for name, syncedAgo := range map[string]time.Duration{"the old box's ticks": 0, "its own ticks": time.Hour} {
		t.Run(name, func(t *testing.T) {
			tracker := aTrackerPathedToVPS(t)
			storyOn(t, tracker, "mw-gq6.1", "One", "vps")
			note := application.StarvedRig{Rig: "millwright", Since: statusNow.Add(-4 * time.Hour), Hold: "smoke hold", Reason: "the grist smoke failed"}
			mustDo(t, tracker.SetNote(ctx, application.StarvedKey("cloud1"), application.FormatStarved([]application.StarvedRig{note})))
			mustDo(t, tracker.SetNote(ctx, application.TicksKey("cloud1"), failing))
			if syncedAgo > 0 {
				syncedAt(t, tracker, "cloud1", syncedAgo)
			}
			book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11", Boxes: []application.CloudBox{
				{Name: "cloud1", Up: statusNow.Add(-5 * time.Hour), Checked: statusNow.Add(-4 * time.Hour)}}}}
			alarms, err := application.DamperSurvey{Tracker: tracker, Notes: tracker, Cloud: book, CloudPlan: cloudPlan}.Starved(ctx, statusNow)
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if syncedAgo > 0 {
				want = 0
			}
			if len(alarms) != want {
				t.Errorf("expected %d starved alarms, got %+v", want, alarms)
			}
		})
	}
}
