package application_test

import (
	"context"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// testClaim is a batch claim of host, on the notes both hosts share, with the
// clock a test moves.
func testClaim(notes application.SlotNotes, host string, k *slotClock) application.BatchClaim {
	if k.renew == 0 {
		k.renew = slotTTL / 3
	}
	return application.BatchClaim{Notes: notes, Host: host, TTL: slotTTL, Settle: slotSettle, Now: k.Now, Wait: k.Wait}
}

func batchClaimOf(t *testing.T, tracker *apptest.FakeTracker, rig string) (application.CrossSlotLease, bool) {
	t.Helper()
	note, err := tracker.Note(context.Background(), application.BatchClaimKey(rig))
	if err != nil {
		t.Fatal(err)
	}
	return application.ReadCrossSlotLease(note)
}

func TestAFreeBatchClaimIsTakenNamingTheHostAndGivenBack(t *testing.T) {
	tracker, k := apptest.NewFakeTracker(), newSlotClock()
	got := testClaim(tracker, "cloud14", k).Take(context.Background(), "lampas", "mw land on cloud14, landing a batch of lampas")
	if got.Holding == nil || got.Held || len(got.Said) != 0 {
		t.Fatalf("expected the claim taken with nothing to say, got %+v", got)
	}
	lease, ok := batchClaimOf(t, tracker, "lampas")
	if !ok || lease.Host != "cloud14" || !lease.Expires.After(k.Now()) {
		t.Fatalf("expected the claim written naming cloud14, got %+v", lease)
	}
	if trouble := got.Holding.Release(context.Background()); len(trouble) != 0 {
		t.Fatalf("expected a clean release, got %q", trouble)
	}
	if _, ok := batchClaimOf(t, tracker, "lampas"); ok {
		t.Fatal("expected the claim given back")
	}
}

// The rig's one batch at a time: a second host that finds the claim held does
// not wait for it and does not land.
func TestABatchClaimTheOtherHostHoldsIsNotTakenAndNamesTheHost(t *testing.T) {
	tracker, k := apptest.NewFakeTracker(), newSlotClock()
	first := testClaim(tracker, "cloud14", k).Take(context.Background(), "lampas", "first")
	if first.Holding == nil {
		t.Fatalf("expected the first claim taken, got %+v", first)
	}
	defer first.Holding.Release(context.Background())

	second := testClaim(tracker, "desktop", k).Take(context.Background(), "lampas", "second")
	if second.Holding != nil || !second.Held || second.By.Host != "cloud14" {
		t.Fatalf("expected the claim of cloud14 left alone and named, got %+v", second)
	}
	// The claim of another rig is its own.
	if other := testClaim(tracker, "desktop", k).Take(context.Background(), "millwright", "third"); other.Holding == nil {
		t.Fatalf("expected the claim of another rig free, got %+v", other)
	} else {
		other.Holding.Release(context.Background())
	}
}

// A host that died holding the claim does not hold the rig for ever: its lease
// ran out unrenewed, and the next host takes the claim back, saying whose it was.
func TestABatchClaimWhoseHostDiedIsTakenBackAfterItsLeaseRunsOut(t *testing.T) {
	tracker, k := apptest.NewFakeTracker(), newSlotClock()
	dead := application.CrossSlotLease{Holder: "mw land on cloud14, landing a batch of lampas", Host: "cloud14",
		Token: "dead", Taken: k.Now(), Expires: k.Now().Add(application.BatchClaimTTL)}
	if err := tracker.SetNote(context.Background(), application.BatchClaimKey("lampas"), dead.Note()); err != nil {
		t.Fatal(err)
	}
	// Within its lease the claim is held.
	if got := testClaim(tracker, "desktop", k).Take(context.Background(), "lampas", "me"); !got.Held {
		t.Fatalf("expected the live claim of cloud14 held, got %+v", got)
	}
	k.mu.Lock()
	k.now = k.now.Add(application.BatchClaimTTL + time.Minute)
	k.mu.Unlock()

	got := testClaim(tracker, "desktop", k).Take(context.Background(), "lampas", "me")
	if got.Holding == nil || got.Held {
		t.Fatalf("expected the claim taken back once its lease ran out, got %+v", got)
	}
	defer got.Holding.Release(context.Background())
	if len(got.Said) != 1 || !strings.Contains(got.Said[0], "cloud14") || !strings.Contains(got.Said[0], "batch claim of lampas") {
		t.Fatalf("expected the take-back to name cloud14 and the claim, got %q", got.Said)
	}
	if lease, ok := batchClaimOf(t, tracker, "lampas"); !ok || lease.Host != "desktop" {
		t.Fatalf("expected the claim now desktop's, got %+v", lease)
	}
}

// Two hosts that try for the claim in one moment: one lands, the other skips.
func TestTwoHostsRacingForABatchClaimOneLandsAndTheOtherSkips(t *testing.T) {
	for round := 0; round < 20; round++ {
		tracker := apptest.NewFakeTracker()
		now := time.Date(2026, 11, 21, 21, 0, 0, 0, time.UTC)
		var arrived sync.WaitGroup
		arrived.Add(2)
		var once [2]sync.Once
		claim := func(host string, i int) application.BatchClaim {
			return application.BatchClaim{Notes: tracker, Host: host, TTL: slotTTL, Settle: slotSettle,
				Now: func() time.Time { return now },
				Wait: func(ctx context.Context, d time.Duration) error {
					if d != slotSettle {
						<-ctx.Done() // the renewal's wait
						return ctx.Err()
					}
					// Both have written their lease, if both are going to, before either reads it back.
					once[i].Do(arrived.Done)
					done := make(chan struct{})
					go func() { arrived.Wait(); close(done) }()
					select {
					case <-done:
					case <-time.After(500 * time.Millisecond): // the other found the claim held, and wrote none
					}
					return nil
				}}
		}
		var wg sync.WaitGroup
		results := make([]application.BatchClaimTaken, 2)
		for i, host := range []string{"desktop", "cloud14"} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				results[i] = claim(host, i).Take(context.Background(), "lampas", "mw land on "+host)
			}()
		}
		wg.Wait()
		landed := 0
		for _, r := range results {
			if r.Holding != nil {
				landed++
				defer r.Holding.Release(context.Background())
			} else if !r.Held {
				t.Fatalf("round %d: a host neither landed nor was told the claim was held: %+v", round, r)
			}
		}
		if landed != 1 {
			t.Fatalf("round %d: expected exactly one host to land, got %d: %+v", round, landed, results)
		}
	}
}

