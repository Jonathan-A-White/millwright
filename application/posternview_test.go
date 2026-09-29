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

// viewNow is the clock every live view test reads.
var viewNow = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

// viewDoc builds the live view of tracker on host "desktop", failing the test
// on an error.
func viewDoc(t *testing.T, tracker *apptest.FakeTracker) application.PosternViewDoc {
	t.Helper()
	doc, err := application.PosternView{
		Tracker: tracker,
		Notes:   tracker,
		Host:    "desktop",
		Now:     func() time.Time { return viewNow },
	}.Build(context.Background())
	if err != nil {
		t.Fatalf("building the view: %v", err)
	}
	return doc
}

// viewBead is the bead id in doc, failing the test when it is not there.
func viewBead(t *testing.T, doc application.PosternViewDoc, id string) application.PosternViewBead {
	t.Helper()
	for _, b := range doc.Beads {
		if b.ID == id {
			return b
		}
	}
	t.Fatalf("the view has no bead %s; it has %v", id, viewIDs(doc))
	return application.PosternViewBead{}
}

func viewIDs(doc application.PosternViewDoc) []string {
	ids := make([]string, 0, len(doc.Beads))
	for _, b := range doc.Beads {
		ids = append(ids, b.ID)
	}
	return ids
}

func viewHas(doc application.PosternViewDoc, id string) bool {
	for _, b := range doc.Beads {
		if b.ID == id {
			return true
		}
	}
	return false
}

// viewNeeds is doc's needs as "kind:bead" strings, in the order the view
// lists them.
func viewNeeds(doc application.PosternViewDoc) []string {
	out := make([]string, 0, len(doc.Needs))
	for _, n := range doc.Needs {
		out = append(out, n.Kind+":"+n.Bead)
	}
	return out
}

func viewNeed(t *testing.T, doc application.PosternViewDoc, kind, bead string) application.PosternViewNeed {
	t.Helper()
	for _, n := range doc.Needs {
		if n.Kind == kind && n.Bead == bead {
			return n
		}
	}
	t.Fatalf("the view has no %s need on %q; it has %v", kind, bead, viewNeeds(doc))
	return application.PosternViewNeed{}
}

func viewLacksNeed(t *testing.T, doc application.PosternViewDoc, kind, bead string) {
	t.Helper()
	for _, n := range doc.Needs {
		if n.Kind == kind && n.Bead == bead {
			t.Fatalf("expected no %s need on %q, got %+v", kind, bead, n)
		}
	}
}

