package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// requestMoveRig is a GristRequest over an in-memory store holding records, a
// tracker that shows mw-a to mw-d, and a mill that forwarded the request.
type requestMoveRig struct {
	tracker  *apptest.FakeTracker
	postern  *apptest.FakePostern
	requests *memRequests
	mover    application.GristRequest
}

const movedOnRequest = "direct:aa"

func newRequestMoveRig(records ...application.GristRequestRecord) *requestMoveRig {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-app", domain.Path{Rig: "lampas", Branch: "main"})
	for _, id := range []string{"mw-a", "mw-b", "mw-c", "mw-d"} {
		tracker.AddStory("mw-app", domain.Story{ID: id, Title: "title of " + id})
	}
	runs := &apptest.FakeGristRuns{Senders: map[string]string{movedOnRequest: "02ab"}}
	runs.Kept = append(runs.Kept, application.GristRunTiming{Txid: movedOnRequest, App: "lampas", Kind: application.GristForwardKind})
	r := &requestMoveRig{tracker: tracker, postern: apptest.NewFakePostern(), requests: newMemRequests(records...)}
	r.mover = application.GristRequest{
		Postern: r.postern, Cipher: apptest.NewFakeCipher(), Keys: &fakeMillKey{}, Runs: runs,
		Tracker: tracker, Requests: r.requests, Now: func() time.Time { return time.UnixMilli(1000) },
	}
	return r
}

func (r *requestMoveRig) commentsOn(t *testing.T, id string) string {
	t.Helper()
	comments, err := r.tracker.StoryComments(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	var all []string
	for _, c := range comments {
		all = append(all, c.Text)
	}
	return strings.Join(all, "\n")
}

// Two beads were linked to one request by hand, neither marked moved: the next
// mw grist request moves it from the newest, the one mw grist status follows.
func TestGristRequestMovesFromTheNewestOfTwoUnmarkedRecords(t *testing.T) {
	for name, records := range map[string][]application.GristRequestRecord{
		"newest sorts last":  {filedRecord("mw-a", movedOnRequest, 1), filedRecord("mw-b", movedOnRequest, 2)},
		"newest sorts first": {filedRecord("mw-a", movedOnRequest, 2), filedRecord("mw-b", movedOnRequest, 1)},
	} {
		t.Run(name, func(t *testing.T) {
			rig := newRequestMoveRig(records...)
			newest, oldest := "mw-a", "mw-b"
			if records[1].Last.Ts > records[0].Last.Ts {
				newest, oldest = "mw-b", "mw-a"
			}

			if err := rig.mover.Run(context.Background(), "mw-c", movedOnRequest); err != nil {
				t.Fatal(err)
			}

			if got := rig.requests.records[newest].MovedTo; got != "mw-c" {
				t.Errorf("expected %s, the newest, marked moved to mw-c, it says %q", newest, got)
			}
			if !strings.Contains(rig.commentsOn(t, newest), "request moved to mw-c") {
				t.Errorf("expected %s commented that its request moved", newest)
			}
			if strings.Contains(rig.commentsOn(t, oldest), "request moved to") {
				t.Errorf("expected %s, the older, left alone, it has %q", oldest, rig.commentsOn(t, oldest))
			}
			if got := len(rig.postern.Delivered()); got != 1 {
				t.Errorf("expected the sender told once, got %d", got)
			}
		})
	}
}

// A bead whose link was moved by hand to a newer bead (the old record never
// marked) is not linked, and its request is not moved again.
func TestARequestMovedOnByHandIsNotMovedAgain(t *testing.T) {
	rig := newRequestMoveRig(filedRecord("mw-a", movedOnRequest, 1), filedRecord("mw-b", movedOnRequest, 2))

	linked, err := rig.mover.Linked(context.Background(), "mw-a")
	if err != nil {
		t.Fatal(err)
	}
	if linked {
		t.Error("expected mw-a, whose request is followed on mw-b, not linked")
	}
	if err := rig.mover.MoveRequest(context.Background(), "mw-a", "mw-c"); err != nil {
		t.Fatal(err)
	}
	if got := len(rig.postern.Delivered()); got != 0 {
		t.Errorf("expected the sender told nothing, got %d statuses", got)
	}
	if rig.requests.records["mw-a"].MovedTo != "" || rig.requests.records["mw-c"].Bead != "" {
		t.Errorf("expected the records left as they were, got %+v", rig.requests.records)
	}

	linked, err = rig.mover.Linked(context.Background(), "mw-b")
	if err != nil || !linked {
		t.Errorf("expected mw-b, the newest, linked, got %v, %v", linked, err)
	}
}

// mw fold supersede does not move the request of a folded bug whose link
// already moved on, and its sender is not told moved.
func TestFoldSupersedeDoesNotMoveARequestThatMovedOnByHand(t *testing.T) {
	// the folded bug mw-b.1 is the old bead: its record was never marked
	rig := newRequestMoveRig(filedRecord("mw-b.1", movedOnRequest, 1), filedRecord("mw-b", movedOnRequest, 2))
	tracker := supersedeTracker(t)
	rig.mover.Tracker = tracker

	report, err := application.FoldSupersede{Tracker: tracker, Requests: rig.mover, Fold: "mw-f", Out: &strings.Builder{}}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	for _, bug := range report.Bugs {
		if bug.ID == "mw-b.1" && (bug.RequestMoved || bug.RequestLeft != "") {
			t.Errorf("expected mw-b.1's request neither moved nor left, got %+v", bug)
		}
	}
	if got := len(rig.postern.Delivered()); got != 0 {
		t.Errorf("expected the sender told nothing, got %d statuses", got)
	}
}
