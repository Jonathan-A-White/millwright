package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/ticklog"

	"github.com/spf13/cobra"
)

// newTidyCmd builds `mw tidy`: close what is plainly finished, within the fixed
// bounds of docs/tidy.md. The Millhand's tick runs the same rule after its
// sweep.
func newTidyCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "tidy",
		Short: "Close what is plainly finished: old Answer and read mail, question notes of closed beads",
		Long: "tidy does three things and nothing else (docs/tidy.md is the table):\n" +
			"  1. closes an \"Answer: ...\" mail bead over 1 day old;\n" +
			"  2. closes a mail bead the mailbox holds read, over 7 days old;\n" +
			"  3. clears a postern question note whose bead is closed.\n\n" +
			"Each act writes one line on what it touched, \"Tidied by mw tidy: <why>\", and one to the\n" +
			"Millhand's tick log. It never closes a story, an epic, a map or a hitl bead, and never\n" +
			"deletes. --dry-run lists what it would do and changes nothing.",
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
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("there is no home directory to keep the tick log in: %w", err)
			}

			gateway := mwGateway(dir, host)
			tidy := application.Tidy{
				Mail:   gateway,
				Notes:  gateway,
				Beads:  gateway,
				DryRun: dryRun,
				Out:    cmd.OutOrStdout(),
			}
			// A rehearsal is not a run: it leaves nothing in the log.
			if !dryRun {
				tidy.Log = ticklog.New(filepath.Join(home, MillhandTickStateDir))
			}
			_, err = tidy.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list what would be tidied, and change nothing")
	return cmd
}
