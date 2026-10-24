package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// FilesOutsideTheKey is the opening of the comment a landing leaves on a story
// whose branch changed files its files key did not name (mw-gq6.406), so the
// Mayor can widen the keys of stories like it.
const FilesOutsideTheKey = "FILES OUTSIDE THE KEY:"

// recordFilesChanged writes the files the story's branch has changed from its
// target branch to the story's metadata (FilesChangedField), where a dispatch on
// any host reads them with the files key. It writes only when they differ from
// what is recorded, and a branch that cannot be read, or a write that fails, is
// a note on Out: the next tick tries again.
func (n Next) recordFilesChanged(ctx context.Context, storyID string) {
	if n.Changes == nil {
		return
	}
	detail, err := n.Tracker.ShowStory(ctx, storyID)
	if err != nil {
		n.print(fmt.Sprintf("mw next: %s could not be read to record the files its branch changed: %v\n", storyID, err))
		return
	}
	path := detail.Merged()
	rigDir, checkedOut := n.Rigs[path.Rig]
	if !checkedOut || path.Branch == "" {
		return
	}
	files, err := n.Changes.ChangedFiles(ctx, rigDir, StartPoint(n.remote(), path.Branch), StoryBranch(storyID))
	if err != nil {
		n.print(fmt.Sprintf("mw next: the files the branch of %s changed could not be read: %v\n", storyID, err))
		return
	}
	if strings.Join(files, ",") == strings.Join(detail.FilesChanged, ",") {
		return
	}
	if err := n.Tracker.SetStoryMetadata(ctx, storyID, map[string]string{FilesChangedField: strings.Join(files, ",")}); err != nil {
		n.print(fmt.Sprintf("mw next: the files the branch of %s changed could not be recorded: %v\n", storyID, err))
	}
}

// readFilesOutsideTheKey is what the story's branch changes that its files key
// does not name, read before the landing moves the target branch: afterwards the
// branch is part of it and changes nothing. A story with no files key names
// nothing to be outside of, and a branch that cannot be read is a note.
func (n Next) readFilesOutsideTheKey(ctx context.Context, c *closeOut, report *NextReport) {
	key := domain.FileList(c.path.Files)
	if n.Changes == nil || len(key) == 0 {
		return
	}
	files, err := n.Changes.ChangedFiles(ctx, c.rigDir, StartPoint(n.remote(), c.target), c.branch)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the files %s changed could not be read to compare with its files key: %v", c.id, err))
		return
	}
	for _, file := range files {
		if _, named := domain.SharedFile(key, []string{file}); !named {
			c.outsideKey = append(c.outsideKey, file)
		}
	}
}

// sayFilesOutsideTheKey leaves the comment readFilesOutsideTheKey's files are
// named in; none, no comment.
func (n Next) sayFilesOutsideTheKey(ctx context.Context, c *closeOut, report *NextReport) {
	if len(c.outsideKey) == 0 {
		return
	}
	text := fmt.Sprintf("%s %s", FilesOutsideTheKey, strings.Join(c.outsideKey, ", "))
	n.writeOrSay(ctx, c, report, "the files outside the key", fmt.Sprintf("the files %s changed outside its files key could not be commented", c.id),
		func() error { return n.Tracker.CommentOnStory(ctx, c.id, text) })
}

// MaxTrackedBytes is the size past which a file a branch adds is turned away
// (mw-gq6.517); scripts/check-tracked-binaries.sh holds the repository to the
// same figure.
const MaxTrackedBytes = 2 << 20

// trackedBinaryRefusal is the refusal of a branch that adds an executable at the
// repo root or a file over MaxTrackedBytes outside testdata/: build output left
// by a session, which a landing would put on the target branch for good. A rig
// with no Changes, or a branch that cannot be read, is not refused: the check
// is a net and the rig's own gate runs after it.
func (n Next) trackedBinaryRefusal(ctx context.Context, c *closeOut, report *NextReport) (Refusal, bool) {
	if n.Changes == nil {
		return Refusal{}, false
	}
	added, err := n.Changes.AddedFiles(ctx, c.rigDir, StartPoint(n.remote(), c.target), c.branch)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the files %s added could not be read to look for build output: %v", c.id, err))
		return Refusal{}, false
	}
	var found []string
	for _, file := range added {
		switch {
		case file.Executable:
			found = append(found, fmt.Sprintf("- %s is an executable at the repo root", file.Path))
		case file.Size > MaxTrackedBytes && !strings.HasPrefix(file.Path, "testdata/") && !strings.Contains(file.Path, "/testdata/"):
			found = append(found, fmt.Sprintf("- %s is %d bytes, over the %d a landing takes outside testdata/", file.Path, file.Size, MaxTrackedBytes))
		}
	}
	if len(found) == 0 {
		return Refusal{}, false
	}
	why := fmt.Sprintf("%s adds build output or a very large file: %s", c.branch, strings.TrimPrefix(found[0], "- "))
	said := fmt.Sprintf("The file(s) the branch adds:\n\n%s\n\nA binary or a big file on the target branch stays in every clone for good. "+
		"Nothing was merged: the branch is still the session's to amend. Take it out of the index "+
		"(`git rm --cached <path>`), ignore it in .gitignore if it is build output, amend the commit(s) that added it "+
		"(`git rebase -i %s`), and run `mw next %s` again.",
		strings.Join(found, "\n"), StartPoint(n.remote(), c.target), c.id)
	return Refusal{ReasonTrackedBinary, why, said}, true
}

