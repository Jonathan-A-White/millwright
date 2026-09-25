package application_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// snapshotNow is the clock every snapshot test reads.
var snapshotNow = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

// aSnapshotTracker is an empty fake tracker, ready for epics and children.
func aSnapshotTracker() *apptest.FakeTracker {
	return apptest.NewFakeTracker()
}

// addChild files an open, unclaimed child under epicID.
func addChild(tracker *apptest.FakeTracker, epicID, id, title string) {
	tracker.AddStory(epicID, domain.Story{ID: id, Title: title})
}

// askQuestion marks id's question open, as PosternSend's recordQuestion does:
// a note holding the txid, and the QUESTION comment a snapshot reads back.
func askQuestion(t *testing.T, tracker *apptest.FakeTracker, id, askedAt, text, recommend, optionsCSV string) {
	t.Helper()
	ctx := context.Background()
	comment := "QUESTION " + askedAt + " asked by postern, txid txid-" + id + ": " + text +
		" (recommended " + recommend + "; options " + optionsCSV + ")"
	if err := tracker.CommentOnStory(ctx, id, comment); err != nil {
		t.Fatalf("commenting the question on %s: %v", id, err)
	}
	if err := tracker.SetNote(ctx, application.PosternQuestionKey(id), "txid-"+id); err != nil {
		t.Fatalf("marking %s's question open: %v", id, err)
	}
}

// closeLanded closes id, closed at the given time, with no VERIFIED comment.
func closeLanded(t *testing.T, tracker *apptest.FakeTracker, id string, closedAt time.Time) {
	t.Helper()
	if err := tracker.SetStatus(id, apptest.StatusClosed); err != nil {
		t.Fatalf("closing %s: %v", id, err)
	}
	if err := tracker.SetClosedAt(id, closedAt); err != nil {
		t.Fatalf("dating %s's close: %v", id, err)
	}
}

func snapshotDoc(t *testing.T, tracker *apptest.FakeTracker) application.PosternSnapshotDoc {
	t.Helper()
	doc, err := application.PosternSnapshot{
		Tracker: tracker,
		Notes:   tracker,
		Now:     func() time.Time { return snapshotNow },
	}.Build(context.Background())
	if err != nil {
		t.Fatalf("building the snapshot: %v", err)
	}
	return doc
}

func epicOf(t *testing.T, doc application.PosternSnapshotDoc, id string) application.PosternSnapshotEpic {
	t.Helper()
	for _, e := range doc.Epics {
		if e.ID == id {
			return e
		}
	}
	t.Fatalf("the snapshot has no epic %s; got %+v", id, doc.Epics)
	return application.PosternSnapshotEpic{}
}

