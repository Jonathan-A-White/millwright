package main

import "github.com/spf13/cobra"

// version is the factory's version. Release builds override it with
// -ldflags "-X main.version=<version>".
var version = "0.1.0-dev"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of mw",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.Printf("mw %s\n", version)
			return nil
		},
	}
}
