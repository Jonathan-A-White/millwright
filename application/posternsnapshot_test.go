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

func TestPosternSnapshotNeverReadsCommentsOfAClosedStoryOutsideTheWindow(t *testing.T) {
	tracker := aSnapshotTracker()
	tracker.AddEpic("mw-a", domain.Path{})

	addChild(tracker, "mw-a", "mw-a.1", "Closed long ago, question note never cleared")
	closeLanded(t, tracker, "mw-a.1", snapshotNow.Add(-10*24*time.Hour))
	if err := tracker.SetNote(context.Background(), application.PosternQuestionKey("mw-a.1"), "txid-mw-a.1"); err != nil {
		t.Fatalf("leaving a stale question note on mw-a.1: %v", err)
	}

	addChild(tracker, "mw-a", "mw-a.2", "Closed within the window")
	closeLanded(t, tracker, "mw-a.2", snapshotNow.Add(-3*24*time.Hour))
	if err := tracker.CommentOnStory(context.Background(), "mw-a.2", "shipped fine"); err != nil {
		t.Fatalf("leaving an ordinary comment on mw-a.2: %v", err)
	}

	doc := snapshotDoc(t, tracker)
	a := epicOf(t, doc, "mw-a")

	if tracker.CommentReads("mw-a.1") != 0 {
		t.Fatalf("expected mw-a.1's comments never to be read, got %d reads", tracker.CommentReads("mw-a.1"))
	}
	if tracker.CommentReads("mw-a.2") != 1 {
		t.Fatalf("expected mw-a.2's comments to be read once (it carries one, so the landed check must find it is not VERIFIED), got %d reads", tracker.CommentReads("mw-a.2"))
	}
	if len(a.NeedsYou) != 0 {
		t.Fatalf("expected a closed story never to appear in needs_you, got %+v", a.NeedsYou)
	}
	if len(a.Landed) != 1 || a.Landed[0].ID != "mw-a.2" {
		t.Fatalf("expected landed to hold only mw-a.2, got %+v", a.Landed)
	}
}

