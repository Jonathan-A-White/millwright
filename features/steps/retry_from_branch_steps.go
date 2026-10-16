package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// mw-1ljpo9.7: an attempt refused at close-out or capped hands its branch to
// the next attempt, rebased onto the target. These steps run the real mw next,
// mw retry and dispatch against nextContext's real rig and origin, and read the
// next attempt's worktree and boot file back.

// registerRetryFromBranchSteps registers features/retry_from_branch.feature's
// own steps.
func registerRetryFromBranchSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the config file says next\.retry_from_branch is false$`, c.theConfigTurnsRetryFromBranchOff)
	ctx.Given(`^the other host landed its own version of the work of "([^"]*)" on "([^"]*)"$`, c.theOtherHostLandedAConflictingVersion)

	ctx.When(`^mw retry runs for "([^"]*)"$`, func(id string) error { return c.mwRetryRuns(id, false) })
	ctx.When(`^mw retry --from-main runs for "([^"]*)"$`, func(id string) error { return c.mwRetryRuns(id, true) })

	ctx.Then(`^the worktree of "([^"]*)" holds its first attempt's work, rebased onto "([^"]*)"$`, c.theWorktreeHoldsTheFirstAttemptRebased)
	ctx.Then(`^the worktree of "([^"]*)" holds the committed leftover "([^"]*)", rebased onto "([^"]*)"$`, c.theWorktreeHoldsTheCommittedLeftover)
	ctx.Then(`^the worktree of "([^"]*)" starts clean from "([^"]*)"$`, c.theWorktreeStartsCleanFrom)
	ctx.Then(`^the boot of attempt (\d+) of "([^"]*)" says "([^"]*)"$`, c.theBootOfAttemptSays)
	ctx.Then(`^the boot of attempt (\d+) of "([^"]*)" does not say "([^"]*)"$`, c.theBootOfAttemptDoesNotSay)
}

// theConfigTurnsRetryFromBranchOff writes next.retry_from_branch = false into a
// config file of the scenario's own and reads it back as mw would, moving HOME
// only for as long as the read takes.
func (c *nextContext) theConfigTurnsRetryFromBranchOff() error {
	root, err := c.workspace()
	if err != nil {
		return err
	}
	home := filepath.Join(root, "home")
	dir := filepath.Join(home, ".config", "mw")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[next]\nretry_from_branch = false\n"), 0o644); err != nil {
		return err
	}
	before := os.Getenv("HOME")
	os.Setenv("HOME", home)
	defer os.Setenv("HOME", before)
	on, err := config.NextRetryFromBranch()
	if err != nil {
		return err
	}
	c.fromMain = !on
	return nil
}

// theOtherHostLandedAConflictingVersion pushes, from the other host's clone, a
// file of the same name as the story's own work with other words in it: the
// story's branch cannot be rebased onto that without a conflict.
func (c *nextContext) theOtherHostLandedAConflictingVersion(id, branch string) error {
	if err := os.WriteFile(filepath.Join(c.seed, id+".md"), []byte("the other host's own version\n"), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"add", "-A"}, {"commit", "-qm", "The other host's own version of " + id}, {"push", "-q", "origin", branch},
	} {
		if err := gitRun(c.seed, "git", args...); err != nil {
			return err
		}
	}
	return nil
}

// mwRetryRuns is mw retry on this host once the story's session has ended,
// with --from-main when fromMain says so, and as the config file says.
func (c *nextContext) mwRetryRuns(id string, fromMain bool) error {
	worktrees := rig.New(rig.WithProgram(c.gitProgram))
	report, err := application.Retry{
		Tracker:   c.tracker,
		Worktrees: worktrees,
		Landing:   worktrees,
		Runner:    c.runner,
		Files:     c.files,
		Vault:     vault.New(c.vault),
		Host:      nextHost,
		Rigs:      map[string]string{c.rigKey(): c.rig},
		Notes:     c.tracker,
		FromMain:  fromMain || c.fromMain,
	}.Run(context.Background(), id)
	if err != nil {
		return fmt.Errorf("mw retry %s: %w (%+v)", id, err, report)
	}
	return nil
}

