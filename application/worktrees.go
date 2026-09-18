package application

import (
	"context"
	"path/filepath"
)

// WorktreesDir is the directory a rig's story worktrees are made in, beside the
// rig's own checkout, BranchPrefix is what a story's branch is called, and
// DefaultRemote is the remote a rig's target branch is read from.
const (
	WorktreesDir  = ".mw-worktrees"
	BranchPrefix  = "mw/"
	DefaultRemote = "origin"
)

// WorktreeDir is where the story's own working copy of a rig belongs: beside
// the rig's checkout, under WorktreesDir, named after the story.
func WorktreeDir(rigDir, storyID string) string {
	return filepath.Join(filepath.Dir(filepath.Clean(rigDir)), WorktreesDir, storyID)
}

// StoryBranch is the branch a story's work lands on.
func StoryBranch(storyID string) string { return BranchPrefix + storyID }

// StartPoint is the commit a story's branch is cut from: the target branch as
// the rig's origin has it, not as this host's local branch has it. Two hosts
// work one rig, so the local branch may be behind whatever the other host
// pushed, and a story worked on stale code is a merge conflict later.
func StartPoint(remote, branch string) string { return remote + "/" + branch }

// Worktrees is the port the factory cuts a story its own working copy of a rig
// through. One adapter is git; every path it is given is a directory on this
// host, named in the factory's config.
type Worktrees interface {
	// Fetch brings the rig's view of its origin up to date, so that a branch
	// cut afterwards is cut from what the other host has really pushed.
	Fetch(ctx context.Context, rigDir string) error

	// Add makes a worktree of the rig at dir, on a new branch cut from start.
	// It fails if the directory or the branch is already there: a story's
	// worktree is its own, and reusing one would hide work nobody looked at.
	Add(ctx context.Context, rigDir, dir, branch, start string) error

	// Remove takes a worktree and its branch away again, as if they had never
	// been made. Removing what is not there is not an error — it is how a
	// dispatch that failed halfway tidies up after itself.
	Remove(ctx context.Context, rigDir, dir, branch string) error
}
