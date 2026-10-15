package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostload"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/spf13/cobra"
)

// newCheckCmd builds `mw check`: the checks mw next makes before it lands a
// story, run on a story's branch by the session working it. It is read-only —
// it writes nothing to the tracker, the ledger, the vault or git — and it exits
// non-zero when any check fails.
func newCheckCmd() *cobra.Command {
	var noSlot, background, logIsStdout bool
	cmd := &cobra.Command{
		Use:   "check <story-id>",
		Short: "Run the checks mw next makes before landing a story, on its branch, and change nothing",
		Long: "check runs what mw next would refuse a story's branch for, while the session that is working the\n" +
			"story can still do something about it: the branch must hold commits, none of those commits may be\n" +
			"signed by a machine, every step of the story's formula must be closed, and the rig's own tests\n" +
			"must pass in the story's worktree. Each refusal is printed as mw next would word it, all of them\n" +
			"at once rather than the first only, and check exits non-zero when there is any.\n\n" +
			"check is read-only: it writes no comment, run state or ledger line, commits nothing in the vault\n" +
			"(bar the story's check log, below),\n" +
			"and does not fetch, merge or push. The one thing it takes is the rig's merge slot, around the\n" +
			"rig's tests, so that a close-out's tests and a check's on one rig do not run at once and starve\n" +
			"each other: it waits for the slot, saying who holds it, and --no-slot skips the wait.\n\n" +
			"check treats a busy host as the close-out does (mw-gq6.228): it waits, up to the close-out's bound,\n" +
			"for the host to calm before the tests, and runs tests that failed while it was busy once more when\n" +
			"it has calmed, reporting only a second failure and saying it ran twice.\n\n" +
			"Every check also writes its whole output to runs/<story>/check-<attempt>.log in the vault, named\n" +
			"for its story and attempt so that no other story's check can overwrite it. --background starts the\n" +
			"check detached, writing that log, prints the log's path and returns at once: the tests can outlast\n" +
			"a tool call's time limit, and the verdict is the last lines of the log when the check is done.\n" +
			"A check that passes on a clean worktree names the commit it passed at on a tip line just before\n" +
			"its verdict: on a rig that lands in batches, mw next closing out that very commit trusts it and\n" +
			"does not run the rig's tests again, and its ledger line says so (plans/0117).\n\n" +
			"It does not read the session's result, which is not written\n" +
			"until the session ends, and it does not try the merge, so a branch that passes here can still be\n" +
			"stopped by a conflict or by the other host's work. The commits are counted against the target\n" +
			"branch as the rig last saw the remote's.\n\n" +
			"The formula's last step is normally still open when a session runs check, so a session runs it\n" +
			"after committing and closes that step once nothing else is refused.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			if background {
				_, err := application.CheckBackground{
					Tracker: mwGateway(dir, host),
					Logs:    mwVault(dir, host),
					Starter: checkStarter{noSlot: noSlot},
					Out:     cmd.OutOrStdout(),
				}.Run(cmd.Context(), args[0])
				return err
			}
			rigs, err := config.Rigs()
			if err != nil {
				return err
			}
			tests, err := config.Tests()
			if err != nil {
				return err
			}
			changedSpecs, err := config.ChangedSpecs()
			if err != nil {
				return err
			}
			nice, err := config.BuilderNice()
			if err != nil {
				return err
			}
			jobs, err := config.BuilderJobs()
			if err != nil {
				return err
			}

			var slot application.MergeSlot
			if !noSlot {
				bench, err := config.Benchmark()
				if err != nil {
					return err
				}
				slot = rig.NewSlots(
					gateScaledSlotWait(vault.Benchmarks{Vault: mwVault(dir, host)}, rigs, host, benchmarkLimits(bench)),
					rig.WithSlotNotice(func(said string) { fmt.Fprint(cmd.ErrOrStderr(), said) }))
			}
			worktrees := rig.New()
			checks := rig.NewChecks(rig.WithCommands(tests), rig.WithSpecCommands(changedSpecs), rig.WithNice(nice), rig.WithJobs(jobs))
			k := application.Check{
				Tracker:    mwGateway(dir, host),
				Landing:    worktrees,
				Checks:     checks,
				SpecChecks: checks,
				Slot:       slot,
				Load:       hostload.Proc{},
				Host:       host,
				Rigs:       rigs,
				Out:        cmd.OutOrStdout(),
			}
			if !logIsStdout {
				k.Logs = mwVault(dir, host)
			}
			_, err = k.Run(cmd.Context(), args[0])
			return err
		},
	}
	cmd.Flags().BoolVar(&background, "background", false, "start the check detached, writing its whole output to runs/<story>/check-<attempt>.log in the vault, print that log's path and return at once")
	cmd.Flags().BoolVar(&logIsStdout, "log-is-stdout", false, "the output of this check is already being written to the story's log (set by --background)")
	_ = cmd.Flags().MarkHidden("log-is-stdout")
	cmd.Flags().BoolVar(&noSlot, "no-slot", false, "run the rig's tests without taking the rig's merge slot, so without waiting for a close-out's gate")
	return cmd
}

// checkStarter implements application.CheckStarter: it runs this same mw as
// `mw check <story>` in a session and process group of its own, so that it
// outlives the tool call that asked, with its output appended to the story's log.
type checkStarter struct{ noSlot bool }

func (s checkStarter) Start(_ context.Context, storyID, logPath string) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding mw to run the check with: %w", err)
	}
	log, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("opening the log %s: %w", logPath, err)
	}
	defer log.Close()
	args := []string{"check", storyID, "--log-is-stdout"}
	if s.noSlot {
		args = append(args, "--no-slot")
	}
	// Not exec.CommandContext: the check is meant to outlive the context it was
	// started in.
	cmd := exec.Command(self, args...)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting the check: %w", err)
	}
	// Nobody waits for it: let go of it rather than leave a zombie of this process.
	return cmd.Process.Release()
}
