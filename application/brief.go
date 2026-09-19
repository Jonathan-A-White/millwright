package application

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// The headings of the groups a brief lists a bead's unfinished children under,
// in the order it lists them. A child that is neither in progress nor held —
// open, or in any other state the tracker has that is not finished — is Open.
const (
	BriefInProgressHeading = "In progress"
	BriefOpenHeading       = "Open"
	BriefHeldHeading       = "Held"
)

// BriefWaitWidth is the most a title is given when a line names it as what a
// child waits on: the blocker is listed in full wherever it is live, and a
// brief that named every one of them whole would be most of what it saves.
const BriefWaitWidth = 24

// Brief prints what a booting seat still needs of the beads it is pointed at:
// for each, its title, id, status and priority, its children that are not
// closed — in progress, open, held — each with its priority, the host and model
// its Path names and what it waits on, and how many children are closed. It
// leaves out the description and every closed child, which is most of what
// `bd show` would print of a map. A title is cut to Width runes, and a blocker's
// to BriefWaitWidth, so that three maps fit a phone's screen and a seat's boot.
// With Comments set, the newest comments of the bead follow in full: they hold
// the Governor's words, so nothing clips them.
//
// It only reads: every call it makes is a ShowEpic, a ShowStory or a
// StoryComments, and nothing is written to the tracker.
type Brief struct {
	Tracker WorkTracker

	// Comments is how many of the newest comments of each bead to print. Zero
	// prints none.
	Comments int

	// Out is where the brief is printed. A nil Out prints nothing.
	Out io.Writer
}

// BeadBrief is one bead as a brief shows it.
type BeadBrief struct {
	ID       string
	Title    string
	Status   string
	Priority int

	InProgress []BriefLine
	Open       []BriefLine
	Held       []BriefLine
	// Closed is how many children are finished, whose lines are left out.
	Closed int

	// Comments are the newest comments asked for, oldest first, and
	// CommentsTotal is how many the bead has altogether.
	Comments      []Comment
	CommentsTotal int
}

// BriefLine is one unfinished child of a bead, with the titles of the stories
// it is still waiting on.
type BriefLine struct {
	Story StoryDetail
	// Waits are the titles of what the child waits on that is not finished.
	Waits []string
}

// BriefReport is every bead a brief was asked for, in the order it was asked.
type BriefReport struct {
	Beads []BeadBrief
}

// Run reads the beads named and prints their briefs, in the order given. Every
// bead is read before any is printed, so that a bead the tracker does not have
// is a refusal naming it rather than half a brief followed by an error.
func (b Brief) Run(ctx context.Context, ids ...string) (BriefReport, error) {
	if b.Tracker == nil {
		return BriefReport{}, fmt.Errorf("briefing: there is no work tracker to read from")
	}
	if len(ids) == 0 {
		return BriefReport{}, fmt.Errorf("briefing: which bead?")
	}
	if b.Comments < 0 {
		return BriefReport{}, fmt.Errorf("briefing: cannot print %d comments", b.Comments)
	}

	lookup := map[string]StoryDetail{}
	var report BriefReport
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			return BriefReport{}, fmt.Errorf("briefing: which bead?")
		}
		one, err := b.bead(ctx, id, lookup)
		if err != nil {
			return BriefReport{}, err
		}
		report.Beads = append(report.Beads, one)
	}

	if b.Out != nil {
		fmt.Fprint(b.Out, report.String())
	}
	return report, nil
}

// bead reads one bead and sorts its children into the groups a brief shows.
// lookup remembers the stories already read, so that a story many children
// wait on is read once.
func (b Brief) bead(ctx context.Context, id string, lookup map[string]StoryDetail) (BeadBrief, error) {
	epic, err := b.Tracker.ShowEpic(ctx, id)
	if err != nil {
		return BeadBrief{}, fmt.Errorf("briefing %s: %w", id, err)
	}
	brief := BeadBrief{ID: epic.ID, Title: epic.Title, Status: epic.Status, Priority: epic.Priority}

	for _, child := range epic.Stories {
		lookup[child.Story.ID] = child
	}
	for _, child := range epic.Stories {
		if child.Closed() {
			brief.Closed++
			continue
		}
		waits, err := b.waitsOn(ctx, child, lookup)
		if err != nil {
			return BeadBrief{}, fmt.Errorf("briefing %s: %w", id, err)
		}
		line := BriefLine{Story: child, Waits: waits}
		switch {
		case strings.EqualFold(strings.TrimSpace(child.Status), StatusInProgress):
			brief.InProgress = append(brief.InProgress, line)
		case child.Held():
			brief.Held = append(brief.Held, line)
		default:
			brief.Open = append(brief.Open, line)
		}
	}

	if b.Comments > 0 {
		comments, err := b.Tracker.StoryComments(ctx, id)
		if err != nil {
			return BeadBrief{}, fmt.Errorf("briefing %s: reading its comments: %w", id, err)
		}
		brief.CommentsTotal = len(comments)
		if len(comments) > b.Comments {
			comments = comments[len(comments)-b.Comments:]
		}
		brief.Comments = comments
	}
	return brief, nil
}

