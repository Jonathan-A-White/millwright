package steps

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// dispatchContext holds everything one dispatch scenario runs against: a real
// rig with a real origin in a temp directory, a vault holding the seat, and
// in-memory stand-ins for the work tracker and the runner. Nothing here reaches
// the factory's own vault, rigs, beads database or terminal.
type dispatchContext struct {
	root    string // holds the origin, the rig checkout and the vault
	vault   string
	rig     string
	seed    string // a second clone, standing in for the other host
	tracker *apptest.FakeTracker
	runner  *apptest.FakeRunner
	files   *apptest.FakeVaultFiles

	lastEpic string
	report   application.DispatchReport
	err      error
}

// The text a scenario's fixtures hold, so that a scenario can say what reached
// the worktree and what reached the boot file.
const (
	dispatchCharter    = "# Builder — charter\n\nYou are the Builder of millwright, and you work one story.\n"
	dispatchLaterFile  = "later.md"
	dispatchLaterWords = "the other host pushed this"
)

// InitializeDispatchScenario registers the steps of features/dispatch.feature.
func InitializeDispatchScenario(ctx *godog.ScenarioContext) {
	c := &dispatchContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = dispatchContext{
			tracker: apptest.NewFakeTracker(),
			runner:  apptest.NewFakeRunner(),
			files:   &apptest.FakeVaultFiles{},
		}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.root != "" {
			_ = os.RemoveAll(c.root)
		}
		return ctx, nil
	})

	ctx.Given(`^a vault holding the charter of the "([^"]*)" seat$`, c.aVaultHoldingTheCharterOf)
	ctx.Given(`^the rig "([^"]*)" is checked out here from its origin$`, c.theRigIsCheckedOutHere)
	ctx.Given(`^the formula "([^"]*)" is installed$`, c.theFormulaIsInstalled)
	ctx.Given(`^an epic "([^"]*)" whose stories are planned with the path:$`, c.anEpicWithTheDefaultPathForDispatch)
	ctx.Given(`^a ready story "([^"]*)" of that epic$`, c.aReadyStoryOfThatEpic)
	ctx.Given(`^a ready story "([^"]*)" of that epic that overrides "([^"]*)" with "([^"]*)"$`, c.aReadyStoryThatOverrides)
	ctx.Given(`^a story "([^"]*)" of that epic is already running here$`, c.aStoryAlreadyRunningHere)
	ctx.Given(`^the other host has pushed a later commit to the rig's origin$`, c.theOtherHostHasPushed)
	ctx.Given(`^the beads sync halts with exit code (\d+)$`, c.theBeadsSyncHalts)
	ctx.Given(`^the runner refuses to start anything$`, c.theRunnerRefuses)

	ctx.When(`^dispatch runs on "([^"]*)" with a cap of (\d+)$`, c.dispatchRuns)
	ctx.When(`^dispatch runs on "([^"]*)" with a cap of (\d+) as a dry run$`, c.dispatchRunsDry)

	ctx.Then(`^one session was started, for "([^"]*)"$`, c.oneSessionWasStartedFor)
	ctx.Then(`^no session was started$`, c.noSessionWasStarted)
	ctx.Then(`^the worktree of "([^"]*)" is a checkout of the rig on branch "([^"]*)"$`, c.theWorktreeIsOnBranch)
	ctx.Then(`^the session for "([^"]*)" runs in the worktree of "([^"]*)"$`, c.theSessionRunsInTheWorktree)
	ctx.Then(`^the story "([^"]*)" is claimed by this host$`, c.theStoryIsClaimedByDispatch)
	ctx.Then(`^the story "([^"]*)" is not claimed$`, c.theStoryIsNotClaimed)
	ctx.Then(`^the story "([^"]*)" is recorded as running$`, c.theStoryIsRecordedAsRunning)
	ctx.Then(`^the worktree of "([^"]*)" holds the later commit$`, c.theWorktreeHoldsTheLaterCommit)
	ctx.Then(`^the work tracker was asked, in this order:$`, c.theTrackerWasAskedInThisOrder)
	ctx.Then(`^dispatch failed, saying: (.+)$`, c.dispatchFailedSaying)
	ctx.Then(`^the story "([^"]*)" carries a comment saying the dispatch failed$`, c.theStoryCarriesAFailureComment)
	ctx.Then(`^there is no worktree for "([^"]*)"$`, c.thereIsNoWorktreeFor)
	ctx.Then(`^the formula "([^"]*)" was poured for "([^"]*)"$`, c.theFormulaWasPouredFor)
	ctx.Then(`^the boot file of "([^"]*)" holds every poured step, in order$`, c.theBootFileHoldsEveryStep)
	ctx.Then(`^nothing was poured$`, c.nothingWasPoured)
	ctx.Then(`^there is no boot file for "([^"]*)"$`, c.thereIsNoBootFileFor)
	ctx.Then(`^dispatch would start "([^"]*)"$`, c.dispatchWouldStart)
}

