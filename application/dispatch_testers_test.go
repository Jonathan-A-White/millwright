package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// aTesterOn files a Tester story of rig under its own epic on host, running
// when claimed and ready otherwise.
func aTesterOn(t *testing.T, tracker *apptest.FakeTracker, id, rig, host string, running bool) {
	t.Helper()
	epic := "mw-" + strings.ReplaceAll(id, ".", "-")
	tracker.AddEpic(epic, domain.Path{Rig: rig, Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Formula: "tdd-feature", Host: host})
	tracker.AddStory(epic, domain.Story{ID: id, Title: "Test the landed work"})
	if err := tracker.SetLabels(id, application.LabelTester); err != nil {
		t.Fatalf("labelling %s: %v", id, err)
	}
	if err := tracker.SetDescription(id, application.TesterLandedLine+"mw-landed.1 (landed on main)"); err != nil {
		t.Fatalf("describing %s: %v", id, err)
	}
	if running {
		if err := tracker.ClaimStory(context.Background(), id); err != nil {
			t.Fatalf("claiming %s: %v", id, err)
		}
	}
}

func passedWhy(report application.DispatchReport, id string) string {
	for _, p := range report.Passed {
		if p.StoryID == id {
			return p.Why
		}
	}
	return ""
}

// mw-gq6.546: testers_per_rig is enforced by dispatch, on any host, from the
// running list of every host.
func TestADispatchHoldsAReadyTesterWhileItsRigRunsTheLimitOnAnotherHost(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	aTesterOn(t, tracker, "mw-t.1", "millwright", "cloud2", true)
	aTesterOn(t, tracker, "mw-t.2", "millwright", "vps", false)

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if got := startedIDs(report); len(got) != 0 {
		t.Errorf("expected no Tester started while millwright runs one, started %v", got)
	}
	want := "waiting: millwright already runs 1 Tester stories (testers_per_rig 1)"
	if why := passedWhy(report, "mw-t.2"); why != want {
		t.Errorf("expected %q, got %q in %+v", want, why, report.Passed)
	}
}

func TestADispatchStartsAReadyTesterOfAnotherRigThanTheOneRunning(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	dispatch.Rigs["lampas"] = "/rigs/lampas"
	aTesterOn(t, tracker, "mw-t.1", "millwright", "cloud2", true)
	aTesterOn(t, tracker, "mw-t.2", "lampas", "vps", false)

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if got := startedIDs(report); len(got) != 1 || got[0] != "mw-t.2" {
		t.Errorf("expected the lampas Tester started, got %v (passed %+v)", got, report.Passed)
	}
}

func TestADispatchStartsOnlyOneTesterOfARigInAPass(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	dispatch.Cap = 3
	aTesterOn(t, tracker, "mw-t.1", "millwright", "vps", false)
	aTesterOn(t, tracker, "mw-t.2", "millwright", "vps", false)

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if got := startedIDs(report); len(got) != 1 {
		t.Errorf("expected one Tester started at a limit of 1, got %v", got)
	}
}

func TestATestersPerRigOfThreeLetsThreeRun(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	dispatch.Cap = 4
	dispatch.TestersPerRig = 3
	aTesterOn(t, tracker, "mw-t.1", "millwright", "cloud2", true)
	aTesterOn(t, tracker, "mw-t.2", "millwright", "cloud3", true)
	aTesterOn(t, tracker, "mw-t.3", "millwright", "vps", false)
	aTesterOn(t, tracker, "mw-t.4", "millwright", "vps", false)

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if got := startedIDs(report); len(got) != 1 || got[0] != "mw-t.3" {
		t.Errorf("expected exactly the first ready Tester started at a limit of 3 with 2 running, got %v", got)
	}
	want := "waiting: millwright already runs 3 Tester stories (testers_per_rig 3)"
	if why := passedWhy(report, "mw-t.4"); why != want {
		t.Errorf("expected %q, got %q", want, why)
	}
}

// mw-gq6.546: mw status says what dispatch will say about a held Tester.
func TestStatusSaysUnderAReadyTesterThatItsRigRunsTheLimit(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	aTesterOn(t, tracker, "mw-t.1", "millwright", "cloud2", true)
	aTesterOn(t, tracker, "mw-t.2", "millwright", "vps", false)
	aTesterOn(t, tracker, "mw-t.3", "lampas", "vps", false)

	// The line is wrapped to the report's width, so it is read as words.
	out := strings.Join(strings.Fields(otherHostStatus(t, tracker).String()), " ")

	held := "waiting: millwright already runs 1 Tester stories (testers_per_rig 1)"
	if strings.Count(out, held) != 1 {
		t.Fatalf("expected the line once, for mw-t.2 only, in:\n%s", out)
	}
	at := strings.Index(out, "mw-t.2")
	if at < 0 || strings.Index(out, held) < at || strings.Index(out, held) > at+300 {
		t.Errorf("expected the line under mw-t.2 in READY:\n%s", out)
	}
}
