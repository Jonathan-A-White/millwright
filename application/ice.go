package application

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// The icebox is work the Governor has decided not to do yet, and has not
// decided against. It was three edits by hand — a label, priority 4, held —
// and shows among the real holds; mw ice and mw thaw make it one word each.
const (
	// LabelIcebox marks an iced bead. It is the truth of the icebox: the
	// notes beside it only remember when.
	LabelIcebox = "icebox"

	// IcedPriority is the priority an iced bead has: the least urgent.
	IcedPriority = LeastUrgentPriority

	// ThawedPriority is the priority a thawed bead comes back at when the
	// thaw names none.
	ThawedPriority = 3

	// IceboxThawedWindow is how far back mw status counts thaws.
	IceboxThawedWindow = 7 * 24 * time.Hour

	// IcedNotePrefix and ThawedNotePrefix start the notes that remember when
	// a bead was iced (cleared by the thaw) and when it was last thawed.
	IcedNotePrefix    = "icebox.iced."
	ThawedNotePrefix  = "icebox.thawed."
	icedCommentPrefix = "Iced: "
	thawedComment     = "Thawed"
)

// IcedNoteKey is the note the time a bead was iced is kept under.
func IcedNoteKey(id string) string { return IcedNotePrefix + id }

// ThawedNoteKey is the note the time a bead was thawed is kept under.
func ThawedNoteKey(id string) string { return ThawedNotePrefix + id }

// Iced reports whether the bead is in the icebox.
func (d StoryDetail) Iced() bool { return hasLabel(d.Labels, LabelIcebox) }

// IceNotes is where the icebox remembers when: PosternNotes and the tracker's
// own notes, narrowed.
type IceNotes interface {
	SetNote(ctx context.Context, key, value string) error
	ClearNote(ctx context.Context, key string) error
	NotesWithPrefix(ctx context.Context, prefix string) (map[string]string, error)
}

// IcedBeads lists the beads in the icebox: held, carrying LabelIcebox, epics
// left out. The beads gateway is the one reader.
type IcedBeads interface {
	IcedBeads(ctx context.Context) ([]StoryDetail, error)
}

// Ice puts a bead in the icebox: the label, the least urgent priority, held,
// and one comment saying why. Nothing is written for a bead that cannot be
// iced: an epic, a finished one, or one a session has claimed.
type Ice struct {
	Tracker WorkTracker
	// Notes remembers when; nil remembers nothing.
	Notes IceNotes
	Now   func() time.Time
}

// Run ices the bead for the reason.
func (i Ice) Run(ctx context.Context, id, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("icing %s: say why with --reason", id)
	}
	detail, err := i.Tracker.ShowStory(ctx, id)
	if err != nil {
		return fmt.Errorf("reading %s to ice it: %w", id, err)
	}
	switch {
	case detail.IsEpic:
		return fmt.Errorf("icing %s: it is an epic; ice its stories one by one", id)
	case detail.Closed():
		return fmt.Errorf("icing %s: it is closed", id)
	case detail.Iced():
		return fmt.Errorf("icing %s: it is already iced", id)
	case strings.EqualFold(strings.TrimSpace(detail.Status), StatusInProgress) || strings.TrimSpace(detail.Assignee) != "":
		return fmt.Errorf("icing %s: it is claimed by %s; end the session first", id, detail.Assignee)
	}
	if err := i.Tracker.AddLabel(ctx, id, LabelIcebox); err != nil {
		return fmt.Errorf("labelling %s %s: %w", id, LabelIcebox, err)
	}
	if err := i.Tracker.SetStoryPriority(ctx, id, IcedPriority); err != nil {
		return fmt.Errorf("setting %s to priority %d: %w", id, IcedPriority, err)
	}
	if err := i.Tracker.HoldStory(ctx, id); err != nil {
		return fmt.Errorf("holding %s: %w", id, err)
	}
	if err := i.Tracker.CommentOnStory(ctx, id, icedCommentPrefix+reason); err != nil {
		return fmt.Errorf("commenting on %s: %w", id, err)
	}
	if i.Notes != nil {
		now := time.Now
		if i.Now != nil {
			now = i.Now
		}
		if err := i.Notes.SetNote(ctx, IcedNoteKey(id), now().UTC().Format(time.RFC3339)); err != nil {
			return fmt.Errorf("noting when %s was iced: %w", id, err)
		}
	}
	return nil
}

// Thaw takes a bead out of the icebox: the label off, the priority set, one
// comment. The bead stays held: releasing it is a separate yes.
type Thaw struct {
	Tracker WorkTracker
	Notes   IceNotes
	Now     func() time.Time
	// Comment is what is written on the bead; empty writes "Thawed".
	Comment string
}

