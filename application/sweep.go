package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// SweepKey is the key a close-out or a dispatch clears in the tracker's
// key-value store when a fresh attempt of a story starts, on the off chance
// an earlier version of Sweep left a note behind under it: this Sweep, judged
// by StaleClaims alone, writes none. The key carries the story's id.
func SweepKey(id string) string { return "sweep." + id }

// SweepNotes is the part of the tracker's key-value store a close-out or a
// dispatch clears when a fresh attempt of a story starts. It is TrackerSync's
// own Note, SetNote and ClearNote, narrowed.
type SweepNotes interface {
	Note(ctx context.Context, key string) (string, error)
	SetNote(ctx context.Context, key, value string) error
	ClearNote(ctx context.Context, key string) error
}

// Sweep finds, for one host, the claimed stories whose lease has run out with
// no heartbeat since — StaleClaims's own definition, and the only one — and
// marks each one run=stuck, once. It is the zero-token, read-mostly twin of a
// person watching tmux: it never kills or restarts a session, never gives a
// claim back, and never touches a worktree or the ledger. Settling a stuck
// claim (what becomes of the worktree, giving the claim back) is a separate
// story.
type Sweep struct {
	Tracker WorkTracker

	// Host is which of the factory's hosts this sweep is for.
	Host string

	// Now is the clock staleness is measured against. The zero value reads the
	// real one.
	Now func() time.Time

	// Out is where the report is printed. A nil Out prints nothing.
	Out io.Writer
}

// SweepReport is what one sweep pass found and recorded.
type SweepReport struct {
	Host string
	// Claimed is how many stories this sweep found claimed here, whatever it
	// did with each.
	Claimed int
	// Stuck is the stories newly marked run=stuck this pass, with their epic's
	// defaults overlaid.
	Stuck []StoryDetail
	// Skipped is the claimed stories left alone because an earlier mw next or
	// mw sweep had already recorded their session gone.
	Skipped []string
	// Notes are the things that went sideways without stopping the sweep: one
	// story's trouble is never a reason to leave the rest unexamined.
	Notes []string
}

// Run reads what this host has claimed and, of those, which StaleClaims says
// have a lease that ran out with no heartbeat since, comments the finding
// once and records run=stuck. A story a sweep or mw next already recorded
// gone is left exactly as it was.
func (s Sweep) Run(ctx context.Context) (SweepReport, error) {
	report := SweepReport{Host: s.Host}
	switch {
	case s.Tracker == nil:
		return report, fmt.Errorf("sweeping %s: there is no work tracker to read it from", s.Host)
	case s.Host == "":
		return report, fmt.Errorf("sweeping: which host is this? set MW_HOST, or host in the config file")
	}

	claimed, err := s.Tracker.RunningStories(ctx, s.Host)
	if err != nil {
		return report, fmt.Errorf("reading what is claimed on %s: %w", s.Host, err)
	}
	report.Claimed = len(claimed)

	stale, err := s.Tracker.StaleClaims(ctx, s.now())
	if err != nil {
		return report, fmt.Errorf("reading the stale claims on %s: %w", s.Host, err)
	}
	staleIDs := make(map[string]bool, len(stale))
	for _, detail := range stale {
		staleIDs[detail.Story.ID] = true
	}

	for _, detail := range claimed {
		if staleIDs[detail.Story.ID] {
			s.one(ctx, detail, &report)
		}
	}

	s.print(report.String())
	return report, nil
}

// one records what sweep found of one claimed story whose lease StaleClaims
// says has run out, unless a mw next close-out or an earlier sweep already
// recorded its session gone — the guard both share, so that whichever of
// them notices first is the one that gets to say so.
func (s Sweep) one(ctx context.Context, detail StoryDetail, report *SweepReport) {
	id := detail.Story.ID
	if recordedGone(ctx, s.Tracker, id) {
		report.Skipped = append(report.Skipped, id)
		return
	}
	why := fmt.Sprintf(
		"mw sweep on %s found this story's claim: its lease had expired at %s with no heartbeat since. "+
			"The claim was left alone; settling a stuck claim is a separate story.",
		s.Host, detail.LeaseExpires.UTC().Format(time.RFC3339))
	s.markStuck(ctx, detail, why, report)
}

// markStuck comments the finding on the bead once and records run=stuck — the
// one state sweep writes, because a claim gone stuck is an event worth
// keeping. A write that fails is noted and sweep moves on to the next claimed
// story rather than stopping: one story's trouble is not a reason to leave
// every other claimed story unexamined.
func (s Sweep) markStuck(ctx context.Context, detail StoryDetail, why string, report *SweepReport) {
	id := detail.Story.ID
	if err := s.Tracker.CommentOnStory(ctx, id, why); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s could not be commented on: %v", id, err))
	}
	if err := s.Tracker.SetStoryState(ctx, id, RunState, RunStuck, why); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s could not be recorded as %s=%s: %v", id, RunState, RunStuck, err))
		return
	}
	report.Stuck = append(report.Stuck, detail)
}

// now is the clock staleness is measured against.
func (s Sweep) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

// print writes the report, when there is somewhere to write it.
func (s Sweep) print(text string) {
	if s.Out == nil {
		return
	}
	fmt.Fprint(s.Out, text)
}

// String is the sweep report as a person reads it: no line wider than Width.
func (r SweepReport) String() string {
	var b strings.Builder
	clip(&b, fmt.Sprintf("mw sweep · %s", r.Host))
	clip(&b, fmt.Sprintf("claimed %d, stuck %d, skipped %d", r.Claimed, len(r.Stuck), len(r.Skipped)))
	if len(r.Stuck) == 0 {
		clip(&b, "  nothing newly stuck")
	}
	for _, d := range r.Stuck {
		writeStory(&b, d, "")
	}
	for _, note := range r.Notes {
		clip(&b, "  ! "+note)
	}
	return b.String()
}
