package main

import (
	"fmt"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/spf13/cobra"
)

// BuilderSeat is the seat a dispatched session boots into. Every story is
// worked by the Builder; the model and the effort come from the story's path,
// not from the seat.
const BuilderSeat = "builder"

// newDispatchCmd builds `mw dispatch`: the command that turns ready stories
// into running sessions. It is the one command in the factory that spends fuel,
// so everything it does before spending any is reversible, and --dry-run does
// none of it.
func newDispatchCmd() *cobra.Command {
	var dryRun bool
	var capOverride int

	cmd := &cobra.Command{
		Use:   "dispatch",
		Short: "Start a fresh Builder session for each story ready on this host",
		Long: "dispatch brings this host level with the other one, asks beads what is ready here, and for\n" +
			"each story it may take: claims it, cuts a worktree of its rig on branch mw/<story> from the\n" +
			"freshly fetched target branch, pours its formula into step beads, writes the boot file, and\n" +
			"starts the session. It never takes more than the cap allows, never takes a story whose path\n" +
			"names another host or no host at all, and gives the claim back if anything fails before the\n" +
			"session starts.\n\n" +
			"--dry-run prints what it would start and writes nothing: nothing is synced, claimed, cut,\n" +
			"poured or started.",
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
			atOnce, err := config.Cap()
			if err != nil {
				return err
			}
			if capOverride > 0 {
				atOnce = capOverride
			}
			rigs, err := config.Rigs()
			if err != nil {
				return err
			}
			if len(rigs) == 0 {
				return fmt.Errorf("no rig is checked out on %s: add one under [%s] in ~/%s, as `<rig> = \"<directory>\"`",
					host, config.RigsTable, config.File)
			}

			gateway := beads.New(dir)
			files := vault.New(dir)
			report, err := application.Dispatch{
				Tracker:   gateway,
				Worktrees: rig.New(),
				Runner:    tmux.New(),
				Boot:      builderBoot(files, host),
				Sync:      application.Sync{Vault: files, Tracker: gateway, Host: host},
				Host:      host,
				Cap:       atOnce,
				Rigs:      rigs,
				DryRun:    dryRun,
				Out:       cmd.OutOrStdout(),
			}.Run(cmd.Context())
			if err != nil {
				return err
			}
			if len(report.Started) > 0 && !report.DryRun {
				cmd.Printf("Attach to a session with: tmux attach -t =%s\n", report.Started[0].Session)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"print what would be started and write nothing")
	cmd.Flags().IntVar(&capOverride, "cap", 0,
		"how many sessions may run at once, instead of what the config file says")
	return cmd
}
