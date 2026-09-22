package main

import (
	"fmt"
	"os"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"

	"github.com/spf13/cobra"
)

// newRetryCmd builds `mw retry <story-id>`: the recovery a refused landing
// used to take a person several hand steps to run, as one command (friction:
// mw-gq6.92).
func newRetryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "retry <story-id>",
		Short: "Recover a story after a refused landing, so the next dispatch tick tries it again",
		Long: "retry is run on the story's own host once its session has ended — typically after `mw next`\n" +
			"has refused its landing. In order, refusing at the first that does not hold: the session named\n" +
			"on the story must not still be running; a worktree a session left dirty is committed clean;\n" +
			"the branch's commits ahead of the target are bundled into runs/<story>/attempt-<n>.bundle in the\n" +
			"vault and the bundle must verify; the vault must accept and push it. Only then are the worktree\n" +
			"and branch taken away — the worktree never forced, the branch deleted safely and forced only\n" +
			"when that refuses it — and the claim given back with the story set open again, so the next\n" +
			"dispatch tick claims it as a fresh attempt. A refusal changes nothing at all.\n\n" +
			"It never resets a story's attempts, and a story already started max_attempts times is refused\n" +
			"with \"attempts exhausted\" rather than retried — resetting the counter by hand is what allows\n" +
			"another attempt.",
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
			maxAttempts, err := config.MaxAttempts()
			if err != nil {
				return err
			}

			// A retry commits and pushes the vault's own files, and it may be run
			// from anywhere: never from inside the worktree it is about to remove.
			if err := os.Chdir(dir); err != nil {
				return fmt.Errorf("retrying %s: the vault %s cannot be worked from: %w", args[0], dir, err)
			}

			gateway := mwGateway(dir, host)
			files := mwVault(dir, host)
			worktrees := rig.New()

			_, err = application.Retry{
				Tracker:     gateway,
				Worktrees:   worktrees,
				Landing:     worktrees,
				Runner:      tmux.New(),
				Files:       files,
				Vault:       files,
				Host:        host,
				Rigs:        rigs,
				MaxAttempts: maxAttempts,
				Out:         cmd.OutOrStdout(),
			}.Run(cmd.Context(), args[0])
			return err
		},
	}
	return cmd
}
