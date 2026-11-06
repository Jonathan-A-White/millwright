package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

func aFactoryUpdate() (application.SelfUpdate, *fakeCheckout, *fakeAfter, fakeBuilt) {
	checkout := &fakeCheckout{head: "027f977aaaaaaaa", tip: "bfd385cbbbbbbbb"}
	after := &fakeAfter{command: "make build"}
	built := fakeBuilt{"millwright.deployed": "027f977aaaaaaaa"}
	return application.SelfUpdate{
		Rigs: map[string]string{"millwright": "/rigs/millwright"}, Checkout: checkout, After: after, Built: built,
	}, checkout, after, built
}

// mw-gq6.1009: the self-update's passing build of the factory rig is a deploy on
// this host, so it moves the deployed mark mw verify reads.
func TestASelfUpdateThatBuildsTheFactoryRigMovesItsDeployedMark(t *testing.T) {
	update, _, _, built := aFactoryUpdate()

	update.Run(context.Background())

	if built["millwright"] != "bfd385cbbbbbbbb" {
		t.Errorf("expected the build remembered at the new head, got %v", built)
	}
	if built["millwright.deployed"] != "bfd385cbbbbbbbb" {
		t.Errorf("expected the deployed mark moved to the new head, got %q", built["millwright.deployed"])
	}
}

func TestASelfUpdateWhoseBuildFailsLeavesTheDeployedMarkWhereItWas(t *testing.T) {
	update, _, after, built := aFactoryUpdate()
	after.ran = application.Ran{Command: "make build", Status: 2, Output: "boom\n"}

	update.Run(context.Background())

	if built["millwright.deployed"] != "027f977aaaaaaaa" {
		t.Errorf("expected the deployed mark left at the old head, got %q", built["millwright.deployed"])
	}
}

func TestMwVerifyReadsDeployOKAfterTheSelfUpdateBuiltTheLanding(t *testing.T) {
	update, _, _, built := aFactoryUpdate()
	update.Run(context.Background())

	beads := &apptest.FakeVerifyBeads{
		Details: map[string]application.StoryDetail{"mw-1": {
			Story:       domain.Story{ID: "mw-1", Title: "a landing", Overrides: domain.Path{Rig: "millwright"}},
			Status:      "closed",
			CloseReason: "landed on main at bfd385cbbbbbbbb",
		}},
		Comments: map[string][]application.Comment{},
	}
	out := &strings.Builder{}
	_, err := application.VerifyDeploy{
		Beads: beads, Rules: apptest.NewFakeEpicRules(), Marks: deployMarks(built), Out: out,
		After: deployAfter{"millwright": "make build"}, History: deployHistory{},
	}.Run(context.Background(), "mw-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "deploy OK") || strings.Contains(out.String(), "MISMATCH") {
		t.Errorf("expected deploy OK, got:\n%s", out.String())
	}
}

func TestALevelFactoryRigBuiltAtHeadHasItsStaleDeployedMarkCaughtUp(t *testing.T) {
	update, checkout, after, built := aFactoryUpdate()
	checkout.head = checkout.tip
	built["millwright"] = checkout.tip

	update.Run(context.Background())

	if built["millwright.deployed"] != checkout.tip || after.runs != 0 {
		t.Errorf("expected the mark caught up with no build, got %q after %d builds", built["millwright.deployed"], after.runs)
	}
}
