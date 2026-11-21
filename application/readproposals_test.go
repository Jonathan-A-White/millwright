package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// fullComments is a VerifyBeads that holds two landed stories, one with a
// standing proposal among its comments, and counts the reads of comments.
type fullComments struct {
	read int
}

func (f *fullComments) WorkInHand(context.Context) (application.WorkInHand, error) {
	return application.WorkInHand{}, nil
}

func (f *fullComments) ClosedStories(_ context.Context, since time.Time) ([]application.ClosedStory, error) {
	closed := since.Add(time.Hour)
	return []application.ClosedStory{
		{ID: "t-1", Type: "task", ClosedAt: closed},
		{ID: "t-2", Type: "bug", ClosedAt: closed},
	}, nil
}

const proposedComment = "PROPOSED VERIFIED by the Governor's use, from tx-1 (2026-11-01T10:00:00Z): it shows (confidence 0.9, sonnet, run r-1)"

func (f *fullComments) StoriesComments(_ context.Context, ids []string) (map[string][]application.Comment, error) {
	f.read++
	said := map[string][]application.Comment{}
	for _, id := range ids {
		said[id] = []application.Comment{{Author: "builder", Text: "Done. A long closing comment."}}
	}
	said["t-1"] = append(said["t-1"], application.Comment{Author: "mw", Text: proposedComment, Created: time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)})
	return said, nil
}

func (f *fullComments) CommentOnStory(context.Context, string, string) error { return nil }

// narrowComments is a fullComments that also reads, of each bead, only the
// comments that open PROPOSED, VERIFIED, UNVERIFIED or REJECTED.
type narrowComments struct {
	fullComments
	narrow int
}

func (n *narrowComments) StoriesProposalComments(_ context.Context, ids []string) (map[string][]application.Comment, error) {
	n.narrow++
	return map[string][]application.Comment{
		"t-1": {{Author: "mw", Text: proposedComment, Created: time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)}},
	}, nil
}

// mw-gq6.1323: the dampers line read every comment of every story landed in 14
// days (1,563 beads, 3 to 5 s at load) to find the few that open PROPOSED or
// VERIFIED. A tracker that can read only those is asked for only those, and the
// proposals found are the same.
func TestReadProposalsAsksForOnlyTheProposalCommentsWhenTheTrackerCanRead(t *testing.T) {
	narrow := &narrowComments{}
	got, err := application.ReadProposals(context.Background(), narrow, time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Bead != "t-1" {
		t.Fatalf("expected the one proposal on t-1, got %+v", got)
	}
	if narrow.read != 0 || narrow.narrow != 1 {
		t.Errorf("expected one read of the proposal comments and none of every comment, got %d and %d", narrow.narrow, narrow.read)
	}
}

// A tracker that cannot narrow the read gives the same proposals from every comment.
func TestReadProposalsReadsEveryCommentWhenTheTrackerCannotNarrow(t *testing.T) {
	full := &fullComments{}
	got, err := application.ReadProposals(context.Background(), full, time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Bead != "t-1" || full.read != 1 {
		t.Errorf("expected the one proposal on t-1 from one read of every comment, got %+v after %d reads", got, full.read)
	}
}