func mustDo(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// liveEpic files an open epic with defaults, titled after its id.
func liveEpic(tracker *apptest.FakeTracker, id string, defaults domain.Path) {
	tracker.AddEpic(id, defaults)
	tracker.DescribeEpic(id, "Epic "+id, apptest.StatusOpen, 1)
}

func closedAt(t *testing.T, tracker *apptest.FakeTracker, id string, at time.Time) {
	t.Helper()
	mustDo(t, tracker.SetStatus(id, apptest.StatusClosed))
	mustDo(t, tracker.SetClosedAt(id, at))
}

// The view carries every live epic and every bead under it, with the §11
// fields: the story's merged Path, its type, labels, times, attempts, a plain
// summary cut to 280 runes and its comment count; a bead closed within 7 days
// is in it, one closed earlier only counts in its epic's done_earlier.
func TestPosternViewCarriesEveryLiveEpicAndItsBeadsWithTheSectionElevenFields(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	liveEpic(tracker, "mw-a", domain.Path{Rig: "postern", Branch: "main", Host: "desktop", Model: "sonnet"})
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.1", Title: "Pick the storage engine", Overrides: domain.Path{Effort: "high"}})
	mustDo(t, tracker.SetType("mw-a.1", "bug"))
	mustDo(t, tracker.SetLabels("mw-a.1", "hitl"))
	mustDo(t, tracker.SetCreated("mw-a.1", viewNow.Add(-27*time.Hour)))
	mustDo(t, tracker.SetUpdated("mw-a.1", viewNow.Add(-2*time.Hour)))
	mustDo(t, tracker.SetDescription("mw-a.1", "## Why\n\nThe **store** must be `fast`.\n"+strings.Repeat("x", 400)))
	mustDo(t, tracker.SetStoryMetadata(context.Background(), "mw-a.1", map[string]string{application.AttemptsField: "2"}))
	mustDo(t, tracker.CommentOnStory(context.Background(), "mw-a.1", "one"))

	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.2", Title: "Landed this week"})
	closedAt(t, tracker, "mw-a.2", viewNow.Add(-3*24*time.Hour))
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.3", Title: "Landed last month"})
	closedAt(t, tracker, "mw-a.3", viewNow.Add(-30*24*time.Hour))
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.4", Title: "Landed long ago, time unknown"})
	mustDo(t, tracker.SetStatus("mw-a.4", apptest.StatusClosed))

	liveEpic(tracker, "mw-z", domain.Path{})
	tracker.DescribeEpic("mw-z", "Long finished", apptest.StatusClosed, 2)
	tracker.AddStory("mw-z", domain.Story{ID: "mw-z.1", Title: "Not live"})

	doc := viewDoc(t, tracker)

	if doc.V != 2 || doc.Host != "desktop" || doc.WrittenAt != "2026-09-28T12:00:00Z" {
		t.Fatalf("expected v 2 written by desktop at 12:00, got v %d host %q at %q", doc.V, doc.Host, doc.WrittenAt)
	}
	for _, gone := range []string{"mw-a.3", "mw-a.4", "mw-z", "mw-z.1"} {
		if viewHas(doc, gone) {
			t.Errorf("expected %s not to be in the view, got %v", gone, viewIDs(doc))
		}
	}
	epic := viewBead(t, doc, "mw-a")
	if epic.Type != "epic" || epic.Status != "open" || epic.Priority != 1 || epic.Parent != "" || epic.DoneEarlier != 2 {
		t.Errorf("expected the open root epic with 2 children done earlier, got %+v", epic)
	}
	if epic.Path == nil || epic.Path.Rig != "postern" || epic.Path.Model != "sonnet" {
		t.Errorf("expected the epic's own defaults as its path, got %+v", epic.Path)
	}

	story := viewBead(t, doc, "mw-a.1")
	if story.Type != "bug" || story.Parent != "mw-a" || story.Priority != 2 || story.Attempts != 2 || story.Comments != 1 {
		t.Errorf("expected the bug under mw-a with 2 attempts and 1 comment, got %+v", story)
	}
	if len(story.Labels) != 1 || story.Labels[0] != "hitl" {
		t.Errorf("expected the hitl label, got %v", story.Labels)
	}
	if story.Created != "2026-09-27T09:00:00Z" || story.Updated != "2026-09-28T10:00:00Z" || story.Started != "" || story.Closed != "" {
		t.Errorf("expected RFC 3339 times or empty, got created %q updated %q started %q closed %q",
			story.Created, story.Updated, story.Started, story.Closed)
	}
	if story.Path == nil || *story.Path != (application.PosternViewPath{Rig: "postern", Branch: "main", Host: "desktop", Model: "sonnet", Effort: "high"}) {
		t.Errorf("expected the merged path, got %+v", story.Path)
	}
	if n := len([]rune(story.Summary)); n > 280 {
		t.Errorf("expected a summary of at most 280 runes, got %d", n)
	}
	if !strings.HasPrefix(story.Summary, "Why The store must be fast. xxx") {
		t.Errorf("expected a plain-text summary, got %q", story.Summary)
	}
	if landed := viewBead(t, doc, "mw-a.2"); landed.Closed != "2026-09-25T12:00:00Z" || landed.Status != "closed" {
		t.Errorf("expected mw-a.2 closed three days ago, got %+v", landed)
	}
}

// Every bead under a live epic at any depth: a live child epic is read as the
// live epic it is; a held one, which is not live, is read too, so its own
// children are still in the view.
func TestPosternViewReadsChildEpicsAtAnyDepth(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	liveEpic(tracker, "mw-a", domain.Path{Rig: "millwright", Branch: "main"})
	tracker.AddChildEpic("mw-a", "mw-a.1", "Live child epic")
	tracker.DescribeEpic("mw-a.1", "Live child epic", apptest.StatusInProgress, 2)
	tracker.AddStory("mw-a.1", domain.Story{ID: "mw-a.1.1", Title: "Grandchild"})

	tracker.AddChildEpic("mw-a", "mw-a.2", "Held child epic")
	tracker.DescribeEpic("mw-a.2", "Held child epic", apptest.StatusDeferred, 2)
	mustDo(t, tracker.SetStatus("mw-a.2", apptest.StatusDeferred))
	tracker.AddStory("mw-a.2", domain.Story{ID: "mw-a.2.1", Title: "Under the held epic"})
	mustDo(t, tracker.SetStatus("mw-a.2.1", apptest.StatusDeferred))

	doc := viewDoc(t, tracker)

	for _, id := range []string{"mw-a", "mw-a.1", "mw-a.1.1", "mw-a.2", "mw-a.2.1"} {
		if !viewHas(doc, id) {
			t.Errorf("expected %s in the view, got %v", id, viewIDs(doc))
		}
	}
	if child := viewBead(t, doc, "mw-a.1"); child.Parent != "mw-a" || child.Type != "epic" || child.Status != "in_progress" {
		t.Errorf("expected the live child epic under mw-a, got %+v", child)
	}
	if held := viewBead(t, doc, "mw-a.2"); held.Parent != "mw-a" || held.Status != "deferred" {
		t.Errorf("expected the held child epic under mw-a, got %+v", held)
	}
	if grand := viewBead(t, doc, "mw-a.2.1"); grand.Parent != "mw-a.2" {
		t.Errorf("expected mw-a.2.1 under mw-a.2, got %+v", grand)
	}
}

// A live epic's parent chain is in the view up to its root, however the
// parents stand, read in one batch rather than once each.
func TestPosternViewCarriesALiveEpicsParentChainToTheRoot(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-root", domain.Path{})
	tracker.DescribeEpic("mw-root", "The map", apptest.StatusDeferred, 2)
	tracker.AddChildEpic("mw-root", "mw-root.1", "Held middle")
	tracker.DescribeEpic("mw-root.1", "Held middle", apptest.StatusDeferred, 2)
	mustDo(t, tracker.SetStatus("mw-root.1", apptest.StatusDeferred))
	tracker.AddChildEpic("mw-root.1", "mw-root.1.1", "Live leaf epic")
	tracker.DescribeEpic("mw-root.1.1", "Live leaf epic", apptest.StatusOpen, 2)
	tracker.AddStory("mw-root.1.1", domain.Story{ID: "mw-root.1.1.1", Title: "Work"})

	doc := viewDoc(t, tracker)

	for _, id := range []string{"mw-root", "mw-root.1", "mw-root.1.1", "mw-root.1.1.1"} {
		if !viewHas(doc, id) {
			t.Errorf("expected %s in the view, got %v", id, viewIDs(doc))
		}
	}
	if root := viewBead(t, doc, "mw-root"); root.Parent != "" || root.Status != "deferred" {
		t.Errorf("expected the held root with no parent, got %+v", root)
	}
	if middle := viewBead(t, doc, "mw-root.1"); middle.Parent != "mw-root" {
		t.Errorf("expected the middle epic under the root, got %+v", middle)
	}
}

// waits names the unfinished beads a bead waits on, a blocker in another
// tree included, and never a finished one.
func TestPosternViewWaitsNameOnlyUnfinishedBlockers(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	liveEpic(tracker, "mw-a", domain.Path{})
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.1", Title: "Done"})
	closedAt(t, tracker, "mw-a.1", viewNow.Add(-time.Hour))
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.2", Title: "Open"})
	tracker.AddEpic("mw-held", domain.Path{})
	tracker.DescribeEpic("mw-held", "Held elsewhere", apptest.StatusDeferred, 2)
	tracker.AddStory("mw-held", domain.Story{ID: "mw-held.1", Title: "Elsewhere, open"})
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.3", Title: "Waits"})
	tracker.Needs("mw-a.3", "mw-a.1", "mw-a.2", "mw-held.1")

	doc := viewDoc(t, tracker)

	if got := viewBead(t, doc, "mw-a.3").Waits; !equalStrings(got, []string{"mw-a.2", "mw-held.1"}) {
		t.Fatalf("expected mw-a.3 to wait on mw-a.2 and mw-held.1, got %v", got)
	}
	if got := viewBead(t, doc, "mw-a.2").Waits; got == nil || len(got) != 0 {
		t.Fatalf("expected an empty, non-null waits for mw-a.2, got %#v", got)
	}
	if viewHas(doc, "mw-held.1") {
		t.Fatalf("expected a blocker in another, held tree not to be drawn in the view, got %v", viewIDs(doc))
	}
}

// Every kind of need in §11's table, each with its options.
func TestPosternViewListsEveryKindOfNeed(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	liveEpic(tracker, "mw-a", domain.Path{Host: "desktop"})

	// question: asked by postern, its note carrying what was asked.
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.1", Title: "Which storage engine?"})
	note, _ := json.Marshal(map[string]any{"txid": "q-txid", "options": []string{"A", "B"},
		"asked": "2026-09-28T10:00:00Z", "q": "Which storage engine?", "rec": "A"})
	mustDo(t, tracker.SetNote(ctx, application.PosternQuestionKey("mw-a.1"), string(note)))

	// approve: two held stories.
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.2", Title: "Held one"})
	mustDo(t, tracker.SetStatus("mw-a.2", apptest.StatusDeferred))
	mustDo(t, tracker.SetCreated("mw-a.2", viewNow.Add(-5*time.Hour)))
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.3", Title: "Held two"})
	mustDo(t, tracker.SetStatus("mw-a.3", apptest.StatusDeferred))

	// verify: landed an hour ago, not verified; another verified; another a
	// day and a half ago.
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.4", Title: "Landed"})
	closedAt(t, tracker, "mw-a.4", viewNow.Add(-time.Hour))
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.5", Title: "Landed and verified"})
	closedAt(t, tracker, "mw-a.5", viewNow.Add(-time.Hour))
	mustDo(t, tracker.CommentOnStory(ctx, "mw-a.5", "VERIFIED by the Governor via postern (x)"))
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.6", Title: "Landed a while ago"})
	closedAt(t, tracker, "mw-a.6", viewNow.Add(-36*time.Hour))

	// demo and hands.
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.7", Title: "Show it"})
	mustDo(t, tracker.SetLabels("mw-a.7", "demo"))
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.8", Title: "Plug the cable in"})
	mustDo(t, tracker.SetLabels("mw-a.8", "hitl"))
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.9", Title: "Hands, but done"})
	mustDo(t, tracker.SetLabels("mw-a.9", "hitl"))
	closedAt(t, tracker, "mw-a.9", viewNow.Add(-40*time.Hour))

	// alarm: a story out of attempts.
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.10", Title: "Out of attempts"})
	mustDo(t, tracker.SetStoryMetadata(ctx, "mw-a.10", map[string]string{
		application.AttemptsField: "3", application.AttemptsExhaustedField: "3"}))

	doc := viewDoc(t, tracker)

	q := viewNeed(t, doc, "question", "mw-a.1")
	if q.Epic != "mw-a" || q.Title != "Which storage engine?" || q.Text != "Which storage engine?" ||
		q.Recommended != "A" || !equalStrings(q.Options, []string{"A", "B"}) || q.Since != "2026-09-28T10:00:00Z" {
		t.Errorf("expected the question as its note recorded it, got %+v", q)
	}
	if tracker.CommentReads("mw-a.1") != 0 {
		t.Errorf("expected a question whose note says what was asked never to have its comments read, got %d", tracker.CommentReads("mw-a.1"))
	}
	approve := viewNeed(t, doc, "approve", "mw-a")
	if approve.Epic != "mw-a" || !equalStrings(approve.Options, []string{"Release"}) || !strings.Contains(approve.Text, "2") ||
		approve.Since != "2026-09-28T07:00:00Z" || approve.Blocks != 2 {
		t.Errorf("expected an approve need for 2 held stories, blocking both, since the oldest was filed, got %+v", approve)
	}
	verify := viewNeed(t, doc, "verify", "mw-a.4")
	if !equalStrings(verify.Options, []string{"Verified"}) || verify.Since != "2026-09-28T11:00:00Z" {
		t.Errorf("expected a verify need since mw-a.4 closed, got %+v", verify)
	}
	viewLacksNeed(t, doc, "verify", "mw-a.5")
	viewLacksNeed(t, doc, "verify", "mw-a.6")
	if demo := viewNeed(t, doc, "demo", "mw-a.7"); demo.Options == nil || len(demo.Options) != 0 {
		t.Errorf("expected a demo need with an empty options array, got %#v", demo.Options)
	}
	viewNeed(t, doc, "hands", "mw-a.8")
	viewLacksNeed(t, doc, "hands", "mw-a.9")
	alarm := viewNeed(t, doc, "alarm", "mw-a.10")
	if !strings.Contains(alarm.Text, "3") {
		t.Errorf("expected the alarm to say how many attempts were used, got %+v", alarm)
	}
}

// A question whose note is a bare txid, written before the note said what
// was asked, is read back from its QUESTION comment — one read, shared with
// the verify candidates.
func TestPosternViewReadsAnOlderQuestionFromItsComment(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	liveEpic(tracker, "mw-a", domain.Path{})
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.1", Title: "Ship now?"})
	mustDo(t, tracker.CommentOnStory(ctx, "mw-a.1",
		"QUESTION 2026-09-27T09:30:00Z asked by postern, txid t1: Ship now or wait? (recommended ship; options ship, wait)"))
	mustDo(t, tracker.SetNote(ctx, application.PosternQuestionKey("mw-a.1"), "t1"))
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.2", Title: "Landed"})
	closedAt(t, tracker, "mw-a.2", viewNow.Add(-time.Hour))
	mustDo(t, tracker.CommentOnStory(ctx, "mw-a.2", "landed fine"))

	doc := viewDoc(t, tracker)

	q := viewNeed(t, doc, "question", "mw-a.1")
	if q.Text != "Ship now or wait?" || q.Recommended != "ship" || !equalStrings(q.Options, []string{"ship", "wait"}) || q.Since != "2026-09-27T09:30:00Z" {
		t.Fatalf("expected the question read from its comment, got %+v", q)
	}
	viewNeed(t, doc, "verify", "mw-a.2")
	if calls := tracker.StoriesCommentsCalls(); calls != 1 {
		t.Fatalf("expected one batched comment read for the question and the landing together, got %d", calls)
	}
}

// A host whose last sync is over 20 minutes old, with work pathed to it, is
// an alarm with no bead; every host with a last sync is listed.
func TestPosternViewAlarmsForAHostThatHasNotSynced(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	liveEpic(tracker, "mw-a", domain.Path{Rig: "millwright", Branch: "main"})
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.1", Title: "On the laptop", Overrides: domain.Path{Host: "laptop"}})
	mustDo(t, tracker.SetNote(ctx, application.LastSyncKey("laptop"), viewNow.Add(-34*time.Minute).Format(time.RFC3339)))
	mustDo(t, tracker.SetNote(ctx, application.LastSyncKey("vps"), viewNow.Add(-2*time.Hour).Format(time.RFC3339)))
	mustDo(t, tracker.SetNote(ctx, application.LastSyncKey("desktop"), viewNow.Add(-time.Minute).Format(time.RFC3339)))

	doc := viewDoc(t, tracker)

	var hosts []string
	for _, h := range doc.Hosts {
		hosts = append(hosts, h.Name+"@"+h.LastSync)
	}
	want := []string{"desktop@2026-09-28T11:59:00Z", "laptop@2026-09-28T11:26:00Z", "vps@2026-09-28T10:00:00Z"}
	if !equalStrings(hosts, want) {
		t.Fatalf("expected hosts %v, got %v", want, hosts)
	}
	alarm := viewNeed(t, doc, "alarm", "")
	if alarm.Epic != "" || !strings.Contains(alarm.Title, "laptop") || alarm.Since != "2026-09-28T11:26:00Z" {
		t.Fatalf("expected an alarm for the laptop, got %+v", alarm)
	}
	alarms := 0
	for _, n := range doc.Needs {
		if n.Kind == "alarm" {
			alarms++
		}
	}
	if alarms != 1 {
		t.Fatalf("expected one alarm (the vps has no work pathed to it), got %v", viewNeeds(doc))
	}
}

// needs are ordered most blocking first, then oldest; blocks counts every
// unfinished bead that waits on the need's bead, directly or through others.
func TestPosternViewOrdersNeedsByWhatTheyBlockThenAge(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	liveEpic(tracker, "mw-a", domain.Path{})
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.1", Title: "Old demo, blocks nothing"})
	mustDo(t, tracker.SetLabels("mw-a.1", "demo"))
	mustDo(t, tracker.SetCreated("mw-a.1", viewNow.Add(-48*time.Hour)))
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.2", Title: "Young hands, blocks two"})
	mustDo(t, tracker.SetLabels("mw-a.2", "hitl"))
	mustDo(t, tracker.SetCreated("mw-a.2", viewNow.Add(-time.Hour)))
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.3", Title: "Waits on .2"})
	tracker.Needs("mw-a.3", "mw-a.2")
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.4", Title: "Waits on .3"})
	tracker.Needs("mw-a.4", "mw-a.3")
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.5", Title: "Newer demo, blocks nothing"})
	mustDo(t, tracker.SetLabels("mw-a.5", "demo"))
	mustDo(t, tracker.SetCreated("mw-a.5", viewNow.Add(-24*time.Hour)))
	_ = ctx

	doc := viewDoc(t, tracker)

	if got, want := viewNeeds(doc), []string{"hands:mw-a.2", "demo:mw-a.1", "demo:mw-a.5"}; !equalStrings(got, want) {
		t.Fatalf("expected needs %v, got %v", want, got)
	}
	if blocks := viewNeed(t, doc, "hands", "mw-a.2").Blocks; blocks != 2 {
		t.Fatalf("expected mw-a.2 to block 2 beads, got %d", blocks)
	}
}

// The view costs one tracker call per live epic for its children, plus a
// handful in all: the live epics, their own fields, every note at once, and
// comments only for a question or landing that needs them — never for the
// rest. A landing's verdict is remembered between runs, so a second run reads
// no comments at all.
func TestPosternViewKeepsItsTrackerCallsFew(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	for _, e := range []string{"mw-a", "mw-b", "mw-c"} {
		liveEpic(tracker, e, domain.Path{})
		for i := 1; i <= 4; i++ {
			id := fmt.Sprintf("%s.%d", e, i)
			tracker.AddStory(e, domain.Story{ID: id, Title: id})
			mustDo(t, tracker.CommentOnStory(ctx, id, "chatter"))
		}
	}
	closedAt(t, tracker, "mw-b.2", viewNow.Add(-2*time.Hour))

	doc := viewDoc(t, tracker)
	viewNeed(t, doc, "verify", "mw-b.2")

	if calls := tracker.ShowEpicsCalls(); calls != 1 {
		t.Errorf("expected every live epic read in one ShowEpics, got %d calls", calls)
	}
	if calls := tracker.StoriesCommentsCalls(); calls != 1 {
		t.Errorf("expected one batched comment read, got %d", calls)
	}
	for _, id := range []string{"mw-a.1", "mw-b.1", "mw-c.4"} {
		if reads := tracker.CommentReads(id); reads != 0 {
			t.Errorf("expected %s's comments never read, got %d", id, reads)
		}
	}
	for _, asked := range tracker.Asked() {
		if asked == "ShowBeads" {
			t.Errorf("expected no extra bead reads when every parent and blocker is in the tree, got %v", tracker.Asked())
		}
	}

	viewDoc(t, tracker)
	if reads := tracker.CommentReads("mw-b.2"); reads != 1 {
		t.Errorf("expected the landing's verdict remembered, its comments read once in two runs, got %d", reads)
	}
}

// Every list in the view is an array, never null, so a strict reader never
// meets a null where §11 promises an array.
func TestPosternViewWritesEmptyListsAsArrays(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	liveEpic(tracker, "mw-a", domain.Path{})
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.1", Title: "Bare"})

	raw, err := json.Marshal(viewDoc(t, tracker))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "null") {
		t.Fatalf("expected no null in the view, got %s", raw)
	}
	empty, err := json.Marshal(viewDoc(t, apptest.NewFakeTracker()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(empty), `"needs":[]`) || !strings.Contains(string(empty), `"beads":[]`) || !strings.Contains(string(empty), `"hosts":[{"name":"desktop","last_sync":""}]`) {
		t.Fatalf("expected empty needs and beads arrays and this host listed, got %s", empty)
	}
}

// Run seals the view as §11 says — gzip of the JSON, BRC-78 to the
// Governor's key, base64 — and writes it through File; the plaintext opens
// back to the same view.
func TestPosternViewRunSealsTheViewAndWritesIt(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	liveEpic(tracker, "mw-a", domain.Path{})
	cipher := apptest.NewFakeCipher()
	cipher.From = "mayor-pubkey-hex"
	file := apptest.NewFakeSnapshotFile("/state/postern/view.b64")
	var out strings.Builder

	doc, err := application.PosternView{
		Tracker: tracker, Notes: tracker, Cipher: cipher, File: file,
		GovernorKey: "governor-pubkey-hex", Host: "desktop",
		Now: func() time.Time { return viewNow }, Out: &out,
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("running the view: %v", err)
	}
	var opened application.PosternViewDoc
	if err := application.OpenPosternDoc(cipher, "any-private-key", string(file.Written()), &opened); err != nil {
		t.Fatalf("opening what was written: %v", err)
	}
	if opened.V != 2 || len(opened.Beads) != len(doc.Beads) || opened.Beads[0].ID != "mw-a" {
		t.Fatalf("expected the written view to open to the built one, got %+v", opened)
	}
	text, _, err := cipher.Decrypt("any-private-key", string(file.Written()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(text, "\x1f\x8b") {
		t.Fatalf("expected the plaintext to be gzip, starting 1f 8b, got %q", text[:2])
	}
	if !strings.Contains(out.String(), "/state/postern/view.b64") {
		t.Fatalf("expected Run to say where it wrote, got %q", out.String())
	}
}

// Run checks everything writing needs before it reads the tracker.
func TestPosternViewRunRefusesWithoutAGovernorKeyBeforeReadingTheTracker(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.Err = fmt.Errorf("the tracker was read before the governor key was checked")

	_, err := application.PosternView{
		Tracker: tracker, Notes: tracker, Cipher: apptest.NewFakeCipher(),
		File: apptest.NewFakeSnapshotFile("/tmp/view.b64"), Host: "desktop",
	}.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "postern_governor_key") {
		t.Fatalf("expected a refusal naming postern_governor_key, got %v", err)
	}
}

// The view and the snapshot keep one landed memory: a landing's comments the
// view read are not read again by the snapshot that follows it, and the
// snapshot still shows them.
func TestPosternViewAndSnapshotShareTheLandedMemory(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	liveEpic(tracker, "mw-a", domain.Path{})
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.1", Title: "Landed"})
	closedAt(t, tracker, "mw-a.1", viewNow.Add(-time.Hour))
	mustDo(t, tracker.CommentOnStory(ctx, "mw-a.1", "landed on main"))

	viewDoc(t, tracker)
	snapshot, err := application.PosternSnapshot{
		Tracker: tracker, Notes: tracker, Now: func() time.Time { return viewNow },
	}.Build(ctx)
	if err != nil {
		t.Fatalf("building the snapshot: %v", err)
	}
	if reads := tracker.CommentReads("mw-a.1"); reads != 1 {
		t.Fatalf("expected the landing's comments read once between the two, got %d", reads)
	}
	landed := epicOf(t, snapshot, "mw-a").Landed
	if len(landed) != 1 || len(landed[0].Comments) != 1 || landed[0].Comments[0].Text != "landed on main" {
		t.Fatalf("expected the snapshot to show the remembered comment, got %+v", landed)
	}
}

// A hands need carries its bead's steps (§17): each with the sha256 his
// approval binds and, once it has run, how — read from the notes the view
// already reads in one go, at no further tracker call.
func TestPosternViewHandsNeedCarriesItsStepsAndHowTheyRan(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	liveEpic(tracker, "mw-f758y", domain.Path{})
	tracker.AddStory("mw-f758y", domain.Story{ID: "mw-f758y.8", Title: "Enable lingering"})
	mustDo(t, tracker.SetLabels("mw-f758y.8", "hitl"))
	linger := domain.HandsStep{ID: "linger", Host: "desktop", As: "root", Run: "loginctl enable-linger jwhite", WayBack: "loginctl disable-linger jwhite"}
	restart := domain.HandsStep{ID: "restart", Host: "vps", As: "user", Run: "systemctl --user restart mw-dispatch"}
	steps, _ := json.Marshal([]application.HandsStepRecord{{HandsStep: linger, AddedAt: "x"}, {HandsStep: restart, AddedAt: "y"}})
	mustDo(t, tracker.SetNote(ctx, application.HandsStepsKey("mw-f758y.8"), string(steps)))
	mustDo(t, tracker.SetNote(ctx, application.HandsRanKey("mw-f758y.8", "linger"), `{"at":"2026-09-28T12:03:00Z","exit":0,"host":"desktop"}`))

	doc := viewDoc(t, tracker)

	hands := viewNeed(t, doc, "hands", "mw-f758y.8")
	if len(hands.Steps) != 2 {
		t.Fatalf("expected two steps on the hands need, got %+v", hands.Steps)
	}
	first, second := hands.Steps[0], hands.Steps[1]
	if first.ID != "linger" || first.Host != "desktop" || first.As != "root" || first.Run != linger.Run || first.WayBack != linger.WayBack ||
		first.SHA256 != domain.HandsSHA256("mw-f758y.8", linger) {
		t.Fatalf("expected the linger step with its hash, got %+v", first)
	}
	if first.Ran == nil || *first.Ran != (application.HandsRan{At: "2026-09-28T12:03:00Z", Exit: 0, Host: "desktop"}) {
		t.Fatalf("expected linger's run, got %+v", first.Ran)
	}
	if second.Ran != nil || second.SHA256 != domain.HandsSHA256("mw-f758y.8", restart) {
		t.Fatalf("expected restart with its hash and no run, got %+v", second)
	}
	raw, _ := json.Marshal(hands)
	if !strings.Contains(string(raw), `"steps":[{"id":"linger","host":"desktop","as":"root","run":"loginctl enable-linger jwhite","way_back":"loginctl disable-linger jwhite","sha256":"`) ||
		strings.Count(string(raw), `"ran":`) != 1 {
		t.Fatalf("expected §17's step shape, ran only once run, got %s", raw)
	}
	for _, n := range doc.Needs {
		if n.Kind != "hands" && n.Steps != nil {
			t.Fatalf("expected steps only on a hands need, got %+v", n)
		}
	}
}

// A hitl bead whose every hands step has run with exit 0 no longer waits on
// his hands, only on the Mayor's acceptance check: it leaves the hands needs.
// A step that failed or has not run keeps it; a hitl bead with no steps at
// all keeps it as before.
func TestPosternViewHandsNeedLeavesOnceEveryStepRanWithExitZero(t *testing.T) {
	ranZero := `{"at":"2026-09-28T12:03:00Z","exit":0,"host":"desktop"}`
	ranOne := `{"at":"2026-09-28T12:03:00Z","exit":1,"host":"desktop"}`
	cases := []struct {
		name  string
		steps []string
		ran   map[string]string
		want  bool
	}{
		{"one step ran with exit 0", []string{"a"}, map[string]string{"a": ranZero}, false},
		{"two steps both ran with exit 0", []string{"a", "b"}, map[string]string{"a": ranZero, "b": ranZero}, false},
		{"one step ran with exit 1", []string{"a"}, map[string]string{"a": ranOne}, true},
		{"one step has not run", []string{"a"}, nil, true},
		{"one of two has not run", []string{"a", "b"}, map[string]string{"a": ranZero}, true},
		{"one of two failed", []string{"a", "b"}, map[string]string{"a": ranZero, "b": ranOne}, true},
		{"no steps at all", nil, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			tracker := apptest.NewFakeTracker()
			liveEpic(tracker, "mw-f758y", domain.Path{})
			tracker.AddStory("mw-f758y", domain.Story{ID: "mw-f758y.8", Title: "Hands"})
			mustDo(t, tracker.SetLabels("mw-f758y.8", "hitl"))
			var records []application.HandsStepRecord
			for _, id := range tc.steps {
				records = append(records, application.HandsStepRecord{HandsStep: domain.HandsStep{ID: id, Host: "desktop", As: "user", Run: "true"}})
			}
			if records != nil {
				raw, _ := json.Marshal(records)
				mustDo(t, tracker.SetNote(ctx, application.HandsStepsKey("mw-f758y.8"), string(raw)))
			}
			for id, ran := range tc.ran {
				mustDo(t, tracker.SetNote(ctx, application.HandsRanKey("mw-f758y.8", id), ran))
			}

			doc := viewDoc(t, tracker)

			var got bool
			for _, n := range doc.Needs {
				if n.Kind == "hands" && n.Bead == "mw-f758y.8" {
					got = true
				}
			}
			if got != tc.want {
				t.Fatalf("expected a hands need: %v, got %v (needs %+v)", tc.want, got, doc.Needs)
			}
		})
	}
}

// A hands or demo need that is not ready says what it waits on and is marked
// not ready, so the app offers neither Approve nor Done; a ready one is as it
// always was.
func TestPosternViewNeedNotReadyNamesWhatItWaitsOn(t *testing.T) {
	ctx := context.Background()
	oneStep := func(t *testing.T, tracker *apptest.FakeTracker, bead string) {
		raw, _ := json.Marshal([]application.HandsStepRecord{{HandsStep: domain.HandsStep{ID: "a", Host: "desktop", As: "user", Run: "true"}}})
		mustDo(t, tracker.SetNote(ctx, application.HandsStepsKey(bead), string(raw)))
	}
	build := func(t *testing.T, label string, blocked, closeBlocker, steps bool) application.PosternViewNeed {
		tracker := apptest.NewFakeTracker()
		liveEpic(tracker, "mw-a", domain.Path{})
		tracker.AddStory("mw-a", domain.Story{ID: "mw-a.1", Title: "Wire the relay"})
		tracker.AddStory("mw-a", domain.Story{ID: "mw-a.2", Title: "Flash the board"})
		tracker.AddStory("mw-a", domain.Story{ID: "mw-a.3", Title: "The card"})
		mustDo(t, tracker.SetLabels("mw-a.3", label))
		if blocked {
			tracker.Needs("mw-a.3", "mw-a.1", "mw-a.2")
			if closeBlocker {
				closedAt(t, tracker, "mw-a.1", viewNow.Add(-time.Hour))
				closedAt(t, tracker, "mw-a.2", viewNow.Add(-time.Hour))
			}
		}
		if steps {
			oneStep(t, tracker, "mw-a.3")
		}
		kind := "hands"
		if label == "demo" {
			kind = "demo"
		}
		return viewNeed(t, viewDoc(t, tracker), kind, "mw-a.3")
	}

	t.Run("hands with no steps waits on the Mayor to write them", func(t *testing.T) {
		n := build(t, "hitl", false, false, false)
		if !n.NotReady || !equalStrings(n.WaitingOn, []string{"the Mayor to write the steps"}) ||
			n.Text != "Not ready yet: waiting on the Mayor to write the steps" {
			t.Fatalf("expected a not-ready need waiting on the Mayor, got %+v", n)
		}
		if len(n.Options) != 0 {
			t.Fatalf("expected no action on a not-ready need, got %v", n.Options)
		}
	})
	t.Run("hands with steps but an open blocker names the blocker's title", func(t *testing.T) {
		n := build(t, "hitl", true, false, true)
		if !n.NotReady || !equalStrings(n.WaitingOn, []string{"Wire the relay", "Flash the board"}) ||
			n.Text != "Not ready yet: waiting on Wire the relay, Flash the board" {
			t.Fatalf("expected a not-ready need naming both blockers, got %+v", n)
		}
	})
	t.Run("hands with no steps and an open blocker names the blockers and the Mayor", func(t *testing.T) {
		n := build(t, "hitl", true, false, false)
		if !n.NotReady || !equalStrings(n.WaitingOn, []string{"Wire the relay", "Flash the board", "the Mayor to write the steps"}) {
			t.Fatalf("expected the blockers then the Mayor, got %+v", n)
		}
	})
	t.Run("demo with an open blocker is not ready", func(t *testing.T) {
		n := build(t, "demo", true, false, false)
		if !n.NotReady || !equalStrings(n.WaitingOn, []string{"Wire the relay", "Flash the board"}) ||
			n.Text != "Not ready yet: waiting on Wire the relay, Flash the board" || len(n.Options) != 0 {
			t.Fatalf("expected a not-ready demo naming both blockers, got %+v", n)
		}
	})
	t.Run("demo with no blocker is ready as today", func(t *testing.T) {
		n := build(t, "demo", false, false, false)
		if n.NotReady || n.WaitingOn != nil {
			t.Fatalf("expected a ready demo, got %+v", n)
		}
	})
	t.Run("hands with the blockers closed and a step is ready as today", func(t *testing.T) {
		n := build(t, "hitl", true, true, true)
		if n.NotReady || n.WaitingOn != nil || len(n.Steps) != 1 {
			t.Fatalf("expected a ready hands need with its step, got %+v", n)
		}
		raw, _ := json.Marshal(n)
		if strings.Contains(string(raw), "not_ready") || strings.Contains(string(raw), "waiting_on") {
			t.Fatalf("expected a ready need to carry neither field, got %s", raw)
		}
	})
}

// A stale need's since is when it went stale, so it sorts by that; and only a
// need past its window reads its comments.
func TestPosternViewStaleNeedSortsBySinceItWentStaleAndReadsItsComments(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	liveEpic(tracker, "mw-a", domain.Path{})
	at := viewNow.Add(-4 * 24 * time.Hour)
	for _, id := range []string{"mw-a.1", "mw-a.2"} {
		tracker.AddStory("mw-a", domain.Story{ID: id, Title: "Story " + id})
		mustDo(t, tracker.SetCreated(id, at))
		mustDo(t, tracker.CommentOnStory(context.Background(), id, "a word"))
	}
	mustDo(t, tracker.SetLabels("mw-a.1", "hitl"))
	mustDo(t, tracker.SetLabels("mw-a.2", "demo"))
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.3", Title: "Young hands"})
	mustDo(t, tracker.SetLabels("mw-a.3", "hitl"))
	mustDo(t, tracker.SetCreated("mw-a.3", viewNow.Add(-time.Hour)))
	mustDo(t, tracker.CommentOnStory(context.Background(), "mw-a.3", "a word"))

	doc := viewDoc(t, tracker)

	// The stale need's since is created + 3 days, a day after the demo's.
	if got, want := viewNeeds(doc), []string{"demo:mw-a.2", "stale:mw-a.1", "hands:mw-a.3"}; !equalStrings(got, want) {
		t.Fatalf("expected needs %v, got %v", want, got)
	}
	if reads := tracker.CommentReads("mw-a.3"); reads != 0 {
		t.Fatalf("expected no comment read for a hands need inside its window, got %d", reads)
	}
	if reads := tracker.CommentReads("mw-a.1"); reads != 1 {
		t.Fatalf("expected one comment read for the stale need, got %d", reads)
	}
}

// A step's why rides to the app on steps[].ran.why, only when it has one.
func TestPosternViewHandsStepCarriesWhyItFailed(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	liveEpic(tracker, "mw-f758y", domain.Path{})
	tracker.AddStory("mw-f758y", domain.Story{ID: "mw-f758y.8", Title: "Reload nginx"})
	mustDo(t, tracker.SetLabels("mw-f758y.8", "hitl"))
	steps, _ := json.Marshal([]application.HandsStepRecord{
		{HandsStep: domain.HandsStep{ID: "dry", Host: "vps", As: "user", Run: "nginx -t"}},
		{HandsStep: domain.HandsStep{ID: "ok", Host: "vps", As: "user", Run: "true"}},
	})
	mustDo(t, tracker.SetNote(ctx, application.HandsStepsKey("mw-f758y.8"), string(steps)))
	mustDo(t, tracker.SetNote(ctx, application.HandsRanKey("mw-f758y.8", "dry"), `{"at":"2026-09-29T18:52:00Z","exit":-1,"host":"vps","why":"the step could not be started: root"}`))
	mustDo(t, tracker.SetNote(ctx, application.HandsRanKey("mw-f758y.8", "ok"), `{"at":"2026-09-29T18:52:00Z","exit":0,"host":"vps"}`))

	hands := viewNeed(t, viewDoc(t, tracker), "hands", "mw-f758y.8")

	raw, _ := json.Marshal(hands.Steps)
	if !strings.Contains(string(raw), `"ran":{"at":"2026-09-29T18:52:00Z","exit":-1,"host":"vps","why":"the step could not be started: root"}`) ||
		strings.Count(string(raw), `"why"`) != 1 {
		t.Fatalf("expected why on the failed step only, got %s", raw)
	}
}
