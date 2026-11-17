package main

import (
	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/peekremote"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// newNamedHereCmd builds `mw named-here <rig> <ref> <test>...`, the far end of
// mw next asking the home to run the tests main fails on a cloud box by name
// (mw-gq6.1395): it runs them in a throwaway worktree of ref and prints how they
// came out. It is not for a person.
func newNamedHereCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "named-here <rig> <ref> <test>...",
		Hidden: true,
		Short:  "Run tests by name in a throwaway worktree of ref, for another host (what mw next asks over ssh)",
		Args:   cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := config.Vault()
			if err != nil {
				return err
			}
			rigs, err := config.Rigs()
			if err != nil {
				return err
			}
			checks, err := landingChecks(dir, gateCommands{})
			if err != nil {
				return err
			}
			worktrees := rig.New()
			return application.NamedHere{
				Worktrees:   worktrees,
				Landing:     worktrees,
				NamedChecks: checks,
				Rigs:        rigs,
				Out:         cmd.OutOrStdout(),
			}.Run(cmd.Context(), args[0], args[1], args[2:])
		},
	}
	return cmd
}

// namedElsewhere is how mw next asks another host to run the failing tests by
// name: over the ssh line [hands_hosts] gives it. Nil when the config cannot be
// read, which asks no one.
func namedElsewhere() application.NamedElsewhere {
	hands, err := config.HandsHosts()
	if err != nil {
		return nil
	}
	return peekremote.Remote{Reach: hands}
}
