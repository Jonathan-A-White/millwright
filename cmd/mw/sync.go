package main

import (
	"fmt"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"

	"github.com/spf13/cobra"
)

// newSyncCmd builds `mw sync`: the one command that brings this host level with
// the other one. It is safe to run by hand, from the dispatcher, or on a timer,
// and it says in one line what it did.
func newSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Bring this host level with the other one: the vault, then beads",
		Long: "sync pulls the other host's vault commits and pushes this host's, then runs one beads\n" +
			"synchronisation cycle, then records when this host was last level. It never migrates and\n" +
			"never forces: what it cannot settle stops it, with the reason in plain words, and nothing\n" +
			"is retried. A sync stopped by beads exits with beads' own exit code, so that a timer can\n" +
			"branch on it: 2 is a merge conflict and 4 a stuck working set, and both wait for a person.\n" +
			"Work nobody committed in the vault is not a failure: the vault half is skipped, beads are\n" +
			"synced anyway, and sync exits 5 with one line naming the files. mw commits nobody's edits.",
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

			report, err := application.Sync{
				Vault:   mwVault(dir, host),
				Tracker: mwGateway(dir, host),
				Host:    host,
			}.Run(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), report)
			return nil
		},
	}
}

// exitCode is the status mw leaves with after a command reported err. A sync
// that beads stopped leaves with beads' own code, so that whoever ran mw reads
// the same number bd would have given them; a sync only somebody's uncommitted
// vault work stood in the way of leaves with a status of its own; anything else
// is a plain 1.
func exitCode(err error) int { return application.ExitStatus(err) }
