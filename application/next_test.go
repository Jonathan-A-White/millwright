package application_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
)

// The two files a close-out may commit in the vault, and the message it commits
// them under. Everything a story writes in the vault is written by one of two
// hands — mw's ledger line and the session's memory of the rig — and these are
// the paths those hands are allowed to reach.

func TestSeatWorkIsTheLedgerAndTheRigMemoryAndNothingElse(t *testing.T) {
	work := application.SeatWork("builder", "millwright")
	want := []string{"seats/builder/ledger.md", "seats/builder/rigs/millwright.md"}
	if strings.Join(work, "\n") != strings.Join(want, "\n") {
		t.Fatalf("expected %q, got %q", want, work)
	}
}

func TestSeatWorkIsTheLedgerAloneWhenNoRigWasWorked(t *testing.T) {
	work := application.SeatWork("mayor", "")
	if len(work) != 1 || work[0] != "seats/mayor/ledger.md" {
		t.Fatalf("expected the ledger alone, got %q", work)
	}
}

func TestSeatWorkIsNothingWithoutASeat(t *testing.T) {
	if work := application.SeatWork("", "millwright"); len(work) != 0 {
		t.Fatalf("expected no paths without a seat, got %q", work)
	}
}

func TestSeatWorkRefusesANameThatWouldReachOutsideTheVault(t *testing.T) {
	if work := application.SeatWork("../../etc", "millwright"); len(work) != 0 {
		t.Fatalf("expected a seat name reaching outside the vault to give no paths, got %q", work)
	}
	work := application.SeatWork("builder", "../../etc/passwd")
	if len(work) != 1 || work[0] != "seats/builder/ledger.md" {
		t.Fatalf("expected a rig name reaching outside the vault to leave the ledger alone in the list, got %q", work)
	}
}

func TestRunRecordIsTheResultOfTheStoryAndNotItsBootFile(t *testing.T) {
	if got, want := application.RunRecord("mw-gq6.48"), "runs/mw-gq6.48/result.json"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestRunRecordRefusesAStoryThatWouldReachOutsideTheVault(t *testing.T) {
	for _, id := range []string{"", "../seats/builder", "a/b"} {
		if got := application.RunRecord(id); got != "" {
			t.Fatalf("expected no path for the story %q, got %q", id, got)
		}
	}
}

func TestLandingErrorRecordIsBesideTheResultOfTheStory(t *testing.T) {
	if got, want := application.LandingErrorRecord("mw-gq6.58"), "runs/mw-gq6.58/landing-error.txt"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	for _, id := range []string{"", "../seats/builder", "a/b"} {
		if got := application.LandingErrorRecord(id); got != "" {
			t.Fatalf("expected no path for the story %q, got %q", id, got)
		}
	}
}

func TestVaultCommitMessageNamesTheStoryAndSignsNothing(t *testing.T) {
	message := application.VaultCommitMessage("mw-gq6.40", "mw next cannot sync\nafter its own landing")
	for _, want := range []string{"mw-gq6.40", "mw next cannot sync after its own landing"} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected the message to hold %q, got %q", want, message)
		}
	}
	if strings.Contains(message, "\n") {
		t.Fatalf("expected one plain line, got %q", message)
	}
	if signature := application.AIAttribution(message); signature != "" {
		t.Fatalf("expected a message signed by nobody, got %q", signature)
	}
}

func TestVaultCommitMessageNamesTheStoryWhenItHasNoTitle(t *testing.T) {
	message := application.VaultCommitMessage("mw-gq6.40", "   ")
	if !strings.Contains(message, "mw-gq6.40") {
		t.Fatalf("expected the message to name the story, got %q", message)
	}
}

// The rig's merge slot across hosts is held only while a close-out fetches,
// merges, re-runs the merged tests and pushes: never while it waits in line
// for this host's own merge slot (mw-gq6.421). On 2026-10-14 the Laptop's
// close-out of mw-5r3p30.341 took the cross-host slot of lampas and then
// waited for the Laptop's own lampas slot, which mw-5r3p30.340 held through its
// gate; the desktop's close-out of mw-86e9r1.21 waited the whole 180m cap
// behind it while nothing landed.

// aHostSlot is one host's merge slot of a rig while close-out A holds it:
// whoever takes it says so on waiting and waits until A gives it back.
type aHostSlot struct {
	waiting chan struct{}
	free    chan struct{}
}

func (s *aHostSlot) Take(ctx context.Context, _, _ string) (application.Holding, error) {
	s.waiting <- struct{}{}
	select {
	case <-s.free:
		return aHolding{}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// aCrossSlotOn is a rig's merge slot across hosts as host sees it, on the
// shared server notes, quick enough for a test. Every poll it makes, which is
// a wait for somebody else's lease, is counted on polls and cut short, so that
// a close-out that would wait says so at once rather than hang the test.
func aCrossSlotOn(notes application.SlotNotes, host string, polls *atomic.Int32) application.CrossSlot {
	const poll = 7 * time.Millisecond
	return application.CrossSlot{
		Notes: notes, Host: host, TTL: time.Minute, Poll: poll, Settle: time.Millisecond, Retry: time.Second,
		Wait: func(ctx context.Context, d time.Duration) error {
			if d == poll {
				polls.Add(1)
				return errors.New("a test does not wait for another close-out's lease")
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(d):
				return nil
			}
		},
	}
}

// aCloseOutOn is a close-out of a finished story on host, under slot and
// cross, whose branch merges as lands says.
func aCloseOutOn(host, id string, slot application.MergeSlot, cross application.CrossSlot, lands application.Landing, checks application.Checks) application.Next {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: host})
	tracker.AddStory("mw-x", domain.Story{ID: id, Title: "A story"})
	vault := newFakeVault()
	vault.written[id+"/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	return application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands.(application.Worktrees), Landing: lands,
		Checks: checks, Slot: slot, CrossSlot: cross,
		Runner: apptest.NewFakeRunner(), Mailbox: apptest.NewFakeMailbox(),
		Boot: application.SeatBoot{Vault: vault, Harness: claude.New(), Seat: "builder", Host: host},
		Seat: "builder", Host: host,
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}
}

