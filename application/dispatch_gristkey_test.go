package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// aGristKeyFactory is a factory with two ready stories and room for one: .1 is
// the one under test, .2 names no grist check. host is the story's host.
func aGristKeyFactory(t *testing.T, hasKey bool) (application.Dispatch, *apptest.FakeTracker) {
	t.Helper()
	dispatch, tracker, _, _, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "Checks its grist"})
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.2", Title: "Changes a heading"})
	dispatch.HasGristTestKey = func() bool { return hasKey }
	return dispatch, tracker
}

func passedFor(report application.DispatchReport, id string) *application.Passed {
	for i := range report.Passed {
		if report.Passed[i].StoryID == id {
			return &report.Passed[i]
		}
	}
	return nil
}

func TestDispatchOnAHostWithNoGristKeyPassesOverAStoryWhoseAcceptanceRunsGristEval(t *testing.T) {
	for _, command := range []string{"mw grist eval lampas", "mw grist smoke lampas --kind talk"} {
		dispatch, tracker := aGristKeyFactory(t, false)
		if err := tracker.SetAcceptance("mw-gq6.1", "1. `"+command+"` passes."); err != nil {
			t.Fatal(err)
		}
		report, err := dispatch.Run(context.Background())
		if err != nil {
			t.Fatalf("%s: dispatching: %v", command, err)
		}
		if ids := startedIDs(report); len(ids) != 1 || ids[0] != "mw-gq6.2" {
			t.Fatalf("%s: started %v, want only mw-gq6.2", command, ids)
		}
		passed := passedFor(report, "mw-gq6.1")
		if passed == nil || passed.Gate != "no-grist-key" || !strings.Contains(passed.Why, "grist test key") {
			t.Fatalf("%s: passed %+v, want mw-gq6.1 passed over with gate no-grist-key", command, report.Passed)
		}
		if d, _ := tracker.ShowStory(context.Background(), "mw-gq6.1"); d.Assignee != "" || d.Status != "open" {
			t.Fatalf("%s: the story was claimed: status %q assignee %q", command, d.Status, d.Assignee)
		}
	}
}

func TestDispatchOnAHostWithNoGristKeyPassesOverAStoryWhoseDescriptionRunsGristSmokeOrNamesSmokeExamples(t *testing.T) {
	dispatch, tracker := aGristKeyFactory(t, false)
	if err := tracker.SetDescription("mw-gq6.1", "Run mw grist smoke lampas and read it."); err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetStoryMetadata(context.Background(), "mw-gq6.2", map[string]string{application.SmokeExamplesField: "lampas/talk/one"}); err != nil {
		t.Fatal(err)
	}
	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(startedIDs(report)) != 0 {
		t.Fatalf("started %v, want nothing", startedIDs(report))
	}
	for _, id := range []string{"mw-gq6.1", "mw-gq6.2"} {
		if passed := passedFor(report, id); passed == nil || passed.Gate != "no-grist-key" {
			t.Errorf("%s: passed %+v, want gate no-grist-key", id, report.Passed)
		}
	}
}

func TestDispatchOnAHostWithTheGristKeyClaimsAStoryWhoseAcceptanceRunsGristEval(t *testing.T) {
	dispatch, tracker := aGristKeyFactory(t, true)
	if err := tracker.SetAcceptance("mw-gq6.1", "1. `mw grist eval lampas` passes."); err != nil {
		t.Fatal(err)
	}
	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if ids := startedIDs(report); len(ids) != 1 || ids[0] != "mw-gq6.1" {
		t.Fatalf("started %v, want mw-gq6.1", ids)
	}
	if passed := passedFor(report, "mw-gq6.1"); passed != nil {
		t.Fatalf("passed over %+v on a host with the key", passed)
	}
}

