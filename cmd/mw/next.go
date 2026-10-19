package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostload"
	"github.com/Jonathan-A-White/millwright/infrastructure/procs"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"
	"github.com/Jonathan-A-White/millwright/infrastructure/userunits"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/spf13/cobra"
)

// newNextCmd builds `mw next`: the command a story's session ends with. The
// dispatch command line chains it after the harness exits, so it runs whether
// the session finished, failed or died, and it is the only place a story is
// landed, ledgered and closed.
func newNextCmd() *cobra.Command {
	var noDispatch bool
	var capOverride int
	var heartbeat bool

	cmd := &cobra.Command{
		Use:   "next <story-id>",
		Short: "Close out a finished story, record what it burned, and start what is ready next",
		Long: "next reads what the story's session reported in runs/<story>/result.json. A session that did not\n" +
			"finish is written on the story, marked blocked and left alone: nothing is merged, nothing is closed\n" +
			"and the worktree is kept, because it is the evidence.\n\n" +
			"A session that did finish is checked before anything is landed: the branch must hold commits, none\n" +
			"of those commits may be signed by a machine — a Co-Authored-By trailer or a \"Generated with\" line\n" +
			"is refused, and the offending commit is named — every step of its formula must be closed, and the\n" +
			"rig's own tests must pass in the worktree. Then, under\n" +
			"the rig's merge slot, the branch is merged into the target branch as the remote has it — and if that\n" +
			"was not a fast-forward the tests are run again on the merged result, because nothing has ever tested\n" +
			"that combination. The push is never forced; a push the remote refuses because the branch moved is\n" +
			"fetched and tried again a few times, and a push that fails on a fault at the remote itself — never a\n" +
			"stated refusal — is tried again in place a few times too, waiting between tries (push_tries and\n" +
			"push_wait_seconds). A branch that does not merge without conflicts is sent back, once, to a fresh Builder\n" +
			"session in the same worktree, told to rebase onto the target branch, resolve, run the suite and\n" +
			"commit; the story is recorded rebase=sent-back and the mw next that session ends with lands it. A\n" +
			"second conflict stops as any refusal does, and nobody is sent back again. Then the worktree goes,\n" +
			"one line is appended to the seat's ledger, the story is closed, the hosts are brought level, and\n" +
			"whatever is ready here is dispatched.\n\n" +
			"On a rig whose file says landing = \"batch\" (plans/0117), a branch that passes the checks is not\n" +
			"merged: it is pushed to the remote as itself, mw/<story>, never forced; the story is recorded\n" +
			"run=queued, left open and claimed with its worktree kept, one ledger line says it is queued for\n" +
			"batch landing, its session is closed and whatever is ready here is dispatched. No mail is sent;\n" +
			"its batch lands it. When mw check passed at that very commit (the tip line of its log), the rig's\n" +
			"tests are not run again, and the ledger line says \"tested by mw check at <commit>\". The status,\n" +
			"the sweep, the dispatch and the quiet alarm call a queued story alive, never stuck or abandoned.\n\n" +
			"One mail goes to the Mayor from mw on this host — Landed, Refused (the checks turned the branch away),\n" +
			"Sent back (to rebase) or Blocked (anything else stopped it) — holding this report; a mail that cannot be sent is said on\n" +
			"stderr and changes nothing. A run that finds nothing to close sends none.\n\n" +
			"That ledger line and the seat's memory of the rig — the only two vault files a story may write — are\n" +
			"committed by path first, under a plain message naming the story, so that a close-out's own writing\n" +
			"does not stop the sync that follows. Anything else uncommitted in the vault still does, naming the\n" +
			"file; the story is landed and closed all the same.\n\n" +
			"Run by hand on a story whose session is still running — a `mw next --heartbeat` of it is alive on this\n" +
			"host — it refuses, naming the session, and changes nothing: no ledger line, label, comment or mail.\n\n" +
			"A story that landed and could not be closed is reported as landed but still open, and nothing of\n" +
			"the landing is undone. Run next on it again: it closes the story and carries on, without merging,\n" +
			"testing or pushing anything a second time and without a second line in the ledger.",
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

			// --heartbeat is not a close-out: it is the other half of what
			// this same command runs when it is chained into a dispatched
			// session's shell line — beside the harness, not after it —
			// renewing the story's claim's lease for as long as the harness
			// is really running, and returning once it is not. It records the
			// files the branch has changed on the story as it goes (mw-gq6.406).
			if heartbeat {
				return application.Next{
					Tracker: mwGateway(dir, host),
					Runner:  tmux.New(),
					Changes: rig.New(),
					Rigs:    rigs,
				}.Heartbeat(cmd.Context(), args[0])
			}

			atOnce, err := config.Cap()
			if err != nil {
				return err
			}
			if capOverride > 0 {
				atOnce = capOverride
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
			fromBranch, err := config.NextRetryFromBranch()
			if err != nil {
				return err
			}
			changedSpecs, err := config.ChangedSpecs()
			if err != nil {
				return err
			}
			relatedTests, err := config.RelatedTests()
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
			maxAttempts, err := config.MaxAttempts()
			if err != nil {
				return err
			}
			testers, err := testersPerRig()
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
			// A [tester] table that cannot be read springs no Tester story, and
			// never stops a close-out: it is said, and the landing goes on.
			tester, err := testerTrial()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "mw next: no Tester story will be sprung: %v\n", err)
				tester = application.TesterTrial{}
			}

			// A close-out takes away the very worktree its session was running
			// in, and this process is standing in it: the shell that chained mw
			// next onto the harness inherited the session's directory. Anything
			// run after the removal — bd, tmux, the dispatch that follows —
			// would be started from a directory that no longer exists, and on
			// Linux that fails before the program is even reached. So the
			// close-out works from the vault, which is never the thing it takes
			// away.
			if err := os.Chdir(dir); err != nil {
				return fmt.Errorf("closing out %s: the vault %s cannot be worked from: %w", args[0], dir, err)
			}

			// A close-out that waits for a slot on another rig reads a note every
			// CrossSlotPoll, and each read started a bd, several connections to the
			// beads server, four times a minute for as long as it waited
			// (mw-gq6.608): the notes are read over one connection kept open.
			defer keepBeadsConnection(dir, cmd.ErrOrStderr())()
			gateway := mwGateway(dir, host)
			bench, err := config.Benchmark()
			if err != nil {
				return err
			}
			files := mwVault(dir, host)
			worktrees := rig.New()
			runner := tmux.New()
			sync, err := hostSync(application.Sync{Vault: files, Tracker: gateway, Host: host, Ticks: hostTickLogs(), Lock: hostSyncLock(dir)})
			if err != nil {
				return err
			}

			// The dispatch that follows does not sync for itself: the close-out
			// syncs once, after the story is closed, so that the other host sees
			// a closed story rather than a claimed one.
			var dispatcher application.Dispatcher
			if !noDispatch {
				dispatcher = withHostDispatchLocks(application.Dispatch{
					Tracker:       gateway,
					Worktrees:     worktrees,
					Landing:       worktrees,
					Runner:        runner,
					Boot:          builderBoot(files, host, tests, nice, jobs, turns),
					Memory:        gateway,
					Host:          host,
					Cap:           atOnce,
					MaxAttempts:   maxAttempts,
					TestersPerRig: testers,
					Mailbox:       gateway,
					Load:          hostload.Proc{},
					Rigs:          rigs,
					Changes:       worktrees,
					Events:        homeEventLog(),
					SmokeHolds:    gristSmokeBook(gateway, host),
					Out:           cmd.OutOrStdout(),

					RetryFromBranch: fromBranch,
					Rebases:         worktrees,
				}, dir)
			}

			// A stamp queue that cannot be placed is said, and queues nothing: no
			// landing waits on a chain stamp.
			stamps, err := stampQueue()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "chain stamp: not queued for this landing: %v\n", err)
				stamps = nil
			}

			// Without the harness's projects directory no transcript can be read, and
			// a claim with no session behind it is left alone rather than given back.
			var transcripts application.ClaimTranscripts
			var modes application.TranscriptModes
			if projects, err := claude.DefaultProjectsRoot(); err == nil {
				transcripts = claude.NewTranscripts(projects)
				modes = claude.NewTranscripts(projects)
			}
			checks := rig.NewChecks(rig.WithCommands(tests), rig.WithSpecCommands(changedSpecs), rig.WithRelatedCommands(relatedTests), rig.WithNice(nice), rig.WithJobs(jobs))
			_, err = application.Next{
				Tracker:    gateway,
				Worktrees:  worktrees,
				Landing:    worktrees,
				Changes:    worktrees,
				Errors:     files,
				RigFacts:   files,
				Checks:     checks,
				SpecChecks: checks,

				// The landing gate runs the rig's related-tests line when it has
				// one; mw check and mw suite are never given it (mw-gq6.601).
				RelatedChecks: checks,

				Load:        hostload.Proc{SwapSample: benchmarkSwapSample},
				Slot:        rig.NewSlots(gateScaledSlotWait(vault.Benchmarks{Vault: files}, rigs, host, benchmarkLimits(bench))),
				CrossSlot:   application.CrossSlot{Notes: gateway, Host: host},
				GateLock:    hostGateLock(),
				Vault:       files,
				Rules:       files,
				Files:       files,
				Mailbox:     gateway,
				Runner:      runner,
				Heartbeats:  procs.Heartbeats{},
				Leftovers:   procs.Table{},
				Transcripts: transcripts,
				Modes:       modes,
				Memory:      gateway,
				Sync:        sync,
				Dispatch:    dispatcher,
				Events:      homeEventLog(),
				Boot:        builderBoot(files, host, tests, nice, jobs, turns),
				Seat:        BuilderSeat,
				Host:        host,
				Rigs:        rigs,
				PushTries:   pushTries,
				PushWait:    time.Duration(pushWaitSeconds) * time.Second,
				Out:         cmd.OutOrStdout(),
				Err:         cmd.ErrOrStderr(),

				HeartbeatGrace: application.DefaultHeartbeatGrace,

				AfterLanding: rig.NewAfterLanding(rig.WithAfterCommands(afterLanding), rig.WithAfterLimits(afterLimits)),
				DeploySlot:   rig.NewSlots(rig.WithSlotSuffix(rig.AfterLandingSlotSuffix)),
				Deployed:     hostDeployedMarks(),
				Home:         files,
				Backend:      hostBackend(gateway, files, host, rigs, cmd.ErrOrStderr()),
				Smoke:        hostGristSmoke(gateway, host, rigs, worktrees, nil, ""),
				Units:        userunits.Systemctl{},
				Stamps:       stamps,
				CloseOuts:    hostCloseOuts(),
				Tester:       tester,
				Benchmarks:   vault.Benchmarks{Vault: files},
				Bench:        benchmarkLimits(bench),
				BenchFlags:   hostBenchFlags(),

				RetryFromBranch: fromBranch,
			}.Run(cmd.Context(), args[0])
			return err
		},
	}

	cmd.Flags().BoolVar(&noDispatch, "no-dispatch", false,
		"close the story out and stop, without starting whatever is ready next")
	cmd.Flags().IntVar(&capOverride, "cap", 0,
		"how many sessions may run at once when dispatching, instead of what the config file says")
	cmd.Flags().BoolVar(&heartbeat, "heartbeat", false,
		"renew the story's claim's lease while its session runs, until the session ends; run beside a dispatched session's harness, not by hand")
	return cmd
}

