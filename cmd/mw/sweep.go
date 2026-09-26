package main

import (
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"

	"github.com/spf13/cobra"
)

// newSweepCmd builds `mw sweep`: for this host, the claimed stories whose
// lease has run out with no heartbeat since, marked run=stuck. It is
// read-mostly and zero-token — it never kills or restarts a session, never
// gives a claim back, and never touches a worktree, git or the ledger.
func newSweepCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sweep",
		Short: "Find claimed stories on this host whose lease has run out",
		Long: "sweep reads this host's claimed stories and asks the tracker which of them StaleClaims\n" +
			"lists: a lease that ran out with no heartbeat since, bd's own definition of a claim whose\n" +
			"session went away. Each one found is commented on the bead once and recorded run=stuck; a\n" +
			"story mw next or an earlier sweep already recorded gone is left alone.\n\n" +
			"sweep never kills or restarts a session, never gives a claim back, and never touches a\n" +
			"worktree, git or the ledger. Settling a stuck claim is a separate command.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}

			gateway := mwGateway(dir, host)
			_, err = application.Sweep{
				Tracker: gateway,
				Host:    host,
				Out:     cmd.OutOrStdout(),
			}.Run(cmd.Context())
			return err
		},
	}
}
