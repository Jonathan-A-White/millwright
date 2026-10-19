package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// BatchFixing is the one fixer a batch was given (plans/0117, Land Rush 5), and
// what came of it.
type BatchFixing struct {
	// Name is the fixer's session, and its run's directory under runs/.
	Name string
	// Why is what it was started for: branches set aside, or a red run.
	Why string
	// Tested says the rig's tests had run on the batch before it was started,
	// and Gate is the run it was given.
	Tested bool
	Gate   Checked
	// GateLog is that run as gate.log holds it, the changed specs included.
	GateLog string

	Started bool
	Ended   SessionStatus
	// Result is what the harness reported, when its result could be read.
	Result     SessionResult
	ResultRead bool
	// Commit is where it left the batch.
	Commit string

	// Touched are the stories it said it touched, with the files it named;
	// Causes are what it said made the batch red. Both are its word,
	// recorded as it gave it and never bisected.
	Touched []FixerWord
	Causes  []FixerWord
	// Merged are the stories set aside that it merged into the batch.
	Merged []string

	// Trouble is why it could not be started or did not end well, empty when
	// it ran and ended.
	Trouble string
	// Stopped is the harness's stop for a limit of the factory's
	// (HarnessStopOf) that ended it, or that this host was held for when it
	// was to be started (mw-gq6.472): nil when none. It says nothing of the
	// batch, so the batch is held until the stop's time, not judged after it.
	Stopped *HarnessStop
}

// FixerWord is one line of a fixer's word about one story.
type FixerWord struct {
	Story string
	Said  string
}

// ReadFixerWord reads the lines a fixer ended its last message with: TOUCHED
// <story>: <files> and CAUSE <story>: <sentence>, anywhere in what it said,
// each on a line of its own. Anything else it said is not read.
func ReadFixerWord(said string) (touched, causes []FixerWord) {
	for _, line := range strings.Split(said, "\n") {
		// A line may come as a list item, or in backticks or bold.
		line = strings.Trim(line, " \t`*-")
		for _, kind := range []string{FixerTouched, FixerCause} {
			rest, ok := strings.CutPrefix(line, kind+" ")
			if !ok {
				continue
			}
			story, what, _ := strings.Cut(rest, ":")
			story = strings.TrimSpace(story)
			if story == "" || strings.ContainsAny(story, " \t") {
				continue
			}
			word := FixerWord{Story: story, Said: strings.TrimSpace(what)}
			if kind == FixerTouched {
				touched = append(touched, word)
			} else {
				causes = append(causes, word)
			}
		}
	}
	return touched, causes
}

// mayFix says this mw land may give a batch a fixer: it was asked to, and it
// has a seat boot with a harness and a runner to start one through.
func (b BatchLand) mayFix() bool {
	return b.Fix && b.Next.Runner != nil && b.Next.Boot.Harness != nil
}

// conflicted says a branch of the batch was set aside because it conflicts
// with it: what a fixer can merge. One that is not at the origin it cannot.
func (r BatchReport) conflicted() bool {
	for _, s := range r.SetAside {
		if len(s.Conflicts) > 0 {
			return true
		}
	}
	return false
}

// storiesInOrder is the queue as the batch left it: each story merged with its
// merge, each set aside with why.
func (r BatchReport) storiesInOrder() []BatchStory {
	stories := make([]BatchStory, 0, len(r.Queue))
	for _, s := range r.Queue {
		if merged, ok := r.merged(s.ID); ok {
			stories = append(stories, merged)
		} else if aside, ok := r.setAside(s.ID); ok {
			stories = append(stories, aside)
		} else {
			stories = append(stories, s)
		}
	}
	return stories
}

