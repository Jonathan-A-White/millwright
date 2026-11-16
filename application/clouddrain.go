package application

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ReasonDrained is the reason word of the hold a drained cloud puts on each of
// its boxes: the Governor drained the cloud, the harness stopped nothing.
const ReasonDrained Reason = "drained"

// DrainedKey is the note a cloud box's drained hold is kept under, the time the
// cloud was drained and the words. It is the box's own note, apart from
// HarnessHoldKey: a harness stop and a drain neither set nor clear each other.
func DrainedKey(host string) string { return "host." + host + ".drained" }

// DrainedBoxesKey is the note the home keeps the names of the boxes it held in
// a drain under, so that undrain clears the hold of a box destroyed idle since.
const DrainedBoxesKey = "cloud.drained"

// drainedUntil is the time a drained hold lasts until: undrain ends it, not a clock.
var drainedUntil = time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)

// drainedSaid is what a drained hold says.
const drainedSaid = "the cloud is drained by the Governor"

type noteReader interface {
	Note(ctx context.Context, key string) (string, error)
}

// ReadDrainedHold reads the drained hold in the host's note: a hold that lasts
// until undrain.
func ReadDrainedHold(ctx context.Context, notes noteReader, host string) (HarnessHold, bool) {
	if notes == nil {
		return HarnessHold{}, false
	}
	value, err := notes.Note(ctx, DrainedKey(host))
	if err != nil || strings.TrimSpace(value) == "" {
		return HarnessHold{}, false
	}
	return HarnessHold{Until: drainedUntil, Reason: ReasonDrained, Said: drainedSaid}, true
}

// ReadHostHold is the hold on the host's new claims: the drained hold if there is one, else
// the harness stop's while its time has not come.
func ReadHostHold(ctx context.Context, notes noteReader, host string, now time.Time) (HarnessHold, bool) {
	if hold, held := ReadDrainedHold(ctx, notes, host); held {
		return hold, true
	}
	if hold, held := readHarnessHold(ctx, notes, host); held && hold.Until.After(now) {
		return hold, true
	}
	return HarnessHold{}, false
}

// drained says the hold is a drained cloud's, not a harness stop's.
func (h HarnessHold) drained() bool { return h.Reason == ReasonDrained }

// CloudDrainFiles is where max_boxes is kept: the [cloud] table of the home's
// ~/.config/mw/config.toml and its copy in the vault (hosts/vultr/cloud-table.toml).
type CloudDrainFiles interface {
	// MaxBoxesKept is the max_boxes the home's [cloud] table says, the value a
	// drain kept beside it, and whether one did.
	MaxBoxesKept(ctx context.Context) (maxBoxes, kept int, drained bool, err error)
	// SetDrained writes max_boxes 0 and the old value kept in both files, the
	// home's config last, and commits the vault's by path.
	SetDrained(ctx context.Context, old int) error
	// SetUndrained writes max_boxes back at the kept value in both files and
	// takes the kept value away, in the same order.
	SetUndrained(ctx context.Context, back int) error
}

// CloudDrain is mw cloud drain and mw cloud undrain. A drain stops the cloud
// taking more work: max_boxes 0 makes no box, and each box up claims nothing
// new, while the sessions it runs finish and an idle box is destroyed at
// idle_minutes as ever. It reaches no box. It is for the home only.
type CloudDrain struct {
	Files CloudDrainFiles
	Book  CloudBook
	Notes SweepNotes
	Home  HomeFile
	Host  string
	Now   func() time.Time
}

// CloudDrainReport is what a drain or an undrain changed.
type CloudDrainReport struct {
	Undrain bool
	// Was and Now are max_boxes before and after.
	Was, Now int
	// Boxes are the boxes whose drained hold was set or cleared.
	Boxes []string
	// Already is a drain that found the cloud drained, or an undrain that found it not.
	Already bool
}

// zeroKeptSaid is said when the value a drain keeps is 0, as a drain of a hand
// drain's max_boxes 0 does: undrain would put 0 back, and the old value must be said.
const zeroKeptSaid = "max_boxes was already 0, so undrain leaves it at 0. To say the old value, set drained_max_boxes = <the old value>\n" +
	"beside max_boxes in the [cloud] table of the home's ~/.config/mw/config.toml and of the vault's hosts/vultr/cloud-table.toml, then run mw cloud undrain."

// undrainZeroSaid is said when an undrain put 0 back: the drain had kept no value.
const undrainZeroSaid = "The value the drain kept was 0, so max_boxes stays 0 and no box is made. Set max_boxes in the [cloud] table of\n" +
	"the home's ~/.config/mw/config.toml and of the vault's hosts/vultr/cloud-table.toml; a later drain keeps it as drained_max_boxes."

// String is the report, with the way back.
func (r CloudDrainReport) String() string {
	text := r.say()
	if r.zeroKept() {
		if r.Undrain {
			text += "\n" + undrainZeroSaid
		} else {
			text += "\n" + zeroKeptSaid
		}
	}
	return text
}

// zeroKept is a drain that keeps 0, or an undrain that puts 0 back: the old value was never said.
func (r CloudDrainReport) zeroKept() bool {
	if r.Undrain {
		return !r.Already && r.Now == 0
	}
	return r.Was == 0
}

