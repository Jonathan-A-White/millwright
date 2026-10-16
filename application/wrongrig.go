package application

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// RigFiles is the port by which a rig's checkout on this host says whether a
// path of its files key is there: a file, a directory, or a glob that matches
// something; and how many files a directory holds. Asking changes nothing.
type RigFiles interface {
	Exists(rigDir, pattern string) bool

	// FilesIn is how many files are under dir in the checkout at rigDir, all of
	// it when dir is "", counted no further than one past most, and whether dir
	// is a directory there at all (mw-gq6.442).
	FilesIn(rigDir, dir string, most int) (int, bool)
}

// WrongRig is a story whose files key names paths that exist in none of its
// rig's checkout on this host, nor their top-level directories, which is what a
// story filed into the wrong rig looks like (mw-gq6.428, mw-ev7oyy.12). A story
// of new files in new top-level directories would look the same, so it is a
// warning, never a refusal.
type WrongRig struct {
	ID  string
	Rig string
}

// Line is the warning as mw status, mw release and mw file say it.
func (w WrongRig) Line() string {
	return fmt.Sprintf("%s: none of its files exist in %s: wrong rig?", w.ID, w.Rig)
}

// wrongRigOf is whether a story of this path names files and none of them is in
// its rig's checkout. A rig not checked out here cannot be judged, and a story
// that names no files, or one that is a tester's, is not in question.
func wrongRigOf(rigs map[string]string, files RigFiles, path domain.Path) bool {
	rig := strings.TrimSpace(path.Rig)
	dir, here := rigs[rig]
	if files == nil || !here || dir == "" {
		return false
	}
	named := domain.FileList(path.Files)
	if len(named) == 0 {
		return false
	}
	for _, name := range named {
		if inRig(files, dir, name) {
			return false
		}
	}
	return true
}

// inRig is whether a path of a files key belongs in the checkout at dir: it is
// there, it is a name at the checkout's root, or its top-level directory is
// there. A story names files it will create (docs/research/new.md in
// millwright, src/handwriting/*.ts in lampas), and a new file under a
// directory the rig has is in it; one under src/ in a rig with no src/ is not
// (mw-gq6.448).
func inRig(files RigFiles, dir, name string) bool {
	top, _, nested := strings.Cut(name, "/")
	return files.Exists(dir, name) || !nested || files.Exists(dir, top)
}

// wrongRigStories is the stories among details that are open, held or running,
// are worked by a session, and whose files are none of them in their rig; each
// once, in order.
func wrongRigStories(rigs map[string]string, files RigFiles, details ...StoryDetail) []WrongRig {
	var found []WrongRig
	seen := map[string]bool{}
	for _, d := range details {
		id := d.Story.ID
		if seen[id] || d.IsEpic || d.Hitl() ||
			!slices.Contains([]string{StateOpen, StateHeld, StateInProgress}, StateOf(d.Status)) {
			continue
		}
		seen[id] = true
		if path := d.Merged(); wrongRigOf(rigs, files, path) {
			found = append(found, WrongRig{ID: id, Rig: strings.TrimSpace(path.Rig)})
		}
	}
	return found
}

// warnWrongRig prints the warning for each story of details filed into a rig
// that has none of its files.
func warnWrongRig(out io.Writer, rigs map[string]string, files RigFiles, details ...StoryDetail) {
	if out == nil {
		return
	}
	for _, w := range wrongRigStories(rigs, files, details...) {
		fmt.Fprintln(out, w.Line())
	}
}