// fix starts the batch's one fixer in its landing worktree dir, waits for it to
// end, and reads what it left: its word, and which branches set aside it
// merged. It reports whether the batch may go on to be tested; when it may not
// the batch is held and Held says why. A fixer that could not be started holds
// a red batch, and leaves one with branches set aside to land the rest.
func (b BatchLand) fix(ctx context.Context, dir, rigDir string, settings domain.LandingSettings, report *BatchReport, why string) bool {
	n := b.Next
	f := &BatchFixing{Name: BatchFixerName(report.Rig, report.Number), Why: why, Tested: report.Tested, Gate: report.Gate, GateLog: report.GateLog}
	report.Fixer = f
	notStarted := func(trouble string) bool {
		f.Trouble = trouble
		if report.Tested {
			report.Held = fmt.Sprintf("%s failed, and its fixer %s", gateOnTheBatch, trouble)
			return false
		}
		report.Notes = append(report.Notes, fmt.Sprintf("the fixer %s, so what was set aside stays queued", trouble))
		return true
	}

	// A fixer started while the harness holds this host for a limit of the
	// factory's would be stopped by it at once: none is spent on the limit.
	if hold, held := readHarnessHold(ctx, n.Memory, n.Host); held && hold.Until.After(n.now()) {
		f.Stopped = &hold
		return notStarted(fmt.Sprintf("was not started: the harness holds %s for a limit of the factory's, %s, and no fixer is spent on it before then", n.Host, hold))
	}

	spec, err := n.Boot.BatchFix(ctx, BatchFixBrief{
		Rig: report.Rig, Target: report.Target, Number: report.Number, Base: report.Base,
		Stories: report.storiesInOrder(), Tested: report.Tested, Gate: report.Gate, Settings: settings,
	}, dir)
	if err != nil {
		return notStarted(fmt.Sprintf("could not be assembled: %v", err))
	}
	if err := n.Runner.Start(ctx, spec); err != nil {
		return notStarted(fmt.Sprintf("%s could not be started: %v", spec.Name, err))
	}
	f.Started = true
	n.print(fmt.Sprintf("land on %s: %s given its fixer %s (%s); waiting for it to end\n", n.Host, report.Label(), f.Name, why))

	f.Ended, err = n.Runner.Wait(ctx, spec.Name)
	if err != nil {
		f.Trouble = fmt.Sprintf("could not be waited on: %v", err)
		report.Held = fmt.Sprintf("its fixer %s %s", f.Name, f.Trouble)
		return false
	}
	if err := n.Runner.Close(ctx, spec.Name); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the fixer's session %s could not be closed: %v", spec.Name, err))
	}
	b.readFixer(ctx, f, report)
	if stop := f.Stopped; stop != nil {
		// Stopped by the harness, not by its work: what it left is neither
		// tested nor judged, and the batch waits for the stop's time, when it
		// is given a fixer afresh (mw-gq6.472).
		if err := keepHarnessHold(ctx, n.Memory, n.Host, *stop); err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf(
				"the hold on new claims until %s could not be kept in the host's note: %v", stop.Until.UTC().Format(time.RFC3339), err))
		}
		report.Held = fmt.Sprintf("the harness stopped its fixer %s, not its work: %s. No fixer was spent on the batch: "+
			"mw dispatch on %s claims nothing new until then, and the batch is tried again, given a fixer afresh, once that time has come",
			f.Name, stop, n.Host)
		return false
	}
	return b.afterFixer(ctx, dir, rigDir, report)
}

// readFixer reads the result the fixer's harness left, and its word in it. A
// result that is missing or unreadable is trouble, not a hold: the tests on the
// batch are the judge. One the harness ended for a limit of the factory's is
// its Stopped.
func (b BatchLand) readFixer(ctx context.Context, f *BatchFixing, report *BatchReport) {
	printed, err := b.Next.Vault.ReadRunFile(ctx, f.Name, ResultFileName)
	if err != nil {
		f.Trouble = fmt.Sprintf("its result could not be read: %v", err)
		return
	}
	result, err := ReadSessionResult(printed)
	if err != nil {
		f.Trouble = fmt.Sprintf("its result could not be read: %v", err)
		return
	}
	f.Result, f.ResultRead = result, true
	f.Touched, f.Causes = ReadFixerWord(result.Said)
	if !result.Finished() {
		f.Trouble = result.Trouble()
		if stop, stopped := HarnessStopOf(result, b.Next.now()); stopped {
			f.Stopped = &stop
			f.Trouble = "the harness stopped it, not its work: " + stop.String()
		}
	}
	if record := RunRecord(f.Name); record != "" {
		report.Records = append(report.Records, record)
	}
}

// afterFixer looks at the batch as the fixer left it. Changes it did not
// commit, or a story merged before it that the batch no longer holds, hold the
// batch: what would be tested is not what would be pushed, or a story's change
// was dropped. A branch set aside that the batch now holds was merged by it.
func (b BatchLand) afterFixer(ctx context.Context, dir, rigDir string, report *BatchReport) bool {
	n := b.Next
	f := report.Fixer
	left, err := n.Landing.Uncommitted(ctx, dir)
	switch {
	case err != nil:
		report.Held = fmt.Sprintf("what its fixer %s left uncommitted could not be read: %v", f.Name, err)
		return false
	case len(left) > 0:
		report.Held = fmt.Sprintf("its fixer %s left changes uncommitted, so what would be tested is not what would be pushed: %s", f.Name, strings.Join(left, ", "))
		return false
	}
	if f.Commit, err = n.Landing.RefHead(ctx, dir, "HEAD"); err != nil {
		report.Held = fmt.Sprintf("the batch as its fixer %s left it could not be read: %v", f.Name, err)
		return false
	}
	// What the fixer committed is held to the rule every branch is: nothing
	// signed as a machine's work is pushed, whoever made the commit.
	if why := b.signedWhy(ctx, dir, "HEAD", report.Base, "its fixer "+f.Name+"'s batch"); why != "" {
		report.Held = why + ": nothing was pushed"
		return false
	}
	holds := func(s BatchStory) bool {
		ahead, err := n.Landing.Ahead(ctx, rigDir, s.Branch, f.Commit)
		return err == nil && ahead == 0
	}
	for _, s := range report.Merged {
		if !holds(s) {
			report.Held = fmt.Sprintf("its fixer %s left the batch without %s, which was merged into it: a story's change may not be dropped", f.Name, s.ID)
			return false
		}
	}
	kept := report.SetAside[:0]
	for _, s := range report.SetAside {
		if holds(s) {
			s.Merge, s.ByFixer, s.Why = f.Commit, true, ""
			report.Merged = append(report.Merged, s)
			f.Merged = append(f.Merged, s.ID)
			continue
		}
		kept = append(kept, s)
	}
	report.SetAside = kept
	return true
}

