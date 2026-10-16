package steps

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright"
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// mw-gq6.461: what a run writes into the vault and never commits is named by
// the .gitignore mw init lays, so git status of the vault is quiet after a run;
// what mw commits is never ignored. These steps put that .gitignore in the
// real git clone of next_reattempt_steps.go, run what the factory runs, and
// read git's own status back.

// registerVaultLeftoversSteps registers the steps of
// features/vault_leftovers.feature.
func registerVaultLeftoversSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the vault holds the \.gitignore mw init lays$`, c.theVaultHoldsTheTemplatesGitignore)
	ctx.Given(`^what the vault holds so far is committed$`, c.whatTheVaultHoldsIsCommitted)

	ctx.When(`^the session of "([^"]*)" ends as its launch line runs it, its harness reporting success$`, c.theLaunchLineRuns)
	ctx.When(`^the session of "([^"]*)" is killed as its launch line runs it, before its result is moved into place$`, c.theLaunchLineIsKilled)
	ctx.When(`^the mayor's and the millhand's reapers each note a line$`, c.theReapersNote)
	ctx.When(`^the mayor's talk model notes a line$`, c.theTalkModelNotes)
	ctx.When(`^the mayor is handed over$`, c.theMayorIsHandedOver)
	ctx.When(`^the day's key metrics are kept$`, c.theDaysMetricsAreKept)
	ctx.When(`^a todo ranking pass keeps its log$`, c.aTodoRankingKeepsItsLog)

	ctx.Then(`^the run of "([^"]*)" holds its result's temp file$`, c.theRunHoldsItsResultsTempFile)
	ctx.Then(`^git status of the vault is quiet$`, c.gitStatusOfTheVaultIsQuiet)
	ctx.Then(`^the vault ignores each of these:$`, c.theVaultIgnoresEach)
	ctx.Then(`^the vault ignores none of these:$`, c.theVaultIgnoresNone)
}

// theVaultHoldsTheTemplatesGitignore lays the template's .gitignore in the
// vault, as mw init does, before the vault becomes a git clone.
func (c *nextContext) theVaultHoldsTheTemplatesGitignore() error {
	ignore, err := fs.ReadFile(millwright.Template(), ".gitignore")
	if err != nil {
		return fmt.Errorf("reading the template's .gitignore: %w", err)
	}
	return os.WriteFile(filepath.Join(c.vault, ".gitignore"), ignore, 0o644)
}

// whatTheVaultHoldsIsCommitted commits what a scenario's own setup wrote into
// the vault after it became a clone (a rig file), as a real vault holds it.
func (c *nextContext) whatTheVaultHoldsIsCommitted() error {
	if err := gitRun(c.vault, "git", "add", "-A"); err != nil {
		return err
	}
	return gitRun(c.vault, "git", "commit", "-qm", "The scenario's setup")
}

func (c *nextContext) theLaunchLineRuns(id string) error {
	return c.runLaunchLine(id, false)
}

func (c *nextContext) theLaunchLineIsKilled(id string) error {
	return c.runLaunchLine(id, true)
}

// runLaunchLine boots the story's session as mw dispatch does, through the
// real seat boot and the real claude harness, and runs the very command line
// that would be started, in its own directory and environment, with a claude
// and an mw of the scenario's own first on PATH: the claude prints a finished
// session's result (or, killed, kills the shell running the line before the
// result is moved into place); the mw only says it ran, the close-out itself
// being the scenario's own next step.
func (c *nextContext) runLaunchLine(id string, killed bool) error {
	ctx := context.Background()
	detail, err := c.tracker.ShowStory(ctx, id)
	if err != nil {
		return err
	}
	boot := application.SeatBoot{
		Vault: vault.New(c.vault), Harness: claude.New(), Seat: nextSeat, Host: nextHost,
		After: []string{"mw", "next"},
	}
	spec, err := boot.Boot(ctx, detail, application.WorktreeDir(c.rig, id))
	if err != nil {
		return err
	}

	bin := filepath.Join(c.root, "launch-bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		return err
	}
	harness := "#!/bin/sh\nprintf '%s\\n' '" + plainSuccess + "'\n"
	if killed {
		harness += "kill -KILL $PPID\n"
	}
	if err := os.WriteFile(filepath.Join(bin, claude.Program), []byte(harness), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(bin, "mw"), []byte("#!/bin/sh\necho \"mw $*: closed out by the scenario\"\n"), 0o755); err != nil {
		return err
	}

	cmd := exec.Command(spec.Command[0], spec.Command[1:]...)
	cmd.Dir = spec.Dir
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for name, value := range spec.Env {
		cmd.Env = append(cmd.Env, name+"="+value)
	}
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	switch {
	case killed && errors.As(err, &exit):
		return nil
	case killed:
		return fmt.Errorf("expected the launch line to be killed, it ended with %v:\n%s", err, out)
	case err != nil:
		return fmt.Errorf("running the launch line of %s: %v\n%s", id, err, out)
	}
	return nil
}

