package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/reaper"
	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/spf13/cobra"
)

// newSeatCmd builds `mw seat`: the commands about a seat's own session. It has
// no behaviour of its own; each subcommand is one thing to do with it.
func newSeatCmd() *cobra.Command {
	seat := &cobra.Command{
		Use:   "seat",
		Short: "Work with a seat's own session",
		Long:  "seat holds the commands about a seat's live session, as opposed to the story it is working.",
		Args:  cobra.NoArgs,
	}
	seat.AddCommand(newSeatContextCmd())
	seat.AddCommand(newSeatUpCmd())
	seat.AddCommand(newSeatReapCmd())
	return seat
}

// TmuxSocketEnv names the tmux server the seat commands work on, tmux's -L. Unset,
// they work on tmux's default server: the one the person's windows are in. It
// exists so that a test can run them against a server of its own.
const TmuxSocketEnv = "MW_TMUX_SOCKET"

// seatWindows is the terminal the seat commands work on.
func seatWindows() *tmux.Windows {
	return tmux.NewWindows(tmux.WithSocket(os.Getenv(TmuxSocketEnv)))
}

// newSeatUpCmd builds `mw seat up`: the seat's next session, started in a
// window of its own. It is the one command here that starts anything.
func newSeatUpCmd() *cobra.Command {
	var model, effort, reason string
	var reapWhenIdle bool
	cmd := &cobra.Command{
		Use:   "up <seat>",
		Short: "Start a seat's next session in a window of its own",
		Long: "up starts one interactive Claude Code session for the seat, in a new window of the tmux\n" +
			"session mw is running in — or of a detached session named " + tmux.SeatsSession + ", when mw is running\n" +
			"outside tmux. The window is named <seat>-<UTC date>-<nn>, with nn one past the highest\n" +
			"number the seat has used for a handoff or a window.\n\n" +
			"The session is primed with the seat's charter and told the seat's own kickoff text\n" +
			"(seats/<seat>/kickoff.md), or a default when it keeps none, ending with the newest handoff\n" +
			"to boot from and the --reason it was started for. Nothing else the vault holds is read or\n" +
			"passed: no ledger, no memory of a rig, no prime from the tracker.\n\n" +
			"Handoffs are read from seats/<seat>/hosts/<host>/handoffs/ for a seat that keeps a directory\n" +
			"for this host, and from seats/<seat>/handoffs/ for one that does not.\n\n" +
			"It refuses and starts nothing when the seat has no charter, has written no handoff, or is\n" +
			"already acting in a window that is still open and has written no handoff since it opened.\n" +
			"mw never writes the acting file: the new session writes it at boot, and that is how the\n" +
			"seat is handed over.\n\n" +
			"Run from a tmux window the seat's acting file names, it also arms `mw seat reap` on that window, so\n" +
			"that the outgoing session's window closes itself once its successor has the seat. --reap-when-idle\n" +
			"arms one in idle mode on the window it opens, for a session that hands over to nobody.",
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

			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("finding the mw that is running, to arm a reaper with: %w", err)
			}

			windows := seatWindows()
			_, err = application.SeatUp{
				Seats:   vault.New(dir),
				Windows: windows,
				Harness: claude.New(),
				Seat:    args[0],
				Host:    host,
				Model:   domain.Model(model),
				Effort:  domain.Effort(effort),
				Reason:  reason,
				Out:     cmd.OutOrStdout(),

				Terminal:     windows,
				Armer:        reaper.New(exe),
				ReapWhenIdle: reapWhenIdle,
			}.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().BoolVar(&reapWhenIdle, "reap-when-idle", false, "close the new window once a handoff newer than it exists and its pane is idle")
	cmd.Flags().StringVar(&model, "model", "", "the model the session runs on (default: the harness's own)")
	cmd.Flags().StringVar(&effort, "effort", "", "how hard the session is asked to think (default: the harness's own)")
	cmd.Flags().StringVar(&reason, "reason", "", "why the session is being started, told to it after the kickoff")
	return cmd
}

