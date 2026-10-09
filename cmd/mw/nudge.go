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
			"Mayor about it since it was claimed, counted from the latest claim a dispatch recorded. It\n" +
			"then reads every other host that holds a claim and, of each, whether its last recorded sync\n" +
			"is older than nudge_sync_stale_minutes (default 20): a host that is simply off, holding no\n" +
			"claim, is not an alarm. On a host the vault's home file says is not home, it prints nothing.\n" +
			"A need that has waited on the Mayor more than 30 minutes (the ones mw status lists under\n" +
			"WAITING ON THE MAYOR) is a clause too, keyed mayor.<bead>.\n" +
			"A story whose close-out (mw next) is running on this host is not quiet and is left out, however\n" +
			"long it has been claimed: mw status shows it as closing out, and as waiting for calm when it is.\n" +
			"When this host's own sync is halted, the other hosts' ages are read off notes\n" +
			"this host cannot currently refresh, so they are left out in favour of one clause naming\n" +
			"this host's own halt instead — unless beads_sync is backup or shared, where every host's note\n" +
			"is read live out of the one database and the ages are kept beside the halt. For each clause\n" +
			"that holds, it prints one line, tab-separated:\n" +
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
			setting, err := hostBeads(cmd.Context(), mwVault(dir, host), host)
			if err != nil {
				return err
			}

			tracker := mwGateway(dir, host)
			clauses, err := application.Nudge{
				Tracker:    tracker,
				Notes:      tracker,
				Mayor:      application.MayorReader{Tracker: tracker, Notes: tracker},
				Host:       host,
				Home:       mwVault(dir, host),
				SyncHalt:   hostSyncHalt(),
				CloseOuts:  hostCloseOuts(),
				SyncMode:   setting.Mode(),
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
