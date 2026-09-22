package application_test

import (
	"context"
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
