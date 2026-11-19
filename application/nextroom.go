package application

import (
	"context"
	"fmt"
)

// noRoomForASession reads the host's room right before a close-out would start a
// Builder session (the send-back to rebase, the send-back to fix the merged tests)
// and says why it has none, or "" when it has room or cannot be asked: Dispatch
// starts no story on a host with no room, and a send-back is a story's session
// too (mw-gq6.1512). It is ReadRoom, the one read dispatch and status make.
func (n Next) noRoomForASession(ctx context.Context) string {
	why, _ := ReadRoom(ctx, n.Load, n.Room)
	return why
}

// heldWhy is why a send-back found the host unfit to start a Builder session: the
// host giving way, or having no room. Set only after givesWayBeforeASendBack
// reported true.
func (c *closeOut) heldWhy() string {
	if c.gaveWay != nil {
		return c.gaveWay.String()
	}
	return "the host has no room: " + c.noRoom
}

// handBackForGivingWay hands a story back that was to be sent to a fresh Builder
// session (to do what) on a host that gives way, or has no room: nothing is
// started, recorded as sent back or merged, the claim, the worktree and the branch
// are kept, and the story says why. The next dispatch tick, once the host is free
// and has room, runs its close-out again, which sends it back then if it is still
// to be (mw-2891blf.19, mw-gq6.1512).
func (n Next) handBackForGivingWay(ctx context.Context, c *closeOut, report *NextReport, what string) (NextReport, error) {
	if c.gaveWay != nil {
		return n.handBack(ctx, c, report, ReasonGivesWay,
			fmt.Sprintf("the host %s, so no Builder session is started to %s now; its close-out is run again once the host is free", c.gaveWay, what))
	}
	return n.handBack(ctx, c, report, ReasonHostBusy,
		fmt.Sprintf("%s, so no Builder session is started to %s now; its close-out is run again once the host has room", c.heldWhy(), what))
}
