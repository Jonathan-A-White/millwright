package main

import (
	"context"
	"fmt"
	"io"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/pause"
	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"

	"github.com/spf13/cobra"
)

// newNoteCmd builds `mw note <bead> "<text>" [--file <path>]`: a bead comment
// whose {now} is the clock's time, so a seat never types one.
func newNoteCmd() *cobra.Command {
	var file, typeHere string
	var toBuilder bool
	cmd := &cobra.Command{
		Use:   `note <bead> "<text>" | note <bead> --file <path>`,
		Short: "Comment on a bead, with {now} replaced by the clock's UTC time",
		Long: "note adds the text to the bead as a comment, with every " + application.NoteStampToken + " in it replaced by the\n" +
			"current UTC time from the clock at the moment of writing, to the minute, like\n" +
			"2026-10-13 06:30Z. A seat that types a time into a comment gets it wrong; write " + application.NoteStampToken + "\n" +
			"instead. Text with no " + application.NoteStampToken + " is written as it is, as bd comment would.\n\n" +
			"With --to-builder the note is for the Builder of a story that is running: it is written as a\n" +
			"comment that opens " + application.NoteToBuilderMark + " and, when a session works the story, typed into that\n" +
			"session (on this host, or on the story's own over ssh) with a request to reply with a comment that\n" +
			"opens " + application.NoteReplyMark + ". The delivery is recorded on the bead. With no session live the note\n" +
			"is only written, and the boot of the story's next attempt leads with it. Use it to withdraw or change\n" +
			"something on a story that is under way: a plain comment is read by nobody until the story is closed.\n\n" +
			"With --file the text is read from the file instead of the command line, with the same\n" +
			"replacement. The time used is printed.\n\n" +
			"A bead the tracker does not know, a file that cannot be read and a note with no text\n" +
			"are refused, and nothing is written.",
		Args: func(cmd *cobra.Command, args []string) error {
			want := 2
			if file != "" || typeHere != "" {
				want = 1
			}
			if len(args) != want {
				return fmt.Errorf("note takes the bead and either the text or --file <path>")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if typeHere != "" {
				// The home's note asking this host to type a line into a session of its own.
				return application.Note{Runner: tmux.New()}.TypeHere(cmd.Context(), args[0], typeHere)
			}
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			note := application.Note{Tracker: gateway, Out: cmd.OutOrStdout()}
			if toBuilder {
				host, err := config.Host()
				if err != nil {
					return err
				}
				note.ToBuilder, note.Host, note.Runner = true, host, tmux.New()
				note.Reach = lazyBuilderReach{out: cmd.ErrOrStderr()}
			}
			if file != "" {
				_, err = note.RunFile(cmd.Context(), args[0], file)
			} else {
				_, err = note.Run(cmd.Context(), args[0], args[1])
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&toBuilder, "to-builder", false, "the note is for the Builder of a running story: type it into its session, or lead its next attempt with it")
	cmd.Flags().StringVar(&typeHere, "type-here", "", "type this line into the story's session on this host, as the home asks over ssh")
	_ = cmd.Flags().MarkHidden("type-here")
	cmd.Flags().StringVar(&file, "file", "", "read the text of the note from this file")
	return cmd
}

// lazyBuilderReach reads how hosts are reached only when a note is for a story
// on another host.
type lazyBuilderReach struct{ out io.Writer }

func (l lazyBuilderReach) Type(ctx context.Context, host, storyID, line string) error {
	reach, gaps, err := storyReachOf(ctx, pauseCloud(ctx, l.out))
	if err != nil {
		return err
	}
	return pause.BuilderReach{Reach: reach, Gaps: gaps}.Type(ctx, host, storyID, line)
}
