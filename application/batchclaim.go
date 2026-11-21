package application

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// BatchClaimKey is the key a rig's batch claim is kept under in the shared
// tracker's key-value store.
func BatchClaimKey(rig string) string { return "batch-claim." + rig }

// BatchClaimTTL is how long a batch claim's lease lasts unless its holder renews
// it, which it does every third of that while it lands: a host that died
// holding the claim keeps the rig for this long and no longer (mw-gq6.1616).
const BatchClaimTTL = 5 * time.Minute

// BatchClaim is a rig's batch claim across hosts: a lease, kept on the beads
// server every host shares, that the one mw land of a rig, on whichever host it
// runs, holds from the moment it has a batch to take until it has landed or held
// it. A rig has one batch at a time across all hosts; its merge slot keeps the
// landings of one host apart, and this keeps the hosts apart, so that a batch
// the home hands to another host (BatchLandings.landElsewhere) is not landed a
// second time by the home's own follower, nor by a second host.
//
// It is taken the way CrossSlot takes its lease, on a store with no
// compare-and-set: by writing it, waiting Settle and reading it back. It is
// not waited for: an mw land that finds it held lands nothing, says whose it is
// and ends, since the batch that holds it takes the whole queue. A lease past
// its expiry is a host that went away, and is taken back, and said so. A claim
// that cannot be read or written is skipped, and said so: the landing goes ahead
// as it did before there was one, and the remote refuses the second push.
//
// The zero BatchClaim, or one with no Notes, takes nothing and says nothing.
type BatchClaim struct {
	Notes SlotNotes
	// Host is this host, written in the lease so that mw status can say where
	// the batch lands.
	Host string
	// TTL and Settle are BatchClaimTTL and CrossSlotSettle when zero.
	TTL    time.Duration
	Settle time.Duration
	// Now and Wait are the clock: the real one unless a test moves time.
	Now  func() time.Time
	Wait func(ctx context.Context, d time.Duration) error
}

// BatchClaimTaken is what taking a batch claim came to: the Holding, when it is
// this landing's to give back; Held and By, when another landing has it and this
// one is not to land; neither, when the claim was skipped or there is none, and
// the landing goes ahead. Said is what the report says of it.
type BatchClaimTaken struct {
	Holding *CrossHolding
	Held    bool
	By      CrossSlotLease
	Said    []string
}

func (c BatchClaim) slot() CrossSlot {
	return CrossSlot{Notes: c.Notes, Host: c.Host, TTL: orDefault(c.TTL, BatchClaimTTL), Settle: c.Settle, Now: c.Now, Wait: c.Wait}
}

// Take tries once for rig's batch claim, for holder.
func (c BatchClaim) Take(ctx context.Context, rig, holder string) BatchClaimTaken {
	if c.Notes == nil || strings.TrimSpace(rig) == "" {
		return BatchClaimTaken{}
	}
	s := c.slot()
	key := BatchClaimKey(rig)
	skipped := func(err error) BatchClaimTaken {
		return BatchClaimTaken{Said: []string{fmt.Sprintf("the batch claim of %s was skipped, so this landing went ahead without it: the beads server could not be reached: %v", rig, err)}}
	}
	note, err := s.Notes.Note(ctx, key)
	if err != nil {
		return skipped(err)
	}
	now := s.now()
	var said []string
	if strings.TrimSpace(note) != "" {
		held, ok := ReadCrossSlotLease(note)
		switch {
		case !ok:
			said = []string{fmt.Sprintf("took over the batch claim of %s, whose note could not be read as a lease: %q", rig, clippedTo(note, 120))}
		case now.Before(held.Expires):
			return BatchClaimTaken{Held: true, By: held}
		default:
			said = []string{fmt.Sprintf("took back the batch claim of %s from %s: its lease was taken %s ago and ran out %s ago, so its host is gone",
				rig, held.Holder, SlotAge(now.Sub(held.Taken)), SlotAge(now.Sub(held.Expires)))}
		}
	}
	mine := CrossSlotLease{Holder: holder, Host: s.Host, Token: newSlotToken(), Taken: now, Expires: now.Add(s.ttl())}
	if err := s.Notes.SetNote(ctx, key, mine.Note()); err != nil {
		return skipped(err)
	}
	if err := s.wait(ctx, s.settle()); err != nil {
		return BatchClaimTaken{Said: append(said, fmt.Sprintf("the batch claim of %s was not taken: %v; the lease written for it runs out by itself at %s", rig, err, mine.Expires.UTC().Format(time.RFC3339)))}
	}
	back, err := s.Notes.Note(ctx, key)
	if err != nil {
		return skipped(err)
	}
	if read, ok := ReadCrossSlotLease(back); !ok || read.Token != mine.Token {
		// Another host wrote its own in the same moment, after this one: it has
		// the claim, and this landing leaves the rig to it.
		return BatchClaimTaken{Held: true, By: read}
	}
	holding := s.hold(ctx, key, rig, mine)
	holding.name = "the batch claim"
	return BatchClaimTaken{Holding: holding, Said: said}
}

// BatchLandsOn is the host a rig's batch is landing on: the host of its batch
// claim, when the lease has not run out at now. mw status says it.
func BatchLandsOn(ctx context.Context, notes interface {
	Note(ctx context.Context, key string) (string, error)
}, rig string, now time.Time) (host string, landing bool) {
	if notes == nil {
		return "", false
	}
	note, err := notes.Note(ctx, BatchClaimKey(rig))
	if err != nil {
		return "", false
	}
	lease, ok := ReadCrossSlotLease(note)
	if !ok || !now.Before(lease.Expires) || lease.Host == "" {
		return "", false
	}
	return lease.Host, true
}

// HeldMessage is what an mw land that finds the claim held says: whose it is.
func (t BatchClaimTaken) HeldMessage(rig string) string {
	by := t.By.Host
	if by == "" {
		by = "another host"
	}
	return fmt.Sprintf("a batch of %s is landing on %s (%s): this landing takes nothing, and the queue is that batch's",
		rig, by, t.By.Holder)
}

// claimTheRig takes the rig's batch claim for this landing. It returns what the
// report is to say of it, how to give it back, and whether the landing goes on:
// not when another landing holds the claim, which says so in the report's notes.
func (b BatchLand) claimTheRig(ctx context.Context, rig string, report *BatchReport) (said []string, unclaim func(), claimed bool) {
	got := b.Claim.Take(ctx, rig, fmt.Sprintf("mw land on %s, landing a batch of %s", b.Next.Host, rig))
	if got.Held {
		report.Notes = append(report.Notes, got.HeldMessage(rig))
		return nil, func() {}, false
	}
	return got.Said, func() {
		// What went wrong while it was held is said as it ends: the report has
		// been printed by then.
		for _, trouble := range got.Holding.Release(context.WithoutCancel(ctx)) {
			b.say("%s", trouble)
		}
	}, true
}