// resolvedByFixer says the batch's fixer had a hand in id: it merged it, or
// named it as touched. A fixer of a red batch that named no story had a hand
// in every one, since the batch was red as a whole.
func (r BatchReport) resolvedByFixer(id string) bool {
	f := r.Fixer
	if f == nil || !f.Started {
		return false
	}
	for _, merged := range f.Merged {
		if merged == id {
			return true
		}
	}
	for _, word := range f.Touched {
		if word.Story == id {
			return true
		}
	}
	return len(f.Touched) == 0 && len(f.Merged) == 0 && f.Tested
}

// fixerNotes are what a landed story's ledger line says of the fixer: that it
// resolved the story, and what it said made the batch red, as its word.
func (r BatchReport) fixerNotes(id string) []string {
	if !r.resolvedByFixer(id) {
		return nil
	}
	notes := []string{"resolved by the fixer " + r.Fixer.Name}
	for _, cause := range r.Fixer.Causes {
		if cause.Story == id {
			notes = append(notes, "the fixer's word, unbisected: "+cause.Said)
		}
	}
	return notes
}

// fixerRecord is the fixer.txt of the batch's records.
func (r BatchReport) fixerRecord() string {
	f := r.Fixer
	var b strings.Builder
	fmt.Fprintf(&b, "fixer %s of %s, started for: %s\n", f.Name, r.Label(), f.Why)
	if !f.Started {
		fmt.Fprintf(&b, "not started: %s\n", f.Trouble)
		return b.String()
	}
	fmt.Fprintf(&b, "ended: %s\n", f.Ended.State)
	if f.Trouble != "" {
		fmt.Fprintf(&b, "trouble: %s\n", f.Trouble)
	}
	if f.ResultRead {
		fmt.Fprintf(&b, "spent: %s\n", f.Result.Spent())
		fmt.Fprintf(&b, "its run: %s\n", RunRecord(f.Name))
	}
	if f.Commit != "" {
		fmt.Fprintf(&b, "left the batch at %s\n", f.Commit)
	}
	if len(f.Merged) > 0 {
		fmt.Fprintf(&b, "merged what was set aside: %s\n", strings.Join(f.Merged, ", "))
	}
	for _, word := range f.Touched {
		fmt.Fprintf(&b, "touched %s: %s\n", word.Story, word.Said)
	}
	for _, word := range f.Causes {
		fmt.Fprintf(&b, "its word, unbisected, on what made the batch red: %s: %s\n", word.Story, word.Said)
	}
	if r.Tested {
		verdict := "pass"
		if !r.Gate.Passed {
			verdict = "FAIL"
		}
		fmt.Fprintf(&b, "mw's own run of the rig's tests after it: %s\n", verdict)
	}
	return b.String()
}

// fixerMail is what the Held mail says of the fixer the batch was given.
func (r BatchReport) fixerMail() string {
	f := r.Fixer
	var b strings.Builder
	if !f.Started {
		fmt.Fprintf(&b, "The batch was to be given one fixer, %s, for %s, but it %s.\n", f.Name, f.Why, f.Trouble)
		return b.String()
	}
	fmt.Fprintf(&b, "The batch was given one fixer, %s, for %s; it ended %s", f.Name, f.Why, f.Ended.State)
	if f.Trouble != "" {
		fmt.Fprintf(&b, " (%s)", f.Trouble)
	}
	if f.Stopped != nil {
		fmt.Fprintf(&b, ". mw did not look at the batch it left nor test it again, and it is not the batch's one fixer: the batch is held until %s, then tried again and given a fixer afresh.\n",
			f.Stopped.Until.UTC().Format(time.RFC3339))
		fmt.Fprintf(&b, "Its run: %s/%s/\n", RunsDir, f.Name)
		return b.String()
	}
	if !r.Tested && len(r.Merged) == 0 {
		b.WriteString(". mw looked at the batch it left: none of the branches set aside was in it, so the rig's tests were not run, and the batch is held: no second fixer is started.\n")
	} else {
		b.WriteString(". mw looked at the batch it left and ran the rig's tests on it again itself, and the batch is held: no second fixer is started.\n")
	}
	if f.Commit != "" {
		fmt.Fprintf(&b, "Its work is at commit %s in the rig, on no branch.\n", f.Commit)
	}
	for _, word := range f.Causes {
		fmt.Fprintf(&b, "Its word, unbisected, on what made the batch red: %s: %s\n", word.Story, word.Said)
	}
	fmt.Fprintf(&b, "Its run: %s/%s/\n", RunsDir, f.Name)
	return b.String()
}

// gateLog is the run the fixer was given as gate.log holds it.
func (f BatchFixing) gateLog() string {
	if f.GateLog != "" {
		return f.GateLog
	}
	return gateLogOf(f.Gate)
}
