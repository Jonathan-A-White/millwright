package application

import (
	"context"
	"fmt"
	"slices"
)

// A rig held on main's red has one landing no wait on that red may keep back: the
// open bug that carries it, once its fix is queued on the rig. Its landing is
// what moves main's head, so waiting for the head (mw-gq6.1704) or running the
// hold's tests on main alone before it (mw-gq6.1706) left lampas's carrier
// mw-y1k16cd queued, with 17 stories behind it, from 16:25Z on 24 November
// 2026; and a batch of the oldest six (batch_max) never held it, queued last
// (mw-gq6.1708). It lands first and alone, and its own gate runs the rig's tests,
// which prove its fix.

// redCarrierIn is the carrier of the hold line m records when it is among ids,
// the stories queued, and its landing is worth trying: a hold whose own batch
// was the carrier alone says its fix did not mend main's red, so it waits for
// main's head as before. A batch that held the carrier with other stories says
// nothing of it: any of them may have made the red, and the carrier was never
// tried alone, so it is landed alone and not left for the Mayor (batch 694 of
// lampas held mw-fnvjof.17.290 with five others, and the follower did not ask
// again for an hour, mw-gq6.1749).
func (m BatchMetric) redCarrierIn(ids []string) (string, bool) {
	if m.MainBug == "" || !slices.Contains(ids, m.MainBug) || slices.Equal(m.IDs, []string{m.MainBug}) {
		return "", false
	}
	return m.MainBug, true
}

// redCarrierDue says what a rig whose last batch was held on main's red does at
// a pass: waits is true when the rig is left be (main's head is where the batch
// held and the carrier is not queued), and carrier is the open carrier queued
// on it, to be landed alone, with main's head unchanged. A head that has moved
// is the old rule: the whole queue lands, the carrier first (leaveCarrierFirst).
func (b *BatchLandings) redCarrierDue(ctx context.Context, rig string, lines []BatchMetric, q batchQueue) (carrier string, waits bool) {
	if !b.waitsOnMain(ctx, rig, lines) {
		return "", false
	}
	held, _ := HeldOnMainRed(lines)
	if carrier, ok := held.redCarrierIn(q.ids); ok {
		return carrier, false
	}
	return "", true
}

// landCarrier lands the carrier of main's red alone, on a host with room when
// the home is busy: mw land <rig> --only <carrier>. A follower with no scoped
// landing lands the queue, which takes the carrier first and alone.
func (b *BatchLandings) landCarrier(ctx context.Context, d batchDue) error {
	if b.LandScoped == nil {
		return b.landWhole(ctx, d)
	}
	return b.landScoped(ctx, d, LandScope{Only: []string{d.carrier}})
}

// leaveCarrierFirst puts the carrier of main's red, when it is queued for the
// rig's branch of the hold, ahead of the stories queued before it, and with no
// --only leaves nothing in the queue but it: a batch of the oldest stories would
// leave it out under batch_max, and one that held it with the rest would be one
// more gate on a red the carrier's own landing ends.
func (b BatchLand) leaveCarrierFirst(ctx context.Context, rig string, queue []BatchStory, report *BatchReport) []BatchStory {
	if b.Metrics == nil || len(queue) < 1 {
		return queue
	}
	held, ok := HeldOnMainRed(rigBatches(readBatches(ctx, b.Metrics), rig))
	if !ok {
		return queue
	}
	carrier, ok := held.redCarrierIn(storyIDs(queue))
	if !ok {
		return queue
	}
	at := slices.IndexFunc(queue, func(s BatchStory) bool { return s.ID == carrier })
	if held.MainBranch != "" && queue[at].path.Branch != held.MainBranch {
		return queue
	}
	first := queue[at]
	rest := slices.Concat(queue[:at:at], queue[at+1:])
	if len(b.Only) == 0 {
		if len(rest) > 0 {
			report.Notes = append(report.Notes, fmt.Sprintf("%s carries main's red as %s held it, so it lands first and alone: the %d queued with it wait for the next batch", carrier, BatchLabel(rig, held.Batch), len(rest)))
		}
		return []BatchStory{first}
	}
	report.Notes = append(report.Notes, fmt.Sprintf("%s carries main's red as %s held it, so it is put first in the batch", carrier, BatchLabel(rig, held.Batch)))
	return slices.Concat([]BatchStory{first}, rest)
}
