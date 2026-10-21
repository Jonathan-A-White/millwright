package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/grist"
)

// newGristTalksCmd builds `mw grist talks`: the talks the mill keeps a live
// session for, one a line.
func newGristTalksCmd() *cobra.Command {
	var asJSON, end bool
	cmd := &cobra.Command{
		Use:   "talks [app] | --end <app> <talk>",
		Short: "List the talks the mill keeps a live Claude session for",
		Long: "mw grist talks lists every talk whose session the mill would resume now (its last turn under an\n" +
			"hour ago), the most lately used first, one a line: the app, the talk, its turns, the context\n" +
			"tokens of its last turn, that turn's cache read, the time since its last use, and the session's\n" +
			"id. An app names only its talks. --json prints the same as a JSON list. It reads the records\n" +
			"under the mill's state directory (talks/) and changes nothing.\n\n" +
			"mw grist talks --end <app> <talk> ends that talk's session now, writing no key points: its next\n" +
			"turn starts a fresh session with the app's history, and its transcript is deleted a day later,\n" +
			"when the next talk grist for the app comes. (A session is also ended, with key points, at\n" +
			"[grist] talk_context_cap or talk_max_turns, and one unused for 30 days is ended and its\n" +
			"transcript deleted.)",
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			stateDir, err := config.GristStateDir()
			if err != nil {
				return err
			}
			if end {
				if len(args) != 2 || asJSON {
					return fmt.Errorf("mw grist talks --end takes the app and the talk: mw grist talks --end <app> <talk>")
				}
				_, err = application.GristTalksEnd{Store: grist.NewTalks(stateDir), Out: cmd.OutOrStdout()}.Run(cmd.Context(), args[0], args[1])
				return err
			}
			if len(args) > 1 {
				return fmt.Errorf("mw grist talks takes at most the app: a talk is named only with --end")
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
	cmd.Flags().BoolVar(&end, "end", false, "end the named talk's session now (mw grist talks --end <app> <talk>), writing no key points")
	return cmd
}
