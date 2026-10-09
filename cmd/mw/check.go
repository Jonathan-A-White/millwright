package main

import (
	"fmt"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"

	"github.com/spf13/cobra"
)

// newCheckCmd builds `mw check`: the checks mw next makes before it lands a
// story, run on a story's branch by the session working it. It is read-only —
// it writes nothing to the tracker, the ledger, the vault or git — and it exits
// non-zero when any check fails.
func newCheckCmd() *cobra.Command {
	var noSlot bool
	cmd := &cobra.Command{
		Use:   "check <story-id>",
		Short: "Run the checks mw next makes before landing a story, on its branch, and change nothing",
		Long: "check runs what mw next would refuse a story's branch for, while the session that is working the\n" +
			"story can still do something about it: the branch must hold commits, none of those commits may be\n" +
			"signed by a machine, every step of the story's formula must be closed, and the rig's own tests\n" +
			"must pass in the story's worktree. Each refusal is printed as mw next would word it, all of them\n" +
			"at once rather than the first only, and check exits non-zero when there is any.\n\n" +
			"check is read-only: it writes no comment, run state or ledger line, commits nothing in the vault,\n" +
			"and does not fetch, merge or push. The one thing it takes is the rig's merge slot, around the\n" +
			"rig's tests, so that a close-out's tests and a check's on one rig do not run at once and starve\n" +
			"each other: it waits for the slot, saying who holds it, and --no-slot skips the wait. It does not read the session's result, which is not written\n" +
			"until the session ends, and it does not try the merge, so a branch that passes here can still be\n" +
			"stopped by a conflict or by the other host's work. The commits are counted against the target\n" +
			"branch as the rig last saw the remote's.\n\n" +
			"The formula's last step is normally still open when a session runs check, so a session runs it\n" +
			"after committing and closes that step once nothing else is refused.",
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
			rigs, err := config.Rigs()
			if err != nil {
				return err
			}
			tests, err := config.Tests()
			if err != nil {
				return err
			}
			nice, err := config.BuilderNice()
			if err != nil {
				return err
			}

			var slot application.MergeSlot
			if !noSlot {
				slot = rig.NewSlots(rig.WithSlotNotice(func(said string) { fmt.Fprint(cmd.ErrOrStderr(), said) }))
			}
			worktrees := rig.New()
			_, err = application.Check{
				Tracker: mwGateway(dir, host),
				Landing: worktrees,
				Checks:  rig.NewChecks(rig.WithCommands(tests), rig.WithNice(nice)),
				Slot:    slot,
				Host:    host,
				Rigs:    rigs,
				Out:     cmd.OutOrStdout(),
			}.Run(cmd.Context(), args[0])
			return err
		},
	}
	cmd.Flags().BoolVar(&noSlot, "no-slot", false, "run the rig's tests without taking the rig's merge slot, so without waiting for a close-out's gate")
	return cmd
}