// workspace makes the temp directory a scenario keeps its vault, its origin and
// its rig in, once.
func (c *dispatchContext) workspace() (string, error) {
	if c.root != "" {
		return c.root, nil
	}
	root, err := os.MkdirTemp("", "mw-dispatch-")
	if err != nil {
		return "", fmt.Errorf("making a workspace: %w", err)
	}
	c.root = root
	return root, nil
}

func (c *dispatchContext) aVaultHoldingTheCharterOf(seat string) error {
	root, err := c.workspace()
	if err != nil {
		return err
	}
	c.vault = filepath.Join(root, "vault")
	path := filepath.Join(c.vault, vault.SeatsDir, seat, vault.CharterFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("making %s: %w", filepath.Dir(path), err)
	}
	return os.WriteFile(path, []byte(dispatchCharter), 0o644)
}

// theRigIsCheckedOutHere makes a bare repository standing in for the rig's
// origin, a clone standing in for the other host, and the checkout this host
// dispatches from. The checkout's worktrees land beside it, as they do on a
// real host.
func (c *dispatchContext) theRigIsCheckedOutHere(name string) error {
	if !rig.Available() {
		return fmt.Errorf("%s is not on PATH", rig.Program)
	}
	root, err := c.workspace()
	if err != nil {
		return err
	}

	origin := filepath.Join(root, "origin.git")
	if err := gitRun(root, "git", "init", "--bare", "-q", "-b", "main", origin); err != nil {
		return err
	}

	c.seed = filepath.Join(root, "other-host")
	if err := gitRun(root, "git", "clone", "-q", origin, c.seed); err != nil {
		return err
	}
	if err := gitIdentify(c.seed); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(c.seed, "README.md"), []byte("# "+name+"\n"), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"add", "-A"}, {"commit", "-qm", "The rig opens"}, {"push", "-q", "-u", "origin", "main"},
	} {
		if err := gitRun(c.seed, "git", args...); err != nil {
			return err
		}
	}

	c.rig = filepath.Join(root, "rigs", name)
	if err := gitRun(root, "git", "clone", "-q", origin, c.rig); err != nil {
		return err
	}
	return gitIdentify(c.rig)
}

// theOtherHostHasPushed puts a commit on the origin that this host's checkout
// has never seen, so that a branch cut without fetching first would miss it.
func (c *dispatchContext) theOtherHostHasPushed() error {
	if err := os.WriteFile(filepath.Join(c.seed, dispatchLaterFile), []byte(dispatchLaterWords+"\n"), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"add", "-A"}, {"commit", "-qm", "A later commit"}, {"push", "-q", "origin", "main"},
	} {
		if err := gitRun(c.seed, "git", args...); err != nil {
			return err
		}
	}
	return nil
}

