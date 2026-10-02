package main

import (
	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/peekremote"
	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"
)

// newPeekCmd builds `mw peek`: where a running Builder has got to, from any host.
func newPeekCmd() *cobra.Command {
	var here bool
	cmd := &cobra.Command{
		Use:   "peek <story>",
		Short: "Show where a story's Builder has got to, on whichever host works it",
		Long: "peek prints, for the story's session: the host, the session name, when it was launched\n" +
			"and how long ago, the formula's current step (or `print-mode run, steps not tracked` when\n" +
			"the story has no molecule), and the last 20 lines of what the harness has recorded of it,\n" +
			"or of the session's pane when it has recorded none. It says when the session is gone or\n" +
			"has exited, and when the story is closed. A story worked on another host is read there:\n" +
			"mw runs `mw peek --here` on it over the ssh line [hands_hosts] gives. Nothing is written.\n\n" +
			"--here reads this host's session only, whoever the story is pathed to; it is what the\n" +
			"far end of an ssh peek runs.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			host, err := config.Host()
			if err != nil {
				return err
			}
			rigs, err := config.Rigs()
			if err != nil {
				return err
			}
			reach, err := config.HandsHosts()
			if err != nil {
				return err
			}
			root, err := claude.DefaultProjectsRoot()
			if err != nil {
				return err
			}
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			return application.Peek{
				Tracker:     gateway,
				Runner:      tmux.New(),
				Transcripts: claude.NewTranscripts(root),
				Remote:      peekremote.Remote{Reach: reach},
				Rigs:        rigs,
				Host:        host,
				Local:       here,
				Out:         cmd.OutOrStdout(),
			}.Run(cmd.Context(), args[0])
		},
	}
	cmd.Flags().BoolVar(&here, "here", false, "read this host's session only, never ssh to the story's host")
	return cmd
}
