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

	root.AddCommand(newDispatchCmd())
	root.AddCommand(newFileCmd())
	root.AddCommand(newNextCmd())
	root.AddCommand(newSyncCmd())
	root.AddCommand(newVersionCmd())

	return root
}
