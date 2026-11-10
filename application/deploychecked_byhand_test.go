package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-t5shinh.35: `mw after-landing <rig>` run by hand writes DEPLOYED-CHECKED on
// the stories landed since the rig's previous deployed mark, through the one
// DeployCheck.Write, by the rules the tick and a landing's own after-landing keep.

// aByHandDeploy is lampas at a new commit with a deployed mark one commit back,
// and a landing log that says mw-1 and mw-2 came in, raising version 2.4.7.
func aByHandDeploy(t *testing.T, web *liveWeb, rules domain.EpicRequirements, ran application.Ran) (application.AfterLandingRun, fakeBuilt, *apptest.FakeTracker, *landedLog, *strings.Builder) {
	t.Helper()
	dir := t.TempDir()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-e", domain.Path{})
	tracker.AddStory("mw-e", domain.Story{ID: "mw-1", Title: "one"})
	tracker.AddStory("mw-e", domain.Story{ID: "mw-2", Title: "two"})
	log := &landedLog{landed: application.LandedRange{Stories: []string{"mw-1", "mw-2"}, Version: "2.4.7"}}
	built := fakeBuilt{"lampas.deployed": "027f977aaaaaaaa"}
	out := &strings.Builder{}
	return application.AfterLandingRun{
		AfterLanding: &rigCommands{commands: map[string]string{"lampas": "make deploy"}, ran: ran},
		Rigs:         map[string]string{"lampas": dir},
		Host:         "desktop",
		Checkout:     dirCheckouts{dir: &fakeCheckout{head: "bfd385cbbbbbbbb"}},
		Built:        built,
		Landed:       log,
		Live:         deployCheckAt(web, rules, tracker),
		Out:          out,
	}, built, tracker, log, out
}

func TestAfterLandingByHandWritesDeployedCheckedOnEachStoryItDeployedWhenTheVersionIsLive(t *testing.T) {
	web := &liveWeb{pages: map[string]string{tickSite: `<meta content="2.4.7">`, tickHealthz: "ok"}}
	run, built, tracker, log, _ := aByHandDeploy(t, web, domain.EpicRequirements{SiteURL: tickSite, HealthzURL: tickHealthz}, application.Ran{})

	if err := run.Run(context.Background(), "lampas"); err != nil {
		t.Fatalf("expected the run to pass, got %v", err)
	}

	if log.from != "027f977aaaaaaaa" || log.to != "bfd385cbbbbbbbb" {
		t.Errorf("expected the stories read from the previous deployed mark to the new head, got %s..%s", log.from, log.to)
	}
	if built["lampas.deployed"] != "bfd385cbbbbbbbb" {
		t.Errorf("expected the deployed mark moved, got %q", built["lampas.deployed"])
	}
	if web.asked[tickSite] != 1 {
		t.Errorf("expected the page read once for both stories, got %d", web.asked[tickSite])
	}
	for _, id := range []string{"mw-1", "mw-2"} {
		found := deployedChecks(tracker.Comments(id))
		if len(found) != 1 {
			t.Fatalf("expected one DEPLOYED-CHECKED on %s, got %q", id, tracker.Comments(id))
		}
		for _, want := range []string{"DEPLOYED-CHECKED by mw on desktop", "commit bfd385cbbbbb", "version 2.4.7 was found on the page"} {
			if !strings.Contains(found[0], want) {
				t.Errorf("expected %q in %q", want, found[0])
			}
		}
		if strings.Contains(strings.ToLower(found[0]), "verified") {
			t.Errorf("expected no VERIFIED in %q", found[0])
		}
	}
}

func TestAfterLandingByHandSaysTheVersionWasNotFoundWhenTheLivePageLacksIt(t *testing.T) {
	web := &liveWeb{pages: map[string]string{tickSite: `<meta content="2.4.6">`}}
	run, _, tracker, _, _ := aByHandDeploy(t, web, domain.EpicRequirements{SiteURL: tickSite}, application.Ran{})

	if err := run.Run(context.Background(), "lampas"); err != nil {
		t.Fatalf("expected the run to pass, got %v", err)
	}

	found := deployedChecks(tracker.Comments("mw-1"))
	if len(found) != 1 || !strings.Contains(found[0], "version 2.4.7 was not found on the page") {
		t.Fatalf("expected a comment saying version 2.4.7 was not found, got %q", tracker.Comments("mw-1"))
	}
}

func TestAfterLandingByHandThatFailedWritesNothingAndLeavesTheMark(t *testing.T) {
	web := &liveWeb{pages: map[string]string{tickSite: `<meta content="2.4.7">`}}
	run, built, tracker, _, _ := aByHandDeploy(t, web, domain.EpicRequirements{SiteURL: tickSite}, application.Ran{Status: 2, Output: "rsync: connection unexpectedly closed\n"})

	if err := run.Run(context.Background(), "lampas"); err == nil {
		t.Fatal("expected a failing command to be an error")
	}

	for _, id := range []string{"mw-1", "mw-2"} {
		if found := deployedChecks(tracker.Comments(id)); len(found) != 0 {
			t.Errorf("expected no DEPLOYED-CHECKED on %s after a failed run, got %q", id, found)
		}
	}
	if len(web.asked) != 0 {
		t.Errorf("expected the live site not read after a failed run, got %v", web.asked)
	}
	if built["lampas.deployed"] != "027f977aaaaaaaa" {
		t.Errorf("expected the deployed mark left where it was, got %q", built["lampas.deployed"])
	}
}

func TestAfterLandingByHandWritesNothingForARigThatNamesNoSiteOrHealthz(t *testing.T) {
	web := &liveWeb{pages: map[string]string{tickSite: `<meta content="2.4.7">`}}
	run, _, tracker, _, _ := aByHandDeploy(t, web, domain.EpicRequirements{}, application.Ran{})

	if err := run.Run(context.Background(), "lampas"); err != nil {
		t.Fatalf("expected the run to pass, got %v", err)
	}

	if found := deployedChecks(tracker.Comments("mw-1")); len(found) != 0 || len(web.asked) != 0 {
		t.Errorf("expected nothing written and nothing read, got %q, asked %v", found, web.asked)
	}
}

func TestAfterLandingByHandOfARigWithNoDeployedMarkSaysItKnowsNoStories(t *testing.T) {
	web := &liveWeb{pages: map[string]string{tickSite: `<meta content="2.4.7">`}}
	run, built, tracker, _, out := aByHandDeploy(t, web, domain.EpicRequirements{SiteURL: tickSite}, application.Ran{})
	delete(built, "lampas.deployed")

	if err := run.Run(context.Background(), "lampas"); err != nil {
		t.Fatalf("expected the run to pass, got %v", err)
	}

	if found := deployedChecks(tracker.Comments("mw-1")); len(found) != 0 {
		t.Errorf("expected no DEPLOYED-CHECKED without a previous mark, got %q", found)
	}
	if !strings.Contains(out.String(), "no deployed mark") {
		t.Errorf("expected the output to say there was no deployed mark, got %q", out.String())
	}
}