// beadsEnvFile is the file where this host keeps the names and values that
// reach a served beads database, which a session mw starts reads before
// anything else (claude.WithEnvFile). It is found under the home directory the
// way dispatch finds its own state; a host with no home directory names none,
// and so has no such file read. Only the path is ever used here, never the
// file.
func beadsEnvFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "mw", "beads.env")
}

// builderBoot is how every dispatched session is assembled: into the Builder
// seat, on this host, with this same mw chained on after the harness exits.
// tests is the [tests] table, so the session may run its own rig's tests
// without being asked, exactly as `mw next` would run them (see
// claude.WithTests). nice is builder_nice: the session runs that much nicer
// than the mill (see claude.WithNice), and jobs is builder_jobs: the most test workers
// it may start (see claude.WithJobs), and turns is next.max_turns: the most
// turns a session takes unless its story's path says otherwise (mw-1ljpo9.6).
// withHostDispatchLocks gives a dispatch the lock the timer's dispatch takes:
// this host's dispatch lock, so that the dispatch that ends a landing and a
// timer tick never claim, pour and start the same story at once (mw-gq6.193).
// A dispatch that cannot take the dispatch lock
// says so and does nothing; the timer's next tick starts what is ready.
func withHostDispatchLocks(d application.Dispatch, vault string) application.Dispatch {
	d.Exclusive = hostDispatchLock(vault)
	return d
}

