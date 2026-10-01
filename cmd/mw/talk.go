package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
