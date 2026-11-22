package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// MayorAnswers is the port a close-out run off a story's host asks that host to
// run mw next there, with the Mayor's word (--met-by-mayor, --rescoped-by-mayor)
// or without one: the far end of an ssh, as mw retry reaches the host a story was worked on
// (mw-gq6.1622). The adapter is infrastructure/peekremote, over the reach mw
// retry reads (cmd/mw sshMayorAnswers); the fake is apptest.FakeMayorAnswers.
type MayorAnswers interface {
	// NextThere runs mw next on host for the story with flags and returns what it
	// printed, which is the host's report, also when it did not close the story: an
	// error carries why it did not, and what it printed is still returned.
	NextThere(ctx context.Context, host, storyID string, flags []string) (string, error)
}

// mayorsWord is the flags that carry the Mayor's word to the host that worked the
// story, and false when the close-out was given none: the flags are then
// --no-dispatch alone. The host is never told to dispatch: a close-out run from
// here asked for a close-out alone.
func (n Next) mayorsWord() ([]string, bool) {
	flags := []string{"--no-dispatch"}
	word := false
	if n.MetByMayorGiven && strings.TrimSpace(n.MetByMayor) != "" {
		flags, word = append(flags, "--met-by-mayor", n.MetByMayor), true
	}
	if strings.TrimSpace(n.RescopedByMayor) != "" {
		flags, word = append(flags, "--rescoped-by-mayor", n.RescopedByMayor), true
	}
	return flags, word
}

// answerHost is the host a close-out the home refused is run on instead of here,
// with the Mayor's word when he gave one and without it when he did not
// (mw-gq6.1624): the host the claim names, else the host key of the story's path,
// when that is not this one. Empty when no host can be asked, or the story names
// this host (its branch is gone from here: nothing to run elsewhere).
func (n Next) answerHost(c *closeOut) string {
	if n.Answers == nil {
		return ""
	}
	host := n.claimHostOf(c.detail.Assignee)
	if host == "" && c.path.Host != domain.HostAuto {
		host = c.path.Host
	}
	if host == n.Host {
		return ""
	}
	return host
}

// answerThere runs the close-out on the host that worked the story, where its
// branch, worktree and run record are, with the Mayor's word, and reports what
// that host printed. Nothing is written here: the host's own close-out records
// the word, lands, ledgers and closes the story (mw-gq6.1622).
func (n Next) answerThere(ctx context.Context, c *closeOut, report *NextReport, host string) (NextReport, error) {
	flags, _ := n.mayorsWord()
	printed, err := n.Answers.NextThere(ctx, host, c.id, flags)
	report.Delegated, report.DelegatedTo = printed, host
	if err != nil {
		return *report, fmt.Errorf("closing out %s: it was worked on %s, and `mw next` run there did not close it out (%s): nothing was written here; run it on %s by hand",
			c.id, host, oneLine(err.Error()), host)
	}
	return *report, nil
}

// answerFromHere is the command that works from a host the story was not worked
// on, for the refusals of a close-out that say where the story is: it runs the
// close-out on that host over ssh.
func answerFromHere(id string) string {
	return fmt.Sprintf("from here, `%s` (or --rescoped-by-mayor \"<why>\") runs it there over ssh", MetRecipe(id))
}
