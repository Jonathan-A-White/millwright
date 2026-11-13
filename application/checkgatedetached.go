package application

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// A whole check whose rig's gate usually takes longer than the Builder's whole
// call (mw-gq6.1273). cloud8's gate took 12m47s against a call's 540 s, so no call
// could ever fit it, and a foreground check started it anyway: the tool cut the
// call off at 600 s with nothing said, no log and the gate lock held. Such a check
// is therefore run as a process of its own (GateStarter), as the changed specs
// (mw-gq6.988) and the parts (mw-gq6.1029) are: the process is the same whole
// check, not kept to a call (--wait), which takes the rig's merge slot and the
// host's gate lock itself, under the story's key, and keeps its verdict for the
// commit it ran at. The call waits for it for what is left of the call, and a call
// that ends before the process does ends in one line naming the files the verdict
// and the output will be in, and that the same `mw check <story>` waits for it and
// reads the verdict without starting the gate again.
//
// A process that ended having kept that the rig's tests passed at this commit is
// read by the check as a pass kept by an earlier call (keptTests): the rest of the
// check is made afresh in the call, so a formula step closed since is seen. Any
// other verdict at this commit is the call's verdict.

// GateStarter runs a story's whole check as a process of its own, in a session of
// its own so that it outlives the tool call that asked, with everything it prints
// appended to the file at logPath, and says whether such a process is still
// running. Start returns once the process has started, not when the check is done.
type GateStarter interface {
	Start(ctx context.Context, storyID, logPath string) (pid int, err error)
	Alive(pid int) bool
}

// CheckGateRunFileName is the name of the record of the process that runs a
// story's whole check in the attempt: its pid and when it began, until a call has
// told how it ended; CheckGateResultFileName the record of its verdict, and
// CheckGateOutFileName what it printed. Their endings keep all three apart from the
// check's log, which the call writes and mw next's trust of a check reads.
func CheckGateRunFileName(attempt int) string {
	return fmt.Sprintf("check-%d-gate.run", max(attempt, 1))
}

func CheckGateResultFileName(attempt int) string {
	return fmt.Sprintf("check-%d-gate.result", max(attempt, 1))
}

func CheckGateOutFileName(attempt int) string {
	return fmt.Sprintf("check-%d-gate.out", max(attempt, 1))
}

// gateVerdict is the verdict of a whole check run by a process of its own, at the
// commit it ran at.
type gateVerdict struct {
	Tip      string    `json:"tip"`
	Running  bool      `json:"running,omitempty"`
	Commits  int       `json:"commits"`
	Refusals []Refusal `json:"refusals,omitempty"`
	Notes    []string  `json:"notes,omitempty"`
}

// detachesGate says a whole check may hand itself to a process of its own: there
// is a way to start one and a place for what it keeps, and the check is kept to a
// Builder's call, which only a foreground one is.
func (k Check) detachesGate() bool {
	return k.GateStarter != nil && !k.GateWorker && k.CallBudget > 0 && k.Logs != nil && k.Records != nil
}

// outrunsACall says the rig's gate usually takes at least a whole call: no call,
// fresh or not, would see it end.
func (f *callFit) outrunsACall() bool { return f != nil && f.expected >= f.budget }

func (k Check) putGateRecord(ctx context.Context, storyID string, attempt int, rec gateVerdict) error {
	text, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = k.Records.PutRunFile(ctx, storyID, CheckGateResultFileName(attempt), string(text)+"\n")
	return err
}

// gateRecordOf reads the verdict the process last kept, false when none was.
func (k Check) gateRecordOf(ctx context.Context, storyID string, attempt int) (gateVerdict, bool) {
	text, err := k.Records.ReadRunFile(ctx, storyID, CheckGateResultFileName(attempt))
	if err != nil {
		return gateVerdict{}, false
	}
	var rec gateVerdict
	if json.Unmarshal([]byte(text), &rec) != nil {
		return gateVerdict{}, false
	}
	return rec, true
}

