package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// BdOverClaimFlag is the flag of mw bd (and of mw path set) that lets a hold, or
// a re-path, land on a story a Builder has claimed, and says why. bd does not
// know it: mw bd takes it off the arguments before it runs bd (mw-gq6.1129).
const BdOverClaimFlag = "--over-claim"

// OverClaimPrefix opens the comment that says why a claimed story was held or
// re-pathed over its claim.
const OverClaimPrefix = "OVER-CLAIM:"

// bdUpdateValueFlags are the flags of bd update that take the next argument as
// their value, so that it is not taken for a bead id.
var bdUpdateValueFlags = map[string]bool{
	"--acceptance": true, "--add-label": true, "--append-notes": true, "-a": true, "--assignee": true,
	"--await-id": true, "--body-file": true, "--defer": true, "-d": true, "--description": true,
	"--design": true, "--design-file": true, "--due": true, "-e": true, "--estimate": true,
	"--external-ref": true, "--if-assignee": true, "--if-status": true, "--metadata": true,
	"--notes": true, "--parent": true, "-p": true, "--priority": true, "--remove-label": true,
	"--session": true, "--set-labels": true, "--set-metadata": true, "--spec-id": true,
	"--title": true, "-t": true, "--type": true, "--unset-metadata": true,
}

// bdHold is a bd update that moves beads to deferred or blocked, taken apart.
type bdHold struct {
	ids []string
	// args is the command line without --over-claim and its reason.
	args []string
	// overClaim is whether the flag was given, and why its reason.
	overClaim bool
	why       string
}

// hold reads the call as a hold: a bd update with --status deferred or blocked.
// ok is false for any other call.
func (c bdCall) hold(args []string) (hold bdHold, ok bool) {
	if c.sub != "update" {
		return bdHold{}, false
	}
	offset := len(args) - len(c.rest)
	kept := append([]string(nil), args[:offset]...)
	status := ""
	for i := 0; i < len(c.rest); i++ {
		arg := c.rest[i]
		switch {
		case arg == BdOverClaimFlag:
			hold.overClaim = true
			if i+1 < len(c.rest) {
				i++
				hold.why = c.rest[i]
			}
			continue
		case strings.HasPrefix(arg, BdOverClaimFlag+"="):
			hold.overClaim = true
			hold.why = strings.TrimPrefix(arg, BdOverClaimFlag+"=")
			continue
		case arg == "--status" || arg == "-s":
			kept = append(kept, arg)
			if i+1 < len(c.rest) {
				i++
				status = c.rest[i]
				kept = append(kept, c.rest[i])
			}
			continue
		case strings.HasPrefix(arg, "--status="):
			status = strings.TrimPrefix(arg, "--status=")
		case strings.HasPrefix(arg, "-"):
			kept = append(kept, arg)
			if (bdUpdateValueFlags[arg] || bdValueFlags[arg]) && i+1 < len(c.rest) {
				i++
				kept = append(kept, c.rest[i])
			}
			continue
		default:
			hold.ids = append(hold.ids, arg)
		}
		kept = append(kept, arg)
	}
	status = strings.ToLower(strings.TrimSpace(status))
	if status != StatusHeld && status != "blocked" {
		return bdHold{}, false
	}
	hold.args = kept
	return hold, true
}

// claimOf reads a bead with bd show and says who has claimed it, or "" when
// nobody has (or the bead could not be read: the guard never stops a hold on a
// server that does not answer). A bead is claimed when it is in progress, or
// when it holds a live claim lease set back to open or held, as mw path set
// reads it.
func (r BdRetry) claimOf(ctx context.Context, call bdCall, id string) (claimer string, claimed bool) {
	run, err := r.Program.Run(ctx, append(append([]string(nil), call.globals...), "show", id, "--json"))
	if err != nil || run.Exit != 0 {
		return "", false
	}
	var shown []struct {
		Status         string   `json:"status"`
		Assignee       string   `json:"assignee"`
		Labels         []string `json:"labels"`
		LeaseExpiresAt string   `json:"lease_expires_at"`
	}
	if json.Unmarshal([]byte(run.Stdout), &shown) != nil || len(shown) == 0 {
		return "", false
	}
	bead := shown[0]
	detail := StoryDetail{Status: bead.Status, Assignee: bead.Assignee, Labels: bead.Labels}
	if at, err := time.Parse(time.RFC3339, bead.LeaseExpiresAt); err == nil {
		detail.LeaseExpires = at
	}
	inProgress := strings.EqualFold(strings.TrimSpace(bead.Status), StatusInProgress)
	if !inProgress && !LiveClaim(r.now(), detail) {
		return "", false
	}
	who := bead.Assignee
	if who == "" {
		who = "a session with no assignee named"
	}
	return fmt.Sprintf("%s (%s)", who, strings.ToLower(strings.TrimSpace(bead.Status))), true
}

// guardHold is the check of a hold before bd runs it: a bd update --status
// deferred|blocked of a bead a Builder has claimed is refused without
// --over-claim "<why>", so that it never leaves a Builder's run held mid-work
// (mw-gq6.1129). It returns the arguments to run bd with, the comments to write
// once the hold has landed, and whether the hold was refused (said on Err).
func (r BdRetry) guardHold(ctx context.Context, args []string) (run []string, comments map[string]string, refused bool) {
	call := parseBdCall(args)
	hold, ok := call.hold(args)
	if !ok || len(hold.ids) == 0 {
		return args, nil, false
	}
	if hold.overClaim && strings.TrimSpace(hold.why) == "" {
		r.say("mw bd: %s needs a reason, as %s \"amending the story\": nothing was run\n", BdOverClaimFlag, BdOverClaimFlag)
		return nil, nil, true
	}
	comments = map[string]string{}
	for _, id := range hold.ids {
		claimer, claimed := r.claimOf(ctx, call, id)
		if !claimed {
			continue
		}
		if !hold.overClaim {
			r.say("mw bd: %s is claimed by %s, and holding it would leave that run held mid-work: nothing was written. "+
				"To hold it anyway, add %s \"<why>\" to the command.\n", id, claimer, BdOverClaimFlag)
			return nil, nil, true
		}
		comments[id] = fmt.Sprintf("%s held over the claim of %s: %s", OverClaimPrefix, claimer, strings.Join(strings.Fields(hold.why), " "))
	}
	return hold.args, comments, false
}

// writeOverClaim writes the why of each hold that went over a claim on the bead
// it held. It reports whether every comment was written.
func (r BdRetry) writeOverClaim(ctx context.Context, call bdCall, comments map[string]string) bool {
	ok := true
	for id, text := range comments {
		run, err := r.Program.Run(ctx, append(append([]string(nil), call.globals...), "comments", "add", id, text))
		if err != nil || run.Exit != 0 {
			r.say("mw bd: the hold of %s landed, but the reason could not be written on it: %s\n", id, strings.TrimSpace(run.Stderr))
			ok = false
		}
	}
	return ok
}
