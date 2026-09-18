package rig

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// Worktrees lands a story's work as well as cutting the worktree it was done
// in: both are the rig's git, and they share the same command and the same
// remote.
var _ application.Landing = (*Worktrees)(nil)

// Ahead implements application.Landing: the commits branch has that base does
// not.
func (w *Worktrees) Ahead(ctx context.Context, rigDir, branch, base string) (int, error) {
	switch {
	case branch == "":
		return 0, fmt.Errorf("counting commits in %s: which branch?", rigDir)
	case base == "":
		return 0, fmt.Errorf("counting commits on %s in %s: ahead of what?", branch, rigDir)
	}
	said, err := w.git(ctx, rigDir, "rev-list", "--count", base+".."+branch)
	if err != nil {
		return 0, err
	}
	commits, err := strconv.Atoi(strings.TrimSpace(said))
	if err != nil {
		return 0, fmt.Errorf("counting the commits %s has and %s does not: %s said %q", branch, base, w.program, said)
	}
	return commits, nil
}

// OpenLanding implements application.Landing. The landing is a worktree of its
// own, detached at the target branch as the remote has it, so that neither the
// rig's own checkout — which a person may be sitting in — nor the story's
// worktree is touched by the landing. Nothing is ever landed on a local branch:
// what is pushed is what was merged and tested, and nothing else.
func (w *Worktrees) OpenLanding(ctx context.Context, rigDir, base string) (string, error) {
	if base == "" {
		return "", fmt.Errorf("opening a landing of %s: landing on what?", rigDir)
	}
	dir, err := os.MkdirTemp(filepath.Dir(filepath.Clean(rigDir)), application.LandingPrefix)
	if err != nil {
		return "", fmt.Errorf("making the directory the landing of %s belongs in: %w", rigDir, err)
	}
	// git worktree add refuses a directory that is already there, and MkdirTemp
	// has just made one: it is taken away again so that the name stays reserved
	// by nobody else while git makes it.
	if err := os.Remove(dir); err != nil {
		return "", fmt.Errorf("making room for the landing of %s: %w", rigDir, err)
	}
	if _, err := w.git(ctx, rigDir, "worktree", "add", "--detach", dir, base); err != nil {
		return "", err
	}
	return dir, nil
}

// Merge implements application.Landing. A merge that conflicts is undone before
// the error comes back: mw resolves nothing for anybody, and a landing worktree
// left half-merged would be a trap for whoever looked at it next.
func (w *Worktrees) Merge(ctx context.Context, landingDir, branch string) (application.Landed, error) {
	if branch == "" {
		return application.Landed{}, fmt.Errorf("merging into %s: which branch?", landingDir)
	}
	before, err := w.git(ctx, landingDir, "rev-parse", "HEAD")
	if err != nil {
		return application.Landed{}, err
	}
	tip, err := w.git(ctx, landingDir, "rev-parse", branch)
	if err != nil {
		return application.Landed{}, err
	}

	// --no-edit so that no editor is ever opened: nobody is at the keyboard.
	if _, err := w.git(ctx, landingDir, "merge", "--no-edit", branch); err != nil {
		if _, abort := w.git(ctx, landingDir, "merge", "--abort"); abort != nil {
			return application.Landed{}, fmt.Errorf("%w: %v (and the half-made merge in %s could not be undone: %v)",
				application.ErrMergeConflict, err, landingDir, abort)
		}
		return application.Landed{}, fmt.Errorf("%w: %v", application.ErrMergeConflict, err)
	}

	after, err := w.git(ctx, landingDir, "rev-parse", "HEAD")
	if err != nil {
		return application.Landed{}, err
	}
	commit := strings.TrimSpace(after)
	return application.Landed{
		Commit: commit,
		// The target branch simply moved onto the story's work: what is being
		// pushed is exactly the commit the story's tests ran on.
		FastForward: commit == strings.TrimSpace(tip) && commit != strings.TrimSpace(before),
	}, nil
}

// Push implements application.Landing: what the landing worktree has checked
// out becomes the branch on the remote. The refspec has no leading plus and
// there is no --force anywhere: a remote that has moved on refuses this push,
// which is the point.
func (w *Worktrees) Push(ctx context.Context, landingDir, remote, branch string) error {
	if remote == "" {
		remote = w.remote
	}
	if branch == "" {
		return fmt.Errorf("pushing from %s: onto which branch?", landingDir)
	}
	if _, err := w.git(ctx, landingDir, "push", remote, "HEAD:refs/heads/"+branch); err != nil {
		if rejected(err) {
			return fmt.Errorf("%w: %v", application.ErrPushRejected, err)
		}
		return err
	}
	return nil
}

// CloseLanding implements application.Landing.
func (w *Worktrees) CloseLanding(ctx context.Context, rigDir, landingDir string) error {
	if landingDir == "" {
		return nil
	}
	// The branch is empty: a landing is detached, and the branch it landed on
	// lives at the remote.
	return w.Remove(ctx, rigDir, landingDir, "")
}

// rejected reports whether git refused a push because the branch had moved on
// the remote, as against failing for any other reason. git says so in words;
// its exit status is 1 either way.
func rejected(err error) bool {
	said := strings.ToLower(err.Error())
	switch {
	case strings.Contains(said, "non-fast-forward"),
		strings.Contains(said, "fetch first"),
		strings.Contains(said, "! [rejected]"),
		strings.Contains(said, "[rejected]") && strings.Contains(said, "behind"):
		return true
	}
	return false
}
