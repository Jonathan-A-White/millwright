package main

import (
	"fmt"
	"io"
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
	cmd.AddCommand(newVerifyMatchCmd(), newVerifyConfirmCmd(), newVerifyListCmd())
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
			match, _, err := newVerifyMatch(cmd.OutOrStdout())
			if err != nil {
				return err
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

// newVerifyMatch is the mw verify match the config describes, saying what it did
// on out, and the [verify] settings it was read with.
func newVerifyMatch(out io.Writer) (application.VerifyMatch, config.VerifySettings, error) {
	dir, err := config.Vault()
	if err != nil {
		return application.VerifyMatch{}, config.VerifySettings{}, err
	}
	host, err := config.Host()
	if err != nil {
		return application.VerifyMatch{}, config.VerifySettings{}, err
	}
	settings, err := config.Verify()
	if err != nil {
		return application.VerifyMatch{}, settings, err
	}
	inbox, err := config.PosternInboxDir()
	if err != nil {
		return application.VerifyMatch{}, settings, err
	}
	runs, err := config.VerifyRunDir()
	if err != nil {
		return application.VerifyMatch{}, settings, err
	}
	gateway := mwGateway(dir, host)
	match := application.VerifyMatch{
		Beads: gateway, Grinder: claude.NewGrinder(), Mailbox: gateway,
		Settings: application.VerifySettings{Model: settings.Model, Effort: settings.Effort, Confidence: settings.Confidence},
		InboxDir: inbox, RunDir: runs, Host: host, Out: out,
	}
	// The API key bills the call when [grist] api_credits is on, as it does the mill's grinds.
	if ceilings, err := config.Grist(); err == nil && ceilings.APICredits {
		if state, err := config.GristStateDir(); err == nil {
			match.Credits = newGristCredits(host, state, ceilings)
		}
	}
	return match, settings, nil
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

func newVerifyConfirmCmd() *cobra.Command {
	var txid, as, shows string
	cmd := &cobra.Command{
		Use:   "confirm <bead> --txid <txid> [--as mayor|deputy] [--shows \"<text>\"]",
		Short: "Record a PROPOSED match as verified by his use: the VERIFIED comment, the demo closed, its card cleared",
		Long: "confirm reads the PROPOSED comment for that txid on the bead (mw verify match wrote it) and writes the\n" +
			"standing comment, whose first word is VERIFIED:\n" +
			"  VERIFIED by the Governor's use, inferred by the <Mayor|Deputy> from <txid>: <shows> (proposed by mw verify match, run <id>)\n" +
			"A bead that carries the label demo and is open or deferred is then closed on that reason, and its card is\n" +
			"cleared as mw postern clear does, with one reply in the card's thread. Any other bead, a landing already\n" +
			"closed, gets the comment only and keeps its status. No agent is called.\n\n" +
			"A bead with no PROPOSED comment for the txid is refused, unless --shows \"<text>\" says what shows it: the\n" +
			"comment then ends (by hand). A demo whose title, description or epic touches money, keys, secrets or a\n" +
			"charter is refused: it is his word to give, not a seat's. A bead already carrying a VERIFIED comment is refused.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gateway, _, err := posternGateway()
			if err != nil {
				return err
			}
			return application.VerifyConfirm{
				Beads: gateway, Notes: gateway, Sender: newHandsPush(gateway), Out: cmd.OutOrStdout(),
			}.Run(cmd.Context(), application.VerifyConfirmRequest{Bead: args[0], Txid: txid, As: as, Shows: shows})
		},
	}
	cmd.Flags().StringVar(&txid, "txid", "", "the txid of his message or picture that shows it (required)")
	cmd.Flags().StringVar(&as, "as", "mayor", "who infers it: mayor or deputy")
	cmd.Flags().StringVar(&shows, "shows", "", "what the evidence shows; required when no PROPOSED comment names the txid, and then recorded as by hand")
	_ = cmd.MarkFlagRequired("txid")
	return cmd
}

func newVerifyListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Print the proposals of mw verify match that wait to be confirmed",
		Long: "list prints every open demo, and every landing of the last 14 days, that has a PROPOSED comment and no\n" +
			"VERIFIED one: the bead, the txid, what it shows, the confidence, and the mw verify confirm line. A demo\n" +
			"that asks his own word (money, keys, a charter) is listed with that reason instead of the line. It reads\n" +
			"the tracker and calls no agent; it supersedes the vault's bin/mayor-unverified.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			gateway, _, err := posternGateway()
			if err != nil {
				return err
			}
			_, err = application.VerifyList{Beads: gateway, Out: cmd.OutOrStdout()}.Run(cmd.Context())
			return err
		},
	}
}
