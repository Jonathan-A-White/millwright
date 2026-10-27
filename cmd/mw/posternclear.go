package main

import (
	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
)

// newPosternClearCmd builds `mw postern clear <bead> --why "<text>"`: clear the
// card the Governor has answered while its bead stays open.
func newPosternClearCmd() *cobra.Command {
	var why, txid string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "clear <bead> --why \"<text>\"",
		Short: "Clear a Postern card he has answered while its bead stays open",
		Long: "clear is for an answered card on a bead that stays open: the card still sits in his Needs you\n" +
			"because the bead's question note (postern.question.<bead>) is still there, and mw tidy clears\n" +
			"that note only when the bead is closed. It does three things, and changes nothing else:\n" +
			"  1. clears the bead's question note;\n" +
			"  2. comments the bead \"Card cleared by mw postern clear: <why>\";\n" +
			"  3. posts a short reply under the card's txid saying it is cleared and why, so his phone\n" +
			"     thread closes the loop.\n\n" +
			"The bead is never changed: not its status, its labels or its fields. A bead with no question\n" +
			"note is refused, naming it. --why is required. --txid <txid> withdraws only the card asked under\n" +
			"that txid (a bead keeps its overtaken cards in the note): the reply goes under that card, and\n" +
			"the bead's other cards and their note stay; without it the whole note is cleared.\n" +
			"--dry-run prints the three acts and does none.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gateway, _, err := posternGateway()
			if err != nil {
				return err
			}
			return application.PosternClear{
				Beads:  gateway,
				Notes:  gateway,
				Sender: newHandsPush(gateway),
				Txid:   txid,
				DryRun: dryRun,
				Out:    cmd.OutOrStdout(),
			}.Run(cmd.Context(), args[0], why)
		},
	}
	cmd.Flags().StringVar(&why, "why", "", "why the card is cleared: it goes on the bead and under the card (required)")
	cmd.Flags().StringVar(&txid, "txid", "", "withdraw only the card asked under this txid, leaving the bead's other cards")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the three acts it would do, and change nothing")
	_ = cmd.MarkFlagRequired("why")
	return cmd
}
