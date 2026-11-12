package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

func notMetRig() (*apptest.FakeVerifyBeads, *bytes.Buffer, application.VerifyNotMet) {
	beads := &apptest.FakeVerifyBeads{Details: map[string]application.StoryDetail{
		"s-1":   {Story: domain.Story{ID: "s-1", Title: "Open in the app"}, Status: application.StatusClosed, Labels: []string{"run:landed"}},
		"s-2":   {Story: domain.Story{ID: "s-2", Title: "Still open"}, Status: application.StatusOpen},
		"s-3":   {Story: domain.Story{ID: "s-3", Title: "Dropped"}, Status: application.StatusClosed},
		"fix-1": {Story: domain.Story{ID: "fix-1", Title: "Make it open"}, Status: application.StatusOpen},
	}}
	var out bytes.Buffer
	return beads, &out, application.VerifyNotMet{Beads: beads, Out: &out, Now: func() time.Time { return time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC) }}
}

func TestVerifyNotMetWritesOneCommentOpeningNOTMETNamingTxidTimeAndFix(t *testing.T) {
	beads, _, notmet := notMetRig()
	if err := notmet.Run(context.Background(), application.VerifyNotMetRequest{Bead: "s-1", Txid: "direct:abc", Fix: "fix-1"}); err != nil {
		t.Fatal(err)
	}
	if len(beads.Written) != 1 || beads.Written[0].ID != "s-1" {
		t.Fatalf("expected one comment on s-1, got %+v", beads.Written)
	}
	text := beads.Written[0].Text
	for _, want := range []string{"NOT MET", "direct:abc", "2026-11-11 12:00Z", "fix-1"} {
		if !strings.Contains(text, want) {
			t.Errorf("the comment %q lacks %q", text, want)
		}
	}
	if !strings.HasPrefix(text, "NOT MET") {
		t.Errorf("the comment must open NOT MET: %q", text)
	}
}

func TestVerifyNotMetWithNoFixSaysSo(t *testing.T) {
	beads, _, notmet := notMetRig()
	if err := notmet.Run(context.Background(), application.VerifyNotMetRequest{Bead: "s-1", Txid: "direct:abc"}); err != nil {
		t.Fatal(err)
	}
	if got := beads.Written[0].Text; !strings.HasSuffix(got, "no fix named") {
		t.Errorf("got %q", got)
	}
}

func TestVerifyNotMetRefusals(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name     string
		req      application.VerifyNotMetRequest
		comments []string
		want     string
	}{
		{"an open bead", application.VerifyNotMetRequest{Bead: "s-2", Txid: "t"}, nil, "not a closed landing"},
		{"a closed bead that never landed", application.VerifyNotMetRequest{Bead: "s-3", Txid: "t"}, nil, "not a closed landing"},
		{"no such bead", application.VerifyNotMetRequest{Bead: "nope", Txid: "t"}, nil, "no bead nope"},
		{"newest verdict VERIFIED", application.VerifyNotMetRequest{Bead: "s-1", Txid: "t"}, []string{"VERIFIED by the Mayor"}, "VERIFIED"},
		{"a fix that is no bead", application.VerifyNotMetRequest{Bead: "s-1", Txid: "t", Fix: "ghost"}, nil, "not a bead"},
		{"no txid", application.VerifyNotMetRequest{Bead: "s-1"}, nil, "his report"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			beads, _, notmet := notMetRig()
			for _, text := range c.comments {
				_ = beads.CommentOnStory(ctx, c.req.Bead, text)
			}
			beads.Written = nil
			err := notmet.Run(ctx, c.req)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("expected a refusal containing %q, got %v", c.want, err)
			}
			if len(beads.Written) != 0 {
				t.Fatalf("a refusal writes nothing, got %+v", beads.Written)
			}
		})
	}
}

