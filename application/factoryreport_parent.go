package application

import (
	"fmt"
	"strings"
	"time"
)

// ParentNoteMember is what the parent's note calls the member when none is
// named.
const ParentNoteMember = "the member"

// epicsMoved counts the epics that had a story closed or claimed in the window,
// leaving a Tester's stories out; it names none.
func (r FactoryReportResult) epicsMoved(stories []StoryDetail) int {
	inWindow := func(at time.Time) bool {
		if at.IsZero() {
			return false
		}
		previous, inside := r.windowOfTime(at)
		return inside && !previous
	}
	moved := map[string]bool{}
	for _, d := range stories {
		if d.IsEpic || d.EpicID == "" || IsTesterStory(d) {
			continue
		}
		if inWindow(d.ClosedAt) || inWindow(d.ClaimedAt) {
			moved[d.EpicID] = true
		}
	}
	return len(moved)
}

// ParentNote is the report as a parent reads it, in plain words: how many
// epics moved, roughly how much landed, whether something did not pass its
// checks, and how many things wait on a decision. It carries no bead id, story
// title, rig, model or fuel figure, only counts and words. A window in which
// nothing moved is one line. name is the member, as the parent calls them.
func (r FactoryReportResult) ParentNote(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = ParentNoteMember
	}
	var landed, refused int
	if r.Whole != nil {
		landed, refused = r.Whole.Quality.Landed, r.Whole.Quality.Refused
	}
	waiting := 0
	if r.Review != nil {
		waiting = r.Review.Queue
	}
	waitingText := ""
	switch {
	case waiting == 1:
		waitingText = "1 thing is waiting on a decision."
	case waiting > 1:
		waitingText = fmt.Sprintf("%d things are waiting on a decision.", waiting)
	}

	if landed == 0 && r.EpicsMoved == 0 {
		line := fmt.Sprintf("Nothing moved in %s's factory since yesterday.", name)
		if waitingText != "" {
			line = strings.TrimSuffix(line, ".") + "; " + strings.ToLower(waitingText[:1]) + waitingText[1:]
		}
		return line + "\n"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s's factory, %s\n", name, r.Window.Until.UTC().Format(LedgerDate))
	areas := "no area of work"
	switch {
	case r.EpicsMoved == 1:
		areas = "1 area of work"
	case r.EpicsMoved > 1:
		areas = fmt.Sprintf("%d areas of work", r.EpicsMoved)
	}
	fmt.Fprintf(&b, "%s worked on %s and finished %s.\n", name, areas, howMuch(landed))
	if refused > 0 {
		b.WriteString("Some work did not pass its checks and will be tried again.\n")
	}
	if waitingText != "" {
		b.WriteString(waitingText + "\n")
	}
	return b.String()
}

// howMuch says in words how much landed.
func howMuch(landed int) string {
	switch {
	case landed == 0:
		return "nothing yet"
	case landed <= 2:
		return "a little"
	case landed <= 6:
		return "a fair amount"
	}
	return "a lot"
}