// newSeatReapCmd builds `mw seat reap`: the watcher that closes a finished
// session's window. It is zero-token and meant to be started detached, by `mw
// seat up`; it is a command of its own so that it can also be run by hand.
func newSeatReapCmd() *cobra.Command {
	var window string
	var whenIdle bool
	var interval, limit time.Duration
	cmd := &cobra.Command{
		Use:   "reap <seat> --window <tmux window id>",
		Short: "Close a finished session's window once it is done",
		Long: "reap watches one tmux window and closes it when the session in it is finished: a session ends its\n" +
			"own window, and no session ever closes another's. It costs no tokens and runs until the window is\n" +
			"closed or the limit is up.\n\n" +
			"In successor mode, the default, it closes the window once the seat's acting file (.<seat>-acting in\n" +
			"the vault) names someone else than it did when the watch was armed, that someone's window is open,\n" +
			"and the window's pane is idle. With --when-idle, for a session with nobody to hand over to, it closes\n" +
			"the window once a handoff newer than the window exists and its pane is idle. Idle means an empty\n" +
			"input line with nothing running, on two looks in a row; a window whose input line holds text is never\n" +
			"closed, in either mode.\n\n" +
			"It looks every --interval (30s) and gives up after --limit (3h), closing nothing and saying so. Arming,\n" +
			"closing and giving up each append one dated line to .<seat>-reaper.log in the vault. It works on\n" +
			"tmux's default server unless $" + TmuxSocketEnv + " names another.\n\n" +
			"It exits non-zero when it gave up or could not close the window.",
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

			files := vault.New(dir)
			_, err = application.SeatReap{
				Seats:    files,
				Terminal: seatWindows(),
				Log:      files,
				Seat:     args[0],
				Host:     host,
				Window:   window,
				WhenIdle: whenIdle,
				Interval: interval,
				Limit:    limit,
				Out:      cmd.OutOrStdout(),
			}.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().StringVar(&window, "window", "", "the tmux window to close, by id (like @3)")
	cmd.Flags().BoolVar(&whenIdle, "when-idle", false, "close once a handoff newer than the window exists and its pane is idle, with no successor needed")
	cmd.Flags().DurationVar(&interval, "interval", application.DefaultReapInterval, "how long between looks")
	cmd.Flags().DurationVar(&limit, "limit", application.DefaultReapLimit, "how long to keep looking before giving up")
	return cmd
}

// newSeatContextCmd builds `mw seat context`: how full a seat's live session
// is against its handoff limit. It is read-only and zero-token: it reads a
// transcript and starts and writes nothing.
func newSeatContextCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Print a seat session's live context against its handoff limit",
		Long: "context prints one line: `context=<n> handoff_at=<limit> ok|handoff session=<id>`. n is the\n" +
			"size of the context the last assistant turn of the newest Claude Code transcript for the\n" +
			"directory was given: its input, cache-read and cache-creation tokens together. The directory\n" +
			"is the vault from config unless --dir names another. The limit is config `handoff_at` (or\n" +
			"$MW_HANDOFF_AT), default 180000, and the line says handoff at or above it.\n\n" +
			"Both ok and handoff exit 0. A directory with no transcript, or a transcript with no\n" +
			"assistant turn yet, is an error that says where it looked. Nothing is started, written or\n" +
			"spent: context only reads.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if dir == "" {
				vault, err := config.Vault()
				if err != nil {
					return err
				}
				dir = vault
			}
			where, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			limit, err := config.HandoffAt()
			if err != nil {
				return err
			}
			root, err := claude.DefaultProjectsRoot()
			if err != nil {
				return err
			}

			_, err = application.SeatContext{
				Transcripts: claude.NewTranscripts(root),
				Dir:         where,
				HandoffAt:   limit,
				Out:         cmd.OutOrStdout(),
			}.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "the working directory of the session to read (default: the vault)")
	return cmd
}
