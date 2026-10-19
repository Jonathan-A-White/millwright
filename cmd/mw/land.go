package main

import (
	"fmt"
	"os"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostload"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"
	"github.com/Jonathan-A-White/millwright/infrastructure/userunits"

	"github.com/spf13/cobra"
)

// newLandCmd builds `mw land <rig>`: the one command that pushes the target
// branch of a rig that lands in batches (plans/0117, Land Rush 3).
func newLandCmd() *cobra.Command {
	var dryRun, one bool

	cmd := &cobra.Command{
		Use:   "land <rig>",
		Short: "Land a rig's queued stories as one batch, under one run of its tests",
		Long: "land takes every story queued for batch landing on the rig (run=queued, by mw next on a rig\n" +
			"whose file says landing = \"batch\"), on any host, oldest queued first. In a landing worktree\n" +
			"detached at the target branch as the origin has it, it merges each story's branch as the origin\n" +
			"has it (mw/<story>) by the rig's union rules (union_merge_files); a branch that does not merge\n" +
			"is set aside with git's conflict list and stays queued. It raises the version once for the batch\n" +
			"(version_files), with one changelog entry per story (changelog_files), and runs the rig's tests\n" +
			"once on the batch, under this host's gate lock and after letting a busy host calm.\n\n" +
			"Only when that run passes is the target branch pushed: a fast-forward, never forced. Each story\n" +
			"is then closed run=landed, naming the batch's commit and its own merge commit, with a chain\n" +
			"stamp, a ledger line naming the batch and a Landed mail; the rig's checkout is brought up and\n" +
			"the after-landing command and the grist smoke run once. A set-aside story is mailed to the\n" +
			"Mayor (Set aside) with its conflict.\n\n" +
			"A red run, a batch of which nothing merged, or a push the origin refuses pushes nothing: the\n" +
			"batch is held, every story in it stays queued, the Mayor is mailed (Held) each story and why,\n" +
			"and land exits non-zero.\n\n" +
			"A batch with a branch set aside for its conflicts (even when every branch was, so that nothing\n" +
			"merged), or whose run is red, is first given one fixer:\n" +
			"a Builder session (batch-<rig>-<n>) in the landing worktree, on the rig's\n" +
			"batch_fixer_model, batch_fixer_effort and batch_fixer_turns, told every branch, git's conflicts\n" +
			"and the run's last lines. land waits for it and runs the rig's tests on the batch again itself:\n" +
			"green lands the batch, red holds it, and no second fixer is started. A batch with nothing queued,\n" +
			"with only branches missing from the origin, or whose push the origin refuses is given none.\n\n" +
			"What the batch did is kept in the vault under runs/batch/<rig>/<n>/: merge-order.txt, gate.log\n" +
			"and outcome.txt, committed with the ledger.\n\n" +
			"--one takes only the oldest queued story, whatever else is queued, and lands it by itself.\n\n" +
			"--dry-run prints the batch it would make and does nothing else: nothing is merged, tested,\n" +
			"pushed, recorded or mailed.",
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
			rigs, err := config.Rigs()
			if err != nil {
				return err
			}
			tests, err := config.Tests()
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
			turns, err := config.NextMaxTurns()
			if err != nil {
				return err
			}
			afterLanding, err := config.AfterLanding()
			if err != nil {
				return err
			}
			afterLimits, err := config.AfterLandingLimits()
			if err != nil {
				return err
			}
			pushTries, err := config.PushTries()
			if err != nil {
				return err
			}
			pushWaitSeconds, err := config.PushWaitSeconds()
			if err != nil {
				return err
			}
			// The landing takes story worktrees away; it works from the vault,
			// which it never does, as mw next does.
			if err := os.Chdir(dir); err != nil {
				return fmt.Errorf("landing a batch of %s: the vault %s cannot be worked from: %w", args[0], dir, err)
			}
			stamps, err := stampQueue()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "chain stamp: not queued for this batch: %v\n", err)
				stamps = nil
			}

			gateway := mwGateway(dir, host)
			files := mwVault(dir, host)
			worktrees := rig.New()
			checks, err := landingChecks(gateCommands{related: true})
			if err != nil {
				return err
			}
			_, err = application.BatchLand{
				Next: application.Next{
					Tracker:   gateway,
					Worktrees: worktrees,
					Landing:   worktrees,
					Changes:   worktrees,
					Checks:    checks,

					// The batch's gate runs the rig's related-tests line when it has
					// one, on the union of its stories' files (mw-gq6.601), and the
					// rig's changed-specs command after it on the union of its specs
					// (mw-gq6.627).
					RelatedChecks: checks,
					SpecChecks:    checks,

					Load:      hostload.Proc{SwapSample: benchmarkSwapSample},
					Slot:      rig.NewSlots(),
					GateLock:  hostGateLock(),
					Vault:     files,
					Rules:     files,
					Files:     files,
					RigFacts:  files,
					Mailbox:   gateway,
					Memory:    gateway,
					Runner:    tmux.New(),
					Boot:      builderBoot(files, host, tests, nice, jobs, turns),
					Events:    homeEventLog(),
					Seat:      BuilderSeat,
					Host:      host,
					Rigs:      rigs,
					PushTries: pushTries,
					PushWait:  time.Duration(pushWaitSeconds) * time.Second,
					Out:       cmd.OutOrStdout(),
					Err:       cmd.ErrOrStderr(),

					AfterLanding: rig.NewAfterLanding(rig.WithAfterCommands(afterLanding), rig.WithAfterLimits(afterLimits)),
					DeploySlot:   rig.NewSlots(rig.WithSlotSuffix(rig.AfterLandingSlotSuffix)),
					Deployed:     hostDeployedMarks(),
					Home:         files,
					Smoke:        hostGristSmoke(gateway, host, rigs, worktrees, cmd.OutOrStdout(), application.BatchLogPrefix+"grist smoke: "),
					Units:        userunits.Systemctl{},
					Stamps:       stamps,
				},
				Records: files,
				Metrics: files,
				Fix:     true,
				DryRun:  dryRun,
				One:     one,
			}.Run(cmd.Context(), args[0])
			return err
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"print the batch it would make, oldest queued first, and do nothing else")
	cmd.Flags().BoolVar(&one, "one", false,
		"take only the oldest queued story: how the follower lands what a rig flipped back to landing = each had queued")
	return cmd
}
