package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/spf13/cobra"
)

// newFileCmd builds `mw file`: the one way the Mayor's plan becomes tracked
// work. It writes nothing until the whole plan holds together, and what it
// writes is held back from every dispatcher until somebody approves it.
func newFileCmd() *cobra.Command {
	var approved bool
	var waive []string
	var because string
	var guestAsk string

	cmd := &cobra.Command{
		Use:   "file <plan.json>",
		Short: "File the Mayor's plan as an epic and its stories, held until approved",
		Long: "file reads a plan — an epic with the default path its stories inherit, and the stories,\n" +
			"each with its acceptance criteria, its estimate, whatever it overrides of that path and\n" +
			"what it needs done first — and checks the whole of it before writing any of it: every\n" +
			"story must resolve to a path, carry acceptance criteria, and wait only on stories the\n" +
			"plan has and not on itself, directly or in a circle.\n\n" +
			"Every story is filed held, so that nothing can be dispatched from a plan nobody has\n" +
			"approved. --approve releases them all as they are filed; without it, mw asks, and an\n" +
			"unattended run leaves them held.\n\n" +
			"A rig's file in the vault (rigs/<rig>.toml) may list epic_sections, headings the epic's\n" +
			"description must contain, and epic_last_story_labels, labels the epic must carry on a story\n" +
			"that waits on every other: a plan that lacks either is refused, naming each, and nothing is\n" +
			"written. Only the Governor's word excuses one: --waive <name> (once for each) with --because\n" +
			"\"<his words>\", which are written on the epic.\n\n" +
			"A rig's file may also say guest = \"<owner>\": a guest repo, whose owner is not the Governor.\n" +
			"A plan with a story in one is refused, naming the rig and the owner, and nothing is\n" +
			"written, unless the owner asked: --guest-ask \"<their words>\", which are written on the\n" +
			"epic. mw status and mw brief show 'guest: <owner>' beside such a rig's stories.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			written, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("reading the plan: %w", err)
			}
			plan, err := domain.ParsePlan(written)
			if err != nil {
				return fmt.Errorf("reading %s: %w", args[0], err)
			}

			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			dir, err := config.Vault()
			if err != nil {
				return err
			}

			_, err = application.File{
				Tracker:  gateway,
				Rules:    vault.New(dir),
				Waive:    application.EpicWaiver{Names: waive, Because: because},
				GuestAsk: guestAsk,
				Out:      cmd.OutOrStdout(),
				Approve:  approval(cmd, approved),
			}.Run(cmd.Context(), plan)
			return err
		},
	}

	cmd.Flags().BoolVar(&approved, "approve", false,
		"release the stories as they are filed, instead of leaving them held")
	cmd.Flags().StringArrayVar(&waive, "waive", nil,
		"a requirement of the rig the Governor waived for this epic, by name; needs --because")
	cmd.Flags().StringVar(&because, "because", "",
		"the Governor's own words for waiving it, written on the epic")
	cmd.Flags().StringVar(&guestAsk, "guest-ask", "",
		"the owner's own words asking for work in a guest rig, written on the epic; needed to file a plan in one")
	return cmd
}

// approval is who says whether the filed stories may be released: --approve if
// it was given, the person at the terminal if there is one, and nobody at all
// otherwise — an unattended run files the plan and leaves it held, which is the
// safe way round.
func approval(cmd *cobra.Command, approved bool) func(context.Context, application.FiledPlan) (bool, error) {
	if approved {
		return func(context.Context, application.FiledPlan) (bool, error) { return true, nil }
	}
	in := cmd.InOrStdin()
	if !atATerminal(in) {
		return nil
	}
	return func(_ context.Context, filed application.FiledPlan) (bool, error) {
		return confirm(in, cmd.OutOrStdout(),
			fmt.Sprintf("\nRelease all %d stories of %s to the dispatcher? [y/N] ", len(filed.Stories), filed.EpicID))
	}
}

// confirm asks a question and reads the answer. Anything but a plain yes is a
// no: filing again is cheap, and releasing work nobody meant to release is not.
func confirm(in io.Reader, out io.Writer, question string) (bool, error) {
	fmt.Fprint(out, question)

	answer, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true, nil
	}
	return false, nil
}

// atATerminal reports whether there is a person to ask on the other end. It is
// the standard library's answer to that question rather than a true isatty,
// which would cost a dependency: a terminal is a character device, and so is
// /dev/null, which is how an unattended run is usually given no input. Both
// ends of the guess are safe — a pipe is never asked, and a run nobody answers
// leaves the plan held.
func atATerminal(in io.Reader) bool {
	file, isFile := in.(*os.File)
	if !isFile {
		return false
	}
	info, err := file.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	return file.Name() != os.DevNull
}
