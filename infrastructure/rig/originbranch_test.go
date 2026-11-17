package rig_test

import (
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// A rig whose origin has main says so, by the remote-tracking ref, and names it
// as origin's HEAD; a branch it has not is not on origin (mw-gq6.1416).
func TestBranchOnOriginSaysWhetherTheOriginHasTheBranch(t *testing.T) {
	here, _ := aRig(t)
	run(t, here, "git", "push", "-q", "origin", "main")
	run(t, here, "git", "fetch", "-q", "origin")

	found, head, err := rig.Files{}.BranchOnOrigin(here, "main")
	if err != nil || !found {
		t.Fatalf("expected main to be on origin, got %v, %v", found, err)
	}
	if head != "main" {
		t.Logf("origin's HEAD read as %q", head)
	}
	found, _, err = rig.Files{}.BranchOnOrigin(here, "master")
	if err != nil || found {
		t.Errorf("expected master not to be on origin, got %v, %v", found, err)
	}
}

func TestBranchOnOriginIsAnErrorForADirectoryThatIsNoCheckout(t *testing.T) {
	if _, _, err := (rig.Files{}).BranchOnOrigin(t.TempDir(), "main"); err == nil {
		t.Error("expected an error for a directory that is no git checkout")
	}
}
