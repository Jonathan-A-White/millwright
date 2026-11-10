package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-t5shinh.27: the home's tick deploying another host's landing, and the home's
// swap of a staged backend, write DEPLOYED-CHECKED by the rules a landing's own
// after-landing does, through the one DeployCheck.

// liveWeb answers each address from a table, counting what it was asked for.
type liveWeb struct {
	pages map[string]string
	asked map[string]int
}

func (l *liveWeb) Get(_ context.Context, url string) (int, []byte, error) {
	if l.asked == nil {
		l.asked = map[string]int{}
	}
	l.asked[url]++
	body, ok := l.pages[url]
	if !ok {
		return 404, nil, nil
	}
	return 200, []byte(body), nil
}

type liveRules struct{ rules domain.EpicRequirements }

func (r liveRules) EpicRequirements(context.Context, string) (domain.EpicRequirements, error) {
	return r.rules, nil
}

// landedLog says the same stories were landed whatever the range.
type landedLog struct {
	landed application.LandedRange
	from   string
	to     string
}

func (l *landedLog) LandedBetween(_ context.Context, _, from, to string) (application.LandedRange, error) {
	l.from, l.to = from, to
	return l.landed, nil
}

const (
	tickSite    = "https://lampas.example/"
	tickHealthz = "https://lampas.example/healthz"
)

func deployCheckAt(web application.VerifyWeb, rules domain.EpicRequirements, tracker application.DeployTracker) application.DeployCheck {
	return application.DeployCheck{
		Web: web, Rules: liveRules{rules}, Tracker: tracker, Host: "desktop",
		Now: func() time.Time { return time.Date(2026, 11, 10, 15, 0, 0, 0, time.UTC) },
	}
}

// aHomeDeployingLampas is the home with lampas one commit behind origin and a
// landing log that says mw-1 and mw-2 came in, raising version 2.4.7.
func aHomeDeployingLampas(t *testing.T, web *liveWeb, rules domain.EpicRequirements) (application.SelfUpdate, *rigCommands, *apptest.FakeTracker, *landedLog) {
	t.Helper()
	dir := t.TempDir()
	lampas := &fakeCheckout{head: "027f977aaaaaaaa", tip: "bfd385cbbbbbbbb"}
	after := &rigCommands{commands: map[string]string{"lampas": "make deploy"}}
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-e", domain.Path{})
	tracker.AddStory("mw-e", domain.Story{ID: "mw-1", Title: "one"})
	tracker.AddStory("mw-e", domain.Story{ID: "mw-2", Title: "two"})
	log := &landedLog{landed: application.LandedRange{Stories: []string{"mw-1", "mw-2"}, Version: "2.4.7"}}
	return application.SelfUpdate{
		Rigs:     map[string]string{"lampas": dir},
		Checkout: dirCheckouts{dir: lampas},
		After:    after,
		Built:    fakeBuilt{"lampas.deployed": "027f977aaaaaaaa"},
		Home:     &apptest.FakeHomeFile{Text: desktopHome},
		Host:     "desktop",
		Landed:   log,
		Live:     deployCheckAt(web, rules, tracker),
	}, after, tracker, log
}

func deployedChecks(comments []string) []string {
	var found []string
	for _, c := range comments {
		if strings.HasPrefix(c, application.DeployedCheckedWord) {
			found = append(found, c)
		}
	}
	return found
}

