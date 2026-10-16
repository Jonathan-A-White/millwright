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
	// by hand (a story in flight from before bump=minor) and mw adds nothing to it.
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
	raise := domain.NextPatch
	if bump.Minor {
		raise = domain.NextMinor
	}
	next, err := raise(current)
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
	noted, tidied, err := changelogsWith(landingDir, bump, next)
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
	return application.Bumped{Version: next, Commit: commit, Tidied: tidied}, nil
}

// noteUnderOwnVersion is what a landing does for a story that raised the version
// itself: nothing to the version, but the story's note, when the rig keeps one,
// goes under that version in a commit of its own.
func (w *Worktrees) noteUnderOwnVersion(ctx context.Context, landingDir string, bump application.VersionBump, version string) (application.Bumped, error) {
	noted, tidied, err := changelogsWith(landingDir, bump, version)
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
	return application.Bumped{Commit: commit, Tidied: tidied}, nil
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
// empty) and returns them with the bump's entries (a batch's several, or the
// one), under version, at the top, the last on top. A bump with no changelog
// files, or whose entries are empty, returns none. Nothing is written, so that
// a file that cannot take an entry leaves the landing worktree as it was.
//
// Each file is tidied before the entries go in (mw-gq6.514): a second entry
// for one version and story is taken out of the .json, and the blocks of one
// version in the .md folded into one. tidied says, a sentence each, what was.
func changelogsWith(landingDir string, bump application.VersionBump, version string) (files []changelogFile, tidied []string, err error) {
	notes := bump.Notes()
	if len(bump.Changelog) == 0 || len(notes) == 0 {
		return nil, nil, nil
	}
	for _, name := range bump.Changelog {
		content, err := os.ReadFile(filepath.Join(landingDir, filepath.FromSlash(name)))
		if err != nil && !os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("reading the changelog %s: %w", name, err)
		}
		if strings.HasSuffix(name, ".json") {
			var removed []string
			if content, removed, err = domain.TidyChangelogJSON(content); err != nil {
				return nil, nil, fmt.Errorf("the changelog %s: %w", name, err)
			}
			for _, key := range removed {
				tidied = append(tidied, fmt.Sprintf("%s held a second entry for %s: it is taken out", name, key))
			}
		} else {
			var folded []string
			content, folded = domain.TidyChangelogMarkdown(content)
			for _, twice := range folded {
				tidied = append(tidied, fmt.Sprintf("%s had %s under more than one heading: its blocks are folded into one", name, twice))
			}
		}
		for _, entry := range notes {
			entry.Version = version
			if strings.HasSuffix(name, ".json") {
				if content, err = domain.AddToChangelogJSON(content, entry); err != nil {
					return nil, nil, fmt.Errorf("the changelog %s: %w", name, err)
				}
			} else {
				content = domain.AddToChangelogMarkdown(content, entry)
			}
		}
		files = append(files, changelogFile{name: name, content: content})
	}
	return files, tidied, nil
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
