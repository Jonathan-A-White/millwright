package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// The changed specs of a whole check that outrun the Builder's foreground call
// (mw-gq6.988). A rig's changed specs can take 25 minutes on a loaded host, and the
// call a Builder runs mw check in is cut off at 600 seconds (the default cap), with the specs and
// their verdict. A foreground whole check therefore hands the specs to a process of
// its own (SpecsStarter), as a --part that takes no merge slot is (mw-gq6.1029),
// and waits for it for what is left of the call. The process takes the rig's merge
// slot and the host's gate lock itself, as the check would have, and keeps what the
// specs said as a record for the commit it ran at. A call that ends before the
// specs do ends in one line: where the verdict will be, and that the same
// `mw check <story>` reads it without running the tests or the specs again.
// A check that is not kept to a call (--wait, --background) runs the specs itself.

const (
	specsPassed  = "passed"
	specsFailed  = "failed"
	specsNotRun  = "notrun"
	specsError   = "error"
	specsRunning = "running"
)

// CheckSpecsPoll is how often a call that waits for the specs' process looks at it.
const CheckSpecsPoll = 500 * time.Millisecond

// SpecsStarter runs a story's changed specs as a process of its own, in a session
// of its own so that it outlives the tool call that asked, with everything it prints
// appended to the file at logPath, and says whether such a process is still running.
// Start returns once the process has started, not when the specs are done.
type SpecsStarter interface {
	Start(ctx context.Context, storyID, logPath string) (pid int, err error)
	Alive(pid int) bool
}

// CheckSpecsRunFileName is the name of the record of the process that runs a story's
// changed specs in the attempt: its pid, when it began and at which commit, until a
// call has told how it ended; CheckSpecsResultFileName the record of what the specs
// said, and CheckSpecsOutFileName what the process printed. Their endings keep all
// three apart from the check's log, which mw next's trust of a check reads.
func CheckSpecsRunFileName(attempt int) string {
	return fmt.Sprintf("check-%d-specs.run", max(attempt, 1))
}

func CheckSpecsResultFileName(attempt int) string {
	return fmt.Sprintf("check-%d-specs.result", max(attempt, 1))
}

func CheckSpecsOutFileName(attempt int) string {
	return fmt.Sprintf("check-%d-specs.out", max(attempt, 1))
}

// specsKeptLines is how much of what the specs printed their record keeps.
const specsKeptLines = 1000

// specsRecord is what the changed specs said, at the commit they ran at.
type specsRecord struct {
	Tip     string   `json:"tip"`
	Result  string   `json:"result"`
	Command string   `json:"command,omitempty"`
	Specs   []string `json:"specs,omitempty"`
	LeftOut []string `json:"left_out,omitempty"`
	Output  string   `json:"output,omitempty"`
	Error   string   `json:"error,omitempty"`
}

func (r specsRecord) checked() SpecsChecked {
	return SpecsChecked{
		Checked: Checked{Command: r.Command, Passed: r.Result == specsPassed, NotRun: r.Result == specsNotRun, Output: r.Output},
		Specs:   r.Specs,
		LeftOut: r.LeftOut,
	}
}

func (k Check) specsPoll() time.Duration {
	if k.SpecsPoll > 0 {
		return k.SpecsPoll
	}
	return CheckSpecsPoll
}

// detachesSpecs says a whole check hands its changed specs to a process of its own:
// there is a way to start one and a place for what it keeps, and the check is kept
// to a Builder's call, which only a foreground one is.
func (k Check) detachesSpecs() bool {
	return k.SpecsStarter != nil && !k.SpecsWorker && k.CallBudget > 0 && k.Logs != nil && k.Records != nil && k.SpecChecks != nil
}

func (k Check) putSpecsRecord(ctx context.Context, storyID string, attempt int, rec specsRecord) error {
	text, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = k.Records.PutRunFile(ctx, storyID, CheckSpecsResultFileName(attempt), string(text)+"\n")
	return err
}

// specsRecordOf reads what the specs last said, false when nothing was kept.
func (k Check) specsRecordOf(ctx context.Context, storyID string, attempt int) (specsRecord, bool) {
	text, err := k.Records.ReadRunFile(ctx, storyID, CheckSpecsResultFileName(attempt))
	if err != nil {
		return specsRecord{}, false
	}
	var rec specsRecord
	if json.Unmarshal([]byte(text), &rec) != nil {
		return specsRecord{}, false
	}
	return rec, true
}

