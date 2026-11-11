package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
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
	var noSlot, background, logIsStdout, partWorker, specsWorker bool
	var part int
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
			"and does not fetch, merge or push. The two things it takes are the rig's merge slot, around the\n" +
			"rig's tests, so that a close-out's tests and a check's on one rig do not run at once and starve\n" +
			"each other, and then this host's gate lock, the one a close-out's gate takes, so that one gate-weight\n" +
			"run goes at a time on the host whoever asked for it (mw-gq6.906). In the foreground it waits for both\n" +
			"together, nine minutes at most, under the 600 seconds a tool call is given by default, saying every minute who\n" +
			"holds each and since when. A wait that runs out keeps this check's place in the queue for ten\n" +
			"minutes and ends with one line (exit 0, nothing run): what is still held, by whom and since when,\n" +
			"'your place N of M in the queue is kept for 10 minutes: run the same command again to go on\n" +
			"waiting'. Run the same command again, in a fresh call, as often as the session allows: it takes up\n" +
			"the same place, not the back of the queue (mw-gq6.939). --wait 40m (or --background, which gives it\n" +
			"40 minutes) is the long wait for a call that is not cut off: a wait that outlasts it ends with one\n" +
			"line saying the wait limit was reached and to hand back at once, with no place kept.\n" +
			"A foreground check that gets the gate lock when the rig's usual gate on this host (the vault's gate\n" +
			"times) would not end inside the call's budget (540 seconds of the default 600) does not start the gate: it gives the lock back,\n" +
			"keeps its place for ten minutes and says the gate needs about so long and only so long is left in\n" +
			"this call, to run the same command again (mw-gq6.940); a gate longer than a whole call is started.\n" +
			"--no-slot skips both waits. mw check --part N does not take the gate lock.\n\n" +
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
			"Two of the checks read the branch's diff, before the formula and the tests: a file the branch adds that\n" +
			"is build output or very large (an executable at the repo root, over 2 MB outside testdata), and a\n" +
			"git+ssh:// line the branch adds to any package-lock.json (mw-gq6.740, mw-fof4qr). npm writes one when it\n" +
			"locks a github:<owner>/<repo>#<sha> dependency, and a host with no GitHub SSH key cannot npm ci it;\n" +
			"the refusal quotes the line(s) and the one fix, sed -i 's#git+ssh://git@github.com/#git+https://github.com/#g'\n" +
			"<file>. Lines the target branch already holds are not the story's and are not refused.\n\n" +
			"It does not read the session's result, which is not written\n" +
			"until the session ends, and it does not try the merge, so a branch that passes here can still be\n" +
			"stopped by a conflict or by the other host's work. The commits are counted against the target\n" +
			"branch as the rig last saw the remote's.\n\n" +
			"A rig whose whole gate outruns a foreground tool call's 600 seconds (the default cap) is named as parts in the host's\n" +
			"config file (the [test_parts.<rig>] tables, mw-gq6.567). mw check <story> --part N then runs part N\n" +
			"alone, after the same wait for a calm host a whole check makes (up to nine minutes, ending in one line\n" +
			"naming the load and the part to run again), and keeps what it said at the branch's tip. A part that failed while\n" +
			"the host was at twice its cores or more is run once more when it has calmed, and only that second run counts,\n" +
			"as a whole check's tests are, saying it ran twice (mw-gq6.1060);\n" +
			"run it once for each part, each as a call of its own, and then mw check <story> with no --part: that\n" +
			"runs none of the gate again, it reads the parts' records and reports the whole gate green, or names\n" +
			"each part that failed or was not run at this commit, with the call that runs it. A part that only\n" +
			"runs tools in the worktree (npm ci, npm run typecheck|lint|build, npx vitest, go build|vet|test)\n" +
			"takes no merge slot; any other part waits up to nine minutes for it while a landing holds it, saying\n" +
			"each minute who holds it and since when, and then says to run the same part again, with its place in\n" +
			"the queue kept for ten minutes (mw-gq6.814, mw-gq6.939). A part that passed keeps how long it took,\n" +
			"by part number and host, in the story's run (check-<attempt>-part-<n>.took); a part that waited for\n" +
			"the slot and wins it with less of the call's budget left than that part's usual time on\n" +
			"this host (the median of its last ten runs) is not started: it gives the slot back, keeps its place\n" +
			"and says what it needs and what is left, to run the same part again (mw-gq6.944).\n" +
			"A part that takes no merge slot is run by a process of its own that outlives the call (mw-gq6.1029):\n" +
			"the call waits for it for what is left of its budget (540 seconds), then ends with one line saying the part is\n" +
			"still running and that `mw check <story> --part N` waits for it, which finds the process and does not\n" +
			"start the part again; when the part is done that call says what it said and exits non-zero if it failed.\n" +
			"The changed specs of a whole check kept to a call are run the same way (mw-gq6.988): by a process of\n" +
			"its own, which takes the merge slot and gate lock itself and keeps what the specs said in\n" +
			"check-<attempt>-specs.result. A call that ends before the specs do says where that verdict will be, and\n" +
			"`mw check <story>`, run again, reads it without running the rig's tests or the specs again.\n\n" +
			"A rig whose file says builder_call = \"55m\" (10m to 55m) gives its Builders' sessions one Bash call of that\n" +
			"length (BASH_MAX_TIMEOUT_MS in their environment, mw-t5shinh.14). A check that finds that variable\n" +
			"larger than the default 600 seconds spends the call less 50 seconds: every wait above (the slot, the\n" +
			"gate lock, a calm host, a part or the specs still running) may take as much, and the 540 seconds and\n" +
			"nine minutes are what it keeps to when the variable is unset. A place is kept ten minutes either way.\n\n" +
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
					Starter: checkStarter{noSlot: noSlot, wait: checkBackgroundWait(cmd)},
					Out:     cmd.OutOrStdout(),
				}.Run(cmd.Context(), args[0])
				return err
			}
			rigs, err := config.Rigs()
			if err != nil {
				return err
			}
			if partWorker && part < 1 {
				return fmt.Errorf("--part-worker runs a part, so it needs --part N")
			}
			if specsWorker && (part > 0 || partWorker) {
				return fmt.Errorf("--specs-worker runs the changed specs of a whole check, so it takes no --part")
			}

			var slot application.MergeSlot
			var gateLock application.GateLock
			var gateExpected func(ctx context.Context, rigName string) time.Duration
			var partExpected func(ctx context.Context, rigName string, part int) time.Duration
			if !noSlot {
				gateLock = hostGateLock()
				bench, err := config.Benchmark()
				if err != nil {
					return err
				}
				book := vault.Benchmarks{Vault: mwVault(dir, host)}
				gateExpected = func(ctx context.Context, rigName string) time.Duration {
					history, err := book.Recent(ctx)
					if err != nil {
						return 0
					}
					return application.GateExpectedFor(history, rigName, host, benchmarkLimits(bench))
				}
				partExpected = func(ctx context.Context, rigName string, n int) time.Duration {
					history, err := vault.PartTimes{Vault: mwVault(dir, host)}.RecentParts(ctx)
					if err != nil {
						return 0
					}
					return application.GatePartExpectedFor(history, rigName, host, n, benchmarkLimits(bench))
				}
				slot = rig.NewSlots(
					gateScaledSlotWait(vault.Benchmarks{Vault: mwVault(dir, host)}, rigs, host, benchmarkLimits(bench)),
					rig.WithSlotNotice(func(said string) { fmt.Fprint(cmd.ErrOrStderr(), said) }))
			}
			worktrees := rig.New()
			var modes application.TranscriptModes
			if projects, err := claude.DefaultProjectsRoot(); err == nil {
				modes = claude.NewTranscripts(projects)
			}
			checks, err := landingChecks(dir, gateCommands{parts: true})
			if err != nil {
				return err
			}
			// The call this check is run in: the harness's 600 seconds, or the longer
			// one the Builder's rig gave it (BASH_MAX_TIMEOUT_MS, mw-t5shinh.14).
			call := checkCallLimits()
			waitLimit, handBackAtOnce, keepPlace := checkWaitLimit(cmd, part, call)
			// Only a foreground check is cut off with its call: a whole check given
			// --wait and --background are the long wait of a call that is not
			// (mw-gq6.940); a --part is always a foreground call (mw-gq6.944).
			var callBudget time.Duration
			if keepPlace > 0 {
				callBudget = call.Budget
			}
			k := application.Check{
				Tracker:        mwGateway(dir, host),
				Landing:        worktrees,
				Checks:         checks,
				SpecChecks:     checks,
				NamedChecks:    checks,
				Changes:        worktrees,
				Slot:           slot,
				GateLock:       gateLock,
				Parts:          checks,
				Records:        mwVault(dir, host),
				Part:           part,
				PartSlotWait:   waitLimit,
				HandBackAtOnce: handBackAtOnce,
				KeepPlace:      keepPlace,
				CallBudget:     callBudget,
				GateExpected:   gateExpected,
				PartExpected:   partExpected,
				Load:           hostload.Proc{},
				Modes:          modes,
				Host:           host,
				Rigs:           rigs,
				Out:            cmd.OutOrStdout(),
			}
			if !logIsStdout {
				k.Logs = mwVault(dir, host)
			}
			// A --part that takes no merge slot is run by a process of its own, so a
			// part that outruns this call goes on (mw-gq6.1029).
			k.PartWorker = partWorker
			if part > 0 && !partWorker {
				k.PartStarter = partProcess{}
			}
			// The changed specs of a whole check kept to this call are run by a process
			// of its own too, so that specs that outrun the call keep their verdict
			// (mw-gq6.988).
			k.SpecsWorker = specsWorker
			if part == 0 && !specsWorker && callBudget > 0 {
				k.SpecsStarter = specsProcess{noSlot: noSlot}
			}
			_, err = k.Run(cmd.Context(), args[0])
			return err
		},
	}
	cmd.Flags().BoolVar(&background, "background", false, "start the check detached, writing its whole output to runs/<story>/check-<attempt>.log in the vault, print that log's path and return at once")
	cmd.Flags().BoolVar(&logIsStdout, "log-is-stdout", false, "the output of this check is already being written to the story's log (set by --background)")
	_ = cmd.Flags().MarkHidden("log-is-stdout")
	cmd.Flags().BoolVar(&partWorker, "part-worker", false, "run part N alone as the process a --part call started for it: no wait for a calm host or the slot (set by mw check --part)")
	_ = cmd.Flags().MarkHidden("part-worker")
	cmd.Flags().BoolVar(&specsWorker, "specs-worker", false, "run the rig's changed specs alone as the process a whole check started for them: waits for the merge slot and gate lock itself, and keeps what the specs said (set by mw check)")
	_ = cmd.Flags().MarkHidden("specs-worker")
	cmd.Flags().IntVar(&part, "part", 0, "run part N of the rig's gate alone, for a rig whose host names its gate as parts, and keep what it said; a check with no --part then reports the whole gate from the parts' records")
	cmd.Flags().Duration("wait", CheckWait, "the most a check queues for the rig's merge slot, this host's gate lock and a calm host together: nine minutes in the foreground (the call's budget in a rig's longer call), ending with the place kept and a line to run it again; a whole check given this (or --background, 40m) ends with the wait limit reached and a line to hand back")
	cmd.Flags().BoolVar(&noSlot, "no-slot", false, "run the rig's tests without taking the rig's merge slot, so without waiting for a close-out's gate")
	return cmd
}

