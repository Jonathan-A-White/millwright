package main

import (
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"

	"github.com/spf13/cobra"
)

// newCauseCmd builds `mw cause <story-id> <class> "<why>" [--fix <bead>]`: the
// Mayor's naming of why a story failed, kept where it can be counted.
func newCauseCmd() *cobra.Command {
	var fix string
	cmd := &cobra.Command{
		Use:   `cause <story-id> <` + strings.Join(application.CauseClasses, "|") + `> "<why>" [--fix <bead>]`,
		Short: "Record why a story failed: its class of cause and the reason",
		Long: "cause records on a story why it failed: one class of cause (" + strings.Join(application.CauseClasses, ", ") + "),\n" +
			"the reason in a few words, and with --fix the bead that mends it. The class, reason and fix\n" +
			"are written as the story's metadata (cause, cause_why, cause_fix) so that causes can be\n" +
			"counted, and as one comment beginning \"CAUSE (<class>):\" so that a person reading the\n" +
			"story finds it. Naming a cause again replaces the class and adds another comment.\n\n" +
			"A class outside the four, an empty reason, and a story or fix that is not a bead are\n" +
			"refused, and nothing is written. Nothing else of the story is touched: not its status,\n" +
			"its claim nor its attempts.",
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			_, err = application.Cause{Tracker: gateway, Out: cmd.OutOrStdout()}.
				Run(cmd.Context(), args[0], args[1], args[2], fix)
			return err
		},
	}
	cmd.Flags().StringVar(&fix, "fix", "", "the bead that mends the cause")
	return cmd
}
