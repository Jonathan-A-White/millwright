package main

import (
	"fmt"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"

	"github.com/spf13/cobra"
)

// newNoteCmd builds `mw note <bead> "<text>" [--file <path>]`: a bead comment
// whose {now} is the clock's time, so a seat never types one.
func newNoteCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   `note <bead> "<text>" | note <bead> --file <path>`,
		Short: "Comment on a bead, with {now} replaced by the clock's UTC time",
		Long: "note adds the text to the bead as a comment, with every " + application.NoteStampToken + " in it replaced by the\n" +
			"current UTC time from the clock at the moment of writing, to the minute, like\n" +
			"2026-10-13 06:30Z. A seat that types a time into a comment gets it wrong; write " + application.NoteStampToken + "\n" +
			"instead. Text with no " + application.NoteStampToken + " is written as it is, as bd comment would.\n\n" +
			"With --file the text is read from the file instead of the command line, with the same\n" +
			"replacement. The time used is printed.\n\n" +
			"A bead the tracker does not know, a file that cannot be read and a note with no text\n" +
			"are refused, and nothing is written.",
		Args: func(cmd *cobra.Command, args []string) error {
			want := 2
			if file != "" {
				want = 1
			}
			if len(args) != want {
				return fmt.Errorf("note takes the bead and either the text or --file <path>")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			note := application.Note{Tracker: gateway, Out: cmd.OutOrStdout()}
			if file != "" {
				_, err = note.RunFile(cmd.Context(), args[0], file)
			} else {
				_, err = note.Run(cmd.Context(), args[0], args[1])
			}
			return err
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "read the text of the note from this file")
	return cmd
}
