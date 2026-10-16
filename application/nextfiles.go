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
