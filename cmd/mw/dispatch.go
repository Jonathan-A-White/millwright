package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostlock"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/synchalt"
	"github.com/Jonathan-A-White/millwright/infrastructure/ticklog"
	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/spf13/cobra"
)

// BuilderSeat is the seat a dispatched session boots into. Every story is
// worked by the Builder; the model and the effort come from the story's path,
// not from the seat.
const BuilderSeat = "builder"

// mwGateway is the beads gateway every command uses: the one in the configured
// vault, acting under mw's own name on this host. mw names itself on every bd
// call rather than leaving it to $BEADS_ACTOR, because a claim made under the
// dispatcher's shell and a close attempted from inside a Builder session are
// the same act by the same program, and bd lets only the actor that claimed a
// story close it.
func mwGateway(dir, host string) *beads.Gateway {
	return beads.New(dir, beads.WithActor(application.SeatIdentity(application.MwSeat, host)))
}

// mwVault is the vault's files as every command uses them: the clone in the
// configured directory, committing under the same name mwGateway writes to the
// tracker under, so that the tracker's history and git's tell the same story.
func mwVault(dir, host string) *vault.Vault {
	return vault.New(dir, vault.WithAuthor(application.SeatIdentity(application.MwSeat, host)))
}

// DispatchStateDir is where mw dispatch keeps its log, under the home
// directory, beside the Millhand tick's. It is this host's own: nothing in it is
// synced anywhere, though the counts of what is in it are.
var DispatchStateDir = filepath.Join(".local", "state", "mw-dispatch")

// SyncHaltStateDir is where this host marks its own sync halted, under the
// home directory: shared between mw dispatch and mw millhand tick, and read
// straight back by mw status here. Nothing in it is synced anywhere.
var SyncHaltStateDir = filepath.Join(".local", "state", "mw")

// hostSyncHalt is this host's own mark of a halted sync, kept in
// SyncHaltStateDir. A host with no home directory keeps none.
func hostSyncHalt() application.SyncHaltMarker {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return synchalt.New(filepath.Join(home, SyncHaltStateDir))
}

// SyncLockStateDir is where this host's sync lock lives, under the home
// directory, beside the sync-halted mark: shared by every command that syncs
// — mw dispatch, mw millhand tick, mw next and mw sync itself — so that two of
// them never run the beads half at once.
var SyncLockStateDir = filepath.Join(".local", "state", "mw")

// hostSyncLock is this host's own sync lock, kept in SyncLockStateDir. A host
// with no home directory keeps none, and syncs unlocked.
func hostSyncLock() application.HostLock {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return hostlock.New(filepath.Join(home, SyncLockStateDir))
}

// hostTickLogs are the logs this host's timers keep, in the home directory. A
// host with no home directory has none.
func hostTickLogs() application.TickLogs {
	home, err := os.UserHomeDir()
	if err != nil {
		return application.TickLogs{}
	}
	return application.TickLogs{
		Dispatch: ticklog.New(filepath.Join(home, DispatchStateDir)),
		Millhand: ticklog.New(filepath.Join(home, MillhandTickStateDir)),
	}
}

// newDispatchCmd builds `mw dispatch`: the command that turns ready stories
// into running sessions. It is the one command in the factory that spends fuel,
// so everything it does before spending any is reversible, and --dry-run does
// none of it.
func newDispatchCmd() *cobra.Command {
	var dryRun bool
	var capOverride int

	cmd := &cobra.Command{
		Use:   "dispatch",
		Short: "Start a fresh Builder session for each story ready on this host",
		Long: "dispatch brings this host level with the other one, asks beads what is ready here, and for\n" +
			"each story it may take: claims it, cuts a worktree of its rig on branch mw/<story> from the\n" +
			"freshly fetched target branch, pours its formula into step beads, writes the boot file, and\n" +
			"starts the session. It never takes more than the cap allows, never takes a story whose path\n" +
			"names another host or no host at all, and gives the claim back if anything fails before the\n" +
			"session starts.\n\n" +
			"A story is started at most max_attempts times in all (3 unless the config file says\n" +
			"otherwise). One that has been started that many times is not started again: it is left\n" +
			"unclaimed and marked run=blocked, and the Mayor is mailed once. Only resetting its\n" +
			"attempts counter by hand lets it be started again.\n\n" +
			"If the sync cannot resolve a name (a host just woken from standby has no network for a\n" +
			"minute), dispatch waits and tries the sync again, dispatch_sync_tries times in all,\n" +
			"dispatch_sync_wait apart. If the name still cannot be resolved it claims nothing, prints one\n" +
			"line beginning \"local network fault\" and leaves with " + fmt.Sprint(application.NetworkFaultExit) + ", which a timer may\n" +
			"treat as a wait. Every other sync failure is not retried.\n\n" +
			"--dry-run prints what it would start and writes nothing: nothing is synced, claimed, cut,\n" +
			"poured or started.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			atOnce, err := config.Cap()
			if err != nil {
				return err
			}
			if capOverride > 0 {
				atOnce = capOverride
			}
			rigs, err := config.Rigs()
			if err != nil {
				return err
			}
			if len(rigs) == 0 {
				return fmt.Errorf("no rig is checked out on %s: add one under [%s] in ~/%s, as `<rig> = \"<directory>\"`",
					host, config.RigsTable, config.File)
			}
			tests, err := config.Tests()
			if err != nil {
				return err
			}

			tries, err := config.DispatchSyncTries()
			if err != nil {
				return err
			}
			wait, err := config.DispatchSyncWait()
			if err != nil {
				return err
			}

			maxAttempts, err := config.MaxAttempts()
			if err != nil {
				return err
			}

			gateway := mwGateway(dir, host)
			files := mwVault(dir, host)
			logs := hostTickLogs()
			worktrees := rig.New()
			dispatch := application.Dispatch{
				Tracker:     gateway,
				Worktrees:   worktrees,
				Landing:     worktrees,
				Files:       files,
				Runner:      tmux.New(),
				Boot:        builderBoot(files, host, tests),
				Memory:      gateway,
				Sync:        application.Sync{Vault: files, Tracker: gateway, Host: host, Ticks: logs, Lock: hostSyncLock()},
				SyncTries:   tries,
				SyncWait:    wait,
				SyncHalts:   hostSyncHalt(),
				Host:        host,
				Cap:         atOnce,
				MaxAttempts: maxAttempts,
				Mailbox:     gateway,
				Rigs:        rigs,
				DryRun:      dryRun,
				Out:         cmd.OutOrStdout(),
			}
			// A rehearsal is not a run: it leaves nothing in the log.
			if !dryRun {
				dispatch.Log = logs.Dispatch
			}
			report, err := dispatch.Run(cmd.Context())
			if _, gaveUp := application.LocalFault(err); gaveUp {
				// The one line naming it has been printed, so cobra is not to print
				// the error too: only the status it leaves with says so.
				cmd.SilenceErrors = true
			}
			if err != nil {
				return err
			}
			if len(report.Started) > 0 && !report.DryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "Attach to a session with: tmux attach -t =%s\n", report.Started[0].Session)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"print what would be started and write nothing")
	cmd.Flags().IntVar(&capOverride, "cap", 0,
		"how many sessions may run at once, instead of what the config file says")
	return cmd
}
