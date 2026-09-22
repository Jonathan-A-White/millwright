package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// mw-gq6.87: a re-dispatched story must never leave a tracked file dirty in
// the vault. These steps put a real git clone behind nextContext's vault (the
// same real Worktrees, Landing, Checks and Slot every next.feature scenario
// already uses), dispatch a story a second time after an earlier attempt's
// boot file and result were committed to it, and read git's own status back —
// nothing else in this suite runs mw next's own commit against real git.

const (
	nextFirstAttemptBoot   = "the boot prompt of the first attempt\n"
	nextFirstAttemptResult = `{"type":"result","subtype":"success","is_error":false,"num_turns":3,` +
		`"duration_ms":10000,"session_id":"s-attempt-1"}` + "\n"
	nextSecondAttemptResult = `{"type":"result","subtype":"success","is_error":false,"num_turns":9,` +
		`"duration_ms":600000,"session_id":"s-attempt-2","total_cost_usd":1.5,` +
		`"usage":{"input_tokens":100,"output_tokens":2000,"cache_read_input_tokens":50000,"cache_creation_input_tokens":3000}}`
)

// registerNextReattemptSteps registers mw-gq6.87's own steps of
// features/next.feature.
func registerNextReattemptSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the vault is a real git clone$`, c.theVaultIsARealGitClone)
	ctx.Given(`^the first attempt of "([^"]*)" already committed its boot file and result to the vault$`, c.theFirstAttemptWasAlreadyCommitted)

	ctx.When(`^mw dispatches "([^"]*)" again$`, c.mwDispatchesAgain)
	ctx.When(`^the session of "([^"]*)" finished its second attempt$`, c.theSessionFinishedItsSecondAttempt)

	ctx.Then(`^one session was started again, for "([^"]*)"$`, c.oneSessionWasStartedAgainFor)
	ctx.Then(`^the vault holds no modified tracked file$`, c.theVaultHoldsNoModifiedTrackedFile)
	ctx.Then(`^the first attempt of "([^"]*)" is unchanged in the vault$`, c.theFirstAttemptsFilesAreUnchanged)
	ctx.Then(`^the vault's last commit holds the result of "([^"]*)"'s second attempt$`, c.theVaultsLastCommitHoldsTheSecondAttemptsResult)
}

// theVaultIsARealGitClone turns the plain directory a factory vault holding a
// charter and a ledger was written to into a real git repository, with what
// is in it already as its first commit — standing in for the vault every host
// really pulls and pushes, so that a scenario using it can read git's own
// status back rather than the in-memory files fake.
func (c *nextContext) theVaultIsARealGitClone() error {
	if err := gitRun(c.vault, "git", "init", "-q", "-b", "main", "."); err != nil {
		return err
	}
	if err := gitIdentify(c.vault); err != nil {
		return err
	}
	if err := gitRun(c.vault, "git", "add", "-A"); err != nil {
		return err
	}
	if err := gitRun(c.vault, "git", "commit", "-qm", "The vault opens"); err != nil {
		return err
	}
	c.realVault = true
	return nil
}

// theFirstAttemptWasAlreadyCommitted leaves the vault as an earlier close-out
// left it: the first attempt's boot file and result written and committed for
// real, and the story recording that one attempt was made.
func (c *nextContext) theFirstAttemptWasAlreadyCommitted(id string) error {
	dir := filepath.Join(c.vault, vault.RunsDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, application.BootFileName), []byte(nextFirstAttemptBoot), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, application.ResultFileName), []byte(nextFirstAttemptResult), 0o644); err != nil {
		return err
	}
	if err := gitRun(c.vault, "git", "add", "-A"); err != nil {
		return err
	}
	if err := gitRun(c.vault, "git", "commit", "-qm", "Close out "+id+": the first attempt"); err != nil {
		return err
	}
	return c.tracker.SetStoryMetadata(context.Background(), id, map[string]string{application.AttemptsField: "1"})
}

