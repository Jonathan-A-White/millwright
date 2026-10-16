package main

import (
	"fmt"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/spf13/cobra"
)

// newLoopCmd builds `mw loop`: the factory's weekly improvement loop.
func newLoopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "loop",
		Short: "The weekly improvement loop's reads",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newLoopBriefCmd())
	return cmd
}

// newLoopBriefCmd builds `mw loop brief`: the one read for the weekly loop sitting.
func newLoopBriefCmd() *cobra.Command {
	var since string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "brief [--since 7d] [--json]",
		Short: "One read for the weekly loop sitting: the week's numbers, causes, errors, verdicts due, unfiled ideas and the investment share",
		Long: "brief builds ONE mw report result for the window (--since, 7d by default) and prints, in order:\n" +
			"QUALITY, SPEED, FUEL (the summary lines of mw report, each against the days before, then MODELS\n" +
			"when there is one); CAUSES (refusals by cause class per rig, the uncaused count last); ERRORS\n" +
			"(every error class of the register with an occurrence dated in the window, the rigs it was seen\n" +
			"in and its shared-component verdict); VERDICTS (each FACTORY CHANGES entry whose 14th day falls\n" +
			"in the window, then what mw invest due lists); IDEAS (the improvements ledger's ideas with no\n" +
			"bead filed, each with its Expect line); SHARE (the investment line of mw report).\n" +
			"No number is computed here that mw report does not compute. It spends no fuel, starts no agent\n" +
			"and writes nothing. --json prints the six parts as one object: quality_speed_fuel, causes,\n" +
			"errors, verdicts, ideas, share.",
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
			from, around, err := application.ReadReportSince(since, time.Now())
			if err != nil {
				return err
			}
			if around != "" {
				return fmt.Errorf("--since %q names a landing; the brief reads a window: 7d, a day or a UTC time", since)
			}
			share, err := config.FactoryInvestShare()
			if err != nil {
				return err
			}
			files := mwVault(dir, here)
			_, err = application.LoopBriefRun{
				Report: application.FactoryReport{
					Tracker:     mwGateway(dir, here),
					Files:       files,
					Benchmarks:  vault.Benchmarks{Vault: files},
					Seat:        BuilderSeat,
					Since:       from,
					Investments: files,
					InvestShare: share,
				},
				Errors:       files,
				Seat:         BuilderSeat,
				Investments:  files,
				Improvements: files,
				JSON:         asJSON,
				Out:          cmd.OutOrStdout(),
			}.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "where the window starts: 7d, a day (2026-10-09) or a UTC time; 7d by default")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the six parts as one JSON object")
	return cmd
}