func (c *nextContext) theRunHoldsItsResultsTempFile(id string) error {
	tmp := vault.New(c.vault).RunFile(id, application.ResultFileName) + ".tmp"
	if _, err := os.Stat(tmp); err != nil {
		return fmt.Errorf("expected the killed session to leave %s: %w", tmp, err)
	}
	return nil
}

func (c *nextContext) theReapersNote() error {
	files := vault.New(c.vault)
	for _, seat := range []string{"mayor", "millhand"} {
		if err := files.Note(context.Background(), seat, "reaped nothing"); err != nil {
			return err
		}
	}
	return nil
}

func (c *nextContext) theTalkModelNotes() error {
	return vault.New(c.vault).NoteTalk(context.Background(), "mayor", "talked")
}

func (c *nextContext) theMayorIsHandedOver() error {
	return vault.New(c.vault).WriteActing(context.Background(), "mayor", "mayor 404\n")
}

func (c *nextContext) theDaysMetricsAreKept() error {
	return vault.New(c.vault).Append(context.Background(), application.MetricSnapshot{Day: "2026-10-16", Landed: 3})
}

func (c *nextContext) aTodoRankingKeepsItsLog() error {
	_, err := vault.New(c.vault).PutRunFile(context.Background(), application.TodoRankRunDir, "2026-10-16.json", "{}\n")
	return err
}

// gitStatusOfTheVaultIsQuiet is git status of the vault, untracked files and
// all, showing nothing: what the scenario's run wrote is committed or ignored.
func (c *nextContext) gitStatusOfTheVaultIsQuiet() error {
	out, err := gitSay(c.vault, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("expected git status of the vault to be quiet, got:\n%s", out)
	}
	return nil
}

func (c *nextContext) theVaultIgnoresEach(table *godog.Table) error {
	return c.vaultIgnores(table, true)
}

func (c *nextContext) theVaultIgnoresNone(table *godog.Table) error {
	return c.vaultIgnores(table, false)
}

// vaultIgnores asks git check-ignore of each path in the table: none of them
// need be there, git reads the .gitignore alone.
func (c *nextContext) vaultIgnores(table *godog.Table, want bool) error {
	var wrong []string
	for _, row := range table.Rows {
		path := strings.TrimSpace(row.Cells[0].Value)
		cmd := exec.Command("git", "check-ignore", "-q", "--no-index", path)
		cmd.Dir = c.vault
		err := cmd.Run()
		var exit *exec.ExitError
		switch {
		case err == nil:
			if !want {
				wrong = append(wrong, path)
			}
		case errors.As(err, &exit) && exit.ExitCode() == 1:
			if want {
				wrong = append(wrong, path)
			}
		default:
			return fmt.Errorf("git check-ignore %s: %w", path, err)
		}
	}
	if len(wrong) > 0 {
		if want {
			return fmt.Errorf("expected the vault to ignore these, it does not: %s", strings.Join(wrong, ", "))
		}
		return fmt.Errorf("expected the vault to ignore none of these, it ignores: %s", strings.Join(wrong, ", "))
	}
	return nil
}
