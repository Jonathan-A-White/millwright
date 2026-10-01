package main

import (
	"os"
	"strconv"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/spf13/cobra"
)

// newTalkCmd builds `mw talk`: the commands the Mayor talks with the Governor
// by. It has no behaviour of its own.
func newTalkCmd() *cobra.Command {
	talk := &cobra.Command{
		Use:   "talk",
		Short: "Talk with the Governor",
		Long:  "talk holds the commands the Mayor uses in a talk with the Governor.",
		Args:  cobra.NoArgs,
	}
	talk.AddCommand(newTalkCallCmd())
	talk.AddCommand(newTalkModelCmd())
	talk.AddCommand(newTalkSayCmd())
	talk.AddCommand(newTalkWaitCmd())
	return talk
}

// talkModelSeat is the seat whose model `mw talk model` switches.
const talkModelSeat = "mayor"

// newTalkModelCmd builds `mw talk model`: the Governor's model chip answered in
// a talk, by a fresh Mayor on that model.
func newTalkModelCmd() *cobra.Command {
	var talkID string
	var turn int
	cmd := &cobra.Command{
		Use:   "model opus|sonnet|fable|haiku --talk <id> --turn <n>",
		Short: "Answer a model switch in a talk: a fresh Mayor on the chosen model takes the line",
		Long: "model answers the Governor's model chip in a talk. A session cannot change its own model, so the\n" +
			"switch is a fresh Mayor on the chosen one: it speaks on the talk 'Switching to <Name>: a fresh\n" +
			"Mayor takes the line in about a minute', appends a dated line to .mayor-talk.log in the vault, and\n" +
			"prints the one line the Mayor runs next: 'hand off, then: bin/respawn-mayor high <full model id>'.\n" +
			"The chip name is mapped to the full id (sonnet is claude-sonnet-5-5).\n\n" +
			"It types into no window and starts nothing itself; --talk and --turn name the Governor's turn it\n" +
			"answers, as mw talk wait printed them. An unknown chip name is refused before anything is said.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			model := domain.Model(args[0])
			if err := application.CheckTalkModel(model); err != nil {
				return err
			}
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			keys, err := posternKeys()
			if err != nil {
				return err
			}
			governorKey, err := config.PosternGovernorKey()
			if err != nil {
				return err
			}
			backend, err := posternBackend(keys)
			if err != nil {
				return err
			}
			_, err = application.TalkModel{
				Say: application.TalkSay{
					Postern:     backend,
					Cipher:      posternCipher(keys),
					Keys:        keys,
					GovernorKey: governorKey,
					Now:         posternClock,
				},
				Log:   vault.New(dir),
				Seat:  talkModelSeat,
				Model: model,
				Out:   cmd.OutOrStdout(),
			}.Run(cmd.Context(), application.TalkModelRequest{TalkID: talkID, Turn: turn})
			return err
		},
	}
	cmd.Flags().StringVar(&talkID, "talk", "", "the id of the talk, as mw talk wait printed it")
	cmd.Flags().IntVar(&turn, "turn", 0, "the number of the Governor's turn this answers")
	return cmd
}

// TalkWaitLimitEnv is the environment variable that sets, in seconds, how long
// mw talk wait waits, as contrib/mail-wait's MW_MAIL_WAIT_LIMIT does.
const TalkWaitLimitEnv = "MW_TALK_WAIT_LIMIT"

// talkWaitLimit is the default of mw talk wait's --limit: $MW_TALK_WAIT_LIMIT
// seconds when that is a whole number, else application.DefaultTalkWaitLimit.
func talkWaitLimit() time.Duration {
	if seconds, err := strconv.Atoi(os.Getenv(TalkWaitLimitEnv)); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return application.DefaultTalkWaitLimit
}

