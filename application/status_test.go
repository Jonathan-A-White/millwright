package application_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// statusNow is the clock every status test reads, so that "silent 28h00m" is
// the same sentence on every machine and in every month.
var statusNow = time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

// otherHostStatus is an mw status over one epic pathed to `vps`, with the
// stories the test adds pathed wherever it says, read as the VPS.
func otherHostStatus(t *testing.T, tracker *apptest.FakeTracker) application.StatusReport {
	t.Helper()
	report, err := application.Status{
		Tracker:     tracker,
		Notes:       tracker,
		Host:        "vps",
		Seat:        "builder",
		HostSilence: 2 * time.Hour,
		Now:         func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	return report
}

// storyOn files a story under the epic, pathed to a host of its own.
func storyOn(t *testing.T, tracker *apptest.FakeTracker, id, title, host string) {
	t.Helper()
	story := domain.Story{ID: id, Title: title}
	if err := story.Overrides.Set("host", host); err != nil {
		t.Fatalf("pathing %s to %s: %v", id, host, err)
	}
	tracker.AddStory("mw-gq6", story)
}

// syncedAt leaves the note a host writes for itself when a sync finishes.
func syncedAt(t *testing.T, tracker *apptest.FakeTracker, host string, ago time.Duration) {
	t.Helper()
	at := statusNow.Add(-ago).UTC().Format(application.LastSyncFormat)
	if err := tracker.SetNote(context.Background(), application.LastSyncKey(host), at); err != nil {
		t.Fatalf("recording the last sync of %s: %v", host, err)
	}
}

// aTrackerPathedToVPS is a fake holding one epic whose default Path is the VPS.
func aTrackerPathedToVPS(t *testing.T) *apptest.FakeTracker {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	// Leases are the tracker's clock's: one on the real clock would be a heartbeat
	// from after the day these tests are set on.
	tracker.Clock = func() time.Time { return statusNow }
	defaults := domain.Path{}
	for field, value := range map[string]string{
		"rig": "millwright", "branch": "main", "harness": "claude",
		"model": "opus", "effort": "high", "formula": "tdd-feature", "host": "vps",
	} {
		if err := defaults.Set(field, value); err != nil {
			t.Fatalf("setting the epic's %s: %v", field, err)
		}
	}
	tracker.AddEpic("mw-gq6", defaults)
	return tracker
}

// section is the OTHER HOSTS block of a printed report, without the headings
// around it.
func section(report application.StatusReport) string {
	printed := report.String()
	from := strings.Index(printed, "OTHER HOSTS")
	if from < 0 {
		return printed
	}
	rest := printed[from:]
	if to := strings.Index(rest, "\n\n"); to >= 0 {
		return rest[:to]
	}
	return rest
}

// TestOtherHostsSection is the sample a person sees: a host that synced a
// moment ago, a host asleep with work stranded on it, and a host that has
// never synced at all. Every case is logged, so that the report of this story
// can show exactly what the section renders.
func TestOtherHostsSection(t *testing.T) {
	t.Run("a host that synced recently holds its work, unstranded", func(t *testing.T) {
		tracker := aTrackerPathedToVPS(t)
		storyOn(t, tracker, "mw-gq6.30", "Teach mw next to hand a worktree back", "laptop")
		syncedAt(t, tracker, "laptop", 30*time.Minute)

		block := section(otherHostStatus(t, tracker))
		t.Logf("\n%s", block)
		for _, want := range []string{"laptop · synced 30m ago", "mw-gq6.30 · millwright", "ready"} {
			if !strings.Contains(block, want) {
				t.Errorf("expected the section to say %q, got:\n%s", want, block)
			}
		}
		if strings.Contains(block, "stranded") {
			t.Errorf("expected nothing stranded on a host that just synced, got:\n%s", block)
		}
	})

	t.Run("a sleeping host's work is stranded, with the line that re-paths it", func(t *testing.T) {
		tracker := aTrackerPathedToVPS(t)
		storyOn(t, tracker, "mw-gq6.31", "Teach mw next to hand a worktree back", "laptop")
		storyOn(t, tracker, "mw-gq6.32", "Sweep a claim whose session is gone", "laptop")
		if err := tracker.ClaimStory(context.Background(), "mw-gq6.32"); err != nil {
			t.Fatalf("claiming mw-gq6.32: %v", err)
		}
		heartbeatAt(t, tracker, "mw-gq6.32", 28*time.Hour)
		syncedAt(t, tracker, "laptop", 28*time.Hour)

		block := section(otherHostStatus(t, tracker))
		t.Logf("\n%s", block)
		for _, want := range []string{
			"laptop · ASLEEP, silent 28h00m",
			"last sync 2026-09-17T08:00:00Z (a cycle behind)",
			"re-path: " + application.RepathHint + "vps",
			"stranded · ready",
			"stranded · claimed",
		} {
			if !strings.Contains(block, want) {
				t.Errorf("expected the section to say %q, got:\n%s", want, block)
			}
		}
	})

	t.Run("a host that has never synced is asleep on the face of it", func(t *testing.T) {
		tracker := aTrackerPathedToVPS(t)
		storyOn(t, tracker, "mw-gq6.33", "Write the host availability ADR", "laptop")

		block := section(otherHostStatus(t, tracker))
		t.Logf("\n%s", block)
		for _, want := range []string{"laptop · ASLEEP, never synced", "stranded · ready"} {
			if !strings.Contains(block, want) {
				t.Errorf("expected the section to say %q, got:\n%s", want, block)
			}
		}
		if strings.Contains(block, "last sync") {
			t.Errorf("expected no last sync line for a host that never synced, got:\n%s", block)
		}
	})
}

// TestOtherHostsSectionFitsAPhone is the width rule applied to the longest
// lines the section can print: a long title, a long host name, and the
// re-pathing hint.
func TestOtherHostsSectionFitsAPhone(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.34",
		"A story whose title runs on and on and on and on and on, well past a phone screen",
		"a-host-with-a-very-long-name-indeed")

	printed := otherHostStatus(t, tracker).String()
	for _, line := range strings.Split(printed, "\n") {
		if n := utf8.RuneCountInString(line); n > application.Width {
			t.Errorf("expected every line at most %d columns, got %d in %q", application.Width, n, line)
		}
	}
}

