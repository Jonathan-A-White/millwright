package main

import (
	"context"
	"io"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/peekremote"
)

// sshChecks is application.CheckElsewhere over ssh: mw check run on the host a
// story was worked on, reached as mw retry and mw next --met-by-mayor reach it
// ([hands_hosts], and each cloud box at its WireGuard address). The reach is
// read when a check is first sent elsewhere, not for every mw check (mw-gq6.1623).
type sshChecks struct{ out io.Writer }

var _ application.CheckElsewhere = sshChecks{}

// CheckThere implements application.CheckElsewhere.
func (s sshChecks) CheckThere(ctx context.Context, host, storyID string, flags []string) (string, error) {
	reach, gaps, err := storyReach(ctx, s.out)
	if err != nil {
		return "", err
	}
	return peekremote.Remote{Reach: reach, Gaps: gaps}.CheckThere(ctx, host, storyID, flags)
}

// checkThereFlags is the flags of this mw check that go with it to the host it is
// run on instead: the part, the wait and the slot, and none of the flags of the
// processes a check starts for itself.
func checkThereFlags(cmd *cobra.Command, part int, noSlot bool) []string {
	var flags []string
	if part > 0 {
		flags = append(flags, "--part", strconv.Itoa(part))
	}
	if cmd.Flags().Changed("wait") {
		wait, _ := cmd.Flags().GetDuration("wait")
		flags = append(flags, "--wait", wait.String())
	}
	if noSlot {
		flags = append(flags, "--no-slot")
	}
	return flags
}
