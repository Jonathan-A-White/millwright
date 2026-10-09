package main

import (
	"fmt"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"

	"github.com/spf13/cobra"
)

// newTesterCmd builds `mw tester`, the Tester trial's commands (mw-it6qk5.5).
func newTesterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tester",
		Short: "The Tester trial: a fresh session drives each landing on a phone-sized screen",
		Long: "While the home's [tester] table names a rig and its until has not passed, mw next files a Tester\n" +
			"story for every landing on that rig whose closing comment has a HOW TO CHECK IT: 'Test: <title>',\n" +
			"labelled tester, worked by the tester formula. Its session follows those steps at 390x844 and tries\n" +
			"to break the app, commits nothing, and writes FINDINGS on the landed story; mw next closes it with\n" +
			"nothing merged and mails the Mayor 'Tested: <id>: N findings'.",
	}
	cmd.AddCommand(newTesterReportCmd())
	return cmd
}

// newTesterReportCmd builds `mw tester report`: the trial summed per rig.
func newTesterReportCmd() *cobra.Command {
	var since string
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Sum the Tester trial per rig: landings, Tester runs, findings, [bug] stories and fuel",
		Long: "report prints, per rig of the [tester] table (or every rig a Tester ran on, once the table is gone):\n" +
			"the landings in the trial, the Tester runs, their findings (bug and taste), the [bug] stories whose\n" +
			"description names a \"" + application.TesterFindingMarker + "\", and each Tester run's fuel from its result.json.\n" +
			"It starts --since a day (2026-10-09) or a UTC time, else a week before [tester] until, else a week ago.\n" +
			"It reads and writes nothing else.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			trial, err := testerTrial()
			if err != nil {
				return err
			}
			var from time.Time
			if since != "" {
				if from, err = time.Parse(time.RFC3339, since); err != nil {
					if from, err = time.Parse("2006-01-02", since); err != nil {
						return fmt.Errorf("--since %q is neither a day (2026-10-09) nor a UTC time (2026-10-09T20:00:00Z)", since)
					}
				}
			}
			_, err = application.TesterReport{
				Tracker: mwGateway(dir, host),
				Files:   mwVault(dir, host),
				Trial:   trial,
				Seat:    BuilderSeat,
				Since:   from,
				Out:     cmd.OutOrStdout(),
			}.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "where the report starts: a day or a UTC time; a week before [tester] until by default")
	return cmd
}

// testerTrial is the [tester] table as the use cases take it, its model and
// effort checked against the ones a path knows.
func testerTrial() (application.TesterTrial, error) {
	settings, err := config.Tester()
	if err != nil {
		return application.TesterTrial{}, err
	}
	if len(settings.Rigs) == 0 {
		return application.TesterTrial{}, nil
	}
	trial := application.TesterTrial{
		Rigs: settings.Rigs, Until: settings.Until,
		Model: domain.Model(settings.Model), Effort: domain.Effort(settings.Effort),
	}
	check := domain.Path{Rig: settings.Rigs[0], Branch: "main", Harness: domain.HarnessClaude, Model: trial.Model, Effort: trial.Effort}
	if err := check.Validate(); err != nil {
		return application.TesterTrial{}, fmt.Errorf("the [%s] table: %w", config.TesterTable, err)
	}
	return trial, nil
}
