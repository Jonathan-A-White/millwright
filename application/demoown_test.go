package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// A story labelled demo with no files key is the Governor's own demo: it closes
// on his word and no Builder works it (mw-gq6.535). One with a files key is
// built by a Builder, and is dispatched as any story.
func TestADemoWithNoFilesKeyIsTheGovernorsOwn(t *testing.T) {
	cases := []struct {
		name   string
		labels []string
		files  string
		own    bool
	}{
		{"demo with no files", []string{application.LabelDemo}, "", true},
		{"demo with files", []string{application.LabelDemo}, "docs/demo-x.md", false},
		{"no demo, no files", nil, "", false},
		{"hitl demo with files", []string{application.LabelDemo, application.LabelHitl}, "docs/demo-x.md", true},
	}
	for _, c := range cases {
		detail := application.StoryDetail{
			Story:  domain.Story{ID: "mw-x.1", Overrides: domain.Path{Files: c.files}},
			Labels: c.labels,
		}
		if got := detail.Hitl(); got != c.own {
			t.Errorf("%s: Hitl() = %v, want %v", c.name, got, c.own)
		}
	}
}

func TestTheQueueHoldsTheGovernorsOwnDemoAndSaysWhy(t *testing.T) {
	ctx := context.Background()
	tracker := aQueueOf(t, queued{id: "mw-q.1", priority: 1}, queued{id: "mw-q.2", priority: 1}, queued{id: "mw-q.3", priority: 1, files: "docs/demo-q.md"})
	for _, id := range []string{"mw-q.1", "mw-q.3"} {
		if err := tracker.SetLabels(id, application.LabelDemo); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	got, err := application.Queue{Tracker: tracker, Host: "vps", Now: func() time.Time { return queueNow }, Out: &out}.Run(ctx)
	if err != nil {
		t.Fatalf("the queue: %v", err)
	}
	held := map[string]string{}
	for _, row := range got.Ready {
		held[row.ID] = row.Held
	}
	if want := "the Governor must be present for it (demo)"; held["mw-q.1"] != want {
		t.Errorf("expected the demo with no files held as %q, got %q", want, held["mw-q.1"])
	}
	if held["mw-q.2"] != "" || held["mw-q.3"] != "" {
		t.Errorf("expected the plain story and the Builder-built demo not held, got %v", held)
	}
	if !strings.Contains(out.String(), "the Governor must be present for it (demo)") {
		t.Errorf("expected the printed queue to say so, got:\n%s", out.String())
	}
}

func TestPathSetWarnsOfADemoWithNeitherHitlNorFiles(t *testing.T) {
	tracker, _ := aDemoEpic(t)
	var out strings.Builder
	if _, err := (application.PathSet{Tracker: tracker, Out: &out}).Run(context.Background(), "mw-e.2", "", []string{"size=S"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "mw-e.2 is labelled demo with no files key and no hitl label") {
		t.Errorf("expected the warning naming mw-e.2, got:\n%s", out.String())
	}
	out.Reset()
	if _, err := (application.PathSet{Tracker: tracker, Out: &out}).Run(context.Background(), "mw-e.2", "", []string{"files=docs/demo-e.md"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "no files key and no hitl label") {
		t.Errorf("expected no warning once the demo names files, got:\n%s", out.String())
	}
}

func TestFilingADemoWithNeitherHitlNorFilesWarns(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature")
	plan := sizedPlan(domain.SizeS, domain.SizeS, domain.SizeS)
	plan.Stories[0].Labels = []string{application.LabelDemo}
	plan.Stories[1].Labels = []string{application.LabelDemo, application.LabelHitl}
	plan.Stories[2].Labels = []string{application.LabelDemo}
	plan.Stories[2].Overrides.Files = "docs/demo-s2.md"
	out := &strings.Builder{}
	filed, err := application.File{Tracker: tracker, Out: out}.Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("filing: %v", err)
	}
	if want := filed.Stories[0].ID + " is labelled demo with no files key and no hitl label"; !strings.Contains(out.String(), want) {
		t.Errorf("expected %q, got:\n%s", want, out.String())
	}
	for _, other := range filed.Stories[1:] {
		if strings.Contains(out.String(), other.ID+" is labelled demo") {
			t.Errorf("expected no warning for %s, got:\n%s", other.ID, out.String())
		}
	}
}

func TestStatusListsTheGovernorsOwnDemoAmongThoseWaitingForHim(t *testing.T) {
	tracker := aQueueOf(t, queued{id: "mw-q.1", priority: 1}, queued{id: "mw-q.2", priority: 1, files: "docs/demo-q.md"})
	for _, id := range []string{"mw-q.1", "mw-q.2"} {
		if err := tracker.SetLabels(id, application.LabelDemo); err != nil {
			t.Fatal(err)
		}
	}
	report, err := application.Status{Tracker: tracker, Notes: tracker, Host: "vps", Seat: "builder"}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var waiting, ready []string
	for _, d := range report.Waiting {
		waiting = append(waiting, d.Story.ID)
	}
	for _, d := range report.Ready {
		ready = append(ready, d.Story.ID)
	}
	if strings.Join(waiting, ",") != "mw-q.1" || strings.Join(ready, ",") != "mw-q.2" {
		t.Errorf("expected mw-q.1 waiting for the Governor and mw-q.2 ready, got waiting %v ready %v", waiting, ready)
	}
}
