package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// A story claimed on one acceptance and now holding another, whose Mayor wrote a
// re-scope and a closing comment of his own: answered, were it read.
func anAnsweredAcceptanceChange() (*readFailLanding, *readFailTracker, Next, *closeOut) {
	committed := time.Now().Add(-time.Hour)
	landing := &readFailLanding{committed: committed}
	tracker := &readFailTracker{comments: []Comment{
		{Author: "Mayor", Created: committed.Add(20 * time.Minute), Text: rescopeClosing},
		rescopeComment("AC reworded", "1. One.\n2. Two, reworded.", committed.Add(25*time.Minute)),
	}}
	n, c := closingReadNext(landing, tracker)
	c.detail.Acceptance = "1. One.\n2. Two, reworded."
	c.detail.AcceptanceAtClaim = AcceptanceAtClaim("1. One.\n2. Two.")
	return landing, tracker, n, c
}

func TestAcceptanceRescopedReadFailureOfTheCommentsIsTrackerFailedNotAcceptanceChanged(t *testing.T) {
	_, tracker, n, c := anAnsweredAcceptanceChange()
	tracker.err, tracker.fails = errors.New("bd comments mw-t.1 --json: Dolt server unreachable: i/o timeout"), 100
	refusal, refused := n.acceptanceRefusal(context.Background(), c, &NextReport{}, "main")
	if !refused || refusal.Reason != ReasonTrackerFailed {
		t.Fatalf("expected a refusal for %s, got %+v, %v", ReasonTrackerFailed, refusal, refused)
	}
	if !strings.Contains(refusal.Why, "the comments of mw-t.1 could not be read") || !strings.Contains(refusal.Why, "i/o timeout") {
		t.Errorf("expected the refusal to name the read and its failure, got %q", refusal.Why)
	}
	if ContinuesFrom(refusal.Reason) {
		t.Errorf("expected %s not to hand its branch on, so the claim is kept", refusal.Reason)
	}
}

func TestAcceptanceRescopedReadFailureOfTheCommitsIsGitFailedNotAcceptanceChanged(t *testing.T) {
	landing, _, n, c := anAnsweredAcceptanceChange()
	landing.err, landing.fails = errors.New("git log: bad object"), 100
	refusal, refused := n.acceptanceRefusal(context.Background(), c, &NextReport{}, "main")
	if !refused || refusal.Reason != ReasonGitFailed {
		t.Fatalf("expected a refusal for %s, got %+v, %v", ReasonGitFailed, refusal, refused)
	}
	if !strings.Contains(refusal.Why, "the commits of mw/mw-t.1 could not be read") || !strings.Contains(refusal.Why, "bad object") {
		t.Errorf("expected the refusal to name the read and its failure, got %q", refusal.Why)
	}
}

func TestAcceptanceRescopedReadThatMetAPassingFaultIsTriedAgainAndAnswersTheChange(t *testing.T) {
	_, tracker, n, c := anAnsweredAcceptanceChange()
	tracker.err = &TransientFault{Why: "dolt server unreachable", Err: errors.New("i/o timeout")}
	tracker.fails = 2
	if refusal, refused := n.acceptanceRefusal(context.Background(), c, &NextReport{}, "main"); refused {
		t.Fatalf("expected the answered change to pass after the retries, got %+v", refusal)
	}
}

func TestAcceptanceRescopedUnansweredChangeIsStillRefusedAsAcceptanceChanged(t *testing.T) {
	_, tracker, n, c := anAnsweredAcceptanceChange()
	tracker.comments = nil
	refusal, refused := n.acceptanceRefusal(context.Background(), c, &NextReport{}, "main")
	if !refused || refusal.Reason != ReasonAcceptanceChanged {
		t.Fatalf("expected a refusal for %s, got %+v, %v", ReasonAcceptanceChanged, refusal, refused)
	}
}