func TestTheTickWritesDeployedCheckedOnEachStoryItsDeployCarriedWhenTheVersionIsLive(t *testing.T) {
	web := &liveWeb{pages: map[string]string{tickSite: `<meta content="2.4.7">`, tickHealthz: "ok"}}
	update, after, tracker, log := aHomeDeployingLampas(t, web, domain.EpicRequirements{SiteURL: tickSite, HealthzURL: tickHealthz})

	notes := strings.Join(update.Run(context.Background()), "\n")

	if after.runs["lampas"] != 1 {
		t.Fatalf("expected the deploy run once, ran %d; notes %q", after.runs["lampas"], notes)
	}
	if log.from != "027f977aaaaaaaa" || log.to != "bfd385cbbbbbbbb" {
		t.Errorf("expected the stories read from the last deployed commit to the new head, got %s..%s", log.from, log.to)
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

func TestTheTickSaysTheVersionWasNotFoundWhenTheLivePageLacksIt(t *testing.T) {
	web := &liveWeb{pages: map[string]string{tickSite: `<meta content="2.4.6">`}}
	update, _, tracker, _ := aHomeDeployingLampas(t, web, domain.EpicRequirements{SiteURL: tickSite})

	update.Run(context.Background())

	found := deployedChecks(tracker.Comments("mw-1"))
	if len(found) != 1 || !strings.Contains(found[0], "version 2.4.7 was not found on the page") {
		t.Fatalf("expected a comment saying version 2.4.7 was not found, got %q", tracker.Comments("mw-1"))
	}
}

func TestTheTickWritesNothingWhenTheAfterLandingFailed(t *testing.T) {
	web := &liveWeb{pages: map[string]string{tickSite: `<meta content="2.4.7">`}}
	update, after, tracker, _ := aHomeDeployingLampas(t, web, domain.EpicRequirements{SiteURL: tickSite})
	after.ran = application.Ran{Status: 2, Output: "rsync: connection unexpectedly closed\n"}

	update.Run(context.Background())

	for _, id := range []string{"mw-1", "mw-2"} {
		if found := deployedChecks(tracker.Comments(id)); len(found) != 0 {
			t.Errorf("expected no DEPLOYED-CHECKED on %s after a failed deploy, got %q", id, found)
		}
	}
	if len(web.asked) != 0 {
		t.Errorf("expected the live site not read after a failed deploy, got %v", web.asked)
	}
}

func TestTheTickWritesNothingForARigThatNamesNoSiteOrHealthz(t *testing.T) {
	web := &liveWeb{pages: map[string]string{tickSite: `<meta content="2.4.7">`}}
	update, _, tracker, _ := aHomeDeployingLampas(t, web, domain.EpicRequirements{})

	update.Run(context.Background())

	if found := deployedChecks(tracker.Comments("mw-1")); len(found) != 0 || len(web.asked) != 0 {
		t.Errorf("expected nothing written and nothing read, got %q, asked %v", found, web.asked)
	}
}

// A swap of a backend: the staged commit is looked for in the healthz's answer.
func aSwappedBackend(t *testing.T, healthzAnswer string, exit int) (autoRig, string, *liveWeb) {
	t.Helper()
	output := swapAnswered
	if exit != 0 {
		output = "curl said no\n"
	}
	a := anAutoRig(t, application.HandsOutcome{Exit: exit, Output: output})
	web := &liveWeb{pages: map[string]string{"https://postern.example.org/api/healthz": healthzAnswer}}
	a.stage.Live = deployCheckAt(web, domain.EpicRequirements{HealthzURL: "https://postern.example.org/api/healthz"}, a.tracker)
	a.stage.Landed(context.Background(), presenceLanding())
	a.stage.Pending(context.Background())
	return a, "mw-j0f2d.28", web
}

func TestTheSwapWritesDeployedCheckedOnTheStoryItCarriedWhenHealthzHoldsTheCommit(t *testing.T) {
	a, story, _ := aSwappedBackend(t, `{"ok":true,"commit":"4c9db71"}`, 0)

	found := deployedChecks(a.tracker.Comments(story))
	if len(found) != 1 {
		t.Fatalf("expected one DEPLOYED-CHECKED on %s, got %q", story, a.tracker.Comments(story))
	}
	for _, want := range []string{"DEPLOYED-CHECKED by mw on desktop", "version 4c9db71 was found in the healthz answer"} {
		if !strings.Contains(found[0], want) {
			t.Errorf("expected %q in %q", want, found[0])
		}
	}
	if strings.Contains(strings.ToLower(found[0]), "verified") {
		t.Errorf("expected no VERIFIED in %q", found[0])
	}
}

func TestTheSwapSaysTheVersionWasNotFoundWhenHealthzHoldsAnotherCommit(t *testing.T) {
	a, story, _ := aSwappedBackend(t, `{"ok":true,"commit":"1234567"}`, 0)

	found := deployedChecks(a.tracker.Comments(story))
	if len(found) != 1 || !strings.Contains(found[0], "version 4c9db71 was not found in the healthz answer") {
		t.Fatalf("expected a comment saying the version was not found, got %q", a.tracker.Comments(story))
	}
}

func TestTheSwapWritesNothingWhenTheSwapFailed(t *testing.T) {
	a, story, web := aSwappedBackend(t, `{"ok":true,"commit":"4c9db71"}`, 1)

	if found := deployedChecks(a.tracker.Comments(story)); len(found) != 0 {
		t.Errorf("expected no DEPLOYED-CHECKED after a failed swap, got %q", found)
	}
	if len(web.asked) != 0 {
		t.Errorf("expected the live site not read after a failed swap, got %v", web.asked)
	}
}
