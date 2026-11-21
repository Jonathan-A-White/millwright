package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// hostUpkeepStateDir is the directory, under the home directory, whose marks say
// when a real sweep and a go-cache tidy last ran (mw doctor host-upkeep reads them).
var hostUpkeepStateDir = filepath.Join(".local", "state", "mw")

// recordUpkeepRun marks that the upkeep run named mark has finished now. A home
// that cannot be found or written is no reason to fail the run that did the work:
// the check says the mark is missing.
func recordUpkeepRun(mark string) {
	if home, err := os.UserHomeDir(); err == nil {
		_ = doctor.RecordHostUpkeepRun(filepath.Join(home, hostUpkeepStateDir), mark, time.Now())
	}
}

// hostUpkeepCheck is mw doctor's host-upkeep check for this host: its worktrees
// are the .mw-worktrees beside each rig, its stories are read from the tracker.
func hostUpkeepCheck(vault, host string, rigs map[string]string) *doctor.HostUpkeep {
	check := &doctor.HostUpkeep{Stories: mwGateway(vault, host, beads.WithRunCache())}
	if home, err := os.UserHomeDir(); err == nil {
		check.StateDir = filepath.Join(home, hostUpkeepStateDir)
	}
	seen := map[string]bool{}
	for _, rigDir := range rigs {
		parent := filepath.Join(filepath.Dir(filepath.Clean(rigDir)), application.WorktreesDir)
		if !seen[parent] {
			seen[parent] = true
			check.Worktrees = append(check.Worktrees, parent)
		}
	}
	return check
}
