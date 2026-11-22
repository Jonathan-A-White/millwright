package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// DefaultDiskStopFreeGB is the stop floor: the free space, in gigabytes, under which
// the disk guard freezes a host's Builders. The config file's [dispatch]
// stop_free_gb changes it, and 0 turns the guard off. The desktop's D: went to 3 MB
// free with four sessions running (mw-6ww.183); 5 GB leaves a Builder's worktree
// and its build room to be frozen where it stands (provisional).
const DefaultDiskStopFreeGB int64 = 5

// DiskGuardLiftMarginGB is how many gigabytes over the stop floor a host's free
// space must be again before the disk guard lets its Builders go on: a floor the
// space only just clears would freeze them again on the next tick (mw-6ww.185.7).
const DiskGuardLiftMarginGB int64 = 5

// DiskGuardFreeEvery is how long the guard waits before it frees space again while
// the host stays frozen and under the line that lifts it: the sweep and the tidy
// read a whole disk.
const DiskGuardFreeEvery = time.Hour

// DiskGuardHolders is how many of the largest directories under the home
// directory the mail names when the freeing could not get the host back over its
// floor.
const DiskGuardHolders = 5

// GiveWayDisk is what a give-way marker holds when the disk guard, and not a
// person at the screen, froze the host: the marker is the disk guard's own, and
// only it lifts it (mw-6ww.185.7).
type GiveWayDisk struct {
	// FreedAt is when the guard last ran the sweep and the tidy this episode.
	FreedAt time.Time `json:"freed_at,omitempty"`
	// Mailed is whether the Mayor has been told of this episode.
	Mailed bool `json:"mailed,omitempty"`
}

// forRequest is the marker as an on of req leaves it: one the disk guard asks for
// is the guard's (it keeps an episode), and one a person asks for over the guard's
// takes it over, so that the guard does not lift it when there is room again.
func (m GiveWayMarker) forRequest(req GiveWayRequest, by string) GiveWayMarker {
	switch {
	case req.Disk && m.Disk == nil:
		m.Disk = &GiveWayDisk{}
	case !req.Disk && m.Disk != nil:
		m.Disk = nil
		m.By = by
	}
	return m
}

// DiskFreeUp frees what the host can free without hurting a story: mw host sweep
// and mw host tidy go-cache and tmp. Each line it returns says one step and what
// it did; a step that fails is a line, not an error.
type DiskFreeUp interface {
	FreeUp(ctx context.Context) []string
}

// DiskHolder is a directory and the space it takes.
type DiskHolder struct {
	Path  string
	Bytes int64
}

// DiskHolders says what holds the space of the home directory.
type DiskHolders interface {
	// Largest is the n largest directories directly under the home directory,
	// largest first.
	Largest(ctx context.Context, n int) ([]DiskHolder, error)
}

// DiskGuard is the stop floor (mw-6ww.185.7). A floor that only refuses new stories
// does not stop the ones already running from filling a disk: when the host's free
// space, its root filesystem's or, on a WSL host, its Windows drive's, is under
// StopFreeGB, the guard freezes every Builder where it stands (give way's own
// machinery; the heartbeats go on, so no lease lapses), frees what can be freed and
// mails the Mayor once. When the space is back over the floor plus
// DiskGuardLiftMarginGB it lets them go on, and a session that died while frozen is
// retried as give way's off does. Nothing is killed and nothing is lost.
//
// A host that gives way to a person is left to her: the guard neither lifts her
// freeze nor adds its own marker over hers.
type DiskGuard struct {
	// Way is the host's give-way: its ports, and the marker the freeze is.
	Way GiveWay
	// Load reads the free space (LoadReading's root and drive).
	Load HostLoad
	// Free frees space; Holders names what holds what is left.
	Free    DiskFreeUp
	Holders DiskHolders
	// Lock keeps two ticks from running the guard at once (the sweep takes minutes);
	// a tick that does not get it does nothing. nil takes none.
	Lock GristLock
	// Mailbox is where the one mail of an episode goes; nil sends none.
	Mailbox Mailbox
	// StopFreeGB is the stop floor, in gigabytes of free space; zero turns the guard off.
	StopFreeGB int64
	Host       string
	Now        func() time.Time
	Out        io.Writer
}

