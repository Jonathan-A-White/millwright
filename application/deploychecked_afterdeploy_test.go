package application

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-t5shinh.45: a run whose command passes the step the host names as the deploy
// and fails a later one sets the deployed mark (mw-gq6.866) and also gets the live
// check on the stories the deploy carried, each comment saying where the command
// failed. A run that fails at or before the deploy step writes nothing.

const afterDeployLine = "After-landing: failed after the deploy, at step 5: npm run shots"

type rangeLog struct{ landed LandedRange }

func (r rangeLog) LandedBetween(context.Context, string, string, string) (LandedRange, error) {
	return r.landed, nil
}

// afterDeployCheck is the live check of a rig whose site holds version 2.4.7.
func afterDeployCheck(tracker DeployTracker, rules domain.EpicRequirements) DeployCheck {
	return DeployCheck{
		Web:     &siteWeb{pages: map[string]string{lampasSite: `<meta content="2.4.7">`}},
		Rules:   siteRules{rules},
		Tracker: tracker,
		Host:    "cloud1",
		Now:     func() time.Time { return time.Date(2026, 11, 10, 15, 0, 0, 0, time.UTC) },
	}
}

func deployedCheckedOn(tracker *commentedStories, id string) []string {
	var found []string
	for _, c := range tracker.said[id] {
		if strings.HasPrefix(c, DeployedCheckedWord) {
			found = append(found, c)
		}
	}
	return found
}

func hasAfterLandingLine(comment string) bool { return strings.Contains(comment, "After-landing:") }

func TestALandingThatFailedAfterTheDeployStepWritesDeployedCheckedSayingSo(t *testing.T) {
	marks := markedBuilt{}
	tracker := &commentedStories{}
	rules := domain.EpicRequirements{SiteURL: lampasSite}
	n := checkedNext(&siteWeb{pages: map[string]string{lampasSite: `<meta content="2.4.7">`}}, rules, tracker)
	n.AfterLanding = &stepsDeploy{ran: shotsFailed(), step: 4}
	n.Deployed = marks
	c := &closeOut{id: "mw-1.1", target: "main", rigDir: "/rigs/lampas"}
	c.path.Rig = "lampas"
	report := &NextReport{Rig: Advanced{Moved: true}, Landed: true}
	report.How.Commit = "abc1234def"

	n.afterLanding(context.Background(), c, report)
	n.deployedChecked(context.Background(), "lampas", report, "abc1234def", "2.4.7", []liveStory{{id: "mw-1.1"}})

	found := deployedCheckedOn(tracker, "mw-1.1")
	if len(found) != 1 {
		t.Fatalf("expected one DEPLOYED-CHECKED on the landed story, got %q", tracker.said["mw-1.1"])
	}
	for _, want := range []string{afterDeployLine, "version 2.4.7 was found on the page"} {
		if !strings.Contains(found[0], want) {
			t.Errorf("expected %q in %q", want, found[0])
		}
	}
	if strings.Contains(strings.ToLower(found[0]), "verified") {
		t.Errorf("expected no VERIFIED in %q", found[0])
	}
	if marks["lampas.deployed"] != "abc1234def" {
		t.Errorf("expected the deployed mark still set, got %v", marks)
	}
}

func TestALandingThatFailedAtOrBeforeTheDeployStepWritesNoDeployedChecked(t *testing.T) {
	for _, step := range []int{3, 4} {
		tracker := &commentedStories{}
		web := &siteWeb{pages: map[string]string{lampasSite: `<meta content="2.4.7">`}}
		n := checkedNext(web, domain.EpicRequirements{SiteURL: lampasSite}, tracker)
		ran := shotsFailed()
		ran.Step = step
		n.AfterLanding = &stepsDeploy{ran: ran, step: 4}
		n.Deployed = markedBuilt{}
		c := &closeOut{id: "mw-1.1", target: "main", rigDir: "/rigs/lampas"}
		c.path.Rig = "lampas"
		report := &NextReport{Rig: Advanced{Moved: true}, Landed: true}
		report.How.Commit = "abc1234def"

		n.afterLanding(context.Background(), c, report)
		n.deployedChecked(context.Background(), "lampas", report, "abc1234def", "2.4.7", []liveStory{{id: "mw-1.1"}})

		if found := deployedCheckedOn(tracker, "mw-1.1"); len(found) != 0 {
			t.Errorf("step %d: expected no DEPLOYED-CHECKED, got %q", step, found)
		}
		if len(web.asked) != 0 {
			t.Errorf("step %d: expected the site not read, got %v", step, web.asked)
		}
	}
}

func TestAWholePassHasNoAfterLandingLine(t *testing.T) {
	tracker := &commentedStories{}
	n := checkedNext(&siteWeb{pages: map[string]string{lampasSite: `<meta content="2.4.7">`}}, domain.EpicRequirements{SiteURL: lampasSite}, tracker)

	n.deployedChecked(context.Background(), "lampas", passedReport(), "abc1234", "2.4.7", []liveStory{{id: "mw-1"}})

	found := deployedCheckedOn(tracker, "mw-1")
	if len(found) != 1 || hasAfterLandingLine(found[0]) {
		t.Errorf("expected one comment with no After-landing line, got %q", found)
	}
}

func byHandAfterDeploy(t *testing.T, ran Ran, marks markedBuilt, tracker *commentedStories) (AfterLandingRun, *bytes.Buffer) {
	t.Helper()
	out := &bytes.Buffer{}
	return AfterLandingRun{
		AfterLanding: &stepsDeploy{ran: ran, step: 4},
		Rigs:         map[string]string{"lampas": t.TempDir()},
		Host:         "cloud1",
		Built:        marks,
		Checkout:     fakeCheckoutHead{head: "abc1234def5678"},
		Landed:       rangeLog{LandedRange{Stories: []string{"mw-1", "mw-2"}, Version: "2.4.7"}},
		Live:         afterDeployCheck(tracker, domain.EpicRequirements{SiteURL: lampasSite}),
		Out:          out,
	}, out
}