// newTalkWaitCmd builds `mw talk wait`: the zero-token wait for the Governor's
// next turn, which the Mayor's harness runs in the background.
func newTalkWaitCmd() *cobra.Command {
	var limit, minBackoff, maxBackoff time.Duration
	cmd := &cobra.Command{
		Use:   "wait",
		Short: "Wait, spending no tokens, for the Governor's next turn in a talk",
		Long: "wait holds the postern backend's event stream open and ends at the first talk turn the Governor\n" +
			"sends the Mayor's key, so that the Mayor's harness, running it in the background, wakes the\n" +
			"Mayor the instant a turn is indexed. It uses no model and types into no window.\n\n" +
			"On a message event it reads the records since its own cursor, a bd kv note of its own that the\n" +
			"postern inbox's never moves. It ends at the first talk record that decrypts, is verifiably the\n" +
			"Governor's, and is a turn or the end of the talk; a record of another class, to another key, or\n" +
			"from anyone else is passed over. It prints the turn (talk id, turn, role, model, cut, text), the\n" +
			"milliseconds from the event to the print, and any new mail the Deputy sent the Mayor since the\n" +
			"last turn, each message once. mw postern inbox and its --unread-count leave talk records alone, so\n" +
			"mail-wait never wakes the Mayor a second time for one turn.\n\n" +
			"It also ends at a new postern message for the Mayor's key, one past the postern inbox's cursor\n" +
			"(which it only reads), printing 'new postern message' with each one's channel, txid and first\n" +
			"line; a message already read does not wake it, and a Governor turn that arrives with one wins.\n\n" +
			"It also ends the instant the Governor's call record arrives, printing a request as 'call <txid> at\n" +
			"<time>: <text>' and a later on a ring as 'later <ring txid>'; answer a request with mw talk call.\n\n" +
			"The first run starts at the index's head: a turn from before it ever ran is not waited for. A\n" +
			"stream that drops is opened again after a pause that doubles from --min-backoff to --max-backoff.\n" +
			"It ends, saying so, after --limit with no turn: arm it again. $" + TalkWaitLimitEnv + " (seconds)\n" +
			"sets the default of --limit, as MW_MAIL_WAIT_LIMIT does for mail-wait.\n\n" +
			"The Mayor's key is let onto the event stream without a licence, but reading the records takes a\n" +
			"cockpit licence on it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			keys, err := posternKeys()
			if err != nil {
				return err
			}
			gateway, _, err := posternGateway()
			if err != nil {
				return err
			}
			backend, err := posternBackend(keys)
			if err != nil {
				return err
			}
			governorKey, err := config.PosternGovernorKey()
			if err != nil {
				return err
			}
			_, err = application.TalkWait{
				Stream:      backend,
				Postern:     backend,
				Cipher:      posternCipher(keys),
				Keys:        keys,
				Memory:      gateway,
				Mailbox:     gateway,
				GovernorKey: governorKey,
				Limit:       limit,
				MinBackoff:  minBackoff,
				MaxBackoff:  maxBackoff,
				Out:         cmd.OutOrStdout(),
				Err:         cmd.ErrOrStderr(),
			}.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().DurationVar(&limit, "limit", talkWaitLimit(), "how long to wait for a turn before ending, saying so")
	cmd.Flags().DurationVar(&minBackoff, "min-backoff", application.DefaultTalkWaitMinBackoff, "the first pause before opening a dropped stream again")
	cmd.Flags().DurationVar(&maxBackoff, "max-backoff", application.DefaultTalkWaitMaxBackoff, "the longest pause before opening a dropped stream again")
	return cmd
}

// newTalkSayCmd builds `mw talk say`: the Mayor's answer, encrypted to the
// Governor and delivered direct.
func newTalkSayCmd() *cobra.Command {
	var talkID string
	var turn int
	var holding, end bool
	var links []string
	cmd := &cobra.Command{
		Use:   "say <text> --talk <id> --turn <n> [--holding|--end] [--link <bead>]...",
		Short: "Answer the Governor in a talk",
		Long: "say encrypts <text> to the Governor as postern's docs/protocol.md section 20 turn plaintext and\n" +
			"hands the record straight to the postern backend (section 9), printing the txid and the\n" +
			"milliseconds it took. The record's class is talk and it carries no summary: no word of the\n" +
			"answer is ever pushed, handed to a hook or logged. It uses the direct channel whatever\n" +
			"postern_channel says, and touches no bead and no note, so that it is as quick as it can be.\n\n" +
			"--talk and --turn name the Governor's turn it answers, as mw talk wait printed them. The role is\n" +
			"answer; --holding makes it the short answer sent while the real one is still coming, and --end\n" +
			"the end of the talk. --link <bead> (repeatable) puts a bead id in the record's links field, for\n" +
			"the Governor to open from the answer; it is never put in the spoken text. It refuses when\n" +
			"postern_governor_key is not set.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			keys, err := posternKeys()
			if err != nil {
				return err
			}
			governorKey, err := config.PosternGovernorKey()
			if err != nil {
				return err
			}
			backend, err := posternBackend(keys)
			if err != nil {
				return err
			}
			_, err = application.TalkSay{
				Postern:     backend,
				Cipher:      posternCipher(keys),
				Keys:        keys,
				GovernorKey: governorKey,
				Now:         posternClock,
				Out:         cmd.OutOrStdout(),
			}.Run(cmd.Context(), application.TalkSayRequest{
				Text: args[0], TalkID: talkID, Turn: turn, Holding: holding, End: end, Links: links,
			})
			return err
		},
	}
	cmd.Flags().StringVar(&talkID, "talk", "", "the id of the talk, as mw talk wait printed it")
	cmd.Flags().IntVar(&turn, "turn", 0, "the number of the Governor's turn this answers")
	cmd.Flags().BoolVar(&holding, "holding", false, "send a short holding answer; the real answer follows")
	cmd.Flags().BoolVar(&end, "end", false, "send the end of the talk")
	cmd.Flags().StringArrayVar(&links, "link", nil, "a bead id to carry in the record's links field, not the text (repeatable)")
	return cmd
}

