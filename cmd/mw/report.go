package main

import (
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/spf13/cobra"
)

// newReportCmd builds `mw report`: the factory's quality over a window.
func newReportCmd() *cobra.Command {
	var since, rig, host string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "report [--since 7d|<day>] [--rig <rig>] [--host <host>] [--json]",
		Short: "How the factory is doing: landed, refused and why, attempts, causes, bugs and Tester findings",
		Long: "report sums the factory's quality over a window, from the builder ledger, the stories and\n" +
			"the Tester's FINDINGS: per rig, then per model, then per host, what landed, what was refused\n" +
			"and under which reason, the refusal rate, the attempts a landing took, the refusals' causes\n" +
			"(mw cause), the [bug] stories filed and the Tester's findings, the last two per landing.\n" +
			"The window is --since (7d by default; a number of days, a day such as 2026-10-09, or a UTC\n" +
			"time) to now; the window of the same length before it is computed too and is in --json.\n" +
			"--rig and --host narrow every block. A ledger row whose story is gone counts under rig\n" +
			"unknown. It reads and writes nothing, and prints in 60 columns.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			here, err := config.Host()
			if err != nil {
				return err
			}
			from, err := application.ParseReportSince(since, time.Now())
			if err != nil {
				return err
			}
			files := mwVault(dir, here)
			_, err = application.FactoryReport{
				Tracker:    mwGateway(dir, here),
				Files:      files,
				Benchmarks: vault.Benchmarks{Vault: files},
				Seat:       BuilderSeat,
				Since:      from,
				Rig:        rig,
				Host:       host,
				JSON:       asJSON,
				Out:        cmd.OutOrStdout(),
			}.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "where the window starts: 7d, a day (2026-10-09) or a UTC time; 7d by default")
	cmd.Flags().StringVar(&rig, "rig", "", "narrow every block to this rig")
	cmd.Flags().StringVar(&host, "host", "", "narrow every block to this host")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the whole structure as JSON")
	return cmd
}
