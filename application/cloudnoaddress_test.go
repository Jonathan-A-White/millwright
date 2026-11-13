package application_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/cloud"
)

// noAddressErr is what BoxWork says of a box the hub has no peer for: the
// wrapper's last line, behind "reading <box>'s address" (mw-gq6.1143).
var noAddressErr = errors.New("reading cloud1's address: cloud1 is not a peer of the hub: it has no address")

// aBoxWithNoAddress is boxWorkRig's cloud1, made without a hub address: the
// provider can read none for it and its worktrees cannot be asked, and the
// tracker is the check's Work and Notes (the box never wrote its LastSyncKey).
func aBoxWithNoAddress(t *testing.T, box application.CloudBox) (*boxWorkRig, *apptest.FakeTracker) {
	t.Helper()
	r := aBoxWithWork()
	r.provider.Unreachable = map[string]bool{"cloud1": true}
	r.box.listErr = noAddressErr
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-n", domain.Path{Rig: "millwright", Branch: "main", Host: domain.HostAuto})
	r.check.Work, r.check.Notes, r.check.Stories = tracker, tracker, tracker
	r.book.State.Boxes = []application.CloudBox{box}
	return r, tracker
}

func claimOnBox(t *testing.T, tracker *apptest.FakeTracker, id, holder string) {
	t.Helper()
	tracker.AddStory("mw-n", domain.Story{ID: id, Title: "story " + id})
	if err := tracker.ClaimAs(id, holder, time.Time{}); err != nil {
		t.Fatal(err)
	}
}

func TestMwCloudDownOfABoxWithNoAddressAndNoClaimDestroysItWithoutForceAndSaysWhyNothingNeededSaving(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	r, _ := aBoxWithNoAddress(t, application.CloudBox{Name: "cloud1", Up: now.Add(-100 * time.Minute)})
	text, err := r.check.Down(context.Background(), "cloud1")
	if err != nil {
		t.Fatalf("expected mw cloud down to destroy a box with no address and no claim, got %v\n%s", err, text)
	}
	if !slices.Contains(r.provider.Calls(), "down cloud1") || len(r.book.State.Boxes) != 0 {
		t.Errorf("expected the box destroyed, calls %v, book %+v", r.provider.Calls(), r.book.State.Boxes)
	}
	if !strings.Contains(text, "no address") || !strings.Contains(text, "nothing") || !strings.Contains(text, "saving") {
		t.Errorf("expected the output to say why nothing needed saving, got:\n%s", text)
	}
}

func TestMwCloudDownOfABoxWithNoAddressStillRefusesWhileAStoryIsClaimedOnIt(t *testing.T) {
	for _, holder := range []string{"mw@cloud1", "builder@cloud1"} {
		now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
		r, tracker := aBoxWithNoAddress(t, application.CloudBox{Name: "cloud1", Up: now.Add(-100 * time.Minute)})
		claimOnBox(t, tracker, "mw-n.1", holder)
		_, err := r.check.Down(context.Background(), "cloud1")
		if err == nil || slices.Contains(r.provider.Calls(), "down cloud1") || len(r.book.State.Boxes) != 1 {
			t.Errorf("%s: expected the box kept and the down refused, got %v, calls %v", holder, err, r.provider.Calls())
		}
	}
}

func TestMwCloudDownLeavesAClaimOnAnotherBoxOutOfTheNoAddressRule(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	r, tracker := aBoxWithNoAddress(t, application.CloudBox{Name: "cloud1", Up: now.Add(-100 * time.Minute)})
	claimOnBox(t, tracker, "mw-n.1", "mw@cloud2")
	if _, err := r.check.Down(context.Background(), "cloud1"); err != nil {
		t.Fatalf("expected a claim on cloud2 to hold cloud1 back no more, got %v", err)
	}
}

func TestABoxThatIsUnreachableForAnotherReasonIsStillRefusedWithNoClaim(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	r, _ := aBoxWithNoAddress(t, application.CloudBox{Name: "cloud1", Up: now.Add(-100 * time.Minute)})
	r.provider.Unreachable = nil // it has an address
	r.box.listErr = errors.New("ssh: connect to host 10.88.0.1 port 22: Connection timed out")
	_, err := r.check.Down(context.Background(), "cloud1")
	if err == nil || slices.Contains(r.provider.Calls(), "down cloud1") {
		t.Errorf("expected a box with an address that cannot be asked refused, got %v, calls %v", err, r.provider.Calls())
	}
}

// madeAt is cloud1 made d ago, never dispatched.
func madeAt(now time.Time, d time.Duration) application.CloudBox {
	return application.CloudBox{Name: "cloud1", Up: now.Add(-d), Made: now.Add(-d)}
}

