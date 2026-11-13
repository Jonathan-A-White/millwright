package main

import (
	"context"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-6ww.175.42: the review hold of mw todo nightly and mw grill --ask judges a
// card through reviewQueueReader. Each judgment counts the queue afresh (a card
// sent changes it), so it reads bd list --all once per card, and no more.
func TestReviewHoldReadsListAllOncePerCardJudged(t *testing.T) {
	asked := aBdThatRecords(t)
	dir := t.TempDir()
	t.Setenv("MW_VAULT", dir)
	t.Setenv("MW_HOST", "laptop")

	hold := application.ReviewHold{Queue: reviewQueueReader(mwGateway(dir, "laptop"))}
	card := application.PosternSendRequest{Class: "decision-needed", Text: "A question?", Bead: "e-1"}
	const cards = 3
	for i := 0; i < cards; i++ {
		hold.Judge(context.Background(), card)
	}
	if lists := listsAll(t, asked); lists != cards {
		t.Errorf("judging %d cards ran bd list --all %d times, want once per card:\n%s", cards, lists, readAsked(t, asked))
	}
}
