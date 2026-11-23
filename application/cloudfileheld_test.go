package application_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.1669: mw cloud counted every ready story of host auto as waiting for
// any host, including the ones held on a file a running story names, which no
// new box can start either; cloud6 and cloud7 were made for 11 of them and sat
// idle.

// fileHeldFactory files `held` ready stories that name the file a running
// story, live on desktop, names, and `free` ready stories that name a file of
// their own. desktop may run `cap` sessions.
func fileHeldFactory(t *testing.T, held, free, cap int) (*apptest.FakeTracker, application.CloudPlan, time.Time) {
	t.Helper()
	now := time.Date(2026, 11, 23, 4, 45, 0, 0, time.UTC)
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-f", domain.Path{Rig: "millwright", Branch: "main", Host: domain.HostAuto})
	add := func(id, files string) {
		tracker.AddStory("mw-f", domain.Story{ID: id, Title: "story " + id})
		if err := tracker.SetStoryMetadata(context.Background(), id, map[string]string{"files": files}); err != nil {
			t.Fatal(err)
		}
	}
	add("mw-f.run", "eslint-suppressions.json")
	if err := tracker.SetStoryMetadata(context.Background(), "mw-f.run", map[string]string{"host": "desktop"}); err != nil {
		t.Fatal(err)
	}
	if err := tracker.ClaimAs("mw-f.run", "mw@desktop", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for i := range held {
		add(fmt.Sprintf("mw-f.held%d", i), "eslint-suppressions.json,src/held"+fmt.Sprint(i)+".ts")
	}
	for i := range free {
		add(fmt.Sprintf("mw-f.free%d", i), fmt.Sprintf("src/free%d.ts", i))
	}
	plan := cloudPlan
	plan.HostCaps = map[string]int{"desktop": cap}
	return tracker, plan, now
}

func runFileHeldCheck(t *testing.T, tracker *apptest.FakeTracker, plan application.CloudPlan, now time.Time) (application.CloudReport, *apptest.FakeCloud) {
	t.Helper()
	cloud := &apptest.FakeCloud{}
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11"}}
	report, err := application.CloudCheck{
		Work: tracker, Stories: tracker, Provider: cloud, Book: book, Host: "desktop",
		Plan: plan, Now: func() time.Time { return now },
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return report, cloud
}

func TestStoriesHeldOnAFileARunningStoryNamesAreNotWaitingForAnyHost(t *testing.T) {
	tracker, plan, now := fileHeldFactory(t, 11, 0, 1)
	report, cloud := runFileHeldCheck(t, tracker, plan, now)
	if report.Waiting != 0 || report.FileHeld != 11 {
		t.Errorf("expected 0 waiting and 11 held on files, got %d and %d", report.Waiting, report.FileHeld)
	}
	if len(report.Making) != 0 || len(cloud.TakeBegun()) != 0 {
		t.Errorf("expected no box made for stories no host may start, made %v", report.Making)
	}
	want := "cloud: 0 stories wait for any host (11 more held on files), 0 sessions free, 0 boxes up"
	if got := report.String(); len(got) < len(want) || got[:len(want)] != want {
		t.Errorf("expected the line to start %q, got %q", want, got)
	}
}

func TestAReadyStoryWithNoHeldFileStillCountsAsWaiting(t *testing.T) {
	// 11 held, 1 free to start, 10 sessions free: the one is served, no box.
	tracker, plan, now := fileHeldFactory(t, 11, 1, 11)
	report, cloud := runFileHeldCheck(t, tracker, plan, now)
	if report.Waiting != 1 || report.FileHeld != 11 || report.Free != 10 {
		t.Errorf("expected 1 waiting, 11 held, 10 free, got %d, %d, %d", report.Waiting, report.FileHeld, report.Free)
	}
	if len(cloud.TakeBegun()) != 0 {
		t.Errorf("expected no box made, made %v", report.Making)
	}
}

func TestElevenStoriesFreeToStartStillMakeABox(t *testing.T) {
	tracker, plan, now := fileHeldFactory(t, 0, 11, 11)
	report, cloud := runFileHeldCheck(t, tracker, plan, now)
	if report.Waiting != 11 || report.FileHeld != 0 || report.Free != 10 {
		t.Errorf("expected 11 waiting, 0 held, 10 free, got %d, %d, %d", report.Waiting, report.FileHeld, report.Free)
	}
	if len(report.Making) != 1 || len(cloud.TakeBegun()) != 1 {
		t.Errorf("expected one box made, got %v", report.Making)
	}
}

// mw-gq6.1673: ready stories that name one file can start only one at a time,
// so a set of them is one unit of demand, not one each; cloud11 and cloud12
// were made for sets like that and sat idle.

// foldedFactory files `sharing` ready stories that all name eslint-suppressions.json
// and `free` that name a file of their own; nothing runs. desktop may run `cap`.
func foldedFactory(t *testing.T, sharing, free, cap int) (*apptest.FakeTracker, application.CloudPlan, time.Time) {
	t.Helper()
	now := time.Date(2026, 11, 23, 6, 52, 0, 0, time.UTC)
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-f", domain.Path{Rig: "millwright", Branch: "main", Host: domain.HostAuto})
	add := func(id, files string) {
		tracker.AddStory("mw-f", domain.Story{ID: id, Title: "story " + id})
		if err := tracker.SetStoryMetadata(context.Background(), id, map[string]string{"files": files}); err != nil {
			t.Fatal(err)
		}
	}
	for i := range sharing {
		add(fmt.Sprintf("mw-f.share%d", i), fmt.Sprintf("eslint-suppressions.json,src/share%d.ts", i))
	}
	for i := range free {
		add(fmt.Sprintf("mw-f.free%d", i), fmt.Sprintf("src/free%d.ts", i))
	}
	plan := cloudPlan
	plan.HostCaps = map[string]int{"desktop": cap}
	return tracker, plan, now
}

func TestReadyStoriesThatShareAFileAreOneUnitOfDemand(t *testing.T) {
	tracker, plan, now := foldedFactory(t, 10, 2, 5)
	report, cloud := runFileHeldCheck(t, tracker, plan, now)
	if report.Waiting != 3 || report.FileFolded != 9 || report.FileHeld != 0 || report.Free != 5 {
		t.Errorf("expected 3 waiting, 9 folded, 0 held, 5 free, got %d, %d, %d, %d", report.Waiting, report.FileFolded, report.FileHeld, report.Free)
	}
	if len(report.Making) != 0 || len(cloud.TakeBegun()) != 0 {
		t.Errorf("expected no box made for a set only one of which can start, made %v", report.Making)
	}
	want := "cloud: 3 stories wait for any host (9 more share a file with another waiting story), 5 sessions free"
	if got := report.String(); len(got) < len(want) || got[:len(want)] != want {
		t.Errorf("expected the line to start %q, got %q", want, got)
	}
}

func TestTwelveStoriesThatShareNothingStillMakeABoxWhenElevenAreFree(t *testing.T) {
	tracker, plan, now := foldedFactory(t, 0, 12, 11)
	report, cloud := runFileHeldCheck(t, tracker, plan, now)
	if report.Waiting != 12 || report.FileFolded != 0 {
		t.Errorf("expected 12 waiting and none folded, got %d and %d", report.Waiting, report.FileFolded)
	}
	if len(report.Making) != 1 || len(cloud.TakeBegun()) != 1 {
		t.Errorf("expected one box made, got %v", report.Making)
	}
}

func TestTheLineSaysBothTheHeldAndTheFoldedStories(t *testing.T) {
	r := application.CloudReport{Waiting: 3, FileHeld: 2, FileFolded: 9}
	want := "3 stories wait for any host (2 more held on files, 9 more share a file with another waiting story)"
	if got := r.String(); len(got) < len(want) || got[len("cloud: "):len("cloud: ")+len(want)] != want {
		t.Errorf("expected %q in the line, got %q", want, got)
	}
}