func TestTheCheckDestroysABoxThatHasNotDispatchedLongAfterItsMakeAndNamesWhy(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	r, _ := aBoxWithNoAddress(t, madeAt(now, 100*time.Minute))
	r.book.State.Boxes[0].PaceFault = "could not set the dispatch timer of cloud1 to every 5 minutes: cloud1 is not a peer of the hub: it has no address"
	r.book.State.Boxes[0].IdleSince = now // not idle long: the idle rule is not what ends it
	if _, err := r.check.Run(context.Background()); err != nil {
		t.Fatalf("expected the check to destroy the box and go on, got %v", err)
	}
	if !slices.Contains(r.provider.Calls(), "down cloud1") || len(r.book.State.Boxes) != 0 {
		t.Fatalf("expected the box destroyed, calls %v, book %+v", r.provider.Calls(), r.book.State.Boxes)
	}
	events := strings.Join(cloudEventDetails(r.log), "\n")
	if !strings.Contains(events, "down cloud1: ") || !strings.Contains(events, "no dispatch") || !strings.Contains(events, "not a peer of the hub") {
		t.Errorf("expected a cloud event naming why, got:\n%s", events)
	}
}

func TestTheCheckMakesAReplacementForABoxItDestroyedForNotDispatching(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	r, tracker := aBoxWithNoAddress(t, madeAt(now, 100*time.Minute))
	tracker.AddStory("mw-n", domain.Story{ID: "mw-n.9", Title: "waits"})
	if err := tracker.SetStoryMetadata(context.Background(), "mw-n.9", map[string]string{"host": domain.HostAuto}); err != nil {
		t.Fatal(err)
	}
	r.check.Plan.HostCaps = map[string]int{}
	if _, err := r.check.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := r.provider.TakeBegun(); !slices.Equal(got, []string{"cloud2"}) {
		t.Errorf("expected cloud2 begun as the replacement (cloud1's name is barred), got %v", got)
	}
}

func TestABoxYoungerThanTheLimitOrAlreadyDispatchedIsNotDestroyedForNotDispatching(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	young, _ := aBoxWithNoAddress(t, madeAt(now, 25*time.Minute))
	if _, err := young.check.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(young.provider.Calls(), "down cloud1") {
		t.Errorf("expected a box 25m old kept, calls %v", young.provider.Calls())
	}
	old := madeAt(now, 100*time.Minute)
	old.Dispatched = now.Add(-90 * time.Minute)
	dispatched, _ := aBoxWithNoAddress(t, old)
	dispatched.book.State.Boxes[0].IdleSince = now
	if _, err := dispatched.check.Run(context.Background()); err == nil {
		// Its pace cannot be set (no address) but it is not destroyed for that.
		_ = err
	}
	if slices.Contains(dispatched.provider.Calls(), "down cloud1") {
		t.Errorf("expected a box that has dispatched kept, calls %v", dispatched.provider.Calls())
	}
}

func cloudEventDetails(log *apptest.FakeEventLog) []string {
	var details []string
	for _, e := range cloudEventsOf(log) {
		details = append(details, e.Detail)
	}
	return details
}

// A vultr-boost stand-in that, like the real one, dies on `address` for a box the
// hub has no peer for, and destroys or lists what it is asked: the whole way from
// the wrapper's words to the destroy, through the real adapters (mw-gq6.1143).
func TestMwCloudDownThroughTheWrapperDestroysABoxItsHubHasNoPeerFor(t *testing.T) {
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	command := filepath.Join(dir, "vultr-boost")
	script := "#!/bin/sh\necho \"$*\" >>" + calls + "\ncase $1 in\n" +
		"address) echo 'vultr-boost: cloud1 is not a peer of the hub: it has no address' >&2; exit 1 ;;\n" +
		"list) exit 0 ;;\nesac\n"
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	vultr := cloud.Vultr{Command: command}
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	vault := newFakeVault()
	check := application.CloudCheck{
		Work: apptest.NewFakeTracker(), Provider: vultr, Box: cloud.BoxWork{Address: vultr.Address, Command: "false"},
		Vault: vault, Files: &apptest.FakeVaultFiles{}, Log: &apptest.FakeEventLog{}, Host: "desktop", Plan: cloudPlan,
		Book: &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-10", Boxes: []application.CloudBox{{Name: "cloud1", Up: now.Add(-100 * time.Minute)}}}},
		Now:  func() time.Time { return now },
	}
	text, err := check.Down(context.Background(), "cloud1")
	if err != nil {
		t.Fatalf("expected mw cloud down to destroy the box, got %v\n%s", err, text)
	}
	raw, _ := os.ReadFile(calls)
	if !strings.Contains(string(raw), "down --yes --name cloud1") || !strings.Contains(text, "nothing on it needed saving") {
		t.Errorf("expected the wrapper asked to destroy cloud1 and the output to say why nothing was saved, calls:\n%s\ntext:\n%s", raw, text)
	}
}

func TestAPlansNoDispatchMinutesMovesWhenTheCheckDestroysAnUndispatchedBox(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		limit time.Duration
		down  bool
	}{{45 * time.Minute, false}, {15 * time.Minute, true}} {
		r, _ := aBoxWithNoAddress(t, madeAt(now, 40*time.Minute))
		r.check.Plan.NoDispatch = c.limit
		_, _ = r.check.Run(context.Background())
		if got := slices.Contains(r.provider.Calls(), "down cloud1"); got != c.down {
			t.Errorf("limit %v: expected destroyed %v, calls %v", c.limit, c.down, r.provider.Calls())
		}
	}
}
