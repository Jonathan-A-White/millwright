package application_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// meetingTracker holds each of two reads until the other has begun, so that a
// status that reads them one after the other waits out the timeout for it.
type meetingTracker struct {
	*apptest.FakeTracker
	hitlBegun, blockedBegun chan struct{}
	mu                      sync.Mutex
	alone                   []string
}

func (m *meetingTracker) meet(name string, begun, other chan struct{}) {
	close(begun)
	select {
	case <-other:
	case <-time.After(2 * time.Second):
		m.mu.Lock()
		m.alone = append(m.alone, name)
		m.mu.Unlock()
	}
}

func (m *meetingTracker) ReadyWithLabel(ctx context.Context, label string) ([]application.StoryDetail, error) {
	if label == application.LabelHitl {
		m.meet("what waits for the Governor", m.hitlBegun, m.blockedBegun)
	}
	return m.FakeTracker.ReadyWithLabel(ctx, label)
}

func (m *meetingTracker) BlockedForHost(ctx context.Context, host string) ([]application.StoryDetail, error) {
	m.meet("what is blocked", m.blockedBegun, m.hitlBegun)
	return m.FakeTracker.BlockedForHost(ctx, host)
}

// Over a link with a 70 ms round trip every read of the tracker is seconds, so
// the reads of a status that do not depend on one another are made together
// (mw-gq6.356).
func TestStatusReadsWhatItCanTogether(t *testing.T) {
	tracker := &meetingTracker{
		FakeTracker:  apptest.NewFakeTracker(),
		hitlBegun:    make(chan struct{}),
		blockedBegun: make(chan struct{}),
	}
	tracker.AddEpic("epic-1", domain.Path{Rig: "spell-forge", Branch: "main", Harness: "claude", Model: "opus", Effort: "high", Host: "vps"})

	if _, err := (application.Status{
		Tracker: tracker,
		Notes:   tracker,
		Host:    "vps",
		Seat:    "builder",
		Now:     func() time.Time { return statusNow },
	}).Run(context.Background()); err != nil {
		t.Fatalf("reading status: %v", err)
	}
	if len(tracker.alone) > 0 {
		t.Errorf("expected what waits for the Governor and what is blocked to be read together, but %v was read alone", tracker.alone)
	}
}

// mw-gq6.492: a Boost reads the tracker through a tunnel, a bd call two seconds
// and the blocked stories, the epics and their children a call apiece (431 calls,
// 131 s). A tracker that offers a snapshot is read through it, once, as the
// Postern view does.
func TestStatusReadsThroughTheTrackersSnapshotWhenItOffersOne(t *testing.T) {
	path := domain.Path{Rig: "spell-forge", Branch: "main", Harness: "claude", Model: "opus", Effort: "high", Host: "vps"}
	live, snap := apptest.NewFakeTracker(), apptest.NewFakeTracker()
	for _, tracker := range []*apptest.FakeTracker{live, snap} {
		tracker.AddEpic("epic-1", path)
		tracker.AddStory("epic-1", domain.Story{ID: "epic-1.1", Title: "Open for a while"})
	}
	// Only the snapshot is told of the story that waits: the status that
	// shows it was read from the snapshot.
	snap.AddStory("epic-1", domain.Story{ID: "epic-1.2", Title: "Waits on the first"})
	snap.Needs("epic-1.2", "epic-1.1")
	offering := &snapshotting{FakeTracker: live, snap: snap}

	report, err := application.Status{
		Tracker: offering, Notes: live, Host: "vps", Seat: "builder",
		Now: func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	if offering.taken != 1 {
		t.Errorf("expected one snapshot for the status, got %d", offering.taken)
	}
	if len(report.Blocked) != 1 || report.Blocked[0].Story.ID != "epic-1.2" {
		t.Errorf("expected the blocked story read through the snapshot, got %+v", report.Blocked)
	}
}

// A snapshot that cannot be taken costs the status its speed and nothing else:
// it reads the tracker's own way.
func TestStatusReadsTheTrackerItselfWhenTheSnapshotFails(t *testing.T) {
	path := domain.Path{Rig: "spell-forge", Branch: "main", Harness: "claude", Model: "opus", Effort: "high", Host: "vps"}
	live := apptest.NewFakeTracker()
	live.AddEpic("epic-1", path)
	live.AddStory("epic-1", domain.Story{ID: "epic-1.1", Title: "Open for a while"})
	live.AddStory("epic-1", domain.Story{ID: "epic-1.2", Title: "Waits on the first"})
	live.Needs("epic-1.2", "epic-1.1")
	offering := &snapshotting{FakeTracker: live, err: errors.New("bd is gone")}

	report, err := application.Status{
		Tracker: offering, Notes: live, Host: "vps", Seat: "builder",
		Now: func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	if len(report.Blocked) != 1 || len(report.Ready) != 1 {
		t.Errorf("expected the status read from the tracker itself, got blocked %+v ready %+v", report.Blocked, report.Ready)
	}
}
