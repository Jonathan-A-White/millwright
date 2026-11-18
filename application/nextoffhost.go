package application

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

// offHostCloseOut is why a close-out refuses a story claimed on another host
// whose run record is not here, or "" when it may go on (mw-gq6.1481). The
// result its session wrote is in that host's run directory; looked for here it
// is missing, and the close-out would stop the story no-result: a false ledger
// line, a vault commit, a run note and a Blocked mail, before it said so. A
// result that is here (a batch lands stories other hosts worked) is judged as
// anywhere. A run record that cannot be read says nothing is known: it goes on.
// It writes nothing, so it is asked before the close-out writes anything.
func (n Next) offHostCloseOut(ctx context.Context, c *closeOut) string {
	host := n.claimHostOf(c.detail.Assignee)
	if host == "" || host == n.Host {
		return ""
	}
	if _, err := n.Vault.ReadRunFile(ctx, c.id, ResultFileNameForAttempt(c.attempt())); !errors.Is(err, fs.ErrNotExist) {
		return ""
	}
	return fmt.Sprintf("its claim is %s, so its run record is on %s, not %s: run `mw next %s` on %s, where nothing was written here",
		c.detail.Assignee, host, n.Host, c.id, host)
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
