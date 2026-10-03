//go:build beads_integration

package beads_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// A claim through a real bd carries bd's own lease, five minutes in bd 1.3.0;
// its holder's heartbeat pushes it forward and nobody else's may; another
// actor's claim fails cleanly, as a *ClaimHeldError, writing nothing; and a
// lease that still holds is neither stale nor reclaimed. bd's lease length is
// not settable, so a lease actually running out is the feature's and the
// stand-in's to show, not this case's: it would take five minutes of waiting.
func TestAClaimIsALeaseInARealBd(t *testing.T) {
	t.Parallel()
	vault := throwawayVault(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	epicID := bdRun(t, vault, beads.Program, "create", "A walking skeleton", "-t", "epic",
		"--metadata", `{"rig":"millwright","branch":"main","harness":"claude","model":"opus","effort":"high","formula":"tdd-feature","host":"vps"}`,
		"--silent")
	storyID := bdRun(t, vault, beads.Program, "create", "A story claimed as a lease",
		"--parent", epicID, "--silent")

	vps := beads.New(vault, beads.WithActor("mw@vps"))
	laptop := beads.New(vault, beads.WithActor("mw@laptop"))

	claimed := time.Now().UTC()
	if err := vps.ClaimStory(ctx, storyID); err != nil {
		t.Fatalf("claiming %s: %v", storyID, err)
	}
	first, err := vps.ShowStory(ctx, storyID)
	if err != nil {
		t.Fatalf("showing %s: %v", storyID, err)
	}
	if first.LeaseExpires.Before(claimed.Add(time.Minute)) || first.LeaseExpires.After(claimed.Add(time.Hour)) {
		t.Fatalf("expected the claim to carry a lease of some minutes from %v, got %v", claimed, first.LeaseExpires)
	}

	// bd prints the lease to the second, and under load a heartbeat can land in
	// the same printed second as the claim: heartbeat again, a second apart,
	// until the printed lease moves, for up to ten seconds.
	var beaten application.StoryDetail
	deadline := time.Now().Add(10 * time.Second)
	for {
		time.Sleep(1100 * time.Millisecond)
		if err := vps.HeartbeatClaim(ctx, storyID); err != nil {
			t.Fatalf("heartbeating %s: %v", storyID, err)
		}
		beaten, err = vps.ShowStory(ctx, storyID)
		if err != nil {
			t.Fatalf("showing %s after the heartbeat: %v", storyID, err)
		}
		if beaten.LeaseExpires.After(first.LeaseExpires) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected the heartbeat to push the lease past %v within ten seconds, got %v", first.LeaseExpires, beaten.LeaseExpires)
		}
	}

	if err := laptop.HeartbeatClaim(ctx, storyID); err == nil {
		t.Fatalf("expected a heartbeat from another actor than the holder to fail")
	}

	err = laptop.ClaimStory(ctx, storyID)
	var held *application.ClaimHeldError
	if !errors.As(err, &held) || held.Holder != "mw@vps" || held.ID != storyID {
		t.Fatalf("expected the second claim to fail as held by mw@vps, got %v", err)
	}
	after, err := vps.ShowStory(ctx, storyID)
	if err != nil {
		t.Fatalf("showing %s after the refused claim: %v", storyID, err)
	}
	if after.Assignee != "mw@vps" || !after.LeaseExpires.Equal(beaten.LeaseExpires) {
		t.Fatalf("expected the refused claim to write nothing, got %q until %v", after.Assignee, after.LeaseExpires)
	}

	if stale, err := vps.StaleClaims(ctx, time.Now()); err != nil || len(stale) != 0 {
		t.Fatalf("expected a lease that still holds not to be stale, got %+v: %v", stale, err)
	}
	stale, err := vps.StaleClaims(ctx, beaten.LeaseExpires.Add(time.Minute))
	if err != nil || len(stale) != 1 || stale[0].Story.ID != storyID {
		t.Fatalf("expected %s stale once its lease is past, got %+v: %v", storyID, stale, err)
	}
	if path := stale[0].Merged(); path.Host != "vps" {
		t.Fatalf("expected the stale claim to carry its epic's defaults, got %+v", path)
	}

	reclaimed, err := vps.ReclaimStory(ctx, storyID)
	if err != nil || reclaimed {
		t.Fatalf("expected a lease that still holds not to be reclaimed, got %v: %v", reclaimed, err)
	}
	if still, err := vps.ShowStory(ctx, storyID); err != nil || still.Assignee != "mw@vps" || still.Status != application.StatusInProgress {
		t.Fatalf("expected %s still claimed by mw@vps, got %+v: %v", storyID, still, err)
	}
}