// CheckWait is how long a whole mw check queues for the host's gate lock, the
// rig's merge slot and a calm host together when it is given --wait or started
// with --background: past the longest gate a Builder is likely to queue behind
// (mw-gq6.938), where nine minutes handed the check back to be tried again at the
// back of the queue. A foreground call is not given it (mw-gq6.939): the tool that
// runs it is cut off at 600 seconds (its default cap) with no words.
const CheckWait = 40 * time.Minute

// checkWaitLimit is the limit of the check's waits, whether a wait that runs out
// tells the Builder to hand back at once, and how long its place in the queue is
// kept when it does not.
//
// A foreground check, whole or a --part N, waits the nine minutes it has always
// had, under the 600 seconds a Builder's tool call is cut off at by default (or,
// when BASH_MAX_TIMEOUT_MS raises that cap, the call's budget, call.Wait); a wait that runs out
// keeps the check's place in the queue for ten minutes and ends in one line saying
// to run the same command again (mw-gq6.939). A whole check given --wait is the
// long wait: it takes that limit and, when it runs out, says the limit was reached
// and to hand back, with no place kept.
func checkWaitLimit(cmd *cobra.Command, part int, call application.CheckCallLimits) (limit time.Duration, handBackAtOnce bool, keep time.Duration) {
	given := cmd.Flags().Changed("wait")
	limit, _ = cmd.Flags().GetDuration("wait")
	if !given {
		limit = call.Wait
	}
	if part == 0 && given {
		return limit, true, 0
	}
	return limit, false, call.Keep
}

