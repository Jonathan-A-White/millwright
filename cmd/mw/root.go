package main

import "github.com/spf13/cobra"

// newRootCmd builds the mw command tree.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "mw",
		Short:         "millwright: a personal software factory",
		Long:          "mw drives the millwright factory: it reads stories, dispatches the sessions that work them, and reports back.",
		SilenceUsage:  true,
		SilenceErrors: false,
	}

	root.AddCommand(newBriefCmd())
	root.AddCommand(newCheckCmd())
	root.AddCommand(newDispatchCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newFileCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newMailCmd())
	root.AddCommand(newMillhandCmd())
	root.AddCommand(newNextCmd())
	root.AddCommand(newNudgeCmd())
	root.AddCommand(newReleaseCmd())
	root.AddCommand(newRetryCmd())
	root.AddCommand(newSeatCmd())
	root.AddCommand(newShowCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newSweepCmd())
	root.AddCommand(newSyncCmd())
	root.AddCommand(newVersionCmd())
	root.AddCommand(newWatchCmd())

	return root
}
