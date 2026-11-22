package application

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// offHostCloseOut is why a close-out refuses a story claimed on another host
// whose run record is not here, or "" when it may go on (mw-gq6.1481). The
// result its session wrote is in that host's run directory; looked for here it
// is missing, and the close-out would stop the story no-result: a false ledger
// line, a vault commit, a run note and a Blocked mail, before it said so. A
// result that is here (a batch lands stories other hosts worked) is judged as
// anywhere. A run record that cannot be read says nothing is known: it goes on.
// A story whose branch is not in this host's checkout is refused as well, with
// the host named (branchNotHereWhy, mw-gq6.1564). It writes nothing, so it is
// asked before the close-out writes anything.
func (n Next) offHostCloseOut(ctx context.Context, c *closeOut) string {
	host := n.claimHostOf(c.detail.Assignee)
	if host == "" || host == n.Host {
		return branchNotHereWhy(ctx, n.Landing, "next", c.id, c.rigDir, n.Host, host, c.path)
	}
	if _, err, _ := n.readResultPulled(ctx, c.id, ResultFileNameForAttempt(c.attempt())); !errors.Is(err, fs.ErrNotExist) {
		return branchNotHereWhy(ctx, n.Landing, "next", c.id, c.rigDir, n.Host, host, c.path)
	}
	return fmt.Sprintf("its claim is %s, so its run record is on %s, not %s: run `mw next %s` on %s, or %s, where nothing was written here",
		c.detail.Assignee, host, n.Host, c.id, host, answerFromHere(c.id))
}

// claimHostOf is the host an assignee names, whether the claim was made as mw
// or under the seat's own name (<seat>@<host>), or "" for any other.
func (n Next) claimHostOf(assignee string) string {
	if host := ClaimHost(assignee); host != "" {
		return host
	}
	if n.Seat == "" {
		return ""
	}
	host, _ := strings.CutPrefix(assignee, n.Seat+"@")
	if host == assignee {
		return ""
	}
	return host
}

// branchNotHereWhy is why a command that reads a story's branch in the rig's
// checkout on this host (mw next's close-out, mw check) refuses when the branch
// mw/<story> is not there, or "" when it is (mw-gq6.1564). A story worked on
// another host has its branch only there: counting its commits here ends on a
// raw git 'ambiguous argument' error after the close-out has written the Mayor's
// word, a ledger line and a mail. The host named is the one its claim names
// (claimHost), else the host key of its path, else 'another host'. It writes
// nothing, so it is asked before anything is written.
func branchNotHereWhy(ctx context.Context, landing Landing, command, storyID, rigDir, here, claimHost string, path domain.Path) string {
	if _, err := landing.RefHead(ctx, rigDir, StoryBranch(storyID)); err == nil {
		return ""
	}
	host := claimHost
	if host == "" && path.Host != domain.HostAuto {
		host = path.Host
	}
	if host == here {
		return fmt.Sprintf("its branch %s is not in %s's checkout of the rig, %s, though the story names this host: it was removed or never made here, so there is nothing to %s, and nothing was written",
			StoryBranch(storyID), here, rigDir, command)
	}
	where, run := "another host", "on the host that worked it"
	if host != "" {
		where, run = host, "on "+host
	}
	if command == "next" {
		run += ", or " + answerFromHere(storyID)
	}
	return fmt.Sprintf("its branch %s is not in %s's checkout of the rig, it was worked on %s: run `mw %s %s` %s, where nothing was written here",
		StoryBranch(storyID), here, where, command, storyID, run)
}