func TestPosternSnapshotRunChecksTheGovernorKeyBeforeReadingTheTracker(t *testing.T) {
	tracker := aSnapshotTracker()
	tracker.AddEpic("mw-a", domain.Path{})
	// If Build ever ran before the key check, this would be the error Run
	// returns instead of the "no governor key" one below.
	tracker.Err = fmt.Errorf("the tracker was read, but the governor key was never checked")

	snapshot := application.PosternSnapshot{
		Tracker: tracker,
		Notes:   tracker,
		Cipher:  apptest.NewFakeCipher(),
		File:    apptest.NewFakeSnapshotFile("/tmp/mw-postern-snapshot-test.bin"),
		Now:     func() time.Time { return snapshotNow },
	}
	_, err := snapshot.Run(context.Background())
	if err == nil {
		t.Fatal("expected Run to refuse without a governor key")
	}
	if !strings.Contains(err.Error(), "postern_governor_key") {
		t.Fatalf("expected the refusal to name postern_governor_key, got: %v", err)
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

	plaintext, _, err := cipher.Decrypt("any-private-key", string(file.Written()))
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

// TestPosternSnapshotWritesEmptyListsAsArraysNotNull covers mw-tfne4.14: the
// app's isValidSnapshot requires needs_you, landed, working, waits and
// options to be arrays on every epic, never null.
func TestPosternSnapshotWritesEmptyListsAsArraysNotNull(t *testing.T) {
	tracker := aSnapshotTracker()
	tracker.AddEpic("mw-a", domain.Path{})
	tracker.DescribeEpic("mw-a", "Epic A, nothing going on", apptest.StatusOpen, 1)

	tracker.AddEpic("mw-b", domain.Path{})
	tracker.DescribeEpic("mw-b", "Epic B, a working child and a question with no options", apptest.StatusOpen, 1)
	addChild(tracker, "mw-b", "mw-b.1", "Open and unblocked, so working with no waits")
	addChild(tracker, "mw-b", "mw-b.2", "Ship now or wait, no options given")
	askQuestion(t, tracker, "mw-b.2", "2026-09-24T12:00:00Z", "Ship now or wait?", "ship", "")

	doc := snapshotDoc(t, tracker)
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshaling the doc: %v", err)
	}
	got := string(data)

	a := epicOf(t, doc, "mw-a")
	if len(a.NeedsYou) != 0 || len(a.Landed) != 0 || len(a.Working) != 0 {
		t.Fatalf("expected epic A to have nothing going on, got %+v", a)
	}
	for _, want := range []string{`"needs_you":[]`, `"landed":[]`, `"working":[]`} {
		if !strings.Contains(got, want) {
			t.Errorf("expected epic A with nothing going on to write %s, got %s", want, got)
		}
	}

	b := epicOf(t, doc, "mw-b")
	var mwB1 *application.PosternSnapshotWorking
	for i := range b.Working {
		if b.Working[i].ID == "mw-b.1" {
			mwB1 = &b.Working[i]
		}
	}
	if mwB1 == nil || len(mwB1.Waits) != 0 {
		t.Fatalf("expected mw-b.1 to be working with no waits, got %+v", b.Working)
	}
	if len(b.NeedsYou) != 1 || len(b.NeedsYou[0].Options) != 0 {
		t.Fatalf("expected mw-b.2's question to hold no options, got %+v", b.NeedsYou)
	}
	if !strings.Contains(got, `"waits":[]`) {
		t.Errorf("expected a working entry with no waits to write \"waits\":[], got %s", got)
	}
	if !strings.Contains(got, `"options":[]`) {
		t.Errorf("expected a question with no options to write \"options\":[], got %s", got)
	}
}

// TestPosternSnapshotReadsEveryLiveEpicAndItsChildrensCommentsInOneCallEach
// covers mw-tfne4.17: with several live epics, each carrying a needs_you
// question and a landed child, Build must still ask the tracker for every
// epic's own fields in one ShowEpics call and every epic's children's
// comments in one StoriesComments call — not one of either per epic — while
// building exactly the same snapshot a call per epic would have.
func TestPosternSnapshotReadsEveryLiveEpicAndItsChildrensCommentsInOneCallEach(t *testing.T) {
	tracker := aSnapshotTracker()

	tracker.AddEpic("mw-a", domain.Path{})
	tracker.DescribeEpic("mw-a", "Epic A", apptest.StatusOpen, 1)
	addChild(tracker, "mw-a", "mw-a.1", "Ship now or wait?")
	askQuestion(t, tracker, "mw-a.1", "2026-09-24T12:00:00Z", "Ship now or wait?", "ship", "ship, wait")
	addChild(tracker, "mw-a", "mw-a.2", "Landed recently")
	closeLanded(t, tracker, "mw-a.2", snapshotNow.Add(-3*24*time.Hour))

	tracker.AddEpic("mw-b", domain.Path{})
	tracker.DescribeEpic("mw-b", "Epic B", apptest.StatusInProgress, 2)
	addChild(tracker, "mw-b", "mw-b.1", "Ready to ship?")
	askQuestion(t, tracker, "mw-b.1", "2026-09-24T13:00:00Z", "Ready to ship?", "ship", "")
	addChild(tracker, "mw-b", "mw-b.2", "Landed recently too")
	closeLanded(t, tracker, "mw-b.2", snapshotNow.Add(-1*24*time.Hour))

	doc := snapshotDoc(t, tracker)

	if got := tracker.ShowEpicsCalls(); got != 1 {
		t.Fatalf("expected the live epics to be read in one ShowEpics call, got %d", got)
	}
	if got := tracker.StoriesCommentsCalls(); got != 1 {
		t.Fatalf("expected every epic's children's comments to be read in one StoriesComments call, got %d", got)
	}

	for _, want := range []struct {
		epic, needsYou, landed string
	}{
		{"mw-a", "mw-a.1", "mw-a.2"},
		{"mw-b", "mw-b.1", "mw-b.2"},
	} {
		e := epicOf(t, doc, want.epic)
		if len(e.NeedsYou) != 1 || e.NeedsYou[0].ID != want.needsYou {
			t.Fatalf("expected %s's needs_you to hold %s, got %+v", want.epic, want.needsYou, e.NeedsYou)
		}
		if len(e.Landed) != 1 || e.Landed[0].ID != want.landed {
			t.Fatalf("expected %s's landed to hold %s, got %+v", want.epic, want.landed, e.Landed)
		}
	}
}

// TestPosternSnapshotNeverReadsALandedChildsCommentsWhenItHasNone covers
// mw-tfne4.24: on a live rig, almost every closed-within-window child across
// every live epic carries no comment at all, so cannot carry
// PosternSnapshotVerifiedMarker either — reading its comments back only to
// find none is what made mw-tfne4.17's single StoriesComments call slow
// again. Two epics, each with a needs_you child and a landed child with no
// comment, must still land in one StoriesComments call that names only the
// needs_you ids.
func TestPosternSnapshotNeverReadsALandedChildsCommentsWhenItHasNone(t *testing.T) {
	tracker := aSnapshotTracker()

	tracker.AddEpic("mw-a", domain.Path{})
	tracker.DescribeEpic("mw-a", "Epic A", apptest.StatusOpen, 1)
	addChild(tracker, "mw-a", "mw-a.1", "Ship now or wait?")
	askQuestion(t, tracker, "mw-a.1", "2026-09-24T12:00:00Z", "Ship now or wait?", "ship", "ship, wait")
	addChild(tracker, "mw-a", "mw-a.2", "Landed recently, never commented on")
	closeLanded(t, tracker, "mw-a.2", snapshotNow.Add(-3*24*time.Hour))

	tracker.AddEpic("mw-b", domain.Path{})
	tracker.DescribeEpic("mw-b", "Epic B", apptest.StatusInProgress, 2)
	addChild(tracker, "mw-b", "mw-b.1", "Ready to ship?")
	askQuestion(t, tracker, "mw-b.1", "2026-09-24T13:00:00Z", "Ready to ship?", "ship", "")
	addChild(tracker, "mw-b", "mw-b.2", "Landed recently too, never commented on")
	closeLanded(t, tracker, "mw-b.2", snapshotNow.Add(-1*24*time.Hour))

	doc := snapshotDoc(t, tracker)

	if got := tracker.StoriesCommentsCalls(); got != 1 {
		t.Fatalf("expected every epic's children's comments to be read in one StoriesComments call, got %d", got)
	}
	for _, id := range []string{"mw-a.1", "mw-b.1"} {
		if got := tracker.CommentReads(id); got != 1 {
			t.Fatalf("expected the needs_you candidate %s to be read once, got %d", id, got)
		}
	}
	for _, id := range []string{"mw-a.2", "mw-b.2"} {
		if got := tracker.CommentReads(id); got != 0 {
			t.Fatalf("expected the commentless landed candidate %s never to be read, got %d", id, got)
		}
	}

	for _, want := range []struct{ epic, landed string }{
		{"mw-a", "mw-a.2"},
		{"mw-b", "mw-b.2"},
	} {
		e := epicOf(t, doc, want.epic)
		if len(e.Landed) != 1 || e.Landed[0].ID != want.landed {
			t.Fatalf("expected %s's landed to hold %s despite never reading its comments, got %+v", want.epic, want.landed, e.Landed)
		}
	}
}

// TestPosternSnapshotRemembersALandedCandidatesVerdictBetweenBuilds covers
// mw-tfne4.27's AC1: a second Build over an unchanged listing must not read a
// landed candidate's comments again once its comment_count and verdict are
// already remembered, and must classify it exactly as the first Build did.
func TestPosternSnapshotRemembersALandedCandidatesVerdictBetweenBuilds(t *testing.T) {
	tracker := aSnapshotTracker()
	tracker.AddEpic("mw-a", domain.Path{})
	tracker.DescribeEpic("mw-a", "Epic A", apptest.StatusOpen, 1)
	addChild(tracker, "mw-a", "mw-a.1", "Landed recently, commented on")
	closeLanded(t, tracker, "mw-a.1", snapshotNow.Add(-3*24*time.Hour))
	if err := tracker.CommentOnStory(context.Background(), "mw-a.1", "shipped fine"); err != nil {
		t.Fatalf("commenting on mw-a.1: %v", err)
	}

	first := snapshotDoc(t, tracker)
	a := epicOf(t, first, "mw-a")
	if len(a.Landed) != 1 || a.Landed[0].ID != "mw-a.1" {
		t.Fatalf("expected the first build to land mw-a.1, got %+v", a.Landed)
	}
	if got := tracker.CommentReads("mw-a.1"); got != 1 {
		t.Fatalf("expected the first build to read mw-a.1's comments once, got %d", got)
	}

	second := snapshotDoc(t, tracker)
	b := epicOf(t, second, "mw-a")
	if len(b.Landed) != 1 || b.Landed[0].ID != "mw-a.1" {
		t.Fatalf("expected the second build to classify mw-a.1 the same way, got %+v", b.Landed)
	}
	if got := tracker.CommentReads("mw-a.1"); got != 1 {
		t.Fatalf("expected the second build to remember mw-a.1's verdict rather than rereading its comments, got %d total reads", got)
	}
}

// TestPosternSnapshotRereadsALandedCandidateWhoseCommentCountRose covers
// mw-tfne4.27's AC2: a risen comment_count invalidates the remembered
// verdict, so it is read again; once that read finds
// PosternSnapshotVerifiedMarker, the candidate drops out of landed, and that
// new verdict is itself remembered without a further read.
func TestPosternSnapshotRereadsALandedCandidateWhoseCommentCountRose(t *testing.T) {
	tracker := aSnapshotTracker()
	tracker.AddEpic("mw-a", domain.Path{})
	tracker.DescribeEpic("mw-a", "Epic A", apptest.StatusOpen, 1)
	addChild(tracker, "mw-a", "mw-a.1", "Landed recently, commented on")
	closeLanded(t, tracker, "mw-a.1", snapshotNow.Add(-3*24*time.Hour))
	if err := tracker.CommentOnStory(context.Background(), "mw-a.1", "shipped fine"); err != nil {
		t.Fatalf("commenting on mw-a.1: %v", err)
	}

	first := snapshotDoc(t, tracker)
	a := epicOf(t, first, "mw-a")
	if len(a.Landed) != 1 || a.Landed[0].ID != "mw-a.1" {
		t.Fatalf("expected the first build to land mw-a.1, got %+v", a.Landed)
	}

	// A VERIFIED comment lands after the first build: comment_count rises.
	if err := tracker.CommentOnStory(context.Background(), "mw-a.1", "VERIFIED GOOD, live on the VPS"); err != nil {
		t.Fatalf("verifying mw-a.1: %v", err)
	}

	second := snapshotDoc(t, tracker)
	b := epicOf(t, second, "mw-a")
	if len(b.Landed) != 0 {
		t.Fatalf("expected mw-a.1 to drop out of landed once verified, got %+v", b.Landed)
	}
	if got := tracker.CommentReads("mw-a.1"); got != 2 {
		t.Fatalf("expected the risen comment_count to trigger a fresh read, got %d total reads", got)
	}

	// A third build with nothing further changed must not read it again: the
	// verified verdict is itself remembered.
	third := snapshotDoc(t, tracker)
	c := epicOf(t, third, "mw-a")
	if len(c.Landed) != 0 {
		t.Fatalf("expected mw-a.1 to stay out of landed, got %+v", c.Landed)
	}
	if got := tracker.CommentReads("mw-a.1"); got != 2 {
		t.Fatalf("expected a verified candidate's memory to stick without a further read, got %d total reads", got)
	}
}

// TestPosternSnapshotDropsAMemoryEntryOnceItsIdLeavesTheWindow covers
// mw-tfne4.27's AC2: a landed candidate's memory entry must not survive in
// the note once it ages out of PosternSnapshotWindow.
func TestPosternSnapshotDropsAMemoryEntryOnceItsIdLeavesTheWindow(t *testing.T) {
	tracker := aSnapshotTracker()
	tracker.AddEpic("mw-a", domain.Path{})
	tracker.DescribeEpic("mw-a", "Epic A", apptest.StatusOpen, 1)
	addChild(tracker, "mw-a", "mw-a.1", "Landed recently, commented on")
	closeLanded(t, tracker, "mw-a.1", snapshotNow.Add(-3*24*time.Hour))
	if err := tracker.CommentOnStory(context.Background(), "mw-a.1", "shipped fine"); err != nil {
		t.Fatalf("commenting on mw-a.1: %v", err)
	}

	build := func(now time.Time) {
		t.Helper()
		_, err := application.PosternSnapshot{
			Tracker: tracker,
			Notes:   tracker,
			Now:     func() time.Time { return now },
		}.Build(context.Background())
		if err != nil {
			t.Fatalf("building the snapshot: %v", err)
		}
	}

	build(snapshotNow)
	raw, err := tracker.Note(context.Background(), application.PosternSnapshotMemoryKey)
	if err != nil {
		t.Fatalf("reading the memory note: %v", err)
	}
	if !strings.Contains(raw, "mw-a.1") {
		t.Fatalf("expected the memory to remember mw-a.1 after the first build, got %q", raw)
	}

	// Nine days later mw-a.1 has aged out of the 7-day landed window.
	build(snapshotNow.Add(9 * 24 * time.Hour))
	raw, err = tracker.Note(context.Background(), application.PosternSnapshotMemoryKey)
	if err != nil {
		t.Fatalf("reading the memory note: %v", err)
	}
	if strings.Contains(raw, "mw-a.1") {
		t.Fatalf("expected mw-a.1 to be dropped from the memory once it left the window, got %q", raw)
	}
}

// TestPosternSnapshotLandedMemoryIsOneNoteReadOnceAndWrittenOnlyWhenChanged
// covers mw-tfne4.27's AC3: the memory is kept as one note, read once per
// Build, and rewritten only when its content actually changed.
func TestPosternSnapshotLandedMemoryIsOneNoteReadOnceAndWrittenOnlyWhenChanged(t *testing.T) {
	tracker := aSnapshotTracker()
	tracker.AddEpic("mw-a", domain.Path{})
	tracker.DescribeEpic("mw-a", "Epic A", apptest.StatusOpen, 1)
	addChild(tracker, "mw-a", "mw-a.1", "Landed recently, commented on")
	closeLanded(t, tracker, "mw-a.1", snapshotNow.Add(-3*24*time.Hour))
	if err := tracker.CommentOnStory(context.Background(), "mw-a.1", "shipped fine"); err != nil {
		t.Fatalf("commenting on mw-a.1: %v", err)
	}

	snapshotDoc(t, tracker)
	if got := tracker.NoteReads(application.PosternSnapshotMemoryKey); got != 1 {
		t.Fatalf("expected the memory note to be read once, got %d", got)
	}
	if got := tracker.NoteWrites(application.PosternSnapshotMemoryKey); got != 1 {
		t.Fatalf("expected the first build to write the memory note once, got %d", got)
	}

	snapshotDoc(t, tracker)
	if got := tracker.NoteReads(application.PosternSnapshotMemoryKey); got != 2 {
		t.Fatalf("expected the second build to read the memory note once more, got %d", got)
	}
	if got := tracker.NoteWrites(application.PosternSnapshotMemoryKey); got != 1 {
		t.Fatalf("expected the second build, seeing nothing changed, to write the memory note no further, got %d", got)
	}
}

// TestPosternSnapshotBuildsWithAMissingOrUnreadableMemoryNote covers
// mw-tfne4.27's AC3: a note that does not parse means remember nothing, and
// Build still succeeds, reading the affected candidate's comments fresh.
func TestPosternSnapshotBuildsWithAMissingOrUnreadableMemoryNote(t *testing.T) {
	tracker := aSnapshotTracker()
	tracker.AddEpic("mw-a", domain.Path{})
	addChild(tracker, "mw-a", "mw-a.1", "Landed recently, commented on")
	closeLanded(t, tracker, "mw-a.1", snapshotNow.Add(-3*24*time.Hour))
	if err := tracker.CommentOnStory(context.Background(), "mw-a.1", "shipped fine"); err != nil {
		t.Fatalf("commenting on mw-a.1: %v", err)
	}
	if err := tracker.SetNote(context.Background(), application.PosternSnapshotMemoryKey, "not valid json"); err != nil {
		t.Fatalf("seeding an unreadable memory note: %v", err)
	}

	doc := snapshotDoc(t, tracker)
	a := epicOf(t, doc, "mw-a")
	if len(a.Landed) != 1 || a.Landed[0].ID != "mw-a.1" {
		t.Fatalf("expected an unreadable memory note to be treated as remembering nothing, got %+v", a.Landed)
	}
	if got := tracker.CommentReads("mw-a.1"); got != 1 {
		t.Fatalf("expected mw-a.1's comments to be read since memory held nothing usable, got %d", got)
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
