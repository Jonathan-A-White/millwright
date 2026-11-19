package application

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
)

// BdBigParentChildren is the number of children above which mw bd refuses an
// unbounded bd list of a parent (mw-gq6.1504). bd 1.3.0 died at Dolt's read
// timeout reading the descendants of mw-5r3p30, an epic of over 1070 children
// (mw-gq6.994), and an unbounded list of it stalled bd for every seat on the
// host for about 25 minutes. 500 is under half of that, and well over any map
// or epic a seat lists whole.
const BdBigParentChildren = 500

// unboundedParentList reads the call as a bd list of the children of one
// parent with no limit, which bd spells --limit 0 (or -n 0): bd's own default
// is 50, so a list with no limit named is already bounded. ok is false for any
// other call.
func (c bdCall) unboundedParentList() (parent string, ok bool) {
	if c.sub != "list" {
		return "", false
	}
	unlimited := false
	for i := 0; i < len(c.rest); i++ {
		arg := c.rest[i]
		value, hasValue := "", false
		switch {
		case arg == "--parent" || arg == "--limit" || arg == "-n":
			if i+1 < len(c.rest) {
				i++
				value, hasValue = c.rest[i], true
			}
		case strings.HasPrefix(arg, "--parent="), strings.HasPrefix(arg, "--limit="), strings.HasPrefix(arg, "-n="):
			cut := strings.Index(arg, "=")
			arg, value, hasValue = arg[:cut], arg[cut+1:], true
		default:
			if bdValueFlags[arg] {
				i++
			}
			continue
		}
		if !hasValue {
			continue
		}
		if arg == "--parent" {
			parent = value
		} else if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && n == 0 {
			unlimited = true
		} else {
			unlimited = false
		}
	}
	return parent, parent != "" && unlimited
}

// guardBigParentList is the check of an unbounded list of a parent before bd
// runs it: bd list --parent <id> --limit 0 of a parent with more than
// BdBigParentChildren children is refused, naming the bounded forms. The
// children are counted by a bd list bounded to one more than the limit, so the
// count itself is cheap. A parent that cannot be counted is listed as before: the
// guard never stops a list on a server that does not answer.
func (r BdRetry) guardBigParentList(ctx context.Context, args []string) (refused bool) {
	call := parseBdCall(args)
	parent, ok := call.unboundedParentList()
	if !ok {
		return false
	}
	probe := append(append([]string(nil), call.globals...),
		"list", "--parent", parent, "--all", "--limit", strconv.Itoa(BdBigParentChildren+1), "--json")
	run, err := r.Program.Run(ctx, probe)
	if err != nil || run.Exit != 0 {
		return false
	}
	var children []json.RawMessage
	if json.Unmarshal([]byte(run.Stdout), &children) != nil || len(children) <= BdBigParentChildren {
		return false
	}
	r.say("mw bd: %s has more than %d children, and an unbounded list of them stalls bd for every seat on this host: nothing was run. "+
		"List a bounded page instead: mw bd list --parent %s --all --limit 50 (or any limit above 0 and up to %d); "+
		"for a map, read it with mw brief %s.\n",
		parent, BdBigParentChildren, parent, BdBigParentChildren, parent)
	return true
}
