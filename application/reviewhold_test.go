package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

type fixedQueue struct {
	size, limit int
	err         error
}

func (q fixedQueue) ReviewQueue(context.Context) (int, int, error) { return q.size, q.limit, q.err }

func aCard(bead string) application.PosternSendRequest {
	return application.PosternSendRequest{
		Class: "decision-needed", Bead: bead, Text: "Ship it now or wait?",
		Options: []string{"A: ship", "B: wait|mw-a.1:held"}, Recommend: "A: ship",
	}
}

func holdOver(t *testing.T, queue fixedQueue) (application.ReviewHold, *apptest.FakeTracker, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-e", domain.Path{})
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.1", Title: "A story"})
	var out, errOut bytes.Buffer
	return application.ReviewHold{Queue: queue, Tracker: tracker, Out: &out, Err: &errOut}, tracker, &out, &errOut
}

func TestReviewHoldJudgesOnlyAQueueOverItsLimit(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		size, limit int
		over        bool
	}{{8, 8, false}, {9, 8, true}, {0, 8, false}, {21, 8, true}} {
		hold, _, _, _ := holdOver(t, fixedQueue{c.size, c.limit, nil})
		if got := hold.Judge(ctx, aCard("mw-e.1")); got.Over != c.over || got.Size != c.size || got.Limit != c.limit {
			t.Errorf("queue %d of %d: verdict %+v, want over = %v", c.size, c.limit, got, c.over)
		}
	}
}

func TestReviewHoldLeavesAnythingButACardOnABeadAlone(t *testing.T) {
	hold, _, _, _ := holdOver(t, fixedQueue{size: 99, limit: 8})
	for name, req := range map[string]application.PosternSendRequest{
		"plain message": {Class: "message", Text: "hello"},
		"card, no bead": {Class: "decision-needed", Text: "which?"},
		"landing":       {Class: "landing", Text: "landed", Bead: ""},
	} {
		if got := hold.Judge(context.Background(), req); got.Over {
			t.Errorf("%s: held, want it left alone", name)
		}
	}
}

func TestReviewHoldSendsWhenTheQueueCannotBeCounted(t *testing.T) {
	hold, _, _, errOut := holdOver(t, fixedQueue{err: errors.New("bd is down")})
	if got := hold.Judge(context.Background(), aCard("mw-e.1")); got.Over {
		t.Fatal("held a card because the queue could not be counted")
	}
	if !strings.Contains(errOut.String(), "bd is down") {
		t.Fatalf("the failure is not said: %q", errOut.String())
	}
}

func TestReviewHoldWritesTheQuestionOnTheBeadAndNamesBeadQueueAndLimit(t *testing.T) {
	ctx := context.Background()
	hold, tracker, out, _ := holdOver(t, fixedQueue{size: 11, limit: 8})
	req := aCard("mw-e.1")
	verdict := hold.Judge(ctx, req)
	if err := hold.Hold(ctx, req, verdict); err != nil {
		t.Fatal(err)
	}
	comments := tracker.Comments("mw-e.1")
	if len(comments) != 1 || !strings.HasPrefix(comments[0], "QUESTION for Monday:") {
		t.Fatalf("comments = %q, want one opening QUESTION for Monday:", comments)
	}
	for _, want := range []string{"Ship it now or wait?", "A: ship", "B: wait", "Recommended: A: ship"} {
		if !strings.Contains(comments[0], want) {
			t.Errorf("the comment does not carry %q:\n%s", want, comments[0])
		}
	}
	for _, want := range []string{"mw-e.1", "11", "8"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the line does not name %q: %s", want, out.String())
		}
	}
}

func TestReviewHoldWritesWhyOnTheBeadWhenSentOverTheLimit(t *testing.T) {
	ctx := context.Background()
	hold, tracker, _, _ := holdOver(t, fixedQueue{size: 11, limit: 8})
	req := aCard("mw-e.1")
	if err := hold.Overrode(ctx, req, hold.Judge(ctx, req), "  the deploy stops at noon ", "direct:abc"); err != nil {
		t.Fatal(err)
	}
	comments := tracker.Comments("mw-e.1")
	if len(comments) != 1 || !strings.HasPrefix(comments[0], "SENT OVER THE LIMIT:") || !strings.Contains(comments[0], "the deploy stops at noon") || !strings.Contains(comments[0], "direct:abc") {
		t.Fatalf("comments = %q", comments)
	}
}

func TestDamperSurveyCountsTheReviewQueueAsTheReviewLineDoes(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-e", domain.Path{})
	for _, id := range []string{"mw-e.1", "mw-e.2", "mw-e.3"} {
		tracker.AddStory("mw-e", domain.Story{ID: id, Title: id})
	}
	ctx := context.Background()
	mustDo(t, tracker.SetNote(ctx, application.PosternQuestionKey("mw-e.1"), `{"q":"which?"}`))
	mustDo(t, tracker.SetNote(ctx, application.PosternQuestionKey("mw-e.2"), `{"q":"which?"}`))
	mustDo(t, tracker.SetLabels("mw-e.3", application.LabelDemo))
	survey := application.DamperSurvey{Tracker: tracker, Notes: tracker, Graph: tracker, Limits: application.DamperLimits{ReviewQueue: 2}}
	size, limit, err := survey.ReviewQueue(ctx)
	if err != nil || size != 3 || limit != 2 {
		t.Fatalf("ReviewQueue = %d, %d, %v; want 3, 2", size, limit, err)
	}
	report, err := survey.Dampers(ctx, mustWork(t, tracker), time.Now())
	if err != nil || report.Counts.ReviewQueue != size {
		t.Fatalf("the DAMPERS count = %d (%v), want the same %d", report.Counts.ReviewQueue, err, size)
	}
	if size, limit, _ := (application.DamperSurvey{Tracker: tracker, Notes: tracker}).ReviewQueue(ctx); limit != application.DefaultDamperReviewQueue || size != 3 {
		t.Fatalf("with no limit set: %d of %d, want 3 of the default", size, limit)
	}
}