// tipWord is a commit as a record keeps it: "-" when the worktree held more.
func tipWord(tip string) string {
	if tip == "" {
		return partNoTip
	}
	return tip
}

// checkSpecsWorker is the process a call started for the changed specs: it takes
// the rig's merge slot and the host's gate lock as the whole check would, runs the
// specs in the story's worktree and keeps what they said. It is mw check <story>
// --specs-worker, and prints nothing of the check's own verdict.
func (k Check) checkSpecsWorker(ctx context.Context, report CheckReport, c *closeOut) (CheckReport, error) {
	if k.SpecChecks == nil || k.Records == nil {
		return report, fmt.Errorf("checking %s: the changed specs need a command to run and a place to keep what they said", c.id)
	}
	k.waiting = &waitingLine{}
	tip := k.cleanTip(ctx, c)
	rec := specsRecord{Tip: tipWord(tip)}
	keep := func(rec specsRecord) error {
		if err := k.putSpecsRecord(ctx, c.id, report.Attempt, rec); err != nil {
			return fmt.Errorf("checking %s: what the changed specs said could not be kept in the run: %w", c.id, err)
		}
		return nil
	}
	if k.Slot != nil {
		turn, err := k.takeForTests(ctx, c, wholeTry(c, "the rig's changed specs", CheckSpecsHeldFileName(report.Attempt)), k.partSlotWait(), nil, nil)
		if err != nil {
			rec.Result, rec.Error = specsError, err.Error()
			if kerr := keep(rec); kerr != nil {
				return report, kerr
			}
			return report, err
		}
		defer turn.Holding.Release(ctx)
	}
	remote := k.Remote
	if remote == "" {
		remote = DefaultRemote
	}
	run, err := k.SpecChecks.RunSpecs(ctx, c.path.Rig, c.worktree, StartPoint(remote, c.target))
	if after, herr := k.Landing.RefHead(ctx, c.rigDir, c.branch); tip != "" && (herr != nil || after != tip) {
		rec.Tip = partNoTip
	}
	rec.Command, rec.Specs, rec.LeftOut = run.Command, run.Specs, run.LeftOut
	rec.Output = RecentLines(run.Output, specsKeptLines)
	switch {
	case err != nil:
		rec.Result, rec.Error = specsError, err.Error()
	case run.NotRun:
		rec.Result = specsNotRun
	case run.Passed, len(run.Specs) == 0:
		rec.Result = specsPassed
	default:
		rec.Result = specsFailed
	}
	if kerr := keep(rec); kerr != nil {
		return report, kerr
	}
	return report, err
}

// detachedSpecs is the changed-specs command of a foreground whole check: it runs
// nothing itself but starts the process that does, or takes up the one that is
// running, and waits for it for what is left of the call.
type detachedSpecs struct {
	k       Check
	c       *closeOut
	attempt int
	// tip is the commit the check runs at, empty when its worktree holds more.
	tip string
	// began is when the call began, and handedBack is told when the call ends on
	// specs that are still running.
	began      time.Time
	handedBack func()
}

// RunSpecs implements SpecChecks.
func (d detachedSpecs) RunSpecs(ctx context.Context, _, _, _ string) (SpecsChecked, error) {
	k, c := d.k, d.c
	// A pass kept for this very commit stands: the specs are not run twice for it.
	if rec, ok := k.specsRecordOf(ctx, c.id, d.attempt); ok && d.tip != "" && rec.Tip == d.tip && rec.Result == specsPassed {
		return rec.checked(), nil
	}
	for try := 0; try < 2; try++ {
		run, outstanding := k.partRunOf(ctx, c.id, CheckSpecsRunFileName(d.attempt))
		if !outstanding {
			var err error
			if run, err = d.start(ctx); err != nil {
				return SpecsChecked{}, err
			}
		}
		if line := d.wait(ctx, run); line != "" {
			k.waiting.set(line)
			if d.handedBack != nil {
				d.handedBack()
			}
			return SpecsChecked{}, errors.New(line)
		}
		_, _ = k.Records.PutRunFile(ctx, c.id, CheckSpecsRunFileName(d.attempt), "told\n")
		rec, ok := k.specsRecordOf(ctx, c.id, d.attempt)
		if !ok || rec.Result == specsRunning {
			return SpecsChecked{}, fmt.Errorf("the changed specs ended without keeping a result (their process was stopped?): run the same check again with `mw check %s` to start them again", c.id)
		}
		if rec.Tip != tipWord(d.tip) {
			// They ran at another commit than this one: they say nothing of it.
			if k.Out != nil {
				fmt.Fprintf(k.Out, "  note    the changed specs that were running were of another commit than %s, so they are run again\n", shortCommit(tipWord(d.tip)))
			}
			continue
		}
		if rec.Result == specsError {
			return SpecsChecked{}, errors.New(rec.Error)
		}
		return rec.checked(), nil
	}
	return SpecsChecked{}, fmt.Errorf("the changed specs were run again for a commit that moved under them twice: run the same check again with `mw check %s`", c.id)
}

