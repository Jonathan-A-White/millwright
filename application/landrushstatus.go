package application

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// LandRushHeading heads the section naming, for each rig whose file says
// landing = "batch", what waits to be landed and how its batches have gone
// (plans/0117, Land Rush 6). The report leaves it out when no rig lands in
// batches.
const LandRushHeading = "LAND RUSH"

// LandRushStatus is one batch rig's line under LAND RUSH.
type LandRushStatus struct {
	Rig string
	// Queued are the stories queued for the rig's next batch, on any host, and
	// Oldest how long the oldest has waited, zero when none has a queued time.
	Queued int
	Oldest time.Duration
	// Last is the rig's newest batch line, when Kept; HeldInARow are the
	// batches held one after another at the end of its history, which is what
	// the trial's stop rule counts.
	Last       BatchMetric
	Kept       bool
	HeldInARow int
	// Today says Last was made on the report's day, so that its time is told
	// without a date.
	Today bool
}

// Lines are the lines printed for the rig: what waits, then how its batches
// have gone.
func (l LandRushStatus) Lines() []string {
	queued := fmt.Sprintf("%s: %d queued", l.Rig, l.Queued)
	if l.Queued > 0 && l.Oldest > 0 {
		queued += ", oldest " + Clock(l.Oldest)
	}
	if !l.Kept {
		return []string{queued, "  no batch yet"}
	}
	when := l.Last.At.Format("2006-01-02 15:04")
	if l.Today {
		when = l.Last.At.Format("15:04")
	}
	last := fmt.Sprintf("  last batch %s: %d stories, %s", when, l.Last.Stories, l.Last.Outcome)
	if l.HeldInARow == 0 {
		return []string{queued, last + " · none held"}
	}
	return []string{queued, last + fmt.Sprintf(" · %d held in a row", l.HeldInARow)}
}

// landRushStatuses reads, for each of this host's rigs whose file says
// landing = "batch", what is queued for it and its newest batches. A rig whose
// file cannot be read is left out, as is the section when the history cannot
// be read: neither is a reason to refuse the rest of the report. Nothing is
// written.
func (s Status) landRushStatuses(ctx context.Context, work WorkInHand) []LandRushStatus {
	if s.Rules == nil || len(s.Rigs) == 0 {
		return nil
	}
	names := make([]string, 0, len(s.Rigs))
	for name := range s.Rigs {
		names = append(names, name)
	}
	sort.Strings(names)
	now := s.now()
	var history []BatchMetric
	historyRead := false
	var out []LandRushStatus
	for _, name := range names {
		rules, err := s.Rules.EpicRequirements(ctx, name)
		if err != nil || rules.LandingSettings().Mode != domain.LandingBatch {
			continue
		}
		one := LandRushStatus{Rig: name}
		for _, detail := range work.Running {
			if !QueuedForBatch(detail) {
				continue
			}
			if path, err := detail.Path(); err != nil || path.Rig != name {
				continue
			}
			one.Queued++
			if !detail.QueuedAt.IsZero() && now.Sub(detail.QueuedAt) > one.Oldest {
				one.Oldest = now.Sub(detail.QueuedAt)
			}
		}
		if s.Batches != nil && !historyRead {
			history, _ = s.Batches.Batches(ctx)
			historyRead = true
		}
		for _, batch := range history {
			if batch.Rig != name {
				continue
			}
			one.Last, one.Kept = batch, true
			if batch.Outcome == BatchHeld {
				one.HeldInARow++
			} else {
				one.HeldInARow = 0
			}
		}
		if one.Kept {
			y, m, d := now.UTC().Date()
			ly, lm, ld := one.Last.At.UTC().Date()
			one.Today = y == ly && m == lm && d == ld
			one.Last.At = one.Last.At.UTC()
		}
		out = append(out, one)
	}
	return out
}

// writeLandRush prints the LAND RUSH section: a line for each batch rig, left
// out when the report holds none.
func (r StatusReport) writeLandRush(b *strings.Builder) {
	if len(r.LandRush) == 0 {
		return
	}
	clip(b, LandRushHeading)
	for _, one := range r.LandRush {
		for _, line := range one.Lines() {
			clip(b, "  "+line)
		}
	}
	b.WriteString("\n")
}
