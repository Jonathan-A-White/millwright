package application

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

// FoldWriter is the tracker narrowed to what mw fold supersede does: it reads
// every story, moves the stories that wait on a folded bug to wait on the fold,
// and closes the bug as superseded by the fold.
type FoldWriter interface {
	TesterStories
	HoldStory(ctx context.Context, id string) error
	ReleaseStory(ctx context.Context, id string) error
	// AddBlocker makes blocker block blocked; the tracker refuses one that would
	// make a cycle, with an error that says so.
	AddBlocker(ctx context.Context, blocked, blocker string) error
	// RemoveBlocker takes the wait of blocked on blocker away.
	RemoveBlocker(ctx context.Context, blocked, blocker string) error
	// SupersedeStory closes id as superseded by with.
	SupersedeStory(ctx context.Context, id, with string) error
}

// RequestMover moves the request linked to a bead to another bead, telling its
// sender (GristRequest is the one that does).
type RequestMover interface {
	// Linked reports whether the bead is linked to a request this host follows.
	Linked(ctx context.Context, bead string) (bool, error)
	// MoveRequest moves the request linked to from to to.
	MoveRequest(ctx context.Context, from, to string) error
}

// FoldSupersede is `mw fold supersede <fold>`: each bug under 'Folded bugs' in
// the fold's description is closed as superseded by the fold, after whatever
// waited on it is moved to wait on the fold. A bug that a session works, one
// already closed, and one whose id is not in the fold's acceptance criteria are
// left alone and said. With DryRun it writes nothing and prints the same plan
// (mw-gq6.606).
type FoldSupersede struct {
	Tracker FoldWriter
	// Requests moves a folded bug's linked request to the fold; nil moves none.
	Requests RequestMover
	Fold     string
	DryRun   bool
	Out      io.Writer
}

// FoldSupersedeBug is what became of one folded bug.
type FoldSupersedeBug struct {
	ID string `json:"id"`
	// Outcome is superseded, in progress, already closed, not in the acceptance
	// criteria or not found.
	Outcome string `json:"outcome"`
	// Moved is the stories now waiting on the fold in its place; LeftOut the
	// ones whose edge would make a cycle and was not made.
	Moved   []string `json:"moved,omitempty"`
	LeftOut []string `json:"left_out,omitempty"`
	// RequestMoved is true when the bug's linked request moved to the fold
	// (would, in a dry run); RequestLeft says why one stayed on the bug.
	RequestMoved bool   `json:"request_moved,omitempty"`
	RequestLeft  string `json:"request_left,omitempty"`
}

// FoldSupersedeReport is what mw fold supersede did, or would do.
type FoldSupersedeReport struct {
	Fold   string             `json:"fold"`
	DryRun bool               `json:"dry_run,omitempty"`
	Bugs   []FoldSupersedeBug `json:"bugs"`
}

const (
	foldSuperseded  = "superseded"
	foldInProgress  = "in progress"
	foldClosed      = "already closed"
	foldNotInAC     = "not in the fold's acceptance criteria"
	foldNotFound    = "not found"
	foldBugsHeading = "folded bugs"
)

var (
	foldBulletID = regexp.MustCompile(`^\s*[•*-]\s*([A-Za-z0-9][A-Za-z0-9._-]*?)\s*:`)
	foldIDToken  = regexp.MustCompile(`[A-Za-z0-9]+(?:-[A-Za-z0-9]+)+(?:\.[A-Za-z0-9]+)*`)
)

// foldedBugIDs is the ids under the 'Folded bugs' heading of a fold's
// description: the bullet lines after it, up to the first blank line that
// follows them.
func foldedBugIDs(description string) []string {
	var ids []string
	inList, started := false, false
	for _, line := range strings.Split(description, "\n") {
		if !inList {
			inList = strings.Contains(strings.ToLower(line), foldBugsHeading)
			continue
		}
		if strings.TrimSpace(line) == "" {
			if started {
				break
			}
			continue
		}
		if m := foldBulletID.FindStringSubmatch(line); m != nil {
			ids = append(ids, m[1])
			started = true
		}
	}
	return ids
}

