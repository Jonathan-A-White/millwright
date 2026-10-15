package main

import (
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/spf13/cobra"
)

// newReleaseCmd builds `mw release`: how a plan filed earlier is approved. The
// Governor is rarely at the machine when the Mayor files a plan, and `mw file`
// holds everything it files, so this is the other half of filing — and the only
// way to approve a plan without filing a second copy of it.
func newReleaseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "release <epic-id|story-id>",
		Short: "Release the held stories of a plan filed earlier, or one held story",
		Long: "release reads an epic that is already in the tracker and prints the same tree mw file\n" +
			"printed when it filed it — every story with the path it is worked by, what it still waits\n" +
			"on, and what the tracker says it is — and then releases the stories that are still held.\n\n" +
			"Running it is the approval, so nobody is asked anything. Nothing else is touched: a story\n" +
			"already taken, already finished or already released is left exactly as it was found, and\n" +
			"releasing the same epic twice does no more than releasing it once. Which of the released\n" +
			"stories a dispatcher may take is the tracker's own answer: the ones that wait on nothing\n" +
			"still to be done.\n\n" +
			"Given the id of a story instead of an epic, release prints that story's entry of the tree and\n" +
			"releases that one story when it is held. A story that is not held is left as it is and said,\n" +
			"and a story whose path names no rig or no target branch is refused.\n\n" +
			"On a rig whose file in the vault says files_expected = true, a story with no files key is\n" +
			"named under the tree, one line each: a warning, and the release goes ahead. So is a story\n" +
			"none of whose files exist in its rig's checkout on this host: '<id>: none of its files\n" +
			"exist in <rig>: wrong rig?'.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			// A rig table that cannot be read costs the release only the wrong-rig warning.
			rigs, _ := config.Rigs()
			_, err = application.Release{
				Tracker:  gateway,
				Rules:    rigRulesIfKnown(),
				Rigs:     rigs,
				RigFiles: rig.Files{},
				Out:      cmd.OutOrStdout(),
			}.Run(cmd.Context(), args[0])
			return err
		},
	}
}

// rigRulesIfKnown is the rig files of the vault, or nil when the vault cannot
// be found: a warning that reads them is not worth failing the command for.
func rigRulesIfKnown() application.EpicRules {
	dir, err := config.Vault()
	if err != nil {
		return nil
	}
	return vault.New(dir)
}
