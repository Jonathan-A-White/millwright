package application

import (
	"context"
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
type candidateReads struct {
	tracker    WorkTracker
	candidates []StoryDetail

	needs map[string]StoryDetail // nil until the first read

	commentsRead bool
	comments     map[string][]Comment // nil when the batch failed
}

func newCandidateReads(tracker WorkTracker, host string, ready []StoryDetail) *candidateReads {
	r := &candidateReads{tracker: tracker}
	for _, detail := range ready {
		path, err := detail.Path()
		if detail.Hitl() || err != nil || (path.Host != host && path.Host != domain.HostAuto) {
			continue
		}
		r.candidates = append(r.candidates, detail)
	}
	return r
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
	if comments, read := r.comments[detail.Story.ID]; read {
		at, handedBack := HandedBack(comments)
		return at, handedBack, nil
	}
	return handedBackOf(ctx, r.tracker, detail)
}
