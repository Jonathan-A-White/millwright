package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// letGoFixture is an applyFixture whose stories mw-e.1 and mw-e.2 are two
// let-go To do cards the weekly card of the epic mw-e listed.
func letGoFixture(t *testing.T, offered []string) *applyFixture {
	t.Helper()
	f := newApplyFixture(t)
	ctx := context.Background()
	for _, id := range []string{"mw-e.1", "mw-e.2"} {
		mustDo(t, f.tracker.SetStatus(id, application.StatusOpen))
		mustDo(t, f.tracker.SetLabels(id, application.LabelHitl, application.LabelHitlTodo))
		mustDo(t, f.tracker.SetStoryMetadata(ctx, id, map[string]string{application.TodoRowField: application.TodoRowLetGo}))
	}
	listed, _ := json.Marshal([]string{"mw-e.1", "mw-e.2"})
	mustDo(t, f.tracker.SetNote(ctx, application.TodoLetGoKey("mw-e"), string(listed)))
	question, _ := json.Marshal(map[string]any{"txid": "tx-weekly", "options": offered})
	mustDo(t, f.tracker.SetNote(ctx, application.PosternQuestionKey("mw-e"), string(question)))
	return f
}

func (f *applyFixture) answer(t *testing.T, txid, bead, answer string) {
	t.Helper()
	reply, _ := json.Marshal(application.PosternReply{Bead: bead, Answer: answer})
	f.message(t, releaseTapGovernorKey, txid, string(reply))
}

// A Drop tap on a question that never offered it closes nothing, and the Mayor
// is told why.
func TestLetGoDropIsRefusedWhenTheQuestionNeverOfferedIt(t *testing.T) {
	f := letGoFixture(t, []string{"A: Keep them all"})
	f.answer(t, "tx-drop", "mw-e", "B: Drop them all")

	f.apply(t)

	for _, id := range []string{"mw-e.1", "mw-e.2"} {
		if reason := f.tracker.CloseReason(id); reason != "" {
			t.Fatalf("expected %s left open, it was closed: %s", id, reason)
		}
	}
	if subjects := f.subjects(t); len(subjects) != 2 || subjects[1] != "Let go not applied: mw-e" {
		t.Fatalf("expected the answer's mail and a not-applied mail, got %v", subjects)
	}
}

// The words of the weekly card's answers on a card that kept no list of let-go
// cards close nothing: only the weekly card's own question is its.
func TestLetGoTapOnACardWithNoListedCardsChangesNothing(t *testing.T) {
	f := letGoFixture(t, []string{"A: Keep them all", "B: Drop them all"})
	mustDo(t, f.tracker.SetNote(context.Background(), application.TodoLetGoKey("mw-e"), ""))
	f.answer(t, "tx-drop", "mw-e", "B: Drop them all")

	f.apply(t)

	for _, id := range []string{"mw-e.1", "mw-e.2"} {
		if reason := f.tracker.CloseReason(id); reason != "" {
			t.Fatalf("expected %s left open, it was closed: %s", id, reason)
		}
	}
	if subjects := f.subjects(t); len(subjects) != 1 {
		t.Fatalf("expected only the answer's mail, got %v", subjects)
	}
}

// A card the pass has since moved out of let-go is not dropped by the tap.
func TestLetGoDropLeavesACardNoLongerInLetGo(t *testing.T) {
	f := letGoFixture(t, []string{"A: Keep them all", "B: Drop them all"})
	mustDo(t, f.tracker.SetStoryMetadata(context.Background(), "mw-e.2", map[string]string{application.TodoRowField: application.TodoRowLater}))
	f.answer(t, "tx-drop", "mw-e", "B: Drop them all")

	f.apply(t)

	if reason := f.tracker.CloseReason("mw-e.1"); reason != application.TodoLetGoReason {
		t.Fatalf("expected mw-e.1 closed with %q, got %q", application.TodoLetGoReason, reason)
	}
	if reason := f.tracker.CloseReason("mw-e.2"); reason != "" {
		t.Fatalf("expected mw-e.2 left open, it was closed: %s", reason)
	}
}

// letGoNightly is a nightly job whose list epic mw-e has two let-go cards, on a
// Monday morning in the week the Let go card is due, with his review queue as
// queue says.
type letGoNightly struct {
	nightly *application.TodoNightly
	tracker *apptest.FakeTracker
	sender  *apptest.FakePosternSender
	errOut  *bytes.Buffer
	now     time.Time
}

func newLetGoNightly(t *testing.T, queue application.ReviewQueueReader) *letGoNightly {
	t.Helper()
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-e", domain.Path{})
	for _, id := range []string{"mw-e.1", "mw-e.2"} {
		tracker.AddStory("mw-e", domain.Story{ID: id, Title: "Card " + id})
		mustDo(t, tracker.SetStatus(id, application.StatusOpen))
		mustDo(t, tracker.SetLabels(id, application.LabelHitl, application.LabelHitlTodo))
		mustDo(t, tracker.SetStoryMetadata(ctx, id, map[string]string{application.TodoRowField: application.TodoRowLetGo}))
	}
	l := &letGoNightly{
		tracker: tracker, sender: &apptest.FakePosternSender{}, errOut: &bytes.Buffer{},
		now: time.Date(2026, time.November, 9, 7, 0, 0, 0, time.UTC),
	}
	l.nightly = &application.TodoNightly{
		WeekStart: time.Monday, Zone: time.UTC,
		Rank:    func(context.Context) error { return nil },
		Tracker: tracker, Notes: tracker, Sender: l.sender,
		Home: func(context.Context) (bool, error) { return true, nil },
		Now:  func() time.Time { return l.now },
		Err:  l.errOut,
		Hold: application.ReviewHold{Queue: queue, Tracker: tracker, Err: l.errOut},
	}
	return l
}

