package main

import (
	"fmt"
	"os"
	"strings"

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
	var overHold string
	cmd := &cobra.Command{
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
			"exist in <rig>: wrong rig?'.\n\n" +
			"A held story whose own comments, or its parent's naming that story's id, say it is held on the Governor's\n" +
			"word (a comment opening 'HELD:' that names the 'Governor' or 'his word'; the same words in\n" +
			"other prose are no hold; not older than the story's last release over a hold) is not released: the release is refused, printing that comment, and\n" +
			"nothing in the epic is released. --over-hold \"<why>\" releases it anyway and writes the why\n" +
			"on the story.",
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
				OverHold: overHold,
				Out:      cmd.OutOrStdout(),
			}.Run(cmd.Context(), args[0])
			return err
		},
	}
	cmd.Flags().StringVar(&overHold, "over-hold", "", "release a story held on the Governor's word anyway, for this reason (written on the story)")
	return cmd
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

// newReleasePacedCmd builds `mw release-paced`: how the next stories of a long
// list filed held are un-held at the epic's pace, in place of by hand at each
// sitting.
func newReleasePacedCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "release-paced <list-file>",
		Short: "Un-hold the next stories of a list file at the pace of the epic: at most 3 at once, none over 60% of the week",
		Long: "release-paced reads a list file of story ids, one a line, in release order (blank lines and\n" +
			"lines starting # are left out), and un-holds the next ones that are held.\n\n" +
			"The pace is the epic's: never more than 3 stories of the list running or open at once, and\n" +
			"none while mw usage show reads over 60% of the week. A story of the list that is open or\n" +
			"running counts against the 3 and is left as it is; one that is closed, not in the tracker,\n" +
			"iced, or held on the Governor's word is skipped and named. When it releases none it says why:\n" +
			"the usage reading, the count, or that the reading is missing or stale (mw usage read).\n\n" +
			"Each story released gets a comment quoting the usage reading and the count it acted on.\n" +
			"--dry-run releases nothing, writes nothing, and prints what it would.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("reading the list: %w", err)
			}
			var ids []string
			for _, line := range strings.Split(string(text), "\n") {
				if line, _, _ = strings.Cut(line, "#"); strings.TrimSpace(line) != "" {
					ids = append(ids, strings.TrimSpace(line))
				}
			}
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			_, err = application.ReleasePaced{
				Tracker:  gateway,
				Readings: application.UsageReadings{Notes: mwGateway(dir, host)},
				DryRun:   dryRun,
				Out:      cmd.OutOrStdout(),
			}.Run(cmd.Context(), ids)
			return err
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "release nothing; print what would be released")
	return cmd
}
