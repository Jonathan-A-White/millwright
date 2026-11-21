package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"

	"github.com/spf13/cobra"
)

// boostGiveBack is what boost-reach does once an outage of the Boost has gone
// 30 minutes (mw-6ww.185.10): the home gives the Boost's lapsed claims back as mw
// retry --force does, and mails the Mayor. It is built when it runs, so a config
// this host cannot read costs the give-back and never the doctor's other checks.
func boostGiveBack(dir, host string, retired []string, out io.Writer) func(ctx context.Context, boost string) error {
	return func(ctx context.Context, boost string) error {
		rigs, err := config.Rigs()
		if err != nil {
			return err
		}
		maxAttempts, err := config.MaxAttempts()
		if err != nil {
			return err
		}
		pushWaitSeconds, err := config.PushWaitSeconds()
		if err != nil {
			return err
		}
		gateway := mwGateway(dir, host)
		files := mwVault(dir, host)
		worktrees := rig.New()
		report, err := application.BoostGiveBack{
			// No Reach: the Boost is not answering, and a forced retry says what
			// it could not read of the Boost's worktree.
			Retry: application.Retry{
				Tracker:     gateway,
				Worktrees:   worktrees,
				Landing:     worktrees,
				Runner:      harnessReadyRunner(host, out),
				Files:       files,
				Vault:       files,
				Host:        host,
				Rigs:        rigs,
				MaxAttempts: maxAttempts,
				Notes:       gateway,
				Mailbox:     gateway,
				PushWait:    time.Duration(pushWaitSeconds) * time.Second,
				Log:         homeEventLog(),
			},
			Notes:        gateway,
			Mailbox:      gateway,
			Home:         host,
			Retired:      retired,
			GiveWayStale: giveWayStaleSetting(),
		}.Run(ctx, boost)
		if report.Skipped != "" {
			fmt.Fprintf(out, "boost-reach: no claim of %s was given back: %s\n", boost, report.Skipped)
		}
		for _, gave := range report.Gave {
			fmt.Fprintf(out, "boost-reach: gave back %s from %s\n", gave.StoryID, boost)
		}
		for _, left := range report.Left {
			fmt.Fprintf(out, "boost-reach: left %s on %s: %s\n", left.StoryID, boost, left.Why)
		}
		for _, note := range report.Notes {
			fmt.Fprintf(out, "boost-reach: %s\n", note)
		}
		return err
	}
}

// endLostSessions is the returning host's half of the give-back, run at the end
// of each dispatch tick: its sessions of stories another host holds now are
// ended, saying 'ended: <story> is now held by <host>'. A story still its own is
// left running, and the worktree of an ended one is left for the sweep. What
// could not be read or closed is said on the standard error and never fails the
// tick.
func endLostSessions(cmd *cobra.Command, tracker application.LostSessionStories, host string, dryRun bool) {
	report, err := application.EndLostSessions{
		Tracker:  tracker,
		Sessions: tmux.New(),
		Host:     host,
		DryRun:   dryRun,
		Out:      cmd.OutOrStdout(),
	}.Run(cmd.Context())
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "no session ended for a story held elsewhere: %v\n", err)
	}
	for _, note := range report.Notes {
		fmt.Fprintln(cmd.ErrOrStderr(), note)
	}
}