// Run reads the fold and its bugs and does, or with DryRun only says, the
// moves and the supersedes, one line per bug. It stops at the first write the
// tracker refuses, other than a cycle, and says which.
func (f FoldSupersede) Run(ctx context.Context) (FoldSupersedeReport, error) {
	report := FoldSupersedeReport{Fold: f.Fold, DryRun: f.DryRun, Bugs: []FoldSupersedeBug{}}
	if f.Tracker == nil {
		return report, fmt.Errorf("superseding the bugs of %s: there is no work tracker to read", f.Fold)
	}
	stories, err := f.Tracker.StoriesSince(ctx, time.Time{})
	if err != nil {
		return report, fmt.Errorf("reading the stories to supersede the bugs of %s: %w", f.Fold, err)
	}
	byID := map[string]StoryDetail{}
	needs := map[string][]string{}
	for _, d := range stories {
		byID[d.Story.ID] = d
		needs[d.Story.ID] = append([]string(nil), d.Needs...)
	}
	fold, ok := byID[f.Fold]
	if !ok {
		return report, fmt.Errorf("superseding the bugs of %s: there is no such story", f.Fold)
	}
	if strings.ToLower(strings.TrimSpace(fold.Status)) == StatusClosed {
		return report, fmt.Errorf("superseding the bugs of %s: it is closed", f.Fold)
	}
	ids := foldedBugIDs(fold.Description)
	if len(ids) == 0 {
		return report, fmt.Errorf("superseding the bugs of %s: its description has no 'Folded bugs' list", f.Fold)
	}
	inFold := map[string]bool{}
	for _, id := range ids {
		inFold[id] = true
	}
	inAcceptance := map[string]bool{}
	for _, token := range foldIDToken.FindAllString(fold.Acceptance, -1) {
		inAcceptance[token] = true
	}
	// the open folds other than this one, with the bugs each carries
	otherFold := map[string]string{}
	for _, d := range stories {
		if d.Story.ID == f.Fold || d.IsEpic || !strings.Contains(strings.ToLower(d.Description), foldBugsHeading) || strings.ToLower(strings.TrimSpace(d.Status)) == StatusClosed {
			continue
		}
		for _, id := range foldedBugIDs(d.Description) {
			if _, taken := otherFold[id]; !taken {
				otherFold[id] = d.Story.ID
			}
		}
	}
	if f.DryRun && f.Out != nil {
		fmt.Fprintf(f.Out, "dry run: nothing is written; this is the plan for %s\n", f.Fold)
	}
	var failure error
	for _, id := range ids {
		bug := FoldSupersedeBug{ID: id}
		d, found := byID[id]
		switch status := strings.ToLower(strings.TrimSpace(d.Status)); {
		case !found:
			bug.Outcome = foldNotFound
		case status == StatusClosed:
			bug.Outcome = foldClosed
		case status == StatusInProgress:
			bug.Outcome = foldInProgress
		case !inAcceptance[id]:
			bug.Outcome = foldNotInAC
		default:
			bug.Outcome = foldSuperseded
		}
		if bug.Outcome == foldSuperseded {
			failure = f.supersede(ctx, &bug, byID, needs, inFold, otherFold)
		}
		report.Bugs = append(report.Bugs, bug)
		f.printBug(bug, failure)
		if failure != nil {
			break
		}
	}
	return report, failure
}

// supersede moves what waits on the bug to wait on the fold, then closes the
// bug as superseded by it. needs is the waits as read, kept up to date as edges
// are made.
func (f FoldSupersede) supersede(ctx context.Context, bug *FoldSupersedeBug, byID map[string]StoryDetail, needs map[string][]string, inFold map[string]bool, otherFold map[string]string) error {
	var dependents []string
	for id, waits := range needs {
		if id == f.Fold || inFold[id] || !slices.Contains(waits, bug.ID) || strings.ToLower(strings.TrimSpace(byID[id].Status)) == StatusClosed {
			continue
		}
		dependents = append(dependents, id)
	}
	sort.Strings(dependents)
	moved := map[string]bool{}
	for _, id := range dependents {
		// a bug of another fold stays as it is: its fold waits on this one
		waiter := id
		if other, folded := otherFold[id]; folded {
			waiter = other
		}
		if moved[waiter] || slices.Contains(needs[waiter], f.Fold) {
			continue
		}
		moved[waiter] = true
		if reaches(needs, f.Fold, waiter) {
			bug.LeftOut = append(bug.LeftOut, waiter)
			continue
		}
		left, err := f.moveToFold(ctx, waiter, byID[waiter])
		if err != nil {
			return err
		}
		if left {
			bug.LeftOut = append(bug.LeftOut, waiter)
			continue
		}
		needs[waiter] = append(needs[waiter], f.Fold)
		bug.Moved = append(bug.Moved, waiter)
	}
	if f.Requests != nil {
		linked, err := f.Requests.Linked(ctx, bug.ID)
		switch {
		case err != nil:
			bug.RequestLeft = err.Error()
		case linked && f.DryRun:
			bug.RequestMoved = true
		case linked:
			// Not fatal: a request that cannot move stays on the bug, said.
			if err := f.Requests.MoveRequest(ctx, bug.ID, f.Fold); err != nil {
				bug.RequestLeft = err.Error()
			} else {
				bug.RequestMoved = true
			}
		}
	}
	if f.DryRun {
		return nil
	}
	if slices.Contains(needs[bug.ID], f.Fold) {
		if err := f.Tracker.RemoveBlocker(ctx, bug.ID, f.Fold); err != nil {
			return fmt.Errorf("taking %s's wait on %s away: %w", bug.ID, f.Fold, err)
		}
	}
	if err := f.Tracker.SupersedeStory(ctx, bug.ID, f.Fold); err != nil {
		return fmt.Errorf("superseding %s by %s: %w", bug.ID, f.Fold, err)
	}
	return nil
}

