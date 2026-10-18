package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	millwright "github.com/Jonathan-A-White/millwright"
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/gitcmd"
)

// newSleepCmd builds `mw sleep`: the sleep pass, the factory's nightly sweep.
func newSleepCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sleep",
		Short: "The sleep pass: priorities, maps and improvements written by one fresh agent",
	}
	cmd.AddCommand(newSleepPassCmd(), newSleepLedgerCmd(), newSleepOutcomeCmd())
	return cmd
}

func newSleepPassCmd() *cobra.Command {
	var dryRun bool
	var model, effort string
	cmd := &cobra.Command{
		Use:   "pass [--dry-run] [--model <m>] [--effort <e>]",
		Short: "Gather the night's bundle and have one fresh agent write the day's priorities, maps and improvements",
		Long: "pass gathers the night's bundle: the triage dump and what moved since the last one, the ready\n" +
			"stories in dispatch's order (mw queue), each rig's git log of the last 10 days, the sessions'\n" +
			"results aggregated by model, and mw report (7 days; on Sundays 30). It starts one fresh agent\n" +
			"with the principles (plans/principles.md, else the newest plans/triage-<date>/principles.md)\n" +
			"and that bundle. The agent answers with the day's priorities (recommendations.json and\n" +
			"questions.md), maps (maps.md) and improvements (improvements.md), which mw writes with a\n" +
			"summary.md and the dump (triage.jsonl, tomorrow's diff starts from it) into\n" +
			"plans/sleep-<date>/ in the vault and commits, with the run log runs/sleep/<date>.json that\n" +
			"mw fuel counts under the kind sleep. It mails the Mayor the summary, under 150 words. It files\n" +
			"no bead.\n\n" +
			"The model and effort are [sleep] nightly_model and nightly_effort (opus and medium), and on\n" +
			"Sundays, UTC, sunday_model and sunday_effort (fable and xhigh); --model and --effort replace\n" +
			"the day's. The follower makes the pass once a day from [events] sleep_hour (07:30 UTC).\n\n" +
			"--dry-run gathers the bundle and prints the model, the system prompt, the prompt and the\n" +
			"bundle's sizes; it starts no agent, writes no file and mails nothing.",
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
			pass, err := newSleepPass(dir, host, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			_, err = pass.Run(cmd.Context(), application.SleepOptions{DryRun: dryRun, Model: model, Effort: effort})
			return err
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the prompt and the bundle sizes; start, write and mail nothing")
	cmd.Flags().StringVar(&model, "model", "", "the agent's model, replacing the day's")
	cmd.Flags().StringVar(&effort, "effort", "", "the agent's effort, replacing the day's")
	return cmd
}

func newSleepLedgerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ledger",
		Short: "Print the improvements ledger, a line an improvement with its verdict",
		Long: "ledger prints plans/improvements/ledger.jsonl in the vault: one line for each improvement the\n" +
			"Mayor approved, with when it was filed and landed, its measure, its result and its verdict\n" +
			"(KEEP or ROLLBACK, PENDING until it is measured) and its lesson. It changes nothing.",
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
			return application.SleepLedger{Vault: mwVault(dir, host), Out: cmd.OutOrStdout()}.Run(cmd.Context())
		},
	}
}

func newSleepOutcomeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "outcome <date>",
		Short: "Score the priority calls of the sleep pass of a date against where the beads are now",
		Long: "outcome reads plans/sleep-<date>/recommendations.json and says, of the pass's priority calls,\n" +
			"how many held: the bead landed, or is open and still where the pass put it. A call is not held\n" +
			"when the pass iced a bead and it was thawed, or set a priority and another was set (overridden).\n" +
			"It also counts the beads the Governor verified. It is meant for a pass 14 days old; a younger\n" +
			"one is scored all the same and the report says what day it is on. The score is kept as\n" +
			"plans/sleep-<date>/outcome.json and committed in the vault. It changes no bead.",
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
			_, err = application.SleepOutcome{
				Vault: mwVault(dir, host), Beads: mwGateway(dir, host), Out: cmd.OutOrStdout(),
			}.Run(cmd.Context(), args[0])
			return err
		},
	}
}

