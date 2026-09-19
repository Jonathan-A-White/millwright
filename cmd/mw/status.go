package main

import (
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"

	"github.com/spf13/cobra"
)

// newStatusCmd builds `mw status`: what this host is doing right now, for a
// phone. It is read-only and zero-token — it starts no session, and it writes
// nothing to beads, the ledger, git or tmux.
func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show what is running, ready and blocked on this host",
		Long: "status reads this host's running stories (with the tmux session name to attach to), what\n" +
			"is ready to be taken, what is blocked on an unfinished dependency, and today's fuel summed\n" +
			"from the Builder's ledger. A story filed with only its path overrides is shown with the\n" +
			"epic's defaults filled in. A claimed story whose close-out is blocked by an open formula\n" +
			"step says so, and a story recorded run=stopped or run=stuck is shown as that, not running.\n\n" +
			"It then shows every other host a story is pathed to: when that host last recorded itself\n" +
			"level, and what it holds. A host silent for longer than the threshold (config\n" +
			"`host_silent_hours`, default 2), or that never synced at all, is marked asleep and its work\n" +
			"is listed as stranded, with the one line that re-paths a story here. Re-pathing is a\n" +
			"person's act: status only says which stories are waiting for one.\n\n" +
			"Every line fits a phone-width terminal, at most 60 columns. Nothing is claimed, nothing is\n" +
			"written and no session is started: status only reads.",
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
			hours, err := config.HostSilentHours()
			if err != nil {
				return err
			}

			tracker := mwGateway(dir, host)
			_, err = application.Status{
				Tracker:     tracker,
				Notes:       tracker,
				Vault:       mwVault(dir, host),
				Host:        host,
				Seat:        BuilderSeat,
				HostSilence: time.Duration(hours) * time.Hour,
				Out:         cmd.OutOrStdout(),
			}.Run(cmd.Context())
			return err
		},
	}
}
