package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/reaper"
	"github.com/Jonathan-A-White/millwright/infrastructure/ticklog"
	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/spf13/cobra"
)

// MillhandTickStateDir is where mw millhand tick keeps its log, under the home
// directory. It is this host's own: nothing in it is synced anywhere.
var MillhandTickStateDir = filepath.Join(".local", "state", "mw-millhand-tick")

// newMillhandTickCmd builds `mw millhand tick`: what the routine timer runs.
func newMillhandTickCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "tick [--dry-run]",
		Short: "Wake the Millhand only if mail has come for it or a story is stuck",
		Long: "tick is run every 15 minutes by a timer, and spends no tokens unless the Millhand is needed.\n" +
			"If a window named millhand-* is already open it says \"already up\" and stops. Otherwise it runs\n" +
			"one mw sync (a sync that fails is said in the line, and the tick looks on this host all the\n" +
			"same), then looks for unread mail for millhand@<host> or millhand, and for a story mw sweep\n" +
			"newly finds stuck on this host. With neither it says \"quiet\". With either, or both, it starts\n" +
			"ONE routine wake of the Millhand (as `mw millhand --wake routine` does) whose reason lists the\n" +
			"mail subjects and the stuck story titles, five of each and then a count. The mail is left unread.\n\n" +
			"It prints one dated line and appends it to ~/.local/state/mw-millhand-tick/log on this host,\n" +
			"which is cut to its last " + fmt.Sprint(application.TickLogLines) + " lines. It leaves with 0 for everything but a fault of its\n" +
			"own, a wake that could not be started included. With --dry-run it says what it would do and\n" +
			"starts nothing; it does not sweep, since a sweep records the stories it finds stuck, and it\n" +
			"writes nothing to the log. It does not consult mw watch.",
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
			routine, err := config.MillhandRoutineModel()
			if err != nil {
				return err
			}
			review, err := config.MillhandReviewModel()
			if err != nil {
				return err
			}
			hours, err := config.StaleHours()
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("there is no home directory to keep the tick log in: %w", err)
			}
			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("finding the mw that is running, to arm a reaper with: %w", err)
			}

			windows := seatWindows()
			gateway := mwGateway(dir, host)
			tick := application.MillhandTick{
				Millhand: application.Millhand{
					Seats:    vault.New(dir),
					Windows:  windows,
					Harness:  claude.New(),
					Terminal: windows,
					Armer:    reaper.New(exe),

					Host:         host,
					RoutineModel: domain.Model(routine),
					ReviewModel:  domain.Model(review),
				},
				Sync: application.Sync{Vault: mwVault(dir, host), Tracker: gateway, Host: host},
				Mail: gateway,
				Sweep: application.Sweep{
					Tracker:    gateway,
					Memory:     gateway,
					Runner:     tmux.New(),
					Host:       host,
					StaleAfter: time.Duration(hours) * time.Hour,
				},
				Host:   host,
				DryRun: dryRun,
				Out:    cmd.OutOrStdout(),
			}
			// A rehearsal is not a tick: it leaves nothing in the log.
			if !dryRun {
				tick.Log = ticklog.New(filepath.Join(home, MillhandTickStateDir))
			}
			_, err = tick.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "say what would be done, and start nothing")
	return cmd
}
