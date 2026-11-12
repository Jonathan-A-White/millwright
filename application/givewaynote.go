package application

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// GiveWayNoteTimeout is how long the host waits on the tracker for one write of
// its give-way note, or the clearing of it. The note is for the home; a tracker
// that is slow or down must not hold the person at the screen up, so a write that
// outruns it is given up on and logged (giveway.log), and nothing else changes.
const GiveWayNoteTimeout = 5 * time.Second

// GiveWayNoteRenewEvery is how often a renew rewrites the note at the most: a
// write is a beads call, and the Windows tick renews every five minutes.
const GiveWayNoteRenewEvery = 10 * time.Minute

// GiveWayNoteBelieved is how many times giveway_stale a note may go without a
// renewal before the home stops believing it. A host asleep, off or crashed
// leaves its note standing; the alarms must come back for it.
const GiveWayNoteBelieved = 2

// GiveWayKey is the note a host that gives way keeps for the home, in the
// tracker's key-value store: "<since RFC3339> <by> <seen RFC3339>", written after the
// freeze (never before) and cleared when the host goes on. Seen is when the note
// was last written, so that a note nobody renews can be told from one that stands.
func GiveWayKey(host string) string { return "host." + host + ".gives_way" }

// GiveWayNote is the value of GiveWayKey.
type GiveWayNote struct {
	Since time.Time
	By    string
	Seen  time.Time
}

// String is the note as it is kept: the three fields, a space apart.
func (n GiveWayNote) String() string {
	by := strings.Join(strings.Fields(n.By), "_")
	if by == "" {
		by = "unknown"
	}
	return fmt.Sprintf("%s %s %s", n.Since.UTC().Format(time.RFC3339), by, n.Seen.UTC().Format(time.RFC3339))
}

// ParseGiveWayNote reads a note. The seen time may be missing (a note of
// "<since> <by>" alone), and is then the since time.
func ParseGiveWayNote(value string) (GiveWayNote, bool) {
	fields := strings.Fields(value)
	if len(fields) < 2 {
		return GiveWayNote{}, false
	}
	since, err := time.Parse(time.RFC3339, fields[0])
	if err != nil {
		return GiveWayNote{}, false
	}
	note := GiveWayNote{Since: since, By: fields[1], Seen: since}
	if len(fields) > 2 {
		if seen, err := time.Parse(time.RFC3339, fields[2]); err == nil {
			note.Seen = seen
		}
	}
	return note, true
}

// GiveWayNoteWriter is where the host keeps its note: TrackerSync's own SetNote
// and ClearNote, narrowed.
type GiveWayNoteWriter interface {
	SetNote(ctx context.Context, key, value string) error
	ClearNote(ctx context.Context, key string) error
}

// GiveWayNoteReader is where the home reads a host's note: TrackerNotes.Note.
type GiveWayNoteReader interface {
	Note(ctx context.Context, key string) (string, error)
}

// GiveWayRead is what the home makes of one host's note.
type GiveWayRead struct {
	// Note is the note read, when there is one that parses.
	Note GiveWayNote
	// Stands says the host gives way and the note is believed: it was written
	// within GiveWayNoteBelieved times giveway_stale. Every alarm that judges a
	// session quiet, stuck, stranded or starved passes the host's stories over.
	Stands bool
	// Stale says a note stands that is not believed: nothing renewed it for
	// longer than that, so the alarms behave as without it.
	Stale bool
	// Stories is how many stories are claimed on the host, set by the one place
	// that counts them (mw status), for the line it prints.
	Stories int
}

// readGiveWay judges a note's value at now. stale is giveway_stale (zero reads
// DefaultGiveWayStale). A value that is empty or does not parse is no note.
func readGiveWay(value string, now time.Time, stale time.Duration) GiveWayRead {
	if strings.TrimSpace(value) == "" {
		return GiveWayRead{}
	}
	note, ok := ParseGiveWayNote(value)
	if !ok {
		return GiveWayRead{}
	}
	if stale <= 0 {
		stale = DefaultGiveWayStale
	}
	read := GiveWayRead{Note: note}
	if now.Sub(note.Seen) > GiveWayNoteBelieved*stale {
		read.Stale = true
		return read
	}
	read.Stands = true
	return read
}

