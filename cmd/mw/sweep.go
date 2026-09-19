package main

import (
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"

	"github.com/spf13/cobra"
)

// newSweepCmd builds `mw sweep`: for this host, the claimed stories whose
// session is gone or has gone quiet, marked run=stuck. It is read-mostly and
// zero-token — it never kills or restarts a session, never gives a claim
// back, and never touches a worktree, git or the ledger.
func newSweepCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sweep",
		Short: "Find claimed stories on this host whose session is gone or silent",
		Long: "sweep reads this host's claimed stories and asks the runner about each one's session. A\n" +
			"session that is no longer there is reported stuck straight away. A session that is still\n" +
			"there but has printed nothing new for longer than the stale threshold (config `stale_hours`,\n" +
			"default 2) is reported stuck too. Either way the finding is commented on the bead once and\n" +
			"recorded run=stuck; a story mw next or an earlier sweep already recorded gone is left alone.\n\n" +
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
			hours, err := config.StaleHours()
			if err != nil {
				return err
			}

			_, err = application.Sweep{
				Tracker:    mwGateway(dir, host),
				Runner:     tmux.New(),
				Host:       host,
				StaleAfter: time.Duration(hours) * time.Hour,
				Out:        cmd.OutOrStdout(),
			}.Run(cmd.Context())
			return err
		},
	}
}
