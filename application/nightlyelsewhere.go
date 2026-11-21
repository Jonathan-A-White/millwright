package application

import (
	"context"
	"fmt"
	"io"
	"time"
)

// The two nightly runs of the home's follower that a busy home hands to another
// host (mw-gq6.1617): the whole suite (mw suite <rig> --file) and the timing run.
const (
	NightlySuiteKind  = "suite"
	NightlyTimingKind = "timing"
)

// NightlyElsewhere is the host book of the nightly runs and how one runs on
// another host: the far end of an ssh. Rooms is BatchElsewhere's ask (mw land
// <rig> --room); Run runs the nightly kind of rig on host, apart from the
// follower, and waits for it. The adapter is cmd/mw's sshNightlyHosts; the fake is
// apptest.FakeNightlyElsewhere.
type NightlyElsewhere interface {
	Rooms(ctx context.Context, rig string) ([]BatchRoom, error)
	Run(ctx context.Context, host, kind, rig string) error
}

// NightlyPlace is where the home's nightly runs are made (mw-gq6.1617): on the
// home as they were, or, when the home is busy and another host has room, on that
// host. The zero NightlyPlace, or one with no Elsewhere, runs everything on the
// home.
type NightlyPlace struct {
	Elsewhere NightlyElsewhere
	// Load and GateHolder are the home's own: its load, and whose gate holds its
	// gate lock ("" for nobody). The home is busy when its load has reached its
	// cores or its gate lock is held.
	Load       HostLoad
	GateHolder func() string
	// Host is this host, left out of the hosts a run may be handed to.
	Host string
	// Notes is where the run's claim is written, so that mw status can say where
	// it runs; nil writes none.
	Notes SlotNotes
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// Out is where the lines are said.
	Out io.Writer
}

// Choose is the host the nightly kind of rig is run on instead of the home
// (mw-gq6.1617): when the home is busy, the host of the book with room that has
// the lowest load for its cores. None, and so the run on the home as before, when
// the home is not busy, the book is not there or cannot be read, or no host has
// room.
func (p NightlyPlace) Choose(ctx context.Context, kind, rig string) (string, bool) {
	if p.Elsewhere == nil {
		return "", false
	}
	busy := homeBusyWhy(ctx, p.Load, p.GateHolder)
	if busy == "" {
		return "", false
	}
	rooms, err := p.Elsewhere.Rooms(ctx, rig)
	if err != nil {
		return "", false
	}
	if kind == NightlyTimingKind {
		rooms = withMwTiming(rooms)
	}
	host, ok := freestHost(rooms, p.Host)
	if ok {
		p.say("  nightly %s of %s: %s is busy (%s), so it runs on %s, which has room\n", kind, rig, p.Host, busy, host)
	}
	return host, ok
}

// withMwTiming is the rooms of the hosts whose mw has mw timing: a timing run
// handed to a box whose mw is older fails with an ssh error and the rig is not
// tried again that day (mw-gq6.1621). A suite goes to any box, mw suite --file
// being older than mw land --room.
func withMwTiming(rooms []BatchRoom) []BatchRoom {
	var with []BatchRoom
	for _, r := range rooms {
		if r.HasTiming {
			with = append(with, r)
		}
	}
	return with
}

// RunOn runs the nightly kind of rig on host and waits for it, with a claim on
// the shared tracker that mw status reads where the run is from. A follower that
// stops leaves the claim to run out by itself (BatchClaimTTL): the run goes on
// apart from it.
func (p NightlyPlace) RunOn(ctx context.Context, host, kind, rig string) error {
	holding := p.claim(ctx, host, kind, rig)
	err := p.Elsewhere.Run(ctx, host, kind, rig)
	if ctx.Err() == nil {
		for _, trouble := range holding.Release(context.WithoutCancel(ctx)) {
			p.say("  nightly %s of %s: %s\n", kind, rig, trouble)
		}
	}
	if err != nil {
		return fmt.Errorf("on %s: %w", host, err)
	}
	return nil
}

// claim writes the lease that says the run is on host, and renews it until it is
// released. A claim that cannot be written is said and skipped: the run goes ahead
// all the same, and status does not say where it is.
func (p NightlyPlace) claim(ctx context.Context, host, kind, rig string) *CrossHolding {
	if p.Notes == nil {
		return nil
	}
	slot := CrossSlot{Notes: p.Notes, Host: host, TTL: BatchClaimTTL, Now: p.Now}
	taken := slot.now()
	lease := CrossSlotLease{
		Holder: fmt.Sprintf("the nightly %s of %s, handed on by %s", kind, rig, p.Host),
		Host:   host, Token: newSlotToken(), Taken: taken, Expires: taken.Add(slot.ttl()),
	}
	if err := p.Notes.SetNote(ctx, NightlyRunKey(kind, rig), lease.Note()); err != nil {
		p.say("  nightly %s of %s: its claim could not be written, so mw status does not say it is on %s: %v\n", kind, rig, host, err)
		return nil
	}
	holding := slot.hold(ctx, NightlyRunKey(kind, rig), rig, lease)
	holding.name = "the nightly " + kind + " claim"
	return holding
}

func (p NightlyPlace) say(format string, args ...any) {
	if p.Out != nil {
		fmt.Fprintf(p.Out, format, args...)
	}
}

// NightlyRunKey is the key the claim of a nightly run handed to another host is
// kept under in the shared tracker's key-value store.
func NightlyRunKey(kind, rig string) string { return "nightly-on." + kind + "." + rig }

// NightlyRun is a nightly run that is under way on another host: its kind
// (NightlySuiteKind or NightlyTimingKind), its rig and the host.
type NightlyRun struct {
	Kind, Rig, Host string
}

// NightlyRunsOn is the host the nightly kind of rig runs on, when it was handed
// to another host and its claim has not run out at now.
func NightlyRunsOn(ctx context.Context, notes interface {
	Note(ctx context.Context, key string) (string, error)
}, kind, rig string, now time.Time) (host string, running bool) {
	if notes == nil {
		return "", false
	}
	note, err := notes.Note(ctx, NightlyRunKey(kind, rig))
	if err != nil {
		return "", false
	}
	lease, ok := ReadCrossSlotLease(note)
	if !ok || !now.Before(lease.Expires) || lease.Host == "" {
		return "", false
	}
	return lease.Host, true
}
