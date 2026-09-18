package main

import (
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"

	"github.com/spf13/cobra"
)

// newReleaseCmd builds `mw release`: how a plan filed earlier is approved. The
// Governor is rarely at the machine when the Mayor files a plan, and `mw file`
// holds everything it files, so this is the other half of filing — and the only
// way to approve a plan without filing a second copy of it.
func newReleaseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "release <epic-id>",
		Short: "Release the held stories of a plan filed earlier",
		Long: "release reads an epic that is already in the tracker and prints the same tree mw file\n" +
			"printed when it filed it — every story with the path it is worked by, what it still waits\n" +
			"on, and what the tracker says it is — and then releases the stories that are still held.\n\n" +
			"Running it is the approval, so nobody is asked anything. Nothing else is touched: a story\n" +
			"already taken, already finished or already released is left exactly as it was found, and\n" +
			"releasing the same epic twice does no more than releasing it once. Which of the released\n" +
			"stories a dispatcher may take is the tracker's own answer: the ones that wait on nothing\n" +
			"still to be done.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			_, err = application.Release{
				Tracker: gateway,
				Out:     cmd.OutOrStdout(),
			}.Run(cmd.Context(), args[0])
			return err
		},
	}
}
