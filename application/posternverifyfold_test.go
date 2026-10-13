package application_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

const day = 24 * time.Hour

// landedUnder files a landing under epic that closed days days ago on the
// view's clock, with the closing comment's HOW TO CHECK IT.
func landedUnder(t *testing.T, tracker *apptest.FakeTracker, epic, id string, days float64) {
	t.Helper()
	tracker.AddStory(epic, domain.Story{ID: id, Title: "Landed " + id})
	closedAt(t, tracker, id, viewNow.Add(-time.Duration(days*float64(day))))
	mustDo(t, tracker.CommentOnStory(context.Background(), id, "HOW TO CHECK IT: 1. Open the app."))
}

// foldedView builds the view of tracker with the event log and the days a
// verify need stays unfolded (0 for the default).
func foldedView(t *testing.T, tracker *apptest.FakeTracker, log application.EventLog, verifyDays int) application.PosternViewDoc {
	t.Helper()
	doc, err := application.PosternView{
		Tracker: tracker, Notes: tracker, Host: "desktop", Events: log, VerifyDays: verifyDays,
		Now: func() time.Time { return viewNow },
	}.Build(context.Background())
	if err != nil {
		t.Fatalf("building the view: %v", err)
	}
	return doc
}

func jsonOf(doc application.PosternViewDoc) (string, error) {
	plain, err := json.Marshal(doc)
	return string(plain), err
}

func countOf(s, sub string) int { return strings.Count(s, sub) }

func verifyNeedOf(doc application.PosternViewDoc, bead string) (application.PosternViewNeed, bool) {
	for _, n := range doc.Needs {
		if n.Kind == application.PosternNeedVerify && n.Bead == bead {
			return n, true
		}
	}
	return application.PosternViewNeed{}, false
}

// A landing's verify need is as today for verify_days (default 4) after it
// closed, then folded until it is verified or 30 days have passed.
func TestAVerifyNeedFoldsAfterFourDaysAndDropsAfterThirty(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	liveEpic(tracker, "mw-a", domain.Path{})
	for id, days := range map[string]float64{"mw-a.1": 1, "mw-a.2": 5, "mw-a.3": 10, "mw-a.4": 29, "mw-a.5": 31} {
		landedUnder(t, tracker, "mw-a", id, days)
	}

	doc := foldedView(t, tracker, nil, 0)

	for id, folded := range map[string]bool{"mw-a.1": false, "mw-a.2": true, "mw-a.3": true, "mw-a.4": true} {
		need, ok := verifyNeedOf(doc, id)
		if !ok {
			t.Fatalf("expected a verify need on %s", id)
		}
		if need.Folded != folded {
			t.Errorf("%s: folded is %v, want %v", id, need.Folded, folded)
		}
	}
	if _, ok := verifyNeedOf(doc, "mw-a.5"); ok {
		t.Errorf("expected no verify need on a landing closed 31 days ago")
	}
}

func TestAVerifiedLandingHasNoVerifyNeedAtAnyAge(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	liveEpic(tracker, "mw-a", domain.Path{})
	for id, days := range map[string]float64{"mw-a.1": 1, "mw-a.2": 5, "mw-a.3": 20} {
		landedUnder(t, tracker, "mw-a", id, days)
		mustDo(t, tracker.CommentOnStory(context.Background(), id, "VERIFIED by the Mayor."))
	}

	doc := foldedView(t, tracker, nil, 0)

	for _, id := range []string{"mw-a.1", "mw-a.2", "mw-a.3"} {
		if _, ok := verifyNeedOf(doc, id); ok {
			t.Errorf("expected no verify need on the verified %s", id)
		}
	}
}

func TestFoldedIsLeftOutOfAnUnfoldedNeedsJSON(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	liveEpic(tracker, "mw-a", domain.Path{})
	landedUnder(t, tracker, "mw-a", "mw-a.1", 1)
	landedUnder(t, tracker, "mw-a", "mw-a.2", 5)

	doc := foldedView(t, tracker, nil, 0)

	plain, err := jsonOf(doc)
	if err != nil {
		t.Fatal(err)
	}
	if got := countOf(plain, `"folded":true`); got != 1 {
		t.Errorf("expected exactly one folded:true in the view's JSON, got %d in %s", got, plain)
	}
	if got := countOf(plain, `"folded"`); got != 1 {
		t.Errorf("expected folded left out of the unfolded need, got %d in %s", got, plain)
	}
}

func TestVerifyDaysInConfigMovesTheFold(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	liveEpic(tracker, "mw-a", domain.Path{})
	landedUnder(t, tracker, "mw-a", "mw-a.1", 1.5)
	landedUnder(t, tracker, "mw-a", "mw-a.2", 2)
	landedUnder(t, tracker, "mw-a", "mw-a.3", 3)

	doc := foldedView(t, tracker, nil, 2)

	for id, folded := range map[string]bool{"mw-a.1": false, "mw-a.2": true, "mw-a.3": true} {
		need, ok := verifyNeedOf(doc, id)
		if !ok || need.Folded != folded {
			t.Errorf("%s: need %+v (present %v), want folded %v", id, need, ok, folded)
		}
	}
}

