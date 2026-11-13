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

// A make whose box Vultr runs but the hub has no peer for is no made box: it
// would stay up and billed, never dispatching (mw-gq6.1173).
func TestAMakeWhoseBoxHasNoHubAddressIsFailedNotMade(t *testing.T) {
	now := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	provider := &apptest.FakeCloud{Unreachable: map[string]bool{"cloud1": true}}
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11",
		Boxes: []application.CloudBox{{Name: "cloud1", Up: now, Making: true}}}}
	var said bytes.Buffer
	err := application.CloudMake{Provider: provider, Book: book, Host: "laptop", Plan: cloudPlan,
		Now: func() time.Time { return now }, Out: &said}.Run(context.Background(), "cloud1")
	if err != nil {
		t.Fatalf("expected the make to end, got %v\n%s", err, said.String())
	}
	calls := provider.Calls()
	if !slices.Contains(calls, "down cloud1") {
		t.Errorf("expected the box with no hub address destroyed, calls %v", calls)
	}
	if n := strings.Count(strings.Join(calls, "|"), "up cloud1"); n != 2 {
		t.Errorf("expected the make tried twice, calls %v", calls)
	}
	for _, box := range book.State.Boxes {
		if box.Name == "cloud1" && (!box.Made.IsZero() || box.Making) {
			t.Errorf("expected no cloud1 written up as made or still making, got %+v", box)
		}
	}
	if book.State.HeldUntil.IsZero() {
		t.Errorf("expected no box made for a while after two failed tries, book %+v", book.State)
	}
	whys := strings.Join(book.Whys, "\n")
	if !strings.Contains(whys, "failed cloud1") || !strings.Contains(whys, "not a peer of the hub") {
		t.Errorf("expected the writes to say cloud1 failed, not a peer of the hub, got %q", whys)
	}
}

// A box with an address is made as before.
func TestAMakeWhoseBoxHasAHubAddressIsStillMade(t *testing.T) {
	now := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	provider := &apptest.FakeCloud{}
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11",
		Boxes: []application.CloudBox{{Name: "cloud1", Up: now, Making: true}}}}
	if err := (application.CloudMake{Provider: provider, Book: book, Host: "laptop", Plan: cloudPlan,
		Now: func() time.Time { return now }}).Run(context.Background(), "cloud1"); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(provider.Calls(), "down cloud1") || book.State.Boxes[0].Made.IsZero() {
		t.Errorf("expected cloud1 made, calls %v, book %+v", provider.Calls(), book.State.Boxes)
	}
}