// Run thaws the bead at the priority, or ThawedPriority when it is nil.
func (th Thaw) Run(ctx context.Context, id string, priority *int) error {
	level := ThawedPriority
	if priority != nil {
		level = *priority
	}
	if err := ValidPriority(level); err != nil {
		return fmt.Errorf("thawing %s: %w", id, err)
	}
	detail, err := th.Tracker.ShowStory(ctx, id)
	if err != nil {
		return fmt.Errorf("reading %s to thaw it: %w", id, err)
	}
	if !detail.Iced() {
		return fmt.Errorf("thawing %s: it is not iced", id)
	}
	if err := th.Tracker.RemoveLabel(ctx, id, LabelIcebox); err != nil {
		return fmt.Errorf("taking the label off %s: %w", id, err)
	}
	if err := th.Tracker.SetStoryPriority(ctx, id, level); err != nil {
		return fmt.Errorf("setting %s to priority %d: %w", id, level, err)
	}
	comment := th.Comment
	if comment == "" {
		comment = thawedComment
	}
	if err := th.Tracker.CommentOnStory(ctx, id, comment); err != nil {
		return fmt.Errorf("commenting on %s: %w", id, err)
	}
	if th.Notes != nil {
		now := time.Now
		if th.Now != nil {
			now = th.Now
		}
		if err := th.Notes.SetNote(ctx, ThawedNoteKey(id), now().UTC().Format(time.RFC3339)); err != nil {
			return fmt.Errorf("noting when %s was thawed: %w", id, err)
		}
		if err := th.Notes.ClearNote(ctx, IcedNoteKey(id)); err != nil {
			return fmt.Errorf("clearing when %s was iced: %w", id, err)
		}
	}
	return nil
}

// IceboxSummary is what mw status says of the icebox.
type IceboxSummary struct {
	// Known is false when the report was not asked to read the icebox.
	Known bool
	Count int
	// Oldest is how long the longest-iced bead has been in; zero when none is
	// iced or none says when.
	Oldest time.Duration
	// Thawed is how many beads were thawed within IceboxThawedWindow.
	Thawed int
}

// Line is the one line the report prints.
func (s IceboxSummary) Line() string {
	line := fmt.Sprintf("ICEBOX: %d", s.Count)
	if s.Count > 0 && s.Oldest > 0 {
		line += " · oldest " + iceAge(s.Oldest)
	}
	return fmt.Sprintf("%s · thawed %d in 7 days", line, s.Thawed)
}

func iceAge(d time.Duration) string {
	if d >= 24*time.Hour {
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	return Clock(d)
}

// IceboxReader reads the icebox for mw status.
type IceboxReader struct {
	Beads IcedBeads
	// Notes, when set, says when each bead was iced and how many were thawed
	// this week.
	Notes IceNotes
	Now   func() time.Time
}

// Icebox reads the beads in the icebox and, from the notes, how long the
// oldest has been there and how many were thawed in the last week.
func (r IceboxReader) Icebox(ctx context.Context) (IceboxSummary, error) {
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}
	iced, err := r.Beads.IcedBeads(ctx)
	if err != nil {
		return IceboxSummary{}, fmt.Errorf("reading the iced beads: %w", err)
	}
	summary := IceboxSummary{Known: true, Count: len(iced)}
	var iceTimes, thawTimes map[string]string
	if r.Notes != nil {
		if iceTimes, err = r.Notes.NotesWithPrefix(ctx, IcedNotePrefix); err != nil {
			return IceboxSummary{}, fmt.Errorf("reading when beads were iced: %w", err)
		}
		if thawTimes, err = r.Notes.NotesWithPrefix(ctx, ThawedNotePrefix); err != nil {
			return IceboxSummary{}, fmt.Errorf("reading when beads were thawed: %w", err)
		}
	}
	var since []time.Time
	for _, d := range iced {
		at, err := time.Parse(time.RFC3339, strings.TrimSpace(iceTimes[IcedNoteKey(d.Story.ID)]))
		if err != nil {
			at = d.Updated
		}
		if !at.IsZero() {
			since = append(since, at)
		}
	}
	sort.Slice(since, func(a, b int) bool { return since[a].Before(since[b]) })
	if len(since) > 0 {
		summary.Oldest = now.Sub(since[0])
	}
	for _, value := range thawTimes {
		if at, err := time.Parse(time.RFC3339, strings.TrimSpace(value)); err == nil && now.Sub(at) <= IceboxThawedWindow {
			summary.Thawed++
		}
	}
	return summary, nil
}
