package application

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// HandedBackMark opens a comment in which a Builder hands a story back, and
// RePathedMark opens the comment that says what changed afterwards: until one
// follows the other, nothing in the factory runs the story again, since the
// same Builder given the same story would hand it back the same way
// (mw-gq6.418).
const (
	HandedBackMark = "HANDED BACK"
	RePathedMark   = "RE-PATHED:"
)

// HandedBackHeading heads the section of `mw status` naming the stories whose
// newest word is a hand-back. The report leaves it out when there are none.
const HandedBackHeading = "HANDED BACK"

// HandedBack says whether a story's comments, oldest first, end in a hand-back
// nobody has answered: the latest comment opening HANDED BACK has no later one
// opening RE-PATHED:. at is when that hand-back was written.
func HandedBack(comments []Comment) (at time.Time, handedBack bool) {
	for _, c := range comments {
		switch text := strings.TrimSpace(c.Text); {
		case strings.HasPrefix(text, HandedBackMark):
			at, handedBack = c.Created, true
		case strings.HasPrefix(text, RePathedMark):
			at, handedBack = time.Time{}, false
		}
	}
	return at, handedBack
}

// HandedBackStory is a story whose newest word is a hand-back, and when that
// was written.
type HandedBackStory struct {
	Detail StoryDetail
	At     time.Time
}

// handedBackOf reads a story's comments, when it has any, and says whether it
// stands handed back. A story with no comments is never read.
func handedBackOf(ctx context.Context, tracker WorkTracker, detail StoryDetail) (time.Time, bool, error) {
	if detail.CommentCount == 0 {
		return time.Time{}, false, nil
	}
	comments, err := tracker.StoryComments(ctx, detail.Story.ID)
	if err != nil {
		return time.Time{}, false, err
	}
	at, handedBack := HandedBack(comments)
	return at, handedBack, nil
}

// handedBackDay is the date of a hand-back as a person reads it; "an unknown
// day" when the tracker gave the comment none.
func handedBackDay(at time.Time) string {
	if at.IsZero() {
		return "an unknown day"
	}
	return at.UTC().Format("2006-01-02")
}

// repathedWith is the mw retry that records what changed and lets the story run.
func repathedWith(id string) string {
	return fmt.Sprintf("mw retry %s --repathed %q", id, "<what changed>")
}
