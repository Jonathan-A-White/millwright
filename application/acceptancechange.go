package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// AcceptanceAtClaimField is the metadata a story carries to say what its
// acceptance read when the claim it is held under was made (mw-gq6.509): a
// short hash of each line, comma-joined, in order. A Builder reads its story
// once, at boot, so a criterion the Mayor adds afterwards never reaches it;
// mw next reads the bead's acceptance again at close-out and compares. A story
// claimed before this was written has no record, and is not compared.
const AcceptanceAtClaimField = "acceptance_at_claim"

// acceptanceLines are the lines of an acceptance text that say something, each
// with its whitespace collapsed, so that re-spacing or a blank line is not a
// change.
func acceptanceLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.Join(strings.Fields(line), " "); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// lineHash is the short hash of one line of acceptance.
func lineHash(line string) string {
	sum := sha256.Sum256([]byte(line))
	return hex.EncodeToString(sum[:4])
}

// AcceptanceAtClaim is the value of AcceptanceAtClaimField for an acceptance
// text. An empty acceptance is recorded as "-", so that it is told from a story
// with no record.
func AcceptanceAtClaim(acceptance string) string {
	var hashes []string
	for _, line := range acceptanceLines(acceptance) {
		hashes = append(hashes, lineHash(line))
	}
	if len(hashes) == 0 {
		return "-"
	}
	return strings.Join(hashes, ",")
}

// AcceptanceChange is how a story's acceptance differs from what it read at the
// claim.
type AcceptanceChange struct {
	// Added are the lines of the acceptance now that were not in it at the claim.
	Added []string
	// Dropped is how many lines of it at the claim are gone or moved.
	Dropped int
}

// AcceptanceChangedSince compares the acceptance a story holds now with the
// record its claim made. It is false when nothing was recorded or nothing
// changed.
func AcceptanceChangedSince(recorded, acceptance string) (AcceptanceChange, bool) {
	if recorded == "" || recorded == AcceptanceAtClaim(acceptance) {
		return AcceptanceChange{}, false
	}
	had := map[string]bool{}
	for _, hash := range strings.Split(recorded, ",") {
		had[hash] = true
	}
	var change AcceptanceChange
	now := map[string]bool{}
	for _, line := range acceptanceLines(acceptance) {
		now[lineHash(line)] = true
		if !had[lineHash(line)] {
			change.Added = append(change.Added, line)
		}
	}
	for hash := range had {
		if hash != "-" && !now[hash] {
			change.Dropped++
		}
	}
	return change, true
}

// acceptanceChangedRefusal is the refusal of a story whose acceptance is not
// what its claim recorded (mw-gq6.509): the Builder worked the text it read at
// boot, so the story is handed back for a session to work the whole of it.
func acceptanceChangedRefusal(c *closeOut) (Refusal, bool) {
	change, changed := AcceptanceChangedSince(c.detail.AcceptanceAtClaim, c.detail.Acceptance)
	if !changed {
		return Refusal{}, false
	}
	said := "The acceptance of " + c.id + " is not what it read when the claim was made, so the session worked a text that is not the story's now."
	if len(change.Added) > 0 {
		said += "\nLines added since the claim:\n" + bulletList(change.Added)
	}
	if change.Dropped > 0 {
		said += fmt.Sprintf("\n%d line(s) the claim had were taken out or changed.", change.Dropped)
	}
	said += "\nThe claim is given back and the branch kept: the next session works the whole acceptance."
	said += "\n" + rescopeRecipeSaid(c.id)
	return Refusal{ReasonAcceptanceChanged, "the acceptance changed after the claim of " + c.id, said}, true
}

// acceptanceRefusal is the refusal of a story whose acceptance changed after the
// claim and that the Mayor has not answered. The answer is read from the bead's
// comments and the branch's commits; a read that fails is a refusal to say so
// (tracker-failed or git-failed, the claim kept), not an answer of no.
func (n Next) acceptanceRefusal(ctx context.Context, c *closeOut, report *NextReport, base string) (Refusal, bool) {
	refusal, changed := acceptanceChangedRefusal(c)
	if !changed {
		return Refusal{}, false
	}
	answer, err := n.rescopeAnswer(ctx, c, report, base)
	if err != nil {
		return readRefusal(err)
	}
	if answer.answered {
		return Refusal{}, false
	}
	if answer.pinned {
		// The Mayor ran the recipe already: say what his closing comment lacks,
		// not the recipe again (mw-gq6.1486).
		refusal.Said = strings.TrimSuffix(refusal.Said, "\n"+rescopeRecipeSaid(c.id)) + "\n" + answer.lacks(c.id)
	}
	return refusal, true
}

