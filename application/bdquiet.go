package application

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"
	"time"
)

// BdQuietLimit is how long mw bd waits for its turn and for quiet before it
// gives up (mw-gq6.568): the beads database takes one writer, and a seat that
// ran a bd beside another on a prompt's word alone did it again and again, so
// the command waits instead.
const BdQuietLimit = 120 * time.Second

// bdQuietPoll is how often mw bd looks again while it waits.
const bdQuietPoll = 500 * time.Millisecond

// BdTurn is this host's one turn to run a bd in: what keeps two mw bd commands
// that start together from each finding the host quiet and running side by
// side.
type BdTurn interface {
	// TryTake takes the turn without waiting. When it is not taken, holder says
	// who has it, for the failure that names the blocker. The turn is held until
	// release is run.
	TryTake(ctx context.Context) (release func(), taken bool, holder string, err error)
}

// BdBusyError is the failure of a mw bd that did not get its turn, or a quiet
// host, within the limit, and so ran no bd.
type BdBusyError struct {
	Blocker string
	Waited  time.Duration
}

func (e *BdBusyError) Error() string {
	return fmt.Sprintf("waited %s for the host to be quiet and bd was not run: %s is still running", e.Waited, e.Blocker)
}

// BdBlocker names the first of procs that a bd must not run beside: another bd,
// or a mw sync, which writes the same database. "" is a quiet host. A bd whose
// working directory is under tmp is a test suite's, in a throwaway vault, and
// not the factory's.
func BdBlocker(procs []Process, tmp string) string {
	for _, p := range procs {
		if len(p.Argv) == 0 {
			continue
		}
		program := path.Base(p.Argv[0])
		switch {
		case program == "bd":
			if tmp != "" && bdUnderDir(p.Cwd, tmp) {
				continue
			}
		case program == "mw" && bdFirstWord(p.Argv[1:]) == "sync":
		default:
			continue
		}
		return fmt.Sprintf("%s (pid %d: %s)", program, p.Pid, strings.Join(append([]string{program}, p.Argv[1:]...), " "))
	}
	return ""
}

// bdFirstWord is the first argument that is not a flag.
func bdFirstWord(args []string) string {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			return arg
		}
	}
	return ""
}

// bdUnderDir reports whether dir is root or inside it.
func bdUnderDir(dir, root string) bool {
	root = strings.TrimSuffix(root, "/")
	return dir == root || strings.HasPrefix(dir, root+"/")
}

// take waits for this host's turn to run a bd and for it to be quiet, and
// returns what lets the turn go. With neither a Turn nor a Quiet table it waits
// for nothing; a negative QuietWait waits for nothing either.
func (r BdRetry) take(ctx context.Context) (func(), error) {
	if (r.Turn == nil && r.Quiet == nil) || r.QuietWait < 0 {
		return func() {}, nil
	}
	limit, poll := r.QuietWait, r.Poll
	if limit == 0 {
		limit = BdQuietLimit
	}
	if poll == 0 {
		poll = bdQuietPoll
	}
	var waited time.Duration
	for {
		release, blocker, err := r.tryTurn(ctx)
		if err != nil {
			return nil, err
		}
		if blocker == "" {
			if waited > 0 {
				r.say("mw bd: waited %s for the host to be quiet\n", waited)
			}
			return release, nil
		}
		if waited+poll > limit {
			return nil, &BdBusyError{Blocker: blocker, Waited: waited}
		}
		if err := r.pause(ctx, poll); err != nil {
			return nil, err
		}
		waited += poll
	}
}

// tryTurn takes the turn and looks at the host once: the turn, with no blocker,
// when both are free, and otherwise no turn and what is in the way. The turn is
// taken first so that two commands looking together are not both told the host
// is quiet.
func (r BdRetry) tryTurn(ctx context.Context) (func(), string, error) {
	release := func() {}
	if r.Turn != nil {
		got, taken, holder, err := r.Turn.TryTake(ctx)
		if err != nil {
			return nil, "", err
		}
		if !taken {
			return nil, holder, nil
		}
		release = got
	}
	if r.Quiet != nil {
		procs, err := r.Quiet.List(ctx)
		if err != nil {
			release()
			return nil, "", fmt.Errorf("looking at the host's processes: %w", err)
		}
		if blocker := BdBlocker(procs, os.TempDir()); blocker != "" {
			release()
			return nil, blocker, nil
		}
	}
	return release, "", nil
}
