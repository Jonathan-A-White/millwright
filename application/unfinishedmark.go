package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// AddedLine is one line a branch adds, as the file has it at the branch's tip:
// the file's path, the line's number in it (from 1) and its text without the
// diff's sign.
type AddedLine struct {
	Path   string
	Number int
	Text   string
}

// BranchAddedLines is the port the lines a story's branch adds are read
// through (mw-gq6.1713): git, in the rig's own checkout. It is its own port,
// not a method of BranchChanges, whose fakes sit in many test files.
type BranchAddedLines interface {
	// AddedLines is every line the commits on branch beyond base add, in the
	// diff's own order: the diff from their merge base to branch, three dots,
	// so not what base gained meanwhile. A line the branch removes or leaves as
	// context is not one. A branch that is not there has added nothing.
	AddedLines(ctx context.Context, rigDir, base, branch string) ([]AddedLine, error)
}

// unfinishedMark finds the words by which a line says the work is unfinished: a
// marker word a Builder leaves for the part it did not do, and the phrase of a
// part not yet read. Case does not matter. (The pattern is built in pieces, so
// that this file's own added lines do not say it.)
var unfinishedMark = regexp.MustCompile(`(?i)(provi` + `sional|still to be ` + `read)`)

// ReasonUnfinishedMark is a branch whose own diff adds a line that says the
// work is unfinished (mw-gq6.1713) and whose closing comment has no
// 'Accepted gap:' line. Like a stale table of contents, it is the Builder's to
// mend in its worktree, so the branch is continued.
const ReasonUnfinishedMark Reason = "unfinished-mark"

// AcceptedGapMark is what a line of the closing comment opens with to accept an
// unfinished mark the branch adds, followed by the Mayor's reason
// (mw-gq6.1713).
const AcceptedGapMark = "Accepted gap:"

// unfinishedMarkQuoted is how many marked lines the refusal quotes.
const unfinishedMarkQuoted = 10

// unfinishedMarkLeftAlone says a file's lines are not asked: a doc may say a
// thing is unfinished, as a plan or a note does, and no landing ships it as
// code. A path under a docs/ directory, at any depth, and a .md file.
func unfinishedMarkLeftAlone(file string) bool {
	return strings.HasPrefix(file, "docs/") || strings.Contains(file, "/docs/") ||
		strings.HasSuffix(strings.ToLower(file), ".md")
}

// acceptedGap is the reason of the first line of text that opens with
// AcceptedGapMark (any case, after a bullet or bold markers) and has a reason
// after it.
func acceptedGap(text string) (string, bool) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimLeft(strings.TrimSpace(line), "-*>• ")
		if len(line) < len(AcceptedGapMark) || !strings.EqualFold(line[:len(AcceptedGapMark)], AcceptedGapMark) {
			continue
		}
		if reason := strings.TrimSpace(strings.Trim(line[len(AcceptedGapMark):], "*_ ")); reason != "" {
			return reason, true
		}
	}
	return "", false
}

// unfinishedMarkRefusal is the refusal of a branch whose own diff adds a line
// that says the work is unfinished (mw-gq6.1713; mw-1l901so.2 landed one chapter
// of a book under such a marker, and .3 only the web): a line the branch adds,
// outside docs/ and the .md files, that holds the marker words. It names each
// file:line. The closing comment, or a comment of the Mayor's after it, that has
// a line opening 'Accepted gap:' and a reason answers it. A line the branch only
// removes or leaves as context is not asked. A rig with no AddedLines, or a
// branch that cannot be read, is not refused: the check is a net.
func (n Next) unfinishedMarkRefusal(ctx context.Context, c *closeOut, report *NextReport, base string) (Refusal, bool) {
	if n.AddedLines == nil {
		return Refusal{}, false
	}
	added, err := n.AddedLines.AddedLines(ctx, c.rigDir, StartPoint(n.remote(), c.target), c.branch)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the lines %s added could not be read to look for unfinished work: %v", c.id, err))
		return Refusal{}, false
	}
	var marked []AddedLine
	for _, line := range added {
		if !unfinishedMarkLeftAlone(line.Path) && unfinishedMark.MatchString(line.Text) {
			marked = append(marked, line)
		}
	}
	if len(marked) == 0 {
		return Refusal{}, false
	}
	text, at, comments, _, err := n.closingCommentOnStory(ctx, c, report, base)
	if err != nil {
		return readRefusal(err)
	}
	if reason, accepted := acceptedGap(gristProofText(text, comments, at)); accepted {
		report.Notes = append(report.Notes, fmt.Sprintf("%d line(s) %s adds say the work is unfinished; accepted: %s", len(marked), c.id, reason))
		return Refusal{}, false
	}
	var named, quoted []string
	for i, line := range marked {
		named = append(named, fmt.Sprintf("%s:%d", line.Path, line.Number))
		if i < unfinishedMarkQuoted {
			quoted = append(quoted, fmt.Sprintf("- %s:%d: %s", line.Path, line.Number, strings.TrimSpace(line.Text)))
		}
	}
	if len(marked) > unfinishedMarkQuoted {
		quoted = append(quoted, fmt.Sprintf("... and %d more", len(marked)-unfinishedMarkQuoted))
	}
	why := fmt.Sprintf("%s adds %d line(s) that say the work is unfinished: %s", c.branch, len(marked), strings.Join(named[:min(len(named), unfinishedMarkQuoted)], ", "))
	said := fmt.Sprintf("The line(s) the branch adds:\n\n%s\n\n"+
		"A line that marks the work as a stand-in, or part of it as not yet read, says the story is not finished, and a landing would put that on the target branch as done. "+
		"Nothing was merged: the branch is still the session's to amend. Finish the work and take the line out, then run `mw next %s` again. "+
		"If the Mayor accepts the gap, the closing comment (or a comment of the Mayor's after it) has a line opening %q followed by his reason.",
		strings.Join(quoted, "\n"), c.id, AcceptedGapMark)
	return Refusal{ReasonUnfinishedMark, why, said}, true
}
