package application

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// stepsDeploy is a rig's after-landing command of six steps whose run ended as
// ran says, with the host's knob for the step after which the rig is deployed.
type stepsDeploy struct {
	ran  Ran
	step int
}

func (s *stepsDeploy) Command(string) string                            { return s.ran.Command }
func (s *stepsDeploy) Run(context.Context, string, string) (Ran, error) { return s.ran, nil }
func (s *stepsDeploy) DeployStep(string) int                            { return s.step }

var sixSteps = []string{"npm ci", "npm run build", "npm test", "rsync dist/ host:/var/www/", "npm run shots", "npm run publish-shots"}

// shotsFailed is the run of mw-j0f2d.44: step 5 of 6 failed after the rsync of
// step 4 had put the new build live (mw-gq6.866).
func shotsFailed() Ran {
	return Ran{
		Command: "npm ci && npm run build && npm test && rsync dist/ host:/var/www/ && npm run shots && npm run publish-shots",
		Status:  1,
		Steps:   sixSteps,
		Step:    5,
		Output:  AfterLandingStepMark(5, 6, "npm run shots") + "\nflaky todo-board\n",
	}
}

type fakeCheckoutHead struct{ head string }

func (f fakeCheckoutHead) Fetch(context.Context, string) error { return nil }
func (f fakeCheckoutHead) Tip(context.Context, string, string, string) (string, error) {
	return f.head, nil
}
func (f fakeCheckoutHead) Head(context.Context, string) (string, error)          { return f.head, nil }
func (f fakeCheckoutHead) Uncommitted(context.Context, string) ([]string, error) { return nil, nil }
func (f fakeCheckoutHead) Advance(context.Context, string, string, string) (Advanced, error) {
	return Advanced{}, nil
}

func runByHand(t *testing.T, deploy *stepsDeploy, marks markedBuilt) (string, error) {
	t.Helper()
	dir := t.TempDir()
	var out bytes.Buffer
	err := AfterLandingRun{
		AfterLanding: deploy,
		Rigs:         map[string]string{"postern": dir},
		Host:         "laptop",
		Built:        marks,
		Checkout:     fakeCheckoutHead{head: "abc1234def5678"},
		Out:          &out,
	}.Run(context.Background(), "postern")
	return out.String(), err
}

func TestAfterLandingByHandMarksDeployedWhenTheDeployStepPassedAndALaterOneFailed(t *testing.T) {
	marks := markedBuilt{"postern.deploy-failed": "oldcommit 1"}
	_, err := runByHand(t, &stepsDeploy{ran: shotsFailed(), step: 4}, marks)

	if err == nil {
		t.Fatal("expected the after-landing to fail: step 5 of 6 failed")
	}
	want := "deployed at abc1234 (step 4 of 6); step 5 of 6 failed: npm run shots"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("expected the failure to say %q, got %q", want, err)
	}
	if marks["postern.deployed"] != "abc1234def5678" {
		t.Errorf("expected the deployed mark at the landed commit, got %v", marks)
	}
	if marks["postern.deploy-failed"] != "" {
		t.Errorf("expected the deploy-failed mark cleared, got %v", marks)
	}
}

func TestAfterLandingByHandWithNoKnobMarksOnlyWhenTheWholeCommandPassed(t *testing.T) {
	marks := markedBuilt{}
	_, err := runByHand(t, &stepsDeploy{ran: shotsFailed()}, marks)
	if err == nil || strings.Contains(err.Error(), "deployed at") {
		t.Errorf("expected today's failure line, got %v", err)
	}
	if marks["postern.deployed"] != "" {
		t.Errorf("expected no deployed mark, got %v", marks)
	}

	ok := shotsFailed()
	ok.Status, ok.Step = 0, 6
	if _, err := runByHand(t, &stepsDeploy{ran: ok}, marks); err != nil {
		t.Fatalf("expected the whole command to pass: %v", err)
	}
	if marks["postern.deployed"] != "abc1234def5678" {
		t.Errorf("expected the mark at the end of a passing command, got %v", marks)
	}
}

