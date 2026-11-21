package application

import (
	"context"
	"fmt"
	"path"
	"strconv"
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
	// From is the batch as the fixer was given it: what its commits are ahead of.
	From string

	// Bundle is where a held batch kept the fixer's commits, by path from the
	// vault's root, and Bundled what they were, oldest first; BundleSaid is
	// the one line a held batch's records say of them when there is no bundle,
	// or it could not be made (mw-gq6.865).
	Bundle     string
	Bundled    []Commit
	BundleSaid string

	// Carried are the commits that touched one story's files only and were
	// pushed to that story's branch, so that the next batch merges them, and
	// CarryTrouble why a story's could not be (mw-gq6.1014). The rest stay
	// only in the bundle.
	Carried      []FixerCarried
	CarryTrouble []string

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
	// Failed is how it ended when that was not a success, in the words a hold
	// opens with ("fixer ran out of turns"): it is a failed fixer, and what it
	// left uncommitted was discarded (mw-qqxlwm3.1). Empty when it ended well,
	// or the harness stopped it for a limit of the factory's (Stopped).
	Failed string
	// Discarded are the paths a failed fixer left uncommitted, Leftovers where
	// they are kept as a diff (by path from the vault's root), and LeftoversSaid
	// why the diff could not be kept, when it could not.
	Discarded     []string
	Leftovers     string
	LeftoversSaid string
	// Stopped is the harness's stop for a limit of the factory's
	// (HarnessStopOf) that ended it, or that this host was held for when it
	// was to be started (mw-gq6.472): nil when none. It says nothing of the
	// batch, so the batch is held until the stop's time, not judged after it.
	Stopped *HarnessStop
}

// FixerCarried is one of the fixer's commits pushed to a story's branch: Hash
// and Subject as the bundle names it, Story and Branch where it went, and Tip the
// commit the branch is at now, at the rig's origin.
type FixerCarried struct {
	Hash    string
	Subject string
	Story   string
	Branch  string
	Tip     string
	// Already says the branch held the commit already, so none was pushed for
	// it (mw-gq6.1055).
	Already bool
}

// WhatsNewLine is the What's new line mw writes into the rig's changelog files
// for one story of a batch, once the batch's fixer has ended (mw-gq6.942). Text
// is empty when the story says "What's new: none". FromTitle says the sentence
// is the story's title, used as the note because no comment gave one.
type WhatsNewLine struct {
	Story     string
	Kind      string
	Text      string
	FromTitle bool
}

// whatsNewLines are the lines mw will write at landing for the stories of the
// batch, read with the function a landing reads them with (Next.changelogNote),
// so that the fixer is told what the tests will meet. A rig that raises no
// version or keeps no changelog has none; one that cannot be read is a note.
func (b BatchLand) whatsNewLines(ctx context.Context, rigDir string, rules domain.EpicRequirements, report *BatchReport, nr *NextReport) []WhatsNewLine {
	if len(rules.VersionFiles) == 0 || len(rules.ChangelogFiles) == 0 {
		return nil
	}
	var lines []WhatsNewLine
	for _, s := range report.storiesInOrder() {
		if s.OnTarget {
			continue
		}
		entry, fromTitle, err := b.Next.changelogNote(ctx, b.storyCloseOut(s, rigDir), nr)
		if err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("the What's new line of %s could not be told to the fixer: %v", s.ID, err))
			continue
		}
		lines = append(lines, WhatsNewLine{Story: s.ID, Kind: entry.Kind, Text: entry.Text, FromTitle: fromTitle})
	}
	return lines
}

