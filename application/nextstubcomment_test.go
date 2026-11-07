package application

import (
	"context"
	"strings"
	"testing"
	"time"
)

// stubLanding says the branch's newest commit was made at committed.
type stubLanding struct {
	Landing
	committed time.Time
}

func (l stubLanding) Commits(context.Context, string, string, string) ([]Commit, error) {
	return []Commit{{Committed: l.committed}}, nil
}

// commentsTracker holds a story's comments and nothing else.
type commentsTracker struct {
	WorkTracker
	comments []Comment
}

func (t commentsTracker) StoryComments(context.Context, string) ([]Comment, error) {
	return t.comments, nil
}

func closingRefusalFor(t *testing.T, texts ...string) (Refusal, bool) {
	t.Helper()
	at := time.Now()
	var comments []Comment
	for i, text := range texts {
		comments = append(comments, Comment{Text: text, Created: at.Add(time.Duration(i+1) * time.Minute)})
	}
	n := Next{Landing: stubLanding{committed: at}, Tracker: commentsTracker{comments: comments}}
	c := &closeOut{id: "mw-t.1", rigDir: "/rig", branch: "mw/mw-t.1", target: "main"}
	c.detail.Molecule.RootID = "mw-mol-1"
	return n.closingCommentRefusal(context.Background(), c, &NextReport{}, "main")
}

func TestAStubAsTheOnlyCommentAfterTheNewestCommitIsRefusedNamingTheDashAndTheFileFlag(t *testing.T) {
	refusal, refused := closingRefusalFor(t, "-")
	if !refused || refusal.Reason != ReasonStubClosingComment {
		t.Fatalf("expected a refusal for %s, got %+v, %v", ReasonStubClosingComment, refusal, refused)
	}
	if want := "the newest comment is '-' (1 byte): bd comments add takes -f <file>, not -"; !strings.Contains(refusal.Why, want) {
		t.Errorf("expected the refusal to say %q, got %q", want, refusal.Why)
	}
}

func TestOnlyAStubIsHeldANoCommentOrALongOneIsHandedBack(t *testing.T) {
	long := "Built it, and the tests pass, but I forgot the closing sections."
	for name, texts := range map[string][]string{"none": nil, "a long comment that is not a closing comment": {long}} {
		refusal, refused := closingRefusalFor(t, texts...)
		if !refused || refusal.Reason != ReasonNoClosingComment {
			t.Errorf("%s: expected %s, got %+v, %v", name, ReasonNoClosingComment, refusal, refused)
		}
	}
	if refusal, refused := closingRefusalFor(t, "-", long); refused && refusal.Reason == ReasonStubClosingComment {
		t.Errorf("expected a stub followed by a longer comment not to be held as a stub, got %+v", refusal)
	}
}

func TestARealClosingCommentAfterAStubLetsTheStoryThrough(t *testing.T) {
	real := "Done.\n\nInternal: nothing for the Governor to look at\n\nFor the rig memory:\nnothing"
	if refusal, refused := closingRefusalFor(t, "-", real); refused {
		t.Errorf("expected no refusal, got %+v", refusal)
	}
}
