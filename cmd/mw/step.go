package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// stepGuardExit leaves with the status Claude Code reads as "keep going": a
// Stop hook that exits 2 holds the session back and gives it what the hook
// wrote on standard error. A test swaps it.
var stepGuardExit = os.Exit

// newStepCmd builds `mw step`: the two commands that keep a Builder's session
// from ending with a formula step open (mw-gq6.1676).
func newStepCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "step",
		Short: "Close-out guard: a Builder cannot end with a formula step open (guard, governor)",
		Args:  cobra.NoArgs,
	}
	root.AddCommand(newStepGuardCmd())
	root.AddCommand(newStepGovernorCmd())
	return root
}

// stepGuardSession is the command a Builder's Stop hook runs, with the story's
// id after it: this same mw, `mw step guard`. Nil when the host's
// closeout.open_steps_guard is false. A config that cannot be read leaves the
// guard on: it is a help, and a bad value is the config's to be told of.
func stepGuardSession() []string {
	if on, err := config.OpenStepsGuard(); err == nil && !on {
		return nil
	}
	return append(mwProgram(), "step", "guard")
}

func newStepGuardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "guard <story>",
		Short: "The Stop hook of a Builder's session: list the story's open formula steps and keep the session going",
		Long: "guard is what Claude Code runs when a Builder's session is about to end (a Stop hook mw\n" +
			"installs in the session's --settings; closeout.open_steps_guard = false in the config\n" +
			"installs none). It reads the hook's JSON on standard input, lists the story's open formula\n" +
			"steps and, when any is neither closed nor marked the Governor's (mw step governor), writes\n" +
			"them on standard error, each by id and title, and exits 2: the session goes on and reads\n" +
			"them. It holds one session back at most 3 times, counted in runs/<story>/ of the vault,\n" +
			"and then exits 0, and the close-out refuses the open steps as it always did. A step list\n" +
			"it cannot read is said on standard error and does not hold the session.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gateway, _, err := posternGateway()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "mw step guard: not run, the session is let end: %v\n", err)
				return nil
			}
			dir, err := config.Vault()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "mw step guard: not run, the session is let end: %v\n", err)
				return nil
			}
			host, err := config.Host()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "mw step guard: not run, the session is let end: %v\n", err)
				return nil
			}
			blocked, err := application.OpenStepsGuard{
				Tracker: gateway, Notes: gateway, Records: mwVault(dir, host), Out: cmd.ErrOrStderr(), Vault: dir,
			}.Run(cmd.Context(), args[0], cmd.InOrStdin())
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "mw step guard: not run, the session is let end: %v\n", err)
				return nil
			}
			if blocked {
				stepGuardExit(2)
			}
			return nil
		},
	}
}

func newStepGovernorCmd() *cobra.Command {
	var why string
	cmd := &cobra.Command{
		Use:   "governor <story> <step> --why <reason>",
		Short: "Mark an open formula step of a story as the Governor's, with the reason only he can do it",
		Long: "governor is how a Builder leaves a formula step open on purpose: one only the Governor's own\n" +
			"hands can do. It keeps the step and the reason, which lets the close-out guard (mw step\n" +
			"guard) pass the step. The step is not closed: mw next still refuses the story's open steps,\n" +
			"counts that refusal as hands waiting on him in mw report, not as steps left open, and names\n" +
			"the step and the reason in its mail to the Mayor. The step must be an open step of the\n" +
			"story's formula.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			gateway, _, err := posternGateway()
			if err != nil {
				return err
			}
			marked, err := application.GovernorsStepMark{Tracker: gateway, Notes: gateway, Now: time.Now}.Run(cmd.Context(), args[0], args[1], why)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s of %s is marked the Governor's: %s\n", marked.Step, marked.Story, marked.Reason)
			return nil
		},
	}
	cmd.Flags().StringVar(&why, "why", "", "the reason only the Governor's hands can do the step (required)")
	_ = cmd.MarkFlagRequired("why")
	return cmd
}