func TestABatchClaimTheBeadsServerCannotGiveIsSkippedSayingWhy(t *testing.T) {
	tracker, k := apptest.NewFakeTracker(), newSlotClock()
	tracker.Err = context.DeadlineExceeded
	got := testClaim(tracker, "desktop", k).Take(context.Background(), "lampas", "me")
	if got.Holding != nil || got.Held || len(got.Said) != 1 || !strings.Contains(got.Said[0], "batch claim of lampas was skipped") {
		t.Fatalf("expected the claim skipped and a landing that goes ahead, got %+v", got)
	}
}

func TestTheZeroBatchClaimTakesNothingAndSaysNothing(t *testing.T) {
	got := application.BatchClaim{}.Take(context.Background(), "lampas", "me")
	if got.Holding != nil || got.Held || len(got.Said) != 0 {
		t.Fatalf("expected nothing, got %+v", got)
	}
	if got.Holding.Release(context.Background()) != nil {
		t.Fatal("a release of no holding is nothing")
	}
}

func TestWhereABatchLandsIsTheHostOfItsLiveClaim(t *testing.T) {
	tracker, k := apptest.NewFakeTracker(), newSlotClock()
	if _, on := application.BatchLandsOn(context.Background(), tracker, "lampas", k.Now()); on {
		t.Fatal("expected no claim, no landing")
	}
	got := testClaim(tracker, "cloud14", k).Take(context.Background(), "lampas", "me")
	defer got.Holding.Release(context.Background())
	if host, on := application.BatchLandsOn(context.Background(), tracker, "lampas", k.Now()); !on || host != "cloud14" {
		t.Fatalf("expected the batch landing on cloud14, got %q %v", host, on)
	}
	// A claim whose lease ran out is no landing.
	if _, on := application.BatchLandsOn(context.Background(), tracker, "lampas", k.Now().Add(slotTTL+time.Minute)); on {
		t.Fatal("expected a claim past its lease not to be a landing")
	}
}

// An mw land that finds the rig's claim held by a batch of another host lands
// nothing and says whose batch it is; one that finds it free takes it, and gives
// it back when it ends.
func TestALandingThatFindsTheRigsBatchClaimHeldByAnotherHostLandsNothing(t *testing.T) {
	tracker, k := apptest.NewFakeTracker(), newSlotClock()
	elsewhere := testClaim(tracker, "cloud14", k).Take(context.Background(), "lampas", "mw land on cloud14, landing a batch of lampas")
	if elsewhere.Holding == nil {
		t.Fatalf("expected cloud14's claim taken, got %+v", elsewhere)
	}
	defer elsewhere.Holding.Release(context.Background())

	here := application.BatchLand{Next: application.Next{Host: "desktop"}, Claim: testClaim(tracker, "desktop", k)}
	report := &application.BatchReport{Rig: "lampas"}
	_, _, claimed := here.ClaimTheRig(context.Background(), "lampas", report)
	if claimed {
		t.Fatal("expected a landing that finds the claim held not to go on")
	}
	if len(report.Notes) != 1 || !strings.Contains(report.Notes[0], "landing on cloud14") {
		t.Fatalf("expected the report to say the batch is landing on cloud14, got %q", report.Notes)
	}
}