// TestStatusWithoutNotesLeavesTheOtherHostsOut says what a Status with nowhere
// to read a note does: it lists no other host at all, rather than calling
// every one of them asleep on no evidence.
func TestStatusWithoutNotesLeavesTheOtherHostsOut(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.35", "Something pathed to the laptop", "laptop")

	report, err := application.Status{
		Tracker: tracker,
		Host:    "vps",
		Seat:    "builder",
		Now:     func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	if len(report.Others) != 0 {
		t.Fatalf("expected no other hosts without notes to read, got %+v", report.Others)
	}
	if !strings.Contains(report.String(), "nothing pathed to another host") {
		t.Fatalf("expected the section to say so plainly, got:\n%s", report.String())
	}
}

// TestStatusShowsThisHostsBeadsSize is the plain case: a size under budget is
// shown without a warning.
func TestStatusShowsThisHostsBeadsSize(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	tracker.SetSize(500_000_000)

	report := otherHostStatus(t, tracker)
	if !report.BeadsKnown || report.BeadsBytes != 500_000_000 {
		t.Fatalf("expected a known size of 500,000,000 bytes, got known=%v bytes=%d", report.BeadsKnown, report.BeadsBytes)
	}
	if !strings.Contains(report.String(), "BEADS 500MB") {
		t.Fatalf("expected the report to show the size, got:\n%s", report.String())
	}
	if strings.Contains(report.String(), "past the") {
		t.Fatalf("expected no warning under budget, got:\n%s", report.String())
	}
}

// TestStatusWarnsPastTheBeadsBudget is the friction this story exists for:
// past the budget, mw status says so without a Clerk running du.
func TestStatusWarnsPastTheBeadsBudget(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	tracker.SetSize(2_000_000_000)

	report := otherHostStatus(t, tracker)
	if report.BeadsBytes <= report.BeadsBudgetBytes {
		t.Fatalf("expected the size to be past the budget, got %d/%d", report.BeadsBytes, report.BeadsBudgetBytes)
	}
	want := "BEADS 2.0GB: past the 1.5GB budget"
	if !strings.Contains(report.String(), want) {
		t.Fatalf("expected %q, got:\n%s", want, report.String())
	}
}

// TestStatusSaysWhenTheBeadsWereLastCollected is mw-gq6.449: a collection the
// halted backup starved for ten days shows on the BEADS line, so nobody has to
// read the note to find out.
func TestStatusSaysWhenTheBeadsWereLastCollected(t *testing.T) {
	for _, tc := range []struct {
		name string
		note string // empty: no note at all
		want string
	}{
		{"never", "", "BEADS 500MB · never collected\n"},
		{"ten days ago", statusNow.Add(-10*24*time.Hour - time.Hour).UTC().Format(application.LastSyncFormat), "BEADS 500MB · last GC 10d ago\n"},
		{"hours ago", statusNow.Add(-3*time.Hour - 5*time.Minute).UTC().Format(application.LastSyncFormat), "BEADS 500MB · last GC 3h05m ago\n"},
		{"unreadable", "yesterday-ish", "BEADS 500MB · last GC unreadable\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tracker := aTrackerPathedToVPS(t)
			tracker.SetSize(500_000_000)
			if tc.note != "" {
				if err := tracker.SetNote(context.Background(), application.LastGCKey("vps"), tc.note); err != nil {
					t.Fatalf("seeding the last collection: %v", err)
				}
			}
			report := otherHostStatus(t, tracker)
			if !strings.Contains(report.String(), tc.want) {
				t.Fatalf("expected %q, got:\n%s", tc.want, report.String())
			}
		})
	}
}

