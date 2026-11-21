package beads

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// proposalCommentsQuery reads, of the beads {ids}, only the comments whose text
// opens, after any white space, with a word of mw verify (PROPOSED, VERIFIED,
// UNVERIFIED, REJECTED): the comments application.ProposalsOf reads. It is a
// superset (VERIFIEDLY opens with VERIFIED), which the caller's own test of the
// first word narrows. Of 35,000 comments on the live vault 4,000 open one.
const proposalCommentsQuery = "select 'c' as k, c.id as id, c.issue_id as a, c.author as b, c.text as t, c.created_at as at" +
	" from comments c where c.issue_id in ({ids})" +
	" and regexp_like(c.text, '^[[:space:]]*(PROPOSED|VERIFIED|UNVERIFIED|REJECTED)', 'c')" +
	" order by at, id"

// proposalCommentsChunk is how many ids one query names over the kept connection,
// where there is no limit on an argument but the server's own; by bd sql it is
// commentsChunk.
const proposalCommentsChunk = 2000

// StoriesProposalComments implements application.ProposalCommentReader: the
// comments of each of the beads that open a word of mw verify, oldest first,
// in one query over the kept connection, however many ids there are. The
// doctor's dampers line asked StoriesComments for 1,563 beads whole, 3 to 5 s
// at load, to find a few of them (mw-gq6.1323). A bead with none is left out.
// Where no query can be made the comments are read whole, as StoriesComments
// reads them.
func (g *Gateway) StoriesProposalComments(ctx context.Context, ids []string) (map[string][]application.Comment, error) {
	if len(ids) == 0 {
		return map[string][]application.Comment{}, nil
	}
	size := commentsChunk
	if g.keptDB() != nil {
		size = proposalCommentsChunk
	}
	said := map[string][]application.Comment{}
	for chunk := range slices.Chunk(ids, size) {
		quoted := make([]string, 0, len(chunk))
		for _, id := range chunk {
			quoted = append(quoted, sqlQuoted(id))
		}
		out, err := g.sqlRows(ctx, strings.ReplaceAll(proposalCommentsQuery, "{ids}", strings.Join(quoted, ", ")))
		if err != nil {
			return g.StoriesComments(ctx, ids)
		}
		var rows []noteRow
		if err := json.Unmarshal(out, &rows); err != nil {
			return nil, fmt.Errorf("parsing bd sql --json: %w", err)
		}
		for _, r := range rows {
			var created time.Time
			if r.At != nil {
				if created, err = sqlTime(*r.At); err != nil {
					return nil, err
				}
			}
			said[r.A] = append(said[r.A], application.Comment{Author: r.B, Created: created, Text: r.Text})
		}
	}
	for _, comments := range said {
		sort.SliceStable(comments, func(i, j int) bool { return comments[i].Created.Before(comments[j].Created) })
	}
	return said, nil
}