func (r CloudDrainReport) say() string {
	boxes := "no box is up"
	if len(r.Boxes) > 0 {
		boxes = strings.Join(r.Boxes, ", ")
	}
	switch {
	case r.Undrain && r.Already:
		return "The cloud is not drained: nothing changed."
	case r.Undrain:
		return fmt.Sprintf("max_boxes is %d (was %d): written in the home's [cloud] table and the vault's hosts/vultr/cloud-table.toml.\n"+
			"Drained hold cleared: %s.\n"+
			"Way back: mw cloud drain", r.Now, r.Was, boxes)
	case r.Already:
		return fmt.Sprintf("The cloud was already drained: max_boxes is 0, the first old value, %d, is kept.\n"+
			"Held: %s.\nWay back: mw cloud undrain (max_boxes %d)", r.Was, boxes, r.Was)
	}
	return fmt.Sprintf("max_boxes is 0 (was %d): written in the home's [cloud] table and the vault's hosts/vultr/cloud-table.toml, the old value kept beside it.\n"+
		"Held, claiming nothing new: %s. Running sessions finish; an idle box is destroyed at idle_minutes.\n"+
		"Way back: mw cloud undrain (max_boxes %d)", r.Was, boxes, r.Was)
}

func (c CloudDrain) home(ctx context.Context) error {
	home, err := IsHome(ctx, c.Home, c.Host)
	if err != nil {
		return err
	}
	if !home {
		record, _ := WhereIsHome(ctx, c.Home)
		return &HomeElsewhere{Home: record.Host, Host: c.Host}
	}
	return nil
}

// now is the time of the hold; a drain with no clock holds at the zero time.
func (c CloudDrain) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Time{}
}

// heldBoxes are the boxes the home kept a hold for in an earlier drain.
func (c CloudDrain) heldBoxes(ctx context.Context) []string {
	value, err := c.Notes.Note(ctx, DrainedBoxesKey)
	if err != nil {
		return nil
	}
	return strings.Fields(value)
}

func (c CloudDrain) upBoxes(ctx context.Context) ([]string, error) {
	state, err := c.Book.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading the cloud's book: %w", err)
	}
	var names []string
	for _, box := range state.Boxes {
		names = append(names, box.Name)
	}
	return names, nil
}

func union(lists ...[]string) []string {
	var out []string
	seen := map[string]bool{}
	for _, list := range lists {
		for _, name := range list {
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	return out
}

// Drain sets max_boxes to 0 keeping the old value, and puts each box up on a
// drained hold. A drain of a cloud already drained keeps the first old value and
// holds the boxes not yet held. On a host that is not home it changes nothing.
func (c CloudDrain) Drain(ctx context.Context) (CloudDrainReport, error) {
	if err := c.home(ctx); err != nil {
		return CloudDrainReport{}, err
	}
	maxBoxes, kept, drained, err := c.Files.MaxBoxesKept(ctx)
	if err != nil {
		return CloudDrainReport{}, err
	}
	old := maxBoxes
	if drained {
		old = kept
	} else if err := c.Files.SetDrained(ctx, old); err != nil {
		return CloudDrainReport{}, err
	}
	report := CloudDrainReport{Was: old, Already: drained}
	up, err := c.upBoxes(ctx)
	if err != nil {
		return report, fmt.Errorf("max_boxes is 0 but no box was held: %w", err)
	}
	all := union(c.heldBoxes(ctx), up)
	if err := c.Notes.SetNote(ctx, DrainedBoxesKey, strings.Join(all, " ")); err != nil {
		return report, fmt.Errorf("max_boxes is 0 but the boxes held could not be written down: %w", err)
	}
	value := c.now().UTC().Format(time.RFC3339) + " " + drainedSaid
	for _, name := range up {
		if kept, err := c.Notes.Note(ctx, DrainedKey(name)); err == nil && strings.TrimSpace(kept) != "" {
			report.Boxes = append(report.Boxes, name)
			continue
		}
		if err := c.Notes.SetNote(ctx, DrainedKey(name), value); err != nil {
			return report, fmt.Errorf("max_boxes is 0 but %s could not be held (run mw cloud drain again): %w", name, err)
		}
		report.Boxes = append(report.Boxes, name)
	}
	return report, nil
}

// Undrain puts max_boxes back at the kept value and clears every drained hold.
// A cloud that is not drained is left as it is, and the report says so.
func (c CloudDrain) Undrain(ctx context.Context) (CloudDrainReport, error) {
	if err := c.home(ctx); err != nil {
		return CloudDrainReport{}, err
	}
	maxBoxes, kept, drained, err := c.Files.MaxBoxesKept(ctx)
	if err != nil {
		return CloudDrainReport{}, err
	}
	up, err := c.upBoxes(ctx)
	if err != nil {
		return CloudDrainReport{}, err
	}
	names := union(c.heldBoxes(ctx), up)
	var held []string
	for _, name := range names {
		if value, err := c.Notes.Note(ctx, DrainedKey(name)); err == nil && strings.TrimSpace(value) != "" {
			held = append(held, name)
		}
	}
	if !drained && len(held) == 0 {
		return CloudDrainReport{Undrain: true, Already: true, Was: maxBoxes, Now: maxBoxes}, nil
	}
	report := CloudDrainReport{Undrain: true, Was: maxBoxes, Now: maxBoxes, Boxes: held}
	if drained {
		if err := c.Files.SetUndrained(ctx, kept); err != nil {
			return CloudDrainReport{}, err
		}
		report.Now = kept
	}
	for _, name := range held {
		if err := c.Notes.ClearNote(ctx, DrainedKey(name)); err != nil {
			return report, fmt.Errorf("max_boxes is back but the hold of %s could not be cleared (run mw cloud undrain again): %w", name, err)
		}
	}
	if err := c.Notes.ClearNote(ctx, DrainedBoxesKey); err != nil {
		return report, fmt.Errorf("max_boxes is back and the holds are cleared, but the list of held boxes could not be: %w", err)
	}
	return report, nil
}
