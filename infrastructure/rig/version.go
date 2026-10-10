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
	// A file the story added, with no version before, is a rig's first landing
	// (mw-gq6.334): that is not a raise, and the patch is raised from what it holds.
	fork, err := w.git(ctx, landingDir, "merge-base", bump.Before, bump.Branch)
	if err != nil {
		return application.Bumped{}, err
	}
	was := w.versionAt(ctx, landingDir, strings.TrimSpace(fork), bump.Files[0])
	now := w.versionAt(ctx, landingDir, bump.Branch, bump.Files[0])
	if was != "" && was != now {
		return w.noteUnderOwnVersion(ctx, landingDir, bump, now)
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
	noted, err := changelogsWith(landingDir, bump, next)
	if err != nil {
		return application.Bumped{}, err
	}
	for i, file := range bump.Files {
		if err := os.WriteFile(filepath.Join(landingDir, filepath.FromSlash(file)), written[i], 0o644); err != nil {
			return application.Bumped{}, fmt.Errorf("writing the version in %s: %w", file, err)
		}
	}
	if err := writeAll(landingDir, noted); err != nil {
		return application.Bumped{}, err
	}

	commit, err := w.commitFiles(ctx, landingDir, fmt.Sprintf("Version %s (%s)", next, bump.StoryID), append(append([]string(nil), bump.Files...), changelogNames(noted)...))
	if err != nil {
		return application.Bumped{}, err
	}
	return application.Bumped{Version: next, Commit: commit}, nil
}

// noteUnderOwnVersion is what a landing does for a story that raised the version
// itself: nothing to the version, but the story's note, when the rig keeps one,
// goes under that version in a commit of its own.
func (w *Worktrees) noteUnderOwnVersion(ctx context.Context, landingDir string, bump application.VersionBump, version string) (application.Bumped, error) {
	noted, err := changelogsWith(landingDir, bump, version)
	if err != nil || len(noted) == 0 {
		return application.Bumped{}, err
	}
	if err := writeAll(landingDir, noted); err != nil {
		return application.Bumped{}, err
	}
	commit, err := w.commitFiles(ctx, landingDir, fmt.Sprintf("What's new %s (%s)", version, bump.StoryID), changelogNames(noted))
	if err != nil {
		return application.Bumped{}, err
	}
	return application.Bumped{Commit: commit}, nil
}

// commitFiles stages the files and commits them under message, returning the
// commit. --no-verify: the commit is mw's own, and a rig's pre-commit hook has
// no say in it any more than it has in the merge commit.
func (w *Worktrees) commitFiles(ctx context.Context, dir, message string, files []string) (string, error) {
	if _, err := w.git(ctx, dir, append([]string{"add", "--"}, files...)...); err != nil {
		return "", err
	}
	if _, err := w.git(ctx, dir, "commit", "-q", "--no-verify", "-m", message); err != nil {
		return "", err
	}
	head, err := w.git(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(head), nil
}

// changelogFile is a changelog file as it will be written: its path in the
// rig and its new content.
type changelogFile struct {
	name    string
	content []byte
}

// changelogsWith reads each of the rig's changelog files (absent ones begin
// empty) and returns them with the bump's entry, under version, at the top. A
// bump with no changelog files, or whose entry is empty, returns none. Nothing
// is written, so that a file that cannot take the entry leaves the landing
// worktree as it was.
func changelogsWith(landingDir string, bump application.VersionBump, version string) ([]changelogFile, error) {
	if len(bump.Changelog) == 0 || bump.Entry.Text == "" {
		return nil, nil
	}
	entry := bump.Entry
	entry.Version = version
	var files []changelogFile
	for _, name := range bump.Changelog {
		existing, err := os.ReadFile(filepath.Join(landingDir, filepath.FromSlash(name)))
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("reading the changelog %s: %w", name, err)
		}
		var content []byte
		if strings.HasSuffix(name, ".json") {
			if content, err = domain.AddToChangelogJSON(existing, entry); err != nil {
				return nil, fmt.Errorf("the changelog %s: %w", name, err)
			}
		} else {
			content = domain.AddToChangelogMarkdown(existing, entry)
		}
		files = append(files, changelogFile{name: name, content: content})
	}
	return files, nil
}

func writeAll(landingDir string, files []changelogFile) error {
	for _, f := range files {
		path := filepath.Join(landingDir, filepath.FromSlash(f.name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("making room for the changelog %s: %w", f.name, err)
		}
		if err := os.WriteFile(path, f.content, 0o644); err != nil {
			return fmt.Errorf("writing the changelog %s: %w", f.name, err)
		}
	}
	return nil
}

func changelogNames(files []changelogFile) []string {
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.name
	}
	return names
}

// versionAt is the version the file holds at rev, or empty when the file is not
// there or holds none.
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
