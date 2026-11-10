package application

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// A part that outruns the Builder's foreground call (mw-gq6.1029). A loaded host
// can take more than the 590 seconds a tool call is given over one part of a split
// gate, and a part cut off with its call keeps nothing. A --part that takes no
// merge slot is therefore run by a process of its own (PartStarter), in a session
// of its own, and the call waits for that process for what is left of the call. A
// part still running when the call ends is said to be, in one line naming the one
// command that waits for it, and that command, run again, finds the process and
// waits for it, not starting the part twice.

const (
	// partRunning is the second line of a part's record while a process is running
	// it: not a result, so the last call counts the part as not run.
	partRunning = "running"
	// partRunPid and partRunStarted name the lines of the record of a part's
	// process (CheckPartRunFileName).
	partRunPid     = "pid"
	partRunStarted = "started"
)

// CheckPartRunFileName is the name of the record of the process that runs part n
// of a story's attempt: its pid and when it began, until a call has told the
// Builder how it ended; CheckPartOutFileName the file the process writes to. Their
// endings keep both apart from the part's own record and the check's log, which
// mw next's trust of a check reads.
func CheckPartRunFileName(attempt, n int) string {
	if attempt < 1 {
		attempt = 1
	}
	return fmt.Sprintf("check-%d-part-%d.run", attempt, n)
}

func CheckPartOutFileName(attempt, n int) string {
	if attempt < 1 {
		attempt = 1
	}
	return fmt.Sprintf("check-%d-part-%d.out", attempt, n)
}

func (k Check) partPoll() time.Duration {
	if k.PartPoll > 0 {
		return k.PartPoll
	}
	return CheckPartPoll
}

// detachesPart says the part is run by a process of its own: there is a way to
// start one, a place for what it writes, and the part takes no merge slot, which
// a process that outlives its call could not go on holding.
func (k Check) detachesPart(part GatePart) bool {
	return k.PartStarter != nil && k.Logs != nil && k.Records != nil && (k.Slot == nil || !PartNeedsSlot(part))
}

// partRun is the process a call started for a part.
type partRun struct {
	pid     int
	started time.Time
}

// partRunOf reads the record of a part's process: the outstanding one, or false
// when none was started or a call has already told how it ended.
func (k Check) partRunOf(ctx context.Context, storyID, name string) (partRun, bool) {
	text, err := k.Records.ReadRunFile(ctx, storyID, name)
	if err != nil {
		return partRun{}, false
	}
	var run partRun
	for _, line := range strings.Split(text, "\n") {
		word, rest, _ := strings.Cut(strings.TrimSpace(line), " ")
		switch word {
		case partRunPid:
			run.pid, _ = strconv.Atoi(rest)
		case partRunStarted:
			run.started, _ = time.Parse(time.RFC3339, rest)
		}
	}
	return run, run.pid > 0
}

// checkPartDetached is checkPart for a part run by a process of its own: it joins
// the outstanding process of the part if there is one, else waits for a calm host
// and starts one, and then waits for it for what is left of the call.
func (k Check) checkPartDetached(ctx context.Context, report CheckReport, c *closeOut, label string, part GatePart, tip string) (CheckReport, error) {
	began := k.callNow()
	runName := CheckPartRunFileName(report.Attempt, k.Part)
	run, outstanding := k.partRunOf(ctx, c.id, runName)
	if !outstanding {
		wait := time.Now()
		left := func() time.Duration { return max((k.partSlotWait() - time.Since(wait)).Round(time.Millisecond), 0) }
		if _, err := k.calmForPart(ctx, c, report.Attempt, k.Part, label, left); err != nil {
			return report, fmt.Errorf("checking %s: %w", c.id, err)
		}
		var err error
		if run, err = k.startPart(ctx, report, c, label, tip); err != nil {
			return report, fmt.Errorf("checking %s: %w", c.id, err)
		}
	}
	return report, k.waitForPart(ctx, report, c, label, run, began)
}

