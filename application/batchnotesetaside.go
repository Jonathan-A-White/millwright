package application

import (
	"context"
	"errors"
	"fmt"
)

// noteSetAside tells, on each story a landed batch set aside and in the report,
// that the rest of the batch landed without it and it stays queued (mw-qqxlwm3.2).
// A comment that cannot be written is a note: the landing is done either way.
func (b BatchLand) noteSetAside(ctx context.Context, report *BatchReport) {
	n := b.Next
	for _, s := range report.SetAside {
		if s.HandedBack {
			continue // handBackRepeaters has said so on it
		}
		if b.leftToANewAttempt(ctx, s.ID, report) {
			continue
		}
		line := fmt.Sprintf("%s was set aside by %s (%s); the other %s landed on %s at %s without waiting for it, and it stays queued",
			s.ID, report.Label(), firstLine(s.Why), countStories(len(report.Merged)), report.Target, shortCommit(report.Commit))
		next := "This story stays queued."
		if len(s.Conflicts) > 0 {
			next = fmt.Sprintf("This story stays queued; if the next batch sets it aside for a conflict too, it is handed back to a Builder to rebase it on %s.", report.Target)
		} else if s.ConflictSetAsides > 0 {
			next = "This story stays queued; if the next batch sets it aside too, it is handed back to a Builder."
		}
		comment := fmt.Sprintf("Set aside by %s: %s\nThe other %s landed on %s at %s without waiting for it. %s",
			report.Label(), s.Why, countStories(len(report.Merged)), report.Target, shortCommit(report.Commit), next)
		left := false
		err := writeRetrying(ctx, n, nil, "the note on a story set aside", func() error {
			err := n.Tracker.CommentOnStoryIfQueued(ctx, s.ID, comment)
			if errors.Is(err, ErrStoryMovedOn) {
				left = true // a retry landed since the re-read above: its note would be false
				return nil
			}
			return err
		})
		if left {
			report.Notes = append(report.Notes, b.leftAloneNote(s.ID))
			continue
		}
		report.Notes = append(report.Notes, line)
		if err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("the note saying why %s was set aside could not be written on it: %v", s.ID, err))
		}
	}
}