// mwDispatchesAgain runs a standalone dispatch the way mw dispatch would run
// it on a host with a session free, against the same real rig and real vault
// the rest of a scenario uses — not the fake used for a plain dispatch.feature
// scenario, because this one has to write into a vault that already holds a
// committed attempt.
func (c *nextContext) mwDispatchesAgain(id string) error {
	worktrees := rig.New(rig.WithProgram(c.gitProgram))
	boot := application.SeatBoot{
		Vault: vault.New(c.vault), Harness: claude.New(), Seat: nextSeat, Host: nextHost,
	}
	report, err := application.Dispatch{
		Tracker:   c.tracker,
		Worktrees: worktrees,
		Runner:    c.runner,
		Memory:    c.tracker,
		Boot:      boot,
		Host:      nextHost,
		Cap:       1,
		Rigs:      map[string]string{"millwright": c.rig},
	}.Run(context.Background())
	c.dispatchReport, c.err = report, err
	return nil
}

func (c *nextContext) oneSessionWasStartedAgainFor(id string) error {
	if len(c.dispatchReport.Started) != 1 || c.dispatchReport.Started[0].StoryID != id {
		return fmt.Errorf("expected %s to have been dispatched again, got %+v (dispatch said: %v)", id, c.dispatchReport, c.err)
	}
	return nil
}

// theVaultHoldsNoModifiedTrackedFile is `git status --short` of the vault
// showing no tracked file changed and not committed — what mw-gq6.87 is
// about: a re-dispatch, or a close-out, that leaves the vault exactly as
// dirty as whoever is meant to commit its own work left it, and no dirtier.
func (c *nextContext) theVaultHoldsNoModifiedTrackedFile() error {
	changed, err := vault.New(c.vault).Uncommitted(context.Background())
	if err != nil {
		return fmt.Errorf("reading what is uncommitted in the vault: %w", err)
	}
	if len(changed) != 0 {
		return fmt.Errorf("expected git status --short of the vault to hold no modified tracked file, got %q", changed)
	}
	return nil
}

func (c *nextContext) theFirstAttemptsFilesAreUnchanged(id string) error {
	dir := filepath.Join(c.vault, vault.RunsDir, id)
	for name, want := range map[string]string{
		application.BootFileName:   nextFirstAttemptBoot,
		application.ResultFileName: nextFirstAttemptResult,
	} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("reading the first attempt's %s: %w", name, err)
		}
		if string(got) != want {
			return fmt.Errorf("expected the first attempt's %s to be untouched, got %q", name, got)
		}
	}
	return nil
}

// theSessionFinishedItsSecondAttempt is what a story's second session leaves
// behind before its own mw next runs: a commit in its worktree, and its
// result at the path this dispatch's boot gave it — not the first attempt's
// path, which is already committed.
func (c *nextContext) theSessionFinishedItsSecondAttempt(id string) error {
	dir := application.WorktreeDir(c.rig, id)
	if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte("the work of "+id+", second attempt\n"), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-qm", "The work of " + id + ", attempt 2"}} {
		if err := gitRun(dir, "git", args...); err != nil {
			return err
		}
	}

	if _, ok := c.runner.Spec(application.SessionName(id)); !ok {
		return fmt.Errorf("no session is running for %s", id)
	}
	result := vault.New(c.vault).RunFile(id, application.ResultFileNameForAttempt(2))
	return os.WriteFile(result, []byte(nextSecondAttemptResult), 0o644)
}

func (c *nextContext) theVaultsLastCommitHoldsTheSecondAttemptsResult(id string) error {
	want := application.RunRecordForAttempt(id, 2)
	out, err := gitSay(c.vault, "show", "--pretty=format:", "--name-only", "HEAD")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == want {
			return nil
		}
	}
	return fmt.Errorf("expected the vault's last commit to hold %q, got %q", want, out)
}
