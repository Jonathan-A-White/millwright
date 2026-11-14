package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// readFailLanding fails the read of the branch's commits with err, or after the
// first fails times, says the newest commit was made at committed.
type readFailLanding struct {
	Landing
	committed time.Time
	err       error
	fails     int
	reads     int
}

func (l *readFailLanding) Commits(context.Context, string, string, string) ([]Commit, error) {
	l.reads++
	if l.err != nil && l.reads <= l.fails {
		return nil, l.err
	}
	return []Commit{{Committed: l.committed}}, nil
}

// readFailTracker holds a story's comments and fails the read of them with err
// the first fails times.
type readFailTracker struct {
	WorkTracker
	comments []Comment
	err      error
	fails    int
	reads    int
}

func (t *readFailTracker) StoryComments(context.Context, string) ([]Comment, error) {
	t.reads++
	if t.err != nil && t.reads <= t.fails {
		return nil, t.err
	}
	return t.comments, nil
}

const aRealClosingComment = "Done.\n\nInternal: nothing for the Governor to look at\n\nFor the rig memory:\nnothing"

func closingReadNext(landing *readFailLanding, tracker *readFailTracker) (Next, *closeOut) {
	n := Next{Landing: landing, Tracker: tracker, RetryWait: func(context.Context, time.Duration) error { return nil }}
	c := &closeOut{id: "mw-t.1", rigDir: "/rig", branch: "mw/mw-t.1", target: "main"}
	c.detail.Molecule.RootID = "mw-mol-1"
	return n, c
}

func aStoryWithItsClosingComment() (*readFailLanding, *readFailTracker) {
	at := time.Now()
	return &readFailLanding{committed: at},
		&readFailTracker{comments: []Comment{{Text: aRealClosingComment, Created: at.Add(time.Minute)}}}
}

func TestAClosingCommentThatCannotBeReadIsNotRefusedAsMissingAndTheClaimIsKept(t *testing.T) {
	timeout := errors.New("bd comments mw-t.1 --json: Dolt server unreachable: i/o timeout")
	landing, tracker := aStoryWithItsClosingComment()
	tracker.err, tracker.fails = timeout, 100
	n, c := closingReadNext(landing, tracker)
	refusal, refused := n.closingCommentRefusal(context.Background(), c, &NextReport{}, "main")
	if !refused || refusal.Reason != ReasonTrackerFailed {
		t.Fatalf("expected a refusal for %s, got %+v, %v", ReasonTrackerFailed, refusal, refused)
	}
	if !strings.Contains(refusal.Why, "the comments of mw-t.1 could not be read") || !strings.Contains(refusal.Why, "i/o timeout") {
		t.Errorf("expected the refusal to name the read and its failure, got %q", refusal.Why)
	}
	if ContinuesFrom(refusal.Reason) {
		t.Errorf("expected %s not to hand its branch on", refusal.Reason)
	}
}

func TestAClosingCommentWhoseCommitsCannotBeReadIsNotRefusedAsMissingAndTheClaimIsKept(t *testing.T) {
	landing, tracker := aStoryWithItsClosingComment()
	landing.err, landing.fails = errors.New("git log: bad object"), 100
	n, c := closingReadNext(landing, tracker)
	refusal, refused := n.closingCommentRefusal(context.Background(), c, &NextReport{}, "main")
	if !refused || refusal.Reason != ReasonGitFailed {
		t.Fatalf("expected a refusal for %s, got %+v, %v", ReasonGitFailed, refusal, refused)
	}
	if !strings.Contains(refusal.Why, "the commits of mw/mw-t.1 could not be read") || !strings.Contains(refusal.Why, "bad object") {
		t.Errorf("expected the refusal to name the read and its failure, got %q", refusal.Why)
	}
}

func TestAClosingCommentReadThatMetAPassingFaultIsTriedAgainAndFound(t *testing.T) {
	landing, tracker := aStoryWithItsClosingComment()
	tracker.err = &TransientFault{Why: "dolt server unreachable", Err: errors.New("i/o timeout")}
	tracker.fails = 2
	n, c := closingReadNext(landing, tracker)
	report := &NextReport{}
	if refusal, refused := n.closingCommentRefusal(context.Background(), c, report, "main"); refused {
		t.Fatalf("expected the comment to be found after the retries, got %+v", refusal)
	}
	if tracker.reads != 3 {
		t.Errorf("expected 3 reads of the comments, got %d", tracker.reads)
	}
	if len(report.Notes) != 2 {
		t.Errorf("expected a note for each retry, got %v", report.Notes)
	}
}

func TestAClosingCommentReadThatFailsEveryTryIsRefusedAfterTheRetries(t *testing.T) {
	landing, tracker := aStoryWithItsClosingComment()
	tracker.err = &TransientFault{Why: "dolt server unreachable", Err: errors.New("i/o timeout")}
	tracker.fails = 100
	n, c := closingReadNext(landing, tracker)
	refusal, refused := n.closingCommentRefusal(context.Background(), c, &NextReport{}, "main")
	if !refused || refusal.Reason != ReasonTrackerFailed {
		t.Fatalf("expected a refusal for %s, got %+v, %v", ReasonTrackerFailed, refusal, refused)
	}
	if tracker.reads != ReadRetries+1 {
		t.Errorf("expected %d reads, got %d", ReadRetries+1, tracker.reads)
	}
}

func TestAClosingCommentThatCannotBeReadIsNotTakenForOpenStepsWhenOneStepIsOpen(t *testing.T) {
	landing, tracker := aStoryWithItsClosingComment()
	tracker.err, tracker.fails = errors.New("i/o timeout"), 100
	n, c := closingReadNext(landing, tracker)
	step := FormulaStep{ID: "mw-mol-s", Title: ClosingCommentStepTitle + " for mw-t.1"}
	if refusal, ok := readRefusal(&closeReadError{ReasonTrackerFailed, "x", errors.New("y")}); !ok || refusal.Reason != ReasonTrackerFailed {
		t.Fatalf("expected readRefusal to name a tracker failure, got %+v, %v", refusal, ok)
	}
	if written, err := n.closingCommentWritten(context.Background(), c, &NextReport{}, step, "main"); written || err == nil {
		t.Errorf("expected an error and no 'written', got %v, %v", written, err)
	}
}
