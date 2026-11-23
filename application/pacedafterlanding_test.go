package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// A list driven by mw release-paced is run again by mw itself once a story on
// it has landed (mw-gq6.1682): nobody has to remember the command.

type pacedFileLists []application.PacedList

func (f pacedFileLists) PacedLists(context.Context) ([]application.PacedList, error) { return f, nil }

func pacedAfter(tracker *apptest.FakeTracker, lists ...application.PacedList) application.PacedAfterLanding {
	return application.PacedAfterLanding{
		Lists: pacedFileLists(lists),
		Release: application.ReleasePaced{
			Tracker:  tracker,
			Readings: application.UsageReadings{Notes: tracker},
			Now:      func() time.Time { return pacedNow },
		},
	}
}

func TestAStoryOfAPacedListLandingReleasesTheNextHeldStoryOfTheList(t *testing.T) {
	held, closed := application.StatusHeld, "closed"
	tracker, ids := pacedList(t, 30, closed, held, held)
	notes := pacedAfter(tracker, application.PacedList{Name: "plans/p/p-order.txt", IDs: ids}).ReleaseAfter(context.Background(), []string{"p-1"})

	if got := statusOf(t, tracker, "p-2"); got == application.StatusHeld {
		t.Errorf("expected p-2 to be released by the landing of p-1, it is %s", got)
	}
	if got := statusOf(t, tracker, "p-3"); got == application.StatusHeld {
		t.Errorf("expected p-3 to be released too, with room for 3, it is %s", got)
	}
	if text := strings.Join(notes, "\n"); !strings.Contains(text, "plans/p/p-order.txt") || !strings.Contains(text, "released 2: p-2, p-3") {
		t.Errorf("expected a note naming the list and what it released, got %q", text)
	}
}

func TestALandingOfAStoryOnNoPacedListReleasesNothingAndSaysNothing(t *testing.T) {
	held, closed := application.StatusHeld, "closed"
	tracker, ids := pacedList(t, 30, closed, held)
	notes := pacedAfter(tracker, application.PacedList{Name: "plans/p/p-order.txt", IDs: ids}).ReleaseAfter(context.Background(), []string{"other-1"})
	if len(notes) != 0 {
		t.Errorf("expected no note for a story on no list, got %v", notes)
	}
	if got := statusOf(t, tracker, "p-2"); got != application.StatusHeld {
		t.Errorf("expected p-2 to stay held, it is %s", got)
	}
}

func TestALandingReleasesNothingWhenThreeOfTheListAreAlreadyReleasedAndSaysSo(t *testing.T) {
	held, closed, open := application.StatusHeld, "closed", application.StatusOpen
	tracker, ids := pacedList(t, 30, closed, open, open, open, held)
	notes := pacedAfter(tracker, application.PacedList{Name: "plans/p/p-order.txt", IDs: ids}).ReleaseAfter(context.Background(), []string{"p-1"})
	if got := statusOf(t, tracker, "p-5"); got != application.StatusHeld {
		t.Errorf("expected p-5 to stay held with 3 running or open, it is %s", got)
	}
	if text := strings.Join(notes, "\n"); !strings.Contains(text, "3 of the list running or open, at most 3 at once: nothing released") {
		t.Errorf("expected the note to say why none was released, got %q", text)
	}
}

func TestALandingReleasesNothingWhenTheUsageIsOverTheLineAndSaysSo(t *testing.T) {
	held, closed := application.StatusHeld, "closed"
	tracker, ids := pacedList(t, 75, closed, held)
	notes := pacedAfter(tracker, application.PacedList{Name: "plans/p/p-order.txt", IDs: ids}).ReleaseAfter(context.Background(), []string{"p-1"})
	if got := statusOf(t, tracker, "p-2"); got != application.StatusHeld {
		t.Errorf("expected p-2 to stay held at 75%% of the week, it is %s", got)
	}
	if text := strings.Join(notes, "\n"); !strings.Contains(text, "is over 60%: nothing released") {
		t.Errorf("expected the note to name the usage line, got %q", text)
	}
}

func TestALandingReleasesNothingWithoutAFreshUsageReadingAndSaysSo(t *testing.T) {
	held, closed := application.StatusHeld, "closed"
	tracker, ids := pacedList(t, -1, closed, held)
	notes := pacedAfter(tracker, application.PacedList{Name: "plans/p/p-order.txt", IDs: ids}).ReleaseAfter(context.Background(), []string{"p-1"})
	if got := statusOf(t, tracker, "p-2"); got != application.StatusHeld {
		t.Errorf("expected p-2 to stay held with no usage reading, it is %s", got)
	}
	if text := strings.Join(notes, "\n"); !strings.Contains(text, "no usage was reported") {
		t.Errorf("expected the note to say no usage was reported, got %q", text)
	}
}

// With no port given, a landing runs no list: a Next built without one is as before.
func TestANilPacedAfterLandingReleasesNothing(t *testing.T) {
	var none application.PacedAfterLanding
	if notes := none.ReleaseAfter(context.Background(), []string{"p-1"}); len(notes) != 0 {
		t.Errorf("expected no notes from no lists, got %v", notes)
	}
}

// pacedRecorder is a PacedReleaser that keeps what it was asked.
type pacedRecorder struct{ asked [][]string }

func (r *pacedRecorder) ReleaseAfter(_ context.Context, landed []string) []string {
	r.asked = append(r.asked, landed)
	return []string{"release-paced plans/p/p-order.txt: released 1: p-2"}
}

func TestASingleLandingRunsThePacedListsOfTheStoryItClosedAndSaysWhatTheyDid(t *testing.T) {
	rec := &pacedRecorder{}
	lands := &aLandingLanding{fakeRetryLanding{AheadCount: 1}}
	n := aCloseOutOn("desktop", "mw-x.1", aSlot{}, application.CrossSlot{}, lands, &scriptedChecks{passes: []bool{true}})
	n.Paced = rec
	report, err := n.Run(context.Background(), "mw-x.1")
	if err != nil || !report.Landed || !report.Closed {
		t.Fatalf("expected mw-x.1 to land and close, got %v and %+v", err, report)
	}
	if len(rec.asked) != 1 || strings.Join(rec.asked[0], ",") != "mw-x.1" {
		t.Errorf("expected the paced lists to be run once for mw-x.1 after it closed, got %v", rec.asked)
	}
	if !strings.Contains(strings.Join(report.Notes, "\n"), "released 1: p-2") {
		t.Errorf("expected the report to say what the list did, got %v", report.Notes)
	}
}

func TestABatchLandingRunsThePacedListsOfEveryStoryItClosedOnce(t *testing.T) {
	rec := &pacedRecorder{}
	aBatchLandingLogged(t, nil, nil, func(n *application.Next, _ *portLog) { n.Paced = rec })
	if len(rec.asked) != 1 || strings.Join(rec.asked[0], ",") != "mw-x.1,mw-x.2" {
		t.Errorf("expected the paced lists to be run once for the batch's two stories, got %v", rec.asked)
	}
}
