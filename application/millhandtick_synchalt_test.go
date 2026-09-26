package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// stubSync is a application.HostSync that always answers with err, and counts
// how often it was run.
type stubSync struct {
	err  error
	runs int
}

func (s *stubSync) Run(context.Context) (application.SyncReport, error) {
	s.runs++
	return application.SyncReport{Host: "vps"}, s.err
}

// stubNotifier is an application.Notifier that records every line it was
// asked to raise.
type stubNotifier struct {
	lines []string
	err   error
}

func (n *stubNotifier) Notify(_ context.Context, line string) error {
	n.lines = append(n.lines, line)
	return n.err
}

// aTick is a tick with everything else stood in for, so a test can walk
// through what its sync does without a terminal, a tracker, mail or a runner
// of its own.
func aTick(t *testing.T) (application.MillhandTick, *stubSync, *apptest.FakeSyncHaltMarker, *stubNotifier) {
	t.Helper()
	sync := &stubSync{}
	marker := apptest.NewFakeSyncHaltMarker()
	notifier := &stubNotifier{}
	tick := application.MillhandTick{
		Millhand: application.Millhand{
			Windows: apptest.NewFakeWindows(),
		},
		Sync:      sync,
		Mail:      apptest.NewFakeMailbox(),
		SyncHalts: marker,
		Notify:    notifier,
		Sweep: application.Sweep{
			Tracker: apptest.NewFakeTracker(),
			Host:    "vps",
		},
		Host: "vps",
		Now:  func() time.Time { return level },
	}
	return tick, sync, marker, notifier
}

func TestATickThatHaltsWritesTheMarkerAndNotifiesOnce(t *testing.T) {
	tick, sync, marker, notifier := aTick(t)
	sync.err = &application.SyncHalt{Code: 2, Said: "conflict in the working set"}

	if _, err := tick.Run(context.Background()); err != nil {
		t.Fatalf("expected a halted sync not to fail the tick, got %v", err)
	}
	info, there, err := marker.Read(context.Background())
	if err != nil || !there {
		t.Fatalf("expected the marker written, there=%v, err=%v", there, err)
	}
	if !info.At.Equal(level) || info.Said != "conflict in the working set" {
		t.Fatalf("expected the marker to hold the halt, got %+v", info)
	}
	if len(notifier.lines) != 1 {
		t.Fatalf("expected exactly one notice, got %v", notifier.lines)
	}
	if want := "sync halted on vps"; !strings.Contains(notifier.lines[0], want) {
		t.Fatalf("expected the notice to say %q, got %q", want, notifier.lines[0])
	}

	// A second tick, still halted, does not notify again.
	if _, err := tick.Run(context.Background()); err != nil {
		t.Fatalf("expected the second halted tick not to fail either, got %v", err)
	}
	if sync.runs != 2 {
		t.Fatalf("expected the sync tried twice, got %d", sync.runs)
	}
	if len(notifier.lines) != 1 {
		t.Fatalf("expected the second halt not to notify again, got %v", notifier.lines)
	}
}

func TestATickThatGetsLevelClearsTheMarker(t *testing.T) {
	tick, _, marker, notifier := aTick(t)
	if err := marker.Write(context.Background(), application.SyncHaltInfo{At: level.Add(-time.Hour), Said: "old"}); err != nil {
		t.Fatalf("seeding the marker: %v", err)
	}

	if _, err := tick.Run(context.Background()); err != nil {
		t.Fatalf("running the tick: %v", err)
	}
	if _, there, _ := marker.Read(context.Background()); there {
		t.Fatal("expected a level sync to clear the marker")
	}
	if len(notifier.lines) != 0 {
		t.Fatalf("expected no notice for a level sync, got %v", notifier.lines)
	}
}

func TestATickThatHaltsInADryRunDoesNotNotify(t *testing.T) {
	tick, sync, marker, notifier := aTick(t)
	sync.err = &application.SyncHalt{Code: 4, Said: "stuck working set"}
	tick.DryRun = true

	if _, err := tick.Run(context.Background()); err != nil {
		t.Fatalf("running the tick: %v", err)
	}
	if _, there, _ := marker.Read(context.Background()); !there {
		t.Fatal("expected the marker still written in a dry run")
	}
	if len(notifier.lines) != 0 {
		t.Fatalf("expected a dry run to send no notice, got %v", notifier.lines)
	}
}
