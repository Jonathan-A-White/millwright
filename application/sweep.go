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

// SweepNotes is the part of the tracker's key-value store Sweep remembers in
// between runs. Sweep keeps no daemon and no state of its own: what it saw of
// a claimed story's session, and since when, is its only memory, and the
// tracker is the only place that survives between runs. It is written as a
// note, never as state on the story: every state change is a bead of its own
// in the tracker, closed and synced to the other host, and a sweep every few
// minutes would file two of them per story per pass. It is TrackerSync's own
// Note, SetNote and ClearNote, narrowed.
type SweepNotes interface {
	Note(ctx context.Context, key string) (string, error)
	SetNote(ctx context.Context, key, value string) error
	ClearNote(ctx context.Context, key string) error
}

// SweepKey is where Sweep keeps what it saw of one story's session: the note's
// key carries the story's id, and its value is the fingerprint of the session's
// recent output and the Unix time that output was first seen — or, for a
// session sweep had not seen before, when its story was claimed.
func SweepKey(id string) string { return "sweep." + id }

// Sweep finds, for one host, the claimed stories whose Runner session is gone
// or has gone quiet, and marks each one run=stuck — once. It is the
// zero-token, mostly-read twin of a person watching tmux: it never kills or
// restarts a session, never gives a claim back, and never touches a worktree
// or the ledger. Settling a stuck claim (what becomes of the worktree, giving
// the claim back) is a separate story.
type Sweep struct {
	Tracker WorkTracker
	Runner  Runner

	// Memory is where Sweep keeps what it saw of each session between runs.
	Memory SweepNotes

	// Host is which of the factory's hosts this sweep is for.
	Host string

	// Activity reads a claimed story's worktree for the newest file it has
	// written, so a session that only writes — every Builder, since its
	// session runs headless (`claude --print ... > result.json.tmp`) and
	// buffers everything it would print until it exits — is not judged by its
	// blank pane alone. dir not existing yet is not an error: Activity reports
	// the zero time. A nil Activity, or a rig Sweep has no directory for in
	// Rigs, leaves a session judged by its pane alone, exactly as before this
	// fold-in.
	Activity func(ctx context.Context, dir string) (time.Time, error)

	// Rigs is where each rig is checked out on this host, keyed by name — the
	// same config Dispatch and Check read — so Sweep can place a claimed
	// story's worktree for Activity.
	Rigs map[string]string

	// StaleAfter is how long a claimed story's session may show no new output
	// or worktree activity before its claim is called stuck. Zero reads
	// DefaultStaleAfter.
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
	case s.Memory == nil:
		return report, fmt.Errorf("sweeping %s: there is nowhere to remember what a session printed", s.Host)
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

	stuck, why, err := s.silent(ctx, detail, name)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the sign of life of %s's session could not be read: %v", id, err))
		return
	}
	if stuck {
		s.markStuck(ctx, detail, why, report)
	}
}

// silent reports whether a running session has printed or written nothing new
// since sweep last looked, for at least StaleAfter. What sweep saw last time,
// and since when, is the note SweepKey names in Memory. A session that
// changed gets its clock reset to now rather than reported; one sweep has not
// seen before starts its clock at the claim, when the tracker says when that
// was, and at this look when it does not — either way every session is owed
// one full StaleAfter, counted from the claim at the earliest, before it is
// called stuck.
func (s Sweep) silent(ctx context.Context, detail StoryDetail, name string) (bool, string, error) {
	id := detail.Story.ID
	output, err := s.Runner.Output(ctx, name, SweepOutputLines)
	if err != nil {
		return false, "", err
	}
	newest, err := s.worktreeActivity(ctx, detail)
	if err != nil {
		return false, "", fmt.Errorf("reading its worktree's activity: %w", err)
	}
	seen := fingerprint(output)
	if !newest.IsZero() {
		seen = fingerprint(output + "\x00" + newest.UTC().Format(time.RFC3339Nano))
	}

	saved, err := s.Memory.Note(ctx, SweepKey(id))
	if err != nil {
		return false, "", err
	}
	last, since, remembered := parseSweepNote(saved)

	now := s.now()
	if !remembered || last != seen {
		first := now
		if !remembered && !detail.Started.IsZero() && detail.Started.Before(now) {
			first = detail.Started
		}
		note := seen + " " + strconv.FormatInt(first.Unix(), 10)
		if err := s.Memory.SetNote(ctx, SweepKey(id), note); err != nil {
			return false, "", err
		}
		return false, "", nil
	}

	if now.Sub(since) < s.staleAfter() {
		return false, "", nil
	}
	why := fmt.Sprintf(
		"mw sweep on %s found this story's session %s has printed nothing new for over %s, since %s. "+
			"The claim was left alone; settling a stuck claim is a separate story.",
		s.Host, name, s.staleAfter(), since.UTC().Format(time.RFC3339))
	return true, why, nil
}

// worktreeActivity is the newest file a claimed story's worktree holds, or
// the zero time when Sweep cannot place one: Activity is not wired, or the
// story's rig is not in Rigs. Either way a session is then judged by its pane
// alone, exactly as sweep always has.
func (s Sweep) worktreeActivity(ctx context.Context, detail StoryDetail) (time.Time, error) {
	if s.Activity == nil {
		return time.Time{}, nil
	}
	rigDir, ok := s.Rigs[detail.Merged().Rig]
	if !ok {
		return time.Time{}, nil
	}
	return s.Activity(ctx, WorktreeDir(rigDir, detail.Story.ID))
}

// parseSweepNote reads a note Sweep wrote: the fingerprint and the time it was
// first seen. A note that is missing or is not one of Sweep's is taken as a
// session sweep has not seen, so that a note somebody edited by hand costs one
// threshold of waiting rather than a sweep that never comes right.
func parseSweepNote(note string) (fingerprint string, since time.Time, ok bool) {
	fingerprint, when, found := strings.Cut(strings.TrimSpace(note), " ")
	if !found || fingerprint == "" {
		return "", time.Time{}, false
	}
	seconds, err := strconv.ParseInt(when, 10, 64)
	if err != nil {
		return "", time.Time{}, false
	}
	return fingerprint, time.Unix(seconds, 0), true
}

// markStuck comments the finding on the bead once, records run=stuck — the one
// state sweep writes, because a claim gone stuck is an event worth keeping —
// and forgets what it remembered of the session. A write that fails is noted
// and sweep moves on to the next claimed story rather than stopping: one
// story's trouble is not a reason to leave every other claimed story
// unexamined.
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
	// A story recorded stuck is skipped from now on, so what sweep remembered
	// of its session is no use to anyone.
	if err := s.Memory.ClearNote(ctx, SweepKey(id)); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("what sweep remembered of %s could not be cleared: %v", id, err))
	}
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
