package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
	"github.com/Jonathan-A-White/millwright/infrastructure/closeout"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostload"
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

// hostCloseOuts is where close-outs running on this host leave their marks,
// kept under SyncHaltStateDir. A host with no home directory keeps none.
func hostCloseOuts() application.CloseOutMarks {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	return closeout.New(filepath.Join(home, SyncHaltStateDir, closeout.Dir))
}

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

// hostDispatchLock is this host's dispatch lock, kept in DispatchStateDir: held
// for the whole of a real mw dispatch, so that two never run at once. A host
// with no home directory keeps none.
func hostDispatchLock() application.GristLock {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return hostlock.NewTry(filepath.Join(home, DispatchStateDir), hostlock.DispatchFile)
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
			"Only one real dispatch runs on a host at a time: one started while another holds the\n" +
			"host's dispatch lock (~/.local/state/mw-dispatch/lock) prints \"another mw dispatch is running\n" +
			"here; nothing done\" and leaves with 0.\n\n" +
			"Once it holds that lock, before the sync, a real dispatch keeps this host's mw level with the\n" +
			"factory rig's main as mw millhand tick does, so a host that runs no Millhand tick stays level too:\n" +
			"on a host whose [after_landing] table names a command for the millwright rig, a clean checkout\n" +
			"behind origin's main is fast-forwarded and the command run in it, and it prints \"self-update:\n" +
			"millwright <old> → <new>, built\". A commit already built (by either tick) is not built again; a\n" +
			"dirty checkout is left alone, and a build that fails keeps the old mw and is tried by the next tick.\n\n" +
			"On the host that is home (mw home), and where the config file has a [grist] table, it then\n" +
			"runs one pass of the grist mill (mw grist grind), so a grist left waiting for a slot is\n" +
			"answered by this tick. The mill has grind slots of its own ([grist] concurrency): a grind takes\n" +
			"none of the sessions the cap counts, and the cap never holds a grind back.\n\n" +
			"While the network is metered (config `metered`, or Windows' own setting read from WSL) it\n" +
			"starts no story whose rig is named under [heavy_net] in the config file (`postern = true`),\n" +
			"because its gate or close-out installs dependencies: the story stays open, and the first tick\n" +
			"after the network is unmetered takes it. Any change of the network's state is one event.\n\n" +
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
			nice, err := config.BuilderNice()
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

			heavy, err := config.HeavyNet()
			if err != nil {
				return err
			}
			// A rehearsal only looks: it keeps no answer and writes no event.
			net, err := hostNetwork(dryRun)
			if err != nil {
				return err
			}

			gateway := mwGateway(dir, host)
			files := mwVault(dir, host)
			logs := hostTickLogs()
			worktrees := rig.New()
			sync, err := hostSync(application.Sync{Vault: files, Tracker: gateway, Host: host, Ticks: logs, Lock: hostSyncLock(), Network: net})
			if err != nil {
				return err
			}
			dispatch := application.Dispatch{
				Tracker:     gateway,
				Worktrees:   worktrees,
				Landing:     worktrees,
				Files:       files,
				Runner:      tmux.New(),
				Boot:        builderBoot(files, host, tests, nice),
				Memory:      gateway,
				Sync:        sync,
				SyncTries:   tries,
				SyncWait:    wait,
				SyncHalts:   hostSyncHalt(),
				Host:        host,
				Cap:         atOnce,
				MaxAttempts: maxAttempts,
				Mailbox:     gateway,
				Load:        hostload.Proc{},
				Rigs:        rigs,
				Network:     net,
				HeavyNet:    heavy,
				Exclusive:   hostDispatchLock(),
				Events:      homeEventLog(),
				Home:        files,
				DryRun:      dryRun,
				Out:         cmd.OutOrStdout(),
			}
			// The mill answers what it left waiting, on the host that is home and
			// where a [grist] table says so. A mill that cannot be set up costs
			// the tick its grist pass and nothing else.
			if configured, err := config.GristConfigured(); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "no grist pass this tick: %v\n", err)
			} else if configured && !dryRun {
				if mill, err := newMill(cmd.OutOrStdout()); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "no grist pass this tick: %v\n", err)
				} else {
					dispatch.Mill = mill
				}
			}
			// The tick keeps this host's mw level with the factory rig's main
			// first, as the Millhand's tick does, for a host that runs no
			// Millhand tick. A config or home that cannot be read costs the tick
			// that look and nothing else.
			if !dryRun {
				if afterLanding, err := config.AfterLanding(); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "no self-update this tick: %v\n", err)
				} else if afterLimits, err := config.AfterLandingLimits(); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "no self-update this tick: %v\n", err)
				} else if home, err := os.UserHomeDir(); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "no self-update this tick: there is no home directory to remember a build in: %v\n", err)
				} else {
					dispatch.SelfUpdate = hostSelfUpdate(rigs, afterLanding, afterLimits, home)
				}
				dispatch.Backend = hostBackend(gateway, files, host, rigs, cmd.ErrOrStderr())
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