// theFormulaIsInstalled installs a formula in the fake tracker. Its steps stand
// in for a real formula's: what matters here is that they are poured in order
// and reach the boot file, not what they say.
func (c *dispatchContext) theFormulaIsInstalled(name string) error {
	c.tracker.AddFormula(name,
		application.FormulaStep{Title: "Understand {{story}}", Description: "Read the story and the code it touches."},
		application.FormulaStep{Title: "Write the failing feature first", Description: "Watch it fail for the right reason."},
		application.FormulaStep{Title: "Implement until green", Description: "The smallest code that passes."},
	)
	return nil
}

func (c *dispatchContext) anEpicWithTheDefaultPathForDispatch(id string, table *godog.Table) error {
	var defaults domain.Path
	for _, row := range table.Rows {
		if len(row.Cells) != 2 {
			return fmt.Errorf("default path rows need a field and a value, got %d cells", len(row.Cells))
		}
		if err := defaults.Set(row.Cells[0].Value, row.Cells[1].Value); err != nil {
			return err
		}
	}
	c.tracker.AddEpic(id, defaults)
	c.lastEpic = id
	return nil
}

func (c *dispatchContext) aReadyStoryOfThatEpic(id string) error {
	c.tracker.AddStory(c.lastEpic, domain.Story{ID: id, Title: "The story " + id})
	return nil
}

func (c *dispatchContext) aReadyStoryThatOverrides(id, field, value string) error {
	story := domain.Story{ID: id, Title: "The story " + id}
	if err := story.Overrides.Set(field, value); err != nil {
		return err
	}
	c.tracker.AddStory(c.lastEpic, story)
	return nil
}

func (c *dispatchContext) aStoryAlreadyRunningHere(id string) error {
	if err := c.aReadyStoryOfThatEpic(id); err != nil {
		return err
	}
	return c.tracker.ClaimStory(context.Background(), id)
}

func (c *dispatchContext) theBeadsSyncHalts(code int) error {
	c.tracker.SyncExits(code, "conflict in the working set")
	return nil
}

func (c *dispatchContext) theRunnerRefuses() error {
	c.runner.Err = fmt.Errorf("this runner starts nothing")
	return nil
}

func (c *dispatchContext) dispatchRuns(host string, cap int) error {
	return c.dispatch(host, cap, false)
}

func (c *dispatchContext) dispatchRunsDry(host string, cap int) error {
	return c.dispatch(host, cap, true)
}

// dispatch runs the use case the way mw does, with the real worktrees adapter,
// the real vault and the real Claude Code harness — and with stand-ins for the
// only two things that would reach outside this temp directory: the beads
// database and the terminal a session runs in.
func (c *dispatchContext) dispatch(host string, cap int, dryRun bool) error {
	c.report, c.err = application.Dispatch{
		Tracker:   c.tracker,
		Worktrees: rig.New(),
		Runner:    c.runner,
		Boot: application.SeatBoot{
			Vault:   vault.New(c.vault),
			Harness: claude.New(),
			Seat:    "builder",
			Host:    host,
		},
		Sync:   application.Sync{Vault: c.files, Tracker: c.tracker, Host: host},
		Host:   host,
		Cap:    cap,
		Rigs:   map[string]string{"millwright": c.rig},
		DryRun: dryRun,
	}.Run(context.Background())
	return nil
}

// dispatched is the report the last When produced, or the reason there is none.
func (c *dispatchContext) dispatched() (application.DispatchReport, error) {
	if c.err != nil {
		return application.DispatchReport{}, fmt.Errorf("the dispatch failed: %w", c.err)
	}
	return c.report, nil
}

func (c *dispatchContext) oneSessionWasStartedFor(id string) error {
	report, err := c.dispatched()
	if err != nil {
		return err
	}
	if len(report.Started) != 1 || report.Started[0].StoryID != id {
		return fmt.Errorf("expected one session, for %s, got %+v (and %+v failed)", id, report.Started, report.Failed)
	}
	if names := c.runner.Names(); len(names) != 1 || names[0] != application.SessionName(id) {
		return fmt.Errorf("expected the runner to hold one session named %q, got %q", application.SessionName(id), names)
	}
	return nil
}

