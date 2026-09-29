package main

import (
	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

// newPosternMirrorCmd builds `mw postern mirror`: on the home, copy the postern
// backend's data to the boost, and the watchdog's two files to the VPS.
func newPosternMirrorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mirror",
		Short: "Copy the postern backend's data to the boost, and the watchdog's files to the VPS",
		Long: "mirror is what the home does every ten minutes (mw-postern-mirror.timer), so that a dead\n" +
			"home loses little of the postern's data. On the home it copies the backend's data\n" +
			"directory (postern_data, its POSTERN_DATA: the index, the vapid keys, the push\n" +
			"subscriptions and blobs/) to the same path on the boost with rsync over ssh, reached as\n" +
			"the other host's line of [hands_hosts] says (`desktop = \"ssh desktop\"`). When\n" +
			"postern_watchdog_target is set, an rsync destination, it also copies the vapid keys and\n" +
			"push subscriptions there: the VPS watchdog's copy. It also copies the mill's state\n" +
			"directory (grist_state_dir) to the same path on the boost, when it exists on this host.\n\n" +
			"A host that is not home does nothing, says so and leaves with 0. A boost whose index's\n" +
			"last line is newer than this host's is left alone: the run says so and leaves with 1,\n" +
			"once the other copies are done; the mill's state is kept the same way by the time of\n" +
			"the last line of its grinds.jsonl. It never writes to this host's own data.",
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
			data, err := config.PosternData()
			if err != nil {
				return err
			}
			state, err := config.GristStateDir()
			if err != nil {
				return err
			}
			reach, err := config.HandsHosts()
			if err != nil {
				return err
			}
			watchdog, err := config.PosternWatchdogTarget()
			if err != nil {
				return err
			}
			return application.PosternMirror{
				Files:          mwVault(dir, host),
				Mirrorer:       postern.Mirrorer{},
				Out:            cmd.OutOrStdout(),
				Host:           host,
				DataDir:        data,
				MillStateDir:   state,
				Reach:          reach,
				WatchdogTarget: watchdog,
			}.Run(cmd.Context())
		},
	}
}