// pass makes the night's pass on a later day, as the follower would.
func (l *letGoNightly) pass(t *testing.T, at time.Time) {
	t.Helper()
	l.now = at
	job := l.nightly.Job()
	if err := job.Run(context.Background()); err != nil {
		t.Fatalf("the pass failed: %v", err)
	}
}

func heldLetGoComments(l *letGoNightly) []string {
	comments, err := l.tracker.StoryComments(context.Background(), "mw-e")
	if err != nil {
		panic(err)
	}
	var held []string
	for _, c := range comments {
		if strings.HasPrefix(c.Text, application.QuestionForMondayPrefix) {
			held = append(held, c.Text)
		}
	}
	return held
}

func TestWeeklyLetGoOverTheReviewQueueLimitIsHeldForMonday(t *testing.T) {
	l := newLetGoNightly(t, fixedQueue{size: 12, limit: 8})
	l.pass(t, l.now)

	if sent := l.sender.Sent(); len(sent) != 0 {
		t.Fatalf("expected no send over the limit, got %+v", sent)
	}
	held := heldLetGoComments(l)
	if len(held) != 1 {
		t.Fatalf("expected one QUESTION for Monday on the list epic, got none")
	}
	for _, want := range []string{"Let go: 2 cards look ready to go", "mw-e.1", "mw-e.2",
		application.TodoKeepAllOption, application.TodoDropAllOption, "carried by the digest of 2026-11-09"} {
		if !strings.Contains(held[0], want) {
			t.Errorf("expected the held comment to say %q:\n%s", want, held[0])
		}
	}
	if week, _ := l.tracker.Note(context.Background(), application.TodoRankWeeklyKey); week != "2026-11-09" {
		t.Errorf("expected the week marked 2026-11-09, got %q", week)
	}
	if said := strings.Count(l.errOut.String(), "\n"); said != 1 || !strings.Contains(l.errOut.String(), "held") {
		t.Errorf("expected one line on the error stream saying it was held, got %q", l.errOut.String())
	}
}

func TestWeeklyLetGoHeldIsNotWrittenAgainOnTuesday(t *testing.T) {
	l := newLetGoNightly(t, fixedQueue{size: 12, limit: 8})
	l.pass(t, l.now)
	l.pass(t, l.now.Add(24*time.Hour))
	l.pass(t, l.now.Add(24*time.Hour))

	if held := heldLetGoComments(l); len(held) != 1 {
		t.Fatalf("expected one held comment for the week, got %d: %q", len(held), held)
	}
	if sent := l.sender.Sent(); len(sent) != 0 {
		t.Fatalf("expected no send, got %+v", sent)
	}
}

func TestWeeklyLetGoUnderTheLimitSendsAsBefore(t *testing.T) {
	l := newLetGoNightly(t, fixedQueue{size: 8, limit: 8})
	l.pass(t, l.now)

	sent := l.sender.Sent()
	if len(sent) != 1 || sent[0].Class != "decision-needed" || sent[0].Bead != "mw-e" || len(sent[0].Options) != 2 {
		t.Fatalf("expected the Let go card sent, got %+v", sent)
	}
	if held := heldLetGoComments(l); len(held) != 0 {
		t.Fatalf("expected nothing held, got %q", held)
	}
}

func TestWeeklyLetGoHeldKeepsTheLetGoNoteSoAnAnswerStillApplies(t *testing.T) {
	l := newLetGoNightly(t, fixedQueue{size: 12, limit: 8})
	l.pass(t, l.now)

	kept, _ := l.tracker.Note(context.Background(), application.TodoLetGoKey("mw-e"))
	if kept != `["mw-e.1","mw-e.2"]` {
		t.Fatalf("expected the Let go note to keep both ids, got %q", kept)
	}
}

func TestWeeklyLetGoOverAQueueThatCannotBeCountedSendsTheCard(t *testing.T) {
	l := newLetGoNightly(t, fixedQueue{err: errors.New("bd is down")})
	l.pass(t, l.now)

	if sent := l.sender.Sent(); len(sent) != 1 {
		t.Fatalf("expected the card sent, got %+v", sent)
	}
	if said := strings.Count(l.errOut.String(), "\n"); said != 1 || !strings.Contains(l.errOut.String(), "bd is down") {
		t.Errorf("expected one line on the error stream naming the failed count, got %q", l.errOut.String())
	}
}

func TestWeeklyLetGoHeldAfterTheDigestIsCarriedByTheNextMondaysDigest(t *testing.T) {
	l := newLetGoNightly(t, fixedQueue{size: 12, limit: 8})
	l.pass(t, time.Date(2026, time.November, 9, 12, 0, 0, 0, time.UTC))

	held := heldLetGoComments(l)
	if len(held) != 1 || !strings.Contains(held[0], "carried by the digest of 2026-11-16") {
		t.Fatalf("expected the next Monday's digest named, got %q", held)
	}
}