// checkCallLimits are the times a foreground check keeps to, from the
// BASH_MAX_TIMEOUT_MS of its own environment: today's when it says nothing, the
// longer call's less 50 seconds when a rig gave its Builders one (builder_call).
func checkCallLimits() application.CheckCallLimits {
	return application.CheckCallLimitsFor(os.Getenv(application.BashMaxTimeoutEnv))
}

// checkBackgroundWait is how long a check started with --background waits: --wait
// when the Builder gave it, else the long forty minutes, since the detached check
// has no tool call to fit.
func checkBackgroundWait(cmd *cobra.Command) time.Duration {
	wait, _ := cmd.Flags().GetDuration("wait")
	return wait
}

// checkStarter implements application.CheckStarter: it runs this same mw as
// `mw check <story>` in a session and process group of its own, so that it
// outlives the tool call that asked, with its output appended to the story's log.
type checkStarter struct {
	noSlot bool
	// wait is --wait for the detached check: the Builder's, else CheckWait.
	wait time.Duration
}

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
	if s.wait > 0 {
		args = append(args, "--wait", s.wait.String())
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

// specsProcess implements application.SpecsStarter: it runs this same mw as
// `mw check <story> --specs-worker` in a session of its own, so that it outlives
// the tool call that asked, with its output appended to the file named. The
// process waits for the slot and gate lock as long as a --wait check does.
type specsProcess struct{ noSlot bool }

func (s specsProcess) Start(_ context.Context, storyID, logPath string) (int, error) {
	self, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("finding mw to run the changed specs with: %w", err)
	}
	log, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return 0, fmt.Errorf("opening %s: %w", logPath, err)
	}
	defer log.Close()
	args := []string{"check", storyID, "--specs-worker", "--wait", CheckWait.String()}
	if s.noSlot {
		args = append(args, "--no-slot")
	}
	// Not exec.CommandContext: the specs are meant to outlive the context they were
	// started in.
	cmd := exec.Command(self, args...)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("starting the changed specs: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return cmd.Process.Pid, nil
}

