package application

import (
	"context"
	"strings"
	"testing"
	"time"
)

// rescopeTracker holds a story's comments, and takes the comment a re-scope
// writes.
type rescopeTracker struct {
	WorkTracker
	comments []Comment
	now      time.Time
}

func (t *rescopeTracker) StoryComments(context.Context, string) ([]Comment, error) {
	return t.comments, nil
}

func (t *rescopeTracker) CommentOnStory(_ context.Context, _ string, text string) error {
	t.comments = append(t.comments, Comment{Author: "Mayor", Text: text, Created: t.now})
	return nil
}

const rescopeClosing = "Done as re-scoped.\n\nInternal: nothing for the Governor to look at\n\nFor the rig memory:\nnothing"

// rescopedStory is a story claimed on one acceptance and now holding another,
// whose branch was last committed at the returned time.
func rescopedStory(comments func(committed time.Time) []Comment) (Next, *closeOut, *rescopeTracker) {
	committed := time.Now().Add(-time.Hour)
	tracker := &rescopeTracker{now: committed.Add(30 * time.Minute)}
	if comments != nil {
		tracker.comments = comments(committed)
	}
	c := &closeOut{id: "mw-t.1", rigDir: "/rig", branch: "mw/mw-t.1", target: "main"}
	c.detail.Acceptance = "1. One.\n2. Two, reworded."
	c.detail.AcceptanceAtClaim = AcceptanceAtClaim("1. One.\n2. Two.")
	return Next{Landing: stubLanding{committed: committed}, Tracker: tracker}, c, tracker
}

func rescopeComment(why string, acceptance string, at time.Time) Comment {
	return Comment{Author: "Mayor", Created: at, Text: RescopedMark + " " + why + "\n" + RescopedAcceptanceLine + AcceptanceAtClaim(acceptance)}
}

func TestARescopeWithAMayorClosingCommentAnswersTheChangedAcceptance(t *testing.T) {
	n, c, _ := rescopedStory(func(committed time.Time) []Comment {
		return []Comment{
			{Author: "builder@cloud2", Created: committed.Add(5 * time.Minute), Text: "old " + rescopeClosing},
			{Author: "Mayor", Created: committed.Add(20 * time.Minute), Text: rescopeClosing},
			rescopeComment("AC 4 and 5 reworded", "1. One.\n2. Two, reworded.", committed.Add(25*time.Minute)),
		}
	})
	if rescoped, err := n.acceptanceRescoped(context.Background(), c, &NextReport{}, "main"); err != nil || !rescoped {
		t.Fatal("expected a recorded re-scope with a Mayor closing comment to answer the change")
	}
}

func TestARescopeNoMayorClosingCommentAnswersIsStillRefused(t *testing.T) {
	cases := map[string]func(committed time.Time) []Comment{
		"no re-scope comment": func(committed time.Time) []Comment {
			return []Comment{{Author: "Mayor", Created: committed.Add(20 * time.Minute), Text: rescopeClosing}}
		},
		"no closing comment": func(committed time.Time) []Comment {
			return []Comment{rescopeComment("why", "1. One.\n2. Two, reworded.", committed.Add(25*time.Minute))}
		},
		"only the Builder's closing comment": func(committed time.Time) []Comment {
			return []Comment{
				{Author: "builder@cloud2", Created: committed.Add(5 * time.Minute), Text: rescopeClosing},
				rescopeComment("why", "1. One.\n2. Two, reworded.", committed.Add(25*time.Minute)),
			}
		},
		"a Mayor closing comment from before the last commit": func(committed time.Time) []Comment {
			return []Comment{
				{Author: "Mayor", Created: committed.Add(-5 * time.Minute), Text: rescopeClosing},
				rescopeComment("why", "1. One.\n2. Two, reworded.", committed.Add(25*time.Minute)),
			}
		},
		"a re-scope of an acceptance since changed again": func(committed time.Time) []Comment {
			return []Comment{
				{Author: "Mayor", Created: committed.Add(20 * time.Minute), Text: rescopeClosing},
				rescopeComment("why", "1. One.\n2. Something else.", committed.Add(25*time.Minute)),
			}
		},
	}
	for name, comments := range cases {
		n, c, _ := rescopedStory(comments)
		if rescoped, err := n.acceptanceRescoped(context.Background(), c, &NextReport{}, "main"); err != nil || rescoped {
			t.Errorf("%s: expected the change not to be answered", name)
		}
	}
}

func TestRecordRescopeWritesWhoWhenAndWhyOnTheBeadOnce(t *testing.T) {
	n, c, tracker := rescopedStory(func(committed time.Time) []Comment {
		return []Comment{{Author: "Mayor", Created: committed.Add(20 * time.Minute), Text: rescopeClosing}}
	})
	n.RescopedByMayor = "AC 4 and 5 reworded after the headless seek artefact"
	report := &NextReport{}
	for i := 0; i < 2; i++ {
		if err := n.recordRescope(context.Background(), c, report); err != nil {
			t.Fatalf("recording: %v", err)
		}
	}
	var written []string
	for _, comment := range tracker.comments {
		if strings.HasPrefix(comment.Text, RescopedMark) {
			written = append(written, comment.Text)
		}
	}
	if len(written) != 1 || !strings.Contains(written[0], n.RescopedByMayor) {
		t.Fatalf("expected one re-scope comment holding the why, got %q", written)
	}
	if rescoped, err := n.acceptanceRescoped(context.Background(), c, &NextReport{}, "main"); err != nil || !rescoped {
		t.Error("expected the comment just written to answer the change")
	}
}

func TestRecordRescopeWritesNothingWhenTheAcceptanceDidNotChange(t *testing.T) {
	n, c, tracker := rescopedStory(nil)
	c.detail.Acceptance = "1. One.\n2. Two."
	n.RescopedByMayor = "no reason"
	if err := n.recordRescope(context.Background(), c, &NextReport{}); err != nil {
		t.Fatal(err)
	}
	if len(tracker.comments) != 0 {
		t.Errorf("expected no comment, got %+v", tracker.comments)
	}
}
