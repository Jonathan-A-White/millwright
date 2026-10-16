package main

import (
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
			"printed as not verified, left out. With none to list it prints 'nothing to digest' and sends\n" +
			"nothing. --dry-run prints the card and files and sends nothing.",
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
			_, err = application.Digest{
				Tracker: gateway,
				Sender:  newDigestSender(gateway),
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
