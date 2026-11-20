package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

var groomNow = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

// groomSource is a read-only tracker for the groom: stories, the graph and the
// comments of held beads. It has no write method, so a groom that wrote would
// not compile.
type groomSource struct {
	stories  []application.StoryDetail
	graph    []domain.GraphBead
	comments map[string][]application.Comment
}

func (s *groomSource) StoriesSince(context.Context, time.Time) ([]application.StoryDetail, error) {
	return s.stories, nil
}

func (s *groomSource) StoriesComments(_ context.Context, ids []string) (map[string][]application.Comment, error) {
	out := map[string][]application.Comment{}
	for _, id := range ids {
		out[id] = s.comments[id]
	}
	return out, nil
}

func (s *groomSource) BeadGraph(context.Context) ([]domain.GraphBead, error) { return s.graph, nil }

// bead adds a story of rig lampas (or another rig) to the source and the graph.
type groomSpec struct {
	id, title, status, typ, files, rig, closeReason string
	priority                                        int
	labels                                          []string
	created, closedAt                               time.Time
	needs                                           []string
	parent                                          string
}

func (s *groomSource) add(b groomSpec) {
	if b.rig == "" {
		b.rig = "lampas"
	}
	if b.status == "" {
		b.status = "open"
	}
	if b.typ == "" {
		b.typ = "task"
	}
	if b.priority == 0 {
		b.priority = 2
	}
	d := application.StoryDetail{
		Story:       domain.Story{ID: b.id, Title: b.title, Overrides: domain.Path{Files: b.files}},
		Defaults:    domain.Path{Rig: b.rig},
		Status:      b.status,
		Type:        b.typ,
		Labels:      b.labels,
		Priority:    b.priority,
		Created:     b.created,
		ClosedAt:    b.closedAt,
		CloseReason: b.closeReason,
		Needs:       b.needs,
	}
	s.stories = append(s.stories, d)
	s.graphBead(b)
}

func (s *groomSource) graphBead(b groomSpec) {
	g := domain.GraphBead{ID: b.id, Title: b.title, Status: b.status, Type: b.typ, Labels: b.labels, Priority: b.priority, Created: b.created}
	for _, n := range b.needs {
		g.Edges = append(g.Edges, domain.BeadEdge{To: n, Kind: "blocks"})
	}
	if b.parent != "" {
		g.Edges = append(g.Edges, domain.BeadEdge{To: b.parent, Kind: domain.EdgeParentChild})
	}
	s.graph = append(s.graph, g)
}

func (s *groomSource) epic(id, title, status string) {
	s.graphBead(groomSpec{id: id, title: title, status: status, typ: "epic"})
}

func groomDaysAgo(n int) time.Time { return groomNow.AddDate(0, 0, -n) }

func runGroom(t *testing.T, source *groomSource, rules application.EpicRules, lists application.PacedLists, asJSON bool) (application.GroomReport, string) {
	t.Helper()
	var out bytes.Buffer
	report, err := application.Groom{
		Source: source, Rig: "lampas", Rules: rules, Lists: lists,
		Now: func() time.Time { return groomNow }, JSON: asJSON, Out: &out,
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return report, out.String()
}

type groomLists []application.PacedList

func (f groomLists) PacedLists(context.Context) ([]application.PacedList, error) { return f, nil }

func groomRules(share float64, floor int) *apptest.FakeEpicRules {
	rules := apptest.NewFakeEpicRules()
	rules.Require("lampas", domain.EpicRequirements{FeatureShare: share, FeatureFloor: floor})
	return rules
}

func TestGroomJSONHasEightKeys(t *testing.T) {
	_, out := runGroom(t, &groomSource{}, nil, nil, true)
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &keys); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(keys) != 8 {
		t.Errorf("expected 8 keys, got %d: %s", len(keys), out)
	}
	for key, raw := range keys {
		if string(raw) == "null" {
			t.Errorf("key %s is null, want an empty list or object", key)
		}
	}
}

