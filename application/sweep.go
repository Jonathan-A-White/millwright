package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// DefaultStaleAfter is how long a claimed story's session may print nothing
// new before Sweep calls it stuck, when nothing says otherwise. It is the
// same two hours infrastructure/config.DefaultStaleHours reads as its default.
const DefaultStaleAfter = 2 * time.Hour

// SweepOutputLines is how much of a session's recent output Sweep reads to
// tell whether it has changed since the last sweep: enough to catch a session
// that is still printing something, not so much that reading it costs real
// time.
const SweepOutputLines = 20

// The state dimensions Sweep records on a bead between runs. Sweep keeps no
// daemon and no state of its own: a claimed story's session output
// fingerprint, and when that fingerprint was first seen, are its only memory,
// and the bead is the only place that survives between runs. Neither
// dimension is a run state mw status reads.
const (
	SweepOutputState = "sweep-output"
	SweepSinceState  = "sweep-output-since"
)

// Sweep finds, for one host, the claimed stories whose Runner session is gone
// or has gone quiet, and marks each one run=stuck — once. It is the
// zero-token, mostly-read twin of a person watching tmux: it never kills or
// restarts a session, never gives a claim back, and never touches a worktree
// or the ledger. Settling a stuck claim (what becomes of the worktree, giving
// the claim back) is a separate story.
type Sweep struct {
	Tracker WorkTracker
	Runner  Runner

	// Host is which of the factory's hosts this sweep is for.
	Host string

	// StaleAfter is how long a claimed story's session may show no new output
	// before its claim is called stuck. Zero reads DefaultStaleAfter.
	StaleAfter time.Duration

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

// Run reads what this host has claimed, and for each story whose session is
// gone or has gone quiet past the threshold, comments the finding once and
// records run=stuck. A story a sweep or mw next already recorded gone is left
// exactly as it was.
func (s Sweep) Run(ctx context.Context) (SweepReport, error) {
	report := SweepReport{Host: s.Host}
	switch {
	case s.Tracker == nil:
		return report, fmt.Errorf("sweeping %s: there is no work tracker to read it from", s.Host)
	case s.Runner == nil:
		return report, fmt.Errorf("sweeping %s: there is no runner to ask about sessions", s.Host)
	case s.Host == "":
		return report, fmt.Errorf("sweeping: which host is this? set MW_HOST, or host in the config file")
	}

	claimed, err := s.Tracker.RunningStories(ctx, s.Host)
	if err != nil {
		return report, fmt.Errorf("reading what is claimed on %s: %w", s.Host, err)
	}
	report.Claimed = len(claimed)

	for _, detail := range claimed {
		s.one(ctx, detail, &report)
	}

	s.print(report.String())
	return report, nil
}

// one examines one claimed story and records what sweep found, unless a
// mw next close-out or an earlier sweep already recorded its session gone —
// the guard both share, so that whichever of them notices first is the one
// that gets to say so.
func (s Sweep) one(ctx context.Context, detail StoryDetail, report *SweepReport) {
	id := detail.Story.ID
	if recordedGone(ctx, s.Tracker, id) {
		report.Skipped = append(report.Skipped, id)
		return
	}

	name := SessionName(id)
	status, err := s.Runner.Status(ctx, name)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the session of %s could not be asked about: %v", id, err))
		return
	}

	if !status.Running() {
		s.markStuck(ctx, detail, fmt.Sprintf(
			"mw sweep on %s found this story claimed here with no session behind it: %s is %s. "+
				"The claim was left alone; settling a stuck claim is a separate story.",
			s.Host, name, status.State), report)
		return
	}

	stuck, why, err := s.silent(ctx, id, name)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the output of %s's session could not be read: %v", id, err))
		return
	}
	if stuck {
		s.markStuck(ctx, detail, why, report)
	}
}

// silent reports whether a running session has printed nothing new since
// sweep last looked, for at least StaleAfter. Sweep keeps no state of its own
// between runs, so what it saw last time, and when, is written on the bead
// itself under SweepOutputState and SweepSinceState — the only place a
// zero-daemon sweep can remember anything. A session that changed, or one
// swept for the first time, gets its clock reset rather than reported: every
// session is owed at least one full StaleAfter before it is called stuck.
func (s Sweep) silent(ctx context.Context, id, name string) (bool, string, error) {
	output, err := s.Runner.Output(ctx, name, SweepOutputLines)
	if err != nil {
		return false, "", err
	}
	seen := fingerprint(output)

	last, err := s.Tracker.StoryState(ctx, id, SweepOutputState)
	if err != nil {
		return false, "", err
	}
	sinceText, err := s.Tracker.StoryState(ctx, id, SweepSinceState)
	if err != nil {
		return false, "", err
	}

	now := s.now()
	if last != seen || sinceText == "" {
		if err := s.Tracker.SetStoryState(ctx, id, SweepOutputState, seen,
			"mw sweep recorded this session's output"); err != nil {
			return false, "", err
		}
		if err := s.Tracker.SetStoryState(ctx, id, SweepSinceState, strconv.FormatInt(now.Unix(), 10),
			"mw sweep recorded when this output was first seen"); err != nil {
			return false, "", err
		}
		return false, "", nil
	}

	since, err := strconv.ParseInt(sinceText, 10, 64)
	if err != nil {
		return false, "", fmt.Errorf("the recorded %s of %s is not a timestamp: %q", SweepSinceState, id, sinceText)
	}
	quiet := now.Sub(time.Unix(since, 0))
	if quiet < s.staleAfter() {
		return false, "", nil
	}
	why := fmt.Sprintf(
		"mw sweep on %s found this story's session %s has printed nothing new for over %s, since %s. "+
			"The claim was left alone; settling a stuck claim is a separate story.",
		s.Host, name, s.staleAfter(), time.Unix(since, 0).UTC().Format(time.RFC3339))
	return true, why, nil
}

// markStuck comments the finding on the bead once and records run=stuck. A
// write that fails is noted and sweep moves on to the next claimed story
// rather than stopping: one story's trouble is not a reason to leave every
// other claimed story unexamined.
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

// staleAfter is the threshold a session's unchanged output is measured
// against.
func (s Sweep) staleAfter() time.Duration {
	if s.StaleAfter <= 0 {
		return DefaultStaleAfter
	}
	return s.StaleAfter
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

// fingerprint is a short, stable stand-in for a session's recent output, so
// that Sweep can tell whether it changed without keeping the output itself on
// the bead.
func fingerprint(output string) string {
	sum := sha256.Sum256([]byte(output))
	return hex.EncodeToString(sum[:])[:16]
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
