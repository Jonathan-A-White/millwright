package application

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// MayorReader finds which of the beads labelled hitl the postern view says wait
// on the Mayor — a hands bead with no step filed — by the view's own rule
// (need and markNotReady, waitsFor the Mayor), but without building the view.
// The view reads every live epic, a bd call or more each, and mw status is run
// by hand: this costs one read of the notes and, only when a bead with no
// steps has comments, one read of those.
//
// It leaves out what the view makes a need of another kind: a bead older than
// PosternViewStaleHands, which the view turns into a stale need waiting on the
// Governor, one he has kept, a demo-only bead, one whose BY HAND comment gives
// him his instructions, and one whose question card stands (it waits on his
// answer). Nor does it see a landing with no HOW TO CHECK IT or a story out of
// attempts: those take the view's whole tree.
//
// It writes nothing: the landed memory the view keeps is not touched.
type MayorReader struct {
	Tracker WorkTracker
	Notes   PosternNotes
	// Now is the clock a bead's age is measured against. The zero value reads
	// the real one.
	Now func() time.Time
}

// MayorNeeds lists the hands needs waiting on the Mayor, in the order hitl
// gives the beads, each once.
func (r MayorReader) MayorNeeds(ctx context.Context, hitl []StoryDetail) ([]PosternViewNeed, error) {
	if len(hitl) == 0 {
		return nil, nil
	}
	if r.Tracker == nil || r.Notes == nil {
		return nil, fmt.Errorf("reading what waits on the Mayor: there is no tracker or notes to read it from")
	}
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}
	// The notes and the comments do not depend on each other, so they are read
	// at once: a comment is asked for every bead that could still be a need and
	// has one, whether or not its notes turn out to give it steps.
	var maybe []string
	for _, d := range hitl {
		if d.CommentCount > 0 && r.maybeNeed(d, now) {
			maybe = append(maybe, d.Story.ID)
		}
	}
	type commentsRead struct {
		comments map[string][]Comment
		err      error
	}
	read := make(chan commentsRead, 1)
	go func() {
		comments := map[string][]Comment{}
		if len(maybe) == 0 {
			read <- commentsRead{comments, nil}
			return
		}
		comments, err := r.Tracker.StoriesComments(ctx, maybe)
		read <- commentsRead{comments, err}
	}()
	notes, err := r.Notes.NotesWithPrefix(ctx, "")
	got := <-read
	if err != nil {
		return nil, fmt.Errorf("reading the notes: %w", err)
	}
	if got.err != nil {
		return nil, fmt.Errorf("reading the comments of the hands beads with no steps: %w", got.err)
	}
	comments := got.comments

	b := &viewBuild{now: now}
	var candidates []*viewEntry
	seen := map[string]bool{}
	for _, d := range hitl {
		id := d.Story.ID
		e := &viewEntry{detail: d, waits: []string{}}
		if seen[id] || !r.maybeNeed(d, now) || b.kept(e, notes) {
			continue
		}
		seen[id] = true
		// A standing question card — the view's question need, the same note —
		// waits on the Governor, not the Mayor.
		if strings.TrimSpace(notes[PosternQuestionKey(id)]) != "" {
			continue
		}
		if len(viewHandsSteps(id, notes)) > 0 {
			continue
		}
		candidates = append(candidates, e)
	}

	var needs []PosternViewNeed
	for _, e := range candidates {
		d := e.detail
		if byHandInstructions(comments[d.Story.ID]) != "" {
			continue
		}
		need := b.need(PosternNeedHands, e, firstKnown(d.Created, d.Updated), viewSummary(d.Description))
		b.markNotReady(&need, e, true)
		if need.WaitsFor == PosternWaitsMayor {
			needs = append(needs, need)
		}
	}
	return needs, nil
}

// maybeNeed reports whether d could be a hands need waiting on the Mayor by
// what the bead says of itself alone: open or claimed, a hands kind of hitl
// bead (bare hitl, or hitl:hands) but not demo, and not yet old enough for the view to call it stale.
func (r MayorReader) maybeNeed(d StoryDetail, now time.Time) bool {
	if !workable(d) || !hasHitlLabel(d.Labels) || hitlNeedKind(d.Labels) != PosternNeedHands || hasLabel(d.Labels, LabelDemo) {
		return false
	}
	since := firstKnown(d.Created, d.Updated)
	return since.IsZero() || now.Sub(since) <= PosternViewStaleHands
}

// HeldHands lists the held beads with a hands step that has not run clean and
// was not superseded, in the order of their ids. It costs one read of the
// notes and, when any bead keeps steps, one read of those beads. It writes
// nothing.
func (r MayorReader) HeldHands(ctx context.Context) ([]StoryDetail, error) {
	if r.Tracker == nil || r.Notes == nil {
		return nil, fmt.Errorf("reading the held hands beads: there is no tracker or notes to read them from")
	}
	notes, err := r.Notes.NotesWithPrefix(ctx, "hands.")
	if err != nil {
		return nil, fmt.Errorf("reading the notes: %w", err)
	}
	var ids []string
	for key := range notes {
		id := strings.TrimPrefix(key, "hands.")
		if id == key || strings.HasPrefix(id, "ran.") || strings.HasPrefix(id, "superseded.") || strings.HasPrefix(id, "approval.") {
			continue
		}
		if !waitingHandsStep(viewHandsSteps(id, notes)) {
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	sort.Strings(ids)
	found, err := r.Tracker.ShowBeads(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("reading the beads that keep hands steps: %w", err)
	}
	var held []StoryDetail
	for _, d := range found {
		if d.Held() {
			held = append(held, d)
		}
	}
	return held, nil
}

// waitingHandsStep reports whether any of steps is still for the Governor's
// hands: not run clean, not superseded.
func waitingHandsStep(steps []PosternViewHandsStep) bool {
	for _, step := range steps {
		if step.SupersededBy == "" && (step.Ran == nil || step.Ran.Exit != 0) {
			return true
		}
	}
	return false
}
