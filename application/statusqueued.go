package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// QueuedClock is how mw status writes the time a story was queued, and the
// time a batch began: the date and the minute, in UTC.
const QueuedClock = "2006-01-02 15:04Z"

// QueuedTooLongFactor is how many times its rig's batch_every a story may wait
// in the queue, with no batch running, before mw status raises QUEUED TOO LONG.
const QueuedTooLongFactor = 2

// BatchWait is what mw status says of a claimed story queued for batch landing
// (run=queued), whose lease has run out by design: it has no session while it
// waits for its batch, so a lapsed lease is no sign of a stuck claim
// (mw-gq6.604).
type BatchWait struct {
	// Since is when mw next queued the story, zero when it recorded none.
	Since time.Time
	// InBatchSince is when the batch-land job for the story's rig began
	// running, zero when none is running.
	InBatchSince time.Time
	// Landing says an `mw land` of the story's rig is alive on this host, though
	// no batch-land job event names the rig: the job names the rigs it was
	// scheduled for, and a pass lands a rig that fell due after (mw-gq6.858).
	Landing bool
	// GateHeldBy is whose gate holds the host's gate lock while the rig's lander
	// waits for it, empty when it does not wait.
	GateHeldBy string
	// TooLong says the story has waited past QueuedTooLongFactor times its
	// rig's batch_every with no batch running or landing.
	TooLong bool
	// Every is the rig's batch_every, the clock TooLong is judged by.
	Every time.Duration
}

// Lines are the lines printed under the story.
func (w BatchWait) Lines() []string {
	since := "queued for batch landing"
	if !w.Since.IsZero() {
		since += " since " + w.Since.UTC().Format(QueuedClock)
	}
	switch {
	case w.GateHeldBy != "":
		return []string{since + ", its batch waiting for the gate held by " + w.GateHeldBy}
	case !w.InBatchSince.IsZero():
		return []string{since + ", in the batch running since " + w.InBatchSince.UTC().Format(QueuedClock)}
	case w.Landing:
		return []string{since + ", its batch is landing now"}
	case w.TooLong:
		return []string{
			fmt.Sprintf("QUEUED TOO LONG: waiting past twice batch_every (%s) with no batch running", Clock(w.Every)),
			since,
		}
	}
	return []string{since + ": no session until its batch"}
}

// write prints the wait, indented.
func (w BatchWait) write(b *strings.Builder, pad string) {
	for _, line := range w.Lines() {
		wrapInto(b, pad, line)
	}
}

// batchWaits reads, for each claimed story in hand that is queued for batch
// landing on any host, what it waits for: since when, whether its rig's batch
// is running, whether it has waited too long. A rig whose file cannot be read
// is judged on its default batch_every; nothing here fails the report.
func (s Status) batchWaits(ctx context.Context, work WorkInHand) map[string]BatchWait {
	queued := work.Queued()
	if len(queued) == 0 {
		return nil
	}
	jobs := s.batchJobsRunning(ctx)
	landing := s.landingRigs(ctx)
	holder := ""
	if s.GateHolder != nil && len(landing) > 0 {
		holder = s.GateHolder()
	}
	every := map[string]time.Duration{}
	now := s.now()
	waits := make(map[string]BatchWait, len(queued))
	for _, d := range queued {
		rig := d.Merged().Rig
		if _, read := every[rig]; !read {
			every[rig] = domain.DefaultBatchEvery
			if s.Rules != nil {
				if rules, err := s.Rules.EpicRequirements(ctx, rig); err == nil {
					every[rig] = rules.LandingSettings().BatchEvery
				}
			}
		}
		w := BatchWait{Since: d.QueuedAt, InBatchSince: jobs[rig], Landing: landing[rig], Every: every[rig]}
		if w.Landing && holder != "" && !ownBatch(holder, rig) {
			w.GateHeldBy = holder
		}
		w.TooLong = w.InBatchSince.IsZero() && !w.Landing && !w.Since.IsZero() && now.Sub(w.Since) > QueuedTooLongFactor*w.Every
		waits[d.Story.ID] = w
	}
	return waits
}

// landingRigs are the rigs with an `mw land` alive on this host: the landing
// itself is the sign the rig's queue is being landed, whatever the job's events
// name. A Landings that cannot be read names none.
func (s Status) landingRigs(ctx context.Context) map[string]bool {
	if s.Landings == nil {
		return nil
	}
	names, err := s.Landings.Pending(ctx)
	if err != nil {
		return nil
	}
	out := make(map[string]bool, len(names))
	for _, name := range names {
		out[name] = true
	}
	return out
}

// ownBatch says the gate lock's holder is a batch of rig itself, the gate its
// own lander is running in (BatchLabel), not another gate it waits behind.
func ownBatch(holder, rig string) bool {
	return strings.HasPrefix(holder, "batch ") && strings.HasSuffix(holder, " of "+rig)
}

// batchRigNamed picks the rigs out of the reason a batch-land job was
// scheduled for: "landing of lampas (3 queued), millwright (1 queued, one at a
// time)".
var batchRigNamed = regexp.MustCompile(`([^\s,]+) \(\d+ queued`)

// batchJobsRunning is, for each rig, when a batch-land job that names it began
// running, read off the newest events the log keeps of each job (NewestOf): a
// job whose newest event says running is running now. The log is not scanned for
// them and no window is read: a batch runs for as long as its gate takes, and
// the factory writes events faster than that, so its running event falls out of
// any fixed window (mw-gq6.652, mw-gq6.655). A log that is absent or cannot be
// read names none. The rigs are read from the newest scheduled event that says
// what the pass is for: a pass the restarted follower runs again is scheduled
// with a reason that names no rig (mw-gq6.727).
func (s Status) batchJobsRunning(ctx context.Context) map[string]time.Time {
	return batchJobsRunning(ctx, s.Log)
}

// batchJobsRunning is Status.batchJobsRunning for any log, which mw retry reads
// too (mw-gq6.862).
func batchJobsRunning(ctx context.Context, log EventLog) map[string]time.Time {
	if log == nil {
		return nil
	}
	newest, err := NewestOf(ctx, log)
	if err != nil {
		return nil
	}
	out := map[string]time.Time{}
	for actor, j := range newest.Jobs {
		if !strings.HasPrefix(actor, BatchLandJobName+"@") || j.Last.To != events.JobRunning {
			continue
		}
		for _, m := range batchRigNamed.FindAllStringSubmatch(j.Reasoned.Detail, -1) {
			if at, had := out[m[1]]; !had || j.Running.Ts.Before(at) {
				out[m[1]] = j.Running.Ts
			}
		}
	}
	return out
}
