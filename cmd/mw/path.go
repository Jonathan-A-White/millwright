package main

import (
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"

	"github.com/spf13/cobra"
)

// newPathCmd builds `mw path`: setting and reading a story's path keys.
func newPathCmd() *cobra.Command {
	path := &cobra.Command{
		Use:   "path",
		Short: "Set or show a story's path",
	}
	path.AddCommand(newPathSetCmd(), newPathShowCmd())
	return path
}

func newPathSetCmd() *cobra.Command {
	var from string
	cmd := &cobra.Command{
		Use:   "set <story-id> [--from <sibling-id>] [key=value ...]",
		Short: "Set a story's path keys in one write",
		Long: "set gives a story its path keys (rig, branch, harness, model, effort, formula, host,\n" +
			"files) in one write, in place of one bd update --set-metadata call for each. files is\n" +
			"the comma-separated repo paths or globs the story expects to change: dispatch does not\n" +
			"start it while a running story on its rig shares one.\n\n" +
			"  mw path set B --from A               copy the seven keys from the story A\n" +
			"  mw path set B --from A model=opus    copy them, with model overridden\n" +
			"  mw path set B rig=lampas             set only the rig\n" +
			"  mw path set B files=src/Reader.tsx,src/data/*.ts\n\n" +
			"Only the seven keys are copied from the sibling, never its files nor its other\n" +
			"metadata. An unknown key, a sibling with no rig, and a story that is in progress or\n" +
			"closed are refused, and nothing is written: a path is adjusted only before a story starts.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			_, err = application.PathSet{Tracker: gateway, Out: cmd.OutOrStdout()}.
				Run(cmd.Context(), args[0], from, args[1:])
			return err
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "the sibling story to copy the seven path keys from")
	return cmd
}

func newPathShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <story-id>",
		Short: "Print a story's eight path keys",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			_, err = application.PathShow{Tracker: gateway, Out: cmd.OutOrStdout()}.Run(cmd.Context(), args[0])
			return err
		},
	}
}
