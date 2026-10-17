package application

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// The Tester trial (mw-it6qk5.5): for a week, every landing on a rig the home's
// [tester] table names, whose closing comment tells the Governor HOW TO CHECK
// IT, springs a Tester story. Its session drives the landed app on a
// phone-sized screen by those steps and by adversarial moves, commits nothing,
// and writes its FINDINGS on the landed story; mw next closes it with nothing
// merged and tells the Mayor how many findings there were.
const (
	// LabelTester and TesterFormula mark a Tester story, either one enough.
	LabelTester   = "tester"
	TesterFormula = "tester"

	// TesterTitlePrefix starts a Tester story's title, before the landed
	// story's own.
	TesterTitlePrefix = "Test: "

	// TesterLandedLine starts the line of a Tester story's description that
	// names the landed story it tests, which mw next reads back.
	TesterLandedLine = "Landed story: "

	// TesterFindingsHeading starts the section of the Tester's closing comment
	// that lists what it found, each finding tagged [bug] or [taste]; written
	// "FINDINGS: none" when it found nothing. TesterFuelHeading ends it where
	// a Tester wrote its own fuel, as the tester formula asked before
	// mw-2zh33g.12: a session cannot count its fuel, so mw next records it from
	// the result and the formula asks for none.
	TesterFindingsHeading = "FINDINGS"
	TesterFuelHeading     = "Tester fuel:"

	// TesterFindingMarker is the words a [bug] story's description carries when
	// it was filed from a Tester's finding, which mw tester report counts.
	TesterFindingMarker = "Tester finding"

	// MailTested is the verdict a Tester story's close-out mails the Mayor.
	MailTested = "Tested"

	// TesterTrialDays is how long a trial runs: mw tester report reads from
	// this long before the trial's until, when not told where to start.
	TesterTrialDays = 7
)

// TesterTrial is the home's [tester] table: the rigs whose landings spring a
// Tester story, the moment the trial ends, and the model and effort a Tester
// session runs on. The zero TesterTrial is no trial.
type TesterTrial struct {
	Rigs   []string
	Until  time.Time
	Model  domain.Model
	Effort domain.Effort
}

// On reports whether a landing on rig at at is in the trial.
func (t TesterTrial) On(rig string, at time.Time) bool {
	if t.Until.IsZero() || !at.Before(t.Until) {
		return false
	}
	for _, name := range t.Rigs {
		if strings.EqualFold(strings.TrimSpace(name), rig) {
			return true
		}
	}
	return false
}

// IsTesterStory reports whether d is a Tester story: labelled tester, or worked
// by the tester formula.
func IsTesterStory(d StoryDetail) bool {
	return hasLabel(d.Labels, LabelTester) || d.Merged().Formula == TesterFormula
}

// TesterLanded is the id of the landed story a Tester story tests, as its
// description's TesterLandedLine names it; "" when it names none.
func TesterLanded(d StoryDetail) string {
	for _, line := range strings.Split(d.Description, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), TesterLandedLine); ok {
			if fields := strings.Fields(rest); len(fields) > 0 {
				return strings.TrimRight(fields[0], ".,;:")
			}
		}
	}
	return ""
}

// TesterFindings is what one Tester found: how many findings it tagged bug and
// taste, and the FINDINGS section as it wrote it.
type TesterFindings struct {
	Bug, Taste int
	Text       string
	// Held are the stories the Tester filed held for its findings, each named
	// on a "held: <id>" line of its FINDINGS, and Duplicates the open beads it
	// named as already holding one ("duplicate of: <id>"), in the order written.
	Held, Duplicates []string
	// Untagged are the held stories beyond the findings tagged [bug] or [taste]:
	// a Tester that files a story and then writes its FINDINGS without the tag
	// (a correction, a bullet the reader does not know) still filed a finding
	// (mw-gq6.555).
	Untagged int
}

// Count is how many findings there were: the tagged ones, and any held story
// no tag accounts for.
func (f TesterFindings) Count() int { return f.Bug + f.Taste + f.Untagged }

// Counted is the count as a mail subject says it: "2 findings", "1 finding".
func (f TesterFindings) Counted() string {
	if f.Count() == 1 {
		return "1 finding"
	}
	return fmt.Sprintf("%d findings", f.Count())
}

