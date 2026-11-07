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
	// Rerun is the command to run again, as a shell line; "" says none.
	Rerun string
}

func (e *BdBusyError) Error() string {
	msg := fmt.Sprintf("waited %s for the host to be quiet and bd was not run: %s is still running", e.Waited, e.Blocker)
	if e.Rerun != "" {
		msg += "; run again: " + e.Rerun
	}
	return msg
}

// bdRerunLine is the mw bd command line that runs args again, quoted for a shell.
func bdRerunLine(args []string) string {
	words := []string{"mw", "bd"}
	for _, arg := range args {
		words = append(words, bdShellWord(arg))
	}
	return strings.Join(words, " ")
}

// bdShellWord quotes arg for a shell when it holds anything but plain characters.
func bdShellWord(arg string) string {
	plain := arg != ""
	for _, r := range arg {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./:=@%+,", r)) {
			plain = false
			break
		}
	}
	if plain {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

// BdBlocker names the first of procs that a bd must not run beside: another bd,
// or a mw sync, which writes the same database. "" is a quiet host. A bd whose
// working directory is under tmp is a test suite's, in a throwaway vault, and
// not the factory's. dir is the caller's beads directory ("" when not known):
// a bd that works on another one (its -C, else its working directory) holds no
// lock on this one, as a Go test's child with -C at its own temporary vault
// does not, whatever directory it runs from (mw-gq6.1021). The environment of a
// process is not read, so a BEADS_DOLT_* target is not told apart.
func BdBlocker(procs []Process, tmp, dir string) string {
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
			if dir != "" && !bdSameDir(BdBeadsDir(p.Argv[1:], p.Cwd), dir) {
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

// BdBeadsDir is the directory a bd works on: the value of its -C (or
// --directory) flag, taken from cwd when relative, else cwd.
func BdBeadsDir(args []string, cwd string) string {
	dir := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			i = len(args)
		case (arg == "-C" || arg == "--directory") && i+1 < len(args):
			i++
			dir = args[i]
		case strings.HasPrefix(arg, "--directory="):
			dir = strings.TrimPrefix(arg, "--directory=")
		case strings.HasPrefix(arg, "-C") && !strings.HasPrefix(arg, "--") && len(arg) > 2:
			dir = strings.TrimPrefix(strings.TrimPrefix(arg, "-C"), "=")
		}
	}
	if dir == "" {
		return cwd
	}
	if !path.IsAbs(dir) {
		dir = path.Join(cwd, dir)
	}
	return dir
}

// bdSameDir reports whether two directories are the same place by name.
func bdSameDir(a, b string) bool {
	return path.Clean(a) == path.Clean(b)
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
			return nil, &BdBusyError{Blocker: blocker, Waited: waited, Rerun: bdRerunLine(r.args)}
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
		if blocker := BdBlocker(procs, os.TempDir(), r.Dir); blocker != "" {
			release()
			return nil, blocker, nil
		}
	}
	return release, "", nil
}
