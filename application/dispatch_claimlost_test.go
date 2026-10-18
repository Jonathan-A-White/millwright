package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// racedTracker is a tracker whose first claim is lost to another host: just
// before the claim reaches the database, the rival's lands (mw-gq6.569).
type racedTracker struct {
	application.WorkTracker
	rival  func(id string)
	raced  map[string]bool
	toLose string
}

func (r *racedTracker) ClaimStory(ctx context.Context, id string) error {
	if id == r.toLose && !r.raced[id] {
		r.raced[id] = true
		r.rival(id)
	}
	return r.WorkTracker.ClaimStory(ctx, id)
}

// mw-gq6.569: another host claimed a story between this host reading the ready
// list and claiming, and the whole tick ended in an error (the unit failed and
// the Mayor was mailed) with every story after it unstarted. A claim lost to
// another holder passes that story and goes on.
func TestDispatchPassesAStoryAnotherHostClaimedFirstAndStartsTheNext(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	dispatch.Cap = 1 // the lost claim takes none of the one slot
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.2", Title: "Another story"})
	dispatch.Tracker = &racedTracker{
		WorkTracker: tracker,
		toLose:      "mw-gq6.1",
		raced:       map[string]bool{},
		rival: func(id string) {
			if err := tracker.ClaimAs(id, "mw@cloud4", time.Now().Add(time.Hour)); err != nil {
				t.Fatalf("the rival claiming %s: %v", id, err)
			}
		},
	}

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected a lost claim race not to fail the tick, got %v", err)
	}
	if len(report.Failed) != 0 {
		t.Fatalf("expected no failure, got %+v", report.Failed)
	}
	var passed bool
	for _, p := range report.Passed {
		if p.StoryID == "mw-gq6.1" && p.Why == "claimed by mw@cloud4 first" && p.Gate == application.ClaimLostGate {
			passed = true
		}
	}
	if !passed {
		t.Fatalf("expected mw-gq6.1 passed as claimed by mw@cloud4 first, got %+v", report.Passed)
	}
	if !strings.Contains(report.String(), "passed  mw-gq6.1 · claimed by mw@cloud4 first") {
		t.Errorf("expected the printed line, got:\n%s", report.String())
	}
	if len(report.Started) != 1 || report.Started[0].StoryID != "mw-gq6.2" {
		t.Fatalf("expected the next ready story started, got %+v", report.Started)
	}
	if names := runner.Names(); len(names) != 1 || names[0] != application.SessionName("mw-gq6.2") {
		t.Errorf("expected only the next story's session, got %v", names)
	}
}

// mw-gq6.569 sweep: a story whose needs cannot be read (a bead it names is not
// there) is one story's trouble. It is passed over with the reason and the
// stories after it are still started.
func TestDispatchPassesAStoryWhoseNeedsCannotBeReadAndStartsTheNext(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.2", Title: "Another story"})
	tracker.Needs("mw-gq6.1", "mw-gq6.404")

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected one story's unreadable need not to fail the tick, got %v", err)
	}
	var passed bool
	for _, p := range report.Passed {
		if p.StoryID == "mw-gq6.1" && p.Gate == "blocked" && strings.Contains(p.Why, "mw-gq6.404") {
			passed = true
		}
	}
	if !passed {
		t.Fatalf("expected mw-gq6.1 passed with the unreadable need named, got %+v", report.Passed)
	}
	if len(report.Started) != 1 || report.Started[0].StoryID != "mw-gq6.2" {
		t.Fatalf("expected the next story started, got %+v", report.Started)
	}
}

// mw-gq6.569 sweep: formulas that cannot be read pass each story that names
// one, with the reason, rather than ending the tick.
func TestDispatchPassesAStoryWhenTheInstalledFormulasCannotBeRead(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	dispatch.Tracker = &formulasDown{WorkTracker: tracker}

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the formulas being unreadable not to fail the tick, got %v", err)
	}
	if len(report.Passed) != 1 || report.Passed[0].Gate != "formula-not-installed" || !strings.Contains(report.Passed[0].Why, "could not be read") {
		t.Fatalf("expected the story passed with the reason, got %+v", report.Passed)
	}
}

type formulasDown struct{ application.WorkTracker }

func (formulasDown) Formulas(context.Context) ([]string, error) {
	return nil, errors.New("the database is down")
}
