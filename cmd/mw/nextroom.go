package main

import (
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// withHostNextRoom gives a close-out, or the batch landing built on one, what it
// judges the host's room by before it starts a Builder session (the send-back to
// rebase, the send-back to fix the merged tests, a batch's fixer): the same limits
// the timer's dispatch reads, from one config's room (mw-gq6.1512;
// TestEveryNextCmdMwRunsAfterALandingIsGivenTheHostRoom).
func withHostNextRoom(n application.Next, room config.RoomSettings) application.Next {
	n.Room = roomLimits(room)
	return n
}
