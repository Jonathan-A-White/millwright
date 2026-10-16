package main

import (
	"fmt"
	"os"
	"strings"

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
	var repathed string
	var fromMain bool
	cmd := &cobra.Command{
		Use:   "retry <story-id>",
		Short: "Recover a story after a refused landing, so the next dispatch tick tries it again",
		Long: "retry is run on the story's own host once its session has ended — typically after `mw next`\n" +
			"has refused its landing. In order, refusing at the first that does not hold: the session named\n" +
			"on the story must not still be running; a worktree a session left dirty is committed clean;\n" +
			"the branch's commits ahead of the target, if there are any, are bundled into\n" +
			"runs/<story>/attempt-<n>.bundle in the vault and the bundle must verify, and the vault must\n" +
			"accept and push it — a branch with nothing ahead of the target has nothing to bundle, and\n" +
			"that step is skipped. Only then are the worktree\n" +
			"and branch taken away — the worktree never forced, the branch deleted safely and forced only\n" +
			"when that refuses it — and the claim given back with the story set open again, so the next\n" +
			"dispatch tick claims it as a fresh attempt. A refusal changes nothing at all.\n\n" +
			"A story worked on another host has its worktree there, so run from anywhere else, retry reads\n" +
			"no worktree: it gives the claim back, guarded on whoever actually holds it, and leaves that\n" +
			"host's worktree and branch to be bundled by that host's next dispatch. Where a story was worked\n" +
			"is read from its claim (mw@<host>) first, and only without one from the host key of its path,\n" +
			"so changing the host key of a story in progress moves neither its worktree nor its retry.\n\n" +
			"It never resets a story's attempts, and a story already started max_attempts times is refused\n" +
			"with \"attempts exhausted\" rather than retried — resetting the counter by hand is what allows\n" +
			"another attempt.\n\n" +
			"A story its Builder handed back (its newest hand-back comment opens HANDED BACK) is refused,\n" +
			"and mw dispatch passes it over, until a comment opening RE-PATHED: follows it: the same Builder\n" +
			"given the same story hands it back the same way. --repathed \"<what changed>\" writes that\n" +
			"comment, once the retry is past its refusals, and goes on.\n\n" +
			"A story whose last attempt mw next refused at the gate or the merge, or stopped at its turn cap,\n" +
			"keeps its branch (mw-1ljpo9.7): the worktree is taken away, the branch is not, and the next\n" +
			"dispatch cuts the next attempt from it, rebased onto the target, so the fix is a delta. A branch\n" +
			"that does not rebase cleanly falls back to the target, and the boot says so. --from-main deletes\n" +
			"the branch as before, so the next attempt starts from the target; so does\n" +
			"next.retry_from_branch = false in the config file.\n\n" +
			"An attempt keeps its branch when it left work, committed or found in the worktree (which retry\n" +
			"commits), whatever ended it, unless mw next refused it for what its commits hold (a signed\n" +
			"commit, a tracked binary) or --from-main was given; a branch with nothing ahead is deleted.",
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
			fromBranch, err := config.NextRetryFromBranch()
			if err != nil {
				return err
			}

			if cmd.Flags().Changed("repathed") && strings.TrimSpace(repathed) == "" {
				return fmt.Errorf("retrying %s: --repathed needs a sentence on what changed since the Builder handed the story back", args[0])
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
				Notes:       gateway,
				Out:         cmd.OutOrStdout(),
				RePathed:    repathed,
				FromMain:    fromMain || !fromBranch,
			}.Run(cmd.Context(), args[0])
			return err
		},
	}
	cmd.Flags().StringVar(&repathed, "repathed", "",
		"what changed since the Builder handed the story back: written as a RE-PATHED: comment, and lets the retry of a handed-back story go on")
	cmd.Flags().BoolVar(&fromMain, "from-main", false,
		"start the next attempt from the target, deleting the branch, rather than continuing the branch a refused or capped attempt left")
	return cmd
}
