package application

import (
	"context"
	"strings"
	"testing"
	"time"
)

// mw-gq6.1620: a host with no grist test key stops a story whose branch adds a
// grind example, and the Mayor runs the eval from the home. His PASS line in a
// comment of his own after the Builder's closing comment proves the example; the
// Builder's later comment does not.

const verseProof = "mw grist eval lampas bible-talk --example verse-text-ask: PASS"

// pushingLanding adds a Push that works to the commits readFailLanding reads.
type pushingLanding struct{ *readFailLanding }

func (pushingLanding) Push(context.Context, string, string, string) error { return nil }

func gristProofNext(comments ...Comment) (Next, *closeOut) {
	at := time.Now()
	landing := &readFailLanding{committed: at}
	for i := range comments {
		comments[i].Created = at.Add(time.Duration(i+1) * time.Minute)
	}
	tracker := &readFailTracker{comments: comments}
	n, c := closingReadNext(landing, tracker)
	n.Landing = pushingLanding{landing}
	n.Changes = changedStub{branchChangesStub: branchChangesStub{verseExample}}
	return n, c
}

func TestAGristExampleProvedInTheMayorsCommentAfterTheClosingCommentIsProved(t *testing.T) {
	n, c := gristProofNext(
		Comment{Author: "builder@cloud17", Text: aRealClosingComment},
		Comment{Author: "mayor@laptop", Text: "Ran it from the home:\n" + verseProof},
	)
	if refusal, refused := n.closingCommentRefusal(context.Background(), c, &NextReport{}, "main"); refused {
		t.Fatalf("expected the Mayor's PASS line to prove the example, got %+v", refusal)
	}
}

func TestAGristExampleProvedOnlyInABuildersLaterCommentIsNotProved(t *testing.T) {
	n, c := gristProofNext(
		Comment{Author: "builder@cloud17", Text: aRealClosingComment},
		Comment{Author: "builder@cloud17", Text: verseProof},
	)
	refusal, refused := n.closingCommentRefusal(context.Background(), c, &NextReport{}, "main")
	if !refused || refusal.Reason != ReasonGristExampleUnproved {
		t.Fatalf("expected a refusal for %s, got %+v, %v", ReasonGristExampleUnproved, refusal, refused)
	}
}

func TestAGristExampleProvedInAMayorsCommentBeforeTheClosingCommentIsNotProved(t *testing.T) {
	n, c := gristProofNext(
		Comment{Author: "mayor@laptop", Text: verseProof},
		Comment{Author: "builder@cloud17", Text: aRealClosingComment},
	)
	if refusal, refused := n.closingCommentRefusal(context.Background(), c, &NextReport{}, "main"); !refused || refusal.Reason != ReasonGristExampleUnproved {
		t.Fatalf("expected a refusal for %s, got %+v, %v", ReasonGristExampleUnproved, refusal, refused)
	}
}

func TestTheGristExampleRefusalNamesTheMayorsCommentNotOnlyTheClosingComment(t *testing.T) {
	for name, haveKey := range map[string]bool{"no key": false, "with key": true} {
		n, c := gristProofNext(Comment{Author: "builder@cloud17", Text: aRealClosingComment})
		n.HasGristTestKey = func() bool { return haveKey }
		refusal, refused := n.closingCommentRefusal(context.Background(), c, &NextReport{}, "main")
		if !refused || refusal.Reason != ReasonGristExampleUnproved {
			t.Fatalf("%s: expected a refusal for %s, got %+v, %v", name, ReasonGristExampleUnproved, refusal, refused)
		}
		if !strings.Contains(refusal.Said, "a comment of the Mayor's on the story after it") {
			t.Errorf("%s: expected the refusal to name the Mayor's comment, got:\n%s", name, refusal.Said)
		}
	}
}
