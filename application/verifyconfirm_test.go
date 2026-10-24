package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// confirmRig is a verify rig whose match has run, so that its beads carry the
// PROPOSED comment mw verify match really writes, and a card sits on d-1.
type confirmRig struct {
	*verifyRig
	notes  *apptest.FakeTracker
	sender *apptest.FakePosternSender
	run    string
}

func newConfirmRig(t *testing.T) *confirmRig {
	t.Helper()
	r := newVerifyRig(t, verifyTwoAnswers)
	result, err := r.match().Run(context.Background(), application.VerifyOptions{Since: 7 * 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	notes := apptest.NewFakeTracker()
	card, _ := json.Marshal(map[string]any{"txid": "direct:card", "q": "Seen it?", "options": []string{"A: Yes"}})
	if err := notes.SetNote(context.Background(), application.PosternQuestionKey("d-1"), string(card)); err != nil {
		t.Fatal(err)
	}
	r.beads.Written, r.beads.Shut = nil, nil
	return &confirmRig{verifyRig: r, notes: notes, sender: &apptest.FakePosternSender{}, run: result.Run}
}

func (r *confirmRig) confirm() application.VerifyConfirm {
	return application.VerifyConfirm{Beads: r.beads, Notes: r.notes, Sender: r.sender, Out: &r.out}
}

func TestVerifyConfirmWritesTheStandingFormClosesTheDemoAndClearsItsCard(t *testing.T) {
	ctx := context.Background()
	r := newConfirmRig(t)
	if err := r.confirm().Run(ctx, application.VerifyConfirmRequest{Bead: "d-1", Txid: "direct:aaa", As: "mayor"}); err != nil {
		t.Fatal(err)
	}
	want := "VERIFIED by the Governor's use, inferred by the Mayor from direct:aaa: the green tick after Save (proposed by mw verify match, run " + r.run + ")"
	if len(r.beads.Written) < 1 || r.beads.Written[0].Text != want {
		t.Fatalf("the first comment is not the standing form:\n got %+v\nwant %q", r.beads.Written, want)
	}
	if len(r.beads.Shut) != 1 || r.beads.Shut[0].ID != "d-1" || !strings.HasPrefix(r.beads.Shut[0].Text, "VERIFIED by the Governor's use") {
		t.Fatalf("expected d-1 closed on the VERIFIED reason, got %+v", r.beads.Shut)
	}
	if note, _ := r.notes.Note(ctx, application.PosternQuestionKey("d-1")); note != "" {
		t.Errorf("the card's note should be cleared, got %q", note)
	}
	if sent := r.sender.Sent(); len(sent) != 1 || sent[0].Re != "direct:card" || sent[0].Thread != "d-1" {
		t.Errorf("expected one reply under the card, got %+v", sent)
	}
}

func TestVerifyConfirmWithNoProposalIsRefusedUnlessShowsIsGiven(t *testing.T) {
	ctx := context.Background()
	r := newConfirmRig(t)
	err := r.confirm().Run(ctx, application.VerifyConfirmRequest{Bead: "d-1", Txid: "direct:zzz"})
	if err == nil || !strings.Contains(err.Error(), "no PROPOSED comment") || !strings.Contains(err.Error(), "direct:zzz") || !strings.Contains(err.Error(), "--shows") {
		t.Fatalf("expected a plain refusal naming the txid and --shows, got %v", err)
	}
	if len(r.beads.Written) != 0 || len(r.beads.Shut) != 0 || len(r.sender.Sent()) != 0 {
		t.Fatalf("a refusal changes nothing: %+v %+v", r.beads.Written, r.beads.Shut)
	}

	if err := r.confirm().Run(ctx, application.VerifyConfirmRequest{Bead: "d-1", Txid: "direct:zzz", Shows: "the tick on his screen", As: "deputy"}); err != nil {
		t.Fatal(err)
	}
	want := "VERIFIED by the Governor's use, inferred by the Deputy from direct:zzz: the tick on his screen (by hand)"
	if len(r.beads.Written) < 1 || r.beads.Written[0].Text != want {
		t.Fatalf("got %+v, want %q", r.beads.Written, want)
	}
}

func TestVerifyConfirmOnAClosedLandingCommentsAndChangesNoStatus(t *testing.T) {
	ctx := context.Background()
	r := newConfirmRig(t)
	r.beads.Details = map[string]application.StoryDetail{
		"s-1": {Story: domain.Story{ID: "s-1", Title: "Story s-1"}, Status: application.StatusClosed},
	}
	err := r.confirm().Run(ctx, application.VerifyConfirmRequest{Bead: "s-1", Txid: "direct:bbb", Shows: "the lamp lit"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.beads.Written) != 1 || firstWordOf(r.beads.Written[0].Text) != "VERIFIED" {
		t.Fatalf("expected one VERIFIED comment, got %+v", r.beads.Written)
	}
	if len(r.beads.Shut) != 0 || len(r.sender.Sent()) != 0 {
		t.Fatalf("a landing is not closed again and has no card: %+v %+v", r.beads.Shut, r.sender.Sent())
	}
}

func TestVerifyConfirmRefusesADemoWhoseEpicAsksHisOwnWord(t *testing.T) {
	ctx := context.Background()
	r := newConfirmRig(t)
	r.beads.Details = map[string]application.StoryDetail{
		"e-1": {Story: domain.Story{ID: "e-1", Title: "Rotate the signing keys"}, IsEpic: true, Status: application.StatusOpen},
	}
	err := r.confirm().Run(ctx, application.VerifyConfirmRequest{Bead: "d-1", Txid: "direct:aaa"})
	if err == nil || !strings.Contains(err.Error(), "his own word") || !strings.Contains(err.Error(), "keys") {
		t.Fatalf("expected a refusal that says why, got %v", err)
	}
	if len(r.beads.Written) != 0 || len(r.beads.Shut) != 0 {
		t.Fatalf("a refusal changes nothing: %+v %+v", r.beads.Written, r.beads.Shut)
	}
}

func TestVerifyConfirmRefusesABeadAlreadyVerifiedAndAnUnknownAs(t *testing.T) {
	ctx := context.Background()
	r := newConfirmRig(t)
	if err := r.confirm().Run(ctx, application.VerifyConfirmRequest{Bead: "d-1", Txid: "direct:aaa", As: "governor"}); err == nil || !strings.Contains(err.Error(), "--as") {
		t.Fatalf("expected --as refused, got %v", err)
	}
	if err := r.confirm().Run(ctx, application.VerifyConfirmRequest{Bead: "d-1", Txid: "direct:aaa"}); err != nil {
		t.Fatal(err)
	}
	r.beads.Details["d-1"] = verifyDemo("d-1", "e-1", "still open in the fake")
	if err := r.confirm().Run(ctx, application.VerifyConfirmRequest{Bead: "d-1", Txid: "direct:aaa"}); err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("expected the second confirm refused, got %v", err)
	}
}

func TestVerifyListPrintsTheProposalsWaitingAndOmitsAVerifiedBead(t *testing.T) {
	ctx := context.Background()
	r := newConfirmRig(t)
	// d-2's proposal is under the confidence, so only d-1 has one; give s-1 one
	// and then record d-1 verified.
	r.beads.Comments["s-1"] = append(r.beads.Comments["s-1"], application.Comment{
		Text: "PROPOSED VERIFIED by the Governor's use, from direct:bbb (2026-10-21 16:00Z): the lamp lit (confidence 0.85, haiku, run r-2)",
	})
	var out bytes.Buffer
	l := application.VerifyList{Beads: r.beads, Now: func() time.Time { return verifyNow }, Out: &out}
	waiting, err := l.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(waiting) != 2 {
		t.Fatalf("expected two proposals waiting, got %+v", waiting)
	}
	said := out.String()
	for _, want := range []string{"d-1", "direct:aaa", "the green tick after Save", "0.90", "mw verify confirm d-1 --txid direct:aaa", "s-1", "mw verify confirm s-1 --txid direct:bbb"} {
		if !strings.Contains(said, want) {
			t.Errorf("the list does not say %q:\n%s", want, said)
		}
	}

	if err := r.confirm().Run(ctx, application.VerifyConfirmRequest{Bead: "d-1", Txid: "direct:aaa"}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	waiting, err = l.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(waiting) != 1 || waiting[0].Bead != "s-1" || strings.Contains(out.String(), "d-1") {
		t.Fatalf("a verified bead is left out, got %+v:\n%s", waiting, out.String())
	}
}

func TestVerifyListMarksADemoThatNeedsHisWord(t *testing.T) {
	r := newConfirmRig(t)
	r.beads.Details = map[string]application.StoryDetail{
		"e-1": {Story: domain.Story{ID: "e-1", Title: "Spend money on the box"}, IsEpic: true, Status: application.StatusOpen},
	}
	var out bytes.Buffer
	waiting, err := application.VerifyList{Beads: r.beads, Now: func() time.Time { return verifyNow }, Out: &out}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(waiting) != 1 || waiting[0].NeedsHisWord == "" || !strings.Contains(out.String(), "his own word") {
		t.Fatalf("expected d-1 listed with the reason, got %+v:\n%s", waiting, out.String())
	}
}

// A txid written before the bead landed cannot confirm it (mw-gq6.742).
func TestVerifyConfirmRefusesAProposalWrittenBeforeTheDemosEpicLanded(t *testing.T) {
	ctx := context.Background()
	r := newConfirmRig(t)
	// The proposal for d-1 is from direct:aaa, written 17:00Z; e-1 landed 17:30Z.
	r.beads.Closed = append(r.beads.Closed, verifyClosed("s-e1", "e-1", "landed on main", 30*time.Minute))
	err := r.confirm().Run(ctx, application.VerifyConfirmRequest{Bead: "d-1", Txid: "direct:aaa"})
	if err == nil || !strings.Contains(err.Error(), "before d-1 landed") || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("expected a refusal naming the landing and --force, got %v", err)
	}
	if len(r.beads.Written) != 0 || len(r.beads.Shut) != 0 {
		t.Fatalf("a refusal changes nothing: %+v %+v", r.beads.Written, r.beads.Shut)
	}
	if err := r.confirm().Run(ctx, application.VerifyConfirmRequest{Bead: "d-1", Txid: "direct:aaa", Force: "he used it again later"}); err != nil {
		t.Fatal(err)
	}
	if len(r.beads.Written) < 1 || firstWordOf(r.beads.Written[0].Text) != "VERIFIED" || !strings.Contains(r.beads.Written[0].Text, "forced although") || !strings.Contains(r.beads.Written[0].Text, "he used it again later") {
		t.Fatalf("the reason stands in the record: %+v", r.beads.Written)
	}
}

func TestVerifyConfirmByHandDatesTheTxidByTheInboxAndRefusesOneBeforeTheLanding(t *testing.T) {
	ctx := context.Background()
	r := newConfirmRig(t)
	r.beads.Details = map[string]application.StoryDetail{
		"s-1": {Story: domain.Story{ID: "s-1", Title: "Story s-1"}, Status: application.StatusClosed, ClosedAt: verifyNow.Add(-90 * time.Minute)},
	}
	confirm := r.confirm()
	confirm.InboxDir = r.inbox
	// direct:bbb was written two hours before the clock, s-1 landed 90 minutes before it.
	err := confirm.Run(ctx, application.VerifyConfirmRequest{Bead: "s-1", Txid: "direct:bbb", Shows: "the lamp lit"})
	if err == nil || !strings.Contains(err.Error(), "before s-1 landed") {
		t.Fatalf("expected a refusal, got %v", err)
	}
	if len(r.beads.Written) != 0 {
		t.Fatalf("a refusal writes nothing: %+v", r.beads.Written)
	}
	if err := confirm.Run(ctx, application.VerifyConfirmRequest{Bead: "s-1", Txid: "direct:aaa", Shows: "the lamp lit"}); err != nil {
		t.Fatalf("evidence after the landing confirms: %v", err)
	}
}