func TestGroomEpicsDoneStillOpenAndOnlyDemo(t *testing.T) {
	s := &groomSource{}
	s.epic("mw-done", "Finished epic", "open")
	s.add(groomSpec{id: "mw-done.1", title: "one", status: "closed", closedAt: groomDaysAgo(30), closeReason: "Landed", parent: "mw-done"})
	s.add(groomSpec{id: "mw-done.2", title: "two", status: "closed", closedAt: groomDaysAgo(30), closeReason: "Landed", parent: "mw-done"})
	s.epic("mw-demo", "Only a demo left", "open")
	s.add(groomSpec{id: "mw-demo.1", title: "built", status: "closed", closedAt: groomDaysAgo(30), closeReason: "Landed", parent: "mw-demo"})
	s.add(groomSpec{id: "mw-demo.2", title: "Demo: show it", status: "deferred", labels: []string{"demo"}, parent: "mw-demo"})
	s.epic("mw-busy", "Still going", "open")
	s.add(groomSpec{id: "mw-busy.1", title: "closed", status: "closed", closedAt: groomDaysAgo(3), closeReason: "Landed", parent: "mw-busy"})
	s.add(groomSpec{id: "mw-busy.2", title: "open work", parent: "mw-busy"})
	s.add(groomSpec{id: "mw-busy.3", title: "Demo", status: "deferred", labels: []string{"demo"}, parent: "mw-busy"})
	s.epic("mw-closed", "Closed epic", "closed")
	s.add(groomSpec{id: "mw-closed.1", title: "x", status: "closed", closedAt: groomDaysAgo(3), closeReason: "Landed", parent: "mw-closed"})
	s.epic("mw-other", "Other rig's epic", "open")
	s.add(groomSpec{id: "mw-other.1", title: "x", rig: "postern", status: "closed", closedAt: groomDaysAgo(3), closeReason: "Landed", parent: "mw-other"})

	report, out := runGroom(t, s, nil, nil, false)
	if got := groomIDs(report.Epics.DoneStillOpen); got != "mw-done" {
		t.Errorf("done, still open: got %q, want mw-done", got)
	}
	if got := groomIDs(report.Epics.OnlyDemo); got != "mw-demo" {
		t.Errorf("only a demo left: got %q, want mw-demo", got)
	}
	if n := strings.Count(out, "done, still open"); n != 1 {
		t.Errorf("text says 'done, still open' %d times, want 1:\n%s", n, out)
	}
}

func groomIDs(beads []application.GroomBead) string {
	var out []string
	for _, b := range beads {
		out = append(out, b.ID)
	}
	return strings.Join(out, ",")
}

func TestGroomHeldWithLiveBeadsBehindAndNoReason(t *testing.T) {
	s := &groomSource{comments: map[string][]application.Comment{
		"mw-h.2": {{Text: "HELD: waits for his word"}},
	}}
	s.add(groomSpec{id: "mw-h.1", title: "held, no reason", status: "deferred"})
	s.add(groomSpec{id: "mw-h.2", title: "held, with reason", status: "deferred"})
	s.add(groomSpec{id: "mw-h.3", title: "held, nobody behind", status: "deferred"})
	s.add(groomSpec{id: "mw-h.4", title: "live behind h1", needs: []string{"mw-h.1"}})
	s.add(groomSpec{id: "mw-h.5", title: "live behind h2", needs: []string{"mw-h.2"}})
	report, out := runGroom(t, s, nil, nil, false)
	if len(report.HeldNoReason) != 1 || report.HeldNoReason[0].ID != "mw-h.1" || report.HeldNoReason[0].Behind != 1 {
		t.Fatalf("held with no reason: got %+v", report.HeldNoReason)
	}
	if !strings.Contains(out, "mw-h.1") || strings.Contains(out, "mw-h.2") {
		t.Errorf("text should name mw-h.1 and not mw-h.2:\n%s", out)
	}
}

