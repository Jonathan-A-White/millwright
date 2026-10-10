package application_test

import (
	"context"
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