// whatsNewSection is the part of a fixer's boot file about what mw writes into
// the batch itself: the version and the What's new lines. It is empty for a rig
// that raises no version. A test that reads those files meets what mw wrote,
// not what the fixer left, which is how batch 37 of spell-forge went red on a
// title (mw-gq6.942).
func (f BatchFixBrief) whatsNewSection() string {
	if len(f.VersionFiles) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## What mw writes into the batch itself\n\n")
	when := "after you end, and before it runs the rig's tests on the batch again"
	if f.Tested {
		when = "before the rig's tests ran on the batch, and it is already in the tree you are in"
	}
	fmt.Fprintf(&b, "mw raises the version in the rig's version_files (%s) %s. ", strings.Join(f.VersionFiles, ", "), when)
	if len(f.ChangelogFiles) == 0 {
		b.WriteString("A test that reads the version meets the number mw wrote, not the one it finds now.\n\n")
		return b.String()
	}
	fmt.Fprintf(&b, "It also writes one What's new line for each story into the rig's changelog_files (%s), %s, "+
		"so a test that reads those files meets them, and a story's own words in them (its title, say) can fail a test "+
		"that bans them. The lines:\n\n", strings.Join(f.ChangelogFiles, ", "), when)
	for _, line := range f.WhatsNew {
		if line.Text == "" {
			fmt.Fprintf(&b, "- %s: no line (its What's new: none)\n", line.Story)
			continue
		}
		fmt.Fprintf(&b, "- %s: %s: %s", line.Story, line.Kind, line.Text)
		if line.FromTitle {
			b.WriteString(" (the story's title, used as the note because it has no What's new: comment)")
		}
		b.WriteString("\n")
	}
	b.WriteString("\nA `What's new:` comment on the story changes its line (`What's new: none` writes none): " +
		"put one on a story whose line a test refuses, and say so in your last message.\n\n")
	return b.String()
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
func (b BatchLand) fix(ctx context.Context, dir, rigDir string, rules domain.EpicRequirements, report *BatchReport, why string, nr *NextReport) bool {
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

	// Where the batch stands as the fixer is given it, so that what it commits
	// can be told from the batch (bundleFixer).
	f.From, _ = n.Landing.RefHead(ctx, dir, "HEAD")

	spec, err := n.Boot.BatchFix(ctx, BatchFixBrief{
		Rig: report.Rig, Target: report.Target, Number: report.Number, Base: report.Base,
		Stories: report.storiesInOrder(), Tested: report.Tested, Gate: report.Gate, Settings: rules.LandingSettings(),
		VersionFiles: rules.VersionFiles, ChangelogFiles: rules.ChangelogFiles,
		WhatsNew: b.whatsNewLines(ctx, rigDir, rules, report, nr),
	}, dir)
	if err != nil {
		return notStarted(fmt.Sprintf("could not be assembled: %v", err))
	}
	// The last look at the marker, right before the fixer starts: the batch was
	// begun, and its tests run, before the person at the host's screen turned give
	// way on. A host that gives way starts no fixer, red batch or set-aside branches
	// alike: the batch is held, and every story stays queued (mw-2891blf.19).
	if standing, giving := GivesWayNow(ctx, n.GiveWay, false); giving {
		f.Trouble = fmt.Sprintf("was not started: the host %s", standing)
		report.Held = fmt.Sprintf("the host %s, so its fixer %s was not started; the stories stay queued", standing, f.Name)
		return false
	}
	// Nor does a host with no room (load, memory or the Windows drive's floor): a
	// fixer is a Builder session, and dispatch starts none there (mw-gq6.1512).
	if why := n.noRoomForASession(ctx); why != "" {
		f.Trouble = fmt.Sprintf("was not started: the host has no room: %s", why)
		report.Held = fmt.Sprintf("the host has no room (%s), so its fixer %s was not started; the stories stay queued", why, f.Name)
		return false
	}
	if err := n.Runner.Start(ctx, spec); err != nil {
		return notStarted(fmt.Sprintf("%s could not be started: %v", spec.Name, err))
	}
	f.Started = true
	n.print(fmt.Sprintf("land on %s: %s given its fixer %s (%s); waiting for it to end\n", n.Host, report.Label(), f.Name, why))

	// The batch waits for its fixer with the merge slot shared, so that the
	// rig's checks run meanwhile; it takes the slot alone again before it looks
	// at what the fixer left.
	resume := n.yieldSlot(ctx)
	f.Ended, err = n.Runner.Wait(ctx, spec.Name)
	resume()
	if lost := b.slotLost(); lost != "" {
		f.Trouble = lost
		report.Held = fmt.Sprintf("its fixer %s ended, but %s", f.Name, lost)
		return false
	}
	if err != nil {
		f.Trouble = fmt.Sprintf("could not be waited on: %v", err)
		report.Held = fmt.Sprintf("its fixer %s %s", f.Name, f.Trouble)
		return false
	}
	if err := n.Runner.Close(ctx, spec.Name); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the fixer's session %s could not be closed: %v", spec.Name, err))
	}
	b.readFixer(ctx, f, report)
	if end := f.failedEnd(); end != "" {
		return b.failedFixer(ctx, dir, report, end)
	}
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
	if f.Failed != "" {
		fmt.Fprintf(&b, "failed: the %s\n", f.Failed)
		b.WriteString(f.leftoversLines())
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

// changelogMail is the part of the Held mail about the What's new lines mw
// wrote into the batch: for a batch whose tests failed with the output naming
// one of the rig's changelog files, that mw wrote the line itself, and the line
// (mw-gq6.942). It is empty when the failure names none, or mw wrote none.
func (r BatchReport) changelogMail() string {
	if !r.Tested || r.Gate.Passed || len(r.changelogWritten) == 0 {
		return ""
	}
	var named string
	for _, file := range r.changelogFiles {
		if strings.Contains(r.Gate.Output, file) || strings.Contains(r.Gate.Output, path.Base(file)) {
			named = file
			break
		}
	}
	if named == "" {
		return ""
	}
	var b strings.Builder
	when := "when it made the batch, before the tests ran"
	if r.writtenAfterFixer {
		when = "after the fixer exited"
	}
	fmt.Fprintf(&b, "The failing tests name %s: mw wrote its What's new line itself %s; it is on no story's branch, and a story's own words (its title, when it has no What's new: comment) can be what a test refuses. The lines:\n", named, when)
	for _, entry := range r.changelogWritten {
		fmt.Fprintf(&b, "  %s: %s: %s\n", entry.Story, entry.Kind, entry.Text)
	}
	b.WriteString("A `What's new:` comment on the story changes its line.\n")
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
	switch {
	case f.Failed != "":
		fmt.Fprintf(&b, ". The %s, so mw did not look at the batch it left nor test it, and the batch is held: no second fixer is started.\n", f.Failed)
		b.WriteString(r.failedCauseLine())
		b.WriteString(f.leftoversLines())
	case !r.Tested && len(r.Merged) == 0:
		b.WriteString(". mw looked at the batch it left: none of the branches set aside was in it, so the rig's tests were not run, and the batch is held: no second fixer is started.\n")
	default:
		b.WriteString(". mw looked at the batch it left and ran the rig's tests on it again itself, and the batch is held: no second fixer is started.\n")
	}
	if f.Bundle != "" {
		fmt.Fprintf(&b, "Its commits are kept in the vault, in a bundle: %s\n%s", f.Bundle, f.bundledList())
	} else if f.BundleSaid != "" {
		fmt.Fprintf(&b, "%s\n", f.BundleSaid)
	}
	b.WriteString(f.carriedLines())
	if f.Commit != "" && f.Bundle == "" {
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

// bundledList names each commit of the fixer's bundle, a short hash and the
// subject on a line each, oldest first.
func (f BatchFixing) bundledList() string {
	var b strings.Builder
	for _, c := range f.Bundled {
		subject, _, _ := strings.Cut(strings.TrimSpace(c.Message), "\n")
		fmt.Fprintf(&b, "  %s %s\n", c.Hash, subject)
	}
	return b.String()
}

// carriedLines say which of the fixer's commits were pushed to a story's branch
// and where, and what stays only in the bundle; empty when the fixer left none.
func (f BatchFixing) carriedLines() string {
	var b strings.Builder
	for _, c := range f.Carried {
		if b.Len() == 0 {
			b.WriteString("Its commits that touched one story's files only were pushed to that story's branch at the rig's origin, so the batch landed again merges them:\n")
		}
		if c.Already {
			fmt.Fprintf(&b, "  %s %s: already on %s\n", c.Hash, c.Subject, c.Branch)
			continue
		}
		fmt.Fprintf(&b, "  %s %s: pushed to %s (the branch is at %s)\n", c.Hash, c.Subject, c.Branch, shortCommit(c.Tip))
	}
	for _, trouble := range f.CarryTrouble {
		fmt.Fprintf(&b, "%s\n", trouble)
	}
	switch {
	case f.Bundle == "":
	case len(f.Carried) == 0:
		b.WriteString("No commit of its touched one story's files only, so each stays only in the bundle.\n")
	case len(f.Carried) < len(f.Bundled):
		b.WriteString("The others touched more than one story's files, or could not be pushed, and stay only in the bundle.\n")
	}
	return b.String()
}

// carryFixer pushes the fixer's commits that touch one story's files only to
// that story's branch at the origin, where the next batch merges them: they are
// on no branch, and a batch landed again would meet the red the fixer mended. A
// commit belongs to a story when the files of the story's branch (against the
// batch's base) hold every file it touches and no other story's do; one that
// belongs to none or to two stays in the bundle alone. A merge of the fixer's is
// picked against the parent the story's branch does not hold, and one whose parents it holds neither of is refused by name (mw-gq6.1371). A story's commits go together, in order, or not at all: trouble is a
// line of the records, never another hold (mw-gq6.1014).
func (b BatchLand) carryFixer(ctx context.Context, dir string, report *BatchReport, commits []Commit) {
	f := report.Fixer
	n := b.Next
	said := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		f.CarryTrouble = append(f.CarryTrouble, line)
		report.Notes = append(report.Notes, line)
	}
	owned := map[string][]string{}
	for _, s := range report.Merged {
		if s.OnTarget {
			continue
		}
		files, err := n.Landing.FilesBetween(ctx, dir, report.Base, s.Branch)
		if err != nil {
			said("the files of %s could not be read, so no commit of the fixer's was pushed to its branch: %v", s.ID, err)
			continue
		}
		owned[s.ID] = files
	}
	if len(owned) == 0 {
		return
	}
	byStory := map[string][]Commit{}
	var stories []string
	for _, c := range commits {
		files, err := n.Landing.FilesBetween(ctx, dir, c.Hash+"^", c.Hash)
		if err != nil || len(files) == 0 {
			continue
		}
		var owner string
		owners := 0
		for _, s := range report.Merged {
			if holdsAll(owned[s.ID], files) {
				owner = s.ID
				owners++
			}
		}
		if owners != 1 {
			continue
		}
		if len(byStory[owner]) == 0 {
			stories = append(stories, owner)
		}
		byStory[owner] = append(byStory[owner], c)
	}
	for _, id := range stories {
		var story BatchStory
		for _, s := range report.Merged {
			if s.ID == id {
				story = s
			}
		}
		branch := StoryBranch(id)
		// A commit the story's branch holds already (the fixer merged the
		// branch, so its own tip is among the fixer's commits) is nothing to
		// push (mw-gq6.1055).
		var hashes []string
		have := map[string]bool{}
		checked := true
		for _, c := range byStory[id] {
			on, err := n.Landing.OnBranch(ctx, dir, c.Hash, story.Branch)
			if err != nil {
				said("the fixer's commit %s for %s could not be checked against its branch %s: %v", c.Hash, id, branch, err)
				checked = false
				break
			}
			if on {
				have[c.Hash] = true
				continue
			}
			hashes = append(hashes, c.Hash)
		}
		if !checked {
			continue
		}
		var tip string
		if len(hashes) > 0 {
			var err error
			tip, err = n.Landing.Carry(ctx, dir, n.remote(), hashes, story.Branch, branch)
			if err != nil {
				said("the fixer's commits for %s (%s) could not be pushed to its branch %s: %v", id, strings.Join(hashes, ", "), branch, err)
				continue
			}
		}
		for _, c := range byStory[id] {
			subject, _, _ := strings.Cut(strings.TrimSpace(c.Message), "\n")
			f.Carried = append(f.Carried, FixerCarried{Hash: c.Hash, Subject: subject, Story: id, Branch: branch, Tip: tip, Already: have[c.Hash]})
		}
	}
}

// holdsAll says every file of want is in have.
func holdsAll(have, want []string) bool {
	if len(have) == 0 {
		return false
	}
	held := make(map[string]bool, len(have))
	for _, file := range have {
		held[file] = true
	}
	for _, file := range want {
		if !held[file] {
			return false
		}
	}
	return true
}

// bundleFixer keeps, in the held batch's records, the commits the fixer made:
// the landing worktree is cleared once the batch is held, and they are on no
// branch, so a batch landed again would lose them. It bundles them (the batch
// as the fixer was given it, up to where the worktree stands) into
// fixer.bundle and checks the bundle verifies. A fixer that left no commits
// writes none, and the records say so. Trouble is a note on the batch and a
// line of its records, never another hold (mw-gq6.865).
func (b BatchLand) bundleFixer(ctx context.Context, dir string, report *BatchReport) {
	f := report.Fixer
	if f == nil || !f.Started || f.Stopped != nil || f.From == "" || b.Records == nil {
		return
	}
	n := b.Next
	trouble := func(err error) {
		f.BundleSaid = fmt.Sprintf("the fixer's commits could not be kept in a bundle: %v", err)
		report.Notes = append(report.Notes, f.BundleSaid)
	}
	commits, err := n.Landing.Commits(ctx, dir, "HEAD", f.From)
	if err != nil {
		trouble(err)
		return
	}
	if len(commits) == 0 {
		f.BundleSaid = "its fixer " + f.Name + " left no commits ahead of the batch it was given, so no bundle was written"
		return
	}
	b.carryFixer(ctx, dir, report, commits)
	dest := b.Records.BatchFilePath(report.Rig, report.Number, BatchFixerBundleFile)
	if dest == "" {
		trouble(fmt.Errorf("the batch's records have no place for it"))
		return
	}
	if _, err := n.Landing.Bundle(ctx, dir, "HEAD", f.From, dest); err != nil {
		trouble(err)
		return
	}
	if err := n.Landing.VerifyBundle(ctx, dir, dest); err != nil {
		trouble(fmt.Errorf("the bundle does not verify: %w", err))
		return
	}
	f.Bundle = path.Join(BatchDir, report.Rig, strconv.Itoa(report.Number), BatchFixerBundleFile)
	f.Bundled = commits
	report.Records = append(report.Records, f.Bundle)
}

// fixerBundleLines are the lines of a held batch's outcome.txt about its
// fixer's commits: the bundle and each commit in it, or why there is none.
func (r BatchReport) fixerBundleLines() string {
	f := r.Fixer
	switch {
	case f == nil:
		return ""
	case f.Bundle != "":
		return f.leftoversLines() + fmt.Sprintf("the fixer %s's commits are kept in %s:\n%s", f.Name, f.Bundle, f.bundledList()) + f.carriedLines()
	case f.BundleSaid != "":
		return f.leftoversLines() + f.BundleSaid + "\n" + f.carriedLines()
	}
	return f.leftoversLines()
}

// failedEnd is how the fixer ended in the words a hold opens with, when that was
// not a success: it ran out of turns, ended in an error, or was killed. Empty
// when it ended well, or the harness stopped it for a limit of the factory's,
// which says nothing of its work (Stopped). A result that could not be read of a
// session that exited cleanly is not an end it can be called failed for: the
// tests on the batch are the judge then.
func (f BatchFixing) failedEnd() string {
	if f.Stopped != nil || !f.Started {
		return ""
	}
	if f.ResultRead {
		switch {
		case f.Result.Finished():
			return ""
		case f.Result.Subtype == "error_max_turns":
			return "fixer ran out of turns"
		case f.Result.Subtype == "" || f.Result.Subtype == "success":
			return "fixer ended in error"
		default:
			return "fixer ended as " + f.Result.Subtype
		}
	}
	if f.Ended.State != StateExited || f.Ended.ExitCode != 0 {
		return "fixer was killed (its session ended: " + string(f.Ended.State) + ")"
	}
	return ""
}

// failedFixer holds the batch for a fixer that did not end in success. What it
// left uncommitted is discarded from the landing and kept as a diff in the
// batch's records, so that what would be tested is what would be pushed and no
// work is lost; its commits are kept by bundleFixer, as for any held batch. The
// hold names the story set aside as the cause (mw-qqxlwm3.1).
func (b BatchLand) failedFixer(ctx context.Context, dir string, report *BatchReport, end string) bool {
	f := report.Fixer
	f.Failed = end
	left, err := b.Next.Landing.Uncommitted(ctx, dir)
	switch {
	case err != nil:
		f.LeftoversSaid = fmt.Sprintf("what it left uncommitted could not be read, so none of it was kept: %v", err)
		report.Notes = append(report.Notes, f.LeftoversSaid)
	case len(left) > 0:
		f.Discarded = left
		b.keepLeftovers(ctx, dir, report)
	}
	why := f.Trouble
	if why == "" {
		why = "its session ended " + string(f.Ended.State)
	}
	report.Held = fmt.Sprintf("the %s (%s: %s), so what it left was not tested or pushed; %s", end, f.Name, why, report.failedCause())
	return false
}

// keepLeftovers keeps the files a failed fixer left uncommitted as a diff in the
// batch's records and clears the landing's worktree of them. When it cannot, the
// reason is said in the records and the mail, and the batch is held all the same.
func (b BatchLand) keepLeftovers(ctx context.Context, dir string, report *BatchReport) {
	f := report.Fixer
	trouble := func(why string) {
		f.LeftoversSaid = fmt.Sprintf("its %d uncommitted files could not be kept as a diff, and stay in the landing worktree until it is taken away: %s", len(f.Discarded), why)
		report.Notes = append(report.Notes, f.LeftoversSaid)
	}
	keeper, ok := b.Next.Landing.(LeftoversKeeper)
	if !ok {
		trouble("this landing cannot keep a diff")
		return
	}
	var dest string
	if b.Records != nil {
		dest = b.Records.BatchFilePath(report.Rig, report.Number, BatchFixerLeftoversFile)
	}
	if dest == "" {
		trouble("the batch's records have no place for it")
		return
	}
	if err := keeper.KeepLeftovers(ctx, dir, dest); err != nil {
		trouble(err.Error())
		return
	}
	f.Leftovers = path.Join(BatchDir, report.Rig, strconv.Itoa(report.Number), BatchFixerLeftoversFile)
	report.Records = append(report.Records, f.Leftovers)
}

// failedCause says, for the hold, what the failed fixer had been given to mend:
// the stories set aside, or a red batch.
func (r BatchReport) failedCause() string {
	if len(r.SetAside) == 0 {
		return "the cause is the rig's tests failing on the batch, which it was to mend"
	}
	return "the cause is the story set aside that it was to merge: " + strings.Join(storyIDs(r.SetAside), ", ")
}

// failedCauseLine is the Held mail's line naming the cause of a failed fixer: each
// story set aside, why, and the files it conflicted in.
func (r BatchReport) failedCauseLine() string {
	if len(r.SetAside) == 0 {
		return "The cause: the rig's tests failed on the batch, and the fixer that was to mend it did not end well.\n"
	}
	var b strings.Builder
	for _, s := range r.SetAside {
		fmt.Fprintf(&b, "The cause: %s was set aside (%s), and the fixer that was given it to merge did not end well", s.ID, s.Why)
		if len(s.ConflictFiles) > 0 {
			fmt.Fprintf(&b, "; it conflicted in %s", strings.Join(s.ConflictFiles, ", "))
		}
		b.WriteString(".\n")
	}
	return b.String()
}

// leftoversLines are the lines of a held batch's records and mail about what a
// failed fixer left uncommitted: where the diff is kept and which files, or why
// it is not; empty for a fixer that left none.
func (f BatchFixing) leftoversLines() string {
	if len(f.Discarded) == 0 && f.LeftoversSaid == "" {
		return ""
	}
	var b strings.Builder
	switch {
	case f.Leftovers != "":
		fmt.Fprintf(&b, "Its %d uncommitted files were discarded from the landing and are kept as a diff in the vault: %s\n", len(f.Discarded), f.Leftovers)
	default:
		fmt.Fprintf(&b, "%s\n", f.LeftoversSaid)
	}
	const shown = 30
	for i, file := range f.Discarded {
		if i == shown {
			fmt.Fprintf(&b, "  and %d more\n", len(f.Discarded)-shown)
			break
		}
		fmt.Fprintf(&b, "  %s\n", file)
	}
	return b.String()
}
