package main

import (
	"fmt"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostload"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/spf13/cobra"
)

// newSuiteCmd builds `mw suite <rig>`: the rig's whole test suite, run on a
// fresh worktree of its origin's main, with the result kept in the vault. It
// spends no tokens.
func newSuiteCmd() *cobra.Command {
	var calmBound time.Duration
	var file bool
	cmd := &cobra.Command{
		Use:   "suite <rig>",
		Short: "Run a rig's whole test suite on its main and keep the result in the vault",
		Long: "suite runs the whole of a rig's tests on the head of its origin's default branch, where a\n" +
			"story's gate runs only what the story touched. It cuts a fresh detached worktree named\n" +
			"suite-<rig> beside the rig's story worktrees (never the rig's own checkout) and removes it\n" +
			"after. What runs is the rig's [tests] command from the config file, then, if the rig has a\n" +
			"[changed_specs] command, that command with {specs} replaced by nothing, so every spec runs.\n" +
			"A rig with no [tests] entry is refused.\n\n" +
			"It first waits for the host to be calm, by the rule a close-out's gate uses (the load under\n" +
			"the core count), for at most --calm-bound (default 2h); a host that never calms is run on\n" +
			"anyway, and the result says so.\n\n" +
			"Each test file that failed is then run once more alone: for a failure of [tests], the\n" +
			"command with the file added to it; for a spec, the [changed_specs] command with just that\n" +
			"spec. A file that fails twice is a failure; one that passes alone is marked flaky.\n\n" +
			"The whole output is written to runs/suite/<rig>/<UTC date>.log in the vault (each run's\n" +
			"output capped as a gate log is), and a summary to runs/suite/<rig>/<UTC date>.json: main's\n" +
			"sha, when it started, how long it took, the load it started at, whether it gave up waiting,\n" +
			"and each failing file with its first failing test, the first lines of its error, and\n" +
			"whether it is flaky. Both are committed in the vault, not pushed; mw sync carries them. A\n" +
			"second run on the same UTC day replaces the first's files.\n\n" +
			"With --file, what it found is then written down in the tracker: each failing file gets one\n" +
			"held [bug] under the rig's suite_epic (labels bug,suite, the epic's path and files=<file>), or,\n" +
			"when an open bead under that epic labelled suite already names the file, a dated comment on it\n" +
			"and no second bug. One mail to the mayor then names each bead and whether it is new. A rig with\n" +
			"no suite_epic in its rules file is refused with --file before anything runs. Without --file\n" +
			"nothing is filed and no mail is sent.\n\n" +
			"Exits 0 when everything passed and 1 when any file failed or the suite could not be run.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			rigs, err := config.Rigs()
			if err != nil {
				return err
			}
			tests, err := config.Tests()
			if err != nil {
				return err
			}
			changedSpecs, err := config.ChangedSpecs()
			if err != nil {
				return err
			}
			nice, err := config.BuilderNice()
			if err != nil {
				return err
			}

			files := mwVault(dir, host)
			var filer *application.SuiteFiler
			if file {
				gateway := mwGateway(dir, host)
				filer = &application.SuiteFiler{Tracker: gateway, Rules: vault.New(dir), Mailbox: gateway, Host: host}
			}
			worktrees := rig.New()
			checks := rig.NewChecks(rig.WithNice(nice))
			report, err := application.Suite{
				Trees:        worktrees,
				Heads:        worktrees,
				Runner:       checks,
				Records:      files,
				Files:        files,
				Filer:        filer,
				Load:         hostload.Proc{},
				LoadBound:    calmBound,
				Host:         host,
				Rigs:         rigs,
				Tests:        tests,
				ChangedSpecs: changedSpecs,
				Out:          cmd.OutOrStdout(),
			}.Run(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if report.Failed {
				return fmt.Errorf("the suite of %s failed on its main (see %s)", args[0], report.ResultPath)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&file, "file", false, "file a held bug per failing file under the rig's suite_epic (or comment on its open one) and mail the mayor")
	cmd.Flags().DurationVar(&calmBound, "calm-bound", application.SuiteCalmBound, "the longest to wait for a busy host to calm before the suite runs anyway")
	return cmd
}