// RescopedMark opens the comment that records a Mayor's re-scope of a story
// whose work is done (mw-gq6.1147): who wrote it and when are the comment's
// own, and the why follows the mark. RescopedAcceptanceLine, on the next line,
// pins the acceptance it answers, so that a change made after it is not
// answered by it.
const (
	RescopedMark           = "RESCOPED-BY-MAYOR:"
	RescopedAcceptanceLine = "acceptance now: "
)

// RescopeRecipe is the one line that lands a handed-back story the Mayor has
// re-scoped: the Mayor rewords the criteria and writes a closing comment of his
// own, then runs this.
func RescopeRecipe(id string) string {
	return "mw next " + id + ` --no-dispatch --rescoped-by-mayor "<why>"`
}

// rescopeRecipeSaid is the line of the acceptance-changed refusal that gives the
// Mayor the command to run, with the lines his closing comment needs.
func rescopeRecipeSaid(id string) string {
	return "If the Mayor re-scoped it on purpose and wrote a closing comment after the change, " + RescopeRecipe(id) +
		" lands it without a fresh session. " + ClosingCommentLinesSaid
}

// ClosingCommentLinesSaid is the lines a closing comment must hold for mw next
// to read it as one (isClosingComment), as a refusal quotes them.
const ClosingCommentLinesSaid = "A closing comment counts only when it holds the line 'For the rig memory' and either 'HOW TO CHECK IT' or 'Internal: nothing for the Governor to look at'."

// closingCommentLacks names which of the lines isClosingComment wants a text
// does not hold, in words; "" when it holds them.
func closingCommentLacks(text string) string {
	var lacks []string
	if !strings.Contains(text, "For the rig memory") {
		lacks = append(lacks, "'For the rig memory'")
	}
	if !strings.Contains(text, "HOW TO CHECK IT") && !strings.Contains(text, "Internal: nothing for the Governor to look at") {
		lacks = append(lacks, "'HOW TO CHECK IT' or 'Internal: nothing for the Governor to look at'")
	}
	return strings.Join(lacks, " and ")
}

// builderAuthor says a comment was written by a Builder session rather than by
// the Mayor or the Governor: its author is builder@<host>.
func builderAuthor(author string) bool {
	return strings.HasPrefix(strings.TrimSpace(author), "builder@")
}

