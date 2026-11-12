package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// recordingNotes is the tracker's key-value store as the host's give-way note is
// written to it: every call goes in the host's shared Order, so a test can say
// what came before the last stop.
type recordingNotes struct {
	host    *apptest.FakeGiveWayHost
	values  map[string]string
	sets    int
	clears  int
	setErr  error
	hangSet bool
}

func (n *recordingNotes) SetNote(ctx context.Context, key, value string) error {
	n.host.Note("note set")
	n.sets++
	if n.hangSet {
		<-ctx.Done()
		return ctx.Err()
	}
	if n.setErr != nil {
		return n.setErr
	}
	if n.values == nil {
		n.values = map[string]string{}
	}
	n.values[key] = value
	return nil
}

func (n *recordingNotes) ClearNote(_ context.Context, key string) error {
	n.host.Note("note clear")
	n.clears++
	if n.setErr != nil {
		return n.setErr
	}
	delete(n.values, key)
	return nil
}

func TestGiveWayNoteIsWrittenOnlyAfterTheLastStop(t *testing.T) {
	way, host, _, _ := aGiveWayWorld()
	notes := &recordingNotes{host: host}
	way.Notes = notes
	if err := way.On(context.Background(), application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	set, lastStop := -1, -1
	for i, event := range host.Order {
		switch {
		case event == "note set":
			set = i
		case strings.HasPrefix(event, "stop "):
			lastStop = i
		}
	}
	if set < 0 || lastStop < 0 || set < lastStop {
		t.Fatalf("the note must be written after the last stop; order = %v", host.Order)
	}
	want := "2026-11-13T16:40:00Z mw@desktop 2026-11-13T16:40:00Z"
	if got := notes.values[application.GiveWayKey("desktop")]; got != want {
		t.Fatalf("note = %q, want %q", got, want)
	}
	if entry := logEntry(t, host.Lines[0]); entry["note"] != "set" {
		t.Fatalf("log entry = %v, want note set", entry)
	}
}

func TestGiveWayNoteSurvivesATrackerThatErrorsOrTimesOut(t *testing.T) {
	cases := map[string]func(*recordingNotes){
		"errors":    func(n *recordingNotes) { n.setErr = errors.New("bd: server gone") },
		"times out": func(n *recordingNotes) { n.hangSet = true },
	}
	for name, break_ := range cases {
		t.Run(name, func(t *testing.T) {
			way, host, _, _ := aGiveWayWorld()
			notes := &recordingNotes{host: host}
			break_(notes)
			way.Notes, way.NoteTimeout = notes, 20*time.Millisecond
			start := time.Now()
			if err := way.On(context.Background(), application.GiveWayRequest{}); err != nil {
				t.Fatalf("on failed because the note did: %v", err)
			}
			if time.Since(start) > 2*time.Second {
				t.Fatalf("on waited %s on the tracker", time.Since(start))
			}
			if host.Marker == nil || len(host.Stopped()) != 5 {
				t.Fatalf("a failed note changed the freeze: marker %+v, stopped %v", host.Marker, host.Stopped())
			}
			entry := logEntry(t, host.Lines[0])
			if note, _ := entry["note"].(string); !strings.HasPrefix(note, "failed:") {
				t.Fatalf("log entry = %v, want the failure under note", entry)
			}
			// The next renew tries again at once: the failed write set no time.
			notes.setErr, notes.hangSet = nil, false
			if err := way.On(context.Background(), application.GiveWayRequest{}); err != nil {
				t.Fatal(err)
			}
			if notes.sets != 2 || notes.values[application.GiveWayKey("desktop")] == "" {
				t.Fatalf("the renew after a failure did not write the note: %d sets, %v", notes.sets, notes.values)
			}
		})
	}
}

func TestGiveWayNoteRenewRewritesAtMostEveryTenMinutes(t *testing.T) {
	way, host, _, now := aGiveWayWorld()
	notes := &recordingNotes{host: host}
	way.Notes = notes
	ctx := context.Background()
	for _, step := range []struct {
		after time.Duration
		sets  int
		note  string
	}{{0, 1, "set"}, {5 * time.Minute, 1, "kept"}, {5 * time.Minute, 2, "set"}, {5 * time.Minute, 2, "kept"}} {
		*now = now.Add(step.after)
		if err := way.On(ctx, application.GiveWayRequest{}); err != nil {
			t.Fatal(err)
		}
		if notes.sets != step.sets {
			t.Fatalf("after %s: %d writes, want %d", step.after, notes.sets, step.sets)
		}
		if entry := logEntry(t, host.Lines[len(host.Lines)-1]); entry["note"] != step.note {
			t.Fatalf("after %s: log entry %v, want note %s", step.after, entry, step.note)
		}
	}
}

func TestGiveWayOffAndTheExpiredPathClearTheNote(t *testing.T) {
	ctx := context.Background()
	way, host, _, now := aGiveWayWorld()
	notes := &recordingNotes{host: host}
	way.Notes = notes
	if err := way.On(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	if err := way.Off(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	if len(notes.values) != 0 || notes.clears != 1 {
		t.Fatalf("off left the note: %v (%d clears)", notes.values, notes.clears)
	}
	if entry := logEntry(t, host.Lines[len(host.Lines)-1]); entry["note"] != "cleared" {
		t.Fatalf("off entry = %v, want note cleared", entry)
	}

	if err := way.On(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(20 * time.Minute)
	if _, giving, err := way.Standing(ctx, true); giving || err != nil {
		t.Fatalf("a stale marker: giving=%v err=%v", giving, err)
	}
	if len(notes.values) != 0 {
		t.Fatalf("the expired path left the note: %v", notes.values)
	}
	if entry := logEntry(t, host.Lines[len(host.Lines)-1]); entry["event"] != "expired" || entry["note"] != "cleared" {
		t.Fatalf("expired entry = %v", entry)
	}
}

func TestGiveWayOffSurvivesATrackerThatErrors(t *testing.T) {
	ctx := context.Background()
	way, host, _, _ := aGiveWayWorld()
	notes := &recordingNotes{host: host}
	way.Notes = notes
	if err := way.On(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	notes.setErr = errors.New("bd: server gone")
	if err := way.Off(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatalf("off failed because the note did: %v", err)
	}
	if host.Marker != nil || len(host.Stopped()) != 0 {
		t.Fatalf("off did not go on: marker %+v, stopped %v", host.Marker, host.Stopped())
	}
	if entry := logEntry(t, host.Lines[len(host.Lines)-1]); !strings.HasPrefix(entry["note"].(string), "failed to clear:") {
		t.Fatalf("off entry = %v", entry)
	}
}

func TestGiveWayANoteIsBelievedFor2xGiveWayStaleAndNotBeyond(t *testing.T) {
	now := time.Date(2026, 11, 13, 17, 0, 0, 0, time.UTC)
	note := application.GiveWayNote{Since: now.Add(-3 * time.Hour), By: "windows-unlock", Seen: now.Add(-29 * time.Minute)}
	tracker := apptest.NewFakeTracker()
	if err := tracker.SetNote(context.Background(), application.GiveWayKey("desktop"), note.String()); err != nil {
		t.Fatal(err)
	}
	reader := application.GiveWayReader{Notes: tracker, Stale: 15 * time.Minute}
	if read := reader.Of(context.Background(), "desktop", now); !read.Stands || read.Stale {
		t.Fatalf("29 minutes since the last write, giveway_stale 15m: %+v", read)
	}
	if read := reader.Of(context.Background(), "desktop", now.Add(2*time.Minute)); read.Stands || !read.Stale {
		t.Fatalf("31 minutes since the last write must not be believed: %+v", read)
	}
	if reader.Stands(context.Background(), "laptop", now) {
		t.Fatal("a host with no note gives way")
	}
	if (application.GiveWayReader{}).Stands(context.Background(), "desktop", now) {
		t.Fatal("a reader with no notes gives way")
	}
	// A note without the seen time (the value in the story: "<since> <by>") reads
	// as written at its since time.
	parsed, ok := application.ParseGiveWayNote("2026-11-13T16:40:00Z windows-unlock")
	if !ok || !parsed.Seen.Equal(parsed.Since) {
		t.Fatalf("parsed = %+v %v", parsed, ok)
	}
	if _, ok := application.ParseGiveWayNote("soon"); ok {
		t.Fatal("a note that is not a time parsed")
	}
}

// A claim held on a host whose note stands is frozen: mw dispatch on another host
// does not give it back from under its session, and with no note, or a stale
// one, it does as it always did.
func TestDispatchDoesNotGiveBackAnUnhostedClaimOfAHostThatGivesWay(t *testing.T) {
	for name, c := range map[string]struct {
		note     string
		reclaims int
	}{
		"standing": {"16:50", 0},
		"none":     {"", 1},
		"stale":    {"15:00", 1},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			dispatch, tracker, _, _, _ := aFactory(t)
			now := time.Date(2026, 11, 13, 17, 0, 0, 0, time.UTC)
			dispatch.Now = func() time.Time { return now }
			dispatch.Memory, dispatch.GiveWayStale = tracker, 15*time.Minute
			tracker.Clock = func() time.Time { return now }
			anUnhostedClaim(t, tracker, "mw-gq6.9", "mw@desktop", now, 7*time.Hour)
			if c.note != "" {
				seen, err := time.Parse("2006-01-02 15:04", "2026-11-13 "+c.note)
				if err != nil {
					t.Fatal(err)
				}
				note := application.GiveWayNote{Since: seen.Add(-time.Hour), By: "windows-unlock", Seen: seen}
				if err := tracker.SetNote(ctx, application.GiveWayKey("desktop"), note.String()); err != nil {
					t.Fatal(err)
				}
			}
			report, err := dispatch.Run(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Reclaimed) != c.reclaims {
				t.Fatalf("reclaimed %+v, want %d", report.Reclaimed, c.reclaims)
			}
		})
	}
}

func TestStuckSurveyPassesOverAClaimOnAHostThatGivesWay(t *testing.T) {
	for name, noted := range map[string]bool{"a note stands": true, "no note": false} {
		t.Run(name, func(t *testing.T) {
			f := newStuckFixture()
			f.bead("mw-s.1", "in_progress", nil)
			f.bead("mw-s.2", "open", nil, "mw-s.1")
			f.tracker.Clock = func() time.Time { return stuckNow }
			if err := f.tracker.ClaimAs("mw-s.1", "mw@desktop", stuckNow.Add(-7*time.Hour)); err != nil {
				t.Fatal(err)
			}
			if noted {
				note := application.GiveWayNote{Since: stuckNow.Add(-time.Hour), By: "windows-unlock", Seen: stuckNow.Add(-5 * time.Minute)}
				if err := f.tracker.SetNote(context.Background(), application.GiveWayKey("desktop"), note.String()); err != nil {
					t.Fatal(err)
				}
			}
			report, err := application.StuckSurvey{
				Graph: f.graph, Comments: f.tracker, Claims: f.tracker, Now: func() time.Time { return stuckNow },
				GiveWay: application.GiveWayReader{Notes: f.tracker},
			}.Find(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got := len(report.Roots) == 1; got == noted {
				t.Fatalf("roots = %v with a note standing: %v", rootIDs(report), noted)
			}
		})
	}
}