func TestAfterLandingByHandDoesNotMarkWhenTheDeployStepItselfFailed(t *testing.T) {
	marks := markedBuilt{}
	ran := shotsFailed()
	ran.Step = 4
	_, err := runByHand(t, &stepsDeploy{ran: ran, step: 4}, marks)
	if err == nil || strings.Contains(err.Error(), "deployed at") {
		t.Errorf("expected the failure to be today's, with no deploy said, got %v", err)
	}
	if marks["postern.deployed"] != "" {
		t.Errorf("expected no deployed mark, got %v", marks)
	}
}

func TestALandingMarksDeployedWhenTheDeployStepPassedAndALaterOneFailed(t *testing.T) {
	marks := markedBuilt{"postern.deploy-failed": "abc1234def 1"}
	deploy := &stepsDeploy{ran: shotsFailed(), step: 4}
	n := Next{AfterLanding: deploy, Tracker: &smokeComments{}, Host: "laptop", Deployed: marks}
	c := &closeOut{id: "mw-1.1", target: "main", rigDir: "/rigs/postern"}
	c.path.Rig = "postern"
	report := &NextReport{Rig: Advanced{Moved: true}, Landed: true}
	report.How.Commit = "abc1234def"

	n.afterLanding(context.Background(), c, report)

	if marks["postern.deployed"] != "abc1234def" || marks["postern.deploy-failed"] != "" {
		t.Errorf("expected the deployed mark at the landed commit and no deploy-failed mark, got %v", marks)
	}
	said := strings.Join(report.Notes, "\n")
	if !strings.Contains(said, "deployed at abc1234 (step 4 of 6); step 5 of 6 failed: npm run shots") {
		t.Errorf("expected the report to say the deploy stood, got:\n%s", said)
	}
}

func TestALandingWithNoKnobLeavesAFailedCommandUnmarked(t *testing.T) {
	marks := markedBuilt{}
	n := Next{AfterLanding: &stepsDeploy{ran: shotsFailed()}, Tracker: &smokeComments{}, Host: "laptop", Deployed: marks}
	c := &closeOut{id: "mw-1.1", target: "main", rigDir: "/rigs/postern"}
	c.path.Rig = "postern"
	report := &NextReport{Rig: Advanced{Moved: true}, Landed: true}
	report.How.Commit = "abc1234def"

	n.afterLanding(context.Background(), c, report)

	if marks["postern.deployed"] != "" {
		t.Errorf("expected no deployed mark, got %v", marks)
	}
}

func TestTheHomesDeployThatFailedAfterItsDeployStepIsNotCountedAsAFailedDeploy(t *testing.T) {
	// The home's tick must not write a deploy-failed try over the deployed mark
	// the run just set: the commit is live (mw-gq6.866).
	marks := markedBuilt{}
	deploy := &stepsDeploy{ran: shotsFailed(), step: 4}
	s := SelfUpdate{
		Checkout: fakeCheckoutHead{head: "abc1234def5678"},
		After:    deploy,
		Built:    marks,
		Rigs:     map[string]string{"postern": t.TempDir()},
	}
	marks["postern.deployed"] = "older"

	notes := s.deployRig(context.Background(), "postern", s.Rigs["postern"])

	if marks["postern.deployed"] != "abc1234def5678" {
		t.Errorf("expected the deployed mark at head, got %v (%v)", marks, notes)
	}
	if marks["postern.deploy-failed"] != "" {
		t.Errorf("expected no deploy-failed try, got %v", marks)
	}
}

func TestTheHomesDeployWhoseDeployStepItselfFailedIsCountedAsAFailedDeploy(t *testing.T) {
	marks := markedBuilt{"postern.deployed": "older"}
	ran := shotsFailed()
	ran.Step = 4
	s := SelfUpdate{
		Checkout: fakeCheckoutHead{head: "abc1234def5678"},
		After:    &stepsDeploy{ran: ran, step: 4},
		Built:    marks,
		Rigs:     map[string]string{"postern": t.TempDir()},
	}

	s.deployRig(context.Background(), "postern", s.Rigs["postern"])

	if marks["postern.deployed"] != "older" || marks["postern.deploy-failed"] != "abc1234def5678 1" {
		t.Errorf("expected the old deployed mark and one failed try, got %v", marks)
	}
}