func TestVerifyNotMetAgainOnALandingAlreadyMarkedNotMetIsAllowed(t *testing.T) {
	ctx := context.Background()
	beads, _, notmet := notMetRig()
	_ = beads.CommentOnStory(ctx, "s-1", application.NotMetComment("t0", "x", ""))
	beads.Written = nil
	// A second report on a landing already marked not met is allowed.
	if err := notmet.Run(ctx, application.VerifyNotMetRequest{Bead: "s-1", Txid: "t1", Fix: "fix-1"}); err != nil {
		t.Fatal(err)
	}
}

// The view reads the NOT MET comment where it reads VERIFIED.
func notMetLanding(t *testing.T, fixStatus string, comments ...string) (application.PosternViewDoc, *apptest.FakeTracker) {
	t.Helper()
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	liveEpic(tracker, "mw-a", domain.Path{})
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.1", Title: "Open in the app"})
	closedAt(t, tracker, "mw-a.1", viewNow.Add(-5*24*time.Hour))
	mustDo(t, tracker.CommentOnStory(ctx, "mw-a.1", "HOW TO CHECK IT, for the Governor: 1. Open the app."))
	if fixStatus != "" {
		tracker.AddStory("mw-a", domain.Story{ID: "mw-a.2", Title: "Make Open in the app work"})
		if fixStatus == apptest.StatusClosed {
			closedAt(t, tracker, "mw-a.2", viewNow.Add(-time.Hour))
		}
	}
	for _, text := range comments {
		mustDo(t, tracker.CommentOnStory(ctx, "mw-a.1", text))
	}
	return viewDoc(t, tracker), tracker
}

func TestPosternViewNotMetLandingWithItsFixOpenWaitsOnTheFactory(t *testing.T) {
	doc, _ := notMetLanding(t, apptest.StatusOpen, application.NotMetComment("direct:abc", "2026-11-04 21:17Z", "mw-a.2"))
	n := viewNeed(t, doc, "verify", "mw-a.1")
	if n.WaitsFor != "factory" || !n.NotReady || len(n.WaitingOn) != 1 || n.WaitingOn[0] != "Make Open in the app work" || len(n.Options) != 0 || n.Folded {
		t.Fatalf("expected a factory wait on the fix's title with no options, got %+v", n)
	}
	count := 0
	for _, n := range doc.Needs {
		if n.Bead == "mw-a.1" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected one need on the landing, got %d", count)
	}
}

func TestPosternViewNotMetLandingWithItsFixClosedAsLandedMakesNoNeed(t *testing.T) {
	doc, _ := notMetLanding(t, apptest.StatusClosed, application.NotMetComment("direct:abc", "2026-11-04 21:17Z", "mw-a.2"))
	viewLacksNeed(t, doc, "verify", "mw-a.1")
}

func TestPosternViewNotMetLandingWithNoFixWaitsOnTheMayor(t *testing.T) {
	doc, _ := notMetLanding(t, "", application.NotMetComment("direct:abc", "2026-11-04 21:17Z", ""))
	n := viewNeed(t, doc, "verify", "mw-a.1")
	if n.WaitsFor != "mayor" || !n.NotReady || len(n.Options) != 0 {
		t.Fatalf("expected a wait on the Mayor, got %+v", n)
	}
}

func TestPosternViewAVerifiedCommentAfterNotMetRemovesTheNeed(t *testing.T) {
	doc, _ := notMetLanding(t, apptest.StatusOpen, application.NotMetComment("direct:abc", "2026-11-04 21:17Z", "mw-a.2"), "VERIFIED by the Governor via postern")
	viewLacksNeed(t, doc, "verify", "mw-a.1")
}

func TestPosternViewNotMetNamingAFixTheTrackerLacksWaitsOnTheMayor(t *testing.T) {
	doc, _ := notMetLanding(t, "", application.NotMetComment("direct:abc", "2026-11-04 21:17Z", "mw-gone"))
	n := viewNeed(t, doc, "verify", "mw-a.1")
	if n.WaitsFor != "mayor" || !n.NotReady {
		t.Fatalf("got %+v", n)
	}
}
