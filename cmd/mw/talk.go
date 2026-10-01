package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
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
	talk.AddCommand(newTalkModelCmd())
	talk.AddCommand(newTalkSayCmd())
	talk.AddCommand(newTalkWaitCmd())
	return talk
}

// talkModelSeat is the seat whose model `mw talk model` switches.
const talkModelSeat = "mayor"

// startDetached starts mw again with the given arguments, as a process of its
// own that outlives this one. A test swaps it to see what would be started.
var startDetached = func(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding the mw that is running, to start the watch with: %w", err)
	}
	// Not exec.CommandContext: the watch is meant to outlive the command that
	// started it. Its standard streams are left nil, the null device, and it
	// runs in a session of its own, so that it survives the turn it was started
	// from ending.
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting the watch: %w", err)
	}
	return cmd.Process.Release()
}

// newTalkModelCmd builds `mw talk model`: the Mayor's model switched from the
// next turn, typed into the Mayor's window once it is safe to type there.
func newTalkModelCmd() *cobra.Command {
	var foreground bool
	var interval, limit time.Duration
	cmd := &cobra.Command{
		Use:   "model opus|sonnet|fable",
		Short: "Switch the Mayor's model from the next turn, typed at an empty idle prompt",
		Long: "model switches the model the acting Mayor's session runs on, so that the Governor's 'use Sonnet'\n" +
			"takes effect from the next turn. It types /model <model> and Enter into the window the vault's\n" +
			".mayor-acting names, the way a person at the keyboard would.\n\n" +
			"It starts a watch detached and returns at once: the Mayor runs it mid-turn, and nothing can be\n" +
			"typed until that turn is over. The watch types only once the window is idle at an empty input\n" +
			"line on two looks in a row, and never over anything typed on that line; Claude Code's dim\n" +
			"suggested prompt is not a draft. It looks every --interval (2s) and gives up after --limit (10m),\n" +
			"typing nothing. Arming, typing and giving up each append one dated line to .mayor-talk.log in\n" +
			"the vault. It works on tmux's default server unless $" + TmuxSocketEnv + " names another.\n\n" +
			"--foreground watches in this process instead, and exits non-zero when it gave up or could not type.",
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
			host, err := config.Host()
			if err != nil {
				return err
			}

			if !foreground {
				watch := []string{"talk", "model", string(model), "--foreground",
					"--interval", interval.String(), "--limit", limit.String()}
				if err := startDetached(watch); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "watching the %s's window to type /model %s at an empty idle prompt; see %s\n",
					talkModelSeat, model, filepath.Join(dir, application.TalkLogFileName(talkModelSeat)))
				return nil
			}

			files := vault.New(dir)
			_, err = application.TalkModel{
				Seats:    files,
				Terminal: seatWindows(),
				Log:      files,
				Seat:     talkModelSeat,
				Host:     host,
				Model:    model,
				Interval: interval,
				Limit:    limit,
				Out:      cmd.OutOrStdout(),
			}.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().BoolVar(&foreground, "foreground", false, "watch in this process instead of starting the watch detached")
	cmd.Flags().DurationVar(&interval, "interval", application.DefaultTalkModelInterval, "how long between looks")
	cmd.Flags().DurationVar(&limit, "limit", application.DefaultTalkModelLimit, "how long to keep looking before giving up")
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

// talkHoldingReply is the holding answer mw talk wait sends: the configured
// text when --hold is on, none otherwise.
func talkHoldingReply(hold bool, configured string) string {
	if !hold {
		return ""
	}
	return configured
}

// newTalkWaitCmd builds `mw talk wait`: the zero-token wait for the Governor's
// next turn, which the Mayor's harness runs in the background.
func newTalkWaitCmd() *cobra.Command {
	var limit, minBackoff, maxBackoff time.Duration
	var hold bool
	holdingConfigured, holdingErr := config.TalkHoldingReply()
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
			"The first run starts at the index's head: a turn from before it ever ran is not waited for. A\n" +
			"stream that drops is opened again after a pause that doubles from --min-backoff to --max-backoff.\n" +
			"It ends, saying so, after --limit with no turn: arm it again. $" + TalkWaitLimitEnv + " (seconds)\n" +
			"sets the default of --limit, as MW_MAIL_WAIT_LIMIT does for mail-wait.\n\n" +
			"With --hold (on when config talk_holding_reply, or $" + config.TalkHoldingReplyEnv + ", is set) a\n" +
			"turn is answered the moment it is heard, before it is printed: the configured text is sent, at\n" +
			"zero tokens, as the section 20 holding answer to that talk and turn, exactly what mw talk say\n" +
			"--holding sends, and a line 'holding sent in N ms' follows the turn. The end of a talk gets no\n" +
			"holding reply, nor does a wait with nothing configured; --hold=false turns it off for one wait.\n\n" +
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
			if holdingErr != nil {
				return holdingErr
			}
			_, err = application.TalkWait{
				Stream:       backend,
				Postern:      backend,
				Cipher:       posternCipher(keys),
				Keys:         keys,
				Memory:       gateway,
				Mailbox:      gateway,
				GovernorKey:  governorKey,
				HoldingReply: talkHoldingReply(hold, holdingConfigured),
				Limit:        limit,
				MinBackoff:   minBackoff,
				MaxBackoff:   maxBackoff,
				Out:          cmd.OutOrStdout(),
				Err:          cmd.ErrOrStderr(),
			}.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().BoolVar(&hold, "hold", holdingConfigured != "", "send the configured holding reply (talk_holding_reply) the moment a turn arrives")
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
