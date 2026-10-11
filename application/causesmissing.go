package application

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// causesMissing is the stories refused within CausesMissingWindow of now that
// no cause is recorded for: a story whose comments hold a refusal (RefusedPhrase)
// left in the window. Comments are read only for a story that carries some and
// names no cause.
func causesMissing(ctx context.Context, stories TesterStories, tracker WorkTracker, now time.Time) ([]StoryDetail, error) {
	since := now.Add(-CausesMissingWindow)
	touched, err := stories.StoriesSince(ctx, since)
	if err != nil {
		return nil, err
	}
	var unnamed []StoryDetail
	var ids []string
	for _, d := range touched {
		if d.IsEpic || d.Cause != "" || d.CommentCount == 0 {
			continue
		}
		unnamed = append(unnamed, d)
		ids = append(ids, d.Story.ID)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	comments, err := tracker.StoriesComments(ctx, ids)
	if err != nil {
		return nil, err
	}
	var missing []StoryDetail
	for _, d := range unnamed {
		for _, comment := range comments[d.Story.ID] {
			if _, refused := refusalIn(comment.Text); refused && !comment.Created.Before(since) {
				missing = append(missing, d)
				break
			}
		}
	}
	return missing, nil
}

// writeCausesMissing writes the CAUSES MISSING section, one line a story, and
// nothing at all when there are none.
func (r StatusReport) writeCausesMissing(b *strings.Builder) {
	if len(r.CausesMissing) == 0 {
		return
	}
	clip(b, fmt.Sprintf("%s (%d)", CausesMissingHeading, len(r.CausesMissing)))
	for _, d := range r.CausesMissing {
		clip(b, fmt.Sprintf("  %s · %s", d.Story.ID, d.Story.Title))
	}
	clip(b, "  name each: "+CauseLine("<id>"))
	b.WriteString("\n")
}
