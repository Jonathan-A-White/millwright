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
	cmd.AddCommand(newLoopBriefCmd(), newLoopRankCmd())
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
			run, err := newLoopBriefRun(dir, here, from)
			if err != nil {
				return err
			}
			run.JSON, run.Out = asJSON, cmd.OutOrStdout()
			_, err = run.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "where the window starts: 7d, a day (2026-10-09) or a UTC time; 7d by default")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the six parts as one JSON object")
	return cmd
}

// newLoopRankCmd builds `mw loop rank`: the unfiled ideas in the order the weekly sitting files them.
func newLoopRankCmd() *cobra.Command {
	var top int
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "rank [--top N] [--json]",
		Short: "The improvements ledger's unfiled ideas, ordered by his criteria",
		Long: "rank prints the ideas of plans/improvements/ledger.jsonl that have no bead filed, a line each: its\n" +
			"rank, title, the four axes as arrows (quality, his_time, fuel, speed; ↑ up, → level, ↓ down), C\n" +
			"when it compounds, its size and its Expect line. The order is quality up first, then his time,\n" +
			"fuel and speed (each down last), then a compounding change before one that does not, then the\n" +
			"smallest size, then the ledger's own order. An idea missing a field, or whose Expect line\n" +
			"mw report cannot read, is listed last as unscored with the field named; none is dropped.\n" +
			"--top N prints the first N; --json prints them as a list. It changes nothing.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if top < 0 {
				return fmt.Errorf("--top %d: give a number of ideas, 1 or more", top)
			}
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			here, err := config.Host()
			if err != nil {
				return err
			}
			return application.LoopRank{Vault: mwVault(dir, here), Top: top, JSON: asJSON, Out: cmd.OutOrStdout()}.Run(cmd.Context())
		},
	}
	cmd.Flags().IntVar(&top, "top", 0, "print only the first N ideas")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the ideas as a JSON list")
	return cmd
}

// newLoopBriefRun is the loop's reads as mw loop brief and the weekly digest
// make them: one report from since (zero is the default window), the register,
// the investments and the improvements ledger, all in the vault at dir.
func newLoopBriefRun(dir, here string, since time.Time) (application.LoopBriefRun, error) {
	share, shareMin, shareMax, err := investShareSettings()
	if err != nil {
		return application.LoopBriefRun{}, err
	}
	files := mwVault(dir, here)
	return application.LoopBriefRun{
		Report: application.FactoryReport{
			Tracker:     mwGateway(dir, here),
			Files:       files,
			Benchmarks:  vault.Benchmarks{Vault: files},
			Rules:       files,
			Seat:        BuilderSeat,
			Since:       since,
			Investments: files,
			InvestShare: share, InvestShareMin: shareMin, InvestShareMax: shareMax,
		},
		Errors:       files,
		Seat:         BuilderSeat,
		Investments:  files,
		Improvements: files,
	}, nil
}