func TestALandingThatTakesTheRigsBatchClaimGivesItBackWhenItEnds(t *testing.T) {
	tracker, k := apptest.NewFakeTracker(), newSlotClock()
	here := application.BatchLand{Next: application.Next{Host: "cloud14"}, Claim: testClaim(tracker, "cloud14", k)}
	report := &application.BatchReport{Rig: "lampas"}
	_, unclaim, claimed := here.ClaimTheRig(context.Background(), "lampas", report)
	if !claimed {
		t.Fatalf("expected the free claim taken, got notes %q", report.Notes)
	}
	if lease, ok := batchClaimOf(t, tracker, "lampas"); !ok || lease.Host != "cloud14" {
		t.Fatalf("expected cloud14's claim held while the landing runs, got %+v", lease)
	}
	unclaim()
	if _, ok := batchClaimOf(t, tracker, "lampas"); ok {
		t.Fatal("expected the claim given back when the landing ends")
	}
}

// A BatchLand with no claim (a rig on a host that keeps none) lands as it always did.
func TestALandingWithNoBatchClaimGoesAhead(t *testing.T) {
	_, unclaim, claimed := application.BatchLand{}.ClaimTheRig(context.Background(), "lampas", &application.BatchReport{})
	if !claimed {
		t.Fatal("expected a landing with no claim to go on")
	}
	unclaim()
}

// mw-gq6.1616: a batch handed to another host is landed by mw land there, and its
// fixer is a session of that host (Next.Runner is the landing host's own): the
// busy home's no-room refusal of a fixer (mw-gq6.1512) does not follow it.
func TestTheFixerOfABatchRunsOnTheHostLandingIt(t *testing.T) {
	vault := newFakeVault()
	vault.written["batch-millwright-4/"+application.ResultFileName] = `{"subtype":"success"}`
	var out strings.Builder
	runner := endedRunner{apptest.NewFakeRunner(), application.SessionStatus{State: application.StateExited}}
	land := application.BatchLand{
		Next: application.Next{
			Landing: &fakeRetryLanding{}, Runner: runner,
			Boot:  application.SeatBoot{Vault: vault, Harness: claude.New(), Seat: "builder", Host: "cloud14"},
			Vault: vault, Host: "cloud14", Seat: "builder", Out: &out, Err: io.Discard,
			// The landing host has room: the home it was handed from is far over its cores.
			Load: &apptest.FakeHostLoad{Reading: application.LoadReading{Load: 0.5, Cores: 8}},
		},
		Records: noRecords{}, Fix: true,
	}
	report := &application.BatchReport{
		Rig: "millwright", Target: "main", Number: 4, Base: "base0123",
		Queue:    []application.BatchStory{{ID: "mw-r9xo1pj.4", Title: "The set-aside story"}},
		SetAside: []application.BatchStory{{ID: "mw-r9xo1pj.4", Title: "The set-aside story", Why: "conflicts", Conflicts: []string{"CONFLICT (content): a_test.ts"}}},
	}
	land.FixTheBatch(context.Background(), "/rigs/millwright/.landing-4", report)
	if report.Fixer == nil || !report.Fixer.Started {
		t.Fatalf("expected the fixer started on cloud14, got %+v (held %q)", report.Fixer, report.Held)
	}
	if got := runner.Started(); len(got) != 1 || got[0] != application.BatchFixerName("millwright", 4) {
		t.Fatalf("expected the fixer batch-millwright-4 started by cloud14's own runner, got %v", got)
	}
	if !strings.Contains(out.String(), "land on cloud14: batch 4 of millwright given its fixer") {
		t.Fatalf("expected the landing to say its fixer runs on cloud14, got %q", out.String())
	}
}

// And a landing host with no room starts no fixer: the batch is held and stays
// queued, as on any host.
func TestABatchHandedToAHostWithNoRoomIsGivenNoFixer(t *testing.T) {
	vault := newFakeVault()
	runner := endedRunner{apptest.NewFakeRunner(), application.SessionStatus{State: application.StateExited}}
	land := application.BatchLand{
		Next: application.Next{
			Landing: &fakeRetryLanding{}, Runner: runner,
			Boot:  application.SeatBoot{Vault: vault, Harness: claude.New(), Seat: "builder", Host: "cloud14"},
			Vault: vault, Host: "cloud14", Seat: "builder", Out: io.Discard, Err: io.Discard,
			Load: &apptest.FakeHostLoad{Reading: application.LoadReading{Load: 9, Cores: 8}},
		},
		Records: noRecords{}, Fix: true,
	}
	report := &application.BatchReport{
		Rig: "millwright", Target: "main", Number: 4, Base: "base0123",
		Queue:    []application.BatchStory{{ID: "mw-r9xo1pj.4", Title: "A story"}},
		SetAside: []application.BatchStory{{ID: "mw-r9xo1pj.4", Title: "A story", Why: "conflicts", Conflicts: []string{"CONFLICT (content): a_test.ts"}}},
	}
	land.FixTheBatch(context.Background(), "/rigs/millwright/.landing-4", report)
	if got := runner.Started(); len(got) != 0 {
		t.Fatalf("expected no fixer on a host with no room, got %v", got)
	}
}