// DiskGuardResult is what one tick of the guard did.
type DiskGuardResult struct {
	// Frozen is whether the guard's freeze stands after the tick, Lifted whether the
	// tick lifted it. Freed is what the sweep and the tidy said, when they ran.
	Frozen, Lifted bool
	Freed          []string
}

func (g DiskGuard) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now() //mw:realclock the default of the injected Now, used where none is set
}

func (g DiskGuard) say(format string, args ...any) {
	if g.Out != nil {
		fmt.Fprintf(g.Out, format+"\n", args...)
	}
}

func (g DiskGuard) by() string { return "mw@" + g.Host + " (disk guard)" }

// diskSpace is one free space the guard reads: the root filesystem's, or the
// Windows drive's of a WSL host.
type diskSpace struct {
	name string
	free int64
}

func (s diskSpace) String() string {
	return fmt.Sprintf("%s has %.1f GB free", s.name, float64(s.free)/(1<<30))
}

// spaces are the free spaces a reading knows. A host the reading does not know
// anything of is held to nothing.
func (r LoadReading) diskSpaces() []diskSpace {
	var spaces []diskSpace
	if r.RootKnown {
		spaces = append(spaces, diskSpace{"root", r.RootFreeBytes})
	}
	if r.WSL && r.DriveKnown {
		name := r.DriveName
		if name == "" {
			name = "the Windows drive"
		}
		spaces = append(spaces, diskSpace{name, r.DriveFreeBytes})
	}
	return spaces
}

func (g DiskGuard) under(spaces []diskSpace, gb int64) bool {
	for _, s := range spaces {
		if s.free < gb<<30 {
			return true
		}
	}
	return false
}

func (g DiskGuard) said(spaces []diskSpace) string {
	var parts []string
	for _, s := range spaces {
		parts = append(parts, s.String())
	}
	return strings.Join(parts, ", ")
}

// Run is one tick of the guard, made before the dispatch starts anything.
func (g DiskGuard) Run(ctx context.Context) (DiskGuardResult, error) {
	var result DiskGuardResult
	if g.Lock != nil {
		release, taken, err := g.Lock.TryTake(ctx)
		if err != nil {
			return result, fmt.Errorf("taking the disk guard's lock: %w", err)
		}
		if !taken {
			return result, nil
		}
		defer release()
	}
	way := g.Way
	// The guard freezes the home's Builders too: a full disk stops the whole factory.
	way.Home = nil
	way.By = g.by()
	marker, there, err := way.Marker.Read(ctx)
	if err != nil {
		return result, fmt.Errorf("reading the give-way marker: %w", err)
	}
	if there && marker.Disk == nil {
		// A person at the screen gives way: it is hers to lift.
		return result, nil
	}
	if g.StopFreeGB <= 0 {
		if there {
			return g.lift(ctx, way, result, "stop_free_gb is 0")
		}
		return result, nil
	}
	reading, err := g.Load.Load(ctx)
	if err != nil {
		reading = LoadReading{}
	}
	spaces := reading.diskSpaces()
	if len(spaces) == 0 && !there {
		return result, nil
	}
	lifted := g.StopFreeGB + DiskGuardLiftMarginGB
	if !there && !g.under(spaces, g.StopFreeGB) {
		return result, nil
	}
	if there && len(spaces) > 0 && !g.under(spaces, lifted) {
		return g.lift(ctx, way, result, g.said(spaces))
	}

	before := g.said(spaces)
	if err := way.On(ctx, GiveWayRequest{Disk: true, Notes: map[string]string{"disk": before}}); err != nil {
		return result, fmt.Errorf("freezing the Builders for the disk: %w", err)
	}
	result.Frozen = true
	if !there {
		g.say("%s is frozen for its disk: %s, under the %d GB stop floor", g.Host, before, g.StopFreeGB)
	}
	marker, _, err = way.Marker.Read(ctx)
	if err != nil {
		return result, fmt.Errorf("reading the give-way marker: %w", err)
	}
	if marker.Disk == nil {
		return result, nil
	}
	if g.Free != nil && g.now().Sub(marker.Disk.FreedAt) >= DiskGuardFreeEvery {
		result.Freed = g.Free.FreeUp(ctx)
		marker.Disk.FreedAt = g.now()
		if err := way.Marker.Write(ctx, marker); err != nil {
			return result, fmt.Errorf("writing the give-way marker: %w", err)
		}
		if after, err := g.Load.Load(ctx); err == nil && len(after.diskSpaces()) > 0 {
			spaces = after.diskSpaces()
		}
		if !g.under(spaces, lifted) {
			g.mail(ctx, before, spaces, result.Freed, false)
			out, err := g.lift(ctx, way, result, g.said(spaces))
			return out, err
		}
	}
	if !marker.Disk.Mailed {
		if g.mail(ctx, before, spaces, result.Freed, true) {
			marker.Disk.Mailed = true
			if err := way.Marker.Write(ctx, marker); err != nil {
				return result, fmt.Errorf("writing the give-way marker: %w", err)
			}
		}
	}
	return result, nil
}

