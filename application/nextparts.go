package application

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// partsAtTip is what the part records of a story's attempt say of the commit at
// its branch's tip, read by a close-out of a rig that lands in batches when no
// whole check passed there (mw-6ww.119.11.1.1): a part's record names a tip only
// when the worktree was clean at it, so a part that passed at this tip passed the
// very commands the close-out would run, on the same tree. Of a rig whose gate is
// not split into parts, or one whose records say nothing of the tip, it is nil.
type partsAtTip struct {
	tip string
	// names are the rig's parts as this host names them, in order; passed are the
	// numbers (from 1) of those whose record is a pass at tip.
	names  []string
	passed []int
	// specs says the changed specs' record is a pass at tip.
	specs bool
	// skipped are the numbers of the parts the close-out's gate did not run
	// because they passed at tip: what its ledger line says it trusted.
	skipped []int
}

// has says part n passed at the tip.
func (p *partsAtTip) has(n int) bool {
	return p != nil && slices.Contains(p.passed, n)
}

// all says every part passed at the tip, so that the close-out runs none.
func (p *partsAtTip) all() bool {
	return p != nil && len(p.names) > 0 && len(p.passed) == len(p.names)
}

// skip says the gate does not run part n: it passed at the tip, and the gate is
// the one on the worktree at the tip (the run on a merged result is another tree).
// It notes the part as skipped.
func (p *partsAtTip) skip(onTheWorktree bool, n int) bool {
	if !onTheWorktree || !p.has(n) {
		return false
	}
	if !slices.Contains(p.skipped, n) {
		p.skipped = append(p.skipped, n)
	}
	return true
}

// readPartsAtTip reads the part records of the close-out's attempt against tip.
// A close-out with no way to read them, a gate not split into parts and a tip no
// part passed at read none.
func (n Next) readPartsAtTip(ctx context.Context, c *closeOut, tip string) *partsAtTip {
	if n.GateParts == nil || n.GateRecords == nil {
		return nil
	}
	parts := n.GateParts.Parts(c.path.Rig)
	if len(parts) == 0 {
		return nil
	}
	gate := recordedGate{records: n.GateRecords, storyID: c.id, attempt: c.attempt(), parts: parts, tip: tip}
	p := &partsAtTip{tip: tip}
	for i, part := range parts {
		p.names = append(p.names, part.Name)
		if gate.stateOf(ctx, i+1) == partPassed {
			p.passed = append(p.passed, i+1)
		}
	}
	if len(p.passed) == 0 {
		return nil
	}
	rec, ok := specsRecordIn(ctx, n.GateRecords, c.id, c.attempt())
	p.specs = ok && rec.Tip == tip && rec.Result == specsPassed
	return p
}

// specsRecordIn reads what the changed specs of a story's attempt last said,
// false when nothing was kept.
func specsRecordIn(ctx context.Context, records CheckRecords, storyID string, attempt int) (specsRecord, bool) {
	text, err := records.ReadRunFile(ctx, storyID, CheckSpecsResultFileName(attempt))
	if err != nil {
		return specsRecord{}, false
	}
	var rec specsRecord
	if json.Unmarshal([]byte(text), &rec) != nil {
		return specsRecord{}, false
	}
	return rec, true
}

// trustedPartsRefusal is the rest of the close-out of a story whose every part
// passed at its tip: no part is run, and the changed specs are, unless their
// record is a pass at the tip. It says whether the story is refused, and why.
func (n Next) trustedPartsRefusal(ctx context.Context, c *closeOut, report *NextReport, base string) (Refusal, bool) {
	c.parts.skipped = slices.Clone(c.parts.passed)
	if c.parts.specs || n.SpecChecks == nil {
		return Refusal{}, false
	}
	return n.specsRefusal(ctx, c, report, c.worktree, base, "in the worktree", n.specsFailReason(ctx, c), true)
}

// tested is what a queued story's ledger line says of how it was tested when the
// gate trusted the parts that passed at the tip: empty when it trusted none. It
// names the tip the parts passed at, which is not the branch's once the queue has
// merged origin's copy of it in.
func (p *partsAtTip) tested() string {
	if p == nil || len(p.skipped) == 0 {
		return ""
	}
	short := shortCommit(p.tip)
	if len(p.skipped) == len(p.names) {
		specs := "changed specs run at close-out"
		if p.specs {
			specs = "changed specs passed at " + short
		}
		return fmt.Sprintf("tested by mw check, parts at %s: pass; %s", short, specs)
	}
	var trusted, ran []string
	for i, name := range p.names {
		if slices.Contains(p.skipped, i+1) {
			trusted = append(trusted, strconv.Itoa(i+1))
		} else {
			ran = append(ran, fmt.Sprintf("%d (%s)", i+1, name))
		}
	}
	noun := "part"
	if len(ran) > 1 {
		noun = "parts"
	}
	return fmt.Sprintf("tested by mw check, parts %s of %d at %s: pass; %s %s run at close-out: pass",
		strings.Join(trusted, ", "), len(p.names), short, noun, strings.Join(ran, ", "))
}

// specsPassed says the changed specs' record is a pass at the tip, so that the
// close-out does not run them again.
func (p *partsAtTip) specsPassed() bool { return p != nil && p.specs }
