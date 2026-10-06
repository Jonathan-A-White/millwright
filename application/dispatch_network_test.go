package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// twoRigs is a factory with a story on a rig whose gate installs dependencies
// and one on a rig whose does not.
func twoRigs(t *testing.T, network application.NetworkReader) (application.Dispatch, *apptest.FakeTracker) {
	t.Helper()
	dispatch, tracker, _, _, _ := aFactory(t)
	tracker.AddEpic("mw-npm", domain.Path{
		Rig: "webapp", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Formula: "tdd-feature", Host: "vps",
	})
	tracker.AddStory("mw-npm", domain.Story{ID: "mw-npm.1", Title: "Needs npm ci"})
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "Needs no network"})
	dispatch.Cap = 5
	dispatch.Rigs = map[string]string{"millwright": "/rigs/millwright", "webapp": "/rigs/webapp"}
	dispatch.HeavyNet = map[string]bool{"webapp": true}
	dispatch.Network = network
	return dispatch, tracker
}

func startedIDs(report application.DispatchReport) []string {
	var ids []string
	for _, s := range report.Started {
		ids = append(ids, s.StoryID)
	}
	return ids
}

func TestMeteredDispatchPassesOverAHeavyNetRigAndTakesTheOther(t *testing.T) {
	dispatch, tracker := twoRigs(t, apptest.Metered())
	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if ids := startedIDs(report); len(ids) != 1 || ids[0] != "mw-gq6.1" {
		t.Fatalf("started %v, want only mw-gq6.1", ids)
	}
	var passed *application.Passed
	for i := range report.Passed {
		if report.Passed[i].StoryID == "mw-npm.1" {
			passed = &report.Passed[i]
		}
	}
	if passed == nil || !strings.Contains(passed.Why, "metered") {
		t.Fatalf("passed %+v, want mw-npm.1 passed over for the metered network", report.Passed)
	}
	if d, _ := tracker.ShowStory(context.Background(), "mw-npm.1"); d.Assignee != "" {
		t.Fatalf("the heavy story was claimed by %q", d.Assignee)
	}
}

func TestUnmeteredDispatchTakesBothAndTheHeavyOneIsTakenOnceTheNetworkIs(t *testing.T) {
	network := apptest.Metered()
	dispatch, _ := twoRigs(t, network)
	if report, err := dispatch.Run(context.Background()); err != nil || len(report.Started) != 1 {
		t.Fatalf("metered: %+v, %v", report, err)
	}

	network.Reading = apptest.Unmetered().Reading
	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if ids := startedIDs(report); len(ids) != 1 || ids[0] != "mw-npm.1" {
		t.Fatalf("the first tick on an unmetered network started %v, want mw-npm.1", ids)
	}
}

func TestUnmeteredDispatchTakesBoth(t *testing.T) {
	dispatch, _ := twoRigs(t, apptest.Unmetered())
	report, err := dispatch.Run(context.Background())
	if err != nil || len(report.Started) != 2 {
		t.Fatalf("started %v, %v; want both stories", startedIDs(report), err)
	}
}

func TestDispatchWithNoNetworkReaderTakesBoth(t *testing.T) {
	dispatch, _ := twoRigs(t, nil)
	if report, err := dispatch.Run(context.Background()); err != nil || len(report.Started) != 2 {
		t.Fatalf("started %v, %v; want both stories", startedIDs(report), err)
	}
}

func TestDispatchAsksTheNetworkOncePerTick(t *testing.T) {
	network := apptest.Metered()
	dispatch, _ := twoRigs(t, network)
	if _, err := dispatch.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if network.Reads != 1 {
		t.Fatalf("a tick asked the network %d times, want 1", network.Reads)
	}
}

func TestADryRunPassesOverTheHeavyNetStoryToo(t *testing.T) {
	dispatch, _ := twoRigs(t, apptest.Metered())
	dispatch.DryRun = true
	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if ids := startedIDs(report); len(ids) != 1 || ids[0] != "mw-gq6.1" {
		t.Fatalf("a dry run would start %v, want only mw-gq6.1", ids)
	}
}
