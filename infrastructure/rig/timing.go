package rig

import (
	"context"
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// Worktrees satisfies the port a rig's timing tests and what changed are read
// through.
var _ application.TimingRig = (*Worktrees)(nil)

// Tagged implements application.TimingRig: the files of the tree at commit that
// one of the globs names (domain.Covers: a file, a directory or a glob), each
// with the globs of the covers: line in its first lines.
func (w *Worktrees) Tagged(ctx context.Context, rigDir, commit string, globs []string) ([]application.TaggedTest, error) {
	listed, err := w.git(ctx, rigDir, "ls-tree", "-r", "--name-only", "-z", commit)
	if err != nil {
		return nil, err
	}
	var tagged []application.TaggedTest
	for _, file := range strings.Split(listed, "\x00") {
		if file == "" || !anyCovers(globs, file) {
			continue
		}
		head, err := w.git(ctx, rigDir, "show", commit+":"+file)
		if err != nil {
			return nil, err
		}
		lines := strings.SplitN(head, "\n", application.TimingCoversLines+1)
		if len(lines) > application.TimingCoversLines {
			lines = lines[:application.TimingCoversLines]
		}
		tagged = append(tagged, application.TaggedTest{File: file, Covers: application.ParseCovers(strings.Join(lines, "\n"))})
	}
	return tagged, nil
}

func anyCovers(globs []string, file string) bool {
	for _, glob := range globs {
		if domain.Covers(strings.TrimPrefix(glob, "./"), file) {
			return true
		}
	}
	return false
}

// ChangedBetween implements application.TimingRig: `git diff --name-only` from one
// commit to the other, two dots.
func (w *Worktrees) ChangedBetween(ctx context.Context, rigDir, from, to string) ([]string, error) {
	out, err := w.git(ctx, rigDir, "diff", "--name-only", "-z", from+".."+to, "--")
	if err != nil {
		return nil, fmt.Errorf("what changed from %s to %s: %w", from, to, err)
	}
	var files []string
	for _, file := range strings.Split(out, "\x00") {
		if file != "" {
			files = append(files, file)
		}
	}
	return files, nil
}