func TestACloseOutWaitingForItsHostsMergeSlotLeavesTheSlotAcrossHostsToTheOtherHost(t *testing.T) {
	ctx := context.Background()
	server := apptest.NewFakeTracker()
	desktop := &aHostSlot{waiting: make(chan struct{}, 4), free: make(chan struct{})}
	var desktopPolls, laptopPolls atomic.Int32

	// A holds the desktop's own slot of millwright; B, on the desktop too,
	// waits in line for it.
	type ended struct {
		report application.NextReport
		err    error
	}
	b := make(chan ended, 1)
	go func() {
		report, err := aCloseOutOn("desktop", "mw-x.2", desktop, aCrossSlotOn(server, "desktop", &desktopPolls),
			&aLandingLanding{fakeRetryLanding{AheadCount: 1}}, &scriptedChecks{passes: []bool{true}}).Run(ctx, "mw-x.2")
		b <- ended{report, err}
	}()
	select {
	case <-desktop.waiting:
	case <-time.After(10 * time.Second):
		t.Fatal("expected close-out B to wait for the desktop's merge slot")
	}

	// C, on the laptop, takes the slot across hosts at once and lands.
	c, err := aCloseOutOn("laptop", "mw-x.3", aSlot{}, aCrossSlotOn(server, "laptop", &laptopPolls),
		&aLandingLanding{fakeRetryLanding{AheadCount: 1}}, &scriptedChecks{passes: []bool{true}}).Run(ctx, "mw-x.3")
	if err != nil || !c.Landed {
		t.Fatalf("expected close-out C on the laptop to land, got %v and %+v", err, c)
	}
	if n := laptopPolls.Load(); n != 0 || len(c.CrossSlot) != 0 {
		t.Fatalf("expected close-out C to take the slot across hosts at once, without waiting for A or B: it polled %d times and its report says %q", n, c.CrossSlot)
	}

	// A gives the desktop's slot back: B takes it, then the slot across hosts,
	// and lands.
	close(desktop.free)
	var got ended
	select {
	case got = <-b:
	case <-time.After(10 * time.Second):
		t.Fatal("expected close-out B to end once A gave the desktop's slot back")
	}
	if got.err != nil || !got.report.Landed || desktopPolls.Load() != 0 {
		t.Fatalf("expected close-out B to land without waiting on the slot across hosts, got %v, %d polls and %+v", got.err, desktopPolls.Load(), got.report)
	}
	if note, _ := server.Note(ctx, application.CrossSlotKey("millwright")); note != "" {
		t.Fatalf("expected nobody to hold the slot across hosts, it says %s", note)
	}
}

// aConflictingLanding is a branch that does not merge into its target.
type aConflictingLanding struct{ fakeRetryLanding }

func (*aConflictingLanding) Merge(context.Context, string, string) (application.Landed, error) {
	return application.Landed{}, fmt.Errorf("CONFLICT (content): Merge conflict in a.go: %w", application.ErrMergeConflict)
}

// aCrossSlotWatcher is the shared server, saying whether a close-out held the
// slot across hosts at any time.
type aCrossSlotWatcher struct {
	*apptest.FakeTracker
	held atomic.Bool
}

func (w *aCrossSlotWatcher) SetNote(ctx context.Context, key, value string) error {
	if _, ok := application.ReadCrossSlotLease(value); ok {
		w.held.Store(true)
	}
	return w.FakeTracker.SetNote(ctx, key, value)
}

func TestAStorySentBackGivesBackTheSlotAcrossHostsItHeld(t *testing.T) {
	for name, landing := range map[string]struct {
		lands  application.Landing
		reason application.Reason
	}{
		"merge conflict":    {&aConflictingLanding{fakeRetryLanding{AheadCount: 1}}, application.ReasonMergeConflict},
		"merged tests fail": {&mergingLanding{fakeRetryLanding{AheadCount: 1}}, application.ReasonMergedTestsFail},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			server := &aCrossSlotWatcher{FakeTracker: apptest.NewFakeTracker()}
			var polls atomic.Int32
			report, err := aCloseOutOn("desktop", "mw-x.4", aSlot{}, aCrossSlotOn(server, "desktop", &polls),
				landing.lands, &scriptedChecks{passes: []bool{true, false}}).Run(ctx, "mw-x.4")
			if report.SentBack == "" || report.Reason != landing.reason {
				t.Fatalf("expected the story sent back for %s, got %v and %+v", landing.reason, err, report)
			}
			if !server.held.Load() {
				t.Fatal("expected the close-out to have held the slot across hosts while it merged")
			}
			if note, _ := server.Note(ctx, application.CrossSlotKey("millwright")); note != "" {
				t.Fatalf("expected the slot across hosts given back, it says %s", note)
			}
		})
	}
}
