package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// movedToWords reads the id a closed epic's close reason says its open work
// moved to ('open work moved to mw-fnvjof.17').
var movedToWords = regexp.MustCompile(`(?i)\bmoved to ([A-Za-z0-9]+(?:-[A-Za-z0-9]+)+(?:\.[0-9]+)*)`)

// openParentHops bounds how far a chain of closed epics is followed.
const openParentHops = 6

// OpenParent says which bead a bead filed for a story of the epic parent hangs
// from. An open parent is the parent. A closed one drops its open children out
// of mw brief and the weighing by vision, so the bead goes where the epic said
// its open work moved ('moved to <id>' in its close reason) when that bead is
// open, else under the nearest open ancestor, else under none (standalone is
// true and parent empty). why is empty when the parent stands, and otherwise
// names the closed epic and the bead chosen (mw-gq6.1720). A tracker that
// cannot be read leaves the parent as it was.
func OpenParent(ctx context.Context, tracker WorkTracker, parent string) (chosen string, standalone bool, why string) {
	if parent == "" {
		return "", false, ""
	}
	current := parent
	var closed []string
	var successor, above string // above: the parent of the closed bead walked last
	for hop := 0; hop < openParentHops; hop++ {
		found, err := tracker.ShowBeads(ctx, []string{current})
		if err != nil {
			return parent, false, ""
		}
		if len(found) == 0 {
			// A bead the close reason names that the tracker does not know: go
			// on to the ancestors of the epic that named it.
			if hop == 0 || successor == "" {
				return parent, false, ""
			}
			closed = append(closed, fmt.Sprintf("%s is not known", current))
			if above == "" {
				return "", true, openParentWhy(parent, closed, "", "")
			}
			current, successor = above, ""
			continue
		}
		bead := found[0]
		if !bead.Closed() {
			if hop == 0 {
				return parent, false, ""
			}
			return current, false, openParentWhy(parent, closed, current, successor)
		}
		closed = append(closed, fmt.Sprintf("%s is closed (%s)", current, strings.TrimSpace(bead.CloseReason)))
		above = bead.EpicID
		if m := movedToWords.FindStringSubmatch(bead.CloseReason); m != nil && m[1] != current {
			current, successor = m[1], m[1]
			continue
		}
		if bead.EpicID == "" || bead.EpicID == current {
			return "", true, openParentWhy(parent, closed, "", "")
		}
		current, successor = bead.EpicID, ""
	}
	return parent, false, ""
}

// openParentWhy words the reason a bead is not under the parent it would have
// had: the closed beads walked past, and where it went.
func openParentWhy(parent string, closed []string, chosen, successor string) string {
	how := "its nearest open ancestor"
	switch {
	case chosen == "":
		return fmt.Sprintf("under no parent, not under %s: %s, and no open epic is above it", parent, strings.Join(closed, "; "))
	case successor != "":
		how = "where its close reason says the open work moved"
	}
	return fmt.Sprintf("under %s, not under %s: %s; %s is %s", chosen, parent, strings.Join(closed, "; "), chosen, how)
}

// UnderOpenParent moves a story about to be filed off a closed parent, as
// OpenParent says, and returns the words for why; empty when it stays.
func UnderOpenParent(ctx context.Context, tracker WorkTracker, story *NewStory) string {
	if story.Standalone || story.EpicID == "" {
		return ""
	}
	chosen, alone, why := OpenParent(ctx, tracker, story.EpicID)
	if why != "" {
		story.EpicID, story.Standalone = chosen, alone
	}
	return why
}

// underOpenParent is UnderOpenParent with its words, if any, left in the
// report's notes as what's filed.
func (n Next) underOpenParent(ctx context.Context, report *NextReport, story *NewStory, what string) {
	if why := UnderOpenParent(ctx, n.Tracker, story); why != "" {
		report.Notes = append(report.Notes, what+" is filed "+why)
	}
}
