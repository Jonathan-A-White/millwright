package main

import (
	"fmt"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"

	"github.com/spf13/cobra"
)

// newDoctorCmd builds `mw doctor`: a table of checks, each its own probe,
// cure, damper and way back, run on this host and logged here. It calls
// nothing but a check's own probe or cure, and never AI, mail or a push
// notice itself: a check left needing a person is a beads note, one key per
// check, and `mw millhand tick` is what wakes the Millhand for it.
func newDoctorCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "doctor [check]",
		Short: "Cure this host's known faults offline, on a table of checks",
		Long: "doctor works a table of checks, each with a probe (never changes anything), a cure (run\n" +
			"only when the probe says faulty and the damper allows it), a damper (a minimum wait\n" +
			"between cures and a cap on how many one fault episode may spend before it gives up and\n" +
			"waits for a person), and a way back. With no check named it works the whole table; named,\n" +
			"only that one. Every run appends one dated line per check to the doctor's log.\n\n" +
			"--dry-run prints, for each faulty check, the reason and the way back, and changes\n" +
			"nothing: no cure runs, no state is written, no log line is appended.\n\n" +
			"It leaves with 0 when every check is ok or cured, and " + fmt.Sprint(application.DoctorFaultExit) +
			" when any check is left faulty and uncured (damped, or its cure failed), so a timer's\n" +
			"journal shows it.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}

			units, err := config.DoctorUnits()
			if err != nil {
				return err
			}
			dir, err := config.DoctorStateDir()
			if err != nil {
				return err
			}
			reach, err := config.DoctorReach()
			if err != nil {
				return err
			}
			powershell, err := config.DoctorPowershell()
			if err != nil {
				return err
			}
			tunnelUnit, err := config.DoctorTunnelUnit()
			if err != nil {
				return err
			}
			tunnelHost, err := config.DoctorTunnelHost()
			if err != nil {
				return err
			}
			tunnelProbe, err := config.DoctorTunnelProbe()
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

			store := doctor.New(dir)
			return runDoctor(cmd, application.Doctor{
				Checks: application.DoctorChecks{
					doctor.NewDaemonReload(units),
					doctor.NewWifi(reach, powershell, store),
					doctor.NewTunnel(tunnelHost, reach, tunnelUnit, tunnelProbe),
					doctor.NewVaultDirty(vault, host),
					doctor.NewTimers(units),
				},
				State: store,
				Log:   store,
				Notes: mwGateway(vault, host),
				Out:   cmd.OutOrStdout(),
			}, name, dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what each faulty check would do; change nothing")
	return cmd
}

// runDoctor runs the doctor. A check left faulty and uncured is an outcome
// and not a failure: the report naming it has already been printed, so cobra
// is not to print the error too, and only the status it leaves with says so.
func runDoctor(cmd *cobra.Command, doc application.Doctor, name string, dryRun bool) error {
	_, err := doc.Run(cmd.Context(), name, dryRun)
	if _, fault := application.DoctorFaults(err); fault {
		cmd.SilenceErrors = true
	}
	return err
}
