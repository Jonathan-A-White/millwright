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
// person watching tmux. Of a session it kills one kind and no other: the idle
// session of a story the tracker holds closed (landed and verified), which a
// close-out can leave behind; never a session of a story still open, claimed
// or running, never one whose pane is at work, never one that is no story's.
// It never restarts a session, never gives a claim back, and never touches a
// worktree or the ledger. Settling a stuck claim (what becomes of the
// worktree, giving the claim back) is a separate story.
type Sweep struct {
	Tracker WorkTracker

	// Sessions is the host's tmux sessions, for closing the finished stories'.
	// Nil leaves every session alone.
	Sessions SweepSessions

	// Host is which of the factory's hosts this sweep is for.
	Host string

	// Notes is where the hosts' give-way notes are read (mw-2891blf.3): the claims
	// of a host whose note stands are frozen, not stuck. nil judges every claim by
	// its lease. GiveWayStale is giveway_stale (zero reads DefaultGiveWayStale).
	Notes        GiveWayNoteReader
	GiveWayStale time.Duration

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
	// Queued is the claimed stories left alone because they are queued for
	// batch landing: their session is gone and their lease lapsed by design.
	Queued []string
	// HandedBack is the claimed stories left alone because their close-out
	// handed them back for their close-out alone (RunCloseOutAgain,
	// mw-gq6.467): the next dispatch tick runs it again.
	HandedBack []string
	// GaveWay is the claimed stories left alone because this host gives way: its
	// sessions are frozen and silent, whatever their leases say.
	GaveWay []string
	// SessionsSeen is how many tmux sessions this host held when the sweep
	// looked; zero when Sweep.Sessions is nil or there was no tmux or none.
	SessionsSeen int
	// SessionsClosed is the idle sessions of closed stories this pass killed.
	SessionsClosed []string
	// SessionsKept is the sessions of closed stories left alone because their
	// pane is not idle.
	SessionsKept []string
	// sessionsAsked is that the host's sessions were listed, so that a host
	// with none can say so.
	sessionsAsked bool
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

	giving := GiveWayReader{Notes: s.Notes, Stale: s.GiveWayStale}.Stands(ctx, s.Host, s.now())
	for _, detail := range claimed {
		if !staleIDs[detail.Story.ID] {
			continue
		}
		if giving {
			report.GaveWay = append(report.GaveWay, detail.Story.ID)
			continue
		}
		s.one(ctx, detail, &report)
	}

	s.sessions(ctx, &report)

	s.print(report.String())
	return report, nil
}

// one records what sweep found of one claimed story whose lease StaleClaims
// says has run out, unless a mw next close-out or an earlier sweep already
// recorded its session gone — the guard both share, so that whichever of
// them notices first is the one that gets to say so.
func (s Sweep) one(ctx context.Context, detail StoryDetail, report *SweepReport) {
	id := detail.Story.ID
	if QueuedForBatch(detail) {
		report.Queued = append(report.Queued, id)
		return
	}
	if CloseOutAgain(detail) {
		report.HandedBack = append(report.HandedBack, id)
		return
	}
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
		//mw:realclock the default of the injected Now, used where none is set
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
	if len(r.Queued) > 0 {
		clip(&b, "  queued for batch landing, not stuck: "+strings.Join(r.Queued, ", "))
	}
	if len(r.HandedBack) > 0 {
		clip(&b, "  handed back for its close-out alone, not stuck: "+strings.Join(r.HandedBack, ", "))
	}
	if len(r.GaveWay) > 0 {
		clip(&b, "  this host gives way, not stuck: "+strings.Join(r.GaveWay, ", "))
	}
	if len(r.SessionsClosed) > 0 {
		clip(&b, "  closed the idle session of a closed story: "+strings.Join(r.SessionsClosed, ", "))
	}
	if len(r.SessionsKept) > 0 {
		clip(&b, "  kept, the pane of a closed story is at work: "+strings.Join(r.SessionsKept, ", "))
	}
	if r.SessionsSeen == 0 && r.sessionsAsked {
		clip(&b, "  sessions: none on this host (no tmux, or no sessions)")
	}
	for _, note := range r.Notes {
		clip(&b, "  ! "+note)
	}
	return b.String()
}
