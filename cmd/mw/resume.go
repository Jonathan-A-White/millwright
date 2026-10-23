package main

import (
	"encoding/json"
	"fmt"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/pause"

	"github.com/spf13/cobra"
)

// newResumeCmd builds `mw resume`: what mw pause did, undone from the paused
// set (mw-gq6.707).
func newResumeCmd() *cobra.Command {
	var dryRun, here bool
	var order string
	cmd := &cobra.Command{
		Use:   "resume [--dry-run]",
		Short: "Resume the factory from the paused set: clear the hold notes, tell idle Builders to carry on, retry the gone ones",
		Long: "resume is run on the home, by hand or by the timer mw pause --until scheduled. From the paused set\n" +
			"in plans/pause-<date>/paused.json it: clears the hold note of every host, types RESUME into each\n" +
			"Builder's session still alive, runs mw retry for each story whose session is gone (a cloud box's\n" +
			"were ended, so its stories start again from their pushed branches), puts cloud.max_boxes back as\n" +
			"the set recorded it, cancels the timer if one is waiting, marks the set resumed and mails the\n" +
			"Mayor. A second resume, or one with no pause in force, changes nothing and says so. The mw-seats\n" +
			"windows are never touched. --dry-run prints the set and changes nothing.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			if here {
				var o application.HostResumeOrder
				if err := json.Unmarshal([]byte(order), &o); err != nil {
					return fmt.Errorf("--order does not read: %w", err)
				}
				local, err := pauseHostSide(dir, host)
				if err != nil {
					return err
				}
				got, err := local.Resume(cmd.Context(), o)
				if err != nil {
					return err
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(got)
			}
			parts, err := pauseParts(cmd.Context(), dir, host, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			gateway := mwGateway(dir, host)
			return application.Resume{
				Fleet: parts.fleet, Config: parts.config, Book: parts.book, Timer: parts.timer,
				Retry: pause.Retry{Mw: parts.mw}, Mail: gateway, Tracker: gateway, Host: host, Out: cmd.OutOrStdout(),
			}.Run(cmd.Context(), dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the paused set and change nothing")
	cmd.Flags().BoolVar(&here, "here", false, "this host's part only, as the home asks it over ssh; prints JSON")
	cmd.Flags().StringVar(&order, "order", "", "with --here: the order, as JSON")
	_ = cmd.Flags().MarkHidden("here")
	_ = cmd.Flags().MarkHidden("order")
	return cmd
}
