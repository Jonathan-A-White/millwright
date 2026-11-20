package application_test

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// A rig's share: one rig on host vps, some stories running, and the stories
// ready, each filed so many days ago. A title starting [bug] is a fix; any other
// is a feature.
type shared struct {
	id       string
	title    string
	priority int
	days     int
}

func aShareFixture(t *testing.T, ready []shared) (*apptest.FakeTracker, *apptest.FakeEpicRules) {
	t.Helper()
	tracker := aQueueOf(t)
	for _, s := range ready {
		addShared(t, tracker, s)
	}
	return tracker, apptest.NewFakeEpicRules()
}

func addShared(t *testing.T, tracker *apptest.FakeTracker, s shared) {
	t.Helper()
	tracker.AddStory("mw-q", domain.Story{ID: s.id, Title: s.title})
	if err := tracker.SetPriority(s.id, s.priority); err != nil {
		t.Fatal(err)
	}
	if s.days > 0 {
		if err := tracker.SetCreated(s.id, queueNow.Add(-time.Duration(s.days)*24*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
}

// runningShared files stories as running on vps: fixes first, then features.
func runningShared(t *testing.T, tracker *apptest.FakeTracker, fixes, features int) {
	t.Helper()
	for i := 0; i < fixes; i++ {
		id := fmt.Sprintf("mw-q.run-fix%d", i)
		addShared(t, tracker, shared{id, "[bug] running fix", 2, 0})
		if err := tracker.ClaimAs(id, "vps", queueNow.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < features; i++ {
		id := fmt.Sprintf("mw-q.run-feat%d", i)
		addShared(t, tracker, shared{id, "A running feature", 2, 0})
		if err := tracker.ClaimAs(id, "vps", queueNow.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
}

func theQueueIDs(t *testing.T, tracker *apptest.FakeTracker, rules application.EpicRules) ([]string, application.QueueReport) {
	t.Helper()
	var out bytes.Buffer
	report, err := application.Queue{Tracker: tracker, Host: "vps", Rig: "millwright", Rules: rules,
		Now: func() time.Time { return queueNow }, Out: &out}.Run(context.Background())
	if err != nil {
		t.Fatalf("the queue: %v", err)
	}
	var ids []string
	for _, row := range report.Ready {
		ids = append(ids, row.ID)
	}
	return ids, report
}

func theDryRunIDs(t *testing.T, tracker *apptest.FakeTracker, rules application.EpicRules) []string {
	t.Helper()
	dispatch, _, _, _, _ := aFactory(t)
	tracker.AddFormula("tdd-feature")
	dispatch.Tracker = tracker
	dispatch.Rules = rules
	dispatch.Cap = 20
	dispatch.DryRun = true
	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("the dry run: %v", err)
	}
	if len(report.Started) == 0 {
		t.Fatalf("expected the dry run to start something, passed %+v, notes %v", report.Passed, report.Notes)
	}
	var ids []string
	for _, s := range report.Started {
		ids = append(ids, s.StoryID)
	}
	return ids
}

// The fixture of the story: five fixes running, an older ready fix and a newer
// ready feature, the rig at 0.6.
func TestShareOrderListsTheFeatureBeforeAnOlderFixWithItsReason(t *testing.T) {
	tracker, rules := aShareFixture(t, []shared{
		{"mw-q.fix", "[bug] an older fix", 2, 9},
		{"mw-q.feat", "A newer feature", 2, 2},
	})
	runningShared(t, tracker, 5, 0)
	rules.Require("millwright", domain.EpicRequirements{FeatureShare: 0.6})

	ids, report := theQueueIDs(t, tracker, rules)
	if want := []string{"mw-q.feat", "mw-q.fix"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("expected the feature first, got %v", ids)
	}
	if why := report.Ready[0].Why; !strings.Contains(why, "feature share: 0 of 5 running, target 60%") {
		t.Fatalf("expected the share in the reason, got %q", why)
	}
	if why := report.Ready[1].Why; strings.Contains(why, "feature share") {
		t.Fatalf("expected the fix to carry no share reason, got %q", why)
	}
	// The dry run takes them in the same order.
	started := theDryRunIDs(t, tracker, rules)
	if want := []string{"mw-q.feat", "mw-q.fix"}; !reflect.DeepEqual(started, want) {
		t.Fatalf("expected the dry run to start %v, got %v", want, started)
	}
}

func TestShareOrderWithNoKeyIsTodaysOrder(t *testing.T) {
	tracker, rules := aShareFixture(t, []shared{
		{"mw-q.fix", "[bug] an older fix", 2, 9},
		{"mw-q.feat", "A newer feature", 2, 2},
	})
	runningShared(t, tracker, 5, 0)
	rules.Require("millwright", domain.EpicRequirements{})

	ids, report := theQueueIDs(t, tracker, rules)
	if want := []string{"mw-q.fix", "mw-q.feat"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("expected the fix first with no key, got %v", ids)
	}
	if strings.Contains(report.Ready[1].Why, "feature share") {
		t.Fatalf("expected no share reason with no key, got %q", report.Ready[1].Why)
	}
	if started := theDryRunIDs(t, tracker, rules); !reflect.DeepEqual(started, ids) {
		t.Fatalf("expected the dry run in the queue's order %v, got %v", ids, started)
	}
	// And no rules at all.
	if ids, _ := theQueueIDs(t, tracker, nil); !reflect.DeepEqual(ids, []string{"mw-q.fix", "mw-q.feat"}) {
		t.Fatalf("expected the fix first with no rules, got %v", ids)
	}
}

func TestShareOrderLeavesTheOrderWhenTheShareIsMet(t *testing.T) {
	tracker, rules := aShareFixture(t, []shared{
		{"mw-q.fix", "[bug] an older fix", 2, 9},
		{"mw-q.feat", "A newer feature", 2, 2},
	})
	// 4 of 5 running are features: 4 is not under 0.6 x 6.
	runningShared(t, tracker, 1, 4)
	rules.Require("millwright", domain.EpicRequirements{FeatureShare: 0.6})

	if ids, _ := theQueueIDs(t, tracker, rules); !reflect.DeepEqual(ids, []string{"mw-q.fix", "mw-q.feat"}) {
		t.Fatalf("expected today's order with the share met, got %v", ids)
	}
}

func TestShareOrderNeverMovesAFeatureBehindAFixAndKeepsP1Where(t *testing.T) {
	tracker, rules := aShareFixture(t, []shared{
		{"mw-q.p1", "[bug] urgent fix", 1, 1},
		{"mw-q.feat-old", "An older feature", 2, 9},
		{"mw-q.fix", "[bug] a fix", 2, 8},
		{"mw-q.feat-new", "A newer feature", 3, 1},
	})
	runningShared(t, tracker, 0, 4)
	rules.Require("millwright", domain.EpicRequirements{FeatureShare: 0.6})

	// The share is met, so the order is today's: P1 first, the older feature
	// ahead of the fix it was ahead of.
	ids, _ := theQueueIDs(t, tracker, rules)
	if want := []string{"mw-q.p1", "mw-q.feat-old", "mw-q.fix", "mw-q.feat-new"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("expected %v, got %v", want, ids)
	}
}

func TestShareOrderPlacesFeaturesAsIfEachWereStarted(t *testing.T) {
	tracker, rules := aShareFixture(t, []shared{
		{"mw-q.p1", "[bug] urgent fix", 1, 1},
		{"mw-q.fix1", "[bug] fix one", 2, 9},
		{"mw-q.fix2", "[bug] fix two", 2, 8},
		{"mw-q.fix3", "[bug] fix three", 2, 7},
		{"mw-q.feat1", "Feature one", 2, 3},
		{"mw-q.feat2", "Feature two", 2, 2},
		{"mw-q.feat3", "Feature three", 3, 1},
	})
	// Nothing running at half: a feature, a fix, a feature, a fix ...
	rules.Require("millwright", domain.EpicRequirements{FeatureShare: 0.5})

	ids, report := theQueueIDs(t, tracker, rules)
	want := []string{"mw-q.p1", "mw-q.feat1", "mw-q.fix1", "mw-q.feat2", "mw-q.fix2", "mw-q.feat3", "mw-q.fix3"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("expected %v, got %v", want, ids)
	}
	if why := report.Ready[0].Why; strings.Contains(why, "feature share") {
		t.Fatalf("expected the P1 story to carry no share reason, got %q", why)
	}
	if why := report.Ready[1].Why; !strings.Contains(why, "feature share: 0 of 0 running, target 50%") {
		t.Fatalf("got %q", why)
	}
}

func TestShareOrderCountsAStoryAFeatureWaitsOnAsFeatureLane(t *testing.T) {
	tracker, rules := aShareFixture(t, []shared{
		{"mw-q.fix", "[bug] an older fix", 2, 9},
		{"mw-q.root", "[bug] the bug a feature waits on", 2, 5},
		{"mw-q.feature", "A feature that waits", 2, 2},
	})
	tracker.Needs("mw-q.feature", "mw-q.root")
	runningShared(t, tracker, 5, 0)
	rules.Require("millwright", domain.EpicRequirements{FeatureShare: 0.6})

	ids, report := theQueueIDs(t, tracker, rules)
	if want := []string{"mw-q.root", "mw-q.fix"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("expected the story the feature waits on first, got %v", ids)
	}
	if !strings.Contains(report.Ready[0].Why, "feature share: 0 of 5 running, target 60%") {
		t.Fatalf("got %q", report.Ready[0].Why)
	}
	// A feature waiting on a fix running now counts the fix as feature lane.
	if err := tracker.ClaimAs("mw-q.root", "vps", queueNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if ids, report := theQueueIDs(t, tracker, rules); !reflect.DeepEqual(ids, []string{"mw-q.fix"}) {
		t.Fatalf("got %v", ids)
	} else if strings.Contains(report.Ready[0].Why, "feature share") {
		t.Fatalf("got %q", report.Ready[0].Why)
	}
}

func TestShareOrderKeepsEachRigToItsOwnShare(t *testing.T) {
	tracker, rules := aShareFixture(t, []shared{
		{"mw-q.fix", "[bug] an older fix", 2, 9},
		{"mw-q.feat", "A newer feature", 2, 2},
		{"mw-q.other-fix", "[bug] other rig fix", 2, 8},
		{"mw-q.other-feat", "other rig feature", 2, 1},
	})
	tracker.AddEpic("mw-r", domain.Path{
		Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Formula: "tdd-feature", Host: "vps",
	})
	tracker.AddStory("mw-r", domain.Story{ID: "mw-r.fix", Title: "[bug] lampas fix"})
	tracker.AddStory("mw-r", domain.Story{ID: "mw-r.feat", Title: "A lampas feature"})
	_ = tracker.SetCreated("mw-r.fix", queueNow.Add(-7*24*time.Hour))
	_ = tracker.SetCreated("mw-r.feat", queueNow.Add(-1*24*time.Hour))
	rules.Require("lampas", domain.EpicRequirements{FeatureShare: 0.6})

	var out bytes.Buffer
	report, err := application.Queue{Tracker: tracker, Host: "vps", Rules: rules, Now: func() time.Time { return queueNow }, Out: &out}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, row := range report.Ready {
		ids = append(ids, row.ID)
	}
	// Only lampas has a share; the places the rigs held are unchanged.
	pos := map[string]int{}
	for i, id := range ids {
		pos[id] = i
	}
	if pos["mw-r.feat"] > pos["mw-r.fix"] {
		t.Fatalf("expected lampas's feature before its fix, got %v", ids)
	}
	if pos["mw-q.fix"] > pos["mw-q.feat"] {
		t.Fatalf("expected millwright (no key) in today's order, got %v", ids)
	}
}

func TestShareOrderIsThePosternUpNextToo(t *testing.T) {
	tracker, rules := aShareFixture(t, []shared{
		{"mw-q.fix", "[bug] an older fix", 2, 9},
		{"mw-q.feat", "A newer feature", 2, 2},
	})
	runningShared(t, tracker, 5, 0)
	rules.Require("millwright", domain.EpicRequirements{FeatureShare: 0.6})
	if err := tracker.SetNote(context.Background(), "host.vps.last_sync", viewNow.Format(application.LastSyncFormat)); err != nil {
		t.Fatal(err)
	}
	upNext := func(rules application.EpicRules) []string {
		doc, err := application.PosternView{Tracker: tracker, Notes: tracker, Host: "desktop", Rules: rules,
			Now: func() time.Time { return viewNow }}.Build(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, h := range doc.Hosts {
			if h.Name == "vps" {
				for _, u := range h.UpNext {
					ids = append(ids, u.ID)
				}
			}
		}
		return ids
	}
	if got, want := upNext(rules), []string{"mw-q.feat", "mw-q.fix"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected Up next %v, got %v", want, got)
	}
	if got, want := upNext(nil), []string{"mw-q.fix", "mw-q.feat"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected today's Up next %v with no rules, got %v", want, got)
	}
}