func builderBoot(files *vault.Vault, host string, tests map[string]string, nice, jobs, turns int) application.SeatBoot {
	return application.SeatBoot{
		Vault:     files,
		Harness:   sessionHarness(files.Dir(), host, claude.WithTests(tests), claude.WithNice(nice), claude.WithJobs(jobs)),
		Seat:      BuilderSeat,
		Host:      host,
		After:     afterSession(),
		Heartbeat: heartbeatSession(),
		BdRetry:   bdRetrySession(),
		MaxTurns:  turns,
		Queries:   memoryQueryLog(),
	}
}

// afterSession is the command a dispatched session ends with: this same mw,
// closing the story out. It is found by asking the running program where it is,
// so that a mw run from a worktree chains that mw rather than whatever is on
// PATH — and a mw that cannot say where it is falls back to the name, which is
// what a host with mw installed has anyway.
func afterSession() []string {
	return append(mwProgram(), "next")
}

// heartbeatSession is the command that runs beside a dispatched session's
// harness, from the moment it starts to the moment it exits: this same mw,
// renewing the claim's lease. Found the same way afterSession's mw is.
func heartbeatSession() []string {
	return append(mwProgram(), "next", "--heartbeat")
}

// bdRetrySession is the command a dispatched session's bd becomes: this same
// mw, as `mw bd` (mw-gq6.393).
func bdRetrySession() []string {
	return append(mwProgram(), "bd")
}

// mwProgram is the mw this host runs, as one argument: the running program's
// own path when it can be found, so that a mw run from a worktree chains that
// mw rather than whatever is on PATH, and falls back to the name otherwise,
// which is what a host with mw installed has anyway.
func mwProgram() []string {
	program, err := os.Executable()
	if err != nil || program == "" {
		program = "mw"
	}
	return []string{program}
}

// benchmarkSwapSample is how long a close-out samples the swap rate for at the
// start of its gate: vmstat's si and so over that long.
const benchmarkSwapSample = 500 * time.Millisecond
