package main

import (
	"fmt"
	"os"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/spf13/cobra"
)

// newNextCmd builds `mw next`: the command a story's session ends with. The
// dispatch command line chains it after the harness exits, so it runs whether
// the session finished, failed or died, and it is the only place a story is
// landed, ledgered and closed.
func newNextCmd() *cobra.Command {
	var noDispatch bool
	var capOverride int

	cmd := &cobra.Command{
		Use:   "next <story-id>",
		Short: "Close out a finished story, record what it burned, and start what is ready next",
		Long: "next reads what the story's session reported in runs/<story>/result.json. A session that did not\n" +
			"finish is written on the story, marked blocked and left alone: nothing is merged, nothing is closed\n" +
			"and the worktree is kept, because it is the evidence.\n\n" +
			"A session that did finish is checked before anything is landed: the branch must hold commits, every\n" +
			"step of its formula must be closed, and the rig's own tests must pass in the worktree. Then, under\n" +
			"the rig's merge slot, the branch is merged into the target branch as the remote has it — and if that\n" +
			"was not a fast-forward the tests are run again on the merged result, because nothing has ever tested\n" +
			"that combination. The push is never forced; a push the remote refuses is fetched and tried again a\n" +
			"few times. Then the worktree goes, one line is appended to the seat's ledger, the story is closed,\n" +
			"the hosts are brought level, and whatever is ready here is dispatched.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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
			tests, err := config.Tests()
			if err != nil {
				return err
			}

			// A close-out takes away the very worktree its session was running
			// in, and this process is standing in it: the shell that chained mw
			// next onto the harness inherited the session's directory. Anything
			// run after the removal — bd, tmux, the dispatch that follows —
			// would be started from a directory that no longer exists, and on
			// Linux that fails before the program is even reached. So the
			// close-out works from the vault, which is never the thing it takes
			// away.
			if err := os.Chdir(dir); err != nil {
				return fmt.Errorf("closing out %s: the vault %s cannot be worked from: %w", args[0], dir, err)
			}

			gateway := beads.New(dir)
			files := vault.New(dir)
			worktrees := rig.New()
			runner := tmux.New()
			sync := application.Sync{Vault: files, Tracker: gateway, Host: host}

			// The dispatch that follows does not sync for itself: the close-out
			// syncs once, after the story is closed, so that the other host sees
			// a closed story rather than a claimed one.
			var dispatcher application.Dispatcher
			if !noDispatch {
				dispatcher = application.Dispatch{
					Tracker:   gateway,
					Worktrees: worktrees,
					Runner:    runner,
					Boot:      builderBoot(files, host),
					Host:      host,
					Cap:       atOnce,
					Rigs:      rigs,
					Out:       cmd.OutOrStdout(),
				}
			}

			_, err = application.Next{
				Tracker:   gateway,
				Worktrees: worktrees,
				Landing:   worktrees,
				Checks:    rig.NewChecks(rig.WithCommands(tests)),
				Slot:      rig.NewSlots(),
				Vault:     files,
				Runner:    runner,
				Sync:      sync,
				Dispatch:  dispatcher,
				Seat:      BuilderSeat,
				Host:      host,
				Rigs:      rigs,
				Out:       cmd.OutOrStdout(),
			}.Run(cmd.Context(), args[0])
			return err
		},
	}

	cmd.Flags().BoolVar(&noDispatch, "no-dispatch", false,
		"close the story out and stop, without starting whatever is ready next")
	cmd.Flags().IntVar(&capOverride, "cap", 0,
		"how many sessions may run at once when dispatching, instead of what the config file says")
	return cmd
}

// builderBoot is how every dispatched session is assembled: into the Builder
// seat, on this host, with this same mw chained on after the harness exits.
func builderBoot(files *vault.Vault, host string) application.SeatBoot {
	return application.SeatBoot{
		Vault:   files,
		Harness: claude.New(),
		Seat:    BuilderSeat,
		Host:    host,
		After:   afterSession(),
	}
}

// afterSession is the command a dispatched session ends with: this same mw,
// closing the story out. It is found by asking the running program where it is,
// so that a mw run from a worktree chains that mw rather than whatever is on
// PATH — and a mw that cannot say where it is falls back to the name, which is
// what a host with mw installed has anyway.
func afterSession() []string {
	program, err := os.Executable()
	if err != nil || program == "" {
		program = "mw"
	}
	return []string{program, "next"}
}