// lockfileName is the file a git+ssh:// line is looked for in, at the repo root
// or in any workspace; lockfileSSH is the scheme `npm install github:<owner>/<repo>`
// writes, which a machine without a GitHub SSH key cannot install.
const (
	lockfileName = "package-lock.json"
	lockfileSSH  = "git+ssh://"
	githubSSH    = "git+ssh://git@github.com/"
	// lockfileQuoted is how many of a file's git+ssh lines the refusal quotes.
	lockfileQuoted = 3
)

// lockfileSSHRefusal is the refusal of a branch that adds a git+ssh:// line to a
// package-lock.json (mw-gq6.740). Only lines the branch adds are counted, net of
// the same line it removes from the same file: ones already on the target branch
// are not the story's, a branch that turns them into git+https is fine, and a
// line that merely moves is not new. A rig with no Changes, no lockfile, or a
// branch that cannot be read is not refused: the check is a net.
func (n Next) lockfileSSHRefusal(ctx context.Context, c *closeOut, report *NextReport) (Refusal, bool) {
	if n.Changes == nil {
		return Refusal{}, false
	}
	lines, err := n.Changes.DiffLines(ctx, c.rigDir, StartPoint(n.remote(), c.target), c.branch, lockfileName)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the %s lines %s changed could not be read to look for %s: %v", lockfileName, c.id, lockfileSSH, err))
		return Refusal{}, false
	}
	type key struct{ path, text string }
	net := map[key]int{}
	var order []key
	for _, line := range lines {
		if !strings.Contains(line.Text, lockfileSSH) {
			continue
		}
		k := key{line.Path, line.Text}
		if _, seen := net[k]; !seen {
			order = append(order, k)
		}
		if line.Added {
			net[k]++
		} else {
			net[k]--
		}
	}
	var paths []string
	counts := map[string]int{}
	quoted := map[string][]string{}
	foreign := false
	for _, k := range order {
		if net[k] <= 0 {
			continue
		}
		if counts[k.path] == 0 {
			paths = append(paths, k.path)
		}
		counts[k.path] += net[k]
		if len(quoted[k.path]) < lockfileQuoted {
			quoted[k.path] = append(quoted[k.path], "    "+strings.TrimSpace(k.text))
		}
		foreign = foreign || !strings.Contains(k.text, githubSSH)
	}
	if len(paths) == 0 {
		return Refusal{}, false
	}
	var found, fixes []string
	total := 0
	for _, path := range paths {
		total += counts[path]
		found = append(found, fmt.Sprintf("- %s has %d added line(s) with %s", path, counts[path], lockfileSSH))
		found = append(found, quoted[path]...)
		if counts[path] > len(quoted[path]) {
			found = append(found, fmt.Sprintf("    ... and %d more", counts[path]-len(quoted[path])))
		}
		fixes = append(fixes, fmt.Sprintf("sed -i 's#%s#git+https://github.com/#g' %s", githubSSH, path))
	}
	why := fmt.Sprintf("%s adds %d line(s) with %s to %s", c.branch, total, lockfileSSH, strings.Join(paths, ", "))
	said := fmt.Sprintf("The lockfile(s) the branch changes:\n\n%s\n\n"+
		"`npm install github:<owner>/<repo>#<sha>` writes the dependency as git+ssh://, and a machine with no GitHub SSH key "+
		"(CI, a fresh host) cannot `npm ci` it. Nothing was merged: the branch is still the session's to amend. "+
		"In the worktree run:\n\n%s\n\nthen commit the lockfile (amend the commit that added it, or add a commit) and run `mw next %s` again.",
		strings.Join(found, "\n"), strings.Join(fixes, "\n"), c.id)
	if foreign {
		said += " A line for a host other than github.com needs the same rewrite to git+https://<host>/ by hand: the sed above leaves it as it is."
	}
	return Refusal{ReasonLockfileSSH, why, said}, true
}
