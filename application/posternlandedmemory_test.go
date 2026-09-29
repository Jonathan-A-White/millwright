package application_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// memoryFailingNotes is a tracker whose note store refuses to write the
// landed memory, the way bd kv refuses a value too large for its column.
type memoryFailingNotes struct{ *apptest.FakeTracker }

func (n memoryFailingNotes) SetNote(ctx context.Context, key, value string) error {
	if key == application.PosternSnapshotMemoryKey {
		return fmt.Errorf("bd kv set %s %s is too large for column 'value'", key, value)
	}
	return n.FakeTracker.SetNote(ctx, key, value)
}

// aLandedTracker is a tracker with one live epic and n stories closed within
// the last day, each carrying comments comments of size bytes.
func aLandedTracker(t *testing.T, n, comments, size int) *apptest.FakeTracker {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	liveEpic(tracker, "mw-a", domain.Path{})
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("mw-a.%d", i)
		tracker.AddStory("mw-a", domain.Story{ID: id, Title: "Landing " + id})
		closedAt(t, tracker, id, viewNow.Add(-time.Duration(i)*time.Minute))
		for c := 0; c < comments; c++ {
			mustDo(t, tracker.CommentOnStory(context.Background(), id, strings.Repeat(string(rune('a'+c)), size)))
		}
	}
	return tracker
}

// A failure to write the landed memory is said on stderr, in one line, and
// never fails the view: it is still built, sealed and written.
func TestPosternViewSurvivesAFailureToWriteTheLandedMemory(t *testing.T) {
	tracker := aLandedTracker(t, 1, 1, 10)
	cipher := apptest.NewFakeCipher()
	file := apptest.NewFakeSnapshotFile("/state/postern/view.b64")
	var stderr strings.Builder

	doc, err := application.PosternView{
		Tracker: tracker, Notes: memoryFailingNotes{tracker}, Cipher: cipher, File: file,
		GovernorKey: "governor-pubkey-hex", Host: "desktop",
		Now: func() time.Time { return viewNow }, Err: &stderr,
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("expected the view to be built and written despite the memory, got %v", err)
	}
	if len(file.Written()) == 0 || len(doc.Beads) == 0 {
		t.Fatalf("expected a published view, got %d written byte(s) and %d bead(s)", len(file.Written()), len(doc.Beads))
	}
	assertOneLineAbout(t, stderr.String())
}

// The same for the snapshot.
func TestPosternSnapshotSurvivesAFailureToWriteTheLandedMemory(t *testing.T) {
	tracker := aLandedTracker(t, 1, 1, 10)
	file := apptest.NewFakeSnapshotFile("/state/mw/snapshot.bin")
	var stderr strings.Builder

	doc, err := application.PosternSnapshot{
		Tracker: tracker, Notes: memoryFailingNotes{tracker}, Cipher: apptest.NewFakeCipher(), File: file,
		GovernorKey: "governor-pubkey-hex",
		Now:         func() time.Time { return viewNow }, Err: &stderr,
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("expected the snapshot to be built and written despite the memory, got %v", err)
	}
	if len(file.Written()) == 0 || len(epicOf(t, doc, "mw-a").Landed) != 1 {
		t.Fatalf("expected a published snapshot with its landing, got %d written byte(s), %+v", len(file.Written()), doc.Epics)
	}
	assertOneLineAbout(t, stderr.String())
}

func assertOneLineAbout(t *testing.T, said string) {
	t.Helper()
	if strings.Count(said, "\n") != 1 || !strings.HasSuffix(said, "\n") || !strings.Contains(said, "landed comment memory") {
		t.Fatalf("expected one stderr line about the landed comment memory, got %q", said)
	}
	if len(said) > 600 {
		t.Fatalf("expected the line not to carry the whole value, got %d bytes", len(said))
	}
}

func landedMemoryOf(t *testing.T, tracker *apptest.FakeTracker) (string, map[string]json.RawMessage) {
	t.Helper()
	raw, err := tracker.Note(context.Background(), application.PosternSnapshotMemoryKey)
	if err != nil {
		t.Fatal(err)
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		t.Fatalf("the landed memory is not JSON: %v", err)
	}
	return raw, entries
}

