package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain/events"
)

// CloudHolds is where the cloud reads and clears the hold notes kept under a
// box's name (HarnessHoldKey, DrainedKey). A box takes the name of one destroyed
// before it, and the note of a hold outlives the box that earned it
// (mw-gq6.1394). The tracker's notes satisfy it. Nil reads none.
type CloudHolds interface {
	Note(ctx context.Context, key string) (string, error)
	ClearNote(ctx context.Context, key string) error
}

// reasonNamesGovernor says a hold's reason is the Governor's own word: a hold
// he set, which no box and no harness stop earned.
func reasonNamesGovernor(reason Reason) bool {
	return strings.Contains(strings.ToLower(string(reason)), "governor")
}

// standingHolds is called as a box of name is made and again as it comes up. It
// says each hold a note still keeps under the name, in a move and a cloud
// event, and clears the one a harness stop of an older box left: that box is
// gone, and the new one would claim nothing and be billed idle. A hold whose
// reason names the Governor, and a drained hold, are his: kept, and said with
// the command that clears it. A hold that cannot be read or cleared is said.
func (p *cloudPass) standingHolds(ctx context.Context, name string) {
	p.standingPause(ctx, name)
	if p.check.Holds == nil {
		return
	}
	if hold, held := ReadDrainedHold(ctx, p.check.Holds, name); held {
		p.move(ctx, fmt.Sprintf("%s: the cloud's drain stands on this name and is kept (%s): the box claims nothing new until mw cloud undrain", name, hold.Said))
	}
	hold, held := readHarnessHold(ctx, p.check.Holds, name)
	if !held {
		return
	}
	until := hold.Until.UTC().Format(time.RFC3339)
	if reasonNamesGovernor(hold.Reason) {
		p.move(ctx, fmt.Sprintf("%s: the Governor's hold stands on this name and is kept: claims nothing new until %s (%s %s); a box under it is billed idle, and bd kv clear %s ends it",
			name, until, hold.Reason, oneLine(hold.Said), HarnessHoldKey(name)))
		return
	}
	if err := p.check.Holds.ClearNote(ctx, HarnessHoldKey(name)); err != nil {
		p.move(ctx, fmt.Sprintf("%s: a harness hold an older box of that name left stands and could not be cleared (%v): the box claims nothing new until %s unless bd kv clear %s ends it",
			name, oneLine(err.Error()), until, HarnessHoldKey(name)))
		return
	}
	p.move(ctx, fmt.Sprintf("%s: cleared the harness hold an older box of that name left (%s, claims nothing new until %s: %s)",
		name, hold.Reason, until, oneLine(hold.Said)))
}

// askedFor is when the box of name was asked for: its Up in the book, now for
// one the book does not hold yet.
func (p *cloudPass) askedFor(name string) time.Time {
	for _, box := range p.state.Boxes {
		if box.Name == name && !box.Up.IsZero() {
			return box.Up
		}
	}
	return p.now
}

// standingPause ends a pause-host an older box of the name left (mw-gq6.1435).
// A pause is the host's own word, and a box that takes the name of one destroyed
// is a new host: a pause from before the box was asked for was not meant for it,
// and would stop it claiming anything for as long as the event stands, billed
// idle. The note the follower mirrored is cleared and a resume-host is written
// for the name, so that the log, which a dispatch reads too, says it. A pause
// set since the box was asked for is the Governor's for this box and stands.
func (p *cloudPass) standingPause(ctx context.Context, name string) {
	asked := p.askedFor(name)
	var stale []string
	if p.check.Log != nil {
		pause, paused, err := PausedHost(ctx, p.check.Log, name)
		switch {
		case err != nil:
			p.check.say("the pause of %s could not be read from the event log: %v", name, oneLine(err.Error()))
		case paused && pause.At.Before(asked):
			_, err := EventEmit{
				Log: p.check.Log,
				Now: func() time.Time { return p.now },
				Event: events.Event{Kind: events.KindControl, Actor: "cloud@" + p.check.Host,
					Detail: events.Control{Word: events.ControlResumeHost, Host: name}.Detail()},
			}.Run(ctx)
			if err != nil {
				p.move(ctx, fmt.Sprintf("%s: a pause-host an older box of that name left stands in the event log and its resume-host could not be written (%v): the box claims nothing until mw events emit --kind control --detail \"resume-host %s\"",
					name, oneLine(err.Error()), name))
				break
			}
			stale = append(stale, "log")
		}
	}
	if p.check.Holds != nil {
		pause, paused, err := PausedByNote(ctx, p.check.Holds, name)
		switch {
		case err != nil:
			p.check.say("the pause note of %s could not be read: %v", name, oneLine(err.Error()))
		case paused && pause.At.Before(asked):
			if err := p.check.Holds.ClearNote(ctx, PausedKey(name)); err != nil {
				p.move(ctx, fmt.Sprintf("%s: a pause an older box of that name left stands and could not be cleared (%v): the box claims nothing until bd kv clear %s ends it",
					name, oneLine(err.Error()), PausedKey(name)))
				break
			}
			stale = append(stale, "note")
		}
	}
	if len(stale) > 0 {
		p.move(ctx, fmt.Sprintf("%s: cleared the pause an older box of that name left (%s)", name, strings.Join(stale, " and ")))
	}
}

// CloudBoxHold is a hold of the Governor's standing on the name of a box up.
type CloudBoxHold struct {
	Box   string
	Until time.Time
	Said  string
}

// ReadHolds reads, for mw status, the holds of the Governor's that stand on
// the boxes' names: a harness hold whose reason names him and whose time has not
// come. Notes that cannot be read leave none.
func (r *CloudReading) ReadHolds(ctx context.Context, notes noteReader, now time.Time) {
	if r == nil {
		return
	}
	r.Holds = nil
	if notes == nil {
		return
	}
	for _, box := range r.Boxes {
		if hold, held := readHarnessHold(ctx, notes, box.Name); held && reasonNamesGovernor(hold.Reason) && hold.Until.After(now) {
			r.Holds = append(r.Holds, CloudBoxHold{Box: box.Name, Until: hold.Until, Said: oneLine(hold.Said)})
		}
	}
}

// writeHolds is the loud lines of the CLOUD section for the Governor's holds.
func (r CloudReading) writeHolds(b *strings.Builder) {
	for _, h := range r.Holds {
		clip(b, "  HELD BY THE GOVERNOR: "+h.Box)
		clip(b, "    claims nothing new until "+h.Until.UTC().Format("2006-01-02T15:04Z"))
		clip(b, "    the box is billed idle meanwhile")
		clip(b, "    "+h.Said)
		clip(b, "    bd kv clear "+HarnessHoldKey(h.Box))
	}
}
