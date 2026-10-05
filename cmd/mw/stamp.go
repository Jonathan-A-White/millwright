package main

import (
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"

	"github.com/spf13/cobra"
)

// newStampCmd builds `mw stamp`: queue a chain stamp of a rig's head by hand.
func newStampCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "stamp [<rig> [<commit>]] [--all]",
		Short: "Queue a chain stamp of a rig's head, or of a commit, by hand",
		Long: "stamp queues a chain stamp for the chain-stamp job to broadcast: the commit named (in full or\n" +
			"by its first characters), or by default the head of origin's default branch of the rig's\n" +
			"checkout, read after a fetch. The stamp has no story and the title \"Head of <rig>: <subject>\".\n" +
			"It prints what it queued.\n\n" +
			"A commit that is already stamped is refused: \"already stamped: <txid>\" for a sent one,\n" +
			"\"already stamped: queued, not yet sent\" for one still waiting.\n\n" +
			"--all does this for every rig in the config's [rigs] table whose head has no stamp, naming\n" +
			"the rigs it skips. Landings and vault pushes are stamped on their own; this is for a head\n" +
			"neither saw. docs/chain-stamps.md.",
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			rigs, err := config.Rigs()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			queue, err := stampQueue()
			if err != nil {
				return err
			}
			var name, commit string
			if len(args) > 0 {
				name = args[0]
			}
			if len(args) > 1 {
				commit = args[1]
			}
			return application.StampHead{
				Rigs: rigs, Heads: rig.New(), Queue: queue, Store: queue, Host: host, Out: cmd.OutOrStdout(),
			}.Run(cmd.Context(), name, commit, all)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "stamp the head of every rig in [rigs] that has no stamp")
	return cmd
}
