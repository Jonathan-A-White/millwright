package main

import (
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"

	"github.com/spf13/cobra"
)

// newBriefCmd builds `mw brief`: what a booting seat needs of a bead, which is
// what is still live under it and not the whole of what `bd show` would print.
// It is read-only and writes nothing to beads.
func newBriefCmd() *cobra.Command {
	var comments int

	cmd := &cobra.Command{
		Use:   "brief <bead-id>...",
		Short: "Print the live children of one or more beads, for a seat to boot from",
		Long: "brief prints, for each bead named and in the order given: one line with its title, id,\n" +
			"status and priority; then its children that are not closed, under the headings In progress,\n" +
			"Open and Held, each line carrying title, id, priority, the host and model its path names and,\n" +
			"for each open blocker, 'waits on <title>'; then one line saying how many children are\n" +
			"closed. It prints no description and no closed child.\n\n" +
			"With --comments N the newest N comments of each bead follow in full, never truncated: they\n" +
			"hold the Governor's words. Nothing else is printed of a comment's bead.\n\n" +
			"An id the tracker does not have is an error naming it, and nothing is printed. Plain text,\n" +
			"written to standard output; nothing is written to beads.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			_, err = application.Brief{
				Tracker:  mwGateway(dir, host),
				Comments: comments,
				Out:      cmd.OutOrStdout(),
			}.Run(cmd.Context(), args...)
			return err
		},
	}
	cmd.Flags().IntVar(&comments, "comments", 0, "print the newest N comments of each bead, in full")
	return cmd
}