// moveToFold makes the fold block waiter, holding an open waiter while the edge
// is added so a dispatch cannot race the edit. It reports true, writing nothing
// wrong, when the tracker refuses the edge as a cycle. A dry run writes nothing.
func (f FoldSupersede) moveToFold(ctx context.Context, id string, d StoryDetail) (bool, error) {
	if f.DryRun {
		return false, nil
	}
	held := strings.ToLower(strings.TrimSpace(d.Status)) == StatusOpen
	if held {
		if err := f.Tracker.HoldStory(ctx, id); err != nil {
			return false, fmt.Errorf("holding %s to move its wait to %s: %w", id, f.Fold, err)
		}
	}
	addErr := f.Tracker.AddBlocker(ctx, id, f.Fold)
	if held {
		if err := f.Tracker.ReleaseStory(ctx, id); err != nil {
			return false, fmt.Errorf("releasing %s after moving its wait to %s: %w", id, f.Fold, err)
		}
	}
	if addErr != nil {
		if strings.Contains(addErr.Error(), "cycle") {
			return true, nil
		}
		return false, fmt.Errorf("making %s wait on %s: %w", id, f.Fold, addErr)
	}
	return false, nil
}

func (f FoldSupersede) printBug(bug FoldSupersedeBug, failure error) {
	if f.Out == nil {
		return
	}
	verb := "superseded by"
	if f.DryRun {
		verb = "would be superseded by"
	}
	line := fmt.Sprintf("%s  %s", bug.ID, bug.Outcome)
	if bug.Outcome == foldSuperseded {
		line = fmt.Sprintf("%s  %s %s", bug.ID, verb, f.Fold)
		if failure != nil {
			line = fmt.Sprintf("%s  NOT superseded: %v", bug.ID, failure)
		}
	} else {
		line += ": left as it is"
	}
	if len(bug.Moved) > 0 {
		moveVerb := "moved"
		if f.DryRun {
			moveVerb = "would move"
		}
		line += fmt.Sprintf("; %s %s to wait on %s", moveVerb, strings.Join(bug.Moved, ", "), f.Fold)
	}
	if bug.RequestMoved {
		requestVerb := "request moved to"
		if f.DryRun {
			requestVerb = "request would move to"
		}
		line += fmt.Sprintf("; %s %s", requestVerb, f.Fold)
	}
	if bug.RequestLeft != "" {
		line += fmt.Sprintf("; its request stays on it: %s", bug.RequestLeft)
	}
	if len(bug.LeftOut) > 0 {
		line += fmt.Sprintf("; left out, it would make a cycle: %s waiting on %s", strings.Join(bug.LeftOut, ", "), f.Fold)
	}
	fmt.Fprintln(f.Out, line)
}

// reaches is true when from waits on to, directly or through other stories.
func reaches(needs map[string][]string, from, to string) bool {
	seen := map[string]bool{}
	var walk func(string) bool
	walk = func(at string) bool {
		if at == to {
			return true
		}
		if seen[at] {
			return false
		}
		seen[at] = true
		for _, next := range needs[at] {
			if walk(next) {
				return true
			}
		}
		return false
	}
	return walk(from)
}