// theWorktreeHoldsTheFirstAttemptRebased checks that the next attempt's
// worktree holds the first attempt's commit, on top of the target as the
// origin has it now.
func (c *nextContext) theWorktreeHoldsTheFirstAttemptRebased(id, branch string) error {
	dir := application.WorktreeDir(c.rig, id)
	held, err := os.ReadFile(filepath.Join(dir, id+".md"))
	if err != nil || !strings.Contains(string(held), "the work of "+id) {
		return fmt.Errorf("expected the worktree of %s to hold the first attempt's work (dispatch said: %v): %v %q", id, c.err, err, held)
	}
	onto := application.StartPoint(application.DefaultRemote, branch)
	if err := gitRun(dir, "git", "merge-base", "--is-ancestor", onto, "HEAD"); err != nil {
		return fmt.Errorf("expected the worktree of %s to be rebased onto %s: %w", id, onto, err)
	}
	ahead, err := gitSay(dir, "rev-list", "--count", onto+"..HEAD")
	if err != nil {
		return err
	}
	if ahead != "1" {
		return fmt.Errorf("expected the first attempt's 1 commit ahead of %s, got %s", onto, ahead)
	}
	return nil
}

// theWorktreeHoldsTheCommittedLeftover checks that a file the first attempt left
// uncommitted is in the next attempt's worktree as a commit ahead of the target
// (mw-gq6.537).
func (c *nextContext) theWorktreeHoldsTheCommittedLeftover(id, file, branch string) error {
	dir := application.WorktreeDir(c.rig, id)
	if _, err := os.Stat(filepath.Join(dir, file)); err != nil {
		return fmt.Errorf("expected the worktree of %s to hold %s (dispatch said: %v): %w", id, file, c.err, err)
	}
	onto := application.StartPoint(application.DefaultRemote, branch)
	tracked, err := gitSay(dir, "diff", "--name-only", onto+"..HEAD")
	if err != nil {
		return err
	}
	if !strings.Contains(tracked, file) {
		return fmt.Errorf("expected %s committed ahead of %s, got:\n%s", file, onto, tracked)
	}
	return nil
}

// theWorktreeStartsCleanFrom checks that the next attempt's worktree is the
// target as the origin has it, with nothing of the first attempt in it.
func (c *nextContext) theWorktreeStartsCleanFrom(id, branch string) error {
	dir := application.WorktreeDir(c.rig, id)
	onto := application.StartPoint(application.DefaultRemote, branch)
	head, err := gitSay(dir, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("reading the worktree of %s (dispatch said: %v): %w", id, c.err, err)
	}
	target, err := gitSay(dir, "rev-parse", onto)
	if err != nil {
		return err
	}
	if head != target {
		log, _ := gitSay(dir, "log", "--oneline", onto+"..HEAD")
		return fmt.Errorf("expected the worktree of %s to start at %s, got commits ahead of it:\n%s", id, onto, log)
	}
	return nil
}

func (c *nextContext) bootOfAttempt(attempt int, id string) (string, error) {
	written, err := os.ReadFile(vault.New(c.vault).RunFile(id, application.BootFileNameForAttempt(attempt)))
	if err != nil {
		return "", fmt.Errorf("reading the boot of attempt %d of %s (dispatch said: %v): %w", attempt, id, c.err, err)
	}
	return string(written), nil
}

func (c *nextContext) theBootOfAttemptSays(attempt int, id, want string) error {
	boot, err := c.bootOfAttempt(attempt, id)
	if err != nil {
		return err
	}
	if !strings.Contains(boot, want) {
		return fmt.Errorf("expected the boot of attempt %d of %s to say %q, got:\n%s", attempt, id, want, boot)
	}
	return nil
}

func (c *nextContext) theBootOfAttemptDoesNotSay(attempt int, id, unwanted string) error {
	boot, err := c.bootOfAttempt(attempt, id)
	if err != nil {
		return err
	}
	if strings.Contains(boot, unwanted) {
		return fmt.Errorf("expected the boot of attempt %d of %s not to say %q, got:\n%s", attempt, id, unwanted, boot)
	}
	return nil
}
