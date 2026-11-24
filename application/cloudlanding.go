package application

import (
	"context"
	"slices"
)

// landingOn is the boxes that hold a rig's live batch claim: the box a rig's
// batch is landing on, its fixer with it, which is no story's session and so
// is not among sessions (mw-gq6.1701). A claim is read for each rig the work in
// hand names, since a batch has queued stories until it has landed them. A
// claim that cannot be read, or that has run out, is a landing no box is known
// to hold, and a check without Notes reads none.
func (p *cloudPass) landingOn(ctx context.Context, work WorkInHand) map[string]bool {
	if p.check.Notes == nil {
		return nil
	}
	var rigs []string
	for _, list := range [][]StoryDetail{work.Running, work.OpenClaimed, work.Ready, work.Held} {
		for _, d := range list {
			if rig := d.Merged().Rig; rig != "" && !d.IsEpic && !slices.Contains(rigs, rig) {
				rigs = append(rigs, rig)
			}
		}
	}
	on := map[string]bool{}
	for _, rig := range rigs {
		if host, landing := BatchLandsOn(ctx, p.check.Notes, rig, p.now); landing {
			on[host] = true
		}
	}
	return on
}
