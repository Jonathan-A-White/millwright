package application

import (
	"context"
	"fmt"
	"strings"
)

// A story whose work was done and whose closing comment was written, but whose
// formula's steps are open (a claim given back and the story dispatched again
// pours a fresh formula, mw-gq6.1436), is landed on the Mayor's word with no
// session: mw next --no-dispatch with --met-by-mayor or --rescoped-by-mayor
// closes the formula's open steps as met by the closing comment that stands on
// the bead, and the gate and the landing run as for any story.

// OpenStepsWayOut is the words that say how a story whose closing comment is on
// the bead lands past its open formula steps, for a refusal of them and for the
// comment of a claim given back.
func OpenStepsWayOut(id string) string {
	return "mw next " + id + ` --no-dispatch --met-by-mayor "<what he ran and what it printed>" (or --rescoped-by-mayor "<why>")` +
		" closes the formula's open steps as met by that comment, and runs the gate and the landing. " + strings.TrimSuffix(ClosingCommentLinesSaid, ".")
}

// openStepsFlagsSaid is what the refusal for open steps says of the Mayor's
// flags, in the refusal's own line: they close the steps only when a closing
// comment written after the branch's newest commit stands on the story.
const openStepsFlagsSaid = "--met-by-mayor or --rescoped-by-mayor close them as met by the closing comment, when one is on the story after the branch's newest commit"

// mayorsWordGiven says mw next was run with the Mayor's word on the story
// (--met-by-mayor or --rescoped-by-mayor).
func (n Next) mayorsWordGiven() bool {
	return n.MetByMayorGiven && n.MetByMayor != "" || n.RescopedByMayor != ""
}

// closeStepsMetByTheClosingComment closes the open steps of the formula poured
// as root when the Mayor said so and the story carries a closing comment written
// after its branch's newest commit; it reports whether it did, or with preview
// that it would. A closing comment that cannot be looked for is the refusal of
// that read, and one that is not there, a Mayor who did not say so, or a close
// that fails, leaves the steps open for the refusal that follows.
func (n Next) closeStepsMetByTheClosingComment(ctx context.Context, c *closeOut, report *NextReport, mode closingStepMode, root string, open []FormulaStep, base string) (closed bool, refusal Refusal, refused bool) {
	if mode == leaveClosingStep || !n.mayorsWordGiven() {
		return false, Refusal{}, false
	}
	_, written, err := n.closingCommentTextOnStory(ctx, c, report, base)
	if err != nil {
		refusal, refused = readRefusal(err)
		return false, refusal, refused
	}
	if !written {
		return false, Refusal{}, false
	}
	if mode == previewClosingStep {
		report.Notes = append(report.Notes, fmt.Sprintf("would close the %d open formula step(s): the Mayor's word and the closing comment on the story", len(open)))
		return true, Refusal{}, false
	}
	reason := "the closing comment is on the story and the Mayor said so (closed by mw next)"
	for _, step := range open {
		if err := writeRetrying(ctx, n, report, "the close of "+step.ID, func() error {
			return n.closeFormulaBead(ctx, root, step.ID, reason)
		}); err != nil {
			return false, Refusal{}, false
		}
	}
	report.Notes = append(report.Notes, fmt.Sprintf("closed the %d open formula step(s): the Mayor's word and the closing comment on the story", len(open)))
	return true, Refusal{}, false
}
