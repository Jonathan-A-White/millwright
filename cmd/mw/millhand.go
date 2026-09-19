package main

import (
	"fmt"
	"os"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/reaper"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/spf13/cobra"
)

// newMillhandCmd builds `mw millhand`: this host's Millhand, brought up for a
// wake. It is `mw seat up millhand` with the model, the effort, the reaper and
// the kickoff's words settled by the kind of wake, so that a timer has one
// command to run.
func newMillhandCmd() *cobra.Command {
	var wake, reason string
	cmd := &cobra.Command{
		Use:   "millhand [--wake hand|routine|review] [--reason text]",
		Short: "Bring up this host's Millhand for a wake",
		Long: "millhand starts the Millhand's next session on this host, as `mw seat up millhand` does, in a\n" +
			"window of its own that closes itself once the session has handed off (--reap-when-idle).\n\n" +
			"The kind of wake decides the model, and every kind runs at high effort. A wake by hand, the\n" +
			"default, and a routine wake run on config `millhand_routine_model` (sonnet); a review wake runs\n" +
			"on `millhand_review_model` (opus). Either is also read from $" + config.MillhandRoutineModelEnv + " and\n" +
			"$" + config.MillhandReviewModelEnv + ". The kickoff is told the kind of wake and the --reason for it; for a\n" +
			"review wake it is also told what seats/millhand/hosts/<host>/review-since holds, when that file\n" +
			"is there. mw only reads that mark: the Millhand moves it in its handoff.\n\n" +
			"If a window named millhand-* is already open, it starts nothing, says so in one line and leaves\n" +
			"with status " + fmt.Sprint(application.MillhandUpExit) + ", so a timer can tell \"already up\" from a failure.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			host, err := config.Host()
			if err != nil {
				return err
			}
			routine, err := config.MillhandRoutineModel()
			if err != nil {
				return err
			}
			review, err := config.MillhandReviewModel()
			if err != nil {
				return err
			}

			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("finding the mw that is running, to arm a reaper with: %w", err)
			}

			windows := seatWindows()
			_, err = application.Millhand{
				Seats:    vault.New(dir),
				Windows:  windows,
				Harness:  claude.New(),
				Terminal: windows,
				Armer:    reaper.New(exe),

				Host:         host,
				Wake:         application.Wake(wake),
				Reason:       reason,
				RoutineModel: domain.Model(routine),
				ReviewModel:  domain.Model(review),

				Out: cmd.OutOrStdout(),
			}.Run(cmd.Context())
			return err
		},
	}
	cmd.Flags().StringVar(&wake, "wake", string(application.WakeHand), "the kind of wake: hand, routine or review")
	cmd.Flags().StringVar(&reason, "reason", "", "why the Millhand is being woken, told to it after the kind of wake")
	return cmd
}
