package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// A part that outruns the Builder's foreground call (mw-gq6.1029). A loaded host
// can take more than the 600 seconds a tool call is given by default over one part of a split
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

// CheckPartHungCeiling is how long a detached part may write nothing before it is
// killed as hung when mw has no usual time for the part on this host; with one the
// ceiling is hungTimes its usual (PartHungCeilingFor, mw-82lb97i.12).
const CheckPartHungCeiling = 20 * time.Minute

// hungTimes is how many times its usual run a part may go without writing.
const hungTimes = 10

// PartHungCeilingFor is how long a part that writes nothing is let run: ten times
// its usual (the median of its last passing runs on this host) when mw has one,
// else fixed, or CheckPartHungCeiling when that is not given.
func PartHungCeilingFor(usual, fixed time.Duration) time.Duration {
	if usual > 0 {
		return hungTimes * usual
	}
	if fixed > 0 {
		return fixed
	}
	return CheckPartHungCeiling
}

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

// CheckPartProgressFileName is the name of the file a part worker's command line
// streams what it prints to as it prints it, so that a part that is slow but at
// work is told from one that is hung by the file growing. It is apart from the
// part's out file, which the call waiting for the part prints whole when it ends.
func CheckPartProgressFileName(attempt, n int) string {
	if attempt < 1 {
		attempt = 1
	}
	return fmt.Sprintf("check-%d-part-%d.prog", attempt, n)
}

type runProgressKey struct{}

// WithRunProgress is ctx with a writer that a runner of a command line copies what
// the command prints to as it prints it (mw-82lb97i.12). A write that fails is not
// the command's failure.
func WithRunProgress(ctx context.Context, w io.Writer) context.Context {
	return context.WithValue(ctx, runProgressKey{}, w)
}

// RunProgressOf is the writer WithRunProgress put in ctx, nil when there is none.
func RunProgressOf(ctx context.Context) io.Writer {
	w, _ := ctx.Value(runProgressKey{}).(io.Writer)
	return w
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
		wait := k.callNow()
		left := func() time.Duration {
			return max((k.partSlotWait() - k.callNow().Sub(wait)).Round(time.Millisecond), 0)
		}
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
	if _, err := k.Logs.PutRunFile(ctx, c.id, CheckPartProgressFileName(report.Attempt, k.Part), ""); err != nil {
		return partRun{}, fmt.Errorf("%s could not be started: the file it writes to could not be made: %w", label, err)
	}
	pid, err := k.PartStarter.Start(ctx, c.id, k.Part, k.Logs.RunFile(c.id, outName))
	if err != nil {
		return partRun{}, fmt.Errorf("%s could not be started: %w", label, err)
	}
	run := partRun{pid: pid, started: k.callNow()}
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
		if idle, ceiling := k.partSilence(ctx, report, c, run); idle > ceiling {
			return k.killHungPart(ctx, report, c, label, run, idle, ceiling)
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
					label, k.Host, run.started.UTC().Format("15:04:05Z"), k.callNow().Sub(run.started).Round(time.Second), gateWaited(fit.left()))
			}
		case <-time.After(k.partPoll()):
		}
	}
}

// partSilence is how long the part's process has written nothing, and the longest
// it may: ten times the part's usual run on this host when mw has one, else the
// fixed ceiling. It writes when it has written to its out file, its streamed
// progress or, if it never has, when it began.
func (k Check) partSilence(ctx context.Context, report CheckReport, c *closeOut, run partRun) (idle, ceiling time.Duration) {
	var usual time.Duration
	if k.PartExpected != nil {
		usual = k.PartExpected(ctx, c.path.Rig, k.Part)
	}
	ceiling = PartHungCeilingFor(usual, k.PartHungAfter)
	last := run.started
	for _, name := range []string{CheckPartOutFileName(report.Attempt, k.Part), CheckPartProgressFileName(report.Attempt, k.Part)} {
		if info, err := os.Stat(k.Logs.RunFile(c.id, name)); err == nil && info.ModTime().After(last) {
			last = info.ModTime()
		}
	}
	return k.callNow().Sub(last), ceiling
}

// killHungPart stops the process of a part that has written nothing past its
// ceiling, with everything it started, keeps the part as hung and tells the call
// so, in one line. The process is then told of, so that the next call starts the
// part afresh (mw-82lb97i.12).
func (k Check) killHungPart(ctx context.Context, report CheckReport, c *closeOut, label string, run partRun, idle, ceiling time.Duration) error {
	if err := k.PartStarter.Kill(run.pid); err != nil {
		return fmt.Errorf("checking %s: %s wrote nothing for %s and could not be stopped: %w", c.id, label, gateWaited(idle), err)
	}
	said := fmt.Sprintf("checking %s: %s hung on %s: no output for %s (more than the %s a part like it is let go), so it was stopped; run the same part again with `mw check %s --part %d` to start it again",
		c.id, label, k.Host, gateWaited(idle), gateWaited(ceiling), c.id, k.Part)
	ctx = context.WithoutCancel(ctx)
	record := fmt.Sprintf("%s %s\n%s\n%s\n", partTipWord, partNoTip, partHung, said)
	_, _ = k.Records.PutRunFile(ctx, c.id, CheckPartFileName(report.Attempt, k.Part), record)
	_, _ = k.Records.PutRunFile(ctx, c.id, CheckPartRunFileName(report.Attempt, k.Part), "told\n")
	return errors.New(said)
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
		label, k.Host, k.callNow().Sub(run.started).Round(time.Second), usually, c.id, k.Part)
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
	case partHandedBack:
		// The worker ended in an error without a result: tell that error, which says
		// what to run, and not that the process was stopped.
		if said := strings.TrimSpace(strings.Join(lines[2:], "\n")); said != "" {
			return errors.New(said)
		}
	case partPassed:
		return nil
	case partFailed:
		return fmt.Errorf("checking %s: %s did not pass, so mw next would not land it", c.id, label)
	}
	return fmt.Errorf("checking %s: %s ended without keeping a result (its process was stopped?): run the same part again with `mw check %s --part %d` to start it again", c.id, label, c.id, k.Part)
}
