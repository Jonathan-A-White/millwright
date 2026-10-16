package application

import (
	"fmt"
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
