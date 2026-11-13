package application_test

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// A box whose restore ran past the make's deadline, then finished, is no made
// box if the hub has no peer for it: it would sit "waiting for its first
// dispatch" and bill idle (cloud7, 2026-11-13; mw-gq6.1198, mw-gq6.1201). The
// check destroys it, says so in one line naming the box, and writes no "made"
// move.
func TestAdoptedRestoringBoxWithNoHubAddressIsDownedNotMade(t *testing.T) {
	now := time.Date(2026, 11, 13, 6, 0, 0, 0, time.UTC)
	provider := &apptest.FakeCloud{DeadlineUps: 1, Unreachable: map[string]bool{"cloud1": true}}
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11",
		Boxes: []application.CloudBox{{Name: "cloud1", Up: now, Making: true}}}}
	if err := (application.CloudMake{Provider: provider, Book: book, Host: "laptop", Plan: cloudPlan,
		Now: func() time.Time { return now }}).Run(context.Background(), "cloud1"); err != nil {
		t.Fatal(err)
	}
	if !book.State.Boxes[0].Restoring {
		t.Fatalf("expected cloud1 kept as restoring, got %+v", book.State.Boxes)
	}
	provider.FinishRestore("cloud1")
	var said bytes.Buffer
	report, err := application.CloudCheck{
		Work: apptest.NewFakeTracker(), Provider: provider, Book: book, Host: "desktop",
		Plan: cloudPlan, Now: func() time.Time { return now.Add(time.Minute) }, Out: &said,
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, move := range report.Moves {
		if strings.Contains(move, "made cloud1") {
			t.Errorf("expected no made move for a box with no hub address, got %q", move)
		}
	}
	if !slices.Contains(provider.Calls(), "down cloud1") && !slices.Contains(provider.Calls(), "reap cloud1") {
		t.Errorf("expected cloud1 destroyed, calls %v", provider.Calls())
	}
	for _, box := range book.State.Boxes {
		if box.Name == "cloud1" {
			t.Errorf("expected cloud1 off the book, got %+v", box)
		}
	}
	all := strings.Join(report.Moves, "\n") + "\n" + said.String()
	if !strings.Contains(all, "cloud1 is not a peer of the hub") {
		t.Errorf("expected one line naming cloud1 and that it is no peer of the hub, got\n%s", all)
	}
}