// mw-gq6.119: ReleaseClaim names its own actor as bd's --if-assignee guard,
// so a give-back by a host that is not the current holder can never clear
// another host's claim.
func TestReleaseClaimByAnotherActorLeavesTheClaimUntouched(t *testing.T) {
	t.Parallel()
	vault := throwawayVault(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	epicID := bdRun(t, vault, beads.Program, "create", "A walking skeleton", "-t", "epic",
		"--metadata", `{"rig":"millwright","branch":"main","harness":"claude","model":"opus","effort":"high","formula":"tdd-feature","host":"vps"}`,
		"--silent")
	storyID := bdRun(t, vault, beads.Program, "create", "A story claimed by one host",
		"--parent", epicID, "--silent")

	vps := beads.New(vault, beads.WithActor("mw@vps"))
	laptop := beads.New(vault, beads.WithActor("mw@laptop"))

	if err := vps.ClaimStory(ctx, storyID); err != nil {
		t.Fatalf("claiming %s: %v", storyID, err)
	}
	held, err := vps.ShowStory(ctx, storyID)
	if err != nil {
		t.Fatalf("showing %s: %v", storyID, err)
	}

	if err := laptop.ReleaseClaim(ctx, storyID); err == nil {
		t.Fatalf("expected a release by mw@laptop, which does not hold %s, to fail", storyID)
	}

	after, err := vps.ShowStory(ctx, storyID)
	if err != nil {
		t.Fatalf("showing %s after the refused release: %v", storyID, err)
	}
	if after.Status != application.StatusInProgress || after.Assignee != "mw@vps" || !after.LeaseExpires.Equal(held.LeaseExpires) {
		t.Fatalf("expected the refused release to leave %s claimed by mw@vps until %v, got %q held by %q until %v",
			storyID, held.LeaseExpires, after.Status, after.Assignee, after.LeaseExpires)
	}

	if err := vps.ReleaseClaim(ctx, storyID); err != nil {
		t.Fatalf("giving back %s's own claim: %v", storyID, err)
	}
	given, err := vps.ShowStory(ctx, storyID)
	if err != nil {
		t.Fatalf("showing %s after its own release: %v", storyID, err)
	}
	if given.Status != application.StatusOpen || given.Assignee != "" {
		t.Fatalf("expected %s open and unassigned after its holder released it, got %q held by %q", storyID, given.Status, given.Assignee)
	}
}

// A story given back keeps the run:* label its earlier attempt left, and a real
// bd's claim of it clears that label in the same update, keeping the others
// (mw-gq6.258): the event follower reads an in-progress story carrying
// run:blocked as refused.
func TestAClaimClearsTheRunStateAnEarlierAttemptLeft(t *testing.T) {
	t.Parallel()
	vault := throwawayVault(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	epicID := bdRun(t, vault, beads.Program, "create", "A walking skeleton", "-t", "epic", "--silent")
	storyID := bdRun(t, vault, beads.Program, "create", "A story an earlier host gave back",
		"--parent", epicID, "--labels", "needs-governor", "--silent")

	vps := beads.New(vault, beads.WithActor("mw@vps"))
	if err := vps.SetStoryState(ctx, storyID, application.RunState, application.RunBlocked, "left by an earlier host"); err != nil {
		t.Fatalf("recording run=blocked on %s: %v", storyID, err)
	}
	if err := vps.ClaimStory(ctx, storyID); err != nil {
		t.Fatalf("claiming %s: %v", storyID, err)
	}

	claimed, err := vps.ShowStory(ctx, storyID)
	if err != nil {
		t.Fatalf("showing %s: %v", storyID, err)
	}
	if claimed.Status != beads.StatusInProgress || claimed.Assignee != "mw@vps" {
		t.Fatalf("expected %s claimed by mw@vps, got status %q assignee %q", storyID, claimed.Status, claimed.Assignee)
	}
	hasLabel := func(want string) bool {
		for _, label := range claimed.Labels {
			if label == want {
				return true
			}
		}
		return false
	}
	if hasLabel(application.RunState + ":" + application.RunBlocked) {
		t.Errorf("expected the claim to clear run:%s, labels are %v", application.RunBlocked, claimed.Labels)
	}
	if !hasLabel("needs-governor") {
		t.Errorf("expected the claim to leave the other labels alone, labels are %v", claimed.Labels)
	}
	if state := bdRun(t, vault, beads.Program, "state", storyID, application.RunState); state == application.RunBlocked {
		t.Errorf("expected %s no longer to be recorded %s=%s", storyID, application.RunState, application.RunBlocked)
	}
}
