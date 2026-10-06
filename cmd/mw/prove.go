package main

import (
	"github.com/Jonathan-A-White/millwright/application"

	"github.com/spf13/cobra"
)

// newProveCmd builds `mw prove`: what the chain says of a stamped commit.
func newProveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prove <rig> <commit>",
		Short: "Print the chain's proof of a stamped commit: txid, block height and time, preimage, explorer URL",
		Long: "prove finds the stamp the chain-stamp job sent for a commit of a rig — the commit in full or by\n" +
			"its first characters — and asks WhatsOnChain (testnet) about its transaction. It prints the\n" +
			"txid, the block's height and time (UTC), the preimage the stamp's public commitment is made\n" +
			"of (the rig and the commit) and the commitment, and the explorer's URL. A stamp whose\n" +
			"transaction has no block yet is said to be in the mempool.\n\n" +
			"A commit that has no sent stamp is an error: \"no stamp for <rig> <commit>\", exit status 1.\n" +
			"A stamp still waiting to be broadcast is not sent, and is not found.\n\n" +
			"The same txid is a git note on the commit, in the rig's checkout, under refs/notes/chain:\n" +
			"git log --notes=chain. docs/chain-stamps.md.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			queue, err := stampQueue()
			if err != nil {
				return err
			}
			keys, err := posternKeys()
			if err != nil {
				return err
			}
			backend, err := posternBackend(keys)
			if err != nil {
				return err
			}
			return application.Prove{
				Stamps: queue,
				Chain:  newChain(backend, keys, nil),
				Out:    cmd.OutOrStdout(),
			}.Run(cmd.Context(), args[0], args[1])
		},
	}
}
