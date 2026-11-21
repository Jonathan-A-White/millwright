package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostload"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/spf13/cobra"
)

// newLandCmd builds `mw land <rig>`: the one command that pushes the target
// branch of a rig that lands in batches (plans/0117, Land Rush 3).
func newLandCmd() *cobra.Command {
	var dryRun, one bool
	var only, without []string
	var setAside, why string

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
			"the after-landing command runs once and the grist smoke is begun, as a process of its own (mw\n" +
			"land-smoke, its output in ~/.local/state/mw/land/smoke/<rig>.log) that goes on after land ends,\n" +
			"so the rig's next batch can land meanwhile; a failing smoke mails the Mayor from there. A\n" +
			"set-aside story is mailed to the Mayor (Set aside) with its conflict.\n\n" +
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
			"and outcome.txt, committed with the ledger; a held batch keeps the commits its fixer left in\n" +
			"fixer.bundle there, named in outcome.txt and the Held mail. A fixer commit that touches one\n" +
			"story's files only is also pushed to that story's branch at the origin, so the batch landed\n" +
			"again merges it, and the Held mail says where; one that spans stories stays in the bundle alone.\n\n" +
			"--one takes only the oldest queued story, whatever else is queued, and lands it by itself.\n\n" +
			"--only <id>[,<id>...] takes just those queued stories, and --without <id>[,<id>...] leaves those\n" +
			"queued stories out of this batch; what either skips stays queued for the next. After a batch\n" +
			"of several is held red, the follower lands its stories again through them: the story its fixer\n" +
			"named as the cause (or the one whose branch alone changed the failing tests' files) is left\n" +
			"out, the others land together and it lands alone; with none named the stories land as two\n" +
			"halves, and only a half held red again goes one story at a time. By hand, the Mayor leaves a\n" +
			"suspect out with mw land <rig> --without <story>.\n\n" +
			"--set-aside <story> --why \"...\" takes one queued story out of the batch queue and lands nothing:\n" +
			"its branch is kept, its claim is given back held, and a SET-ASIDE comment carries the why. The\n" +
			"next mw land takes the remaining queued stories as one batch; mw release <story> puts it back.\n\n" +
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
			if setAside != "" || why != "" {
				if setAside == "" {
					return fmt.Errorf("land %s: --why goes with --set-aside <story>", args[0])
				}
				if dryRun || one || len(only) > 0 || len(without) > 0 {
					return fmt.Errorf("land %s: --set-aside takes a story out of the queue and lands nothing: it goes with --why and no other flag", args[0])
				}
				return application.SetAside{Tracker: mwGateway(dir, host), Host: host, Out: cmd.OutOrStdout()}.Run(cmd.Context(), args[0], setAside, why)
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
			testRuns, err := hostTestRuns()
			if err != nil {
				return err
			}
			turns, turnsSaid, err := config.NextMaxTurnsSaid()
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
			deploySteps, err := afterLandingDeploySteps(afterLanding)
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
			gateTurnsLimit, err := config.GateTurnsLimit()
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
			bench, err := config.Benchmark()
			if err != nil {
				return err
			}
			room, err := config.Room()
			if err != nil {
				return err
			}
			worktrees := rig.New()
			changedSpecs, err := config.ChangedSpecs()
			if err != nil {
				return err
			}
			// A finding in a story's closing comment is read against main before its
			// bug is filed, as mw next reads it: a rig that lands by batch files its
			// bugs here, so it needs the same reader (mw-6ww.119.10.5).
			recheckSuite := hostRecheckSuite(cmd.Context(), worktrees, files, rigs, tests, changedSpecs, nice, jobs, host)
			// The gate commands are read when the gate runs, not now (mw-gq6.1006);
			// what the builder's boot, the turn cap, the after-landing limits and
			// the push tries are is read once, here.
			checks, err := landingChecks(dir, gateCommands{related: true, parts: true, reread: true})
			if err != nil {
				return err
			}
			next := withHostNextRoom(application.Next{
				Tracker:   gateway,
				Worktrees: worktrees,
				Landing:   worktrees,
				Changes:   worktrees,
				Checks:    checks,

				// The batch's gate runs the rig's related-tests line when it has
				// one, on the union of its stories' files (mw-gq6.601), and the
				// rig's changed-specs command after it on the union of its specs
				// (mw-gq6.627).
				RelatedChecks:  checks,
				SpecChecks:     checks,
				NamedChecks:    checks,
				NamedElsewhere: namedElsewhere(),
				GateParts:      checks,
				GateRecords:    files,

				Load:     hostload.Proc{SwapSample: benchmarkSwapSample},
				Bench:    benchmarkLimits(bench), // the par of a landed story, from the past benchmarks withVaultPorts reads (mw-gq6.966)
				Slot:     hostSlots(),
				GateLock: hostGateLock(),
				Mailbox:  gateway,
				// A finding main already fixes is not filed as a bug (mw-gq6.1245).
				FindingChecks: recheckSuite,
				// A batch's turns end once they waited this long for other gates (mw-gq6.1420).
				GateTurnsLimit: gateTurnsLimit,
				Memory:         gateway,
				Runner:         tmux.New(),
				Boot:           builderBoot(files, host, tests, nice, jobs, turns, turnsSaid),
				GiveWay:        hostGiveWayGate(host), // no fixer and no part of the gate starts once the host gives way (mw-2891blf.19)
				Events:         homeEventLog(),
				Seat:           BuilderSeat,
				Host:           host,
				Rigs:           rigs,
				PushTries:      pushTries,
				PushWait:       time.Duration(pushWaitSeconds) * time.Second,
				Out:            cmd.OutOrStdout(),
				Err:            cmd.ErrOrStderr(),

				AfterLanding: rig.NewAfterLanding(rig.WithAfterCommands(afterLanding), rig.WithAfterCommandsFrom(config.AfterLanding, time.Now), rig.WithAfterLimits(afterLimits), deploySteps, rig.WithAfterNice(nice), rig.WithAfterJobs(jobs), rig.WithAfterTestRuns(testRuns)),
				DeploySlot:   hostSlots(rig.WithSlotSuffix(rig.AfterLandingSlotSuffix)),
				Deployed:     hostDeployedMarks(),
				LiveSite:     verifyWeb{},
				Smoke:        hostGristSmoke(gateway, host, rigs, worktrees, cmd.OutOrStdout(), application.BatchLogPrefix+"grist smoke: "),
				Backend:      hostBackend(gateway, files, host, rigs, cmd.ErrOrStderr()), // staged as after a single landing (mw-gq6.963)
				Units:        hostUnits(),
				Stamps:       stamps,
			}, room)
			landing := batchLandFor(next, files, dryRun, application.LandScope{One: one, Only: only, Without: without}, cmd.ErrOrStderr())
			if self, err := os.Executable(); err == nil {
				if home, err := os.UserHomeDir(); err == nil {
					// The grist smoke goes on apart from this landing (mw-apvyg8j).
					landing.Apart = landSmokeApart{self: self, logDir: landSmokeLogDir(home)}
				}
			}
			_, err = landing.Run(cmd.Context(), args[0])
			return err
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"print the batch it would make, oldest queued first, and do nothing else")
	cmd.Flags().StringSliceVar(&only, "only", nil,
		"take just these queued stories (ids, comma separated): how the follower lands a held batch in parts")
	cmd.Flags().StringSliceVar(&without, "without", nil,
		"leave these queued stories out of this batch (ids, comma separated): they stay queued for the next")
	cmd.Flags().StringVar(&setAside, "set-aside", "",
		"take this queued story out of the batch queue: its branch is kept, its claim given back held; needs --why, lands nothing")
	cmd.Flags().StringVar(&why, "why", "", "why the story is set aside: the SET-ASIDE comment on it carries this")
	cmd.Flags().BoolVar(&one, "one", false,
		"take only the oldest queued story: how the follower lands what a rig flipped back to landing = each had queued")
	return cmd
}

// batchLandFor is the BatchLand mw land runs for the Next it built: the vault's
// ports on it (withVaultPorts), and the Tester trial of the [tester] table, as
// mw next gives its Next (mw-gq6.962). A table that cannot be read springs no
// Tester story and never stops a landing: it is said on errw, and it goes on.
func batchLandFor(next application.Next, files *vault.Vault, dryRun bool, scope application.LandScope, errw io.Writer) application.BatchLand {
	return application.BatchLand{
		Next:    withTesterTrial(withVaultPorts(next, files), "mw land", errw),
		Records: files,
		Metrics: files,
		Fix:     true,
		DryRun:  dryRun,
		One:     scope.One,
		Only:    scope.Only,
		Without: scope.Without,
	}
}

// withVaultPorts gives a landing's Next the ports that are the vault, which a
// Landed mail reads its blocks through: the rig's facts and the common-errors
// register, as mw next gives them (mw-gq6.631).
func withVaultPorts(n application.Next, files *vault.Vault) application.Next {
	n.Vault = files
	n.Rules = files
	n.Files = files
	n.Errors = files
	n.RigFacts = files
	n.Home = files
	n.Benchmarks = vault.Benchmarks{Vault: files}
	return n
}
