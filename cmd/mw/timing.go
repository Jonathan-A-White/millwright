package main

import (
	"fmt"

	"github.com/Jonathan-A-White/millwright/infrastructure/config"

	"github.com/spf13/cobra"
)

// newTimingCmd builds `mw timing <rig>`: the rig's timing tests, run now on this
// host as the home's nightly timing run runs them (docs/timing-tests.md). The
// home runs it over ssh on a host with room while it is busy (mw-gq6.1617).
func newTimingCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "timing <rig>",
		Short: "Run a rig's timing tests on its main now and keep the result in the vault",
		Long: "timing runs the timing tests of a rig (its timing_tests, in its rules file in the vault) now,\n" +
			"whether or not the nightly timing run would find them due: the rig's [tests] command with\n" +
			"MW_TIMING=only, in a throwaway worktree of origin's main, after waiting for this host to calm\n" +
			"and under this host's gate lock. The result is kept under runs/timing/<rig>/ in the vault and\n" +
			"each failing file is filed as one held [bug] under the rig's suite_epic, as the nightly run\n" +
			"does. It is what the home's follower runs over ssh on another host with room while the home\n" +
			"is busy; a host that never calms skips the run, says so and exits 0.\n\n" +
			"Exits 0 when the tests ran or the run was skipped, and 1 when they could not be run.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			host, err := config.Host()
			if err != nil {
				return err
			}
			timing, err := newNightlyTiming(host, 0, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			skipped, err := timing.RunNow(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("the timing run of %s: %w", args[0], err)
			}
			if skipped != "" {
				fmt.Fprintln(cmd.OutOrStdout(), skipped)
			}
			return nil
		},
	}
}