// newSleepPass is the sleep pass as the configured vault and rigs run it.
func newSleepPass(dir, host string, out io.Writer) (application.SleepPass, error) {
	settings, err := config.Sleep()
	if err != nil {
		return application.SleepPass{}, err
	}
	rigs, err := config.Rigs()
	if err != nil {
		rigs = map[string]string{} // no rig is checked out: the bundle says so
	}
	gateway := mwGateway(dir, host)
	files := mwVault(dir, host)
	return application.SleepPass{
		Gatherer: application.SleepGather{
			Dump: func(ctx context.Context, w io.Writer) error {
				_, err := application.TriageDump{Source: gateway}.Run(ctx, w)
				return err
			},
			Mw:      runMwItself,
			Git:     rigGitLog,
			Rigs:    rigs,
			Runs:    files,
			Earlier: files,
		},
		Vault: files, Grinder: claude.NewGrinder(), Mailbox: gateway,
		Settings: application.SleepSettings{
			NightlyModel: settings.NightlyModel, NightlyEffort: settings.NightlyEffort,
			SundayModel: settings.SundayModel, SundayEffort: settings.SundayEffort,
		},
		Instructions: millwright.SleepInstructions, Host: host, Out: out,
	}, nil
}

// runMwItself runs this binary with args and reports what it printed.
func runMwItself(ctx context.Context, args ...string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	// A follower that outlives a self-update runs a binary that was replaced:
	// /proc names it with this suffix, and the file at the path is the new one.
	exe = strings.TrimSuffix(exe, " (deleted)")
	var out, errs bytes.Buffer
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Stdout, cmd.Stderr = &out, &errs
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("mw %s: %w: %s", args[0], err, bytes.TrimSpace(errs.Bytes()))
	}
	return out.String(), nil
}

// sleepGitLines is the most commits of one rig the bundle holds.
const sleepGitLines = 300

// rigGitLog is the log of the rig checked out at dir over the last days days,
// a line a commit, merges left out.
func rigGitLog(ctx context.Context, dir string, days int) (string, error) {
	var out, errs bytes.Buffer
	cmd := gitcmd.Command(ctx, "git", dir, "log", "--no-merges", "--since="+strconv.Itoa(days)+".days",
		"-n", strconv.Itoa(sleepGitLines), "--date=short", "--format=%h %ad %s")
	cmd.Stdout, cmd.Stderr = &out, &errs
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git log in %s: %w: %s", dir, err, bytes.TrimSpace(errs.Bytes()))
	}
	return out.String(), nil
}

// sleepSpringJob is the follower's sleep job: the pass once a day from the time
// [events] sleep_hour names, on the home only.
func sleepSpringJob(host string, hour, minute int, out io.Writer) (application.SpringJob, error) {
	dir, err := config.Vault()
	if err != nil {
		return application.SpringJob{}, err
	}
	pass, err := newSleepPass(dir, host, out)
	if err != nil {
		return application.SpringJob{}, err
	}
	nightly := &application.SleepNightly{
		Hour: hour, Minute: minute, Notes: mwGateway(dir, host),
		Run: func(ctx context.Context) error {
			_, err := pass.Run(ctx, application.SleepOptions{})
			return err
		},
		Home: func(ctx context.Context) (bool, error) {
			return application.IsHome(ctx, mwVault(dir, host), host)
		},
		Now: time.Now,
	}
	return nightly.Job(), nil
}

// newFuelCmd builds `mw fuel`: the model passes' fuel, each kind of pass apart.
func newFuelCmd() *cobra.Command {
	var days int
	cmd := &cobra.Command{
		Use:   "fuel [--days <n>]",
		Short: "The fuel the factory's model passes burned, by kind of pass",
		Long: "fuel adds up the tokens of the passes that keep a run log in the vault's runs/<kind>/<date>.json\n" +
			"(the sleep pass and the To do ranking), each kind on its own line with how many passes ran and\n" +
			"on which model and effort, over the last --days days (default 7). A story's session keeps its\n" +
			"fuel in its seat's ledger, which mw status and mw report count.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if days < 1 {
				return fmt.Errorf("--days must be 1 or more")
			}
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			return application.FuelByKind{Runs: mwVault(dir, host), Days: days, Out: cmd.OutOrStdout()}.Run(cmd.Context())
		},
	}
	cmd.Flags().IntVar(&days, "days", 7, "how many days back to count")
	return cmd
}
