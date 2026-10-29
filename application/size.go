package application

import (
	"fmt"
	"io"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// NoSize is a story whose path has no size, so the triage pass has nothing to
// prioritise it by (mw-1ljpo9.3). It is a warning, never a refusal.
type NoSize struct {
	ID string
}

// Line is the warning as mw file says it.
func (n NoSize) Line() string {
	return fmt.Sprintf("%s: no size (mw path set %s size=%s)", n.ID, n.ID, sizeChoices())
}

// BigStory is a story of size L or XL that is released: bigger than one session
// is likely to finish, so it may be handed back at its turn cap. A warning
// only; splitting it is the Mayor's.
type BigStory struct {
	ID   string
	Size domain.Size
}

// Line is the warning as mw file says it.
func (b BigStory) Line() string {
	return fmt.Sprintf("%s: size %s is released: bigger than one session is likely to finish, so consider splitting it", b.ID, b.Size)
}

// sizeChoices is the sizes as a path key's value is written.
func sizeChoices() string {
	names := make([]string, 0, len(domain.Sizes))
	for _, size := range domain.Sizes {
		names = append(names, string(size))
	}
	return strings.Join(names, "|")
}

// sizeWarnings are the warnings about the sizes of a filed plan's stories: of
// each with no size when missing is set, and of each of size L or XL when big is
// set, which is when the plan was released.
func sizeWarnings(stories []FiledStory, missing, big bool) string {
	var b strings.Builder
	for _, story := range stories {
		switch {
		case missing && story.Path.Size == "":
			b.WriteString(NoSize{ID: story.ID}.Line() + "\n")
		case big && story.Path.Size.Big():
			b.WriteString(BigStory{ID: story.ID, Size: story.Path.Size}.Line() + "\n")
		}
	}
	return b.String()
}

// FilesNamed is the count of the files a story's files key names against its
// size (mw-gq6.839). Test callers (TestCallers) miss a story that is wide in
// itself: an M story naming 10 to 30 files, across screens, grinds, schemas,
// examples, features and docs, ran out of turns and landed on its second attempt,
// 17 times at Lampas between 2026-10-23 and 2026-10-28. A warning only: the
// Mayor judges.
type FilesNamed struct {
	// Label is how the story is named: its plan key, or its id.
	Label string
	// Count is the files the key names: each plain name one, a glob or a
	// directory the files it matches in the rig's checkout on this host.
	Count int
	Size  domain.Size
}

// Allows is the most files the size allows.
func (n FilesNamed) Allows() int { return n.Size.FilesAllowed() }

// Over says the count is more than the size allows.
func (n FilesNamed) Over() bool { return n.Count > n.Allows() }

// Warning is what is said of a story over its size's allowance.
func (n FilesNamed) Warning() string {
	return fmt.Sprintf("WARN: files named: %d for size %s (allows %d); split the story or size it up, for %s",
		n.Count, n.Size, n.Allows(), n.Label)
}

// filesNamedOf counts the files a story worked by this path names, and says
// whether it could: a story with no size or no files key is none to judge. A
// glob or a directory counts as the files it matches in the rig's checkout; with
// no checkout on this host it counts as one.
func filesNamedOf(rigs map[string]string, files RigFiles, label string, path domain.Path) (FilesNamed, bool) {
	if !domain.KnownSize(path.Size) || strings.TrimSpace(path.Files) == "" {
		return FilesNamed{}, false
	}
	rigDir, here := rigs[strings.TrimSpace(path.Rig)]
	here = here && rigDir != "" && files != nil
	count := 0
	for _, named := range domain.Directories(path.Files) {
		count++
		if here {
			if n, isDir := files.FilesIn(rigDir, named.Dir, mostGlobCount); isDir {
				count += n - 1
			}
		}
	}
	for _, glob := range domain.Globs(path.Files) {
		count++
		if here {
			if n := files.Matching(rigDir, glob.Pattern, mostGlobCount); n > 1 {
				count += n - 1
			}
		}
	}
	return FilesNamed{Label: label, Count: count, Size: path.Size}, true
}

// filesNamedNote warns to warn of a story over its size's allowance of files,
// and says nothing of one within it. It reports whether it warned.
func filesNamedNote(warn io.Writer, named FilesNamed) bool {
	if !named.Over() || warn == nil {
		return false
	}
	fmt.Fprintln(warn, named.Warning())
	return true
}