// The first view that folds a need tells the Mayor, once.
func TestTheFirstViewThatFoldsANeedSendsTheMayorOneEventPerBead(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	liveEpic(tracker, "mw-a", domain.Path{})
	landedUnder(t, tracker, "mw-a", "mw-a.1", 1)
	landedUnder(t, tracker, "mw-a", "mw-a.2", 5)
	landedUnder(t, tracker, "mw-a", "mw-a.3", 6)
	log := &apptest.FakeEventLog{}

	foldedView(t, tracker, log, 0)

	got := log.All()
	if len(got) != 2 {
		t.Fatalf("expected two verify_folded events, got %+v", got)
	}
	beads := map[string]bool{}
	for _, ev := range got {
		if ev.Kind != events.KindVerifyFolded || ev.Lane != events.LaneNormal || ev.Actor == "" {
			t.Errorf("expected a normal-lane verify_folded event with an actor, got %+v", ev)
		}
		beads[ev.Bead] = true
	}
	if !beads["mw-a.2"] || !beads["mw-a.3"] {
		t.Errorf("expected events naming mw-a.2 and mw-a.3, got %+v", got)
	}

	foldedView(t, tracker, log, 0)
	if again := len(log.All()); again != 2 {
		t.Fatalf("expected the next view to send none, the log holds %d events", again)
	}

	// A landing that folds later is told when it does.
	landedUnder(t, tracker, "mw-a", "mw-a.4", 7)
	foldedView(t, tracker, log, 0)
	if all := log.All(); len(all) != 3 || all[2].Bead != "mw-a.4" {
		t.Fatalf("expected one more event, on mw-a.4, got %+v", all)
	}
}

// A view with no event log has nowhere to tell the Mayor, and so does not
// count him told.
func TestAViewWithNoEventLogDoesNotCountTheMayorTold(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	liveEpic(tracker, "mw-a", domain.Path{})
	landedUnder(t, tracker, "mw-a", "mw-a.1", 5)
	log := &apptest.FakeEventLog{}

	foldedView(t, tracker, nil, 0)
	foldedView(t, tracker, log, 0)

	if got := log.All(); len(got) != 1 || got[0].Bead != "mw-a.1" {
		t.Fatalf("expected the first view with a log to send the event, got %+v", got)
	}
}

func TestAVerifiedFoldedLandingSendsNoEvent(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	liveEpic(tracker, "mw-a", domain.Path{})
	landedUnder(t, tracker, "mw-a", "mw-a.1", 5)
	mustDo(t, tracker.CommentOnStory(context.Background(), "mw-a.1", "VERIFIED by the Governor."))
	log := &apptest.FakeEventLog{}

	foldedView(t, tracker, log, 0)

	if got := log.All(); len(got) != 0 {
		t.Fatalf("expected no event for a verified landing, got %+v", got)
	}
}

// A landing keeps its verify need whatever its age under 30 days, and
// whether or not its epic is still live: the 7-day window of the view's
// beads does not cut it.
func TestALandingOverAWeekOldStillShowsAsAFoldedVerifyNeed(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	liveEpic(tracker, "mw-a", domain.Path{})
	landedUnder(t, tracker, "mw-a", "mw-a.1", 10)

	doc := foldedView(t, tracker, nil, 0)

	need, ok := verifyNeedOf(doc, "mw-a.1")
	if !ok || !need.Folded {
		t.Fatalf("expected a folded verify need on a landing closed 10 days ago, got %+v (present %v)", need, ok)
	}
	if viewHas(doc, "mw-a.1") {
		t.Errorf("expected the landing to stay out of the view's beads past the week")
	}
}

func TestALandingUnderAnEpicThatHasSinceClosedStillShowsAsAFoldedVerifyNeed(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	liveEpic(tracker, "mw-a", domain.Path{})
	landedUnder(t, tracker, "mw-a", "mw-a.1", 5)
	landedUnder(t, tracker, "mw-a", "mw-a.2", 1)
	landedUnder(t, tracker, "mw-a", "mw-a.3", 12)
	landedUnder(t, tracker, "mw-a", "mw-a.4", 40)
	tracker.DescribeEpic("mw-a", "Epic mw-a", apptest.StatusClosed, 1)

	doc := foldedView(t, tracker, nil, 0)

	for id, folded := range map[string]bool{"mw-a.1": true, "mw-a.2": false, "mw-a.3": true} {
		need, ok := verifyNeedOf(doc, id)
		if !ok || need.Folded != folded {
			t.Errorf("%s: need %+v (present %v), want folded %v", id, need, ok, folded)
		}
		if ok && need.Epic != "mw-a" {
			t.Errorf("%s: expected the need to name its epic mw-a, got %q", id, need.Epic)
		}
	}
	if _, ok := verifyNeedOf(doc, "mw-a.4"); ok {
		t.Errorf("expected no verify need on a landing closed 40 days ago")
	}
	if viewHas(doc, "mw-a") || viewHas(doc, "mw-a.1") {
		t.Errorf("expected a closed epic and its landings to stay out of the view's beads, got %v", viewIDs(doc))
	}
}

// The landed comment memory keeps a landing for the whole verify window, so
// a snapshot built between two views does not make the second read its
// comments again.
func TestTheLandedMemoryKeepsAFoldedLandingAcrossASnapshot(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	liveEpic(tracker, "mw-a", domain.Path{})
	landedUnder(t, tracker, "mw-a", "mw-a.1", 5)

	foldedView(t, tracker, nil, 0)
	_, err := application.PosternSnapshot{
		Tracker: tracker, Notes: tracker, Now: func() time.Time { return viewNow },
	}.Build(ctx)
	if err != nil {
		t.Fatalf("building the snapshot: %v", err)
	}
	foldedView(t, tracker, nil, 0)

	if reads := tracker.CommentReads("mw-a.1"); reads != 1 {
		t.Fatalf("expected the landing's comments read once across two views and a snapshot, got %d", reads)
	}
}
