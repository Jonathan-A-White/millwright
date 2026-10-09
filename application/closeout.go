package application

import (
	"context"
	"fmt"
	"time"
)

// CloseOutMark is the word a running close-out (mw next, not --heartbeat)
// leaves on its host that it is at work on a story: the session is over, but
// the story is not quiet — it is being landed, or waiting its turn or for the
// host to calm before the rig's tests.
type CloseOutMark struct {
	Story string
	// Since is when the close-out began.
	Since time.Time
	// Calming says the close-out is waiting for a busy host to calm.
	Calming bool
}

// CloseOutMarks is where a close-out says it is running, outside the tracker:
// a mark only this host reads, written by mw next and read straight by mw
// status and the quiet alarm. The adapter is a file per story in this host's
// state directory, and a mark whose process has gone is no mark.
type CloseOutMarks interface {
	// Write records mark, replacing whatever mark the story already had.
	Write(ctx context.Context, mark CloseOutMark) error

	// Read is the marks of the close-outs running now. Marks left by a process
	// that is gone are not among them.
	Read(ctx context.Context) ([]CloseOutMark, error)

	// Clear removes the story's mark. One that is not there is already what was
	// asked for, and is not an error.
	Clear(ctx context.Context, story string) error
}

// closingOut reads the close-outs running on this host by story. A nil
// marker, or marks that cannot be read, is none: a story is then judged as it
// was before close-outs left marks.
func closingOut(ctx context.Context, marks CloseOutMarks) map[string]CloseOutMark {
	running := map[string]CloseOutMark{}
	if marks == nil {
		return running
	}
	list, err := marks.Read(ctx)
	if err != nil {
		return running
	}
	for _, m := range list {
		running[m.Story] = m
	}
	return running
}

// since is how mw status words the mark: "closing out 21m", and "waiting for
// calm" beside it when the close-out is waiting for the host to calm.
func (m CloseOutMark) since(now time.Time) string {
	s := fmt.Sprintf("closing out %dm", int(now.Sub(m.Since)/time.Minute))
	if m.Calming {
		s += ", waiting for calm"
	}
	return s
}

// markCloseOut writes the close-out's mark for storyID; a mark that cannot be
// written changes nothing about the close-out.
func (n Next) markCloseOut(ctx context.Context, storyID string, since time.Time, calming bool) {
	if n.CloseOuts != nil {
		_ = n.CloseOuts.Write(ctx, CloseOutMark{Story: storyID, Since: since, Calming: calming})
	}
}

// clearCloseOut takes the close-out's mark back out, whatever became of it. It
// uses a context of its own: the close-out may have ended because its did.
func (n Next) clearCloseOut(storyID string) {
	if n.CloseOuts != nil {
		_ = n.CloseOuts.Clear(context.Background(), storyID)
	}
}
