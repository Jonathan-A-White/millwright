package main

import (
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"

	"github.com/spf13/cobra"
)

// newQueueCmd builds `mw queue`: what a dispatch would start next, and why.
func newQueueCmd() *cobra.Command {
	var host, rig string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "queue",
		Short: "Show the ready stories in the order a dispatch starts them, and the next blocked ones",
		Long: "queue prints the stories ready on a host in the exact order mw dispatch starts them (the\n" +
			"most urgent first, and of equal priority the one filed longest ago), with the reason each\n" +
			"sits where it does: its priority, its age and its size. A story a dispatch would pass over\n" +
			"for now says why under it: its files are shared with a running story, or it needs the\n" +
			"Governor present. After them come the next five blocked stories, each with the stories it\n" +
			"waits on.\n\n" +
			"--host names another host (default: this one) and --rig narrows both lists to one rig.\n" +
			"--json prints the same report as JSON. It only reads: nothing is claimed, written or\n" +
			"started, and it takes the order from the dispatch's own sort, never a copy of it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			if host == "" {
				if host, err = config.Host(); err != nil {
					return err
				}
			}
			_, err = application.Queue{
				Tracker: gateway, Host: host, Rig: rig, Rules: rigRulesIfKnown(),
				JSON: asJSON, Out: cmd.OutOrStdout(),
			}.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().StringVar(&host, "host", "", "the host to show the queue of (default: this host)")
	cmd.Flags().StringVar(&rig, "rig", "", "show only the stories of this rig")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the report as JSON")
	return cmd
}
