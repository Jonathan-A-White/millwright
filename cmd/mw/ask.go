package main

import (
	"context"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"

	"github.com/spf13/cobra"
)

// newAskCmd builds `mw ask`: who outside the factory asked for a piece of work
// or was asked for something, and the beads waiting on them.
func newAskCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "ask",
		Short: "Record asks from and to people outside the factory, and the beads waiting on them",
		Long: "ask keeps the asks on the bead that delivers the work: asked-by:<login> for an inbound\n" +
			"ask, asked-of:<login> for an outbound one, and ask:<role> (tl, skip, drqs) for who that\n" +
			"person is to the Governor, so the history survives a reorg.\n\n" +
			"Once an outbound ask is sent, `mw ask waiting <bead>` labels the bead waiting:others and\n" +
			"notes the time. Needs you then lists it under Waiting on others, with who and since when;\n" +
			"one with no change for three working days (Monday to Friday) becomes a Chase for the\n" +
			"Governor until it moves or closes. `mw status` lists the waiting beads for the Mayor to\n" +
			"recheck their Done-when; `mw ask done <bead>` clears the wait.",
		Args: cobra.NoArgs,
	}
	root.AddCommand(newAskLabelCmd("by", "asked-by", "Record who asked for the bead's work (an inbound ask)", application.Ask.By))
	root.AddCommand(newAskLabelCmd("of", "asked-of", "Record whom the factory asked for something on the bead (an outbound ask)", application.Ask.Of))
	root.AddCommand(newAskWaitingCmd())
	root.AddCommand(newAskDoneCmd())
	return root
}

func newAskLabelCmd(use, label, short string, run func(application.Ask, context.Context, string, string, string) error) *cobra.Command {
	var role string
	cmd := &cobra.Command{
		Use:   use + " <bead> <login> [--role <role>]",
		Short: short,
		Long: short + ": the bead is labelled " + label + ":<login>, and ask:<role> with --role.\n" +
			"A closed bead is refused.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			return run(application.Ask{Tracker: gateway, Notes: gateway, Out: cmd.OutOrStdout()}, cmd.Context(), args[0], args[1], role)
		},
	}
	cmd.Flags().StringVar(&role, "role", "", "who they are to the Governor, e.g. tl, skip, drqs")
	return cmd
}

func newAskWaitingCmd() *cobra.Command {
	var of, role string
	cmd := &cobra.Command{
		Use:   "waiting <bead> [--of <login>] [--role <role>]",
		Short: "Mark a bead waiting on others, from now",
		Long: "waiting labels <bead> waiting:others and notes the time, for Needs you's Waiting on others.\n" +
			"--of also labels it asked-of:<login> (and --role ask:<role>). A bead already waiting keeps\n" +
			"the time it first waited since; a closed bead is refused.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			return application.Ask{Tracker: gateway, Notes: gateway, Out: cmd.OutOrStdout()}.Waiting(cmd.Context(), args[0], of, role)
		},
	}
	cmd.Flags().StringVar(&of, "of", "", "whom the ask was sent to: also labels the bead asked-of:<login>")
	cmd.Flags().StringVar(&role, "role", "", "who they are to the Governor, e.g. tl, skip, drqs")
	return cmd
}

func newAskDoneCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "done <bead>",
		Short: "Clear a bead's wait on others",
		Long: "done takes waiting:others and the noted time off <bead>, which leaves Waiting on others and any\n" +
			"Chase. Who was asked stays on it. A bead that was not waiting is left as it is.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gateway, err := beads.FromConfig()
			if err != nil {
				return err
			}
			return application.Ask{Tracker: gateway, Notes: gateway, Out: cmd.OutOrStdout()}.Done(cmd.Context(), args[0])
		},
	}
}