// detachedGate is a foreground whole check whose gate outruns a call: it joins the
// outstanding process of the check if there is one, else starts one when the gate
// usually takes at least a whole call, and waits for it for what is left of the
// call. done says the call's report is made: the one line the call ends in, or the
// verdict the process kept. When it is not done the check goes on in the call as
// before: there is no process and the gate fits a call, or the process ended
// having kept that the tests passed at this commit.
func (k Check) detachedGate(ctx context.Context, report CheckReport, c *closeOut, tip string, fit *callFit) (CheckReport, bool, error) {
	runName := CheckGateRunFileName(report.Attempt)
	for try := 0; try < 2; try++ {
		run, outstanding := k.partRunOf(ctx, c.id, runName)
		if !outstanding {
			if !fit.outrunsACall() {
				return report, false, nil
			}
			var err error
			if run, err = k.startGate(ctx, c, report.Attempt, fit); err != nil {
				return report, true, fmt.Errorf("checking %s: %w", c.id, err)
			}
		}
		if line := k.waitForGate(ctx, c, report.Attempt, run, fit); line != "" {
			k.waiting.set(line)
			report.Waiting = line
			return report, true, nil
		}
		_, _ = k.Records.PutRunFile(ctx, c.id, runName, "told\n")
		if k.keptTests(ctx, c.id, report.Attempt, tip) != "" {
			return report, false, nil
		}
		rec, ok := k.gateRecordOf(ctx, c.id, report.Attempt)
		if !ok || rec.Running {
			return report, true, fmt.Errorf("checking %s: the check run as a process of its own ended without keeping a verdict (was it stopped?); what it printed is in %s: run `mw check %s` again to start it again",
				c.id, k.Logs.RunFile(c.id, CheckGateOutFileName(report.Attempt)), c.id)
		}
		if rec.Tip != tipWord(tip) {
			// It ran at another commit than this one: it says nothing of it.
			if k.Out != nil {
				fmt.Fprintf(k.Out, "  note    the check that was running as a process of its own was of another commit than %s, so it is run again\n", shortCommit(tipWord(tip)))
			}
			continue
		}
		report.Commits, report.Refusals, report.Notes = rec.Commits, rec.Refusals, rec.Notes
		report.Notes = append(report.Notes, fmt.Sprintf("the rig's gate ran as a process of its own; what it printed is in %s", k.Logs.RunFile(c.id, CheckGateOutFileName(report.Attempt))))
		if tip != "" && rec.Tip == tip {
			report.Tip = tip
		}
		return report, true, nil
	}
	return report, true, fmt.Errorf("checking %s: the check was run again for a commit that moved under it twice: run the same check again with `mw check %s`", c.id, c.id)
}

// startGate starts the process of the whole check. Its record is made to say that
// it is running, so that an old verdict is not read as the new run's, and the file
// the process writes to is emptied.
func (k Check) startGate(ctx context.Context, c *closeOut, attempt int, fit *callFit) (partRun, error) {
	if err := k.putGateRecord(ctx, c.id, attempt, gateVerdict{Tip: partNoTip, Running: true}); err != nil {
		return partRun{}, fmt.Errorf("the check could not be started as a process of its own: %w", err)
	}
	outName := CheckGateOutFileName(attempt)
	if _, err := k.Logs.PutRunFile(ctx, c.id, outName, ""); err != nil {
		return partRun{}, fmt.Errorf("the check could not be started as a process of its own: the file it writes to could not be made: %w", err)
	}
	pid, err := k.GateStarter.Start(ctx, c.id, k.Logs.RunFile(c.id, outName))
	if err != nil {
		return partRun{}, fmt.Errorf("the check could not be started as a process of its own: %w", err)
	}
	run := partRun{pid: pid, started: k.callNow()}
	record := fmt.Sprintf("%s %d\n%s %s\n", partRunPid, run.pid, partRunStarted, run.started.UTC().Format(time.RFC3339))
	if _, err := k.Records.PutRunFile(ctx, c.id, CheckGateRunFileName(attempt), record); err != nil {
		return partRun{}, fmt.Errorf("the check was started as a process of its own, but its process could not be kept in the run, so no later call can wait for it: %w", err)
	}
	if k.Out != nil {
		fmt.Fprintf(k.Out, "  note    the rig's gate usually takes about %s on %s, longer than this call's %s, so the check runs as a process of its own, which takes the merge slot and gate lock itself and goes on if this call ends\n",
			gateWaited(fit.expected), k.Host, gateWaited(fit.budget))
	}
	return run, nil
}