// lift lets the Builders go on, as give way's off does.
func (g DiskGuard) lift(ctx context.Context, way GiveWay, result DiskGuardResult, why string) (DiskGuardResult, error) {
	if err := way.Off(ctx, GiveWayRequest{Notes: map[string]string{"disk": why}}); err != nil {
		return result, fmt.Errorf("letting the Builders go on: %w", err)
	}
	result.Frozen, result.Lifted = false, true
	g.say("%s goes on: %s, over the stop floor of %d GB and %d GB more", g.Host, why, g.StopFreeGB, DiskGuardLiftMarginGB)
	return result, nil
}

// mail tells the Mayor, once an episode, and reports whether it was sent. stays
// says the host is frozen still, and then the mail names what holds the space.
func (g DiskGuard) mail(ctx context.Context, before string, after []diskSpace, freed []string, stays bool) bool {
	if g.Mailbox == nil {
		return false
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s froze its Builders for its disk: %s, under the stop floor of %d GB (stop_free_gb).\n\n", g.Host, before, g.StopFreeGB)
	b.WriteString("Frozen where they stand: nothing killed, nothing lost, every heartbeat going on.\n\n")
	if len(freed) == 0 {
		b.WriteString("Freed: nothing was run to free space.\n")
	} else {
		b.WriteString("Freed:\n")
		for _, line := range freed {
			fmt.Fprintf(&b, "  %s\n", line)
		}
	}
	fmt.Fprintf(&b, "\nNow: %s.\n", g.said(after))
	if !stays {
		fmt.Fprintf(&b, "That is back over the floor and %d GB more: the Builders go on.\n", DiskGuardLiftMarginGB)
	} else {
		fmt.Fprintf(&b, "\nThe sweep and the tidy could not get it back over %d GB: the host stays frozen until it is "+
			"(`mw giveway off` lets it go on at once; the dispatch starts nothing while it is frozen).\n", g.StopFreeGB+DiskGuardLiftMarginGB)
		b.WriteString(g.holders(ctx))
	}
	subject := fmt.Sprintf("%s: %s froze its Builders for its disk", MailBlocked, g.Host)
	if !stays {
		subject = fmt.Sprintf("%s: %s froze its Builders for its disk and freed it", MailBlocked, g.Host)
	}
	if _, err := g.Mailbox.Send(ctx, NewMessage{From: SeatIdentity(MwSeat, g.Host), To: MayorMailbox, Subject: subject, Body: b.String()}); err != nil {
		g.say("the mail to %s that %s is frozen for its disk could not be sent: %v", MayorMailbox, g.Host, err)
		return false
	}
	return true
}

// holders says what holds the space under the home directory.
func (g DiskGuard) holders(ctx context.Context) string {
	if g.Holders == nil {
		return ""
	}
	held, err := g.Holders.Largest(ctx, DiskGuardHolders)
	if err != nil {
		return fmt.Sprintf("\nWhat holds the space could not be read: %s\n", oneLine(err.Error()))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\nThe %d largest directories under the home directory:\n", DiskGuardHolders)
	for _, h := range held {
		fmt.Fprintf(&b, "  %.1f GB  %s\n", float64(h.Bytes)/(1<<30), h.Path)
	}
	return b.String()
}
