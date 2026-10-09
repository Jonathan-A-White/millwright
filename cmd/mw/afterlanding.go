package main

import (
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/userunits"

	"github.com/spf13/cobra"
)

// newAfterLandingCmd builds `mw after-landing <rig>`: the rig's [after_landing]
// command, run now, for a landing whose deploy did not happen (mw-gq6.309).
func newAfterLandingCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "after-landing <rig>",
		Short: "Run a rig's [after_landing] command now, as a landing runs it",
		Long: "after-landing runs the command this host's [after_landing] table names for the rig, in the\n" +
			"rig's checkout as it is now, under the same time limit as a landing's own run\n" +
			"([after_landing_limit]), and prints `after landing: <command>: ok` or the failure, as the\n" +
			"landing's mail does. It exits non-zero when the command fails.\n\n" +
			"It takes the rig's after-landing lock first, the one a landing's deploy holds, so it waits for\n" +
			"a deploy that is running and never runs beside one. A command that fails on a network fault\n" +
			"that may pass (ssh's exit status 255, a name that did not resolve, a connection refused) is\n" +
			"run once more after thirty seconds, and the output says so. It moves nothing: use it after a\n" +
			"landing whose deploy failed or did not happen.",
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
			afterLanding, err := config.AfterLanding()
			if err != nil {
				return err
			}
			afterLimits, err := config.AfterLandingLimits()
			if err != nil {
				return err
			}
			return application.AfterLandingRun{
				AfterLanding: rig.NewAfterLanding(rig.WithAfterCommands(afterLanding), rig.WithAfterLimits(afterLimits)),
				Slot: rig.NewSlots(
					rig.WithSlotSuffix(rig.AfterLandingSlotSuffix),
					rig.WithSlotNotice(func(said string) { cmd.ErrOrStderr().Write([]byte(said)) }),
				),
				Units: userunits.Systemctl{},
				Rigs:  rigs,
				Host:  host,
				Out:   cmd.OutOrStdout(),
			}.Run(cmd.Context(), args[0])
		},
	}
}
