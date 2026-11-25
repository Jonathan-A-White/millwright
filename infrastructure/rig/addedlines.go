package rig

import (
	"context"
	"strconv"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.BranchAddedLines = (*Worktrees)(nil)

// AddedLines implements application.BranchAddedLines: the lines `git diff -U0`
// finds added, from the merge base of base and branch to branch, each with its
// number in the file at branch's tip. A line removed or left as context is not
// one, and a branch the rig does not have has added none.
func (w *Worktrees) AddedLines(ctx context.Context, rigDir, base, branch string) ([]application.AddedLine, error) {
	if _, err := w.git(ctx, rigDir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err != nil {
		return nil, nil
	}
	out, err := w.git(ctx, rigDir, "diff", "-U0", "--no-color", "--no-renames", "--src-prefix=a/", "--dst-prefix=b/",
		base+"..."+branch, "--")
	if err != nil {
		return nil, err
	}
	var lines []application.AddedLine
	var path string
	number := 0
	inHunk := false
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			inHunk = false
		case !inHunk && strings.HasPrefix(line, "+++ "):
			path = ""
			if added, ok := strings.CutPrefix(line, "+++ b/"); ok {
				path = added
			}
		case strings.HasPrefix(line, "@@"):
			inHunk = true
			number = hunkStart(line)
		case inHunk && strings.HasPrefix(line, "+"):
			lines = append(lines, application.AddedLine{Path: path, Number: number, Text: line[1:]})
			number++
		}
	}
	return lines, nil
}

// hunkStart is the first line a hunk header's new side covers: the c of
// "@@ -a,b +c,d @@".
func hunkStart(header string) int {
	_, rest, _ := strings.Cut(header, " +")
	field, _, _ := strings.Cut(rest, " ")
	start, _, _ := strings.Cut(field, ",")
	n, err := strconv.Atoi(start)
	if err != nil {
		return 0
	}
	return n
}
