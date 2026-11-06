package main

import (
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"

	"github.com/spf13/cobra"
)

// newSweepCmd builds `mw sweep`: for this host, the claimed stories whose
// lease has run out with no heartbeat since, marked run=stuck, and the idle
// tmux sessions of stories the tracker holds closed, closed. It is
// read-mostly and zero-token — it kills no other session, never restarts one,
// never gives a claim back, and never touches a worktree, git or the ledger.
func newSweepCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sweep",
		Short: "Find claimed stories on this host whose lease has run out",
		Long: "sweep reads this host's claimed stories and asks the tracker which of them StaleClaims\n" +
			"lists: a lease that ran out with no heartbeat since, bd's own definition of a claim whose\n" +
			"session went away. Each one found is commented on the bead once and recorded run=stuck; a\n" +
			"story mw next or an earlier sweep already recorded gone is left alone.\n\n" +
			"sweep also closes the tmux session mw-<id> of a story the tracker holds closed (landed and\n" +
			"verified) when its pane is idle: the command in it has ended, or only a shell is in the\n" +
			"foreground. A session of a story still open or running, a pane at work, and a session that\n" +
			"is no story's (a seat's, a tester's) are never touched. A host with no tmux or no sessions\n" +
			"says so.\n\n" +
			"sweep never restarts a session, never gives a claim back, and never touches a worktree, git\n" +
			"or the ledger. Settling a stuck claim is a separate command.",
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
				Tracker:  gateway,
				Host:     host,
				Sessions: tmux.New(),
				Out:      cmd.OutOrStdout(),
			}.Run(cmd.Context())
			return err
		},
	}
}
