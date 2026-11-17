package application

import (
	"context"

	"github.com/Jonathan-A-White/millwright/domain"
)

// StopForTest lets the tests of package application_test run the one place a
// close-out that landed nothing writes its note, the give-back and the run
// state, on a story at the given attempt count.
func (n Next) StopForTest(ctx context.Context, id string, attempts int, reason Reason, why string) (NextReport, error) {
	c := &closeOut{id: id, rigDir: "/rig", worktree: "/wt/" + id, branch: "mw/" + id, target: "main", path: domain.Path{Rig: "rig"}}
	c.detail.Story.ID = id
	c.detail.Attempts = attempts
	c.detail.Assignee = "mw@laptop"
	return n.stop(ctx, c, &NextReport{}, reason, why, "")
}
