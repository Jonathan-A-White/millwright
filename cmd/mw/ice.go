package main

import (
	"fmt"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"

	"github.com/spf13/cobra"
)

// newIceCmd builds `mw ice`: the icebox as one word.
func newIceCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "ice <id> --reason <text>",
		Short: "Put a bead in the icebox: label icebox, priority 4, held, one comment",
		Long: "ice sets the label icebox, the priority 4 and the status held on a bead and writes the\n" +
			"comment 'Iced: <reason>': work that is not decided against, only not yet. mw status counts\n" +
			"it on its ICEBOX line and not among the held, and mw release leaves it held.\n\n" +
			"An epic, a finished bead and a claimed one are refused, and nothing is written.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			if err := (application.Ice{Tracker: gateway, Notes: gateway}).Run(cmd.Context(), args[0], reason); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Iced %s.\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why the bead is iced (required)")
	return cmd
}

// newThawCmd builds `mw thaw`: the icebox's way out.
func newThawCmd() *cobra.Command {
	var priority int
	cmd := &cobra.Command{
		Use:   "thaw <id> [--priority n]",
		Short: "Take a bead out of the icebox: label off, priority set (default 3), still held",
		Long: "thaw takes the label icebox off a bead, sets its priority (3 unless --priority says\n" +
			"otherwise) and writes the comment 'Thawed'. The bead stays held: mw release is the yes.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			var level *int
			if cmd.Flags().Changed("priority") {
				level = &priority
			}
			if err := (application.Thaw{Tracker: gateway, Notes: gateway}).Run(cmd.Context(), args[0], level); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Thawed %s; it is still held.\n", args[0])
			return nil
		},
	}
	cmd.Flags().IntVar(&priority, "priority", application.ThawedPriority, "the priority it comes back at, 0 to 4")
	return cmd
}