// A host whose beads live on another's never collects them, so its BEADS line
// has no last-GC to say.
func TestStatusLeavesTheLastCollectionOutOnAHostThatSharesAnothersBeads(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	tracker.SetSize(500_000_000)
	report, err := application.Status{
		Tracker:  tracker,
		Notes:    tracker,
		Host:     "vps",
		Seat:     "builder",
		SyncMode: application.BeadsSyncShared,
		Now:      func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	if strings.Contains(report.String(), "collected") || strings.Contains(report.String(), "last GC") {
		t.Fatalf("expected no word of a collection, got:\n%s", report.String())
	}
}

// TestStatusHoldsTheBeadsSizeToTheBudgetItIsGiven: the budget comes from the
// configuration, so a size past the default but under the given budget is no
// warning, and one past the given budget is.
func TestStatusHoldsTheBeadsSizeToTheBudgetItIsGiven(t *testing.T) {
	for _, tc := range []struct {
		size int64
		want string
	}{
		{2_000_000_000, "BEADS 2.0GB · never collected\n"},
		{3_100_000_000, "BEADS 3.1GB: past the 3.0GB budget"},
	} {
		tracker := aTrackerPathedToVPS(t)
		tracker.SetSize(tc.size)
		report, err := application.Status{
			Tracker:          tracker,
			Notes:            tracker,
			Host:             "vps",
			Seat:             "builder",
			HostSilence:      2 * time.Hour,
			BeadsBudgetBytes: 3_000_000_000,
			Now:              func() time.Time { return statusNow },
		}.Run(context.Background())
		if err != nil {
			t.Fatalf("reading status: %v", err)
		}
		if !strings.Contains(report.String(), tc.want) {
			t.Fatalf("expected %q at %d bytes, got:\n%s", tc.want, tc.size, report.String())
		}
	}
}

// TestStatusWithoutNotesLeavesTheBeadsSizeOut mirrors
// TestStatusWithoutNotesLeavesTheOtherHostsOut: Size is TrackerNotes', so a
// report with no Notes to read it from says nothing of it, rather than
// claiming an empty database.
func TestStatusWithoutNotesLeavesTheBeadsSizeOut(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	tracker.SetSize(500_000_000)

	report, err := application.Status{
		Tracker: tracker,
		Host:    "vps",
		Seat:    "builder",
		Now:     func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	if report.BeadsKnown {
		t.Fatalf("expected the size to be unknown without Notes, got %+v", report)
	}
	if strings.Contains(report.String(), "BEADS") {
		t.Fatalf("expected nothing said of the size, got:\n%s", report.String())
	}
}

// TestStatusShowsThisHostsOwnHaltFromItsMarker is the local half of the story:
// mw status reads its own halted sync straight from its marker, never through
// the tracker, so it says so even while the halt itself is what is blocking
// the tracker from carrying the word anywhere else.
func TestStatusShowsThisHostsOwnHaltFromItsMarker(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	marker := apptest.NewFakeSyncHaltMarker()
	at := statusNow.Add(-40 * time.Minute)
	if err := marker.Write(context.Background(), application.SyncHaltInfo{At: at, Said: "conflict in the working set"}); err != nil {
		t.Fatalf("writing the marker: %v", err)
	}

	report, err := application.Status{
		Tracker:  tracker,
		Host:     "vps",
		Seat:     "builder",
		SyncHalt: marker,
		Now:      func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	if report.Halt == nil || !report.Halt.At.Equal(at) || report.Halt.Said != "conflict in the working set" {
		t.Fatalf("expected the report to hold the local halt, got %+v", report.Halt)
	}
	want := "host vps: sync halted since " + at.UTC().Format(application.LastSyncFormat)
	if !strings.Contains(report.String(), want) {
		t.Fatalf("expected the printed report to say %q, got:\n%s", want, report.String())
	}
}

// TestStatusWithNoLocalHaltSaysNothingOfOne is the other side: a host that has
// never halted, or that cleared, leaves the line out.
func TestStatusWithNoLocalHaltSaysNothingOfOne(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	report := otherHostStatus(t, tracker)
	if report.Halt != nil {
		t.Fatalf("expected no local halt, got %+v", report.Halt)
	}
	if strings.Contains(report.String(), "sync halted") {
		t.Fatalf("expected nothing said of a halt, got:\n%s", report.String())
	}
}

// TestOtherHostsSectionShowsAHostsHaltedSync is the other-hosts half: mw
// status reads the sync-halted note it already has the plumbing to read, the
// same way it reads each host's last sync.
func TestOtherHostsSectionShowsAHostsHaltedSync(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.36", "Something pathed to the laptop", "laptop")
	syncedAt(t, tracker, "laptop", 90*time.Minute)
	at := statusNow.Add(-40 * time.Minute)
	if err := tracker.SetNote(context.Background(), application.SyncHaltKey("laptop"),
		application.FormatSyncHalt(application.SyncHaltInfo{At: at, Said: "conflict in the working set"})); err != nil {
		t.Fatalf("writing the note: %v", err)
	}

	block := section(otherHostStatus(t, tracker))
	t.Logf("\n%s", block)
	want := "host laptop: sync halted since " + at.UTC().Format(application.LastSyncFormat)
	if !strings.Contains(block, want) {
		t.Errorf("expected the section to say %q, got:\n%s", want, block)
	}
}

// otherHostStatusAtMain is otherHostStatus with main's commit named.
func otherHostStatusAtMain(t *testing.T, tracker *apptest.FakeTracker, main string) application.StatusReport {
	t.Helper()
	report, err := application.Status{
		Tracker:     tracker,
		Notes:       tracker,
		Host:        "vps",
		Seat:        "builder",
		HostSilence: 2 * time.Hour,
		MainCommit:  main,
		Now:         func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	return report
}

func hostMwCommit(t *testing.T, tracker *apptest.FakeTracker, host, commit string) {
	t.Helper()
	if err := tracker.SetNote(context.Background(), application.MwCommitKey(host), commit); err != nil {
		t.Fatalf("recording the mw commit of %s: %v", host, err)
	}
}

// TestOtherHostsSectionSaysEachHostsMwCommitAndWhenItIsBehindMain is the
// reading of mw-gq6.521: a Boost on an mw older than main worked a story
// without the newest fixes and nothing said so.
func TestOtherHostsSectionSaysEachHostsMwCommitAndWhenItIsBehindMain(t *testing.T) {
	t.Run("a host on main's commit is level", func(t *testing.T) {
		tracker := aTrackerPathedToVPS(t)
		storyOn(t, tracker, "mw-gq6.30", "Something on the cloud", "cloud1")
		syncedAt(t, tracker, "cloud1", 3*time.Minute)
		hostMwCommit(t, tracker, "cloud1", "b3273a6")

		block := section(otherHostStatusAtMain(t, tracker, "b3273a6c0ffee"))
		if !strings.Contains(block, "mw b3273a6") {
			t.Errorf("expected the host's mw commit, got:\n%s", block)
		}
		if strings.Contains(block, "behind main") {
			t.Errorf("expected a host on main's commit not to be behind, got:\n%s", block)
		}
	})

	t.Run("a host on an older commit is behind main", func(t *testing.T) {
		tracker := aTrackerPathedToVPS(t)
		storyOn(t, tracker, "mw-gq6.30", "Something on the cloud", "cloud1")
		syncedAt(t, tracker, "cloud1", 3*time.Minute)
		hostMwCommit(t, tracker, "cloud1", "74016ca")

		report := otherHostStatusAtMain(t, tracker, "b3273a6")
		block := section(report)
		t.Logf("\n%s", block)
		if !strings.Contains(block, "mw 74016ca") || !strings.Contains(block, "behind main") {
			t.Errorf("expected the host's mw commit and that it is behind main, got:\n%s", block)
		}
		if len(report.Others) != 1 || report.Others[0].MwCommit != "74016ca" || !report.Others[0].BehindMain {
			t.Errorf("expected the report to carry the commit and BehindMain, got %+v", report.Others)
		}
		for _, line := range strings.Split(report.String(), "\n") {
			if n := len([]rune(line)); n > 60 {
				t.Errorf("a line of %d columns: %q", n, line)
			}
		}
	})

	t.Run("a host that left no commit is behind main", func(t *testing.T) {
		tracker := aTrackerPathedToVPS(t)
		storyOn(t, tracker, "mw-gq6.30", "Something on the cloud", "cloud1")
		syncedAt(t, tracker, "cloud1", 3*time.Minute)

		block := section(otherHostStatusAtMain(t, tracker, "b3273a6"))
		if !strings.Contains(block, "behind main") {
			t.Errorf("expected a host that noted no commit to be called behind main, got:\n%s", block)
		}
	})

	t.Run("with main unknown nothing is said to be behind", func(t *testing.T) {
		tracker := aTrackerPathedToVPS(t)
		storyOn(t, tracker, "mw-gq6.30", "Something on the cloud", "cloud1")
		syncedAt(t, tracker, "cloud1", 3*time.Minute)
		hostMwCommit(t, tracker, "cloud1", "74016ca")

		block := section(otherHostStatusAtMain(t, tracker, ""))
		if !strings.Contains(block, "mw 74016ca") || strings.Contains(block, "behind main") {
			t.Errorf("expected the commit and no claim about main, got:\n%s", block)
		}
	})
}

// TestStatusReadsReadyAndRunningStoriesOnce says how often mw status asks the
// tracker for the two listings every section is drawn from — what is ready and
// what is claimed. Each costs a bd call, so it asks once per run, however many
// hosts have work and whether or not there are notes to read.
func TestStatusReadsReadyAndRunningStoriesOnce(t *testing.T) {
	for name, notes := range map[string]bool{"with notes": true, "without notes": false} {
		t.Run(name, func(t *testing.T) {
			tracker := aTrackerPathedToVPS(t)
			storyOn(t, tracker, "mw-gq6.30", "Ready here", "vps")
			storyOn(t, tracker, "mw-gq6.31", "Claimed here", "vps")
			storyOn(t, tracker, "mw-gq6.32", "Ready on the laptop", "laptop")
			storyOn(t, tracker, "mw-gq6.33", "Claimed on the laptop", "laptop")
			for _, id := range []string{"mw-gq6.31", "mw-gq6.33"} {
				if err := tracker.ClaimStory(context.Background(), id); err != nil {
					t.Fatalf("claiming %s: %v", id, err)
				}
			}
			before := len(tracker.Asked())

			status := application.Status{
				Tracker: tracker,
				Host:    "vps",
				Seat:    "builder",
				Now:     func() time.Time { return statusNow },
			}
			if notes {
				status.Notes = tracker
			}
			report, err := status.Run(context.Background())
			if err != nil {
				t.Fatalf("reading status: %v", err)
			}

			listings := 0
			for _, call := range tracker.Asked()[before:] {
				switch call {
				case "WorkInHand", "RunningStories", "ReadyForHost":
					listings++
				}
			}
			if listings != 1 {
				t.Fatalf("expected the ready and running stories read once, asked %d times: %v",
					listings, tracker.Asked()[before:])
			}
			if len(report.Running) != 1 || len(report.Ready) != 1 {
				t.Fatalf("expected one story running and one ready here, got %+v", report)
			}
			if notes && (len(report.Others) != 1 || len(report.Others[0].Stories) != 2) {
				t.Fatalf("expected the laptop's two stories listed, got %+v", report.Others)
			}
		})
	}
}

func TestStatusListsWhatLooksDoneButIsStillOpen(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	graph := &apptest.FakeBeadGraph{}
	child := func(parent string) domain.BeadEdge { return domain.BeadEdge{To: parent, Kind: domain.EdgeParentChild} }
	related := func(other string) domain.BeadEdge { return domain.BeadEdge{To: other, Kind: domain.EdgeRelated} }
	graph.Add("e-done", "An epic all of whose children are closed", "open", "epic", nil)
	graph.Add("e-done.1", "one", "closed", "task", nil, child("e-done"))
	graph.Add("e-live", "An epic with a child still open", "open", "epic", nil)
	graph.Add("e-live.1", "one", "open", "task", nil, child("e-live"))
	graph.Add("g-done", "Grilling: related to two closed epics", "open", "task", []string{"wayfinder:grilling"}, related("c-1"), related("c-2"))
	graph.Add("g-half", "Grilling: related to a closed and an open epic", "open", "task", []string{"wayfinder:grilling"}, related("c-1"), related("e-live"))
	graph.Add("g-none", "Grilling: no links", "open", "task", []string{"wayfinder:grilling"})
	graph.Add("c-1", "closed epic one", "closed", "epic", nil)
	graph.Add("c-2", "closed epic two", "closed", "epic", nil)

	report, err := application.Status{
		Tracker: tracker, Notes: tracker, Graph: graph, Host: "vps", Seat: "builder",
		Now: func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}

	var ids []string
	for _, f := range report.Finished {
		ids = append(ids, f.ID)
	}
	if strings.Join(ids, " ") != "e-done g-done" {
		t.Fatalf("listed %v, want e-done and g-done", ids)
	}
	printed := report.String()
	for _, want := range []string{
		application.FinishedHeading + " (2)",
		"e-done · An epic all of whose children are closed",
		"all 1 children closed",
		"its epics c-1, c-2 closed",
	} {
		if !strings.Contains(printed, want) {
			t.Errorf("the report lacks %q:\n%s", want, printed)
		}
	}
	for _, line := range strings.Split(printed, "\n") {
		if n := utf8.RuneCountInString(line); n > application.Width {
			t.Errorf("a line is %d runes, past %d: %q", n, application.Width, line)
		}
	}
}

func TestStatusSaysNothingWhenNothingLooksDone(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	report, err := application.Status{
		Tracker: tracker, Notes: tracker, Graph: &apptest.FakeBeadGraph{}, Host: "vps", Seat: "builder",
		Now: func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	if want := application.FinishedHeading + " (0)\n  nothing\n"; !strings.Contains(report.String(), want) {
		t.Fatalf("the report lacks %q:\n%s", want, report.String())
	}
}

func TestStatusLeavesOutTheSectionWhenTheGraphIsNotGiven(t *testing.T) {
	if strings.Contains(otherHostStatus(t, aTrackerPathedToVPS(t)).String(), application.FinishedHeading) {
		t.Fatal("the report has the DONE, STILL OPEN section though no graph was given")
	}
}

func TestStatusRefusesWhenTheGraphCannotBeRead(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	_, err := application.Status{
		Tracker: tracker, Notes: tracker, Graph: &apptest.FakeBeadGraph{Err: errors.New("bd is down")},
		Host: "vps", Seat: "builder", Now: func() time.Time { return statusNow },
	}.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "bd is down") {
		t.Fatalf("got %v, want a refusal naming bd is down", err)
	}
}

func TestStatusListsOpenBugsWithoutASweepUnderOneHeading(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	filed := func(id, title, acceptance string, created time.Time) {
		storyOn(t, tracker, id, title, "vps")
		if err := tracker.SetCreated(id, created); err != nil {
			t.Fatal(err)
		}
		if err := tracker.SetAcceptance(id, acceptance); err != nil {
			t.Fatal(err)
		}
	}
	after := domain.SweepRuleFrom.Add(time.Hour)
	filed("mw-gq6.40", "[bug] no sweep", "1. It passes.", after)
	filed("mw-gq6.41", "[bug] a sweep", "1. It passes.\n2. Sweep: grep the class.", after)
	filed("mw-gq6.42", "[bug] none, no reason", "Sweep: none", after)
	filed("mw-gq6.43", "[bug] filed before the rule", "1. It passes.", domain.SweepRuleFrom.Add(-time.Hour))
	filed("mw-gq6.44", "A feature", "1. It passes.", after)

	report := otherHostStatus(t, tracker)
	var ids []string
	for _, bug := range report.NoSweep {
		ids = append(ids, bug.Story.ID)
	}
	if got := strings.Join(ids, ","); got != "mw-gq6.40,mw-gq6.42" {
		t.Fatalf("bugs without a sweep: %q, want mw-gq6.40,mw-gq6.42", got)
	}
	text := report.String()
	if !strings.Contains(text, application.NoSweepHeading+" (2)") || !strings.Contains(text, "mw-gq6.40") {
		t.Errorf("the heading and the bugs should be shown, got:\n%s", text)
	}
	if strings.Count(text, application.NoSweepHeading) != 1 {
		t.Errorf("one heading, got:\n%s", text)
	}
}

func TestStatusSaysAMisplacedSweepIsInTheDescriptionInOneLine(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	after := domain.SweepRuleFrom.Add(time.Hour)
	for id, description := range map[string]string{
		"mw-gq6.40": "Acceptance criteria\nSweep: every X is checked",
		"mw-gq6.41": "Nothing about a check here.",
	} {
		storyOn(t, tracker, id, "[bug] "+id, "vps")
		if err := tracker.SetCreated(id, after); err != nil {
			t.Fatal(err)
		}
		if err := tracker.SetDescription(id, description); err != nil {
			t.Fatal(err)
		}
	}

	text := otherHostStatus(t, tracker).String()
	if want := "its Sweep line is in the description, not the acceptance field: copy the criteria with bd update mw-gq6.40 --acceptance"; !strings.Contains(text, want) {
		t.Errorf("status should say %q, got:\n%s", want, text)
	}
	if strings.Count(text, "in the description") != 1 {
		t.Errorf("only the bug with the line in its description is told so, got:\n%s", text)
	}
	if !strings.Contains(text, "no line begins with 'Sweep:'") {
		t.Errorf("the bug with no Sweep line anywhere keeps today's text, got:\n%s", text)
	}
}

func TestStatusShowsNothingOfSweepsWhenEveryBugHasOne(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.40", "[bug] has a sweep", "vps")
	_ = tracker.SetCreated("mw-gq6.40", domain.SweepRuleFrom.Add(time.Hour))
	_ = tracker.SetAcceptance("mw-gq6.40", "Sweep: none, because a one-off")

	report := otherHostStatus(t, tracker)
	if len(report.NoSweep) != 0 || strings.Contains(report.String(), application.NoSweepHeading) {
		t.Errorf("zero should show nothing, got %+v\n%s", report.NoSweep, report.String())
	}
}

func statusWithErrors(t *testing.T, files *apptest.FakeErrorFiles) application.StatusReport {
	t.Helper()
	tracker := aTrackerPathedToVPS(t)
	report, err := application.Status{
		Tracker: tracker, Notes: tracker, Host: "vps", Seat: "builder", HostSilence: 2 * time.Hour,
		Now: func() time.Time { return statusNow }, Errors: files,
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	return report
}

func TestStatusListsTheErrorClassesNotYetCheckedInEveryRigOfTheirKinds(t *testing.T) {
	files := &apptest.FakeErrorFiles{
		Kinds: "lampas = [\"pwa\", \"grind\"]\nspell-forge = [\"pwa\"]\nmillwright = [\"go-cli\"]\n",
		Classes: map[string]string{
			"swept": "---\nsymptom: s\nkinds: go-cli\nsince: 2026-10-14\n---\n\n## Checked\n\n- 2026-10-14 millwright found:mw-gq6.1\n",
			"twice": "---\nsymptom: s\nkinds: pwa\nsince: 2026-10-14\n---\n\n## Checked\n\n- 2026-10-14 lampas found:mw-a.1\n- 2026-10-14 lampas found:mw-a.2\n",
			"grind": "---\nsymptom: s\nkinds: grind\nsince: 2026-10-14\n---\n",
		},
	}
	report := statusWithErrors(t, files)
	var slugs []string
	for _, unchecked := range report.UncheckedErrors {
		slugs = append(slugs, unchecked.Class.Slug+":"+strings.Join(unchecked.Rigs, "+"))
	}
	if got := strings.Join(slugs, ","); got != "grind:lampas,twice:spell-forge" {
		t.Fatalf("unchecked classes: %q", got)
	}
	text := report.String()
	if strings.Count(text, application.UncheckedErrorsHeading) != 1 || !strings.Contains(text, application.UncheckedErrorsHeading+" (2)") {
		t.Errorf("one heading with the count, got:\n%s", text)
	}
	if !strings.Contains(text, "twice: not checked in spell-forge") || !strings.Contains(text, "seen 2 times: "+application.SharedComponentAsk) {
		t.Errorf("each class names its rigs, and one seen twice asks the question, got:\n%s", text)
	}
}

func TestStatusShowsNothingOfErrorClassesWhenEveryOneIsChecked(t *testing.T) {
	files := &apptest.FakeErrorFiles{
		Kinds:   "millwright = [\"go-cli\"]\n",
		Classes: map[string]string{"swept": "---\nsymptom: s\nkinds: go-cli\nsince: 2026-10-14\n---\n\n## Checked\n\n- 2026-10-14 millwright ok\n"},
	}
	report := statusWithErrors(t, files)
	if len(report.UncheckedErrors) != 0 || strings.Contains(report.String(), application.UncheckedErrorsHeading) {
		t.Errorf("zero shows nothing, got:\n%s", report.String())
	}
}

// openButClaimed is a story that was handed back (or un-held) and is open
// again yet still carries the claim of the host that held it (mw-gq6.417).
func openButClaimed(t *testing.T, tracker *apptest.FakeTracker, id, holder string) {
	t.Helper()
	storyOn(t, tracker, id, "Open, claimed by "+holder, "vps")
	if err := tracker.ClaimAs(id, holder, time.Time{}); err != nil {
		t.Fatalf("claiming %s as %s: %v", id, holder, err)
	}
	if err := tracker.SetStatus(id, apptest.StatusOpen); err != nil {
		t.Fatalf("reopening %s: %v", id, err)
	}
}

func TestStatusNamesAnOpenStoryStillClaimedByAHostWithTheRetryThatFreesIt(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	openButClaimed(t, tracker, "mw-gq6.50", "mw@laptop")
	storyOn(t, tracker, "mw-gq6.51", "Open and unassigned", "vps")
	storyOn(t, tracker, "mw-gq6.52", "Running here", "vps")
	if err := tracker.ClaimStory(context.Background(), "mw-gq6.52"); err != nil {
		t.Fatal(err)
	}

	report := otherHostStatus(t, tracker)

	if len(report.OpenButClaimed) != 1 || report.OpenButClaimed[0].Story.ID != "mw-gq6.50" {
		t.Fatalf("expected only mw-gq6.50 open but claimed, got %+v", report.OpenButClaimed)
	}
	text := report.String()
	if !strings.Contains(text, application.OpenButClaimedHeading+" (1)") {
		t.Errorf("the heading with its count should be shown, got:\n%s", text)
	}
	if !strings.Contains(text, "mw retry mw-gq6.50") {
		t.Errorf("the mw retry that frees the story should be shown, got:\n%s", text)
	}
	for _, id := range []string{"mw-gq6.51", "mw-gq6.52"} {
		if strings.Contains(text, "mw retry "+id) {
			t.Errorf("%s is not open but claimed, got:\n%s", id, text)
		}
	}
}

func TestStatusLeavesOutOpenStoriesClaimedByAnotherActorThanAHost(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	openButClaimed(t, tracker, "mw-gq6.50", "mayor")

	report := otherHostStatus(t, tracker)
	if len(report.OpenButClaimed) != 0 || strings.Contains(report.String(), application.OpenButClaimedHeading) {
		t.Errorf("a bead the Mayor holds is not a host's claim, got %+v\n%s", report.OpenButClaimed, report.String())
	}
}

// TestStatusShowsHowManyAttemptsWereTheStorysOwn is mw-gq6.450: an attempt mw
// cause named test, tooling or process is not the story's own code and does not
// count toward max_attempts, so a RUNNING or READY line that says attempts says
// how many of them were its own when that is fewer.
func TestStatusShowsHowManyAttemptsWereTheStorysOwn(t *testing.T) {
	ctx := context.Background()
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.40", "A story ready after a host went down", "vps")
	storyOn(t, tracker, "mw-gq6.41", "A story running after a flaky step", "vps")
	storyOn(t, tracker, "mw-gq6.42", "A story whose attempts were all its own", "vps")
	claim(t, tracker, "mw-gq6.41", time.Hour)
	for id, fields := range map[string]map[string]string{
		"mw-gq6.40": {application.AttemptsField: "3", application.AttemptCausesField: "1:process,2:test,3:code"},
		"mw-gq6.41": {application.AttemptsField: "2", application.AttemptCausesField: "1:tooling"},
		"mw-gq6.42": {application.AttemptsField: "2", application.AttemptCausesField: "1:code"},
	} {
		if err := tracker.SetStoryMetadata(ctx, id, fields); err != nil {
			t.Fatalf("recording the attempts of %s: %v", id, err)
		}
	}

	report, err := application.Status{
		Tracker: tracker, Host: "vps", Seat: "builder", Cap: 2,
		Now: func() time.Time { return statusNow },
	}.Run(ctx)
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	printed := report.String()
	t.Logf("\n%s", printed)
	for id, want := range map[string]string{
		"mw-gq6.40": "attempts 3 (1 own)",
		"mw-gq6.41": "attempts 2 (1 own)",
		"mw-gq6.42": "attempts 2",
	} {
		if got := attemptsLineOf(printed, id); got != want {
			t.Errorf("expected the block of %s to say %q, got %q", id, want, got)
		}
	}
}

// attemptsLineOf is the attempts line in the block of story id, trimmed, or
// empty when the block has none.
func attemptsLineOf(printed, id string) string {
	lines := strings.Split(printed, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), id+" ") {
			continue
		}
		for _, under := range lines[i+1:] {
			if strings.HasPrefix(strings.TrimSpace(under), "mw-") || strings.TrimSpace(under) == "" {
				return ""
			}
			if strings.HasPrefix(strings.TrimSpace(under), "attempts ") {
				return strings.TrimSpace(under)
			}
		}
	}
	return ""
}

func TestStatusNamesEveryOpenStoryWhoseFormulaIsNotInstalled(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	tracker.AddFormula("tdd-feature")
	storyOn(t, tracker, "mw-gq6.50", "parked by a typo", "vps")
	storyOn(t, tracker, "mw-gq6.51", "worked", "vps")
	storyOn(t, tracker, "mw-gq6.52", "parked on another host", "laptop")
	for id, formula := range map[string]string{"mw-gq6.50": "bug", "mw-gq6.52": "nope"} {
		if err := tracker.SetStoryMetadata(context.Background(), id, map[string]string{"formula": formula}); err != nil {
			t.Fatal(err)
		}
	}

	report := otherHostStatus(t, tracker)
	text := report.String()
	if !strings.Contains(text, application.FormulaNotInstalledHeading+" (2)") {
		t.Errorf("expected the heading with two stories, got:\n%s", text)
	}
	for _, want := range []string{"mw-gq6.50", "formula bug", "mw-gq6.52", "formula nope"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in the report, got:\n%s", want, text)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "mw-gq6.51") && strings.Contains(line, "formula") {
			t.Errorf("a story with an installed formula should not be named: %q", line)
		}
	}
}

func TestStatusShowsNothingOfFormulasWhenEveryOneIsInstalled(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	tracker.AddFormula("tdd-feature")
	storyOn(t, tracker, "mw-gq6.51", "worked", "vps")

	if text := otherHostStatus(t, tracker).String(); strings.Contains(text, application.FormulaNotInstalledHeading) {
		t.Errorf("zero should show nothing, got:\n%s", text)
	}
}

// damperStory is a story as the tracker lists it, last changed `idle` ago.
func damperStory(id string, priority int, idle time.Duration, labels ...string) application.StoryDetail {
	return application.StoryDetail{
		Story:    domain.Story{ID: id, Title: "story " + id},
		Status:   "open",
		Priority: priority,
		Labels:   labels,
		Created:  statusNow.Add(-idle),
		Updated:  statusNow.Add(-idle),
		Defaults: domain.Path{Rig: "millwright"},
	}
}

// aCleanDamperFixture trips nothing: a few fresh P1s, a young demo, a grilling
// with its card, one Tester story running, and a short queue of his.
func aCleanDamperFixture() application.DamperFacts {
	tester := damperStory("mw-t.1", 2, time.Hour, application.LabelTester)
	tester.Status = "in_progress"
	return application.DamperFacts{
		Now: statusNow,
		Work: application.WorkInHand{
			Ready: []application.StoryDetail{
				damperStory("mw-a.1", 1, time.Hour),
				damperStory("mw-a.2", 1, 2*24*time.Hour),
				damperStory("mw-a.3", 2, 30*24*time.Hour),
				damperStory("mw-d.1", 2, 2*24*time.Hour, application.LabelDemo, application.LabelHitl),
			},
			Running: []application.StoryDetail{tester},
		},
		Graph: []domain.GraphBead{
			{ID: "mw-g.1", Title: "grilling: which queue", Status: "open", Created: statusNow.Add(-20 * 24 * time.Hour)},
		},
		Notes: map[string]string{
			application.PosternQuestionKey("mw-g.1"): `{"q":"which queue?"}`,
		},
	}
}

func TestDampersAreQuietOnACleanFixture(t *testing.T) {
	report := application.ReadDampers(aCleanDamperFixture(), application.DamperLimits{})
	if len(report.Alarms) != 0 {
		t.Fatalf("expected no alarm on a clean fixture, got %+v", report.Alarms)
	}
	if report.Counts.ReadyP1 != 2 || report.Counts.ReviewQueue != 2 {
		t.Errorf("expected the counts of the clean fixture to be read, got %+v", report.Counts)
	}
}

func TestEachDamperTripsOnce(t *testing.T) {
	for _, tc := range []struct {
		name      string
		principle string
		beads     []string
		change    func(f *application.DamperFacts)
	}{
		{"too many ready P1s", "WIP", []string{"mw-p.1", "mw-p.2"}, func(f *application.DamperFacts) {
			for i := 1; i <= 9; i++ {
				f.Work.Ready = append(f.Work.Ready, damperStory(fmt.Sprintf("mw-p.%d", i), 1, time.Hour))
			}
		}},
		{"a P1 untouched five days", "ageing", []string{"mw-old.1"}, func(f *application.DamperFacts) {
			f.Work.Ready = append(f.Work.Ready, damperStory("mw-old.1", 1, 6*24*time.Hour))
		}},
		{"a demo waiting a week", "ageing", []string{"mw-d.9"}, func(f *application.DamperFacts) {
			f.Work.Ready = append(f.Work.Ready, damperStory("mw-d.9", 3, 8*24*time.Hour, application.LabelDemo, application.LabelHitl))
		}},
		{"a grilling open a week with no card", "ageing", []string{"mw-g.9"}, func(f *application.DamperFacts) {
			f.Graph = append(f.Graph, domain.GraphBead{ID: "mw-g.9", Title: "grilling: bare", Status: "open", Created: statusNow.Add(-8 * 24 * time.Hour)})
		}},
		{"two Tester stories on one rig", "WIP", []string{"mw-t.1", "mw-t.2"}, func(f *application.DamperFacts) {
			second := damperStory("mw-t.2", 2, time.Hour, application.LabelTester)
			second.Status = "in_progress"
			f.Work.Running = append(f.Work.Running, second)
		}},
		{"his review queue over eight", "review", nil, func(f *application.DamperFacts) {
			for i := 1; i <= 7; i++ {
				f.Notes[application.PosternQuestionKey(fmt.Sprintf("mw-q.%d", i))] = `{"q":"which?"}`
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts := aCleanDamperFixture()
			tc.change(&facts)
			report := application.ReadDampers(facts, application.DamperLimits{})
			if len(report.Alarms) != 1 {
				t.Fatalf("expected exactly this alarm, got %+v", report.Alarms)
			}
			alarm := report.Alarms[0]
			if alarm.Principle != tc.principle {
				t.Errorf("expected the principle %q, got %q", tc.principle, alarm.Principle)
			}
			for _, bead := range tc.beads {
				if !slices.Contains(alarm.Beads, bead) {
					t.Errorf("expected the alarm to name %s, got %v", bead, alarm.Beads)
				}
			}
		})
	}
}

func TestADamperLineNamesFiveBeadsThenTheRest(t *testing.T) {
	facts := aCleanDamperFixture()
	for i := 1; i <= 12; i++ {
		facts.Work.Ready = append(facts.Work.Ready, damperStory(fmt.Sprintf("mw-p.%d", i), 1, time.Hour))
	}
	report := application.ReadDampers(facts, application.DamperLimits{})
	var b strings.Builder
	report.Write(&b)
	text := b.String()
	for _, want := range []string{application.DampersHeading, "WIP", "14 ready P1", "+9"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "mw-p.6") {
		t.Errorf("expected only five beads named, got:\n%s", text)
	}
}

func TestADamperLimitIsConfigurable(t *testing.T) {
	facts := aCleanDamperFixture()
	report := application.ReadDampers(facts, application.DamperLimits{ReadyP1: 1})
	if len(report.Alarms) != 1 || report.Alarms[0].Principle != "WIP" {
		t.Fatalf("expected the ready P1 alarm at a limit of 1, got %+v", report.Alarms)
	}
}

func TestStatusPrintsDampers(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.70", "A P1 left alone", "vps")
	if err := tracker.SetPriority("mw-gq6.70", 1); err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetUpdated("mw-gq6.70", statusNow.Add(-9*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	status := application.Status{
		Tracker: tracker, Notes: tracker, Host: "vps", Seat: "builder",
		Now:     func() time.Time { return statusNow },
		Dampers: application.DamperSurvey{Tracker: tracker, Notes: tracker},
	}
	report, err := status.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	printed := report.String()
	t.Logf("\n%s", printed)
	if !strings.Contains(printed, application.DampersHeading) || !strings.Contains(printed, "mw-gq6.70") {
		t.Errorf("expected the DAMPERS section naming the untouched P1, got:\n%s", printed)
	}
	quiet := application.Status{Tracker: aTrackerPathedToVPS(t), Host: "vps", Seat: "builder", Now: func() time.Time { return statusNow }}
	empty, err := quiet.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	if strings.Contains(empty.String(), application.DampersHeading) {
		t.Errorf("expected no DAMPERS section when nothing is tripped")
	}
}

// fullRenderStatus is an mw status over a seat whose memory of its rigs has the
// sizes given, with a 8000 byte budget.
func fullRenderStatus(t *testing.T, sizes ...application.RigMemorySize) application.StatusReport {
	t.Helper()
	vault := newFakeVault()
	vault.memorySizes = sizes
	tracker := aTrackerPathedToVPS(t)
	report, err := application.Status{
		Tracker:        tracker,
		Notes:          tracker,
		Vault:          vault,
		Host:           "vps",
		Seat:           "builder",
		HostSilence:    2 * time.Hour,
		RigMemoryBytes: 8000,
		Now:            func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	return report
}

// oneSpaced joins the report's wrapped lines, so a sentence the report breaks
// to the screen's width reads as the one line it is.
func oneSpaced(text string) string { return strings.Join(strings.Fields(text), " ") }

func TestStatusNamesARigWhoseRenderIsNearFullAndWhoseNewestFactsAreSearchOnly(t *testing.T) {
	printed := oneSpaced(fullRenderStatus(t, application.RigMemorySize{Rig: "lampas", Bytes: 7936, Facts: true, SearchOnlyNewest: 9}).String())
	want := "lampas memory: boot render 7936/8000; the last 9 new facts are search-only: run `mw memory make-room lampas --bytes N` (seats/mayor/pruning.md)"
	if !strings.Contains(printed, want) {
		t.Errorf("missing %q:\n%s", want, printed)
	}
	if !strings.Contains(printed, application.RigMemoryHeading) {
		t.Errorf("the line should sit under %s:\n%s", application.RigMemoryHeading, printed)
	}
}

func TestStatusSaysOneFactAsOneNotNines(t *testing.T) {
	printed := oneSpaced(fullRenderStatus(t, application.RigMemorySize{Rig: "lampas", Bytes: 7700, Facts: true, SearchOnlyNewest: 1}).String())
	if !strings.Contains(printed, "the last 1 new fact is search-only") {
		t.Errorf("missing the one-fact wording:\n%s", printed)
	}
}

func TestStatusIsSilentWhenTheRenderHasRoomOrTheNewestFactIsInTheRender(t *testing.T) {
	printed := fullRenderStatus(t,
		application.RigMemorySize{Rig: "roomy", Bytes: 5724, Facts: true, SearchOnlyNewest: 9},
		application.RigMemorySize{Rig: "booted", Bytes: 7936, Facts: true},
		application.RigMemorySize{Rig: "justunder", Bytes: 7599, Facts: true, SearchOnlyNewest: 3},
		application.RigMemorySize{Rig: "flat", Bytes: 7936, SearchOnlyNewest: 3},
	).String()
	if strings.Contains(printed, "search-only") || strings.Contains(printed, application.RigMemoryHeading) {
		t.Errorf("expected no line:\n%s", printed)
	}
}

func TestStatusNamesANearFullRenderAtTheEdgeOfFivePercent(t *testing.T) {
	printed := oneSpaced(fullRenderStatus(t, application.RigMemorySize{Rig: "edge", Bytes: 7600, Facts: true, SearchOnlyNewest: 3}).String())
	if !strings.Contains(printed, "edge memory: boot render 7600/8000; the last 3 new facts are search-only: run `mw memory make-room edge --bytes N`") {
		t.Errorf("missing the line at 95%%:\n%s", printed)
	}
}

func TestNewestSearchOnlyCountsTheCurrentFactsNewerThanTheNewestOneInTheRender(t *testing.T) {
	fact := func(slug, since string, status application.FactStatus, unbooted bool) application.RigFact {
		return application.RigFact{Slug: slug, Status: status, Since: since, Unbooted: unbooted}
	}
	cases := []struct {
		name  string
		facts []application.RigFact
		want  int
	}{
		{"none", nil, 0},
		{"all in the render", []application.RigFact{fact("a", "2026-10-01", application.FactCurrent, false)}, 0},
		{"three newest demoted", []application.RigFact{
			fact("old", "2026-10-01", application.FactCurrent, false),
			fact("n1", "2026-10-17", application.FactCurrent, true),
			fact("n2", "2026-10-17", application.FactCurrent, true),
			fact("n3", "2026-10-16", application.FactCurrent, true),
		}, 3},
		{"newest is in the render", []application.RigFact{
			fact("old", "2026-10-01", application.FactCurrent, true),
			fact("new", "2026-10-17", application.FactCurrent, false),
		}, 0},
		{"a booted fact on the same day leaves it silent", []application.RigFact{
			fact("a", "2026-10-17", application.FactCurrent, false),
			fact("b", "2026-10-17", application.FactCurrent, true),
		}, 0},
		{"retired and recheck facts are not new facts", []application.RigFact{
			fact("a", "2026-10-01", application.FactCurrent, false),
			fact("b", "2026-10-17", application.FactRetired, true),
			fact("c", "2026-10-17", application.FactRecheck, true),
		}, 0},
	}
	for _, c := range cases {
		if got := application.NewestSearchOnly(c.facts); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}