func (c *dispatchContext) noSessionWasStarted() error {
	if names := c.runner.Names(); len(names) != 0 {
		return fmt.Errorf("expected no session to have been started, got %q", names)
	}
	// A dry run's report says what it would have started; a real one that
	// started nothing must say so too.
	if c.err == nil && !c.report.DryRun && len(c.report.Started) != 0 {
		return fmt.Errorf("expected the report to say nothing was started, got %+v", c.report.Started)
	}
	return nil
}

func (c *dispatchContext) worktreeOf(id string) string {
	return application.WorktreeDir(c.rig, id)
}

func (c *dispatchContext) theWorktreeIsOnBranch(id, branch string) error {
	dir := c.worktreeOf(id)
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		return fmt.Errorf("expected a checkout of the rig in %s: %w", dir, err)
	}
	on, err := gitSay(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return err
	}
	if on != branch {
		return fmt.Errorf("expected the worktree of %s to be on %q, got %q", id, branch, on)
	}
	// It is a worktree of the rig, not a repository of its own.
	common, err := gitSay(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	if !strings.HasPrefix(common, c.rig) {
		return fmt.Errorf("expected a worktree of %s, got a repository whose git directory is %s", c.rig, common)
	}
	return nil
}

func (c *dispatchContext) theSessionRunsInTheWorktree(id, worktreeOf string) error {
	spec, started := c.runner.Spec(application.SessionName(id))
	if !started {
		return fmt.Errorf("no session was started for %s", id)
	}
	if want := c.worktreeOf(worktreeOf); spec.Dir != want {
		return fmt.Errorf("expected the session of %s to run in %q, got %q", id, want, spec.Dir)
	}
	return nil
}

func (c *dispatchContext) theStoryIsClaimedByDispatch(id string) error {
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return err
	}
	if detail.Assignee == "" || detail.Status != apptest.StatusInProgress {
		return fmt.Errorf("expected %s to be claimed, got status %q assignee %q", id, detail.Status, detail.Assignee)
	}
	return nil
}

func (c *dispatchContext) theStoryIsNotClaimed(id string) error {
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return err
	}
	if detail.Assignee != "" || detail.Status == apptest.StatusInProgress {
		return fmt.Errorf("expected %s to be unclaimed, got status %q assignee %q", id, detail.Status, detail.Assignee)
	}
	return nil
}

func (c *dispatchContext) theStoryIsRecordedAsRunning(id string) error {
	if got := c.tracker.State(id, application.RunState); got != application.RunRunning {
		return fmt.Errorf("expected %s to be recorded %s=%s, got %q", id, application.RunState, application.RunRunning, got)
	}
	return nil
}

func (c *dispatchContext) theWorktreeHoldsTheLaterCommit(id string) error {
	path := filepath.Join(c.worktreeOf(id), dispatchLaterFile)
	held, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("expected the worktree of %s to be cut from the fetched branch: %w", id, err)
	}
	if !strings.Contains(string(held), dispatchLaterWords) {
		return fmt.Errorf("expected %s to hold %q, got %q", path, dispatchLaterWords, held)
	}
	return nil
}

func (c *dispatchContext) theTrackerWasAskedInThisOrder(table *godog.Table) error {
	asked := c.tracker.Asked()
	at := 0
	for _, row := range table.Rows {
		want := row.Cells[0].Value
		found := -1
		for i := at; i < len(asked); i++ {
			if asked[i] == want {
				found = i
				break
			}
		}
		if found < 0 {
			return fmt.Errorf("expected %q to have been asked after the calls before it, got %q", want, asked)
		}
		at = found + 1
	}
	return nil
}