func TestGroomFeaturesAgainstTargetAndTheFloor(t *testing.T) {
	s := &groomSource{}
	// This week: 2 features (one content), 3 fixes, 1 express fix landed; one older landing is outside.
	s.add(groomSpec{id: "f1", title: "A feature", status: "closed", closedAt: groomDaysAgo(1), closeReason: "Landed"})
	s.add(groomSpec{id: "f2", title: "A pass", status: "closed", closedAt: groomDaysAgo(2), closeReason: "Landed", labels: []string{"model-story"}})
	s.add(groomSpec{id: "b1", title: "[bug] lampas: x", status: "closed", typ: "bug", closedAt: groomDaysAgo(1), closeReason: "Landed"})
	s.add(groomSpec{id: "b2", title: "[bug] lampas: y", status: "closed", typ: "bug", closedAt: groomDaysAgo(3), closeReason: "Landed"})
	s.add(groomSpec{id: "b3", title: "[bug] lampas: z", status: "closed", typ: "bug", closedAt: groomDaysAgo(3), closeReason: "Landed"})
	s.add(groomSpec{id: "b4", title: "[bug] lampas: red", status: "closed", typ: "bug", priority: 1, closedAt: groomDaysAgo(3), closeReason: "Landed"})
	s.add(groomSpec{id: "old", title: "Old feature", status: "closed", closedAt: groomDaysAgo(20), closeReason: "Landed"})
	s.add(groomSpec{id: "sup", title: "Superseded", status: "closed", typ: "bug", closedAt: groomDaysAgo(1), closeReason: "Superseded by x"})
	s.add(groomSpec{id: "pw", title: "A postern feature", rig: "postern", status: "closed", closedAt: groomDaysAgo(1), closeReason: "Landed"})
	// Now: one feature ready, two fixes ready, one fix running.
	s.add(groomSpec{id: "rf", title: "Ready feature"})
	s.add(groomSpec{id: "rb1", title: "[bug] lampas: r1", typ: "bug"})
	s.add(groomSpec{id: "rb2", title: "[bug] lampas: r2", typ: "bug"})
	s.add(groomSpec{id: "run", title: "[bug] lampas: running", typ: "bug", status: "in_progress"})

	report, out := runGroom(t, s, groomRules(0.5, 5), nil, false)
	f := report.Features
	if f.Rig != "lampas" || f.Landed != 6 || f.Features != 2 || f.Content != 1 || f.Fixes != 3 || f.Express != 1 || f.Percent != 33 {
		t.Errorf("features: got %+v", f)
	}
	if f.Target != 50 || f.Floor != 5 || f.Ready != 1 {
		t.Errorf("target, floor, ready: got %+v", f)
	}
	b := report.FixBudget
	// (1 - 0.5) x 5 non-express landings = 2.5 -> 3 (rounded); 3 landed + 1 running + 2 ready = 6.
	if b.Budget == nil || *b.Budget != 3 || b.Landed != 3 || b.Running != 1 || b.Ready != 2 || b.Total != 6 {
		t.Errorf("fix budget: got %+v", b)
	}
	for _, want := range []string{"FEATURES lampas 2/6 (33%)", "target 50%", "feature floor 5: 1 ready", "fixes this week: landed 3 + running 1 + ready 2 = 6 of budget 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("text lacks %q:\n%s", want, out)
		}
	}
}

func TestGroomWithNoShareKeyHasNoBudget(t *testing.T) {
	s := &groomSource{}
	s.add(groomSpec{id: "f1", title: "A feature", status: "closed", closedAt: groomDaysAgo(1), closeReason: "Landed"})
	report, out := runGroom(t, s, groomRules(0, 0), nil, false)
	if report.FixBudget.Budget != nil || report.Features.Target != 0 {
		t.Errorf("no share key: got %+v / %+v", report.FixBudget, report.Features)
	}
	if !strings.Contains(out, "no feature_share") {
		t.Errorf("text should say there is no feature_share:\n%s", out)
	}
}

func TestGroomFeatureWhoseRootBlockerIsABug(t *testing.T) {
	s := &groomSource{}
	s.add(groomSpec{id: "bug", title: "[bug] lampas: root", typ: "bug", status: "deferred"})
	s.add(groomSpec{id: "mid", title: "A middle enabler", needs: []string{"bug"}})
	s.add(groomSpec{id: "feat", title: "The feature", needs: []string{"mid"}})
	s.add(groomSpec{id: "free", title: "Another feature"})
	s.add(groomSpec{id: "done", title: "Done blocker", status: "closed", closedAt: groomDaysAgo(1), closeReason: "Landed"})
	s.add(groomSpec{id: "clear", title: "Feature past a closed blocker", needs: []string{"done"}})
	report, out := runGroom(t, s, nil, nil, false)
	var found []string
	for _, b := range report.BlockedFeatures {
		found = append(found, b.Feature+"<-"+b.Bug)
	}
	if strings.Join(found, ",") != "feat<-bug,mid<-bug" {
		t.Errorf("blocked features: got %v", found)
	}
	if !strings.Contains(out, "feat") || !strings.Contains(out, "bug") {
		t.Errorf("text lacks the blocked feature:\n%s", out)
	}
}