// A busy day — twenty landings, each with five 5 KB comments — encodes well
// under the cap, from either reader.
func TestLandedMemoryOfABusyDayEncodesUnderTheCap(t *testing.T) {
	tracker := aLandedTracker(t, 20, 5, 5*1024)
	viewDoc(t, tracker)

	raw, entries := landedMemoryOf(t, tracker)
	if len(entries) != 20 {
		t.Fatalf("expected all twenty landings remembered, got %d", len(entries))
	}
	if len(raw) > application.PosternLandedMemoryLimit {
		t.Fatalf("expected at most %d bytes, got %d", application.PosternLandedMemoryLimit, len(raw))
	}

	snapshotTracker := aLandedTracker(t, 20, 5, 5*1024)
	if _, err := (application.PosternSnapshot{
		Tracker: snapshotTracker, Notes: snapshotTracker, Now: func() time.Time { return viewNow },
	}).Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	if raw, _ := landedMemoryOf(t, snapshotTracker); len(raw) > application.PosternLandedMemoryLimit {
		t.Fatalf("expected the snapshot's memory at most %d bytes, got %d", application.PosternLandedMemoryLimit, len(raw))
	}
}

// Past the cap the oldest-closed beads are dropped first, and the newest kept.
func TestLandedMemoryPastTheCapDropsTheOldestClosedFirst(t *testing.T) {
	tracker := aLandedTracker(t, 60, 3, 5*1024)
	viewDoc(t, tracker)

	raw, entries := landedMemoryOf(t, tracker)
	if len(raw) > application.PosternLandedMemoryLimit {
		t.Fatalf("expected at most %d bytes, got %d", application.PosternLandedMemoryLimit, len(raw))
	}
	if len(entries) == 0 || len(entries) >= 60 {
		t.Fatalf("expected some but not all of sixty landings remembered, got %d", len(entries))
	}
	// aLandedTracker closes mw-a.1 last-but-latest: the higher the number, the older.
	if _, ok := entries["mw-a.1"]; !ok {
		t.Errorf("expected the newest landing kept")
	}
	if _, ok := entries["mw-a.60"]; ok {
		t.Errorf("expected the oldest landing dropped")
	}
}

// What the landed and verify cards show survives the memory: the newest three
// comments, newest first, each cut to PosternLandedMemoryTextLimit runes, and
// the first sentence of the Mayor's landing check even when that comment is
// long, old, and outside those three — the same on the run that read the
// comments and on the run that remembered them.
func TestLandedAndVerifyCardsAreTheSameFromTheMemoryAsFromTheComments(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	liveEpic(tracker, "mw-a", domain.Path{})
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.1", Title: "Landed thing."})
	closedAt(t, tracker, "mw-a.1", viewNow.Add(-time.Hour))
	check := "Landing checked: merged clean, tests green. " + strings.Repeat("More detail. ", 500)
	mustDo(t, tracker.CommentOnStory(ctx, "mw-a.1", check))
	mustDo(t, tracker.CommentOnStory(ctx, "mw-a.1", "short"))
	mustDo(t, tracker.CommentOnStory(ctx, "mw-a.1", strings.Repeat("é", 5000)))
	mustDo(t, tracker.CommentOnStory(ctx, "mw-a.1", strings.Repeat("x", 600)))

	wantVerify := "Landed 28 Sep 11:00 UTC: Landed thing. Checked by the Mayor: merged clean, tests green. " +
		"Tap Verified if you have looked; optional, clears by itself 29 Sep 11:00 UTC."
	for _, run := range []string{"read from the comments", "remembered"} {
		doc := viewDoc(t, tracker)
		if got := viewNeed(t, doc, "verify", "mw-a.1").Text; got != wantVerify {
			t.Errorf("view, %s: expected the verify card %q, got %q", run, wantVerify, got)
		}
		snapshot, err := application.PosternSnapshot{
			Tracker: tracker, Notes: tracker, Now: func() time.Time { return viewNow },
		}.Build(ctx)
		if err != nil {
			t.Fatal(err)
		}
		landed := epicOf(t, snapshot, "mw-a").Landed
		if len(landed) != 1 || len(landed[0].Comments) != application.PosternSnapshotCommentLimit {
			t.Fatalf("snapshot, %s: expected one landing with three comments, got %+v", run, landed)
		}
		got := []string{landed[0].Comments[0].Text, landed[0].Comments[1].Text, landed[0].Comments[2].Text}
		want := []string{strings.Repeat("x", 600), strings.Repeat("é", 599) + "…", "short"}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("snapshot, %s: comment %d: expected %d rune(s) %.20q…, got %d rune(s) %.20q…",
					run, i, len([]rune(want[i])), want[i], len([]rune(got[i])), got[i])
			}
		}
	}
	if reads := tracker.CommentReads("mw-a.1"); reads != 1 {
		t.Errorf("expected the second run to read the memory, not the comments; comments read %d times", reads)
	}
}
