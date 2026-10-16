package application

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

// FilesHeldTooLong is how long a ready story may wait on a files overlap before
// mw status names it (mw-gq6.442): a files key wider than it needs, src/, kept
// a lampas bug waiting three days behind every running story under it, and
// nothing said so.
const FilesHeldTooLong = 6 * time.Hour

// FilesHeldKey is the note a host's dispatch keeps of the ready stories it is
// holding for a file a running story shares, and since when: one line each,
// the story's id and the time it was first held, RFC 3339.
func FilesHeldKey(host string) string { return "host." + host + ".files_held" }

// filesHeld is the stories a host's dispatch holds for a shared file, by id,
// and when each was first held.
type filesHeld map[string]time.Time

// readFilesHeld reads the host's note. A note that is not there or cannot be
// read holds nothing, and a line that does not parse is passed over.
func readFilesHeld(ctx context.Context, notes interface {
	Note(ctx context.Context, key string) (string, error)
}, host string) filesHeld {
	held := filesHeld{}
	if notes == nil {
		return held
	}
	value, err := notes.Note(ctx, FilesHeldKey(host))
	if err != nil {
		return held
	}
	for _, line := range strings.Split(value, "\n") {
		id, at, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		if since, err := time.Parse(time.RFC3339, strings.TrimSpace(at)); err == nil {
			held[id] = since
		}
	}
	return held
}

// String is the note's value, its lines in id order.
func (h filesHeld) String() string {
	var lines []string
	for _, id := range slices.Sorted(maps.Keys(h)) {
		lines = append(lines, id+" "+h[id].UTC().Format(time.RFC3339))
	}
	return strings.Join(lines, "\n")
}

// filesHeldPass is what one dispatch pass found at its shared-files gate: the
// ready stories it held there, and the ones it let past.
type filesHeldPass struct {
	held, past map[string]bool
}

func (p *filesHeldPass) hold(id string) {
	if p.held == nil {
		p.held = map[string]bool{}
	}
	p.held[id] = true
}

func (p *filesHeldPass) pass(id string) {
	if p.past == nil {
		p.past = map[string]bool{}
	}
	p.past[id] = true
}

// keepFilesHeld brings the host's note level with the pass: a story held for
// the first time is held from now, one held before keeps the time it was first
// held, and one let past the gate, or no longer ready, is taken off. A ready
// story an earlier gate stopped this pass is left as it was. The note is
// written only when it changes, so a story held tick after tick costs one read
// a tick. A dry run writes nothing.
func (d Dispatch) keepFilesHeld(ctx context.Context, ready []StoryDetail, pass filesHeldPass, report *DispatchReport) {
	if d.DryRun || d.Memory == nil {
		return
	}
	was := readFilesHeld(ctx, d.Memory, d.Host)
	stillReady := map[string]bool{}
	for _, detail := range ready {
		stillReady[detail.Story.ID] = true
	}
	now := filesHeld{}
	for id, since := range was {
		if stillReady[id] && !pass.past[id] {
			now[id] = since
		}
	}
	for id := range pass.held {
		if _, before := now[id]; !before {
			now[id] = d.now()
		}
	}
	if maps.Equal(was, now) {
		return
	}
	var err error
	if len(now) == 0 {
		err = d.Memory.ClearNote(ctx, FilesHeldKey(d.Host))
	} else {
		err = d.Memory.SetNote(ctx, FilesHeldKey(d.Host), now.String())
	}
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("when each story held for its files was first held could not be kept in the host's note: %v", err))
	}
}

// heldByFiles is the line mw status gives a ready story held for its files
// longer than FilesHeldTooLong, "" for any other.
func heldByFiles(since time.Time, now time.Time) string {
	if since.IsZero() || now.Sub(since) <= FilesHeldTooLong {
		return ""
	}
	return fmt.Sprintf("held by files for %s, since %s", elapsed(now.Sub(since)), since.UTC().Format("01-02 15:04Z"))
}
