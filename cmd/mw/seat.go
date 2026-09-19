package main

import (
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"

	"github.com/spf13/cobra"
)

// newSeatCmd builds `mw seat`: the commands about a seat's own session. It has
// no behaviour of its own; each subcommand is one thing to ask about it.
func newSeatCmd() *cobra.Command {
	seat := &cobra.Command{
		Use:   "seat",
		Short: "Ask about a seat's own session",
		Long:  "seat holds the commands about a seat's live session, as opposed to the story it is working.",
		Args:  cobra.NoArgs,
	}
	seat.AddCommand(newSeatContextCmd())
	return seat
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
