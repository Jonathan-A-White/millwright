package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

func TestABeadsFlushRecordsItself(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	tracker, log := &apptest.FakeTracker{}, &apptest.FakeFlushLog{}

	err := application.BeadsFlush{Tracker: tracker, Log: log, Now: func() time.Time { return now }}.Run(context.Background())

	if err != nil {
		t.Fatalf("flush: %v", err)
	}
	if last, _ := log.Last(context.Background()); !last.At.Equal(now) || last.Failed != "" {
		t.Fatalf("expected a good flush at %v on record, got %+v", now, last)
	}
}

func TestABeadsFlushOfACleanWorkingSetIsAPass(t *testing.T) {
	log := &apptest.FakeFlushLog{}

	err := application.BeadsFlush{Tracker: &apptest.FakeTracker{FlushClean: true}, Log: log}.Run(context.Background())

	if last, _ := log.Last(context.Background()); err != nil || last.At.IsZero() || last.Failed != "" {
		t.Fatalf("expected a pass on record, got %+v (%v)", last, err)
	}
}

func TestABeadsFlushThatFailsRecordsAndReturnsTheFailure(t *testing.T) {
	log := &apptest.FakeFlushLog{}

	err := application.BeadsFlush{Tracker: &apptest.FakeTracker{FlushErr: errors.New("server unreachable\nsecond line")}, Log: log}.Run(context.Background())

	last, _ := log.Last(context.Background())
	if err == nil || last.Failed != "server unreachable" || last.At.IsZero() {
		t.Fatalf("expected the failure recorded and returned, got %+v (%v)", last, err)
	}
}