// Alive says whether the process Start returned is still running: not ended, and
// not a pid some other process has since been given.
func (specsProcess) Alive(pid int) bool { return processAlive(pid, "--specs-worker") }

// partProcess implements application.PartStarter: it runs this same mw as
// `mw check <story> --part N --part-worker` in a session of its own, so that it
// outlives the tool call that asked, with its output appended to the file named.
type partProcess struct{}

func (partProcess) Start(_ context.Context, storyID string, part int, logPath string) (int, error) {
	self, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("finding mw to run the part with: %w", err)
	}
	log, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return 0, fmt.Errorf("opening %s: %w", logPath, err)
	}
	defer log.Close()
	// Not exec.CommandContext: the part is meant to outlive the context it was
	// started in.
	cmd := exec.Command(self, "check", storyID, "--part", strconv.Itoa(part), "--part-worker", "--no-slot")
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("starting the part: %w", err)
	}
	// While this call lives, wait for the process so that it does not stay a
	// zombie that still looks alive; once this call is gone init does.
	go func() { _ = cmd.Wait() }()
	return cmd.Process.Pid, nil
}

// Alive says whether the process Start returned is still running: not ended, and
// not a pid some other process has since been given.
func (partProcess) Alive(pid int) bool { return processAlive(pid, "--part-worker") }

// processAlive says whether pid is running and, where /proc says, still the mw
// that was given flag.
func processAlive(pid int, flag string) bool {
	if err := syscall.Kill(pid, 0); err != nil && !errors.Is(err, syscall.EPERM) {
		return false
	}
	if args, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid)); err == nil {
		return strings.Contains(string(args), flag)
	}
	return true
}
