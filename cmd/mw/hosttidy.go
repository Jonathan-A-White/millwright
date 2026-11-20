package main

import (
	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/gocache"
)

// newHostTidyCmd builds `mw host tidy`: delete what a host's tools only ever
// add to and never take away.
func newHostTidyCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "tidy",
		Short: "Delete what a host's tools only ever add to",
		Args:  cobra.NoArgs,
	}
	root.AddCommand(newHostTidyGoCacheCmd())
	return root
}

// newHostTidyGoCacheCmd builds `mw host tidy go-cache`.
func newHostTidyGoCacheCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "go-cache [--dry-run]",
		Short: "Delete the files of Go's build cache (GOCACHE) not used for over a day",
		Long: "Go keeps an entry of its build cache for 5 days after its last use, and a factory host's Builders\n" +
			"write about 16 GB of it a day, so it settles near 110 GB (mw-gq6.1537). go-cache deletes every\n" +
			"file of the cache (the action and data files Go makes) whose last use, its modification time, is\n" +
			"over a day old. Go rebuilds what it needs again. It touches nothing else under the cache, and\n" +
			"nothing outside it. --dry-run says what it would delete and deletes nothing.\n\n" +
			"mw-host-tidy.timer runs it daily; the go-cache-size check of mw doctor names a host whose cache\n" +
			"is over 30 GB.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := gocache.Dir()
			if err != nil {
				return err
			}
			_, err = application.GoCacheTidy{
				Dir:    dir,
				Disk:   gocache.Disk{},
				DryRun: dryRun,
				Out:    cmd.OutOrStdout(),
			}.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "say what would be deleted and delete nothing")
	return cmd
}
