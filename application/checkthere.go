package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// CheckElsewhere is the port a check run on a host the story was not worked on
// asks that host to run mw check there: the far end of an ssh, as mw next reaches
// it with the Mayor's word (MayorAnswers) and mw retry reaches a worktree
// (mw-gq6.1623). The adapter is infrastructure/peekremote, over the reach mw
// retry reads (cmd/mw sshChecks); the fake is apptest.FakeChecksElsewhere.
type CheckElsewhere interface {
	// CheckThere runs mw check on host for the story with flags and returns what it
	// printed, also when the check did not pass: the error says why, and what it
	// printed is still returned.
	CheckThere(ctx context.Context, host, storyID string, flags []string) (string, error)
}

// thereHost is the host mw check is run on in place of this one, or "" when it is
// run here: the story is open, this host has a way to ask another (There), and it
// holds neither the worktree nor the branch of a story that was worked elsewhere,
// which is the host its claim names, else the host key of its path. It reads and
// writes nothing but the rig's checkout, so it is asked before the check begins
// to keep a log (mw-gq6.1623).
func (k Check) thereHost(ctx context.Context, storyID string, detail StoryDetail) string {
	if k.There == nil || detail.Closed() {
		return ""
	}
	path, err := detail.Path()
	if err != nil {
		return ""
	}
	claimed := ClaimHost(detail.Assignee)
	if OffHostWhy("check", storyID, detail.Assignee, k.Host) != "" {
		return claimed
	}
	rigDir, checkedOut := k.Rigs[path.Rig]
	if !checkedOut || branchNotHereWhy(ctx, k.Landing, "check", storyID, rigDir, k.Host, claimed, path) == "" {
		return ""
	}
	host := claimed
	if host == "" && path.Host != domain.HostAuto {
		host = path.Host
	}
	if host == k.Host {
		return ""
	}
	return host
}

// checkThere runs the check on the host that worked the story, where its worktree
// and branch are, and prints what that host printed: nothing is checked, kept or
// written here, and the error, when the check there did not pass or could not be
// asked for, is the one that makes mw check exit non-zero.
func (k Check) checkThere(ctx context.Context, report CheckReport, host string) (CheckReport, error) {
	printed, err := k.There.CheckThere(ctx, host, report.StoryID, k.ThereFlags)
	report.Delegated, report.DelegatedTo = printed, host
	if k.Out != nil {
		fmt.Fprintf(k.Out, "check of %s: it was worked on %s, so mw check is run there, over ssh; it printed:\n", report.StoryID, host)
		for _, line := range strings.Split(strings.TrimRight(printed, "\n"), "\n") {
			if printed != "" {
				fmt.Fprintf(k.Out, "  %s\n", line)
			}
		}
	}
	if err != nil {
		return report, fmt.Errorf("checking %s: it was worked on %s, and `mw check` run there did not pass (%s): nothing was checked here; run `mw check %s` on %s by hand",
			report.StoryID, host, oneLine(err.Error()), report.StoryID, host)
	}
	return report, nil
}