// startPart starts the process of the part. Its record is made to say that the
// part is running, so that an old result at this commit is not read as the new
// run's, and its output file is emptied.
func (k Check) startPart(ctx context.Context, report CheckReport, c *closeOut, label, tip string) (partRun, error) {
	if tip == "" {
		tip = partNoTip
	}
	marker := fmt.Sprintf("%s %s\n%s\n", partTipWord, partNoTip, partRunning)
	if _, err := k.Records.PutRunFile(ctx, c.id, CheckPartFileName(report.Attempt, k.Part), marker); err != nil {
		return partRun{}, fmt.Errorf("%s could not be started: %w", label, err)
	}
	outName := CheckPartOutFileName(report.Attempt, k.Part)
	if _, err := k.Logs.PutRunFile(ctx, c.id, outName, ""); err != nil {
		return partRun{}, fmt.Errorf("%s could not be started: the file it writes to could not be made: %w", label, err)
	}
	pid, err := k.PartStarter.Start(ctx, c.id, k.Part, k.Logs.RunFile(c.id, outName))
	if err != nil {
		return partRun{}, fmt.Errorf("%s could not be started: %w", label, err)
	}
	run := partRun{pid: pid, started: time.Now()}
	record := fmt.Sprintf("%s %d\n%s %s\n", partRunPid, run.pid, partRunStarted, run.started.UTC().Format(time.RFC3339))
	if _, err := k.Records.PutRunFile(ctx, c.id, CheckPartRunFileName(report.Attempt, k.Part), record); err != nil {
		return partRun{}, fmt.Errorf("%s was started, but its process could not be kept in the run, so no later call can wait for it: %w", label, err)
	}
	if k.Out != nil {
		fmt.Fprintf(k.Out, "  note    %s started on %s as a process of its own, which goes on if this call ends\n", label, k.Host)
	}
	return run, nil
}

// waitForPart waits for the part's process to end, no longer than what is left of
// the call, and then says how it ended, or that it is still running and the one
// command that waits for it.
func (k Check) waitForPart(ctx context.Context, report CheckReport, c *closeOut, label string, run partRun, began time.Time) error {
	budget := k.CallBudget
	if budget <= 0 {
		budget = CheckCallBudget
	}
	fit := &callFit{began: began, budget: budget, now: k.CallNow}
	tick := time.NewTicker(k.gateNoticeEvery())
	defer tick.Stop()
	for {
		if !k.PartStarter.Alive(run.pid) {
			return k.partEnded(ctx, report, c, label)
		}
		if fit.left() <= 0 {
			return errors.New(k.partStillRunningLine(ctx, c, label, run))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			if k.Out != nil {
				fmt.Fprintf(k.Out, "  note    %s is running on %s, since %s (%s ago); %s left in this call\n",
					label, k.Host, run.started.UTC().Format("15:04:05Z"), time.Since(run.started).Round(time.Second), gateWaited(fit.left()))
			}
		case <-time.After(k.partPoll()):
		}
	}
}

// partStillRunningLine is the one line a call ends with when its time ran out
// before the part did: the part is not failed and not stopped, and the command
// that waits for it is the one that was run.
func (k Check) partStillRunningLine(ctx context.Context, c *closeOut, label string, run partRun) string {
	usually := ""
	if k.PartExpected != nil {
		if expected := k.PartExpected(ctx, c.path.Rig, k.Part); expected > 0 {
			usually = fmt.Sprintf(", usually about %s", gateWaited(expected))
		}
	}
	return fmt.Sprintf("%s is still running on %s (started %s ago%s) and goes on without this call: wait for it with `mw check %s --part %d`, which does not start it again",
		label, k.Host, time.Since(run.started).Round(time.Second), usually, c.id, k.Part)
}

// partEnded tells how the part's process ended, from what it kept: what it said,
// and an error when it did not pass. The process is then told of, so that the next
// call starts the part afresh rather than reading this result again.
func (k Check) partEnded(ctx context.Context, report CheckReport, c *closeOut, label string) error {
	defer func() {
		_, _ = k.Records.PutRunFile(ctx, c.id, CheckPartRunFileName(report.Attempt, k.Part), "told\n")
	}()
	if said, err := k.Records.ReadRunFile(ctx, c.id, CheckPartOutFileName(report.Attempt, k.Part)); err == nil && k.Out != nil {
		fmt.Fprint(k.Out, said)
	}
	record, err := k.Records.ReadRunFile(ctx, c.id, CheckPartFileName(report.Attempt, k.Part))
	lines := strings.Split(record, "\n")
	if err != nil || len(lines) < 2 {
		return fmt.Errorf("checking %s: %s ended without keeping a result (its process was stopped?): run the same part again with `mw check %s --part %d` to start it again", c.id, label, c.id, k.Part)
	}
	switch strings.TrimSpace(lines[1]) {
	case partPassed:
		return nil
	case partFailed:
		return fmt.Errorf("checking %s: %s did not pass, so mw next would not land it", c.id, label)
	}
	return fmt.Errorf("checking %s: %s ended without keeping a result (its process was stopped?): run the same part again with `mw check %s --part %d` to start it again", c.id, label, c.id, k.Part)
}