func (c *dispatchContext) dispatchFailedSaying(words string) error {
	if c.err == nil {
		return fmt.Errorf("expected the dispatch to fail saying %q, but it did not fail", words)
	}
	if !strings.Contains(c.err.Error(), strings.TrimSpace(words)) {
		return fmt.Errorf("expected the failure to say %q, got %q", words, c.err)
	}
	return nil
}

func (c *dispatchContext) theStoryCarriesAFailureComment(id string) error {
	comments := c.tracker.Comments(id)
	for _, comment := range comments {
		if strings.Contains(comment, "mw dispatch") && strings.Contains(comment, "claim") {
			return nil
		}
	}
	return fmt.Errorf("expected a comment on %s saying the dispatch failed and the claim was given back, got %q", id, comments)
}

func (c *dispatchContext) thereIsNoWorktreeFor(id string) error {
	dir := c.worktreeOf(id)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("expected no worktree at %s, but there is one", dir)
	}
	return nil
}

func (c *dispatchContext) theFormulaWasPouredFor(formula, id string) error {
	molecule, poured := c.tracker.Poured(id)
	if !poured {
		return fmt.Errorf("expected the formula %s to have been poured for %s, but nothing was", formula, id)
	}
	if molecule.Formula != formula {
		return fmt.Errorf("expected %s to have been poured for %s, got %q", formula, id, molecule.Formula)
	}
	if !molecule.Poured() {
		return fmt.Errorf("expected the molecule of %s to hold steps, got %+v", id, molecule)
	}
	return nil
}

func (c *dispatchContext) theBootFileHoldsEveryStep(id string) error {
	molecule, poured := c.tracker.Poured(id)
	if !poured {
		return fmt.Errorf("nothing was poured for %s", id)
	}
	written, err := os.ReadFile(filepath.Join(c.vault, vault.RunsDir, id, application.BootFileName))
	if err != nil {
		return fmt.Errorf("reading the boot file of %s: %w", id, err)
	}

	boot, at := string(written), 0
	for _, step := range molecule.Steps {
		for _, want := range []string{step.ID, step.Title} {
			found := strings.Index(boot[at:], want)
			if found < 0 {
				if strings.Contains(boot, want) {
					return fmt.Errorf("the boot file holds the step %q, but out of order", want)
				}
				return fmt.Errorf("the boot file of %s does not hold the step %q", id, want)
			}
			at += found + len(want)
		}
	}
	return nil
}

func (c *dispatchContext) nothingWasPoured() error {
	if poured := c.tracker.Molecules(); poured != 0 {
		return fmt.Errorf("expected nothing to have been poured, got %d molecules", poured)
	}
	return nil
}

func (c *dispatchContext) thereIsNoBootFileFor(id string) error {
	path := filepath.Join(c.vault, vault.RunsDir, id, application.BootFileName)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("expected no boot file at %s, but there is one", path)
	}
	return nil
}

func (c *dispatchContext) dispatchWouldStart(id string) error {
	report, err := c.dispatched()
	if err != nil {
		return err
	}
	if !report.DryRun {
		return fmt.Errorf("expected a dry run, got %+v", report)
	}
	for _, started := range report.Started {
		if started.StoryID == id {
			return nil
		}
	}
	return fmt.Errorf("expected the dry run to say it would start %s, got %+v", id, report.Started)
}

// gitRun runs one git command in a directory, for a scenario's fixtures.
func gitRun(dir, program string, args ...string) error {
	cmd := exec.Command(program, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s in %s: %w: %s", program, strings.Join(args, " "), dir, err, out)
	}
	return nil
}

// gitSay runs one git command and returns what it printed, trimmed.
func gitSay(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s in %s: %w", strings.Join(args, " "), dir, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// gitIdentify gives a clone an author, so that committing in it works wherever
// these scenarios run.
func gitIdentify(dir string) error {
	for _, setting := range [][]string{
		{"config", "user.name", "millwright test"},
		{"config", "user.email", "test@millwright.invalid"},
	} {
		if err := gitRun(dir, "git", setting...); err != nil {
			return err
		}
	}
	return nil
}
