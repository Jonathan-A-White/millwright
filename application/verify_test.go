package application_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

var verifyNow = time.Date(2026, time.October, 21, 18, 0, 0, 0, time.UTC)

func verifyDemo(id, epic, text string) application.StoryDetail {
	return application.StoryDetail{
		Story: domain.Story{ID: id, Title: "Demo " + id}, Status: application.StatusOpen,
		Labels: []string{application.LabelDemo, "hitl"}, Description: text, EpicID: epic,
	}
}

func verifyClosed(id, epic, reason string, ago time.Duration) application.ClosedStory {
	return application.ClosedStory{ID: id, Title: "Story " + id, Type: "task", EpicID: epic, CloseReason: reason, ClosedAt: verifyNow.Add(-ago)}
}

func verifyComment(text string) []application.Comment {
	return []application.Comment{{Author: "mw", Created: verifyNow, Text: text}}
}

const closingWithCheck = "Built it.\n\nHOW TO CHECK IT, for the Governor:\n1. Open the app.\n2. Tap Save and see the green tick.\n\nWhat's new: New: a save button\n\nFor the rig memory:\nnothing"

func TestVerifyCandidatesAreOpenDemosAndUnverifiedLandings(t *testing.T) {
	beads := &apptest.FakeVerifyBeads{
		Work: application.WorkInHand{Ready: []application.StoryDetail{
			verifyDemo("d-open", "e-1", "Open the app and look for the green tick."),
			verifyDemo("d-done", "e-1", "A demo he has already verified."),
			{Story: domain.Story{ID: "plain", Title: "no demo label"}, Status: application.StatusOpen},
		}},
		Closed: []application.ClosedStory{
			verifyClosed("s-new", "e-1", "landed on main", 2*24*time.Hour),
			verifyClosed("s-verified", "e-1", "landed on main", 2*24*time.Hour),
			verifyClosed("s-superseded", "e-1", "superseded by s-new", 2*24*time.Hour),
			verifyClosed("s-never", "e-1", "never released: dropped", 2*24*time.Hour),
			verifyClosed("s-old", "e-2", "landed on main", 20*24*time.Hour),
			verifyClosed("s-step", "e-1", "landed", time.Hour),
			{ID: "s-linked", Title: "linked", Type: "task", ClosedAt: verifyNow.Add(-time.Hour), Superseded: true},
			{ID: "s-epicchild", Title: "an epic", Type: "epic", ClosedAt: verifyNow.Add(-time.Hour)},
		},
		Comments: map[string][]application.Comment{
			"s-new":        verifyComment(closingWithCheck),
			"s-verified":   verifyComment("VERIFIED by the Governor's use, from direct:x"),
			"s-superseded": verifyComment(closingWithCheck),
			"s-never":      verifyComment(closingWithCheck),
			"s-old":        verifyComment(closingWithCheck),
			"s-linked":     verifyComment(closingWithCheck),
			"d-done":       verifyComment("VERIFIED by the Governor's use, from direct:y"),
		},
	}
	beads.Closed[5].ID = "mw-mol-step"
	beads.Comments["mw-mol-step"] = verifyComment(closingWithCheck)

	got, err := application.VerifyCandidates(context.Background(), beads, verifyNow, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range got {
		ids = append(ids, c.Bead)
	}
	if want := "d-open s-new"; strings.Join(ids, " ") != want {
		t.Fatalf("candidates = %v, want %s", ids, want)
	}
	demo := got[0]
	if demo.Kind != application.VerifyKindDemo || !strings.Contains(demo.Check, "green tick.") || !strings.Contains(demo.Check, "Tap Save and see the green tick") {
		t.Fatalf("the demo carries its description and its epic's closed story's check, got %q", demo.Check)
	}
	if strings.Contains(demo.Check, "For the rig memory") || strings.Contains(demo.Check, "What's new") {
		t.Fatalf("the check text stops at the HOW TO CHECK IT section, got %q", demo.Check)
	}
	if got[1].Kind != application.VerifyKindLanding || !strings.HasPrefix(got[1].Check, "HOW TO CHECK IT") {
		t.Fatalf("the landing's check is its section, got %+v", got[1])
	}
}

func TestVerifyCandidatesLeaveOutALandingWithNoSection(t *testing.T) {
	beads := &apptest.FakeVerifyBeads{
		Closed:   []application.ClosedStory{verifyClosed("s-internal", "", "landed", time.Hour)},
		Comments: map[string][]application.Comment{"s-internal": verifyComment("Internal: nothing for the Governor to look at")},
	}
	got, err := application.VerifyCandidates(context.Background(), beads, verifyNow, 7*24*time.Hour)
	if err != nil || len(got) != 0 {
		t.Fatalf("expected no candidate, got %+v (%v)", got, err)
	}
}

func TestVerifyEvidenceReadsAMessageAndItsPictureByTxid(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, age time.Duration) {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		at := verifyNow.Add(-age)
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	write("direct-aaa.txt", time.Hour)
	write("direct-aaa.jpg", time.Hour)
	write("direct-bbb.png", 2*time.Hour)
	write("direct-old.txt", 30*24*time.Hour)
	write("direct-aaa.message.json", time.Hour)
	write("direct-voice.webm", time.Hour)

	got, err := application.ReadVerifyEvidence(dir, verifyNow.Add(-7*24*time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Txid != "direct:bbb" || got[1].Txid != "direct:aaa" {
		t.Fatalf("expected direct:bbb then direct:aaa, got %+v", got)
	}
	if len(got[1].Files) != 2 || !strings.HasSuffix(got[1].Files[0], "direct-aaa.jpg") || !strings.HasSuffix(got[1].Files[1], "direct-aaa.txt") {
		t.Fatalf("expected the picture and the text of direct:aaa, got %v", got[1].Files)
	}

	only, err := application.ReadVerifyEvidence(dir, verifyNow, []string{"direct:old"})
	if err != nil || len(only) != 1 || only[0].Txid != "direct:old" {
		t.Fatalf("a txid given is read whatever its age, got %+v (%v)", only, err)
	}
	if _, err := application.ReadVerifyEvidence(dir, verifyNow, []string{"direct:nope"}); err == nil || !strings.Contains(err.Error(), "direct:nope") {
		t.Fatalf("a txid with no file is an error naming it, got %v", err)
	}
}
