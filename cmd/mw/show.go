package main

import (
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"

	"github.com/spf13/cobra"
)

// newShowCmd builds `mw show`: the tree of a plan filed earlier, read and
// nothing else. The Governor approves from a phone, later, and needs to see what
// he is approving before he says yes; `mw release` is the yes.
func newShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <epic-id>",
		Short: "Print the tree of a plan filed earlier, changing nothing",
		Long: "show reads an epic that is already in the tracker and prints the same tree mw release\n" +
			"prints — every story with the path it is worked by, what it still waits on, and what the\n" +
			"tracker says it is — and stops. Nothing is written: a held story stays held, so this is\n" +
			"how to see a plan before approving it with mw release.\n\n" +
			"An epic filed under the epic is named as an epic, not listed as a story; its own stories\n" +
			"are shown by asking for it. An id that is not an epic is refused.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			_, err = application.Show{
				Tracker: gateway,
				Out:     cmd.OutOrStdout(),
			}.Run(cmd.Context(), args[0])
			return err
		},
	}
}
