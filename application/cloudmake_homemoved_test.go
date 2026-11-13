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

// A make runs for minutes, and the home may move meanwhile: the make that
// finds, when it takes the cloud lock, that this host is no longer home writes
// no book and says so (mw-gq6.1181).
func TestCloudMakeAfterTheHomeMovedDuringTheUp(t *testing.T) {
	now := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	provider := &apptest.FakeCloud{}
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11",
		Boxes: []application.CloudBox{{Name: "cloud1", Up: now, Making: true}}}}
	asked := 0
	var said bytes.Buffer
	mk := application.CloudMake{Provider: provider, Book: book, Host: "laptop", Plan: cloudPlan,
		Now: func() time.Time { return now }, Out: &said,
		Home: func(context.Context) (bool, error) {
			asked++
			// The home moves while the provider works: the first lock is taken
			// at home, the second after the move.
			return !slices.Contains(provider.Calls(), "up cloud1"), nil
		},
	}
	if err := mk.Run(context.Background(), "cloud1"); err != nil {
		t.Fatalf("expected the make to end quietly, got %v\n%s", err, said.String())
	}
	if asked < 2 {
		t.Errorf("expected the home asked at each lock, asked %d times", asked)
	}
	if len(book.Whys) != 0 {
		t.Errorf("expected no write of the book after the home moved, got %q", book.Whys)
	}
	if book.State.Boxes[0].Made != (time.Time{}) || !book.State.Boxes[0].Making {
		t.Errorf("expected the book left as the new home has it, got %+v", book.State.Boxes)
	}
	if !strings.Contains(said.String(), "no longer home") || !strings.Contains(said.String(), "cloud1") {
		t.Errorf("expected a note that the host is no longer home and which box, got %q", said.String())
	}
}

// A make begun on a host that is already not home brings up nothing.
func TestCloudMakeAfterTheHomeMovedBeforeTheUp(t *testing.T) {
	now := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	provider := &apptest.FakeCloud{}
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11",
		Boxes: []application.CloudBox{{Name: "cloud1", Up: now, Making: true}}}}
	var said bytes.Buffer
	err := application.CloudMake{Provider: provider, Book: book, Host: "laptop", Plan: cloudPlan,
		Now: func() time.Time { return now }, Out: &said,
		Home: func(context.Context) (bool, error) { return false, nil },
	}.Run(context.Background(), "cloud1")
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(provider.Calls(), func(c string) bool { return strings.HasPrefix(c, "up ") }) {
		t.Errorf("expected no box brought up from a host that is not home, calls %v", provider.Calls())
	}
	if len(book.Whys) != 0 {
		t.Errorf("expected no write of the book, got %q", book.Whys)
	}
	if !strings.Contains(said.String(), "no longer home") {
		t.Errorf("expected a note that the host is no longer home, got %q", said.String())
	}
}
