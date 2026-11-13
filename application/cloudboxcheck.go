package application

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// A cloud box takes no story until the home has cleared it (mw-gq6.1172):
// cloud4 and cloud5 came up from the snapshot on 2026-11-12 with corrupt git
// packs, a corrupt Go module cache and EXT4-fs errors in the kernel log, and
// took stories all the same. The home's cloud check is the one judge. It clears
// a box when Vultr does not report its instance locked and the box's own check
// passes (CloudProvider.CheckBox), keeps the verdict in the book (CloudBox.
// Checked, CheckFault) and writes it into the box's gate note, which the box's
// dispatch reads before it does anything. CloudBox.MayWork is the one test of
// the book: the check's count of free sessions, the quiet alarm's and the
// starved-rig check's read of a box that is down, and mw status all use it.

// CloudBoxChecked is what a box's own check found: Faults is empty for a box
// whose disk is sound, else one line for each thing wrong, as the box said it.
type CloudBoxChecked struct {
	Faults []string
}

// CloudGate is where the cloud check writes each box's gate note, in the one
// database every host reads: the tracker's notes.
type CloudGate interface {
	SetNote(ctx context.Context, key, value string) error
}

// What a box's gate note says. A box's dispatch goes on only when the note
// begins with BoxGateOK: no note, one that cannot be read and a hold all stop
// it, so a box nobody has cleared takes nothing.
const (
	BoxGateOK   = "ok"
	BoxGateHold = "hold"
)

// BoxGateKey is the note a box's gate is kept in.
func BoxGateKey(box string) string { return "host." + box + ".box-gate" }

// CloudBoxRecheck is how long a cleared box works before the check asks it to
// check itself again: the faults of cloud4 and cloud5 showed one to three hours
// after they came up, on disks that had read well before.
const CloudBoxRecheck = time.Hour

// MayWork reports whether the box may take a story: its make is over, Vultr
// does not report it locked, and its own check has passed since. It is the one
// test every reader of the book uses.
func (b CloudBox) MayWork() bool {
	return !b.Making && !b.Restoring && b.LockedSince.IsZero() && b.CheckFault == "" && !b.Checked.IsZero()
}

// heldWhy is why the box may not work, in a phrase; "" when it may.
func (b CloudBox) heldWhy() string {
	switch {
	case b.MayWork():
		return ""
	case b.Making || b.Restoring:
		return "it is still being made"
	case !b.LockedSince.IsZero():
		return "Vultr reports it locked"
	case b.CheckFault != "":
		return "it failed its own check: " + b.CheckFault
	}
	return "its own check has not passed yet"
}

