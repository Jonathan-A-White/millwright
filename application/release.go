package application

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// Release approves a plan that was filed earlier. `mw file` holds every story
// it files, because the Governor is usually not at the machine when the Mayor
// files a plan; this is how he says yes afterwards, without filing the plan a
// second time.
//
// It reads the epic back out of the tracker, prints the same tree the filing
// printed, and then releases the stories that are still held. Nothing else is
// touched: a story somebody has already taken, or already finished, or that was
// released before, is left exactly as it was found. So releasing an epic twice
// does what releasing it once did, and nothing more.
//
// The tree is printed before anything is released, and shows the epic as it was
// found — what was held at the moment the release was asked for. What changed
// is the line under it.
type Release struct {
	Tracker WorkTracker

	// Out is where the tree is printed. A nil Out prints nothing.
	Out io.Writer
}

// Run releases the held stories of one already-filed epic and reports the epic
// as it was found. An epic the tracker does not have is a refusal, and nothing
// is written: the reading comes first, and it is the whole of the reading.
func (r Release) Run(ctx context.Context, epicID string) (FiledPlan, error) {
	if r.Tracker == nil {
		return FiledPlan{}, fmt.Errorf("releasing a plan: there is no work tracker to release it in")
	}
	if strings.TrimSpace(epicID) == "" {
		return FiledPlan{}, fmt.Errorf("releasing a plan: which epic?")
	}

	epic, err := r.Tracker.ShowEpic(ctx, epicID)
	if err != nil {
		return FiledPlan{}, fmt.Errorf("reading the epic %s: %w", epicID, err)
	}
	found := filedFrom(epic)
	r.print(found.Tree())

	held := found.held()
	for _, story := range held {
		if err := r.Tracker.ReleaseStory(ctx, story.ID); err != nil {
			return found, fmt.Errorf("releasing story %s of %s: %w", story.ID, epic.ID, err)
		}
	}
	found.Released = true
	r.print(found.released(len(held)))
	return found, nil
}

// print writes one block of the report, when there is somewhere to write it.
func (r Release) print(block string) {
	if r.Out == nil {
		return
	}
	fmt.Fprint(r.Out, block)
}

// filedFrom is an epic read back out of the tracker, as the same FiledPlan the
// filing of a plan reports — so that one tree serves both. A story's Needs are
// narrowed to what it is still waiting for: a story it waits on that is already
// finished is no longer a wait, and a tree that said otherwise would read as
// blocked work that is not blocked.
func filedFrom(epic EpicDetail) FiledPlan {
	finished := make(map[string]bool, len(epic.Stories))
	for _, story := range epic.Stories {
		finished[story.Story.ID] = story.Closed()
	}

	filed := FiledPlan{EpicID: epic.ID, Title: epic.Title, Defaults: epic.Defaults}
	for _, story := range epic.Stories {
		waits := make([]string, 0, len(story.Needs))
		for _, need := range story.Needs {
			// A need the epic does not hold is one this reading cannot vouch
			// for, so it counts as a wait: the tracker is the one that decides
			// what is ready, and it knows about that story even if we do not.
			if !finished[need] {
				waits = append(waits, need)
			}
		}
		filed.Stories = append(filed.Stories, FiledStory{
			ID:              story.Story.ID,
			Title:           story.Story.Title,
			Path:            story.Merged(),
			EstimateMinutes: story.EstimateMinutes,
			Needs:           waits,
			State:           StateOf(story.Status),
		})
	}
	return filed
}

// held is the stories of this plan the tracker is still holding back.
func (p FiledPlan) held() []FiledStory {
	var holding []FiledStory
	for _, story := range p.Stories {
		if story.state() == StateHeld {
			holding = append(holding, story)
		}
	}
	return holding
}

// ready is the ids of the stories a dispatcher can take once the held ones have
// been released: the ones that are not claimed and not finished, and that wait
// on nothing still to be done. It is what the tracker's own rule comes to, said
// here so that a person is told what the release made takeable.
func (p FiledPlan) ready() []string {
	var takeable []string
	for _, story := range p.Stories {
		switch story.state() {
		case StateHeld, StateOpen:
			if len(story.Needs) == 0 {
				takeable = append(takeable, story.ID)
			}
		}
	}
	return takeable
}

// released is what a person is told once the held stories have been let
// through: how many were released, or that there were none, and what can be
// dispatched now.
func (p FiledPlan) released(count int) string {
	var did string
	switch {
	case len(p.Stories) == 0:
		return fmt.Sprintf("\nThe epic %s has no stories: there is nothing to release.\n", p.EpicID)
	case count == 0:
		did = fmt.Sprintf("Nothing to release: none of the %d stories of %s is held.",
			len(p.Stories), p.EpicID)
	case count == 1:
		did = fmt.Sprintf("Released the 1 held story of %s.", p.EpicID)
	default:
		did = fmt.Sprintf("Released the %d held stories of %s.", count, p.EpicID)
	}

	ready := p.ready()
	if len(ready) == 0 {
		return did + " Nothing is ready: every story left waits on another, or is already taken.\n"
	}
	return fmt.Sprintf("%s Ready now: %s.\n", did, strings.Join(ready, ", "))
}