func TestAfterLandingByHandThatFailedAfterTheDeployStepWritesDeployedCheckedOnEachStory(t *testing.T) {
	marks := markedBuilt{"lampas.deployed": "older"}
	tracker := &commentedStories{}
	run, _ := byHandAfterDeploy(t, shotsFailed(), marks, tracker)

	err := run.Run(context.Background(), "lampas")

	if err == nil {
		t.Fatal("expected the run to be an error: step 5 failed")
	}
	if marks["lampas.deployed"] != "abc1234def5678" {
		t.Errorf("expected the deployed mark set, got %v", marks)
	}
	for _, id := range []string{"mw-1", "mw-2"} {
		found := deployedCheckedOn(tracker, id)
		if len(found) != 1 {
			t.Fatalf("expected one DEPLOYED-CHECKED on %s, got %q", id, tracker.said[id])
		}
		for _, want := range []string{afterDeployLine, "version 2.4.7 was found on the page"} {
			if !strings.Contains(found[0], want) {
				t.Errorf("expected %q in %q", want, found[0])
			}
		}
	}
}

func TestAfterLandingByHandThatFailedAtOrBeforeTheDeployStepWritesNothing(t *testing.T) {
	for _, step := range []int{2, 4} {
		marks := markedBuilt{"lampas.deployed": "older"}
		tracker := &commentedStories{}
		ran := shotsFailed()
		ran.Step = step
		run, _ := byHandAfterDeploy(t, ran, marks, tracker)
		web := run.Live.Web.(*siteWeb)

		if err := run.Run(context.Background(), "lampas"); err == nil {
			t.Fatalf("step %d: expected an error", step)
		}

		for _, id := range []string{"mw-1", "mw-2"} {
			if found := deployedCheckedOn(tracker, id); len(found) != 0 {
				t.Errorf("step %d: expected no DEPLOYED-CHECKED on %s, got %q", step, id, found)
			}
		}
		if len(web.asked) != 0 || marks["lampas.deployed"] != "older" {
			t.Errorf("step %d: expected nothing read and the mark left, got %v, %v", step, web.asked, marks)
		}
	}
}

func TestAfterLandingByHandAfterTheDeployStepOfARigWithNoMarkWritesNoDeployedChecked(t *testing.T) {
	marks := markedBuilt{}
	tracker := &commentedStories{}
	run, out := byHandAfterDeploy(t, shotsFailed(), marks, tracker)

	if err := run.Run(context.Background(), "lampas"); err == nil {
		t.Fatal("expected an error")
	}

	if found := deployedCheckedOn(tracker, "mw-1"); len(found) != 0 {
		t.Errorf("expected none without a previous mark, got %q", found)
	}
	if !strings.Contains(out.String(), "no deployed mark") {
		t.Errorf("expected the output to say there was no deployed mark, got %q", out.String())
	}
}

func theHomeAfterDeploy(t *testing.T, ran Ran, tracker *commentedStories) (SelfUpdate, markedBuilt) {
	t.Helper()
	marks := markedBuilt{"lampas.deployed": "older"}
	return SelfUpdate{
		Checkout: fakeCheckoutHead{head: "abc1234def5678"},
		After:    &stepsDeploy{ran: ran, step: 4},
		Built:    marks,
		Rigs:     map[string]string{"lampas": t.TempDir()},
		Host:     "cloud1",
		Landed:   rangeLog{LandedRange{Stories: []string{"mw-1", "mw-2"}, Version: "2.4.7"}},
		Live:     afterDeployCheck(tracker, domain.EpicRequirements{SiteURL: lampasSite}),
	}, marks
}

func TestTheTickThatFailedAfterTheDeployStepWritesDeployedCheckedOnEachCarriedStory(t *testing.T) {
	tracker := &commentedStories{}
	s, marks := theHomeAfterDeploy(t, shotsFailed(), tracker)

	notes := strings.Join(s.deployRig(context.Background(), "lampas", s.Rigs["lampas"]), "\n")

	if !strings.Contains(notes, "the command failed after the deploy") {
		t.Errorf("expected the tick's note to say the command failed after the deploy, got %q", notes)
	}
	if marks["lampas.deployed"] != "abc1234def5678" || marks["lampas.deploy-failed"] != "" {
		t.Errorf("expected the mark set and no failed try, got %v", marks)
	}
	for _, id := range []string{"mw-1", "mw-2"} {
		found := deployedCheckedOn(tracker, id)
		if len(found) != 1 {
			t.Fatalf("expected one DEPLOYED-CHECKED on %s, got %q", id, tracker.said[id])
		}
		for _, want := range []string{afterDeployLine, "version 2.4.7 was found on the page"} {
			if !strings.Contains(found[0], want) {
				t.Errorf("expected %q in %q", want, found[0])
			}
		}
	}
}

func TestTheTickThatFailedAtTheDeployStepWritesNoDeployedChecked(t *testing.T) {
	tracker := &commentedStories{}
	ran := shotsFailed()
	ran.Step = 4
	s, _ := theHomeAfterDeploy(t, ran, tracker)

	s.deployRig(context.Background(), "lampas", s.Rigs["lampas"])

	for _, id := range []string{"mw-1", "mw-2"} {
		if found := deployedCheckedOn(tracker, id); len(found) != 0 {
			t.Errorf("expected no DEPLOYED-CHECKED on %s, got %q", id, found)
		}
	}
}
