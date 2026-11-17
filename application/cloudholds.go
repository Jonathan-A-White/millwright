package application

import (
	"context"
	"fmt"
	"strings"
	"time"
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