// waitForGate waits for the process to end, no longer than what is left of the
// call. It returns "" when the process has ended, else the one line the call ends
// in: the check is not failed and not stopped, its verdict will be in the record,
// and the same check reads it.
func (k Check) waitForGate(ctx context.Context, c *closeOut, attempt int, run partRun, fit *callFit) string {
	left := &callFit{began: k.began, budget: k.CallBudget, now: k.CallNow}
	tick := time.NewTicker(k.gateNoticeEvery())
	defer tick.Stop()
	for {
		if !k.GateStarter.Alive(run.pid) {
			return ""
		}
		if left.left() <= 0 {
			usually := ""
			if fit != nil {
				usually = fmt.Sprintf(", usually about %s", gateWaited(fit.expected))
			}
			return fmt.Sprintf("the check of %s is still running on %s as a process of its own (started %s ago%s) and goes on without this call: its verdict will be kept in %s (what it prints is in %s), and `mw check %s`, run again, waits for it and reads the verdict without running the rig's gate again",
				c.id, k.Host, k.callNow().Sub(run.started).Round(time.Second), usually,
				k.Logs.RunFile(c.id, CheckGateResultFileName(attempt)), k.Logs.RunFile(c.id, CheckGateOutFileName(attempt)), c.id)
		}
		select {
		case <-ctx.Done():
			return ctx.Err().Error()
		case <-tick.C:
			if k.Out != nil {
				fmt.Fprintf(k.Out, "  note    the check of %s is running on %s as a process of its own, since %s (%s ago); %s left in this call\n",
					c.id, k.Host, run.started.UTC().Format("15:04:05Z"), k.callNow().Sub(run.started).Round(time.Second), gateWaited(left.left()))
			}
		case <-time.After(k.specsPoll()):
		}
	}
}

// lastSpecs remembers the changed specs' last run, for the gate worker to keep.
type lastSpecs struct {
	inner SpecChecks
	mu    sync.Mutex
	ran   bool
	run   SpecsChecked
	err   error
}

func (l *lastSpecs) RunSpecs(ctx context.Context, rig, dir, base string) (SpecsChecked, error) {
	run, err := l.inner.RunSpecs(ctx, rig, dir, base)
	l.mu.Lock()
	l.ran, l.run, l.err = true, run, err
	l.mu.Unlock()
	return run, err
}

// keepGateVerdict is what the process of a whole check keeps when it is done: that
// the rig's tests passed at the tip, and the changed specs too, as an earlier call
// of the check would have, so that the call that reads it does not run them again;
// and its verdict, for a call to read when the tests did not pass or the tip is not
// known.
func (k Check) keepGateVerdict(ctx context.Context, c *closeOut, report CheckReport, tests Checked, specs *lastSpecs) error {
	if k.Records == nil {
		return nil
	}
	if report.Tip != "" {
		k.keepTests(ctx, c, report.Attempt, report.Tip, tests)
		if specs != nil {
			specs.mu.Lock()
			ran, run, err := specs.ran, specs.run, specs.err
			specs.mu.Unlock()
			if ran && err == nil && !run.NotRun && (run.Passed || len(run.Specs) == 0) {
				_ = k.putSpecsRecord(ctx, c.id, report.Attempt, specsRecord{Tip: report.Tip, Result: specsPassed, Command: run.Command, Specs: run.Specs, LeftOut: run.LeftOut, Output: RecentLines(run.Output, specsKeptLines)})
			}
		}
	}
	rec := gateVerdict{Tip: tipWord(report.Tip), Commits: report.Commits, Refusals: report.Refusals, Notes: report.Notes}
	if err := k.putGateRecord(ctx, c.id, report.Attempt, rec); err != nil {
		return fmt.Errorf("checking %s: the verdict of the check could not be kept in the run: %w", c.id, err)
	}
	return nil
}