// start starts the process of the changed specs. Their record is made to say that
// they are running, so that an old result is not read as the new run's, and the file
// the process writes to is emptied.
func (d detachedSpecs) start(ctx context.Context) (partRun, error) {
	k, c := d.k, d.c
	if err := k.putSpecsRecord(ctx, c.id, d.attempt, specsRecord{Tip: partNoTip, Result: specsRunning}); err != nil {
		return partRun{}, fmt.Errorf("the changed specs could not be started: %w", err)
	}
	outName := CheckSpecsOutFileName(d.attempt)
	if _, err := k.Logs.PutRunFile(ctx, c.id, outName, ""); err != nil {
		return partRun{}, fmt.Errorf("the changed specs could not be started: the file they write to could not be made: %w", err)
	}
	pid, err := k.SpecsStarter.Start(ctx, c.id, k.Logs.RunFile(c.id, outName))
	if err != nil {
		return partRun{}, fmt.Errorf("the changed specs could not be started: %w", err)
	}
	run := partRun{pid: pid, started: time.Now()}
	record := fmt.Sprintf("%s %d\n%s %s\n", partRunPid, run.pid, partRunStarted, run.started.UTC().Format(time.RFC3339))
	if _, err := k.Records.PutRunFile(ctx, c.id, CheckSpecsRunFileName(d.attempt), record); err != nil {
		return partRun{}, fmt.Errorf("the changed specs were started, but their process could not be kept in the run, so no later call can wait for it: %w", err)
	}
	if k.Out != nil {
		fmt.Fprintf(k.Out, "  note    the rig's changed specs started on %s as a process of its own, which goes on if this call ends\n", k.Host)
	}
	return run, nil
}

// wait waits for the process to end, no longer than what is left of the call. It
// returns "" when the process has ended, else the one line the call ends in: the
// specs are not failed and not stopped, the verdict will be in the record, and the
// same check reads it.
func (d detachedSpecs) wait(ctx context.Context, run partRun) string {
	k, c := d.k, d.c
	fit := &callFit{began: d.began, budget: k.CallBudget, now: k.CallNow}
	tick := time.NewTicker(k.gateNoticeEvery())
	defer tick.Stop()
	for {
		if !k.SpecsStarter.Alive(run.pid) {
			return ""
		}
		if fit.left() <= 0 {
			return fmt.Sprintf("the rig's changed specs are still running on %s (started %s ago) and go on without this call: their verdict will be kept in %s (what they print is in %s), and `mw check %s`, run again, reads it without running the rig's tests or the specs again",
				k.Host, time.Since(run.started).Round(time.Second), k.Logs.RunFile(c.id, CheckSpecsResultFileName(d.attempt)), k.Logs.RunFile(c.id, CheckSpecsOutFileName(d.attempt)), c.id)
		}
		select {
		case <-ctx.Done():
			return ctx.Err().Error()
		case <-tick.C:
			if k.Out != nil {
				fmt.Fprintf(k.Out, "  note    the rig's changed specs are running on %s, since %s (%s ago); %s left in this call\n",
					k.Host, run.started.UTC().Format("15:04:05Z"), time.Since(run.started).Round(time.Second), gateWaited(fit.left()))
			}
		case <-time.After(k.specsPoll()):
		}
	}
}