// Split is the count with how it divides: "2 findings (1 bug, 1 taste)".
func (f TesterFindings) Split() string {
	if f.Untagged > 0 {
		return fmt.Sprintf("%s (%d bug, %d taste, %d not tagged)", f.Counted(), f.Bug, f.Taste, f.Untagged)
	}
	return fmt.Sprintf("%s (%d bug, %d taste)", f.Counted(), f.Bug, f.Taste)
}

// testerFindingsLine is the FINDINGS heading at the start of a line, and
// testerNoneLine the heading saying there were none.
var (
	testerFindingsLine = regexp.MustCompile(`(?m)^[ \t#*]*` + TesterFindingsHeading + `\b`)
	testerNoneLine     = regexp.MustCompile(`(?i)^[ \t#*]*` + TesterFindingsHeading + `[ \t*]*:[ \t]*none\b`)
	testerTag          = regexp.MustCompile(`(?i)^[ \t]*(?:(?:[-*+•]|\d+[.)])[ \t]+)?(?:[*_]{1,2})?\[(bug|taste)\]`)
	testerHeld         = regexp.MustCompile(`(?i)\bheld:[ \t]*([^\n]*)`)
	testerDuplicate    = regexp.MustCompile(`(?i)\bduplicate of:?[ \t]*([^\n]*)`)
	// testerBeadID is a bead id of the rig's prefix pattern, as beadRe reads one
	// (mw-[a-z0-9]+ and dotted numbers), alone: no word, hyphen or dot runs into it
	// on either side, so tester-mw and re-mw-9 are not ids. Group 1 is the id.
	testerBeadID    = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.-])(mw-[a-z0-9]+(?:\.[0-9]+)*)(?:$|[^A-Za-z0-9_-])`)
	testerIDAtStart = regexp.MustCompile(`^(mw-[a-z0-9]+(?:\.[0-9]+)*)(?:$|[^A-Za-z0-9_-])`)
	testerParens    = regexp.MustCompile(`\([^()]*\)`)
	testerIDJoin    = regexp.MustCompile(`^(?:[ \t,;&]|and\b)+`)
)

// testerHeldIDs is the ids a "held:" line starts with, a list of them joined by
// commas, "and" or spaces: the first word that is not an id ends the list, so
// prose after the ids, and anything in parentheses, names none.
func testerHeldIDs(said string) []string {
	said = testerParens.ReplaceAllString(said, " ")
	var ids []string
	for {
		said = testerIDJoin.ReplaceAllString(said, "")
		m := testerIDAtStart.FindStringSubmatch(said)
		if m == nil {
			return ids
		}
		ids = append(ids, m[1])
		said = said[len(m[1]):]
	}
}

// testerDuplicateID is the first bead id a "duplicate of" line names outside
// parentheses, or "".
func testerDuplicateID(said string) string {
	if m := testerBeadID.FindStringSubmatch(testerParens.ReplaceAllString(said, " ")); m != nil {
		return m[1]
	}
	return ""
}

// ReadTesterFindings reads the FINDINGS section of a comment: from the heading
// to the end, or to the Tester fuel line an older Tester wrote. A finding is a
// line that starts with its tag, after a list marker or number if it has one;
// "No [bug] found." is not.
// ok is false when the comment has no FINDINGS heading at the start of a line.
func ReadTesterFindings(text string) (TesterFindings, bool) {
	at := testerFindingsLine.FindStringIndex(text)
	if at == nil {
		return TesterFindings{}, false
	}
	section := text[at[0]:]
	if end := strings.Index(section, TesterFuelHeading); end >= 0 {
		section = section[:end]
	}
	found := TesterFindings{Text: strings.TrimSpace(section)}
	for _, said := range testerHeld.FindAllStringSubmatch(section, -1) {
		for _, id := range testerHeldIDs(said[1]) {
			if !slices.Contains(found.Held, id) {
				found.Held = append(found.Held, id)
			}
		}
	}
	for _, said := range testerDuplicate.FindAllStringSubmatch(section, -1) {
		if id := testerDuplicateID(said[1]); id != "" {
			found.Duplicates = append(found.Duplicates, id)
		}
	}
	if testerNoneLine.MatchString(section) {
		found.Untagged = len(found.Held)
		return found, true
	}
	for _, line := range strings.Split(section, "\n") {
		if tag := testerTag.FindStringSubmatch(line); tag != nil {
			if strings.EqualFold(tag[1], "bug") {
				found.Bug++
			} else {
				found.Taste++
			}
		}
	}
	found.Untagged = max(0, len(found.Held)-found.Bug-found.Taste)
	return found, true
}

// newestFindings is the FINDINGS of the newest of comments (oldest first)
// written no earlier than since, when both times are known.
func newestFindings(comments []Comment, since time.Time) (TesterFindings, bool) {
	for i := len(comments) - 1; i >= 0; i-- {
		if !since.IsZero() && !comments[i].Created.IsZero() && comments[i].Created.Before(since) {
			continue
		}
		if found, ok := ReadTesterFindings(comments[i].Text); ok {
			return found, true
		}
	}
	return TesterFindings{}, false
}

// testerFindings finds what the Tester story c found about landed: its FINDINGS
// on the landed story, written since its claim, or else on its own story, in
// which case from is c's own id and the close-out carries them over.
func (n Next) testerFindings(ctx context.Context, c *closeOut, landed string) (found TesterFindings, from string, err error) {
	since := c.detail.ClaimStarted()
	for _, id := range []string{landed, c.id} {
		comments, err := n.Tracker.StoryComments(ctx, id)
		if err != nil {
			return TesterFindings{}, "", fmt.Errorf("the comments on %s could not be read: %w", id, err)
		}
		if found, ok := newestFindings(comments, since); ok {
			return found, id, nil
		}
	}
	return TesterFindings{}, "", nil
}

// testerRefusals is refusals for a Tester story: its branch must hold no
// commits, every step of its formula must be closed, and its FINDINGS must be
// written. The rig's tests are not run: a Tester changes nothing to test.
func (n Next) testerRefusals(ctx context.Context, c *closeOut, report *NextReport, all bool) []Refusal {
	var found []Refusal
	refuse := func(reason Reason, why, said string) bool {
		found = append(found, Refusal{reason, why, said})
		return !all
	}

	base := StartPoint(n.remote(), c.target)
	commits, err := n.Landing.Ahead(ctx, c.rigDir, c.branch, base)
	if err != nil {
		refuse(ReasonGitFailed, fmt.Sprintf("the commits on %s could not be counted: %v", c.branch, err), "")
		return found
	}
	report.Commits = commits
	if commits > 0 && refuse(ReasonTesterCommitted,
		fmt.Sprintf("a Tester commits nothing, but the session left %d commit(s) on %s", commits, c.branch),
		fmt.Sprintf("A Tester story is closed with nothing merged, so its branch must hold nothing. Take the commits off it "+
			"(`git reset --hard %s` in %s) and run `mw next %s` again.", base, c.worktree, c.id)) {
		return found
	}
	if refusal, open := n.openStepsRefusal(ctx, c, report, leaveClosingStep, base); open && refuse(refusal.Reason, refusal.Why, refusal.Said) {
		return found
	}

	landed := TesterLanded(c.detail)
	if landed == "" {
		refuse(ReasonNoFindings, fmt.Sprintf("the Tester story names no landed story: its description has no %q line", TesterLandedLine+"<id>"), "")
		return found
	}
	findings, from, err := n.testerFindings(ctx, c, landed)
	switch {
	case err != nil:
		refuse(ReasonTrackerFailed, err.Error(), "")
	case from == "":
		refuse(ReasonNoFindings, fmt.Sprintf("the Tester left no %s on %s, nor on %s", TesterFindingsHeading, landed, c.id),
			fmt.Sprintf("Its closing comment on %s starts %q, each finding tagged [bug] or [taste], or says %q.",
				landed, TesterFindingsHeading, TesterFindingsHeading+": none"))
	default:
		c.tested, c.findings, c.findingsFrom = landed, findings, from
	}
	return found
}

// closeTested closes a Tester story whose checks passed: nothing is merged,
// nothing raised and nothing pushed. Its FINDINGS are carried to the landed
// story when the Tester wrote them on its own, the ledger line says what it
// found, and the Mayor is mailed "Tested: <landed>: N findings".
func (n Next) closeTested(ctx context.Context, c *closeOut, report *NextReport) (NextReport, error) {
	report.Tested, report.Findings = c.tested, c.findings
	if c.findingsFrom == c.id {
		note := fmt.Sprintf("From the Tester %s, which wrote this on its own story:\n\n%s", c.id, c.findings.Text)
		if err := n.Tracker.CommentOnStory(ctx, c.tested, note); err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("the FINDINGS could not be carried to %s: %v", c.tested, err))
		}
	}
	outcome := fmt.Sprintf("tested %s: %s; nothing merged", c.tested, c.findings.Split())
	return n.finish(ctx, c, report, outcome, false)
}

// closeATested is a close-out run again on a Tester story an earlier one closed
// out and could not close: its worktree and branch are gone, its line is in the
// ledger and the Mayor was told, so only the close is left.
func (n Next) closeATested(ctx context.Context, c *closeOut, report *NextReport) (NextReport, error) {
	c.tested = TesterLanded(c.detail)
	report.Tested = c.tested
	outcome := fmt.Sprintf("tested %s by an earlier mw next on %s; closed by a later run", c.tested, n.Host)
	return n.finish(ctx, c, report, outcome, true)
}

// testedAlready reports whether the seat's ledger already holds the line of a
// close-out of this Tester story, so that a run again after a close that failed
// reads no branch and writes and mails nothing twice.
func (n Next) testedAlready(ctx context.Context, id string) (bool, error) {
	lines, err := n.Vault.ReadLedger(ctx, n.Seat)
	if err != nil {
		return false, fmt.Errorf("the %s seat's ledger could not be read to see whether %s was closed out already: %v", n.Seat, id, err)
	}
	for _, line := range lines {
		if !LedgerNamesStory(line, id) {
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), " | ")
		if len(cells) >= 3 && strings.HasPrefix(strings.TrimSpace(cells[2]), "tested ") {
			return true, nil
		}
	}
	return false, nil
}

// springTester files the Tester story a landing in the trial springs, held and
// then let go: a landing on a rig the trial names, before its until, whose
// closing comment carries a HOW TO CHECK IT with steps. Never for a Tester
// story or a demo. Nothing it does can stop the landed story being closed: a
// failure is a note on the report.
func (n Next) springTester(ctx context.Context, c *closeOut, report *NextReport, landed Landed) {
	if !n.Tester.On(c.path.Rig, n.now()) || IsTesterStory(c.detail) || isDemo(c.detail) {
		return
	}
	comments, err := n.Tracker.StoryComments(ctx, c.id)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("no Tester story was filed: the comments on %s could not be read: %v", c.id, err))
		return
	}
	howTo := howToCheck(comments)
	if howTo == "" || howToIsInternal(howTo) {
		return
	}

	title := strings.Join(strings.Fields(c.detail.Story.Title), " ")
	path := c.path
	path.Model, path.Effort, path.Formula = n.Tester.Model, n.Tester.Effort, TesterFormula
	id, err := n.Tracker.CreateStory(ctx, NewStory{
		EpicID:     c.detail.EpicID,
		Standalone: c.detail.EpicID == "",
		Title:      TesterTitlePrefix + title,
		Description: fmt.Sprintf("%s%s (landed on %s of %s at %s)\n\n"+
			"Use what %s landed as the Governor would, on his phone: build %s at that commit in a scratch worktree, "+
			"serve it, and drive it at 390x844 by the steps below and by adversarial moves, as the tester formula says. "+
			"Commit nothing. Write FINDINGS on %s.\n\n"+
			"HOW TO CHECK IT, from the closing comment of %s:\n\n%s",
			TesterLandedLine, c.id, c.target, path.Rig, shortSHA(landed.Commit),
			c.id, path.Rig, c.id, c.id, howTo),
		Acceptance: fmt.Sprintf("A comment on %s starting %q (each finding tagged [bug] or [taste], with the steps and the shot path) "+
			"or %q. Nothing committed.", c.id, TesterFindingsHeading, TesterFindingsHeading+": none"),
		Overrides: path,
		Labels:    []string{LabelTester},
	})
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("no Tester story was filed for %s: %v", c.id, err))
		return
	}
	if err := n.Tracker.ReleaseStory(ctx, id); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the Tester story %s was filed and is still held: %v", id, err))
	}
	report.TesterFiled = id
	report.Notes = append(report.Notes, HoldDemosForTester(ctx, n.Tracker, n.Rules, c.path.Rig, c.detail.EpicID, id)...)
}

// HoldDemosForTester makes the epic's last story wait on the Tester story just
// filed under the epic, so the demo stays the epic's last story and is shown to
// the Governor after the Tester has driven the landing. It is KeepDemoLast, and
// returns what the report should say: a note for a last story left alone and
// for each failure; an epic with no such story, or a rig that names no such
// label, gives none.
func HoldDemosForTester(ctx context.Context, tracker WorkTracker, rules EpicRules, rig, epicID, testerID string) []string {
	_, notes := KeepDemoLast(ctx, tracker, rules, rig, epicID, testerID)
	return notes
}

// KeepDemoLast keeps the epic's last story last when story newID has been
// filed under it (mw-gq6.498): when the rig asks for a last-story label
// (epic_last_story_labels) and exactly one open or held story of the epic
// carries it, that story is made to wait on newID. Neither a story that is the
// last story itself nor one that already waits on it, directly or through
// others, is waited on, as that would be a cycle; a last story that is already
// waiting is left as it is.
//
// The line says what was done, or why it was not when a person might look for
// it (a cycle); it is empty when nothing was to be done. The notes are what
// could not be done, and a last story in progress or closed that was left alone.
// An epic with no such story, or with two, a rig that names no such label and
// a nil rules all give nothing.
func KeepDemoLast(ctx context.Context, tracker WorkTracker, rules EpicRules, rig, epicID, newID string) (line string, notes []string) {
	if rules == nil || epicID == "" {
		return "", nil
	}
	requirements, err := rules.EpicRequirements(ctx, rig)
	if err != nil {
		return "", []string{fmt.Sprintf("%s was not made a need of the epic's last story: what %s requires of its epics could not be read: %v", newID, rig, err)}
	}
	if len(requirements.LastStoryLabels) == 0 {
		return "", nil
	}
	epic, err := tracker.ShowEpic(ctx, epicID)
	if err != nil {
		return "", []string{fmt.Sprintf("%s was not made a need of the epic's last story: %s could not be read: %v", newID, epicID, err)}
	}
	var live, left []StoryDetail
	for _, story := range epic.Stories {
		if story.Story.ID == newID || !carriesAny(story.Labels, requirements.LastStoryLabels) {
			continue
		}
		if status := strings.ToLower(strings.TrimSpace(story.Status)); status == StatusOpen || status == StatusHeld {
			live = append(live, story)
		} else {
			left = append(left, story)
		}
	}
	if len(live) == 0 {
		for _, story := range left {
			notes = append(notes, fmt.Sprintf("the last story %s is %s, so it does not wait on %s", story.Story.ID, story.Status, newID))
		}
		return "", notes
	}
	if len(live) > 1 {
		return "", nil
	}
	last := live[0].Story.ID
	if slices.Contains(live[0].Needs, newID) {
		return "", nil
	}
	if waitsThrough(epic.Stories, newID, last) {
		return fmt.Sprintf("%s waits on the epic's last story %s, so %s was not made to wait on it (that would be a cycle)", newID, last, last), nil
	}
	if err := tracker.AddBlocker(ctx, last, newID); err != nil {
		return "", []string{fmt.Sprintf("the last story %s could not be made to wait on %s: %v", last, newID, err)}
	}
	return fmt.Sprintf("The epic's last story %s now waits on %s, so it stays the epic's last story.", last, newID), nil
}

// waitsThrough reports whether the story from waits on the story target,
// directly or through the stories it waits on, as far as the epic's stories say.
func waitsThrough(stories []StoryDetail, from, target string) bool {
	needs := make(map[string][]string, len(stories))
	for _, story := range stories {
		needs[story.Story.ID] = story.Needs
	}
	seen := map[string]bool{from: true}
	queue := []string{from}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		for _, need := range needs[next] {
			if need == target {
				return true
			}
			if !seen[need] {
				seen[need] = true
				queue = append(queue, need)
			}
		}
	}
	return false
}

// carriesAny reports whether labels holds any of want.
func carriesAny(labels, want []string) bool {
	for _, w := range want {
		if hasLabel(labels, w) {
			return true
		}
	}
	return false
}

// IsTesterFinding says a [bug] story's description is a Tester's finding: it
// carries TesterFindingMarker anywhere, or its first line says the bug came
// from the Tester ("Found by the Tester ...", "... from the Tester's finding
// on ...", "FINDING (Tester's ..."), the words findings were filed with before
// the marker went into every filing. mw report and mw tester report both ask
// this one function.
func IsTesterFinding(description string) bool {
	lower := strings.ToLower(description)
	if strings.Contains(lower, strings.ToLower(TesterFindingMarker)) {
		return true
	}
	first := strings.TrimSpace(lower)
	if i := strings.IndexByte(first, '\n'); i >= 0 {
		first = first[:i]
	}
	return strings.HasPrefix(first, "found by the tester") ||
		strings.HasPrefix(first, "finding (tester's") ||
		strings.Contains(first, "from the tester")
}
