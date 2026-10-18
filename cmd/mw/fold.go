package main

import (
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"

	"github.com/spf13/cobra"
)

// newFoldCmd builds `mw fold <rig>`: the Mayor's weekly list of bugs that share
// a cause (mw-gq6.588).
func newFoldCmd() *cobra.Command {
	var min int
	cmd := &cobra.Command{
		Use:   "fold <rig> [--min N]",
		Short: "List a rig's open and held bugs grouped by error class and by screen, the groups of 3 or more first",
		Long: "fold lists the rig's open and held bugs in groups, the largest first: a bug carrying a class:<slug> label\n" +
			"(mw tester file --class) goes under that class; any other goes under the screen its title names, the\n" +
			"words before the first ':' after the app name ('Lampas Review:'). Each group shows its key, and its\n" +
			"bugs' ids and titles. A bug a session is working is shown, marked 'in progress: do not fold'. Iced bugs\n" +
			"are left out. --min N (default 3) is the smallest group listed.\n\n" +
			application.FoldRule + "\n\n" +
			"It runs no model and writes nothing: it is the list the Mayor's weekly fold pass starts from.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			_, err = application.Fold{Tracker: gateway, Rig: args[0], Min: min, Out: cmd.OutOrStdout()}.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().IntVar(&min, "min", application.DefaultFoldMin, "the smallest group listed")
	cmd.AddCommand(newFoldSupersedeCmd())
	return cmd
}

// newFoldSupersedeCmd builds `mw fold supersede <fold-id> [--dry-run]`: the
// closing of a fold's bugs as superseded by it (mw-gq6.606).
func newFoldSupersedeCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "supersede <fold-id> [--dry-run]",
		Short: "Close each bug a fold carries as superseded by it, after moving what waited on the bug to wait on the fold",
		Long: "supersede reads the ids under 'Folded bugs' in the fold's description. A bug a session is working, one\n" +
			"already closed, and one whose id is not in the fold's acceptance criteria are left alone and said. For\n" +
			"each other bug, every open story that waits on it now waits on the fold (a bug of the same fold: nothing;\n" +
			"a bug of another fold: that fold waits on this one; an edge the tracker refuses as a cycle is left out and\n" +
			"said), an open story held while it is moved; then the bug is closed as superseded by the fold. One line\n" +
			"per bug. --dry-run writes nothing and prints the same plan.\n\n" + application.FoldRule,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			_, err = application.FoldSupersede{Tracker: gateway, Fold: args[0], DryRun: dryRun, Out: cmd.OutOrStdout()}.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "write nothing and print the plan")
	return cmd
}
