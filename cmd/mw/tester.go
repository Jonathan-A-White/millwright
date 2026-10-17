package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

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
	cmd.AddCommand(newTesterReportCmd(), newTesterFileCmd())
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
			"description names a \"" + application.TesterFindingMarker + "\" or opens \"Found by the Tester\" / \"from the Tester's finding\", and each Tester run's fuel from its result.json.\n" +
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

// newTesterFileCmd builds `mw tester file`: a Tester files one of its findings
// as a story held under the landed story's epic (mw-gq6.397).
func newTesterFileCmd() *cobra.Command {
	var finding application.TesterFinding
	cmd := &cobra.Command{
		Use:   "file <tester-story-id> --kind bug|taste --title <one line> --finding <text> --how <text> --accept <text> [--files a,b] [--host <host>] [--shot <path>] [--fix <one line>]",
		Short: "File a Tester's finding as a held story under the landed story's epic",
		Long: "file is what a Tester runs for each finding that still happens on main, in place of the Mayor's triage.\n" +
			"It reads the landed story from the Tester story's 'Landed story:' line and files, held, under the landed\n" +
			"story's epic: '[bug] <app>: <title>' (or [taste]), the finding verbatim with its source, shot and the fix\n" +
			"marked PROVISIONAL, HOW TO CHECK IT, the acceptance, the landed story's path with host auto (or --host)\n" +
			"and --files as its files key, priority 2 for a bug and 3 for a taste finding, a wait on every running\n" +
			"or open story of the rig, and on every earlier held finding of the same landing, that names the same\n" +
			"file, and the epic's open demo waiting on it. A finding whose title an open bead of the rig already has is not filed: the bead\n" +
			"is named. A bug whose --accept has no line beginning 'Sweep:' is refused, as mw dispatch would refuse it.\n" +
			"It prints the id; put 'held: <id>' (or 'duplicate of: <id>') under the finding in FINDINGS.\n" +
			"It releases nothing: that stays the Mayor's. A text flag whose value starts with @ reads that file.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, text := range []*string{&finding.Finding, &finding.Fix, &finding.HowToCheck, &finding.Acceptance} {
				if path, isFile := strings.CutPrefix(*text, "@"); isFile {
					read, err := os.ReadFile(path)
					if err != nil {
						return fmt.Errorf("reading %s: %w", path, err)
					}
					*text = string(read)
				}
			}
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			_, err = application.TesterFiler{
				Tracker: mwGateway(dir, host),
				Rules:   vault.New(dir),
				Out:     cmd.OutOrStdout(),
			}.File(cmd.Context(), args[0], finding)
			return err
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&finding.Kind, "kind", "", "bug (it breaks, or does not do what HOW TO CHECK IT says) or taste")
	flags.StringVar(&finding.App, "app", "", "the app named in the title; the landed story's rig by default")
	flags.StringVar(&finding.Title, "title", "", "the finding in one line")
	flags.StringVar(&finding.Finding, "finding", "", "the finding verbatim, as in FINDINGS (@file reads a file)")
	flags.StringVar(&finding.Shot, "shot", "", "the shot's path")
	flags.StringVar(&finding.Fix, "fix", "", "a one-line suggested fix, filed as PROVISIONAL (@file reads a file)")
	flags.StringVar(&finding.HowToCheck, "how", "", "HOW TO CHECK IT, for the Governor (@file reads a file)")
	flags.StringVar(&finding.Acceptance, "accept", "", "acceptance checkable by running something; a bug's needs a line beginning 'Sweep:' too (@file reads a file)")
	flags.StringVar(&finding.Files, "files", "", "the files key: the repo paths or globs the fix expects to change, comma-separated")
	flags.StringVar(&finding.Host, "host", "", "the host the fix must run on; auto (any host) by default, not the landed story's host")
	return cmd
}
