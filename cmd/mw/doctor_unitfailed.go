package main

import (
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"

	"github.com/spf13/cobra"
)

// unitFailedDir is the directory in the doctor's state directory that keeps a
// timer unit's first failure.
const unitFailedDir = "unit-failed"

// newDoctorUnitFailedCmd builds `mw doctor unit-failed <unit>`, the command
// contrib/systemd/mw-unit-failed@.service runs when a unit ends in failure.
func newDoctorUnitFailedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unit-failed <unit>",
		Short: "Tell the Mayor a unit failed, if it is time to",
		Long: "unit-failed is what systemd's OnFailure= starts when a unit ends in failure. A unit a timer\n" +
			"runs (mw-dispatch.service, by mw-dispatch.timer) often fails once and is clean on its next\n" +
			"run, so its first failure is only recorded, with the last lines of its journal, in the\n" +
			"doctor's state directory (unit-failed/<unit>). A second failure with no clean run of the\n" +
			"unit between them mails the Mayor from doctor@<host>, naming both and carrying both\n" +
			"journals' last lines; a clean run between them starts the count again. A unit no timer\n" +
			"runs is mailed at its first failure. It reads the journal and writes one file and at most\n" +
			"one mail.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			stateDir, err := config.DoctorStateDir()
			if err != nil {
				return err
			}
			vault, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			_, err = application.UnitFailed{
				Units:    doctor.UserUnits{},
				Failures: doctor.UnitFailureFiles{Dir: filepath.Join(stateDir, unitFailedDir)},
				Mailbox:  mwGateway(vault, host),
				Host:     host,
				Out:      cmd.OutOrStdout(),
			}.Run(cmd.Context(), args[0])
			return err
		},
	}
}
