package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
	"github.com/Jonathan-A-White/millwright/infrastructure/gocache"
	"github.com/Jonathan-A-White/millwright/infrastructure/tmpdisk"
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
	root.AddCommand(newHostTidyTmpCmd())
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
			if err == nil && !dryRun {
				recordUpkeepRun(doctor.HostUpkeepGoTidyMark)
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "say what would be deleted and delete nothing")
	return cmd
}

// newHostTidyTmpCmd builds `mw host tidy tmp`.
func newHostTidyTmpCmd() *cobra.Command {
	var dryRun bool
	var dir string
	cmd := &cobra.Command{
		Use:   "tmp [--dir <dir>] [--dry-run]",
		Short: "Delete what tools left in /tmp and have not touched for over a day",
		Long: "Node's compile cache (node-compile-cache) and Playwright's transform cache\n" +
			"(playwright-transform-cache-*) only grow, and a Go test killed before it could clean up leaves its\n" +
			"directory (Test*): the Laptop's /tmp reached 254 GB (mw-d40q096). tmp deletes each file of the two\n" +
			"caches, and each Test* directory with everything in it, that the user running it owns and nothing\n" +
			"in it has been touched for over a day. It reads only the top of the directory and those three\n" +
			"kinds, and follows no link. --dir is the directory (default the system temp, $TMPDIR else /tmp).\n" +
			"--dry-run says what it would delete and deletes nothing.\n\n" +
			"mw-host-tidy.timer runs it daily beside go-cache; the tmp-size check of mw doctor names a host\n" +
			"whose /tmp is over 20 GB.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if dir == "" {
				dir = os.TempDir()
			}
			_, err := application.TmpTidy{
				Dir:    dir,
				Disk:   tmpdisk.Disk{},
				DryRun: dryRun,
				Out:    cmd.OutOrStdout(),
			}.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "the directory to tidy (default the system temp)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "say what would be deleted and delete nothing")
	return cmd
}
