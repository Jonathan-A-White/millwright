package application

import (
	"context"
	"slices"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// candidateReads is what one dispatch pass reads of its candidates together,
// the first time a candidate needs it: what they wait on, and the comments of
// those that carry any. Over the tunnel a Boost reads the tracker through, every
// read is two seconds, and a pass that read each candidate as it came to it cost
// a row of them (49 bd calls one after another, 73 s, on cloud1, mw-gq6.452).
// The candidates are the ready stories this host could take: not hitl, and on
// this host or any. A batch that cannot be read is read a candidate at a time,
// as before, so that one story's fault fails no other.
//
// The run state of what is claimed here, the comments of the candidates a gate
// turns away, and the open molecules the candidates carry are read the same way
// (mw-gq6.826): each in one read for the pass, where a tracker offers one
// (StoriesState, OpenMolecules), and a story at a time where it does not or
// where the read fails.
type candidateReads struct {
	tracker    WorkTracker
	candidates []StoryDetail

	needs map[string]StoryDetail // nil until the first read

	commentsRead bool
	comments     map[string][]Comment // nil when the batch failed

	statesOf   []string // the claims whose state the pass may read
	statesRead bool
	states     map[string]string // nil when there was no batch or it failed

	moleculesRead bool
	molecules     map[string]Molecule // nil when there was no batch or it failed
}

func newCandidateReads(tracker WorkTracker, host string, ready []StoryDetail) *candidateReads {
	r := &candidateReads{tracker: tracker}
	r.takeCandidates(host, ready)
	return r
}

// takeCandidates sets the candidates from the ready stories.
func (r *candidateReads) takeCandidates(host string, ready []StoryDetail) {
	r.candidates = nil
	for _, detail := range ready {
		path, err := detail.Path()
		if detail.Hitl() || err != nil || (path.Host != host && path.Host != domain.HostAuto) {
			continue
		}
		r.candidates = append(r.candidates, detail)
	}
}

// takeClaims sets the claims whose run state the pass may read: every story in
// progress here.
func (r *candidateReads) takeClaims(running []StoryDetail) {
	r.statesOf = nil
	for _, detail := range running {
		r.statesOf = append(r.statesOf, detail.Story.ID)
	}
}

// needed is every bead detail waits on, as the tracker says it now. The first
// call reads what every candidate waits on; a need that read did not find is
// read again for detail alone, which fails as a read of it alone does.
func (r *candidateReads) needed(ctx context.Context, detail StoryDetail) (map[string]StoryDetail, error) {
	if r.needs == nil {
		r.needs = map[string]StoryDetail{}
		_ = readNeeded(ctx, r.tracker, r.candidates, r.needs)
	}
	return r.needs, readNeeded(ctx, r.tracker, []StoryDetail{detail}, r.needs)
}

// handedBack is handedBackOf, from the comments read together.
func (r *candidateReads) handedBack(ctx context.Context, detail StoryDetail) (time.Time, bool, error) {
	if detail.CommentCount == 0 {
		return time.Time{}, false, nil
	}
	if comments, read := r.commentsKept(ctx)[detail.Story.ID]; read {
		at, handedBack := HandedBack(comments)
		return at, handedBack, nil
	}
	return handedBackOf(ctx, r.tracker, detail)
}

// commentsKept is the comments of every candidate that carries any, read in
// one StoriesComments the first time they are asked for; nil when that read
// failed.
func (r *candidateReads) commentsKept(ctx context.Context) map[string][]Comment {
	if !r.commentsRead {
		r.commentsRead = true
		var ids []string
		for _, d := range r.candidates {
			if d.CommentCount > 0 {
				ids = append(ids, d.Story.ID)
			}
		}
		if said, err := r.tracker.StoriesComments(ctx, ids); err == nil {
			r.comments = said
		}
	}
	return r.comments
}

// commentsOf is the comments of detail, from the ones read together when it is
// a candidate that carries any, and from the tracker for it alone when it is
// not or that read failed. A story whose count says it has none is not asked.
func (r *candidateReads) commentsOf(ctx context.Context, detail StoryDetail) ([]Comment, error) {
	if detail.CommentCount == 0 {
		return nil, nil
	}
	if comments, read := r.commentsKept(ctx)[detail.Story.ID]; read {
		return comments, nil
	}
	return r.tracker.StoryComments(ctx, detail.Story.ID)
}

// runState is the run state of the claimed story id, from the states of every
// claim read in one StoriesState the first time one is asked for, and from the
// tracker for it alone when the tracker offers no such read, the read failed,
// or id is not a claim the pass listed.
func (r *candidateReads) runState(ctx context.Context, id string) (string, error) {
	if !r.statesRead {
		r.statesRead = true
		if batch, offered := r.tracker.(StoriesState); offered && len(r.statesOf) > 0 {
			if states, err := batch.StoriesState(ctx, r.statesOf, RunState); err == nil {
				r.states = states
			}
		}
	}
	if state, read := r.states[id]; read {
		return state, nil
	}
	return r.tracker.StoryState(ctx, id, RunState)
}

// openMolecule is the open molecule of the root a candidate recorded, from the
// molecules of every candidate's root read in one OpenMolecules the first time
// one is asked for, and from the tracker for it alone when the tracker offers
// no such read, the read failed, or the root is no candidate's.
func (r *candidateReads) openMolecule(ctx context.Context, rootID string) (Molecule, error) {
	if !r.moleculesRead {
		r.moleculesRead = true
		var roots []string
		for _, d := range r.candidates {
			if d.Molecule.RootID != "" && !slices.Contains(roots, d.Molecule.RootID) {
				roots = append(roots, d.Molecule.RootID)
			}
		}
		if batch, offered := r.tracker.(OpenMolecules); offered && len(roots) > 0 {
			if open, err := batch.OpenMolecules(ctx, roots); err == nil {
				r.molecules = open
			}
		}
	}
	if open, read := r.molecules[rootID]; read {
		return open, nil
	}
	return r.tracker.OpenMolecule(ctx, rootID)
}
