package main

import (
	"fmt"
	"os"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/reaper"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/spf13/cobra"
)

// newDeputyCmd builds `mw deputy`: this host's Deputy, brought up. It is `mw
// seat up deputy` with the model, the effort, the reaper and the kickoff's
// words settled, so that the Mayor has one command to fire.
func newDeputyCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "deputy [--reason text]",
		Short: "Bring up this host's Deputy, or say it is already up",
		Long: "deputy starts the Deputy's next session on this host, as `mw seat up deputy` does, in a window\n" +
			"of its own that closes itself once the session has handed off (--reap-when-idle), at high effort\n" +
			"on config `deputy_model` (sonnet), also read from $" + config.DeputyModelEnv + ". The kickoff tells it to\n" +
			"arm mail-wait with MW_MAIL_MAILBOX=" + application.DeputyMailbox + ", work the mail, report by mail and hand off\n" +
			"when idle, and then the --reason it was brought up.\n\n" +
			"It refuses and starts nothing when seats/deputy has no charter.md. If a window named deputy-* is\n" +
			"already open, it starts nothing, says so in one line and leaves with status " + fmt.Sprint(application.DeputyUpExit) + ",\n" +
			"so the Mayor can mail the Deputy and fire, and tell \"already up\" from a failure.",
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
			model, err := config.DeputyModel()
			if err != nil {
				return err
			}
			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("finding the mw that is running, to arm a reaper with: %w", err)
			}

			windows := seatWindows()
			_, err = application.Deputy{
				Seats:    vault.New(dir),
				Windows:  windows,
				Harness:  sessionHarness(dir, host),
				Terminal: windows,
				Armer:    reaper.New(exe),

				Host:   host,
				Reason: reason,
				Model:  domain.Model(model),

				Out: cmd.OutOrStdout(),
			}.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why the Deputy is being brought up, told to it after its standing instructions")
	return cmd
}