func TestPosternSnapshotBuildsTheSectionSevenShapeWithTheRightGroupsAndOrder(t *testing.T) {
	tracker := aSnapshotTracker()
	tracker.AddEpic("mw-a", domain.Path{})
	tracker.DescribeEpic("mw-a", "Epic A", apptest.StatusOpen, 1)
	tracker.AddEpic("mw-b", domain.Path{})
	tracker.DescribeEpic("mw-b", "Epic B", apptest.StatusInProgress, 2)
	tracker.AddEpic("mw-c", domain.Path{})
	tracker.DescribeEpic("mw-c", "Epic C, long since finished", apptest.StatusClosed, 2)
	addChild(tracker, "mw-c", "mw-c.1", "Not live")

	// mw-a's children.
	addChild(tracker, "mw-a", "mw-a.1", "Ship now or wait?")
	askQuestion(t, tracker, "mw-a.1", "2026-09-24T12:00:00Z", "Ship now or wait?", "ship", "ship, wait")

	addChild(tracker, "mw-a", "mw-a.2", "Landed recently")
	closeLanded(t, tracker, "mw-a.2", snapshotNow.Add(-3*24*time.Hour))

	addChild(tracker, "mw-a", "mw-a.3", "In progress now")
	if err := tracker.SetStatus("mw-a.3", apptest.StatusInProgress); err != nil {
		t.Fatalf("claiming mw-a.3: %v", err)
	}

	addChild(tracker, "mw-a", "mw-a.4", "Open, priority 1")
	if err := tracker.SetPriority("mw-a.4", 1); err != nil {
		t.Fatalf("prioritising mw-a.4: %v", err)
	}

	addChild(tracker, "mw-a", "mw-a.5", "Open, priority 0, most urgent")
	if err := tracker.SetPriority("mw-a.5", 0); err != nil {
		t.Fatalf("prioritising mw-a.5: %v", err)
	}

	addChild(tracker, "mw-a", "blocker", "Not yet closed")
	addChild(tracker, "mw-a", "mw-a.6", "Open but blocked")
	tracker.Needs("mw-a.6", "blocker")

	addChild(tracker, "mw-a", "mw-a.7", "Held back")
	if err := tracker.SetStatus("mw-a.7", apptest.StatusDeferred); err != nil {
		t.Fatalf("holding mw-a.7: %v", err)
	}

	addChild(tracker, "mw-a", "mw-a.8", "Closed too long ago")
	closeLanded(t, tracker, "mw-a.8", snapshotNow.Add(-10*24*time.Hour))

	addChild(tracker, "mw-a", "mw-a.9", "Closed recently but verified")
	closeLanded(t, tracker, "mw-a.9", snapshotNow.Add(-1*24*time.Hour))
	if err := tracker.CommentOnStory(context.Background(), "mw-a.9", "VERIFIED GOOD, live on the VPS"); err != nil {
		t.Fatalf("verifying mw-a.9: %v", err)
	}

	// mw-b's one child.
	addChild(tracker, "mw-b", "mw-b.1", "Open and unblocked")

	doc := snapshotDoc(t, tracker)

	if want := snapshotNow.UTC().Format(time.RFC3339); doc.WrittenAt != want {
		t.Errorf("expected written_at %q, got %q", want, doc.WrittenAt)
	}
	if len(doc.Epics) != 2 {
		t.Fatalf("expected 2 live epics (mw-c is closed), got %d: %+v", len(doc.Epics), doc.Epics)
	}

	a := epicOf(t, doc, "mw-a")
	if a.Title != "Epic A" || a.Status != apptest.StatusOpen || a.Priority != "P1" {
		t.Fatalf("unexpected epic header: %+v", a)
	}

	if len(a.NeedsYou) != 1 || a.NeedsYou[0].ID != "mw-a.1" {
		t.Fatalf("expected needs_you to hold mw-a.1, got %+v", a.NeedsYou)
	}
	q := a.NeedsYou[0]
	if q.AskedAt != "2026-09-24T12:00:00Z" {
		t.Errorf("expected asked_at read from the QUESTION comment, got %q", q.AskedAt)
	}
	if q.Recommended != "ship" {
		t.Errorf("expected the recommended option \"ship\", got %q", q.Recommended)
	}
	if want := []string{"ship", "wait"}; !equalStrings(q.Options, want) {
		t.Errorf("expected the options %v, got %v", want, q.Options)
	}

	if len(a.Landed) != 1 || a.Landed[0].ID != "mw-a.2" {
		t.Fatalf("expected landed to hold only mw-a.2, got %+v", a.Landed)
	}
	if want := snapshotNow.Add(-3 * 24 * time.Hour).UTC().Format(time.RFC3339); a.Landed[0].LandedAt != want {
		t.Errorf("expected landed_at %q, got %q", want, a.Landed[0].LandedAt)
	}

	wantWorking := []string{"mw-a.3", "mw-a.5", "mw-a.4", "mw-a.1", "blocker"}
	var gotWorking []string
	for _, w := range a.Working {
		gotWorking = append(gotWorking, w.ID)
	}
	if !equalStrings(gotWorking, wantWorking) {
		t.Fatalf("expected working %v (in-progress first, then the frontier by priority), got %v", wantWorking, gotWorking)
	}

	if a.ClosedCount != 4 {
		t.Errorf("expected closed_count 4 (blocked, held, closed-too-old, closed-verified), got %d", a.ClosedCount)
	}

	b := epicOf(t, doc, "mw-b")
	if b.Status != apptest.StatusInProgress {
		t.Errorf("expected epic B's status to be in_progress, got %q", b.Status)
	}
	if len(b.Working) != 1 || b.Working[0].ID != "mw-b.1" {
		t.Fatalf("expected epic B's working to hold mw-b.1, got %+v", b.Working)
	}
}

