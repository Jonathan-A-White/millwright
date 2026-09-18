package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// LandingPrefix is what the throwaway worktree a story is landed in is named
// after — it is made beside the rig's story worktrees, with something unique
// after this prefix, so that two landings never share a directory. MergeTries is how many times a landing will fetch, merge and push again
// after losing the race to the other host. Bounded, because a host that keeps
// losing the race is a host with something else wrong with it.
const (
	LandingPrefix = ".mw-landing-"
	MergeTries    = 3
)

// The two ways a landing stops that mw itself knows what to do about: a merge
// it will not resolve, and a push the remote refused because somebody else got
// there first. Adapters wrap their own words around these.
var (
	ErrMergeConflict = errors.New("the merge has conflicts")
	ErrPushRejected  = errors.New("the push was rejected: the branch moved on the remote")
)

// Landed is one merge of a story's branch into its target branch: the commit
// the target branch now points at, and whether the branch simply moved forward
// onto the story's work or a merge commit had to be made. A merge commit means
// the target branch moved while the story was worked, and so that the merged
// result is a combination nothing has ever been tested on.
type Landed struct {
	Commit      string
	FastForward bool
}

// Landing is the port a story's work is put on its target branch through. One
// adapter is git; it works in a throwaway worktree of the rig cut from the
// target branch as the remote has it, so that neither the rig's own checkout nor
// the story's worktree is disturbed by the landing.
//
// Nothing here ever forces anything. A push that is refused comes back as
// ErrPushRejected for the caller to fetch and try again, and a merge that
// conflicts comes back as ErrMergeConflict with the landing already undone.
type Landing interface {
	// Ahead counts the commits branch has that base does not: what landing the
	// story would actually put on the target branch. Zero means the session
	// committed nothing, and there is nothing to land.
	Ahead(ctx context.Context, rigDir, branch, base string) (int, error)

	// OpenLanding cuts a throwaway worktree of the rig at base, detached from
	// every branch, and reports where it is. It is where the merge is made, the
	// merged result is tested and the push is made from.
	OpenLanding(ctx context.Context, rigDir, base string) (string, error)

	// Merge merges branch into what the landing worktree has checked out. A
	// merge that conflicts is undone before the error comes back, so that the
	// landing worktree is left as it was found.
	Merge(ctx context.Context, landingDir, branch string) (Landed, error)

	// Push publishes what the landing worktree has checked out as branch on the
	// remote. A remote that has moved on refuses it, and the refusal comes back
	// as ErrPushRejected; nothing is ever forced.
	Push(ctx context.Context, landingDir, remote, branch string) error

	// CloseLanding takes the throwaway worktree away again. Closing what is not
	// there is not an error.
	CloseLanding(ctx context.Context, rigDir, landingDir string) error
}

// Checked is one run of a rig's own tests: whether they passed, what they
// printed, and the command that was run, so that a person reading a story that
// was stopped can run the same thing by hand.
type Checked struct {
	Command string
	Passed  bool
	Output  string
}

// Tail is the last lines of what the tests printed, for a comment on a story.
// The whole output of a test run is far too long to write onto a bead.
func (c Checked) Tail(lines int) string { return RecentLines(c.Output, lines) }

// Checks is the port a rig's own tests are run through. What the command is
// belongs to the rig and to the host it is run on, not to this package: a rig
// whose tests are `make test` and a rig whose tests are something else are the
// same thing here.
type Checks interface {
	// Run runs a rig's tests in a directory — the story's worktree, or the
	// throwaway worktree a landing was merged in. The rig is named as well as
	// the directory because what the tests are belongs to the rig, while where
	// they are run changes with every story. Tests that fail are a Checked that
	// did not pass, not an error: an error is the command not running at all.
	Run(ctx context.Context, rig, dir string) (Checked, error)
}

// MergeSlot is the port that keeps two close-outs from landing on one rig's
// target branch at the same time. It is per rig and per host: the other host
// has its own, and the remote itself is what settles a race between the two.
type MergeSlot interface {
	// Take waits for the rig's slot and reports the holding, which the caller
	// gives back. holder says who has it, for whoever finds it taken. It gives
	// up when ctx does.
	Take(ctx context.Context, rigDir, holder string) (Holding, error)
}

// Holding is a merge slot somebody has. Releasing it twice is harmless.
type Holding interface {
	// Release gives the slot back.
	Release(ctx context.Context) error

	// HeldBy is who took it, as it was told.
	HeldBy() string
}

// Dispatcher is the port mw carries the baton on with: whatever is ready on
// this host becomes a running session. Dispatch is the implementation; the port
// is here so that closing out a story can be tested without one, and so that a
// close-out depends on the act rather than on the use case.
type Dispatcher interface {
	Run(ctx context.Context) (DispatchReport, error)
}

// Dispatch satisfies the port.
var _ Dispatcher = Dispatch{}

// Rejected reports whether err is a push the remote refused because the branch
// had moved: the one landing failure worth trying again.
func Rejected(err error) bool { return errors.Is(err, ErrPushRejected) }

// Conflicted reports whether err is a merge that could not be made.
func Conflicted(err error) bool { return errors.Is(err, ErrMergeConflict) }

// LandedAs is how a landing is written down in the ledger and on the story.
func (l Landed) LandedAs(target string) string {
	how := "merge commit"
	if l.FastForward {
		how = "fast-forward"
	}
	commit := l.Commit
	if len(commit) > 12 {
		commit = commit[:12]
	}
	return fmt.Sprintf("landed on %s (%s, %s)", target, how, commit)
}

// Holders is the line written into a rig's merge slot, saying who has it.
func Holders(seat, host, storyID string) string {
	return fmt.Sprintf("%s closing out %s", SeatIdentity(seat, host), storyID)
}

// firstLine is the first line of a message, for somewhere only one line fits.
func firstLine(text string) string {
	if cut := strings.IndexByte(text, '\n'); cut >= 0 {
		return strings.TrimSpace(text[:cut])
	}
	return strings.TrimSpace(text)
}