func TestGroomIceCandidatesAreAutoFoundHeld21DaysNeverHisNorWaitedOn(t *testing.T) {
	s := &groomSource{}
	held := func(id string, age int, labels ...string) {
		s.add(groomSpec{id: id, title: "[bug] lampas: " + id, typ: "bug", status: "deferred", created: groomDaysAgo(age), labels: append([]string{"bug"}, labels...)})
	}
	held("old-found", 25, "found-at-close")
	held("young-found", 10, "found-at-close")
	held("his", 40, "found-at-close", "his-report")
	held("hispri", 40, "found-at-close", "his-priority")
	held("hand-filed", 40)
	held("iced", 40, "found-at-close", "icebox")
	held("needed", 30, "found-at-close")
	s.add(groomSpec{id: "feature", title: "A feature that waits", needs: []string{"needed"}})
	report, _ := runGroom(t, s, nil, nil, false)
	if got := groomIDs(report.IceCandidates); got != "old-found" {
		t.Errorf("ice candidates: got %q, want old-found", got)
	}
}

func TestGroomPacedListsWithNothingOpen(t *testing.T) {
	s := &groomSource{}
	s.add(groomSpec{id: "p1", title: "paced 1", status: "closed", closedAt: groomDaysAgo(2), closeReason: "Landed"})
	s.add(groomSpec{id: "p2", title: "paced 2", status: "deferred"})
	s.add(groomSpec{id: "q1", title: "busy 1", status: "closed", closedAt: groomDaysAgo(2), closeReason: "Landed"})
	s.add(groomSpec{id: "q2", title: "busy 2"})
	s.add(groomSpec{id: "z1", title: "other rig", rig: "postern", status: "deferred"})
	lists := groomLists{
		{Name: "plans/a/release-order.txt", IDs: []string{"p1", "p2"}},
		{Name: "plans/b/release-order.txt", IDs: []string{"q1", "q2"}},
		{Name: "plans/c/release-order.txt", IDs: []string{"z1"}},
	}
	report, out := runGroom(t, s, nil, lists, false)
	if len(report.PacedLists) != 1 || report.PacedLists[0].Name != "plans/a/release-order.txt" || report.PacedLists[0].Held != 1 || report.PacedLists[0].Closed != 1 {
		t.Fatalf("paced lists: got %+v", report.PacedLists)
	}
	if !strings.Contains(out, "plans/a/release-order.txt") || strings.Contains(out, "plans/b/") {
		t.Errorf("text should name list a only:\n%s", out)
	}
}

func TestGroomFoldGroupsFoundAtCloseByDirectoryAndTasteByScreen(t *testing.T) {
	s := &groomSource{}
	found := func(id, files string) {
		s.add(groomSpec{id: id, title: "found " + id, typ: "bug", status: "deferred", files: files, labels: []string{"bug", "found-at-close"}})
	}
	found("fa1", "application/a.go,application/a_test.go")
	found("fa2", "application/b.go")
	found("fa3", "application/c.go")
	found("fb1", "cmd/mw/x.go")
	found("fb2", "cmd/mw/y.go")
	for _, id := range []string{"t1", "t2", "t3"} {
		s.add(groomSpec{id: id, title: "[taste] lampas: Lampas Review: tweak " + id, status: "deferred", labels: []string{"taste"}})
	}
	report, out := runGroom(t, s, nil, nil, false)
	keys := map[string]int{}
	for _, g := range report.FoldGroups {
		keys[g.Key] = len(g.Bugs)
	}
	if keys["dir:application"] != 3 || keys["screen:Lampas Review"] != 3 || len(keys) != 2 {
		t.Errorf("fold groups: got %v", keys)
	}
	if !strings.Contains(out, "dir:application (3)") {
		t.Errorf("text lacks the directory group:\n%s", out)
	}
}

func TestFoldAloneStillLeavesFoundAtCloseAndTasteOut(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-e", domain.Path{Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude, Model: domain.ModelSonnet, Effort: domain.EffortHigh, Host: "laptop", Formula: "tdd-feature"})
	for _, id := range []string{"mw-e.1", "mw-e.2", "mw-e.3"} {
		tracker.AddStory("mw-e", domain.Story{ID: id, Title: "[taste] lampas: Lampas Review: tweak " + id})
		if err := tracker.SetLabels(id, application.LabelTaste); err != nil {
			t.Fatal(err)
		}
	}
	report, _ := runFold(t, tracker, 3)
	if len(report.Groups) != 0 {
		t.Errorf("mw fold must keep its groups as they were, got %+v", report.Groups)
	}
}

func TestGroomListsAreFilledFromOneRead(t *testing.T) {
	s := &groomSource{}
	report, out := runGroom(t, s, nil, nil, false)
	if len(report.Epics.DoneStillOpen) != 0 || out == "" {
		t.Errorf("an empty rig should still print its sections:\n%s", out)
	}
}