func TestDispatchGristKeyLeavesAStoryNamingNeitherToBothHosts(t *testing.T) {
	for _, hasKey := range []bool{false, true} {
		dispatch, _ := aGristKeyFactory(t, hasKey)
		report, err := dispatch.Run(context.Background())
		if err != nil {
			t.Fatalf("key %v: dispatching: %v", hasKey, err)
		}
		if ids := startedIDs(report); len(ids) != 1 || ids[0] != "mw-gq6.1" {
			t.Fatalf("key %v: started %v, want mw-gq6.1", hasKey, ids)
		}
	}
}

func TestDispatchGristKeyHoldsAStoryPathedToThisHostToo(t *testing.T) {
	dispatch, tracker := aGristKeyFactory(t, false)
	if err := tracker.SetAcceptance("mw-gq6.1", "1. `mw grist eval lampas` passes."); err != nil {
		t.Fatal(err)
	}
	// The factory's epic names host vps, which is this host: not auto.
	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if passed := passedFor(report, "mw-gq6.1"); passed == nil || passed.Gate != "no-grist-key" {
		t.Fatalf("passed %+v, want the story pathed to this host passed over", report.Passed)
	}
}

func TestADispatchWithNoGristKeyReaderHoldsNothingBack(t *testing.T) {
	dispatch, tracker := aGristKeyFactory(t, false)
	dispatch.HasGristTestKey = nil
	if err := tracker.SetAcceptance("mw-gq6.1", "1. `mw grist eval lampas` passes."); err != nil {
		t.Fatal(err)
	}
	report, err := dispatch.Run(context.Background())
	if err != nil || len(startedIDs(report)) != 1 || startedIDs(report)[0] != "mw-gq6.1" {
		t.Fatalf("started %v, %v; want mw-gq6.1", startedIDs(report), err)
	}
}

func TestDispatchOnAHostWithNoGristKeyReadsTheFilesKeyForGrinds(t *testing.T) {
	for files, held := range map[string]bool{
		"grinds/word-list.json":                     true,
		"grinds/examples/x/y.json":                  true,
		"application/x.go,grinds/examples/*/*.json": true,
		"grinds/**":                  true,
		"grinds":                     true,
		"./grinds/word-list.json":    true,
		"application/x.go,docs/y.md": false,
		"docs/grinds/notes.md":       false,
		"grindstone/x.go":            false,
	} {
		dispatch, tracker := aGristKeyFactory(t, false)
		if err := tracker.SetStoryMetadata(context.Background(), "mw-gq6.1", map[string]string{"files": files}); err != nil {
			t.Fatal(err)
		}
		report, err := dispatch.Run(context.Background())
		if err != nil {
			t.Fatalf("files %q: dispatching: %v", files, err)
		}
		if passed := passedFor(report, "mw-gq6.1"); (passed != nil && passed.Gate == "no-grist-key") != held {
			t.Errorf("files %q: passed %+v, want held = %v", files, passed, held)
		}
	}
}

func TestDispatchGristKeyHoldsAStoryWhoseFilesKeyNamesGrindsOnAHostWithNoKey(t *testing.T) {
	for _, hasKey := range []bool{false, true} {
		dispatch, tracker := aGristKeyFactory(t, hasKey)
		if err := tracker.SetStoryMetadata(context.Background(), "mw-gq6.1", map[string]string{"files": "grinds/examples/word-list/x.json"}); err != nil {
			t.Fatal(err)
		}
		report, err := dispatch.Run(context.Background())
		if err != nil {
			t.Fatalf("key %v: dispatching: %v", hasKey, err)
		}
		passed := passedFor(report, "mw-gq6.1")
		if hasKey {
			if passed != nil {
				t.Fatalf("passed over %+v on a host with the key", passed)
			}
			continue
		}
		if passed == nil || passed.Gate != "no-grist-key" || !strings.Contains(passed.Why, "files key") {
			t.Fatalf("passed %+v, want mw-gq6.1 passed over with gate no-grist-key naming the files key", report.Passed)
		}
	}
}
