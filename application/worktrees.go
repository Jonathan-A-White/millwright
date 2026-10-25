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

	// Exists reports whether dir or branch is already there — left behind by
	// an earlier attempt that never had Remove or RemoveWithoutForce run on
	// it — so that a caller about to cut a fresh worktree can find out before
	// Add refuses it (mw-gq6.107).
	Exists(ctx context.Context, rigDir, dir, branch string) (bool, error)

	// Remove takes a worktree and its branch away again, as if they had never
	// been made. Removing what is not there is not an error — it is how a
	// dispatch that failed halfway tidies up after itself.
	Remove(ctx context.Context, rigDir, dir, branch string) error

	// RemoveWithoutForce takes a worktree away exactly as Remove does, except
	// that it refuses rather than discarding anything a session left
	// uncommitted in it. It is what a retry uses once a worktree's commits are
	// safely bundled and pushed to the vault: nothing here is ever thrown away
	// unread. Removing what is not there is not an error.
	RemoveWithoutForce(ctx context.Context, rigDir, dir string) error

	// DeleteBranch removes a branch that is checked out nowhere, trying a safe
	// delete (`git branch -d`) first and forcing (`git branch -D`) only when
	// the safe delete refuses it. A branch that is not there is not an error.
	DeleteBranch(ctx context.Context, rigDir, branch string) error

	// RenameBranch gives a branch a new name and deletes nothing: it is how
	// a dispatch keeps what an earlier attempt left under another name before
	// the story's own branch is cut again (mw-gq6.326). It refuses when to is
	// already a branch, and when from is not one.
	RenameBranch(ctx context.Context, rigDir, from, to string) error
}

// Rebases is the port the worktree of an attempt cut from the branch an earlier
// attempt left is rebased onto its target through (mw-1ljpo9.7), so that the
// next attempt continues that work on what the target holds now.
type Rebases interface {
	// Rebase rebases the branch checked out in dir onto onto. A rebase that
	// stops on a conflict is undone before it returns false, so that dir is
	// left as it was before; true is a rebase that went through.
	Rebase(ctx context.Context, dir, onto string) (bool, error)
}

// BranchChanges is the port the files a running story's branch has changed so
// far are read through: git, in the rig's own checkout, where every story
// worktree's branch is a branch of the rig (mw-gq6.349).
type BranchChanges interface {
	// ChangedFiles is every file the commits on branch beyond base change: the
	// diff from their merge base to branch, three dots, so not what base gained
	// meanwhile. A branch that is not there, as for a story worked on another
	// host, has changed nothing.
	ChangedFiles(ctx context.Context, rigDir, base, branch string) ([]string, error)

	// AddedFiles is the files the same diff adds (not changes), by path, each
	// with its size in bytes. A branch that is not there has added nothing
	// (mw-gq6.517).
	AddedFiles(ctx context.Context, rigDir, base, branch string) ([]AddedFile, error)

	// DiffLines is every line the same diff adds or removes in a file called
	// name, at the repo root or at any depth, in the diff's own order. A branch
	// that is not there has changed no line (mw-gq6.740).
	DiffLines(ctx context.Context, rigDir, base, branch, name string) ([]DiffLine, error)
}

// BranchFiles is a BranchChanges that can also read a file as a branch has it
// (mw-gq6.801); Next asserts for it, and a Changes without it reads nothing.
type BranchFiles interface {
	// ReadBranchFile reports the file at path, relative to the rig's root, at
	// the tip of branch; found is false when there is no such branch or file.
	ReadBranchFile(ctx context.Context, rigDir, branch, path string) (data []byte, found bool, err error)
}

// DiffLine is one line a branch adds or removes in a file: the file's path, the
// line without its diff sign and whether it was added.
type DiffLine struct {
	Path  string
	Text  string
	Added bool
}

// AddedFile is one file a branch adds: its path, its size and, for a file at
// the root of the repository only, whether it begins as an ELF or Mach-O
// executable does. Executable is not read below the root.
type AddedFile struct {
	Path       string
	Size       int64
	Executable bool
}
