package application_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
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

// Nor is the slot across hosts held while a close-out's gate on a merged
// result waits for this host's gate lock, or for the host to calm
// (mw-gq6.422): the other host's close-out of the rig would wait for this
// host's whole queue of gates, any rig's, up to the gate lock's 3h cap.

// anotherRigsGate is this host's gate lock, which another rig's gate takes
// once the first free takes are over. A close-out asking for it without
// waiting (cancelling its context when told it is held) is refused at once;
// one that waits runs whileWaiting, and then has it.
type anotherRigsGate struct {
	free         int
	whileWaiting func()
	takes        int
}

func (g *anotherRigsGate) Take(ctx context.Context, _ string, held func(string)) (application.GateTurn, error) {
	if g.takes++; g.takes <= g.free {
		return application.GateTurn{Holding: aHolding{}}, nil
	}
	if held != nil {
		held("mw-other.1")
	}
	if err := ctx.Err(); err != nil {
		return application.GateTurn{}, err
	}
	g.whileWaiting()
	return application.GateTurn{Holding: aHolding{}, Waited: 4 * time.Minute, For: "mw-other.1"}, nil
}

func TestACloseOutWaitingForItsHostsGateLockLeavesTheSlotAcrossHostsToTheOtherHost(t *testing.T) {
	ctx := context.Background()
	server := apptest.NewFakeTracker()
	waiting, ended := make(chan struct{}, 1), make(chan struct{})
	var once sync.Once
	otherGateEnds := func() { once.Do(func() { close(ended) }) }
	defer otherGateEnds()
	var desktopPolls, laptopPolls atomic.Int32

	// A, on the desktop, merges into a merge commit; its gate on the merged
	// result finds the desktop's gate lock taken by another rig's gate.
	type endedRun struct {
		report application.NextReport
		err    error
	}
	a := make(chan endedRun, 1)
	go func() {
		next := aCloseOutOn("desktop", "mw-x.5", aSlot{}, aCrossSlotOn(server, "desktop", &desktopPolls),
			&mergingLanding{fakeRetryLanding{AheadCount: 1}}, &scriptedChecks{passes: []bool{true}})
		next.GateLock = &anotherRigsGate{free: 1, whileWaiting: func() { waiting <- struct{}{}; <-ended }}
		report, err := next.Run(ctx, "mw-x.5")
		a <- endedRun{report, err}
	}()
	select {
	case <-waiting:
	case <-time.After(10 * time.Second):
		t.Fatal("expected close-out A to wait for the desktop's gate lock")
	}

	// C, on the laptop, takes the slot across hosts at once and lands.
	c, err := aCloseOutOn("laptop", "mw-x.6", aSlot{}, aCrossSlotOn(server, "laptop", &laptopPolls),
		&aLandingLanding{fakeRetryLanding{AheadCount: 1}}, &scriptedChecks{passes: []bool{true}}).Run(ctx, "mw-x.6")
	if err != nil || !c.Landed {
		t.Fatalf("expected close-out C on the laptop to land, got %v and %+v", err, c)
	}
	if n := laptopPolls.Load(); n != 0 || len(c.CrossSlot) != 0 {
		t.Fatalf("expected close-out C to take the slot across hosts at once, without waiting for the desktop's gate lock: it polled %d times and its report says %q", n, c.CrossSlot)
	}

	// The other gate ends: A takes the lock, takes the slot across hosts
	// again, merges afresh and lands.
	otherGateEnds()
	var got endedRun
	select {
	case got = <-a:
	case <-time.After(10 * time.Second):
		t.Fatal("expected close-out A to end once the other gate gave the lock back")
	}
	if got.err != nil || !got.report.Landed || desktopPolls.Load() != 0 {
		t.Fatalf("expected close-out A to land without waiting on the slot across hosts, got %v, %d polls and %+v", got.err, desktopPolls.Load(), got.report)
	}
	if !strings.Contains(strings.Join(got.report.CrossSlot, "\n"), "gave the cross-host merge slot of millwright back while it waited for desktop's gate lock, held by mw-other.1") {
		t.Fatalf("expected A's report to say it gave the slot back while it waited, got %q", got.report.CrossSlot)
	}
	if note, _ := server.Note(ctx, application.CrossSlotKey("millwright")); note != "" {
		t.Fatalf("expected nobody to hold the slot across hosts, it says %s", note)
	}
}

