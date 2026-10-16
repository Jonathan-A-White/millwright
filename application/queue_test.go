package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

var queueNow = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

// aQueueOf files stories under one epic on host vps: id, priority, days old and size.
func aQueueOf(t *testing.T, stories ...queued) *apptest.FakeTracker {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-q", domain.Path{
		Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Formula: "tdd-feature", Host: "vps",
	})
	for _, s := range stories {
		tracker.AddStory("mw-q", domain.Story{ID: s.id, Title: "Story " + s.id, Overrides: domain.Path{Size: s.size, Files: s.files}})
		if err := tracker.SetPriority(s.id, s.priority); err != nil {
			t.Fatal(err)
		}
		if s.days > 0 {
			if err := tracker.SetCreated(s.id, queueNow.Add(-time.Duration(s.days)*24*time.Hour)); err != nil {
				t.Fatal(err)
			}
		}
	}
	return tracker
}

type queued struct {
	id       string
	priority int
	days     int
	size     domain.Size
	files    string
}

func TestTheQueueIsTheOrderADryRunDispatchTakes(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	for _, s := range []queued{
		{"mw-q.1", 2, 3, domain.SizeS, ""},
		{"mw-q.2", 1, 1, domain.SizeM, ""},
		{"mw-q.3", 2, 9, domain.SizeXS, ""},
		{"mw-q.4", 0, 0, "", ""},
		{"mw-q.5", 1, 7, domain.SizeL, ""},
	} {
		tracker.AddStory("mw-gq6", domain.Story{ID: s.id, Title: "Story " + s.id, Overrides: domain.Path{Size: s.size}})
		_ = tracker.SetPriority(s.id, s.priority)
		if s.days > 0 {
			_ = tracker.SetCreated(s.id, queueNow.Add(-time.Duration(s.days)*24*time.Hour))
		}
	}
	dispatch.Cap = 10
	dispatch.DryRun = true
	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("the dry run: %v", err)
	}
	var want []string
	for _, s := range report.Started {
		want = append(want, s.StoryID)
	}
	if len(want) != 5 {
		t.Fatalf("expected the dry run to take all five, got %v", want)
	}

	var out bytes.Buffer
	got, err := application.Queue{Tracker: tracker, Host: "vps", Now: func() time.Time { return queueNow }, Out: &out}.Run(ctx)
	if err != nil {
		t.Fatalf("the queue: %v", err)
	}
	var ids []string
	for _, row := range got.Ready {
		ids = append(ids, row.ID)
	}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("expected the queue in the dry run's order %v, got %v", want, ids)
	}
	if first := got.Ready[0]; first.Priority != 0 || first.Why == "" {
		t.Fatalf("expected the first row to say why: %+v", first)
	}
	if !strings.Contains(got.Ready[1].Why, "P1") || !strings.Contains(got.Ready[1].Why, "7d") || !strings.Contains(got.Ready[1].Why, "size L") {
		t.Fatalf("expected P, age and size in the reason, got %q", got.Ready[1].Why)
	}
}

