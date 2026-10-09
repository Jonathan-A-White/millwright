package application

import (
	"context"
	"fmt"
	"regexp"
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
	// "FINDINGS: none" when it found nothing. TesterFuelHeading ends it.
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
}

// Count is how many findings there were.
func (f TesterFindings) Count() int { return f.Bug + f.Taste }

// Counted is the count as a mail subject says it: "2 findings", "1 finding".
func (f TesterFindings) Counted() string {
	if f.Count() == 1 {
		return "1 finding"
	}
	return fmt.Sprintf("%d findings", f.Count())
}

// Split is the count with how it divides: "2 findings (1 bug, 1 taste)".
func (f TesterFindings) Split() string {
	return fmt.Sprintf("%s (%d bug, %d taste)", f.Counted(), f.Bug, f.Taste)
}

// testerFindingsLine is the FINDINGS heading at the start of a line, and
// testerNoneLine the heading saying there were none.
var (
	testerFindingsLine = regexp.MustCompile(`(?m)^[ \t#*]*` + TesterFindingsHeading + `\b`)
	testerNoneLine     = regexp.MustCompile(`(?i)^[ \t#*]*` + TesterFindingsHeading + `[ \t*]*:[ \t]*none\b`)
	testerTag          = regexp.MustCompile(`(?i)\[(bug|taste)\]`)
)

// ReadTesterFindings reads the FINDINGS section of a comment: from the heading
// to the Tester fuel line, or the end. ok is false when the comment has no
// FINDINGS heading at the start of a line.
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
	if testerNoneLine.MatchString(section) {
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
	if refusal, open := n.openStepsRefusal(ctx, c); open && refuse(refusal.Reason, refusal.Why, refusal.Said) {
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
			fmt.Sprintf("Its closing comment on %s starts %q, each finding tagged [bug] or [taste], or says %q; then %q.",
				landed, TesterFindingsHeading, TesterFindingsHeading+": none", TesterFuelHeading+" <tokens>"))
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
			"or %q, then %q. Nothing committed.", c.id, TesterFindingsHeading, TesterFindingsHeading+": none", TesterFuelHeading+" <tokens>"),
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
}
