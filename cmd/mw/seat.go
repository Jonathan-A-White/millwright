package main

import (
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
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
	return seat
}

// newSeatUpCmd builds `mw seat up`: the seat's next session, started in a
// window of its own. It is the one command here that starts anything.
func newSeatUpCmd() *cobra.Command {
	var model, effort, reason string
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
			"seat is handed over.",
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

			_, err = application.SeatUp{
				Seats:   vault.New(dir),
				Windows: tmux.NewWindows(),
				Harness: claude.New(),
				Seat:    args[0],
				Host:    host,
				Model:   domain.Model(model),
				Effort:  domain.Effort(effort),
				Reason:  reason,
				Out:     cmd.OutOrStdout(),
			}.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().StringVar(&model, "model", "", "the model the session runs on (default: the harness's own)")
	cmd.Flags().StringVar(&effort, "effort", "", "how hard the session is asked to think (default: the harness's own)")
	cmd.Flags().StringVar(&reason, "reason", "", "why the session is being started, told to it after the kickoff")
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