// checkBoxes clears each box whose make is over, or holds it, and keeps its
// gate note saying which. A box Vultr reports locked is not asked anything: it
// is held, and what it was cleared by is forgotten, because whatever locked it
// may have written its disk. A box not locked is asked to check itself when it
// has not passed yet, and again every CloudBoxRecheck; one that cannot be
// reached is neither passed nor failed, and is asked at the next check. A box
// that fails is held with what it found, a move and a cloud event once, and is
// not asked again: mw cloud down <box> is what ends it, and the book's alarm
// says so (MakeAlarms). When Vultr's instances could not be listed this check,
// no box is cleared on an older word of it.
func (p *cloudPass) checkBoxes(ctx context.Context) {
	for i := range p.state.Boxes {
		box := &p.state.Boxes[i]
		if box.Making || box.Restoring {
			continue
		}
		switch {
		case !box.LockedSince.IsZero():
			if !box.Checked.IsZero() || box.CheckFault != "" {
				box.Checked, box.CheckFault = time.Time{}, ""
				p.dirty = box.Name + " locked"
			}
			p.setGate(ctx, box, BoxGateHold, "Vultr reports it locked")
			continue
		case box.CheckFault != "":
			p.setGate(ctx, box, BoxGateHold, "it failed its own check: "+box.CheckFault)
			continue
		case !box.Checked.IsZero() && p.now.Sub(box.Checked) < CloudBoxRecheck:
			p.setGate(ctx, box, BoxGateOK, "")
			continue
		case !p.listed:
			p.check.say("%s is not asked to check itself: Vultr could not be asked whether it is locked", box.Name)
			continue
		}
		again := !box.Checked.IsZero()
		found, err := p.check.Provider.CheckBox(ctx, box.Name)
		if err != nil {
			// Not a verdict. A box cleared before keeps its gate until it can be
			// asked; one never cleared stays shut.
			p.check.say("%s could not be asked to check itself, so it is asked again at the next check: %v", box.Name, oneLine(err.Error()))
			if !again {
				p.setGate(ctx, box, BoxGateHold, "its own check has not passed yet")
			}
			continue
		}
		if len(found.Faults) > 0 {
			box.Checked, box.CheckFault = time.Time{}, oneLine(strings.Join(found.Faults, "; "))
			p.dirty = box.Name + " failed its check"
			p.setGate(ctx, box, BoxGateHold, "it failed its own check: "+box.CheckFault)
			p.move(ctx, fmt.Sprintf("%s failed its own check, so it takes no story: %s; to destroy it: mw cloud down %s", box.Name, box.CheckFault, box.Name))
			continue
		}
		box.Checked, box.CheckFault = p.now, ""
		p.dirty = box.Name + " checked"
		p.setGate(ctx, box, BoxGateOK, "")
		if !again {
			what := fmt.Sprintf("%s passed its own check (git fsck of the vault and each rig checkout, go mod verify, no EXT4-fs error) and Vultr does not report it locked: it may take stories", box.Name)
			p.check.say("%s", what)
			p.emit(ctx, what)
		}
	}
}

// setGate writes the box's gate note when it does not already say this, and
// keeps what it says in the book so that it is written once. A note that could
// not be written is said and tried at the next check; a check with no Gate
// writes none.
func (p *cloudPass) setGate(ctx context.Context, box *CloudBox, word, why string) {
	said := word
	if why != "" {
		said += ": " + why
	}
	if p.check.Gate == nil || box.Gate == said {
		return
	}
	value := word + " " + p.now.UTC().Format(time.RFC3339)
	if why != "" {
		value += ": " + why
	}
	if err := p.check.Gate.SetNote(ctx, BoxGateKey(box.Name), value); err != nil {
		p.check.say("the gate note of %s could not be written (%s), so it is tried at the next check: %v", box.Name, said, oneLine(err.Error()))
		return
	}
	box.Gate = said
	p.dirty = box.Name + " gate"
}

// shutGate shuts the gate of a name before a box is made under it, or after
// the box of it is destroyed, and reports whether the note says so: a note a
// destroyed box left open is not the next box's.
func (p *cloudPass) shutGate(ctx context.Context, name, why string) error {
	if p.check.Gate == nil {
		return nil
	}
	return p.check.Gate.SetNote(ctx, BoxGateKey(name), BoxGateHold+" "+p.now.UTC().Format(time.RFC3339)+": "+why)
}

// boxHeld reports why this host, a cloud box, may not dispatch: what its gate
// note says when it does not say ok. A host that is no box has no gate, and a
// dispatch with no notes to read reads none.
func (d Dispatch) boxHeld(ctx context.Context) string {
	if d.Memory == nil || !(CloudPlan{}).isBox(d.Host) {
		return ""
	}
	said, err := d.Memory.Note(ctx, BoxGateKey(d.Host))
	said = strings.TrimSpace(said)
	switch {
	case err != nil:
		return fmt.Sprintf("its gate note could not be read: %v", oneLine(err.Error()))
	case said == "":
		return "the home has not cleared it yet: its own check has not passed"
	case said == BoxGateOK || strings.HasPrefix(said, BoxGateOK+" "):
		return ""
	}
	return "the home holds it (" + said + ")"
}
