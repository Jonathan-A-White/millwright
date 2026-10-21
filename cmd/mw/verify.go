package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// newVerifyCmd builds `mw verify`: what the Governor's own use shows met.
func newVerifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Match the Governor's messages and pictures to the demos and landings they show met",
	}
	cmd.AddCommand(newVerifyMatchCmd())
	return cmd
}

func newVerifyMatchCmd() *cobra.Command {
	var since string
	var txids []string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "match [--since <dur>] [--txid <id>]... [--dry-run]",
		Short: "One bounded agent call proposes which open demos and unverified landings his messages and pictures show met",
		Long: "match reads the candidates: every open demo (the demos mw stuck --dampers counts) with its description and\n" +
			"the HOW TO CHECK IT of its epic's closed stories, and every landing closed within --since (default 7d)\n" +
			"that no comment opening VERIFIED records, with the HOW TO CHECK IT of its closing comment; and the\n" +
			"evidence: the messages and pictures of the Governor's the inbox pass saved within --since, or only the\n" +
			"--txid ones given. It makes one agent call (model [verify] model, effort [verify] effort; haiku and\n" +
			"medium; billed to the API key when [grist] api_credits is on) over a directory of those files, asking\n" +
			"which candidates the evidence shows met and what exactly shows it.\n\n" +
			"A match at or over [verify] confidence (0.8) becomes a comment on its bead whose first word is PROPOSED,\n" +
			"naming the txid, the time and what it shows; a bead already proposed for that txid is not commented\n" +
			"twice. One mail to the Mayor lists the run's proposals with the line that confirms each; none when\n" +
			"there are none. Nothing is closed, no status changes, and no comment opens with VERIFIED. The run is\n" +
			"recorded in ~/.local/state/mw/verify/<run>.json: model, effort, tokens, candidates and every answer.\n\n" +
			"--dry-run prints the candidates, the evidence and the directory's size, and calls no agent.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			window, err := parseVerifySince(since)
			if err != nil {
				return err
			}
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			settings, err := config.Verify()
			if err != nil {
				return err
			}
			inbox, err := config.PosternInboxDir()
			if err != nil {
				return err
			}
			runs, err := config.VerifyRunDir()
			if err != nil {
				return err
			}
			gateway := mwGateway(dir, host)
			match := application.VerifyMatch{
				Beads: gateway, Grinder: claude.NewGrinder(), Mailbox: gateway,
				Settings: application.VerifySettings{Model: settings.Model, Effort: settings.Effort, Confidence: settings.Confidence},
				InboxDir: inbox, RunDir: runs, Host: host, Out: cmd.OutOrStdout(),
			}
			// The API key bills the call when [grist] api_credits is on, as it does the mill's grinds.
			if ceilings, err := config.Grist(); err == nil && ceilings.APICredits {
				if state, err := config.GristStateDir(); err == nil {
					match.Credits = newGristCredits(host, state, ceilings)
				}
			}
			_, err = match.Run(cmd.Context(), application.VerifyOptions{Since: window, Txids: txids, DryRun: dryRun})
			return err
		},
	}
	cmd.Flags().StringVar(&since, "since", "7d", "how far back to read landings and evidence: a duration like 36h or 7d")
	cmd.Flags().StringArrayVar(&txids, "txid", nil, "match only the evidence of this txid, whatever its age (repeatable)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the candidates, the evidence and the directory's size; call no agent")
	return cmd
}

// parseVerifySince reads --since: a Go duration, or whole days as 7d.
func parseVerifySince(said string) (time.Duration, error) {
	said = strings.TrimSpace(said)
	if days, ok := strings.CutSuffix(said, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n < 1 {
			return 0, fmt.Errorf("--since %q: days must be a whole number, 1 or more", said)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	window, err := time.ParseDuration(said)
	if err != nil || window <= 0 {
		return 0, fmt.Errorf("--since %q: use a duration like 36h or 7d", said)
	}
	return window, nil
}