func TestPosternSnapshotWorkingEntryNamesWhatItWaitsOn(t *testing.T) {
	tracker := aSnapshotTracker()
	tracker.AddEpic("mw-a", domain.Path{})
	addChild(tracker, "mw-a", "blocker", "Not yet closed")
	addChild(tracker, "mw-a", "mw-a.1", "Blocked")
	tracker.Needs("mw-a.1", "blocker")
	if err := tracker.SetStatus("mw-a.1", apptest.StatusInProgress); err != nil {
		t.Fatalf("claiming mw-a.1: %v", err)
	}

	doc := snapshotDoc(t, tracker)
	a := epicOf(t, doc, "mw-a")
	// mw-a.1 is in progress, so it is working despite waiting; "blocker" is
	// itself open and unblocked, so it is working too, as the frontier.
	if len(a.Working) != 2 {
		t.Fatalf("expected both mw-a.1 and blocker to be working, got %+v", a.Working)
	}
	if a.Working[0].ID != "mw-a.1" {
		t.Fatalf("expected the in-progress child first, got %+v", a.Working)
	}
	if want := []string{"blocker"}; !equalStrings(a.Working[0].Waits, want) {
		t.Errorf("expected waits %v, got %v", want, a.Working[0].Waits)
	}
}

func TestPosternSnapshotRunEncryptsAndWritesAtomically(t *testing.T) {
	tracker := aSnapshotTracker()
	tracker.AddEpic("mw-a", domain.Path{})
	addChild(tracker, "mw-a", "mw-a.1", "Open and unblocked")

	cipher := apptest.NewFakeCipher()
	file := apptest.NewFakeSnapshotFile("/tmp/mw-postern-snapshot-test.bin")

	snapshot := application.PosternSnapshot{
		Tracker:     tracker,
		Notes:       tracker,
		Cipher:      cipher,
		File:        file,
		GovernorKey: "governor-pubkey-hex",
		Now:         func() time.Time { return snapshotNow },
	}
	doc, err := snapshot.Run(context.Background())
	if err != nil {
		t.Fatalf("running the snapshot: %v", err)
	}
	if file.Writes() != 1 {
		t.Fatalf("expected exactly one write, got %d", file.Writes())
	}

	plaintext, err := cipher.Decrypt("any-private-key", string(file.Written()))
	if err != nil {
		t.Fatalf("decrypting what was written: %v", err)
	}
	wantJSON, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshaling the doc Run reported: %v", err)
	}
	if plaintext != string(wantJSON) {
		t.Fatalf("expected the written file to decrypt to the same JSON as the report:\nwant %s\ngot  %s", wantJSON, plaintext)
	}
}

func TestPosternSnapshotRunRefusesWithoutAGovernorKey(t *testing.T) {
	tracker := aSnapshotTracker()
	tracker.AddEpic("mw-a", domain.Path{})

	snapshot := application.PosternSnapshot{
		Tracker: tracker,
		Notes:   tracker,
		Cipher:  apptest.NewFakeCipher(),
		File:    apptest.NewFakeSnapshotFile("/tmp/mw-postern-snapshot-test.bin"),
		Now:     func() time.Time { return snapshotNow },
	}
	if _, err := snapshot.Run(context.Background()); err == nil {
		t.Fatal("expected Run to refuse without a governor key")
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
