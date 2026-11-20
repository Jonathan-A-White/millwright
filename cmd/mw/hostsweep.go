package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostsweep"
	"github.com/Jonathan-A-White/millwright/infrastructure/ticklog"

	"github.com/spf13/cobra"
)

// hostSweepListedMark is the file, under the home directory, whose presence says
// the timer has already listed what the sweep would remove on this host once
// and changed nothing: its later runs act.
var hostSweepListedMark = filepath.Join(".local", "state", "mw", "host-sweep-listed")

// hostSweepDry says whether a run lists only: it was asked to (--dry-run), or
// it is the timer's first run on this host, when no mark of an earlier listing
// is kept. first is true for that second case, so the caller keeps the mark.
func hostSweepDry(markPath string, dryRun, fromTimer bool) (dry, first bool) {
	if dryRun {
		return true, false
	}
	if fromTimer {
		if _, err := os.Stat(markPath); os.IsNotExist(err) {
			return true, true
		}
	}
	return false, false
}

// newHostSweepCmd builds `mw host sweep`: free the disk of what no live story
// can need, within the table of docs/host-sweep.md.
func newHostSweepCmd() *cobra.Command {
	var dryRun, fromTimer bool
	cmd := &cobra.Command{
		Use:   "sweep [--dry-run]",
		Short: "Free this host's disk of what no live story can need: closed stories' worktrees and branches, old landings, runs and set-aside beads",
		Long: "sweep acts on five things and nothing else (docs/host-sweep.md is the table):\n" +
			"  1. the worktree and branch of a story that is closed, not held by a claim or a retry;\n" +
			"  2. a .mw-landing-* directory over a day old with no live landing in it;\n" +
			"  3. a directory of the vault's runs/ older than host_sweep_runs_days (14);\n" +
			"  4. node_modules and Playwright browser caches in a closed story's worktree;\n" +
			"  5. a beads-*-aside-* directory older than host_sweep_aside_days (7).\n\n" +
			"It never touches a worktree with an uncommitted change, a branch with a commit that is neither\n" +
			"on main nor pushed, a story that is not closed, or a worktree a process is running in; those\n" +
			"are listed as kept, with why. --dry-run lists each path, its size and the reason, and changes\n" +
			"nothing. A real run writes one line a path to the Millhand's tick log. With --timer (what\n" +
			"mw-host-sweep.service runs) the first run on a host is a dry run.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			vault, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			rigs, err := config.Rigs()
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("there is no home directory to sweep: %w", err)
			}
			runsDays, err := config.HostSweepRunsDays(int(application.HostSweepRunsAge / application.HostSweepDay))
			if err != nil {
				return err
			}
			asideDays, err := config.HostSweepAsideDays(int(application.HostSweepAsideAge / application.HostSweepDay))
			if err != nil {
				return err
			}

			// The sweep reads its stories' states from the tracker, and a read
			// started a bd, several new connections to the beads server each
			// run of the timer (mw-gq6.1546): one connection kept open serves
			// them, and the run's cache keeps its read to a list of every bead.
			defer keepBeadsConnection(vault, cmd.ErrOrStderr())()

			mark := filepath.Join(home, hostSweepListedMark)
			dry, first := hostSweepDry(mark, dryRun, fromTimer)
			if first {
				fmt.Fprintln(cmd.OutOrStdout(), "host sweep: the first run on this host only lists; the next one removes")
			}
			sweep := application.HostSweep{
				Files:    hostsweep.Disk{Rigs: rigs, Vault: vault, Home: home},
				Stories:  mwGateway(vault, host, beads.WithRunCache()),
				RunsAge:  time.Duration(runsDays) * application.HostSweepDay,
				AsideAge: time.Duration(asideDays) * application.HostSweepDay,
				DryRun:   dry,
				Out:      cmd.OutOrStdout(),
			}
			// A rehearsal is not a run: it leaves nothing in the log.
			if !dry {
				sweep.Log = ticklog.New(filepath.Join(home, MillhandTickStateDir))
			}
			_, runErr := sweep.Run(cmd.Context())
			if first && runErr == nil {
				if err := os.MkdirAll(filepath.Dir(mark), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(mark, []byte("listed\n"), 0o644); err != nil {
					return err
				}
			}
			return runErr
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list each path, its size and the reason, and change nothing")
	cmd.Flags().BoolVar(&fromTimer, "timer", false, "what the timer's unit runs: the first run on a host only lists")
	return cmd
}
