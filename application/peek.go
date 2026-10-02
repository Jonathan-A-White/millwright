package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// PeekLines is how many lines of a Builder's transcript mw peek shows.
const PeekLines = 20

// TranscriptTail is the port a running session's progress is read through: the
// record the harness keeps of it. It is read-only and costs no fuel.
type TranscriptTail interface {
	// Tail reports at most lines lines of the newest session run in dir, one
	// line to an entry, the most recent last. It is "" when the harness has
	// recorded nothing there, which is not an error.
	Tail(ctx context.Context, dir string, lines int) (string, error)
}

// PeekRemote is the port mw peek reaches another host's sessions through.
type PeekRemote interface {
	// Peek runs mw peek for the story on host and reports what it printed.
	Peek(ctx context.Context, host, storyID string) (string, error)
}

// Peek shows where a running Builder has got to: for the story's session, on
// whichever host works it, the host, the session, when it was launched and how
// long ago, where the formula has got to, and the tail of what the harness has
// recorded of it. It reads and writes nothing, so that the Mayor can look at
// any time without guessing from a tmux target name.
//
// A story pathed to another host is read there: Remote runs mw peek on that
// host, and what it prints is printed here. Local reads this host's session
// whoever the story is pathed to, which is what Remote's far end asks for.
type Peek struct {
	Tracker WorkTracker
	Runner  Runner
	// Transcripts is the harness's record of the session. Nil, or empty for the
	// story, falls back to the session's own pane.
	Transcripts TranscriptTail
	// Remote reaches other hosts. Nil refuses a story pathed to one.
	Remote PeekRemote
	// Rigs is each rig's checkout on this host, by name: the story's worktree is
	// beside it.
	Rigs  map[string]string
	Host  string
	Local bool
	// Now is the clock; nil reads the real one.
	Now func() time.Time
	Out io.Writer
}

// Run prints the peek at one story.
func (p Peek) Run(ctx context.Context, storyID string) error {
	if p.Tracker == nil || p.Runner == nil {
		return fmt.Errorf("peeking at %s: there is no work tracker and runner to read it from", storyID)
	}
	detail, err := p.Tracker.ShowStory(ctx, storyID)
	if err != nil {
		return fmt.Errorf("reading %s: %w", storyID, err)
	}

	if host := detail.Merged().Host; !p.Local && host != "" && host != domain.HostAuto && host != p.Host {
		return p.remote(ctx, host, storyID)
	}

	now := time.Now()
	if p.Now != nil {
		now = p.Now()
	}
	session := SessionName(storyID)
	status, err := p.Runner.Status(ctx, session)
	if err != nil {
		return fmt.Errorf("asking the runner about session %s: %w", session, err)
	}

	var out strings.Builder
	fmt.Fprintf(&out, "%s  %s\n", storyID, detail.Story.Title)
	fmt.Fprintf(&out, "host: %s\n", p.Host)
	fmt.Fprintf(&out, "session: %s (%s)\n", session, sessionWords(status))

	started := detail.ClaimStarted()
	if started.IsZero() {
		fmt.Fprintf(&out, "launched: unknown\n")
	} else {
		fmt.Fprintf(&out, "launched: %s, %s ago\n", started.UTC().Format(time.RFC3339), elapsed(now.Sub(started)))
	}
	if detail.Closed() {
		if !started.IsZero() && !detail.ClosedAt.IsZero() {
			fmt.Fprintf(&out, "finished: the story is closed, after %s\n", elapsed(detail.ClosedAt.Sub(started)))
		} else {
			fmt.Fprintf(&out, "finished: the story is closed\n")
		}
	}

	step, err := p.step(ctx, detail)
	if err != nil {
		return err
	}
	fmt.Fprintf(&out, "step: %s\n", step)

	title, tail, err := p.tail(ctx, detail, session, status)
	if err != nil {
		return err
	}
	if tail == "" {
		fmt.Fprintf(&out, "transcript: nothing recorded\n")
	} else {
		fmt.Fprintf(&out, "%s (last %d lines):\n", title, PeekLines)
		for _, line := range strings.Split(tail, "\n") {
			fmt.Fprintf(&out, "  %s\n", line)
		}
	}

	if p.Out != nil {
		fmt.Fprint(p.Out, out.String())
	}
	return nil
}

func (p Peek) remote(ctx context.Context, host, storyID string) error {
	if p.Remote == nil {
		return fmt.Errorf("peeking at %s: it is worked on %s, and there is no way to reach another host", storyID, host)
	}
	text, err := p.Remote.Peek(ctx, host, storyID)
	if err != nil {
		return fmt.Errorf("peeking at %s on %s: %w", storyID, host, err)
	}
	if p.Out != nil {
		fmt.Fprint(p.Out, strings.TrimRight(text, "\n")+"\n")
	}
	return nil
}

// step says where the story's formula has got to. A story with no molecule
// recorded is a run whose steps nobody tracks.
func (p Peek) step(ctx context.Context, detail StoryDetail) (string, error) {
	root := detail.Molecule.RootID
	if root == "" {
		return "print-mode run, steps not tracked", nil
	}
	open, err := p.Tracker.OpenSteps(ctx, root)
	if err != nil {
		return "", fmt.Errorf("reading the open formula steps of %s: %w", detail.Story.ID, err)
	}
	switch len(open) {
	case 0:
		return "every formula step is closed", nil
	case 1:
		return fmt.Sprintf("%s (1 step still open)", open[0].Title), nil
	}
	return fmt.Sprintf("%s (%d steps still open)", open[0].Title, len(open)), nil
}

// tail is what the harness has recorded of the session, else what its pane has
// printed, with the heading to show it under; empty when there is neither.
func (p Peek) tail(ctx context.Context, detail StoryDetail, session string, status SessionStatus) (title, text string, err error) {
	if rig := p.Rigs[detail.Merged().Rig]; p.Transcripts != nil && rig != "" {
		text, err = p.Transcripts.Tail(ctx, WorktreeDir(rig, detail.Story.ID), PeekLines)
		if err != nil {
			return "", "", fmt.Errorf("reading the transcript of %s: %w", detail.Story.ID, err)
		}
		if strings.TrimSpace(text) != "" {
			return "transcript", text, nil
		}
	}
	if status.State == StateGone {
		return "", "", nil
	}
	text, err = p.Runner.Output(ctx, session, PeekLines)
	if err != nil {
		return "", "", fmt.Errorf("reading the pane of session %s: %w", session, err)
	}
	if strings.TrimSpace(text) == "" {
		return "", "", nil
	}
	return "pane", text, nil
}

func sessionWords(s SessionStatus) string {
	if s.State == StateExited {
		return fmt.Sprintf("exited, status %d", s.ExitCode)
	}
	return string(s.State)
}

// elapsed is a duration as a person says it: 8m, 1h05m, 2d03h.
func elapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	minutes := int(d / time.Minute)
	switch {
	case minutes < 60:
		return fmt.Sprintf("%dm", minutes)
	case minutes < 24*60:
		return fmt.Sprintf("%dh%02dm", minutes/60, minutes%60)
	}
	return fmt.Sprintf("%dd%02dh", minutes/(24*60), minutes/60%24)
}
