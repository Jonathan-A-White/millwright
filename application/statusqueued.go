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
	// TooLong says the story has waited past QueuedTooLongFactor times its
	// rig's batch_every with no batch running.
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
	case !w.InBatchSince.IsZero():
		return []string{since + ", in the batch running since " + w.InBatchSince.UTC().Format(QueuedClock)}
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
	var queued []StoryDetail
	for _, d := range work.Running {
		if QueuedForBatch(d) {
			queued = append(queued, d)
		}
	}
	if len(queued) == 0 {
		return nil
	}
	jobs := s.batchJobsRunning(ctx)
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
		w := BatchWait{Since: d.QueuedAt, InBatchSince: jobs[rig], Every: every[rig]}
		w.TooLong = w.InBatchSince.IsZero() && !w.Since.IsZero() && now.Sub(w.Since) > QueuedTooLongFactor*w.Every
		waits[d.Story.ID] = w
	}
	return waits
}

// batchRigNamed picks the rigs out of the reason a batch-land job was
// scheduled for: "landing of lampas (3 queued), millwright (1 queued, one at a
// time)".
var batchRigNamed = regexp.MustCompile(`([^\s,]+) \(\d+ queued`)

// batchJobsRunning is, for each rig, when a batch-land job that names it began
// running, read off the events of the log: a job whose newest event says
// running is running now. The whole log is read, not its newest few: a batch
// runs for as long as its gate takes, and the factory writes events faster
// than that, so its running event falls out of any fixed window (mw-gq6.652).
// The log keeps only what a reader has yet to pass, so this is not the archive.
// A log that is absent or cannot be read names none.
func (s Status) batchJobsRunning(ctx context.Context) map[string]time.Time {
	if s.Log == nil {
		return nil
	}
	evs, err := s.Log.Since(ctx, 0)
	if err != nil {
		return nil
	}
	type job struct {
		rigs    []string
		state   string
		running time.Time
	}
	byActor := map[string]*job{}
	for _, e := range evs {
		if e.Kind != events.KindJob || !strings.HasPrefix(e.Actor, BatchLandJobName+"@") {
			continue
		}
		j := byActor[e.Actor]
		if j == nil {
			j = &job{}
			byActor[e.Actor] = j
		}
		j.state = e.To
		switch e.To {
		case events.JobScheduled:
			j.rigs = nil
			for _, m := range batchRigNamed.FindAllStringSubmatch(e.Detail, -1) {
				j.rigs = append(j.rigs, m[1])
			}
		case events.JobRunning:
			j.running = e.Ts
		}
	}
	out := map[string]time.Time{}
	for _, j := range byActor {
		if j.state != events.JobRunning {
			continue
		}
		for _, rig := range j.rigs {
			if at, had := out[rig]; !had || j.running.Before(at) {
				out[rig] = j.running
			}
		}
	}
	return out
}
