package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"

	"github.com/spf13/cobra"
)

// newDigestCmd builds `mw digest`, the Governor's weekly digest of the demos
// the Mayor verified himself.
func newDigestCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "digest",
		Short: "The weekly digest: one card for the factory demos the Mayor verified himself",
		Args:  cobra.NoArgs,
	}
	root.AddCommand(newDigestSendCmd())
	return root
}

// newDigestSender is what mw digest send posts its card through: the very
// application.PosternSend mw postern send builds, set up only when the card
// is sent, so a dry run needs no postern key. A test swaps it.
var newDigestSender = func(gateway *beads.Gateway) application.PosternSender {
	return handsPush{gateway: gateway}
}

// newDigestSendCmd builds `mw digest send`.
func newDigestSendCmd() *cobra.Command {
	var dryRun bool
	var parent string
	cmd := &cobra.Command{
		Use:   "send [--dry-run] [--parent <bead>]",
		Short: "File a 'Weekly digest <date>' bead of the verified demos labelled digest and send one decision card on it",
		Long: "send lists every open, unclaimed demo labelled " + application.LabelDigest + " whose comments include one\n" +
			"beginning VERIFIED, files an open task bead 'Weekly digest <YYYY-MM-DD>: N factory demos' under\n" +
			"--parent (default " + application.DigestDefaultParent + ") listing each one's id, title and VERIFIED line, and sends one\n" +
			"decision-needed card on it naming each demo by its full id, offering 'A: Looks good on all N'\n" +
			"(recommended) and '" + application.DigestHoldOption + "'. A labelled demo with no VERIFIED comment is\n" +
			"printed as not verified, left out. After the demos the card carries a 'Factory loop' section read\n" +
			"as mw loop brief reads it: PICKS (the investment stories filed in the last 7 days, each with its\n" +
			"Expect line), VERDICTS (factory changes that reached 14 days with KEPT or WORSE, and the mw invest\n" +
			"outcomes recorded) and SHARE (the investment line and its bound, with 'moved to N% on <date>' when\n" +
			"the target differs from the one the previous digest bead recorded). With no demos but a pick or a\n" +
			"verdict the card still goes out, saying 'no factory demos this week'; with neither it prints\n" +
			"'nothing to digest' and sends nothing. --dry-run prints the card and files and sends nothing.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			here, err := config.Host()
			if err != nil {
				return err
			}
			gateway := mwGateway(dir, here)
			loop, err := newDigestLoop(dir, here)
			if err != nil {
				return err
			}
			_, err = application.Digest{
				Tracker: gateway,
				Sender:  newDigestSender(gateway),
				Loop:    loop,
				Parent:  parent,
				DryRun:  dryRun,
				Out:     cmd.OutOrStdout(),
			}.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the card; file and send nothing")
	cmd.Flags().StringVar(&parent, "parent", application.DigestDefaultParent, "the bead the digest is filed under")
	return cmd
}

// digestSpringJob is the job that sends the weekly digest (mw digest send) by
// itself: on Mondays from 11:00 UTC, once, on the home only. Its events' actor
// is digest@<host>.
func digestSpringJob(host string, out io.Writer) (application.SpringJob, error) {
	dir, err := config.Vault()
	if err != nil {
		return application.SpringJob{}, err
	}
	gateway := mwGateway(dir, host)
	weekly := &application.DigestWeekly{
		Notes: gateway,
		Home: func(ctx context.Context) (bool, error) {
			return application.IsHome(ctx, mwVault(dir, host), host)
		},
		Send: func(ctx context.Context) error {
			loop, err := newDigestLoop(dir, host)
			if err != nil {
				return fmt.Errorf("mw digest send: %w", err)
			}
			_, err = application.Digest{Tracker: gateway, Sender: newDigestSender(gateway), Loop: loop, Out: out}.Run(ctx)
			if err != nil {
				return fmt.Errorf("mw digest send: %w", err)
			}
			return nil
		},
		Err: out,
	}
	return weekly.Job(), nil
}

// newDigestLoop is the digest card's Factory loop section's source: the loop
// brief's reads over the default window, with the investment stories filed in it.
func newDigestLoop(dir, here string) (application.LoopSource, error) {
	run, err := newLoopBriefRun(dir, here, time.Time{})
	if err != nil {
		return nil, err
	}
	run.Picks = true
	return run, nil
}
