package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	millwright "github.com/Jonathan-A-White/millwright"
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/gitcmd"
	"github.com/Jonathan-A-White/millwright/infrastructure/ideasonly"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
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
			"results aggregated by model, mw report (7 days; on Sundays 30) and mw stuck (the held roots live\n" +
			"beads wait behind, which the summary names) and needs-you.txt: every item of his review queue\n" +
			"(the cards, demos and hands steps mw stuck --dampers counts), each with its age, question or\n" +
			"step, its epic's and dependencies' states, a demo's version against the rig's, its last two\n" +
			"comments, and, first, the stale candidates mw found evidence for (a card on a parked grilling,\n" +
			"a closed replacement, a handled Ideas only item). It starts one fresh agent\n" +
			"with the principles (plans/principles.md, else the newest plans/triage-<date>/principles.md)\n" +
			"and that bundle. The agent answers with the day's priorities (recommendations.json and\n" +
			"questions.md), maps (maps.md) and improvements (improvements.md), which mw writes with a\n" +
			"summary.md and the dump (triage.jsonl, tomorrow's diff starts from it) into\n" +
			"plans/sleep-<date>/ in the vault and commits, with the run log runs/sleep/<date>.json that\n" +
			"mw fuel counts under the kind sleep. It also appends a line to plans/improvements/ledger.jsonl for\n" +
			"each idea in the recommendations, so mw loop rank sees them. An answer may add stale.md: a line for each item of his Needs\n" +
			"you judged stale, with its evidence and an action (clear-card, retire-step, close-superseded,\n" +
			"tick-item or ask-him); the Mayor clears them at the next sitting. It mails the Mayor the summary,\n" +
			"under 150 words, with how many are stale. It files no bead and clears nothing.\n\n" +
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
			"(KEEP or ROLLBACK, PENDING until it is measured) and its lesson. The sleep pass appends the lines:\n" +
			"one for each idea in its recommendations.json, filed that day with no story, never twice for the\n" +
			"same title and day. This command itself changes nothing.",
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
	// His messages and pictures are read from the dir the inbox pass saves them
	// in; without a home for it the evidence part says it could not be read.
	inbox, _ := config.PosternInboxDir()
	verify, err := config.Verify()
	if err != nil {
		return application.SleepPass{}, err
	}
	return application.SleepPass{
		Gatherer: application.SleepGather{
			Dump: func(ctx context.Context, w io.Writer) error {
				_, err := application.TriageDump{Source: gateway}.Run(ctx, w)
				return err
			},
			Mw:       runMwItself,
			Git:      rigGitLog,
			Rigs:     rigs,
			Runs:     files,
			Earlier:  files,
			NeedsYou: needsYouSurvey(dir, gateway, files, rigs).Read,
			Beads:    gateway, InboxDir: inbox,
		},
		Vault: files, Ledger: files, Grinder: claude.NewGrinder(), Mailbox: gateway,
		Beads:  gateway,
		Verify: application.VerifySettings{Model: verify.Model, Effort: verify.Effort, Confidence: verify.Confidence},
		Settings: application.SleepSettings{
			NightlyModel: settings.NightlyModel, NightlyEffort: settings.NightlyEffort,
			SundayModel: settings.SundayModel, SundayEffort: settings.SundayEffort,
		},
		Instructions: millwright.SleepInstructions, Host: host, Out: out,
	}, nil
}

// needsYouSurvey is where the pass reads his review queue from: the tracker the
// DAMPERS alarms count it in, the cards sent from this host, the ideas of Ideas
// only given back, the Mayor's last handoffs and the rigs' versions. A source
// that is not there costs only what it would have said.
func needsYouSurvey(dir string, gateway *beads.Gateway, files *vault.Vault, rigs map[string]string) application.NeedsYouSurvey {
	survey := application.NeedsYouSurvey{
		Tracker: gateway, Graph: gateway, Notes: gateway, Comments: gateway,
		Versions: func(ctx context.Context) map[string]string { return rigVersions(ctx, files, rigs) },
		Evidence: func(ctx context.Context) ([]application.NeedsYouText, error) { return ideaEvidence(ctx, dir, gateway) },
	}
	if limits, err := config.Dampers(); err == nil {
		survey.Limit = limits.ReviewQueue
	}
	if cards, err := sentCards(); err == nil {
		survey.Cards = cards
	}
	if state, err := config.IdeasOnlyDir(); err == nil {
		survey.Released = ideasonly.New(state).Released
	}
	return survey
}

// sleepHandoffs is how many of the Mayor's newest handoffs the sweep of the
// Ideas only cards reads for the txids they name.
const sleepHandoffs = 60

// ideaEvidence is the texts a handled idea's txid is named in: the Mayor's
// newest handoffs and the live beads' descriptions.
func ideaEvidence(ctx context.Context, vaultDir string, gateway *beads.Gateway) ([]application.NeedsYouText, error) {
	var texts []application.NeedsYouText
	handoffs := filepath.Join(vaultDir, "seats", "mayor", "handoffs")
	if names, err := os.ReadDir(handoffs); err == nil {
		sort.Slice(names, func(a, b int) bool { return names[a].Name() < names[b].Name() })
		if len(names) > sleepHandoffs {
			names = names[len(names)-sleepHandoffs:]
		}
		for _, name := range names {
			if raw, err := os.ReadFile(filepath.Join(handoffs, name.Name())); err == nil {
				texts = append(texts, application.NeedsYouText{Source: "handoff " + strings.TrimSuffix(name.Name(), ".md"), Text: string(raw)})
			}
		}
	}
	var dump bytes.Buffer
	if _, err := (application.TriageDump{Source: gateway}).Run(ctx, &dump); err != nil {
		return texts, fmt.Errorf("the live beads: %w", err)
	}
	lines, err := application.ReadTriageLines(&dump)
	if err != nil {
		return texts, err
	}
	for _, line := range lines {
		texts = append(texts, application.NeedsYouText{Source: line.ID, Text: line.Description})
	}
	return texts, nil
}

// rigVersions is the current version of each rig checked out, by rig name, read
// from the first of the rig's version_files in its checkout; a rig with none
// has no entry.
func rigVersions(ctx context.Context, rules application.EpicRules, rigs map[string]string) map[string]string {
	versions := map[string]string{}
	for name, checkout := range rigs {
		req, err := rules.EpicRequirements(ctx, name)
		if err != nil || len(req.VersionFiles) == 0 {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(checkout, filepath.FromSlash(req.VersionFiles[0])))
		if err != nil {
			continue
		}
		if version, err := domain.VersionOf(raw); err == nil {
			versions[name] = version
		}
	}
	return versions
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
		Now:    time.Now,
		Paused: application.FactoryPausedByNote(mwGateway(dir, host)),
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
