package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// clearFixture is an open bead t.1 whose card was asked under txid
// direct:abc, the note still marking it open.
func clearFixture(t *testing.T) *apptest.FakeTracker {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("t", domain.Path{})
	tracker.AddStory("t", domain.Story{ID: "t.1", Title: "t.1"})
	note, err := json.Marshal(map[string]any{"txid": "direct:abc", "q": "Release?", "options": []string{"A: Release"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetNote(context.Background(), application.PosternQuestionKey("t.1"), string(note)); err != nil {
		t.Fatal(err)
	}
	return tracker
}

func showBead(t *testing.T, tracker *apptest.FakeTracker, id string) application.StoryDetail {
	t.Helper()
	found, err := tracker.ShowBeads(context.Background(), []string{id})
	if err != nil || len(found) != 1 {
		t.Fatalf("showing %s: %v, %v", id, found, err)
	}
	return found[0]
}

// A cleared card loses its note, its bead says why, and his thread under the
// card hears it; the bead itself is not changed.
func TestPosternClearClearsTheNoteCommentsAndRepliesUnderTheCard(t *testing.T) {
	ctx := context.Background()
	tracker := clearFixture(t)
	sender := &apptest.FakePosternSender{}
	before := showBead(t, tracker, "t.1")
	var out bytes.Buffer

	err := application.PosternClear{Beads: tracker, Notes: tracker, Sender: sender, Out: &out}.Run(ctx, "t.1", "he approved it by hand")
	if err != nil {
		t.Fatal(err)
	}
	if note, _ := tracker.Note(ctx, application.PosternQuestionKey("t.1")); note != "" {
		t.Errorf("expected the note cleared, got %q", note)
	}
	if comments := tracker.Comments("t.1"); len(comments) != 1 || comments[0] != "Card cleared by mw postern clear: he approved it by hand" {
		t.Errorf("got comments %q", comments)
	}
	sent := sender.Sent()
	if len(sent) != 1 || sent[0].Re != "direct:abc" || sent[0].Thread != "t.1" || !sent[0].Recorded ||
		!strings.Contains(sent[0].Text, "he approved it by hand") || !strings.Contains(sent[0].Text, "cleared") {
		t.Errorf("expected one reply under the card's txid saying it is cleared and why, got %+v", sent)
	}
	after := showBead(t, tracker, "t.1")
	if after.CommentCount != before.CommentCount+1 {
		t.Errorf("expected one comment more, got %d from %d", after.CommentCount, before.CommentCount)
	}
	after.CommentCount = before.CommentCount
	if !reflect.DeepEqual(before, after) {
		t.Errorf("the bead changed: %+v became %+v", before, after)
	}
}

// A bead with no question note is refused, naming it, and nothing changes.
func TestPosternClearRefusesABeadWithNoQuestionNote(t *testing.T) {
	ctx := context.Background()
	tracker := clearFixture(t)
	tracker.AddStory("t", domain.Story{ID: "t.2", Title: "t.2"})
	sender := &apptest.FakePosternSender{}

	err := application.PosternClear{Beads: tracker, Notes: tracker, Sender: sender}.Run(ctx, "t.2", "x")
	if err == nil || !strings.Contains(err.Error(), "t.2") || !strings.Contains(err.Error(), "no question note") {
		t.Fatalf("expected a refusal naming t.2, got %v", err)
	}
	if len(tracker.Comments("t.2")) != 0 || len(sender.Sent()) != 0 {
		t.Errorf("expected nothing changed, got %q and %+v", tracker.Comments("t.2"), sender.Sent())
	}
	if note, _ := tracker.Note(ctx, application.PosternQuestionKey("t.1")); note == "" {
		t.Errorf("another bead's note was cleared")
	}
}

// A why is required: a cleared card says why on the bead and under the card.
func TestPosternClearNeedsAWhy(t *testing.T) {
	tracker := clearFixture(t)
	err := application.PosternClear{Beads: tracker, Notes: tracker, Sender: &apptest.FakePosternSender{}}.Run(context.Background(), "t.1", "  ")
	if err == nil || !strings.Contains(err.Error(), "--why") {
		t.Fatalf("expected a refusal asking for --why, got %v", err)
	}
	if note, _ := tracker.Note(context.Background(), application.PosternQuestionKey("t.1")); note == "" {
		t.Errorf("the note was cleared without a why")
	}
}

// --dry-run prints the three acts and changes nothing.
func TestPosternClearDryRunPrintsTheThreeActsAndChangesNothing(t *testing.T) {
	ctx := context.Background()
	tracker := clearFixture(t)
	sender := &apptest.FakePosternSender{}
	var out bytes.Buffer

	err := application.PosternClear{Beads: tracker, Notes: tracker, Sender: sender, DryRun: true, Out: &out}.Run(ctx, "t.1", "why not")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"would clear the question note of t.1", "would comment on t.1: Card cleared by mw postern clear: why not", "would reply under direct:abc"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("expected %q in %q", want, out.String())
		}
	}
	if note, _ := tracker.Note(ctx, application.PosternQuestionKey("t.1")); note == "" {
		t.Errorf("the note was cleared on a dry run")
	}
	if len(tracker.Comments("t.1")) != 0 || len(sender.Sent()) != 0 {
		t.Errorf("a dry run changed something: %q, %+v", tracker.Comments("t.1"), sender.Sent())
	}
}

// A note that never recorded a txid has no thread to reply under: it is
// cleared and commented, and no reply is sent.
func TestPosternClearOfABareMarkSendsNoReply(t *testing.T) {
	ctx := context.Background()
	tracker := clearFixture(t)
	if err := tracker.SetNote(ctx, application.PosternQuestionKey("t.1"), "{}"); err != nil {
		t.Fatal(err)
	}
	sender := &apptest.FakePosternSender{}
	if err := (application.PosternClear{Beads: tracker, Notes: tracker, Sender: sender}).Run(ctx, "t.1", "stale mark"); err != nil {
		t.Fatal(err)
	}
	if note, _ := tracker.Note(ctx, application.PosternQuestionKey("t.1")); note != "" || len(sender.Sent()) != 0 || len(tracker.Comments("t.1")) != 1 {
		t.Errorf("got note %q, sent %+v, comments %q", note, sender.Sent(), tracker.Comments("t.1"))
	}
}

// twoCards sets t.1's note to a newer card direct:new that overtook an older
// card direct:old.
func twoCards(t *testing.T, tracker *apptest.FakeTracker) {
	t.Helper()
	note, err := json.Marshal(map[string]any{
		"txid": "direct:new", "q": "Newer?", "options": []string{"A: Go"},
		"earlier": []map[string]any{{"txid": "direct:old", "q": "Older?", "options": []string{"A: Go"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetNote(context.Background(), application.PosternQuestionKey("t.1"), string(note)); err != nil {
		t.Fatal(err)
	}
}

// --txid of an overtaken card replies under THAT card, and the newer card and
// its note are left alone.
func TestPosternClearTxidClearsOnlyTheOlderCard(t *testing.T) {
	ctx := context.Background()
	tracker := clearFixture(t)
	twoCards(t, tracker)
	sender := &apptest.FakePosternSender{}

	err := application.PosternClear{Beads: tracker, Notes: tracker, Sender: sender, Txid: "direct:old"}.Run(ctx, "t.1", "overtaken")
	if err != nil {
		t.Fatal(err)
	}
	sent := sender.Sent()
	if len(sent) != 1 || sent[0].Re != "direct:old" || sent[0].Thread != "t.1" || !strings.Contains(sent[0].Text, "overtaken") {
		t.Errorf("expected one reply under direct:old, got %+v", sent)
	}
	note, _ := tracker.Note(ctx, application.PosternQuestionKey("t.1"))
	var left struct {
		Txid    string
		Q       string
		Earlier []struct{ Txid string }
	}
	if err := json.Unmarshal([]byte(note), &left); err != nil || left.Txid != "direct:new" || left.Q != "Newer?" || len(left.Earlier) != 0 {
		t.Errorf("expected the newer card's note kept without the older card, got %q: %v", note, err)
	}
	if comments := tracker.Comments("t.1"); len(comments) != 1 || !strings.Contains(comments[0], "direct:old") || !strings.HasPrefix(comments[0], application.PosternClearComment+"overtaken") {
		t.Errorf("got comments %q", comments)
	}
}

// --txid of the newest card clears it and lets the card it overtook lead the
// note again.
func TestPosternClearTxidOfTheNewestCardRestoresTheOlder(t *testing.T) {
	ctx := context.Background()
	tracker := clearFixture(t)
	twoCards(t, tracker)
	sender := &apptest.FakePosternSender{}

	if err := (application.PosternClear{Beads: tracker, Notes: tracker, Sender: sender, Txid: "direct:new"}).Run(ctx, "t.1", "wrong"); err != nil {
		t.Fatal(err)
	}
	if sent := sender.Sent(); len(sent) != 1 || sent[0].Re != "direct:new" {
		t.Errorf("expected one reply under direct:new, got %+v", sent)
	}
	note, _ := tracker.Note(ctx, application.PosternQuestionKey("t.1"))
	var left struct{ Txid, Q string }
	if err := json.Unmarshal([]byte(note), &left); err != nil || left.Txid != "direct:old" || left.Q != "Older?" {
		t.Errorf("expected the older card to lead the note, got %q: %v", note, err)
	}
}

// A --txid the note does not hold is refused, naming it, and nothing changes.
func TestPosternClearTxidNotOnTheBeadIsRefused(t *testing.T) {
	ctx := context.Background()
	tracker := clearFixture(t)
	twoCards(t, tracker)
	before, _ := tracker.Note(ctx, application.PosternQuestionKey("t.1"))
	sender := &apptest.FakePosternSender{}

	err := application.PosternClear{Beads: tracker, Notes: tracker, Sender: sender, Txid: "direct:nope"}.Run(ctx, "t.1", "x")
	if err == nil || !strings.Contains(err.Error(), "direct:nope") {
		t.Fatalf("expected a refusal naming direct:nope, got %v", err)
	}
	after, _ := tracker.Note(ctx, application.PosternQuestionKey("t.1"))
	if after != before || len(sender.Sent()) != 0 || len(tracker.Comments("t.1")) != 0 {
		t.Errorf("expected nothing changed, got %q, %+v, %q", after, sender.Sent(), tracker.Comments("t.1"))
	}
}

// Without --txid a bead with several cards clears as before: the whole note
// goes and the reply is under the newest card.
func TestPosternClearWithoutTxidClearsTheWholeNote(t *testing.T) {
	ctx := context.Background()
	tracker := clearFixture(t)
	twoCards(t, tracker)
	sender := &apptest.FakePosternSender{}

	if err := (application.PosternClear{Beads: tracker, Notes: tracker, Sender: sender}).Run(ctx, "t.1", "done"); err != nil {
		t.Fatal(err)
	}
	if note, _ := tracker.Note(ctx, application.PosternQuestionKey("t.1")); note != "" {
		t.Errorf("expected the note cleared, got %q", note)
	}
	if sent := sender.Sent(); len(sent) != 1 || sent[0].Re != "direct:new" {
		t.Errorf("expected one reply under the newest card, got %+v", sent)
	}
	if comments := tracker.Comments("t.1"); len(comments) != 1 || comments[0] != "Card cleared by mw postern clear: done" {
		t.Errorf("got comments %q", comments)
	}
}