// rescopeHash is the acceptance a RESCOPED-BY-MAYOR: comment pins, "" when the
// comment is not one or pins none.
func rescopeHash(comment Comment) string {
	text := strings.TrimSpace(comment.Text)
	if !strings.HasPrefix(text, RescopedMark) {
		return ""
	}
	for _, line := range strings.Split(text, "\n") {
		if rest, found := strings.CutPrefix(strings.TrimSpace(line), RescopedAcceptanceLine); found {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// newestCommit is when the story's branch was last committed to. A read that
// fails, after its retries on a passing fault, is a closeReadError (git-failed),
// not the zero time (mw-gq6.1315).
func (n Next) newestCommit(ctx context.Context, c *closeOut, report *NextReport, base string) (time.Time, error) {
	commits, err := readRetrying(ctx, n, report, "the commits of "+c.branch, func() ([]Commit, error) {
		return n.Landing.Commits(ctx, c.rigDir, c.branch, base)
	})
	if err != nil {
		return time.Time{}, &closeReadError{ReasonGitFailed, "the commits of " + c.branch, err}
	}
	var newest time.Time
	for _, commit := range commits {
		if commit.Committed.After(newest) {
			newest = commit.Committed
		}
	}
	return newest, nil
}

// rescopeAnswered is what the bead's comments say of the Mayor's answer to a
// changed acceptance (mw-gq6.1147, mw-gq6.1486).
type rescopeAnswered struct {
	// pinned: a RESCOPED-BY-MAYOR: comment pins the acceptance the story holds now.
	pinned bool
	// answered: pinned, and a closing comment not written by a Builder stands
	// after the branch's newest commit.
	answered bool
	// committed is when the branch was last committed to.
	committed time.Time
	// nearMisses are the comments after the newest commit, not a Builder's, that
	// are not closing comments, each with what it lacks.
	nearMisses []string
}

// lacks says, of a pinned re-scope no closing comment answers, what the Mayor's
// comments lack.
func (a rescopeAnswered) lacks(id string) string {
	said := "The bead holds a " + RescopedMark + " comment that pins this acceptance, but no closing comment of the Mayor's written after the branch's newest commit (" +
		a.committed.UTC().Format(time.RFC3339) + ") counts."
	if len(a.nearMisses) == 0 {
		said += " No comment but a Builder's stands after that commit."
	} else {
		said += "\nComments of the Mayor's after that commit that do not count:\n" + bulletList(a.nearMisses)
	}
	return said + "\n" + ClosingCommentLinesSaid + " Write one after the re-scope, then run " + RescopeRecipe(id) + " again."
}

// rescopeAnswer reads the Mayor's answer to the change of the story's
// acceptance. A read that fails is the error (a closeReadError), not a no.
func (n Next) rescopeAnswer(ctx context.Context, c *closeOut, report *NextReport, base string) (rescopeAnswered, error) {
	var answer rescopeAnswered
	comments, err := readRetrying(ctx, n, report, "the comments of "+c.id, func() ([]Comment, error) {
		return n.Tracker.StoryComments(ctx, c.id)
	})
	if err != nil {
		return answer, &closeReadError{ReasonTrackerFailed, "the comments of " + c.id, err}
	}
	now := AcceptanceAtClaim(c.detail.Acceptance)
	for _, comment := range comments {
		if hash := rescopeHash(comment); hash != "" {
			answer.pinned = hash == now
		}
	}
	if !answer.pinned {
		return answer, nil
	}
	answer.committed, err = n.newestCommit(ctx, c, report, base)
	if err != nil {
		return answer, err
	}
	for _, comment := range comments {
		if !comment.Created.After(answer.committed) || builderAuthor(comment.Author) || rescopeHash(comment) != "" {
			continue
		}
		if isClosingComment(comment.Text) {
			answer.answered = true
			return answer, nil
		}
		answer.nearMisses = append(answer.nearMisses, fmt.Sprintf("written %s, lacks %s", comment.Created.UTC().Format(time.RFC3339), closingCommentLacks(comment.Text)))
	}
	return answer, nil
}

// acceptanceRescoped says the Mayor answered the change of the story's
// acceptance since the claim (mw-gq6.1147): the bead holds a RESCOPED-BY-MAYOR:
// comment pinning the acceptance it holds now, and a closing comment not
// written by a Builder, after the branch's newest commit. A read that fails is
// the error (a closeReadError), not a no.
func (n Next) acceptanceRescoped(ctx context.Context, c *closeOut, report *NextReport, base string) (bool, error) {
	answer, err := n.rescopeAnswer(ctx, c, report, base)
	return answer.answered, err
}

// recordRescope writes the RESCOPED-BY-MAYOR: comment a --rescoped-by-mayor
// flag asks for, when the acceptance did change since the claim and the bead
// does not already hold one for this acceptance. Who and when are the
// comment's own.
func (n Next) recordRescope(ctx context.Context, c *closeOut, report *NextReport) error {
	if strings.TrimSpace(n.RescopedByMayor) == "" {
		return nil
	}
	if _, changed := AcceptanceChangedSince(c.detail.AcceptanceAtClaim, c.detail.Acceptance); !changed {
		report.Notes = append(report.Notes, "--rescoped-by-mayor: the acceptance of "+c.id+" has not changed since the claim, so nothing was recorded")
		return nil
	}
	now := AcceptanceAtClaim(c.detail.Acceptance)
	comments, err := n.Tracker.StoryComments(ctx, c.id)
	if err != nil {
		return fmt.Errorf("recording the re-scope of %s: the comments could not be read: %w", c.id, err)
	}
	for _, comment := range comments {
		if rescopeHash(comment) == now {
			return nil
		}
	}
	text := RescopedMark + " " + oneLine(n.RescopedByMayor) + "\n" + RescopedAcceptanceLine + now +
		"\n(mw next wrote this on the Mayor's word: the acceptance changed after the claim, and his closing comment answers it.)"
	if err := n.Tracker.CommentOnStory(ctx, c.id, text); err != nil {
		return fmt.Errorf("recording the re-scope of %s: %w", c.id, err)
	}
	report.Notes = append(report.Notes, "recorded the Mayor's re-scope of "+c.id+" on the bead: "+oneLine(n.RescopedByMayor))
	return nil
}

// MetByMayorMark opens the comment that records the Mayor's own run of an
// acceptance criterion a Builder's closing comment calls unmet, and that passed
// (mw-gq6.1378). It answers that closing comment and changes no acceptance; who
// wrote it and when are the comment's own.
const MetByMayorMark = "MET-BY-MAYOR:"

// MetByMayorBlank is what a --met-by-mayor with no run in it is told.
const MetByMayorBlank = "--met-by-mayor needs what the Mayor ran and what it printed: pass it as a sentence, e.g. --met-by-mayor \"ran AC 1 on the laptop at load 23: 1.68 s\""

// MetRecipe is the one line that lands a handed-back story whose unmet criterion
// the Mayor ran himself and found met.
func MetRecipe(id string) string {
	return "mw next " + id + ` --no-dispatch --met-by-mayor "<what he ran and what it printed>"`
}

// closingCommentAt is where the newest comment with a 'For the rig memory:'
// section stands among the comments, -1 when none does: the closing comment
// the landing's verdict reads.
func closingCommentAt(comments []Comment) int {
	for i := len(comments) - 1; i >= 0; i-- {
		if ParseFactProposals(comments[i].Text).Found {
			return i
		}
	}
	return -1
}

// metByMayorAnswer is the text of the newest MET-BY-MAYOR: comment that is not a
// Builder's and stands after comment at, "" when there is none or at is -1. A
// closing comment a Builder wrote after the Mayor's word is not answered by it.
func metByMayorAnswer(comments []Comment, at int) string {
	if at < 0 {
		return ""
	}
	answer := ""
	for _, comment := range comments[at+1:] {
		if !builderAuthor(comment.Author) && strings.HasPrefix(strings.TrimSpace(comment.Text), MetByMayorMark) {
			answer = strings.TrimSpace(comment.Text)
		}
	}
	return answer
}

// metByMayorAfter says the comment at position at has been answered by the
// Mayor's own run (metByMayorAnswer).
func metByMayorAfter(comments []Comment, at int) bool {
	return metByMayorAnswer(comments, at) != ""
}

// recordMetByMayor writes the MET-BY-MAYOR: comment a --met-by-mayor flag asks
// for, when the story's closing comment says a criterion is not met and no word
// of the Mayor's stands after it. Nothing of the acceptance is touched.
func (n Next) recordMetByMayor(ctx context.Context, c *closeOut, report *NextReport) error {
	run := strings.TrimSpace(n.MetByMayor)
	if !n.MetByMayorGiven || run == "" {
		return nil
	}
	comments, err := n.Tracker.StoryComments(ctx, c.id)
	if err != nil {
		return fmt.Errorf("recording the Mayor's run on %s: the comments could not be read: %w", c.id, err)
	}
	at := closingCommentAt(comments)
	if at < 0 || len(UnmetCriteria(comments[at].Text)) == 0 {
		report.Notes = append(report.Notes, "--met-by-mayor: the closing comment of "+c.id+" does not say a criterion is not met, so nothing was recorded")
		return nil
	}
	if metByMayorAfter(comments, at) {
		return nil
	}
	which := "the criteria its closing comment calls unmet"
	if found := ReadACNotMet(comments[at].Text); found.Found() {
		which = "AC " + found.Words()
	}
	text := MetByMayorMark + " " + which + ": " + oneLine(run) + "\nat " + n.now().UTC().Format(time.RFC3339) +
		"\n(mw next wrote this on the Mayor's word: he ran it himself and it passed. The acceptance did not change.)"
	if err := n.Tracker.CommentOnStory(ctx, c.id, text); err != nil {
		return fmt.Errorf("recording the Mayor's run on %s: %w", c.id, err)
	}
	report.Notes = append(report.Notes, "recorded the Mayor's run of "+which+" on "+c.id+": "+oneLine(run))
	return nil
}
