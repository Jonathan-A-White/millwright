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
	"github.com/Jonathan-A-White/millwright/infrastructure/notify"
	"github.com/Jonathan-A-White/millwright/infrastructure/reaper"
	"github.com/Jonathan-A-White/millwright/infrastructure/ticklog"
	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
	"github.com/Jonathan-A-White/millwright/infrastructure/watch"

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
			"If a window named millhand-* is already open it says \"already up\" and stops, unless that Millhand\n" +
			"is finished: it has written a handoff newer than its window and its pane is idle at an empty input\n" +
			"line on two looks, the rule mw seat reap --when-idle closes by, the looks tick_recheck_seconds\n" +
			"(30) apart. Then the tick closes the window, adds one line to .millhand-reaper.log in the vault,\n" +
			"says so in its line and goes on as if no Millhand were up. A Millhand idle on the same two looks\n" +
			"with no handoff since its window opened never got going: the tick closes its window, says\n" +
			"\"restarted\" in its line and ends with a wake that tells the fresh Millhand why. A window whose\n" +
			"input line holds text, or whose pane is working, is never closed. Otherwise, with a [watch] table in the config file, it applies mw watch's rule to\n" +
			"the host it watches, before anything else: local-fault (this host's own network is down) is said\n" +
			"in the line, wakes nobody and skips the sync, which would only time out. Then it runs one mw sync (a sync that fails is\n" +
			"said in the line, and the tick looks on this host all the same), and looks for unread mail for\n" +
			"millhand@<host> or millhand, for a story mw sweep newly finds stuck on this host, for a\n" +
			"watched host that is unwell, stale or down, and for a doctor.<host>.<check> note of its own\n" +
			"host that mw doctor has newly written or changed; another host's own note is left alone.\n" +
			"With none of them it says \"quiet\". With any, it starts ONE routine wake\n" +
			"of the Millhand (as `mw millhand --wake routine` does) whose reason lists the mail subjects\n" +
			"and the stuck story titles, five of each and then a count, the watch line verbatim, and every\n" +
			"doctor note with its check's name and a standing instruction to run mw doctor by hand. If the\n" +
			"host is down or its Mayor is gone the reason ends with the charter's one exception. The mail\n" +
			"is left unread; a doctor note is woken for once, until it clears and comes back.\n\n" +
			"It prints one dated line and appends it to ~/.local/state/mw-millhand-tick/log on this host,\n" +
			"which is cut to its last " + fmt.Sprint(application.TickLogLines) + " lines. It leaves with 0 for everything but a fault of its\n" +
			"own, a wake that could not be started included. With --dry-run it says what it would do and\n" +
			"starts nothing; it runs neither the sweep, the watch nor the doctor note look-up, since each\n" +
			"records what it finds and would leave nobody to wake for it, and it writes nothing to the log.\n" +
			"With no [watch] table it does not consult mw watch.",
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
			recheck, err := config.TickRecheckSeconds()
			if err != nil {
				return err
			}
			settings, err := config.Watch()
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
			files := vault.New(dir)
			gateway := mwGateway(dir, host)
			tick := application.MillhandTick{
				Millhand: application.Millhand{
					Seats:    files,
					Windows:  windows,
					Harness:  claude.New(),
					Terminal: windows,
					Armer:    reaper.New(exe),

					Host:         host,
					RoutineModel: domain.Model(routine),
					ReviewModel:  domain.Model(review),
				},
				Sync:        application.Sync{Vault: mwVault(dir, host), Tracker: gateway, Host: host, Ticks: hostTickLogs()},
				Mail:        gateway,
				DoctorNotes: gateway,
				Sweep: application.Sweep{
					Tracker:    gateway,
					Memory:     gateway,
					Runner:     tmux.New(),
					Host:       host,
					StaleAfter: time.Duration(hours) * time.Hour,
				},
				ReapLog:   files,
				Recheck:   time.Duration(recheck) * time.Second,
				SyncHalts: hostSyncHalt(),
				Notify:    notify.New(),
				Host:      host,
				DryRun:    dryRun,
				Out:       cmd.OutOrStdout(),
			}
			watching := application.WatchSettings{
				SSH: settings.SSH, Host: settings.Host, Outside: settings.Outside, Blog: settings.Blog,
			}
			if !watching.Empty() {
				tick.Watch = application.Watch{
					Probes:   watch.New(filepath.Join(home, WatchStateDir)),
					Notes:    gateway,
					Settings: watching,
				}
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
