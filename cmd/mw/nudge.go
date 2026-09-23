package main

import (
	"fmt"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"

	"github.com/spf13/cobra"
)

// newNudgeCmd builds `mw nudge`: what contrib/mail-notify's quiet alarm has to
// say, this tick. It is read-only and zero-token — it starts no session, and
// it writes nothing to beads, the ledger, git or tmux.
func newNudgeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "nudge",
		Short: "Print the quiet alarm's clauses for this host, one a line",
		Long: "nudge reads this host's claimed stories and, of each, whether it has run longer than\n" +
			"nudge_after_minutes (default 60) with nothing landed, refused or blocked mailed to the\n" +
			"Mayor about it since it was claimed. It then reads every other host a story is pathed to\n" +
			"and, of each, whether its last recorded sync is older than nudge_sync_stale_minutes\n" +
			"(default 20). When this host's own sync is halted, the other hosts' ages are read off notes\n" +
			"this host cannot currently refresh, so they are left out in favour of one clause naming\n" +
			"this host's own halt instead. For each clause that holds, it prints one line, tab-separated:\n" +
			"a key a caller can damp a condition still holding by, and the clause itself. contrib/mail-notify\n" +
			"is the only caller: it composes the clauses into the one line it types into the Mayor's\n" +
			"window, damped to at most once a condition an hour.\n\n" +
			"Nothing is claimed, nothing is written and no session is started: nudge only reads.",
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
			after, err := config.NudgeAfterMinutes()
			if err != nil {
				return err
			}
			stale, err := config.NudgeSyncStaleMinutes()
			if err != nil {
				return err
			}

			tracker := mwGateway(dir, host)
			clauses, err := application.Nudge{
				Tracker:    tracker,
				Notes:      tracker,
				Host:       host,
				SyncHalt:   hostSyncHalt(),
				NudgeAfter: time.Duration(after) * time.Minute,
				SyncStale:  time.Duration(stale) * time.Minute,
			}.Run(cmd.Context())
			if err != nil {
				return err
			}
			for _, c := range clauses {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", c.Key, c.Text)
			}
			return nil
		},
	}
}
