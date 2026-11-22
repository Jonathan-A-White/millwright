package main

import (
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// mw-gq6.1625: the host-upkeep check reads the stories of the host's worktrees
// by id; handed the gateway itself it read every bead for them under the run cache.
func TestHostUpkeepReadsItsStoriesWithoutTheListOfEveryBead(t *testing.T) {
	check := hostUpkeepCheck(beads.New(t.TempDir(), beads.WithRunCache()), nil)
	if _, ok := check.Stories.(fewBeads); !ok {
		t.Errorf("expected the check to read its stories through fewBeads, got %T", check.Stories)
	}
}