func TestTheQueueJSONIsTheReport(t *testing.T) {
	ctx := context.Background()
	tracker := aQueueOf(t, queued{"mw-q.1", 1, 2, domain.SizeS, ""}, queued{"mw-q.2", 0, 1, "", ""})
	var out bytes.Buffer
	_, err := application.Queue{Tracker: tracker, Host: "vps", Now: func() time.Time { return queueNow }, JSON: true, Out: &out}.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Host  string `json:"host"`
		Ready []struct {
			ID string `json:"id"`
		} `json:"ready"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("expected JSON, got %q: %v", out.String(), err)
	}
	if doc.Host != "vps" || len(doc.Ready) != 2 || doc.Ready[0].ID != "mw-q.2" || doc.Ready[1].ID != "mw-q.1" {
		t.Fatalf("unexpected JSON: %s", out.String())
	}
}

func TestTheQueueNamesWhatEachBlockedStoryWaitsOnAndWhatTheFilesGuardHolds(t *testing.T) {
	ctx := context.Background()
	tracker := aQueueOf(t,
		queued{"mw-q.1", 1, 5, domain.SizeS, "a.go"},
		queued{"mw-q.2", 1, 4, domain.SizeS, "a.go"},
		queued{"mw-q.3", 1, 3, domain.SizeS, ""},
		queued{"mw-q.4", 1, 2, domain.SizeS, ""},
	)
	tracker.Needs("mw-q.3", "mw-q.1")
	tracker.Needs("mw-q.4", "mw-q.3", "mw-q.9")
	// mw-q.1 is running with a.go, so mw-q.2 waits on the files guard.
	if err := tracker.SetStatus("mw-q.1", apptest.StatusInProgress); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	report, err := application.Queue{Tracker: tracker, Host: "vps", Now: func() time.Time { return queueNow }, Out: &out}.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Ready) != 1 || report.Ready[0].ID != "mw-q.2" || !strings.Contains(report.Ready[0].Held, "a.go") || !strings.Contains(report.Ready[0].Held, "mw-q.1") {
		t.Fatalf("expected mw-q.2 ready and held by the files guard, got %+v", report.Ready)
	}
	if len(report.Blocked) != 2 {
		t.Fatalf("expected two blocked, got %+v", report.Blocked)
	}
	if b := report.Blocked[0]; b.ID != "mw-q.3" || !reflect.DeepEqual(b.WaitsOn, []string{"mw-q.1"}) {
		t.Fatalf("expected mw-q.3 to name mw-q.1, got %+v", b)
	}
	if !strings.Contains(out.String(), "waits on mw-q.1") {
		t.Fatalf("expected the printed line to name the blocker, got:\n%s", out.String())
	}
}

func TestTheQueueShowsAtMostFiveBlockedAndNarrowsToARig(t *testing.T) {
	ctx := context.Background()
	var stories []queued
	for _, id := range []string{"mw-q.1", "mw-q.2", "mw-q.3", "mw-q.4", "mw-q.5", "mw-q.6", "mw-q.7"} {
		stories = append(stories, queued{id, 2, 1, "", ""})
	}
	tracker := aQueueOf(t, append(stories, queued{"mw-q.0", 2, 1, "", ""})...)
	for _, id := range []string{"mw-q.1", "mw-q.2", "mw-q.3", "mw-q.4", "mw-q.5", "mw-q.6", "mw-q.7"} {
		tracker.Needs(id, "mw-q.0")
	}
	report, err := application.Queue{Tracker: tracker, Host: "vps", Now: func() time.Time { return queueNow }}.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Blocked) != 5 {
		t.Fatalf("expected five blocked at most, got %d", len(report.Blocked))
	}
	other, err := application.Queue{Tracker: tracker, Host: "vps", Rig: "lampas", Now: func() time.Time { return queueNow }}.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(other.Ready) != 0 || len(other.Blocked) != 0 {
		t.Fatalf("expected nothing for another rig, got %+v", other)
	}
}

func TestTheViewCarriesUpNextPerHostInStartOrder(t *testing.T) {
	tracker := aQueueOf(t,
		queued{"mw-q.1", 2, 5, "", ""},
		queued{"mw-q.2", 0, 1, "", ""},
		queued{"mw-q.3", 1, 2, "", ""},
		queued{"mw-q.4", 3, 9, "", ""},
	)
	if err := tracker.SetNote(context.Background(), "host.vps.last_sync", viewNow.Format(application.LastSyncFormat)); err != nil {
		t.Fatal(err)
	}
	doc := viewDoc(t, tracker)
	var vps application.PosternViewHost
	for _, h := range doc.Hosts {
		if h.Name == "vps" {
			vps = h
		}
	}
	var ids []string
	for _, u := range vps.UpNext {
		ids = append(ids, u.ID)
	}
	if want := []string{"mw-q.2", "mw-q.3", "mw-q.1"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("expected up_next %v, got %v (hosts %+v)", want, ids, doc.Hosts)
	}
	if vps.UpNext[0].Title != "Story mw-q.2" {
		t.Fatalf("expected the title, got %+v", vps.UpNext[0])
	}
	raw, _ := json.Marshal(doc)
	if !strings.Contains(string(raw), `"up_next":[{"id":"mw-q.2","title":"Story mw-q.2"}`) {
		t.Fatalf("expected up_next in the JSON, got %s", raw)
	}
}