// GiveWayReader reads whether a host gives way out of the tracker's notes. The
// zero value, and one with no Notes, gives way to nobody, and so does a note that
// cannot be read: the alarms then behave as they always did.
type GiveWayReader struct {
	Notes GiveWayNoteReader
	// Stale is giveway_stale; zero reads DefaultGiveWayStale.
	Stale time.Duration
}

// Of reads host's note at now.
func (r GiveWayReader) Of(ctx context.Context, host string, now time.Time) GiveWayRead {
	if r.Notes == nil || host == "" {
		return GiveWayRead{}
	}
	value, err := r.Notes.Note(ctx, GiveWayKey(host))
	if err != nil {
		return GiveWayRead{}
	}
	return readGiveWay(value, now, r.Stale)
}

// Stands says host's note stands and is believed at now.
func (r GiveWayReader) Stands(ctx context.Context, host string, now time.Time) bool {
	return r.Of(ctx, host, now).Stands
}

// giveWayShort is the time as mw status says it: month, day and the minute, UTC.
const giveWayShort = "01-02 15:04Z"

// line is the one line mw status says of a host that gives way, or of a note it
// does not believe.
func (g GiveWayRead) line(host string) string {
	switch {
	case g.Stands:
		return fmt.Sprintf("%s gives way since %s (%d %s frozen)", host, g.Note.Since.UTC().Format(giveWayShort), g.Stories, storyWord(g.Stories))
	case g.Stale:
		return fmt.Sprintf("%s: gives-way note stale, last written %s: not believed", host, g.Note.Seen.UTC().Format(giveWayShort))
	}
	return ""
}

func storyWord(n int) string {
	if n == 1 {
		return "story"
	}
	return "stories"
}

// claimedOn is the host a claim is held on: the dispatch that took it, from the
// assignee (mw@<host>).
func claimedOn(detail StoryDetail) string { return ClaimHost(detail.Assignee) }

// keepNote writes the host's give-way note after the freeze, and, on a renew, no
// oftener than GiveWayNoteRenewEvery. It returns what to say of it in the log
// line: "" when there is no tracker to write to, "set", "kept" for a renew too
// soon after the last write, or "failed: ..." for a write that failed or timed out
// (nothing else changes). It sets marker.NoteAt when it wrote.
func (g GiveWay) keepNote(ctx context.Context, marker *GiveWayMarker, now time.Time) string {
	if g.Notes == nil {
		return ""
	}
	if !marker.NoteAt.IsZero() && now.Sub(marker.NoteAt) < GiveWayNoteRenewEvery {
		return "kept"
	}
	note := GiveWayNote{Since: marker.Since, By: marker.By, Seen: now}
	ctx, cancel := context.WithTimeout(ctx, g.noteTimeout())
	defer cancel()
	if err := g.Notes.SetNote(ctx, GiveWayKey(g.Host), note.String()); err != nil {
		return "failed: " + oneLine(err.Error())
	}
	marker.NoteAt = now
	return "set"
}

// clearNote takes the host's give-way note away, under the same short timeout.
func (g GiveWay) clearNote(ctx context.Context) string {
	if g.Notes == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, g.noteTimeout())
	defer cancel()
	if err := g.Notes.ClearNote(ctx, GiveWayKey(g.Host)); err != nil {
		return "failed to clear: " + oneLine(err.Error())
	}
	return "cleared"
}

func (g GiveWay) noteTimeout() time.Duration {
	if g.NoteTimeout > 0 {
		return g.NoteTimeout
	}
	return GiveWayNoteTimeout
}