// newTalkCallCmd builds `mw talk call`: the Mayor's call-back, a ring record
// encrypted to the Governor and delivered direct.
func newTalkCallCmd() *cobra.Command {
	var links []string
	var chain bool
	cmd := &cobra.Command{
		Use:   "call <text> [--chain] [--link <bead>]...",
		Short: "Call the Governor back: send a ring",
		Long: "call encrypts <text>, the short line shown with the ring, to the Governor as postern's\n" +
			"docs/protocol.md section 21 ring plaintext and hands the record straight to the postern backend\n" +
			"(section 9), printing the txid and the milliseconds it took. It is the Mayor's answer to a call\n" +
			"request that mw talk wait printed ('call <txid> at <time>: <text>'). The record's class is call\n" +
			"and it carries no summary: the backend never pushes or logs a word of it. It uses the direct\n" +
			"channel whatever postern_channel says, and touches no bead and writes no note.\n\n" +
			"--chain also broadcasts the same record on chain, through the backend's broadcast (local to the\n" +
			"Mayor), so a phone that cannot reach the backend still rings; both txids are printed. Without it\n" +
			"the chain is added by itself when the newest record mw talk wait heard from the Governor came by\n" +
			"chain (a bare txid); one that came direct does not add it.\n\n" +
			"--link <bead> (repeatable) puts a bead id in the record's links field, beside the text and never\n" +
			"in it. It refuses when postern_governor_key is not set.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			keys, err := posternKeys()
			if err != nil {
				return err
			}
			governorKey, err := config.PosternGovernorKey()
			if err != nil {
				return err
			}
			floatSats, err := config.PosternFloatSats()
			if err != nil {
				return err
			}
			backend, err := posternBackend(keys)
			if err != nil {
				return err
			}
			gateway, _, err := posternGateway()
			if err != nil {
				return err
			}
			_, err = application.TalkCall{
				Postern:     backend,
				Cipher:      posternCipher(keys),
				Keys:        keys,
				GovernorKey: governorKey,
				Notes:       gateway,
				FloatSats:   int64(floatSats),
				Now:         posternClock,
				Out:         cmd.OutOrStdout(),
			}.Run(cmd.Context(), application.TalkCallRequest{Text: args[0], Links: links, Chain: chain})
			return err
		},
	}
	cmd.Flags().StringArrayVar(&links, "link", nil, "a bead id to carry in the record's links field, not the text (repeatable)")
	cmd.Flags().BoolVar(&chain, "chain", false, "also broadcast the ring on chain, for a phone that cannot reach the backend")
	return cmd
}