// aRemoteBothHostsLandOn merges into a merge commit and pushes to a remote the
// other host lands on too: a push made from a fetch older than the remote's
// newest landing is refused, as git refuses it.
type aRemoteBothHostsLandOn struct {
	fakeRetryLanding
	remote, fetched, pushes int
}

func (r *aRemoteBothHostsLandOn) Fetch(context.Context, string) error {
	r.fetched = r.remote
	return nil
}

func (*aRemoteBothHostsLandOn) Merge(context.Context, string, string) (application.Landed, error) {
	return application.Landed{Commit: "0123456789abcdef"}, nil
}

func (r *aRemoteBothHostsLandOn) Push(context.Context, string, string, string) error {
	r.pushes++
	if r.fetched != r.remote {
		return fmt.Errorf("! [rejected] main -> main (fetch first): %w", application.ErrPushRejected)
	}
	r.remote++
	return nil
}

// aLoadThatWaitingCalms is a host that is busy from the moment the merged
// result's first run begins until a close-out waits for it to calm.
type aLoadThatWaitingCalms struct{ busy bool }

func (l *aLoadThatWaitingCalms) Load(context.Context) (application.LoadReading, error) {
	if l.busy {
		return busy(), nil
	}
	return calm(), nil
}

// checksThatBusyTheHost are the rig's tests: the worktree's run passes, the
// merged result's first fails while it busies the host, the second passes.
type checksThatBusyTheHost struct {
	load *aLoadThatWaitingCalms
	runs int
}

func (c *checksThatBusyTheHost) Run(context.Context, string, string) (application.Checked, error) {
	c.runs++
	if c.runs == 2 {
		c.load.busy = true
		return application.Checked{Command: "make test", Output: "FAIL: timed out\n"}, nil
	}
	return application.Checked{Command: "make test", Passed: true}, nil
}

func TestACloseOutThatGivesTheSlotAcrossHostsBackToWaitStillLandsWithNoTryLost(t *testing.T) {
	ctx := context.Background()
	server := apptest.NewFakeTracker()
	remote := &aRemoteBothHostsLandOn{fakeRetryLanding: fakeRetryLanding{AheadCount: 1}}
	load := &aLoadThatWaitingCalms{}
	var polls atomic.Int32
	var underTheSlot []string
	// The other host lands whenever the slot across hosts is free, which it
	// must be whenever this close-out waits for this host.
	theOtherHostLands := func(waitingFor string) {
		if note, _ := server.Note(ctx, application.CrossSlotKey("millwright")); note != "" {
			underTheSlot = append(underTheSlot, waitingFor)
			return
		}
		remote.remote++
	}

	next := aCloseOutOn("desktop", "mw-x.7", aSlot{}, aCrossSlotOn(server, "desktop", &polls),
		remote, &checksThatBusyTheHost{load: load})
	next.Tries = 1
	next.GateLock = &anotherRigsGate{free: 1, whileWaiting: func() { theOtherHostLands("the gate lock") }}
	next.Load = load
	next.LoadWait = func(context.Context, time.Duration) error {
		theOtherHostLands("the host to calm")
		load.busy = false
		return nil
	}
	report, err := next.Run(ctx, "mw-x.7")
	if len(underTheSlot) > 0 {
		t.Fatalf("expected the slot across hosts given back while the close-out waited, it held it while it waited for %q", underTheSlot)
	}
	if err != nil || !report.Landed {
		t.Fatalf("expected the story landed in its one try, got %v and %+v", err, report)
	}
	if remote.pushes != 1 || report.Pushes != 1 || remote.remote != 3 {
		t.Fatalf("expected one push, after the other host's 2 landings, got %d pushes and %d landings in all", remote.pushes, remote.remote)
	}
	gaveBack := 0
	for _, said := range report.CrossSlot {
		if strings.HasPrefix(said, "gave the cross-host merge slot of millwright back") {
			gaveBack++
		}
	}
	if gaveBack != 2 {
		t.Fatalf("expected the slot given back twice, for the gate lock and for the host to calm, got %q", report.CrossSlot)
	}
	if !strings.Contains(report.String(), "ran twice") {
		t.Fatalf("expected the report to say the merged result's tests ran twice, got:\n%s", report.String())
	}
}
