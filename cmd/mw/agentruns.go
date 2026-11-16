package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/agentruns"
	"github.com/Jonathan-A-White/millwright/infrastructure/userunits"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// AgentRunsDir is where the follower marks its model runs under way, under
// SyncHaltStateDir in the home directory: this host's own, synced nowhere.
const AgentRunsDir = "agent-runs"

// hostAgentRuns is where this host marks the follower's model runs. A host with
// no home directory has none: nothing is marked and no restart waits.
func hostAgentRuns() application.AgentRunMarks {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	return agentruns.New(filepath.Join(home, SyncHaltStateDir, AgentRunsDir))
}

// hostUnits is the user manager a landing or a build restarts the factory's
// units through: it does not restart the follower while a model run of the
// follower's is under way, and waits for it to end for at most
// application.AgentRunWaitLimit, as `mw sleep pass --help` says (mw-gq6.1339).
func hostUnits() application.UnitRestarter {
	marks := hostAgentRuns()
	if marks == nil {
		return userunits.Systemctl{}
	}
	return &application.RunGuardedRestarter{
		Units: userunits.Systemctl{}, Runs: marks,
		Notice: func(said string) { fmt.Fprintln(os.Stderr, said) },
	}
}

// cutRunFiles is where a model run that was cut is written down: the vault's
// runs/cut, which mw fuel reads.
func cutRunFiles(files *vault.Vault) application.CutRunFiles { return files }
