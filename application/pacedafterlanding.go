package application

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// PacedReleaser is what a landing asks, once the stories it landed are closed,
// to run again the lists of stories released at a pace (mw release-paced) that
// name one of them. It says what it did, as notes, and never fails a landing.
// A nil PacedReleaser runs no list.
type PacedReleaser interface {
	ReleaseAfter(ctx context.Context, landed []string) (notes []string)
}

// PacedAfterLanding runs mw release-paced's own release, by itself, for each
// list a landed story is on: the story that landed no longer counts against the
// 3 at once, so the next held story of its list is released if the pace allows,
// with no Mayor to remember the command (mw-gq6.1682). It releases at the same
// pace as the command does (ReleasePaced), because it is that command's Run.
type PacedAfterLanding struct {
	Lists PacedLists
	// Release holds the ports and the line of the paced release it runs; its
	// Out and DryRun are not used.
	Release ReleasePaced
}

// ReleaseAfter runs the release of each paced list that names a story of landed
// and gives what each said, one note for each line, headed by the list's name: a
// list that released none says why. Lists that name none of landed are not
// read from the tracker. A fault is a note, not an error: the story is landed.
func (p PacedAfterLanding) ReleaseAfter(ctx context.Context, landed []string) []string {
	if p.Lists == nil || len(landed) == 0 {
		return nil
	}
	lists, err := p.Lists.PacedLists(ctx)
	if err != nil {
		return []string{fmt.Sprintf("release-paced: the paced lists could not be read, so none was run again: %v", err)}
	}
	var notes []string
	for _, list := range lists {
		if !slices.ContainsFunc(list.IDs, func(id string) bool { return slices.Contains(landed, id) }) {
			continue
		}
		var out strings.Builder
		run := p.Release
		run.Out, run.DryRun = &out, false
		_, err := run.Run(ctx, list.IDs)
		for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				notes = append(notes, fmt.Sprintf("release-paced %s: %s", list.Name, line))
			}
		}
		if err != nil {
			notes = append(notes, fmt.Sprintf("release-paced %s: it failed, run mw release-paced by hand: %v", list.Name, err))
		}
	}
	return notes
}

// releasePaced is the landed tail's step stepPacedLists: it runs the paced lists
// the stories this landing closed are on. A single landing's story that could
// not be closed, and a demo left for the Governor, are not closed, so they still
// count against the list's 3 and name nothing; a batch names every story it
// closed. What the lists said is a note on the landing's report, and, for a
// batch, a line of its log.
func (n Next) releasePaced(t *tail, s *tailStory) {
	if n.Paced == nil {
		return
	}
	var closed []string
	if t.batch != nil {
		closed = t.batch.report.Closed
	} else if s != nil && s.closed {
		closed = []string{s.c.id}
	}
	notes := n.Paced.ReleaseAfter(t.ctx, closed)
	if t.batch == nil {
		t.once.Notes = append(t.once.Notes, notes...)
		return
	}
	for _, note := range notes {
		t.batch.land.say("%s", note)
	}
	t.batch.nr.Notes = append(t.batch.nr.Notes, notes...)
}
