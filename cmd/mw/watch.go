package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/watch"

	"github.com/spf13/cobra"
)

// WatchStateDir is where mw watch keeps its memory and its log, under the home
// directory. It is this host's own: nothing in it is synced anywhere.
var WatchStateDir = filepath.Join(".local", "state", "mw-watch")

// newWatchCmd builds `mw watch`: is a fault this host's own, or the watched
// host's? It prints one line and leaves with a status a timer can read.
func newWatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "watch",
		Short: "Say whether the watched host is well, or whether this host's own network is down",
		Long: "watch is run on a host that can lose its own network, on a timer, and tells a fault of\n" +
			"that host's own from a fault of the host it watches. It prints ONE line and appends it,\n" +
			"dated, to a log on this host (~/.local/state/mw-watch/log).\n\n" +
			"If neither of two outside places (config [watch] outside) answers, it says local-fault: this\n" +
			"host's network is down and nothing is known of the other. Otherwise it reads the health line\n" +
			"(~/.mw-health) over ssh, with `cat ~/.mw-health`, BatchMode and a 10 second timeout, and says\n" +
			"ok, unwell <reasons>, or stale when the line is older than 40 minutes. If ssh fails it looks\n" +
			"for signs of life, the blog over HTTPS and the host's last sync note fresher than 30 minutes,\n" +
			"and says unreachable-once signs=<blog,sync,none>; only a second failed check at least 3\n" +
			"minutes after the first says down signs=<...>.\n\n" +
			"It leaves with 0 when nobody needs waking and " + fmt.Sprint(application.WatchWakeExit) + " when a wake is called for\n" +
			"(unwell, stale, down). It only reads. With no [watch] table in the config file it says\n" +
			"nothing to watch and leaves with 0.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			settings, err := config.Watch()
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("there is no home directory to keep the watch's memory and log in: %w", err)
			}

			watching := application.Watch{
				Probes: watch.New(filepath.Join(home, WatchStateDir)),
				Settings: application.WatchSettings{
					SSH: settings.SSH, Host: settings.Host, Outside: settings.Outside, Blog: settings.Blog,
				},
				Out: cmd.OutOrStdout(),
			}
			if !watching.Settings.Empty() {
				dir, err := config.Vault()
				if err != nil {
					return err
				}
				host, err := config.Host()
				if err != nil {
					return err
				}
				watching.Notes = mwGateway(dir, host)
			}
			return runWatch(cmd, watching)
		},
	}
}

// runWatch runs the watch. A wake called for is an outcome and not a failure:
// the line naming it has been printed, so cobra is not to print the error too,
// and only the status it leaves with says so.
func runWatch(cmd *cobra.Command, watching application.Watch) error {
	_, err := watching.Run(cmd.Context())
	if _, wake := application.WatchWakes(err); wake {
		cmd.SilenceErrors = true
	}
	return err
}
