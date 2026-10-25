package application

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// WorktreeRemote is the port a command run off a story's host asks that host
// what its worktree of the story holds (mw-gq6.774): the far end of an ssh,
// which runs mw retry --worktree-state there.
type WorktreeRemote interface {
	// WorktreeState reports what the story's worktree on host holds. An error is
	// a host that could not be asked, which is not the same as a clean worktree.
	WorktreeState(ctx context.Context, host, storyID string) (WorktreeState, error)
}

// WorktreeState is what a story's worktree on one host holds: where it is, and
// the paths it has changed and not committed. An empty Path says there is no
// worktree of the story there, so nothing in it to lose.
type WorktreeState struct {
	Path string
	Left []string
}

// Clean says the worktree holds nothing uncommitted, or is not there.
func (s WorktreeState) Clean() bool { return len(s.Left) == 0 }

const (
	worktreeStatePath = "worktree: "
	worktreeStateLeft = "uncommitted: "
	worktreeStateNone = "none"
)

// String is the state as the far end prints it and ParseWorktreeState reads it.
func (s WorktreeState) String() string {
	var b strings.Builder
	if s.Path == "" {
		fmt.Fprintf(&b, "%s%s\n", worktreeStatePath, worktreeStateNone)
		return b.String()
	}
	fmt.Fprintf(&b, "%s%s\n", worktreeStatePath, s.Path)
	for _, left := range s.Left {
		fmt.Fprintf(&b, "%s%s\n", worktreeStateLeft, left)
	}
	return b.String()
}

// ParseWorktreeState reads what a far end printed. Text with no worktree line is
// an error, not a clean worktree: a host that printed something else (a login
// banner, an error) must not be taken to have nothing to lose.
func ParseWorktreeState(text string) (WorktreeState, error) {
	var state WorktreeState
	seen := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, worktreeStatePath):
			seen = true
			if path := strings.TrimPrefix(line, worktreeStatePath); path != worktreeStateNone {
				state.Path = path
			}
		case strings.HasPrefix(line, worktreeStateLeft):
			state.Left = append(state.Left, strings.TrimPrefix(line, worktreeStateLeft))
		}
	}
	if !seen {
		return WorktreeState{}, fmt.Errorf("the host did not say what its worktree holds: %q", strings.TrimSpace(text))
	}
	return state, nil
}

// WorktreeHere reads this host's worktree of a story and prints it as
// WorktreeState.String: it is what the far end of WorktreeRemote runs. It
// writes nothing.
type WorktreeHere struct {
	Tracker   WorkTracker
	Worktrees Worktrees
	Landing   Landing
	Rigs      map[string]string
	Out       io.Writer
}

// Run prints the state of the story's worktree on this host.
func (w WorktreeHere) Run(ctx context.Context, storyID string) (WorktreeState, error) {
	if w.Tracker == nil || w.Worktrees == nil || w.Landing == nil {
		return WorktreeState{}, fmt.Errorf("reading the worktree of %s: it needs a work tracker, worktrees and a landing", storyID)
	}
	detail, err := w.Tracker.ShowStory(ctx, storyID)
	if err != nil {
		return WorktreeState{}, fmt.Errorf("reading the worktree of %s: %w", storyID, err)
	}
	path, err := detail.Path()
	if err != nil {
		return WorktreeState{}, fmt.Errorf("reading the worktree of %s: %w", storyID, err)
	}
	rigDir, checkedOut := w.Rigs[path.Rig]
	if !checkedOut {
		return WorktreeState{}, fmt.Errorf("reading the worktree of %s: the rig %s is not checked out here: add it under [rigs] in the config file", storyID, path.Rig)
	}
	dir := WorktreeDir(rigDir, storyID)
	there, err := w.Worktrees.Exists(ctx, rigDir, dir, "")
	if err != nil {
		return WorktreeState{}, fmt.Errorf("reading the worktree of %s: checking whether %s is there: %w", storyID, dir, err)
	}
	var state WorktreeState
	if there {
		state.Path = dir
		if state.Left, err = w.Landing.Uncommitted(ctx, dir); err != nil {
			return WorktreeState{}, fmt.Errorf("reading the worktree of %s: reading what %s left uncommitted: %w", storyID, dir, err)
		}
	}
	if w.Out != nil {
		fmt.Fprint(w.Out, state.String())
	}
	return state, nil
}

// OffHostWhy is why a command that acts on a story's worktree (command, run as
// mw <command> <story>) refuses on a host other than the one that holds the
// story's claim, or "" when it may go on (mw-gq6.774). The claim (mw@<host>) is
// where a dispatch made the worktree; a story with no claim of mw's has none.
func OffHostWhy(command, storyID, assignee, here string) string {
	host := ClaimHost(assignee)
	if host == "" || here == "" || host == here {
		return ""
	}
	return fmt.Sprintf("its claim is %s, so its worktree is on %s, not %s: run `mw %s %s` on %s",
		assignee, host, here, command, storyID, host)
}
