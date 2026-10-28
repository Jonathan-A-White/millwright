package application_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.509: a dispatch records the acceptance the session is handed.
func TestDispatchRecordsTheAcceptanceItClaimedWith(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	if err := tracker.SetAcceptance("mw-gq6.1", "1. It works.\n2. go test ./... passes."); err != nil {
		t.Fatal(err)
	}

	if _, err := dispatch.Run(context.Background()); err != nil {
		t.Fatalf("dispatching: %v", err)
	}

	want := application.AcceptanceAtClaim("1. It works.\n2. go test ./... passes.")
	if got := tracker.Metadata("mw-gq6.1")[application.AcceptanceAtClaimField]; got != want {
		t.Fatalf("expected %s=%q, got %q", application.AcceptanceAtClaimField, want, got)
	}
}

func TestAcceptanceChangedSince(t *testing.T) {
	claimed := application.AcceptanceAtClaim("1. One.\n2. Two.")
	cases := []struct {
		name     string
		recorded string
		now      string
		changed  bool
		added    []string
		dropped  int
	}{
		{"nothing recorded", "", "1. One.\n2. Two.\n3. Three.", false, nil, 0},
		{"unchanged", claimed, "1. One.\n2. Two.", false, nil, 0},
		{"re-spaced", claimed, "1.  One.\n\n2. Two.  \n", false, nil, 0},
		{"a line added", claimed, "1. One.\n2. Two.\n3. Three.", true, []string{"3. Three."}, 0},
		{"a line reworded", claimed, "1. One.\n2. Twoo.", true, []string{"2. Twoo."}, 1},
		{"a line taken out", claimed, "1. One.", true, nil, 1},
		{"acceptance added to an empty one", application.AcceptanceAtClaim(""), "1. One.", true, []string{"1. One."}, 0},
		{"acceptance still empty", application.AcceptanceAtClaim(""), "  \n", false, nil, 0},
	}
	for _, c := range cases {
		change, changed := application.AcceptanceChangedSince(c.recorded, c.now)
		if changed != c.changed || !reflect.DeepEqual(change.Added, c.added) || change.Dropped != c.dropped {
			t.Errorf("%s: got changed=%v added=%q dropped=%d, want changed=%v added=%q dropped=%d",
				c.name, changed, change.Added, change.Dropped, c.changed, c.added, c.dropped)
		}
	}
}
