package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-gq6.564: the home deploys a rig whose checkout HEAD differs from the commit
// its last passing deploy ran at, not only one that moved on this tick.
func TestTheHomeDeploysALevelCheckoutWhoseHeadWasNeverDeployed(t *testing.T) {
	update, postern, after, built := aHomeWithPostern(t, "desktop")
	postern.head = postern.tip // level, as a hand fast-forward leaves it
	built["postern.deployed"] = "027f977aaaaaaaa"

	notes := strings.Join(update.Run(context.Background()), "\n")
	if after.runs["postern"] != 1 {
		t.Fatalf("expected the level, undeployed checkout deployed once, ran %d; notes %q", after.runs["postern"], notes)
	}
	if !strings.Contains(notes, "self-update: postern") || !strings.Contains(notes, "deployed by the home") {
		t.Errorf("expected the tick to say it deployed postern, got %q", notes)
	}
	if built["postern.deployed"] != "bfd385cbbbbbbbb" {
		t.Errorf("expected the deployed mark set to the head, got %q", built["postern.deployed"])
	}

	if again := update.Run(context.Background()); len(again) != 0 || after.runs["postern"] != 1 {
		t.Errorf("expected a second tick to run nothing and say nothing, got %q after %d runs", again, after.runs["postern"])
	}
}

// A rig the home has no mark for is not deployed on a first look: a deploy the
// Governor holds or runs by hand is his.
func TestTheFirstLookAtALevelRigSetsTheDeployedMarkWithoutDeploying(t *testing.T) {
	update, postern, after, built := aHomeWithPostern(t, "desktop")
	postern.head = postern.tip

	notes := strings.Join(update.Run(context.Background()), "\n")
	if after.runs["postern"] != 0 {
		t.Fatalf("expected nothing deployed on the first look, ran %d", after.runs["postern"])
	}
	if built["postern.deployed"] != "bfd385cbbbbbbbb" {
		t.Errorf("expected the mark set to the head, got %q", built["postern.deployed"])
	}
	for _, want := range []string{"self-update: postern", "without deploying", "mw after-landing postern"} {
		if !strings.Contains(notes, want) {
			t.Errorf("expected %q in %q", want, notes)
		}
	}
	if again := update.Run(context.Background()); len(again) != 0 {
		t.Errorf("expected the first-look note said once, got %q", again)
	}
}

func TestADeployThatFailsIsTriedOnceMoreByTheNextTickThenLeftUntilHeadMoves(t *testing.T) {
	update, postern, after, built := aHomeWithPostern(t, "desktop")
	after.ran = application.Ran{Status: 2, Output: "npm: ERR\n"}

	first := strings.Join(update.Run(context.Background()), "\n")
	if after.runs["postern"] != 1 || !strings.Contains(first, "deploy failed") || !strings.Contains(first, "tries once more") {
		t.Fatalf("expected a failed deploy said with a retry promised, got %q after %d runs", first, after.runs["postern"])
	}

	second := strings.Join(update.Run(context.Background()), "\n")
	if after.runs["postern"] != 2 || !strings.Contains(second, "failed again") || !strings.Contains(second, "mw after-landing postern") || !strings.Contains(second, "until the checkout moves") {
		t.Fatalf("expected one retry said failed again, got %q after %d runs", second, after.runs["postern"])
	}

	if third := update.Run(context.Background()); len(third) != 0 || after.runs["postern"] != 2 {
		t.Fatalf("expected no third try and nothing said, got %q after %d runs", third, after.runs["postern"])
	}

	// HEAD moves: a fresh try, and it passes.
	postern.tip = "cafe000dddddddd"
	after.ran = application.Ran{}
	notes := strings.Join(update.Run(context.Background()), "\n")
	if after.runs["postern"] != 3 || !strings.Contains(notes, "deployed by the home") {
		t.Errorf("expected a deploy when HEAD moved, got %q after %d runs", notes, after.runs["postern"])
	}
	if built["postern.deployed"] != "cafe000dddddddd" {
		t.Errorf("expected the mark set, got %q", built["postern.deployed"])
	}
}

// mw-gq6.564: `mw after-landing <rig>` that passes sets the rig's deployed mark.
func TestAfterLandingRunThatPassesSetsTheDeployedMark(t *testing.T) {
	dir := t.TempDir()
	built := fakeBuilt{"postern.deploy-failed": "bfd385cbbbbbbbb 2"}
	run := func(status int) error {
		return application.AfterLandingRun{
			AfterLanding: &rigCommands{commands: map[string]string{"postern": "make deploy"}, ran: application.Ran{Status: status}},
			Rigs:         map[string]string{"postern": dir},
			Checkout:     dirCheckouts{dir: &fakeCheckout{head: "bfd385cbbbbbbbb"}},
			Built:        built,
		}.Run(context.Background(), "postern")
	}

	if err := run(1); err == nil {
		t.Fatal("expected a failing command to be an error")
	}
	if built["postern.deployed"] != "" {
		t.Errorf("expected no mark after a failed deploy, got %q", built["postern.deployed"])
	}
	if err := run(0); err != nil {
		t.Fatalf("expected the passing run to pass, got %v", err)
	}
	if built["postern.deployed"] != "bfd385cbbbbbbbb" || built["postern.deploy-failed"] != "" {
		t.Errorf("expected the mark set and the failure forgotten, got %v", built)
	}
}
