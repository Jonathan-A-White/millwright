package main

import (
	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/grist"
)

// newGristTalksCmd builds `mw grist talks`: the talks the mill keeps a live
// session for, one a line.
func newGristTalksCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "talks [app]",
		Short: "List the talks the mill keeps a live Claude session for",
		Long: "mw grist talks lists every talk whose session the mill would resume now (its last turn under an\n" +
			"hour ago), the most lately used first, one a line: the app, the talk, its turns, the context\n" +
			"tokens of its last turn, that turn's cache read, the time since its last use, and the session's\n" +
			"id. An app names only its talks. --json prints the same as a JSON list. It reads the records\n" +
			"under the mill's state directory (talks/) and changes nothing.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			stateDir, err := config.GristStateDir()
			if err != nil {
				return err
			}
			app := ""
			if len(args) == 1 {
				app = args[0]
			}
			_, err = application.GristTalksList{Store: grist.NewTalks(stateDir), Out: cmd.OutOrStdout()}.Run(cmd.Context(), app, asJSON)
			return err
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the talks as a JSON list")
	return cmd
}
