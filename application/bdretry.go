package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// BdRetryLimit is how long mw bd keeps trying a bd command that meets a passing
// fault of the beads server before it gives up and fails (mw-gq6.393): a
// desktop's link to the Laptop's server drops for seconds at a time, and a
// Builder session's step close lost to it stopped a finished story.
const BdRetryLimit = 2 * time.Minute

// bdRetryFirstPause is the wait before the first try again; it doubles up to
// bdRetryLongestPause.
const (
	bdRetryFirstPause   = 2 * time.Second
	bdRetryLongestPause = 20 * time.Second
)

// bdCommentSlack is how far before the first try a comment may be dated and
// still be the one that try wrote: the server's clock is not this host's.
const bdCommentSlack = time.Minute

// BdRun is one run of the bd program: what it printed on each stream and its
// exit status.
type BdRun struct {
	Stdout, Stderr string
	Exit           int
}

// BdProgram is the real bd, run with a list of arguments. The error is for a
// bd that could not be started at all; a bd that ran and failed is a BdRun with
// a status.
type BdProgram interface {
	Run(ctx context.Context, args []string) (BdRun, error)
}

// BdRetry is the bd a Builder session runs: the real bd, run again when it
// failed on a passing fault of the beads server (mw-gq6.393). Reads and writes
// that can be done twice are simply run again. A close and a comment cannot be
// told apart from a lost one by bd's words — "write commit result
// indeterminate" may follow a write that landed — so the bead is read first, and
// a close that took, or a comment already on the bead, is not written again. A
// command that cannot be undone or done twice (a create) is never run again.
type BdRetry struct {
	Program BdProgram
	// Wait pauses between tries; nil waits for real.
	Wait func(context.Context, time.Duration) error
	// Now dates the first try; nil reads the clock.
	Now func() time.Time
	// Limit is the total wait before giving up; zero is BdRetryLimit.
	Limit time.Duration
	// Out and Err are bd's own two streams, as a caller sees them.
	Out, Err io.Writer
}

// bdPassingFaults are the words a bd prints for a server it could not reach or
// whose answer did not come, as against a refusal that trying again changes
// nothing about.
var bdPassingFaults = []string{
	"dolt server unreachable",
	"indeterminate",
	"connection refused",
	"i/o timeout",
	"connection reset",
	"lost connection",
	"bad connection",
	"broken pipe",
	"no such host",
	"temporary failure in name resolution",
}

// bdFault is the passing fault a failed run met, or "".
func bdFault(run BdRun) string {
	said := strings.ToLower(run.Stdout + "\n" + run.Stderr)
	for _, fault := range bdPassingFaults {
		if strings.Contains(said, fault) {
			return fault
		}
	}
	return ""
}

// bdCall is a bd command line taken apart enough to be checked: the global flags
// before the subcommand (kept for the reads that check it), the subcommand, and
// what follows it.
type bdCall struct {
	globals []string
	sub     string
	rest    []string
}

// bdValueFlags are the flags of bd that take the next argument as their value,
// so that it is not mistaken for a bead id or a subcommand.
var bdValueFlags = map[string]bool{
	"-C": true, "--directory": true, "--actor": true, "--db": true, "--dolt-auto-commit": true,
	"-r": true, "--reason": true, "--reason-file": true, "--session": true,
	"-f": true, "--file": true, "-m": true, "--message": true,
}

func parseBdCall(args []string) bdCall {
	var call bdCall
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			call.sub, call.rest = arg, args[i+1:]
			return call
		}
		call.globals = append(call.globals, arg)
		if bdValueFlags[arg] && i+1 < len(args) {
			i++
			call.globals = append(call.globals, args[i])
		}
	}
	return call
}

// positional is the arguments of the call that are neither flags nor their
// values.
func (c bdCall) positional() []string {
	var got []string
	for i := 0; i < len(c.rest); i++ {
		switch {
		case strings.HasPrefix(c.rest[i], "-"):
			if bdValueFlags[c.rest[i]] {
				i++
			}
		default:
			got = append(got, c.rest[i])
		}
	}
	return got
}

// hasFlag reports whether the call carries a flag of that name.
func (c bdCall) hasFlag(names ...string) bool {
	for _, arg := range c.rest {
		for _, name := range names {
			if arg == name || strings.HasPrefix(arg, name+"=") {
				return true
			}
		}
	}
	return false
}

// bdKind is how a command may be run again.
type bdKind int

const (
	bdNever   bdKind = iota // may have landed and cannot be told: run once
	bdAgain                 // a read, or a write that does the same thing twice
	bdClose                 // run again unless the beads are closed
	bdComment               // run again unless the comment is on the bead
)

func (c bdCall) kind() bdKind {
	switch c.sub {
	case "close":
		return bdClose
	case "comment":
		return bdComment
	case "comments":
		if pos := c.positional(); len(pos) > 0 && pos[0] == "add" {
			return bdComment
		}
		return bdAgain
	case "show", "list", "ready", "blocked", "state", "update", "reopen", "kv", "children", "search", "count":
		return bdAgain
	}
	return bdNever
}