// waitsOn is the titles of what a child still waits on. A blocker among the
// stories already read is known to be finished or not; one elsewhere is read
// from the tracker, because a listing of children says which stories a child
// waits on but not whether they are done. A blocker that is finished is not a
// wait, and is not named.
func (b Brief) waitsOn(ctx context.Context, child StoryDetail, lookup map[string]StoryDetail) ([]string, error) {
	var titles []string
	for _, need := range child.Needs {
		blocker, known := lookup[need]
		if !known {
			read, err := b.Tracker.ShowStory(ctx, need)
			if err != nil {
				return nil, fmt.Errorf("reading %s, which %s waits on: %w", need, child.Story.ID, err)
			}
			blocker = read
			lookup[need] = blocker
		}
		if blocker.Closed() {
			continue
		}
		title := blocker.Story.Title
		if title == "" {
			title = need
		}
		titles = append(titles, title)
	}
	return titles, nil
}

// String is the brief as a person reads it on a phone: plain text, a blank line
// between one bead and the next. A title is clipped and a comment never is.
func (r BriefReport) String() string {
	var b strings.Builder
	for i, one := range r.Beads {
		if i > 0 {
			b.WriteString("\n")
		}
		one.write(&b)
	}
	return b.String()
}

// write is one bead's brief.
func (o BeadBrief) write(b *strings.Builder) {
	fmt.Fprintf(b, "%s · %s · %s · P%d\n", clipped(o.Title), o.ID, o.Status, o.Priority)
	writeGroup(b, BriefInProgressHeading, o.InProgress)
	writeGroup(b, BriefOpenHeading, o.Open)
	writeGroup(b, BriefHeldHeading, o.Held)
	fmt.Fprintf(b, "%d closed\n", o.Closed)

	if len(o.Comments) == 0 {
		return
	}
	fmt.Fprintf(b, "\nComments (newest %d of %d)\n", len(o.Comments), o.CommentsTotal)
	for _, c := range o.Comments {
		b.WriteString("— " + c.byline() + "\n")
		b.WriteString(strings.TrimRight(c.Text, "\n") + "\n")
	}
}

// byline is who left a comment and when, as far as the tracker said.
func (c Comment) byline() string {
	var parts []string
	if c.Author != "" {
		parts = append(parts, c.Author)
	}
	if !c.Created.IsZero() {
		parts = append(parts, c.Created.UTC().Format("2006-01-02 15:04Z"))
	}
	if len(parts) == 0 {
		return "unsigned"
	}
	return strings.Join(parts, " · ")
}

// writeGroup is one heading and the lines under it, or nothing when the group
// is empty.
func writeGroup(b *strings.Builder, heading string, lines []BriefLine) {
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(b, "%s (%d)\n", heading, len(lines))
	for _, l := range lines {
		b.WriteString("  " + l.String() + "\n")
	}
}

// String is one child on one line: its title, id and priority, the host and
// model its Path names where it names them, and each story it waits on.
func (l BriefLine) String() string {
	path := l.Story.Merged()
	parts := []string{clipped(l.Story.Story.Title), l.Story.Story.ID, fmt.Sprintf("P%d", l.Story.Priority)}
	if path.Host != "" {
		parts = append(parts, path.Host)
	}
	if path.Model != "" {
		parts = append(parts, string(path.Model))
	}
	for _, title := range l.Waits {
		parts = append(parts, "waits on "+clippedTo(title, BriefWaitWidth))
	}
	return strings.Join(parts, " · ")
}
