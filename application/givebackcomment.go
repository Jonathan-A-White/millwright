package application

import (
	"context"
	"fmt"
	"time"
)

// givenBackClosingComment is what the comment of a claim given back adds when
// the story already carries a closing comment written after the newest commit of
// its branch (the Mayor's or a Builder's, in the shape the close-out reads): the
// work may be done, and mw next --no-dispatch with the Mayor's word lands it
// with no fresh session (mw-gq6.1436). It reads the comments itself, the one read a
// claim given back makes of them. Empty when there is none, or when the
// comments or the commits cannot be read: the comment is then as it was.
func (d Dispatch) givenBackClosingComment(ctx context.Context, detail StoryDetail) string {
	id := detail.Story.ID
	path, err := detail.Path()
	if err != nil || d.Landing == nil {
		return ""
	}
	rigDir, checkedOut := d.Rigs[path.Rig]
	if !checkedOut {
		return ""
	}
	commits, err := d.Landing.Commits(ctx, rigDir, StoryBranch(id), StartPoint(d.remote(), path.Branch))
	if err != nil || len(commits) == 0 {
		return ""
	}
	var newest time.Time
	for _, commit := range commits {
		if commit.Committed.After(newest) {
			newest = commit.Committed
		}
	}
	comments, err := d.Tracker.StoryComments(ctx, id)
	if err != nil {
		return ""
	}
	var found time.Time
	for _, comment := range comments {
		if comment.Created.After(newest) && isClosingComment(comment.Text) {
			found = comment.Created
		}
	}
	if found.IsZero() {
		return ""
	}
	return fmt.Sprintf(" Its closing comment, written %s after the newest commit of %s, is on the story: if the work is done it needs no fresh session. %s.",
		found.UTC().Format(time.RFC3339), StoryBranch(id), OpenStepsWayOut(id))
}