func mustWork(t *testing.T, tracker *apptest.FakeTracker) application.WorkInHand {
	t.Helper()
	work, err := tracker.WorkInHand(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return work
}

func digestHeldFixture(t *testing.T) *digestFixture {
	t.Helper()
	f := newDigestFixture()
	for _, id := range []string{"mw-e.1", "mw-e.2", "mw-e.3", "mw-e.4"} {
		f.tracker.AddStory("mw-e", domain.Story{ID: id, Title: "Story " + id})
	}
	held := func(q string) string {
		r := aCard("x")
		r.Text = q
		return application.HeldQuestionComment(r)
	}
	ctx := context.Background()
	// held, nothing since
	mustDo(t, f.tracker.CommentOnStory(ctx, "mw-e.1", held("Should the cap be 4?")))
	// held, then answered
	mustDo(t, f.tracker.CommentOnStory(ctx, "mw-e.2", held("Old question?")))
	mustDo(t, f.tracker.CommentOnStory(ctx, "mw-e.2", "ANSWER 2026-11-09T10:00:00Z from 02ab, txid direct:2: A"))
	// held, then a card sent
	mustDo(t, f.tracker.CommentOnStory(ctx, "mw-e.3", held("Sent later?")))
	mustDo(t, f.tracker.CommentOnStory(ctx, "mw-e.3", "QUESTION 2026-11-09T10:00:00Z asked by postern, txid direct:3: Sent later? (recommended A; options A, B)"))
	// answered once, held again after: held
	mustDo(t, f.tracker.CommentOnStory(ctx, "mw-e.4", "ANSWER 2026-11-01T10:00:00Z from 02ab, txid direct:4: A"))
	mustDo(t, f.tracker.CommentOnStory(ctx, "mw-e.4", held("Second thing\nover two lines?")))
	return f
}

func TestDigestListsEveryQuestionHeldForMondayWithNoLaterAnswerOrCard(t *testing.T) {
	f := digestHeldFixture(t)
	report := f.run(t, false)
	if len(report.Held) != 2 || report.Held[0].Bead != "mw-e.1" || report.Held[1].Bead != "mw-e.4" {
		t.Fatalf("held = %+v, want mw-e.1 and mw-e.4", report.Held)
	}
	sent := f.sender.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent %d cards, want one (a week with only held questions still goes out)", len(sent))
	}
	for _, want := range []string{"- mw-e.1: Should the cap be 4?", "- mw-e.4: Second thing over two lines?"} {
		if !strings.Contains(sent[0].Text, want) {
			t.Errorf("the card does not hold the line %q:\n%s", want, sent[0].Text)
		}
	}
	for _, not := range []string{"mw-e.2", "mw-e.3"} {
		if strings.Contains(sent[0].Text, not) {
			t.Errorf("the card names %s, whose question was met:\n%s", not, sent[0].Text)
		}
	}
}

func TestDigestDryRunPrintsTheSameHeldLines(t *testing.T) {
	f := digestHeldFixture(t)
	f.run(t, true)
	for _, want := range []string{"- mw-e.1: Should the cap be 4?", "- mw-e.4: Second thing over two lines?"} {
		if !strings.Contains(f.out.String(), want) {
			t.Errorf("the dry run does not print %q:\n%s", want, f.out.String())
		}
	}
	if len(f.sender.Sent()) != 0 {
		t.Fatal("a dry run sent a card")
	}
}

func TestDigestLeavesOutAHeldQuestionOfAClosedBead(t *testing.T) {
	f := digestHeldFixture(t)
	mustDo(t, f.tracker.CloseStory(context.Background(), "mw-e.1", "done"))
	f.run(t, true)
	if strings.Contains(f.out.String(), "mw-e.1: Should") {
		t.Fatalf("a closed bead's question is listed:\n%s", f.out.String())
	}
}

func TestDigestReadsHeldQuestionsOffTheGraphToo(t *testing.T) {
	f := newDigestFixture()
	ctx := context.Background()
	// A held story is not in the live stories WorkInHand lists; the graph holds it.
	f.tracker.AddStory("mw-e", domain.Story{ID: "mw-e.9", Title: "Parked"})
	mustDo(t, f.tracker.CommentOnStory(ctx, "mw-e.9", application.HeldQuestionComment(aCard("mw-e.9"))))
	mustDo(t, f.tracker.HoldStory(ctx, "mw-e.9"))
	read := func(graph application.BeadGraph) []application.HeldQuestion {
		report, err := application.Digest{Tracker: f.tracker, Graph: graph, Sender: f.sender, Parent: "mw-6ww.119", DryRun: true, Out: &f.out}.Run(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return report.Held
	}
	if held := read(nil); len(held) != 0 {
		t.Fatalf("without the graph held = %+v, want none", held)
	}
	if held := read(f.tracker); len(held) != 1 || held[0].Bead != "mw-e.9" {
		t.Fatalf("with the graph held = %+v, want mw-e.9", held)
	}
}
