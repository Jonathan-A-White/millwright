package application

import (
	"context"
	"fmt"
)

// LiveHeartbeats says whether a story's session is still being worked, as seen
// from this host's process table. A dispatched session's shell line starts
// `mw next --heartbeat <story>` beside the harness and kills it the moment the
// harness ends, before the close-out it chains on after, so a heartbeat alive
// is a harness at work and none alive is a session over. The session's own tmux
// pane cannot say this: the close-out the session ends with runs inside that
// very pane, which is still running while it does.
type LiveHeartbeats interface {
	// Beating is the process ids of the `mw next --heartbeat` processes now
	// alive for storyID on this host; none is an empty list.
	Beating(ctx context.Context, storyID string) ([]int, error)
}

// stillRunning is the refusal a by-hand mw next gives a story whose session is
// still at work: a close-out then would find no result, stop the story no-result
// and mail the Mayor that it was blocked, all false of an attempt still running
// (mw-gq6.394). It is empty when the session is over, or when nothing is known
// of it: a probe that cannot be read stops no close-out. It writes nothing
// anywhere, so it is asked before the close-out marks, renews or ledgers.
func (n Next) stillRunning(ctx context.Context, storyID string) string {
	if n.Heartbeats == nil {
		return ""
	}
	pids, err := n.Heartbeats.Beating(ctx, storyID)
	if err != nil || len(pids) == 0 {
		return ""
	}
	return fmt.Sprintf("the session %s is still running (mw next --heartbeat %s is alive as process %d), so nothing was touched: "+
		"wait for the session to end, which closes the story out itself", SessionName(storyID), storyID, pids[0])
}
