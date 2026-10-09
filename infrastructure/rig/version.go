package rig

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// BumpVersion implements application.Landing: the landing worktree is where the
// merge was just made, and the commit made here is the last one before the push.
func (w *Worktrees) BumpVersion(ctx context.Context, landingDir string, bump application.VersionBump) (application.Bumped, error) {
	if len(bump.Files) == 0 || bump.Before == "" || bump.Branch == "" {
		return application.Bumped{}, fmt.Errorf("raising the version in %s: which files, merged from which commit and branch?", landingDir)
	}

	// Did the story's own commits change the version? Then the Builder raised it
	// on purpose (the minor of an epic's last story) and mw adds nothing to it.
	fork, err := w.git(ctx, landingDir, "merge-base", bump.Before, bump.Branch)
	if err != nil {
		return application.Bumped{}, err
	}
	was := w.versionAt(ctx, landingDir, strings.TrimSpace(fork), bump.Files[0])
	now := w.versionAt(ctx, landingDir, bump.Branch, bump.Files[0])
	if was != now {
		return application.Bumped{}, nil
	}

	first, err := os.ReadFile(filepath.Join(landingDir, filepath.FromSlash(bump.Files[0])))
	if err != nil {
		return application.Bumped{}, fmt.Errorf("reading the version in %s: %w", bump.Files[0], err)
	}
	current, err := domain.VersionOf(first)
	if err != nil {
		return application.Bumped{}, fmt.Errorf("%s: %w", bump.Files[0], err)
	}
	next, err := domain.NextPatch(current)
	if err != nil {
		return application.Bumped{}, fmt.Errorf("%s: %w", bump.Files[0], err)
	}

	// Every file is read and checked before the first is written, so that a
	// file that cannot be raised leaves the landing worktree as it was.
	written := make([][]byte, len(bump.Files))
	for i, file := range bump.Files {
		path := filepath.Join(landingDir, filepath.FromSlash(file))
		content, err := os.ReadFile(path)
		if err != nil {
			return application.Bumped{}, fmt.Errorf("reading the version in %s: %w", file, err)
		}
		if written[i], err = domain.SetVersion(content, next); err != nil {
			return application.Bumped{}, fmt.Errorf("%s: %w", file, err)
		}
	}
	for i, file := range bump.Files {
		if err := os.WriteFile(filepath.Join(landingDir, filepath.FromSlash(file)), written[i], 0o644); err != nil {
			return application.Bumped{}, fmt.Errorf("writing the version in %s: %w", file, err)
		}
	}

	// --no-verify: the commit is mw's own, and a rig's pre-commit hook has no say
	// in it any more than it has in the merge commit.
	if _, err := w.git(ctx, landingDir, append([]string{"add", "--"}, bump.Files...)...); err != nil {
		return application.Bumped{}, err
	}
	message := fmt.Sprintf("Version %s (%s)", next, bump.StoryID)
	if _, err := w.git(ctx, landingDir, "commit", "-q", "--no-verify", "-m", message); err != nil {
		return application.Bumped{}, err
	}
	head, err := w.git(ctx, landingDir, "rev-parse", "HEAD")
	if err != nil {
		return application.Bumped{}, err
	}
	return application.Bumped{Version: next, Commit: strings.TrimSpace(head)}, nil
}

// versionAt is the version the file holds at rev, or empty when the file is not
// there or holds none: a story that adds the file has changed its version.
func (w *Worktrees) versionAt(ctx context.Context, dir, rev, file string) string {
	content, err := w.git(ctx, dir, "show", rev+":"+file)
	if err != nil {
		return ""
	}
	version, err := domain.VersionOf([]byte(content))
	if err != nil {
		return ""
	}
	return version
}