// Run runs bd with args, and again while it meets a passing fault, up to the
// limit. It returns the status the last run left with; an error only when bd
// could not be started or the context ended.
func (r BdRetry) Run(ctx context.Context, args []string) (int, error) {
	call := parseBdCall(args)
	kind := call.kind()
	began := r.now()
	var waited time.Duration
	var met string
	pause := bdRetryFirstPause

	for tries := 1; ; tries++ {
		run, err := r.Program.Run(ctx, args)
		if err != nil {
			return 1, err
		}
		if run.Exit == 0 {
			r.finish(run, tries, waited, "", met)
			return 0, nil
		}
		fault := bdFault(run)
		if fault == "" || kind == bdNever {
			r.finish(run, tries, waited, "", met)
			return run.Exit, nil
		}
		met = fault
		// Wait, and for a close or a comment, which may have landed whatever
		// bd said, read the bead back before writing again. The read meets the
		// fault too, and then the wait goes on.
	wait:
		for {
			if !r.canWait(waited, pause) {
				r.finish(run, tries, waited, fault, met)
				return run.Exit, nil
			}
			if err := r.pause(ctx, pause); err != nil {
				return 1, err
			}
			waited += pause
			pause = nextBdPause(pause)
			if kind == bdAgain {
				break wait
			}
			switch r.landed(ctx, call, kind, began) {
			case bdLanded:
				r.say("mw bd: bd said %q, but the write had landed, so it was not run again (%d tries, %s of waiting)\n", fault, tries, waited)
				return 0, nil
			case bdNotLanded:
				break wait
			case bdCannotTell:
				// A comment that may be on the bead is not written blind: a
				// second one is worse than none, and says so.
				r.pass(run)
				r.say("mw bd: bd said %q, and the bead could not be read to see whether the write landed, so it was not run again\n", fault)
				return run.Exit, nil
			}
			// bdUnreadable: the read met the fault too; wait and read again.
		}
	}
}

func nextBdPause(pause time.Duration) time.Duration {
	if pause *= 2; pause > bdRetryLongestPause {
		return bdRetryLongestPause
	}
	return pause
}

func (r BdRetry) canWait(waited, pause time.Duration) bool {
	limit := r.Limit
	if limit == 0 {
		limit = BdRetryLimit
	}
	return waited+pause <= limit
}

func (r BdRetry) pause(ctx context.Context, d time.Duration) error {
	if r.Wait != nil {
		return r.Wait(ctx, d)
	}
	return waitFor(ctx, d)
}

func (r BdRetry) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// finish prints the last run as bd printed it, and, when it was tried again, how
// often; gaveUp names the fault a run that never got through ended on, met the
// last fault any try found.
func (r BdRetry) finish(run BdRun, tries int, waited time.Duration, gaveUp, met string) {
	r.pass(run)
	switch {
	case gaveUp != "":
		r.say("mw bd: giving up after %d tries over %s: the beads server is still failing (%s); read the bead before taking this write for done\n", tries, waited, gaveUp)
	case tries > 1:
		r.say("mw bd: went through on try %d, after %s of waiting on a passing fault of the beads server (%s)\n", tries, waited, met)
	}
}

// pass prints what a run printed, on the streams it printed it on.
func (r BdRetry) pass(run BdRun) {
	if r.Out != nil {
		io.WriteString(r.Out, run.Stdout)
	}
	if r.Err != nil {
		io.WriteString(r.Err, run.Stderr)
	}
}

// say writes one line of mw bd's own on bd's standard error.
func (r BdRetry) say(format string, args ...any) {
	if r.Err != nil {
		fmt.Fprintf(r.Err, format, args...)
	}
}

// bdLandedState is what reading the bead back said of a write that failed.
type bdLandedState int

const (
	bdNotLanded  bdLandedState = iota // the bead is as it was: write it again
	bdLanded                          // the write took
	bdUnreadable                      // the read met a passing fault: wait and read again
	bdCannotTell                      // the read cannot say, and a second write would not be safe
)

// landed reads the beads back to say whether the write of a close or a comment
// that failed had taken. A close that cannot be read back is written again: it
// does the same thing twice.
func (r BdRetry) landed(ctx context.Context, call bdCall, kind bdKind, began time.Time) bdLandedState {
	pos := call.positional()
	cannot := bdCannotTell
	read := func(args ...string) (BdRun, bdLandedState, bool) {
		run, err := r.Program.Run(ctx, append(append([]string(nil), call.globals...), args...))
		switch {
		case err == nil && run.Exit == 0:
			return run, 0, true
		case err == nil && bdFault(run) != "":
			return run, bdUnreadable, false
		}
		return run, cannot, false
	}

	switch kind {
	case bdClose:
		cannot = bdNotLanded
		for _, id := range pos {
			run, state, ok := read("show", id, "--json")
			if !ok {
				return state
			}
			var shown []struct {
				Status string `json:"status"`
			}
			if json.Unmarshal([]byte(run.Stdout), &shown) != nil || len(shown) == 0 {
				return bdNotLanded
			}
			if shown[0].Status != "closed" {
				return bdNotLanded
			}
		}
		if len(pos) == 0 {
			return bdNotLanded
		}
		return bdLanded
	case bdComment:
		if call.sub == "comments" {
			pos = pos[1:]
		}
		if len(pos) < 2 || call.hasFlag("-f", "--file") {
			// A comment from a file, or one with no text: nothing to compare.
			return bdCannotTell
		}
		id, text := pos[0], strings.Join(pos[1:], " ")
		run, state, ok := read("comments", id, "--json")
		if !ok {
			return state
		}
		var listed []struct {
			Text      string `json:"text"`
			CreatedAt string `json:"created_at"`
		}
		if json.Unmarshal([]byte(run.Stdout), &listed) != nil {
			return bdCannotTell
		}
		for _, said := range listed {
			if said.Text != text {
				continue
			}
			// An identical comment from before this write is not this write.
			if at, err := time.Parse(time.RFC3339, said.CreatedAt); err == nil && at.Before(began.Add(-bdCommentSlack)) {
				continue
			}
			return bdLanded
		}
		return bdNotLanded
	}
	return bdNotLanded
}
