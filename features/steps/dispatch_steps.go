package steps

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/ticklog"
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
	mail    *apptest.FakeMailbox

	// out is what the dispatch printed, and waits is every wait it asked for,
	// which a scenario never really sits through.
	out   bytes.Buffer
	waits []time.Duration
	// maxAttempts is the cap on attempts a scenario's config file gave, when it
	// gave one; otherwise the dispatch is run with the default the config would.
	maxAttempts int
	// configured is the sync knobs a scenario's config file gave, when it gave
	// them; otherwise the dispatch is run with the defaults the config would.
	configured *syncKnobs
	lastEpic   string
	// earlier is the molecule a scenario poured for a story before the dispatch
	// ran, so that it can say whether the dispatch reused it or poured another.
	earlier application.Molecule
	report  application.DispatchReport
	err     error
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
			mail:    apptest.NewFakeMailbox(),
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
	ctx.Given(`^a ready story "([^"]*)" of that epic at priority (\d+), filed at "([^"]*)"$`, c.aReadyStoryAtPriority)
	ctx.Given(`^a ready story "([^"]*)" of that epic labelled "([^"]*)"$`, c.aReadyStoryLabelled)
	ctx.Given(`^a story "([^"]*)" of that epic is already running here$`, c.aStoryAlreadyRunningHere)
	ctx.Given(`^a story "([^"]*)" of that epic labelled "([^"]*)" is already running here$`, c.aLabelledStoryAlreadyRunningHere)
	ctx.Given(`^the story "([^"]*)" waits on "([^"]*)"$`, c.theStoryWaitsOn)
	ctx.Given(`^the story "([^"]*)" is closed$`, c.theStoryIsClosedGiven)
	ctx.Given(`^the other host has pushed a later commit to the rig's origin$`, c.theOtherHostHasPushed)
	ctx.Given(`^the beads sync halts with exit code (\d+)$`, c.theBeadsSyncHalts)
	ctx.Given(`^the sync cannot resolve a name for its first (\d+) tries$`, c.theSyncCannotResolveForItsFirstTries)
	ctx.Given(`^the sync can never resolve a name$`, c.theSyncCanNeverResolve)
	ctx.Given(`^the vault's pull is refused with "([^"]*)"$`, c.theVaultsPullIsRefused)
	ctx.Given(`^the config file says dispatch_sync_tries is (\d+) and dispatch_sync_wait is "([^"]*)"$`, c.theConfigSaysTheSyncKnobs)
	ctx.Given(`^the config file says max_attempts is (\d+)$`, c.theConfigSaysMaxAttempts)
	ctx.Given(`^the story "([^"]*)" has been tried (\d+) times?$`, c.theStoryHasBeenTried)
	ctx.Given(`^the story "([^"]*)" carries the comment "([^"]*)"$`, c.theStoryCarriesTheComment)
	ctx.Given(`^the runner refuses to start anything$`, c.theRunnerRefuses)
	ctx.Given(`^an earlier session for "([^"]*)" lies dead$`, c.anEarlierSessionLiesDead)
	ctx.Given(`^a session for "([^"]*)" is still running in its worktree$`, c.aSessionIsRunningInItsWorktree)
	ctx.Given(`^the story "([^"]*)" has the formula "([^"]*)" poured and recorded, with its first step closed$`, c.theStoryHasAMoleculeWithItsFirstStepClosed)
	ctx.Given(`^the story "([^"]*)" has the formula "([^"]*)" poured and recorded, with its molecule closed$`, c.theStoryHasAMoleculeClosed)
	ctx.Given(`^the story "([^"]*)" has the formula "([^"]*)" poured and recorded, with every step closed$`, c.theStoryHasAMoleculeWithEveryStepClosed)
	ctx.Given(`^the story "([^"]*)" records a molecule that does not exist$`, c.theStoryRecordsAMissingMolecule)

	ctx.When(`^dispatch runs on "([^"]*)" with a cap of (\d+)$`, c.dispatchRuns)
	ctx.When(`^dispatch runs on "([^"]*)" with a cap of (\d+) as a dry run$`, c.dispatchRunsDry)
	ctx.When(`^the story "([^"]*)" is given back once its session has ended$`, c.theStoryIsGivenBack)
	ctx.When(`^the counter of "([^"]*)" is reset by hand$`, c.theCounterIsResetByHand)
	ctx.When(`^the counter of "([^"]*)" is reset by hand to (\d+)$`, c.theCounterIsResetByHandTo)

	ctx.Then(`^one session was started, for "([^"]*)"$`, c.oneSessionWasStartedFor)
	ctx.Then(`^no session was started$`, c.noSessionWasStarted)
	ctx.Then(`^the dead session for "([^"]*)" was closed$`, c.theDeadSessionWasClosed)
	ctx.Then(`^nothing was closed$`, c.nothingWasClosed)
	ctx.Then(`^the session for "([^"]*)" is running$`, c.theSessionIsRunning)
	ctx.Then(`^the session for "([^"]*)" is still running in the worktree it began in$`, c.theSessionStillRunsWhereItBegan)
	ctx.Then(`^the worktree of "([^"]*)" is a checkout of the rig on branch "([^"]*)"$`, c.theWorktreeIsOnBranch)
	ctx.Then(`^the session for "([^"]*)" runs in the worktree of "([^"]*)"$`, c.theSessionRunsInTheWorktree)
	ctx.Then(`^the story "([^"]*)" is claimed by this host$`, c.theStoryIsClaimedByDispatch)
	ctx.Then(`^the story "([^"]*)" is not claimed$`, c.theStoryIsNotClaimed)
	ctx.Then(`^the story "([^"]*)" is recorded as running$`, c.theStoryIsRecordedAsRunning)
	ctx.Then(`^the worktree of "([^"]*)" holds the later commit$`, c.theWorktreeHoldsTheLaterCommit)
	ctx.Then(`^the work tracker was asked, in this order:$`, c.theTrackerWasAskedInThisOrder)
	ctx.Then(`^dispatch failed, saying: (.+)$`, c.dispatchFailedSaying)
	ctx.Then(`^the sync was tried (\d+) times?, waiting (\d+)s between the tries$`, c.theSyncWasTriedWaiting)
	ctx.Then(`^the sync was tried (\d+) times?, waiting nothing$`, c.theSyncWasTriedWaitingNothing)
	ctx.Then(`^the dispatch report says the sync was retried (\d+) times$`, c.theReportSaysTheSyncWasRetried)
	ctx.Then(`^dispatch printed the one line "([^"]*)"$`, c.dispatchPrintedTheOneLine)
	ctx.Then(`^dispatch leaves with status (\d+)$`, c.dispatchLeavesWithStatus)
	ctx.Then(`^the dispatch log holds exactly one line: "([^"]*)"$`, c.theDispatchLogHoldsExactly)
	ctx.Then(`^the dispatch log holds exactly one line that begins "([^"]*)" and says "([^"]*)"$`, c.theDispatchLogHoldsALineSaying)
	ctx.Then(`^the dispatch log holds (\d+) lines?$`, c.theDispatchLogHoldsLines)
	ctx.Then(`^the story "([^"]*)" carries a comment saying the dispatch failed$`, c.theStoryCarriesAFailureComment)
	ctx.Then(`^there is no worktree for "([^"]*)"$`, c.thereIsNoWorktreeFor)
	ctx.Then(`^the formula "([^"]*)" was poured for "([^"]*)"$`, c.theFormulaWasPouredFor)
	ctx.Then(`^the boot file of "([^"]*)" holds every poured step, in order$`, c.theBootFileHoldsEveryStep)
	ctx.Then(`^nothing was poured$`, c.nothingWasPoured)
	ctx.Then(`^nothing more was poured$`, c.nothingMoreWasPoured)
	ctx.Then(`^the formula "([^"]*)" was poured again for "([^"]*)"$`, c.theFormulaWasPouredAgainFor)
	ctx.Then(`^the story "([^"]*)" still records its first molecule$`, c.theStoryStillRecordsItsFirstMolecule)
	ctx.Then(`^the story "([^"]*)" records the molecule it was poured as$`, c.theStoryRecordsTheMoleculeItWasPouredAs)
	ctx.Then(`^the boot file of "([^"]*)" holds only the steps still open, in order$`, c.theBootFileHoldsOnlyTheOpenSteps)
	ctx.Then(`^there is no boot file for "([^"]*)"$`, c.thereIsNoBootFileFor)
	ctx.Then(`^dispatch would start "([^"]*)"$`, c.dispatchWouldStart)
	ctx.Then(`^dispatch would start, in this order:$`, c.dispatchWouldStartInOrder)
	ctx.Then(`^the dry run report lists them in that order$`, c.theReportListsThemInOrder)
	ctx.Then(`^dispatch passed over "([^"]*)", saying: (.+)$`, c.dispatchPassedOver)
	ctx.Then(`^the dry run report says (\d+) of (\d+) sessions were already running$`, c.theReportSaysHowManyWereRunning)
	ctx.Then(`^the story "([^"]*)" records (\d+) attempts?$`, c.theStoryRecordsAttempts)
	ctx.Then(`^the story "([^"]*)" is recorded as blocked for the reason "([^"]*)"$`, c.theStoryIsRecordedAsBlockedFor)
	ctx.Then(`^the story "([^"]*)" is not marked blocked$`, c.theStoryIsNotRecordedAsBlocked)
	ctx.Then(`^the story "([^"]*)" carries exactly one comment saying it used up its attempts$`, c.theStoryCarriesOneExhaustedComment)
	ctx.Then(`^the story "([^"]*)" carries (\d+) comments saying it used up its attempts$`, c.theStoryCarriesExhaustedComments)
	ctx.Then(`^the story "([^"]*)" carries no comment saying it used up its attempts$`, c.theStoryCarriesNoExhaustedComment)
	ctx.Then(`^the story "([^"]*)" carries exactly one comment saying its formula is not installed$`, c.theStoryCarriesOneFormulaNotInstalledComment)
	ctx.Then(`^the Mayor has (\d+) mails?$`, c.theMayorHasMails)
	ctx.Then(`^the mail to the Mayor says "([^"]*)"$`, c.theMailSays)
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

// aReadyStoryAtPriority adds a ready story with the priority and the filing time
// a scenario names. Scenarios add the stories in the order dispatch must not
// start them in, so that the order they were added in cannot pass for the right
// one.
func (c *dispatchContext) aReadyStoryAtPriority(id string, priority int, filed string) error {
	created, err := time.Parse(time.RFC3339, filed)
	if err != nil {
		return fmt.Errorf("%q is not a time: %w", filed, err)
	}
	if err := c.aReadyStoryOfThatEpic(id); err != nil {
		return err
	}
	if err := c.tracker.SetPriority(id, priority); err != nil {
		return err
	}
	return c.tracker.SetCreated(id, created)
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

// aReadyStoryLabelled adds a ready story carrying a label.
func (c *dispatchContext) aReadyStoryLabelled(id, label string) error {
	if err := c.aReadyStoryOfThatEpic(id); err != nil {
		return err
	}
	return c.tracker.SetLabels(id, label)
}

func (c *dispatchContext) aLabelledStoryAlreadyRunningHere(id, label string) error {
	if err := c.aReadyStoryLabelled(id, label); err != nil {
		return err
	}
	return c.tracker.ClaimStory(context.Background(), id)
}

// theStoryWaitsOn gives a story a dependency, as if bd's ready set had not
// caught up with it yet (mw-gq6.93): the fake still offers the story ready,
// carrying the wait for Dispatch's own guard to find.
func (c *dispatchContext) theStoryWaitsOn(id, need string) error {
	c.tracker.Needs(id, need)
	return nil
}

// theStoryIsClosedGiven closes a story named in a Given, the way a blocker is
// finished before the story waiting on it is dispatched.
func (c *dispatchContext) theStoryIsClosedGiven(id string) error {
	return c.tracker.CloseStory(context.Background(), id, "worked by the test")
}

func (c *dispatchContext) theBeadsSyncHalts(code int) error {
	c.tracker.SyncExits(code, "conflict in the working set")
	return nil
}

// resolveFailure is what the vault's adapter hands back when git could not
// resolve the remote's name: the typed error, with git's own words under it.
func resolveFailure() error {
	said := "ssh: Could not resolve hostname github.com: Temporary failure in name resolution"
	return &application.NameNotResolved{Said: said, Err: fmt.Errorf("git pull --rebase in /vault: exit status 128: %s", said)}
}

func (c *dispatchContext) theSyncCannotResolveForItsFirstTries(tries int) error {
	c.files.PullErr, c.files.PullErrFor = resolveFailure(), tries
	return nil
}

func (c *dispatchContext) theSyncCanNeverResolve() error {
	c.files.PullErr, c.files.PullErrFor = resolveFailure(), 0
	return nil
}

func (c *dispatchContext) theVaultsPullIsRefused(said string) error {
	c.files.PullErr = fmt.Errorf("git pull --rebase in /vault: exit status 128: %s", said)
	return nil
}

// syncKnobs are what the config gives dispatch for its sync.
type syncKnobs struct {
	tries int
	wait  time.Duration
}

// theConfigSaysTheSyncKnobs writes a config file under a home directory of the
// scenario's own and reads the knobs back out of it the way mw does, so that a
// scenario proves they come from the config and not from the step.
func (c *dispatchContext) theConfigSaysTheSyncKnobs(tries int, wait string) error {
	root, err := c.workspace()
	if err != nil {
		return err
	}
	home := filepath.Join(root, "home")
	dir := filepath.Join(home, ".config", "mw")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	file := fmt.Sprintf("dispatch_sync_tries = %d\ndispatch_sync_wait = %q\n", tries, wait)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(file), 0o644); err != nil {
		return err
	}
	// HOME is the process's, and other scenarios read it: it is moved only for as
	// long as it takes to read the config, and put back before the step returns.
	before := os.Getenv("HOME")
	os.Setenv("HOME", home)
	defer os.Setenv("HOME", before)

	for _, env := range []string{config.DispatchSyncTriesEnv, config.DispatchSyncWaitEnv} {
		if os.Getenv(env) != "" {
			return fmt.Errorf("%s is set in the environment, and would answer ahead of the config file", env)
		}
	}
	knobs := &syncKnobs{}
	if knobs.tries, err = config.DispatchSyncTries(); err != nil {
		return err
	}
	if knobs.wait, err = config.DispatchSyncWait(); err != nil {
		return err
	}
	c.configured = knobs
	return nil
}

func (c *dispatchContext) theRunnerRefuses() error {
	c.runner.Err = fmt.Errorf("this runner starts nothing")
	return nil
}

// anEarlierSessionLiesDead leaves what a finished session leaves under mw: a dead
// pane still holding the story's session name, in a directory that is not the
// worktree a new dispatch will cut.
func (c *dispatchContext) anEarlierSessionLiesDead(id string) error {
	name := application.SessionName(id)
	spec := application.SessionSpec{Name: name, Dir: filepath.Join(c.root, "earlier-run"), Command: []string{"claude"}}
	if err := c.runner.Start(context.Background(), spec); err != nil {
		return err
	}
	c.runner.Exit(name, 1)
	return nil
}

// aSessionIsRunningInItsWorktree is a story that is being worked: its worktree
// is there, on its branch, and a session that has not exited is running in it.
func (c *dispatchContext) aSessionIsRunningInItsWorktree(id string) error {
	dir, branch := c.worktreeOf(id), application.StoryBranch(id)
	if err := rig.New().Add(context.Background(), c.rig, dir, branch, application.StartPoint(application.DefaultRemote, "main")); err != nil {
		return fmt.Errorf("cutting the worktree of %s: %w", id, err)
	}
	return c.runner.Start(context.Background(), application.SessionSpec{
		Name: application.SessionName(id), Dir: dir, Command: []string{"claude"},
	})
}

// pourAndRecord pours a formula for a story and records the molecule on it, as
// an earlier dispatch that failed after pouring would have left it.
func (c *dispatchContext) pourAndRecord(id, formula string) (application.Molecule, error) {
	ctx := context.Background()
	molecule, err := c.tracker.PourFormula(ctx, formula, id, "The story "+id)
	if err != nil {
		return application.Molecule{}, err
	}
	if err := c.tracker.SetStoryMetadata(ctx, id, map[string]string{application.MoleculeField: molecule.RootID}); err != nil {
		return application.Molecule{}, err
	}
	c.earlier = molecule
	return molecule, nil
}

func (c *dispatchContext) theStoryHasAMoleculeWithItsFirstStepClosed(id, formula string) error {
	molecule, err := c.pourAndRecord(id, formula)
	if err != nil {
		return err
	}
	c.tracker.CloseStep(molecule.Steps[0].ID)
	return nil
}

func (c *dispatchContext) theStoryHasAMoleculeClosed(id, formula string) error {
	molecule, err := c.pourAndRecord(id, formula)
	if err != nil {
		return err
	}
	c.tracker.CloseMolecule(molecule.RootID)
	return nil
}

func (c *dispatchContext) theStoryHasAMoleculeWithEveryStepClosed(id, formula string) error {
	molecule, err := c.pourAndRecord(id, formula)
	if err != nil {
		return err
	}
	for _, step := range molecule.Steps {
		c.tracker.CloseStep(step.ID)
	}
	return nil
}

func (c *dispatchContext) theStoryRecordsAMissingMolecule(id string) error {
	return c.tracker.SetStoryMetadata(context.Background(), id, map[string]string{application.MoleculeField: "f-mol-gone"})
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
	// Without a config file of its own a scenario runs with what the config would
	// say if it said nothing: 3 tries, 15 seconds apart. Nothing sleeps: the wait
	// is written down instead.
	knobs := syncKnobs{tries: config.DefaultDispatchSyncTries, wait: config.DefaultDispatchSyncWait}
	if c.configured != nil {
		knobs = *c.configured
	}
	c.report, c.err = application.Dispatch{
		Tracker:   c.tracker,
		Worktrees: rig.New(),
		Runner:    c.runner,
		Memory:    c.tracker,
		Boot: application.SeatBoot{
			Vault:   vault.New(c.vault),
			Harness: claude.New(),
			Seat:    "builder",
			Host:    host,
		},
		Sync:      application.Sync{Vault: c.files, Tracker: c.tracker, Host: host},
		SyncTries: knobs.tries,
		SyncWait:  knobs.wait,
		Wait: func(_ context.Context, d time.Duration) error {
			c.waits = append(c.waits, d)
			return nil
		},
		Host:        host,
		Cap:         cap,
		MaxAttempts: c.attempts(),
		Mailbox:     c.mail,
		Rigs:        map[string]string{"millwright": c.rig},
		DryRun:      dryRun,
		Out:         &c.out,
		Log:         ticklog.New(c.dispatchLogDir()),
		Now:         func() time.Time { return dispatchNow },
	}.Run(context.Background())
	return nil
}

// dispatchNow is the time every dispatch scenario runs at, so that a line of
// the log can be said whole.
var dispatchNow = time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

// dispatchLogDir is where a scenario's dispatch keeps its log: inside the
// scenario's own temp directory, never under the real home.
func (c *dispatchContext) dispatchLogDir() string {
	return filepath.Join(c.root, "state", "mw-dispatch")
}

// dispatchLogLines is what the log holds, oldest first.
func (c *dispatchContext) dispatchLogLines() ([]string, error) {
	return ticklog.New(c.dispatchLogDir()).Read(context.Background())
}

func (c *dispatchContext) theDispatchLogHoldsLines(n int) error {
	lines, err := c.dispatchLogLines()
	if err != nil {
		return err
	}
	if len(lines) != n {
		return fmt.Errorf("expected the dispatch log to hold %d lines, it holds %d: %q", n, len(lines), lines)
	}
	return nil
}

func (c *dispatchContext) theDispatchLogHoldsExactly(line string) error {
	lines, err := c.dispatchLogLines()
	if err != nil {
		return err
	}
	if len(lines) != 1 || lines[0] != line {
		return fmt.Errorf("expected the dispatch log to hold exactly %q, it holds %q (dispatch said: %v)", line, lines, c.err)
	}
	return nil
}

func (c *dispatchContext) theDispatchLogHoldsALineSaying(begins, says string) error {
	lines, err := c.dispatchLogLines()
	if err != nil {
		return err
	}
	if len(lines) != 1 || !strings.HasPrefix(lines[0], begins) || !strings.Contains(lines[0], says) {
		return fmt.Errorf("expected the dispatch log to hold one line beginning %q and saying %q, it holds %q", begins, says, lines)
	}
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

func (c *dispatchContext) theDeadSessionWasClosed(id string) error {
	name := application.SessionName(id)
	for _, closed := range c.runner.Closed() {
		if closed == name {
			return nil
		}
	}
	return fmt.Errorf("expected the dead session %s to have been closed, got %q closed (dispatch said: %v)", name, c.runner.Closed(), c.err)
}

func (c *dispatchContext) nothingWasClosed() error {
	if closed := c.runner.Closed(); len(closed) != 0 {
		return fmt.Errorf("expected no session to have been closed, got %q", closed)
	}
	return nil
}

func (c *dispatchContext) theSessionIsRunning(id string) error {
	status, err := c.runner.Status(context.Background(), application.SessionName(id))
	if err != nil {
		return err
	}
	if !status.Running() {
		return fmt.Errorf("expected the session for %s to be running, got %q", id, status.State)
	}
	return nil
}

func (c *dispatchContext) theSessionStillRunsWhereItBegan(id string) error {
	if err := c.theSessionIsRunning(id); err != nil {
		return err
	}
	return c.theSessionRunsInTheWorktree(id, id)
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

// nothingMoreWasPoured says the only molecule there is, is the one the scenario
// poured before the dispatch ran.
func (c *dispatchContext) nothingMoreWasPoured() error {
	if poured := c.tracker.Molecules(); poured != 1 {
		return fmt.Errorf("expected the molecule poured before the dispatch to be the only one, got %d molecules", poured)
	}
	return nil
}

func (c *dispatchContext) theFormulaWasPouredAgainFor(formula, id string) error {
	if poured := c.tracker.Molecules(); poured != 2 {
		return fmt.Errorf("expected the formula %s to have been poured a second time for %s, got %d molecules in all", formula, id, poured)
	}
	molecule, poured := c.tracker.Poured(id)
	if !poured || molecule.Formula != formula || molecule.RootID == c.earlier.RootID {
		return fmt.Errorf("expected a new molecule of %s for %s, got %+v (the one before was %s)", formula, id, molecule, c.earlier.RootID)
	}
	return nil
}

func (c *dispatchContext) theStoryStillRecordsItsFirstMolecule(id string) error {
	if got := c.tracker.Metadata(id)[application.MoleculeField]; got != c.earlier.RootID {
		return fmt.Errorf("expected %s to still record the molecule %s, got %q", id, c.earlier.RootID, got)
	}
	return nil
}

func (c *dispatchContext) theStoryRecordsTheMoleculeItWasPouredAs(id string) error {
	molecule, poured := c.tracker.Poured(id)
	if !poured {
		return fmt.Errorf("nothing was poured for %s", id)
	}
	if got := c.tracker.Metadata(id)[application.MoleculeField]; got != molecule.RootID {
		return fmt.Errorf("expected %s to record the molecule %s it was poured as, got %q", id, molecule.RootID, got)
	}
	return nil
}

// theBootFileHoldsOnlyTheOpenSteps reads the boot file for the steps the earlier
// molecule had left open, in order, and for none of the ones it had closed.
func (c *dispatchContext) theBootFileHoldsOnlyTheOpenSteps(id string) error {
	written, err := os.ReadFile(filepath.Join(c.vault, vault.RunsDir, id, application.BootFileName))
	if err != nil {
		return fmt.Errorf("reading the boot file of %s: %w", id, err)
	}
	boot, at := string(written), 0
	for _, step := range c.earlier.Steps[1:] {
		found := strings.Index(boot[at:], step.ID)
		if found < 0 {
			return fmt.Errorf("the boot file of %s does not hold the open step %s, in order:\n%s", id, step.ID, boot)
		}
		at += found + len(step.ID)
	}
	if first := c.earlier.Steps[0]; strings.Contains(boot, first.ID) {
		return fmt.Errorf("the boot file of %s holds the step %s, which was already closed:\n%s", id, first.ID, boot)
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

// dispatchWouldStartInOrder says the dry run would start exactly these stories,
// most urgent and oldest first.
func (c *dispatchContext) dispatchWouldStartInOrder(table *godog.Table) error {
	report, err := c.dispatched()
	if err != nil {
		return err
	}
	if !report.DryRun {
		return fmt.Errorf("expected a dry run, got %+v", report)
	}
	var want, got []string
	for _, row := range table.Rows {
		want = append(want, row.Cells[0].Value)
	}
	for _, started := range report.Started {
		got = append(got, started.StoryID)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		return fmt.Errorf("expected the dry run to start %q in that order, got %q", want, got)
	}
	return nil
}

// theReportListsThemInOrder reads the dry run as a person does, from what it
// printed: each story's line comes after the line of the one before it.
func (c *dispatchContext) theReportListsThemInOrder() error {
	report, err := c.dispatched()
	if err != nil {
		return err
	}
	printed, at := report.String(), 0
	for _, started := range report.Started {
		found := strings.Index(printed[at:], "would start "+started.StoryID+" ")
		if found < 0 {
			return fmt.Errorf("expected the report to list %s after the stories before it, got:\n%s", started.StoryID, printed)
		}
		at += found
	}
	return nil
}

// dispatchPassedOver says the story was passed over with a reason containing
// the words given, both in the report's data and in what a person reads.
func (c *dispatchContext) dispatchPassedOver(id, saying string) error {
	report, err := c.dispatched()
	if err != nil {
		return err
	}
	for _, passed := range report.Passed {
		if passed.StoryID != id {
			continue
		}
		if !strings.Contains(passed.Why, saying) {
			return fmt.Errorf("expected %s to be passed over saying %q, got %q", id, saying, passed.Why)
		}
		if printed := report.String(); !strings.Contains(printed, "passed  "+id+" · "+passed.Why) {
			return fmt.Errorf("expected the report to print why %s was passed over, got:\n%s", id, printed)
		}
		return nil
	}
	return fmt.Errorf("expected %s to be passed over, got %+v", id, report.Passed)
}

func (c *dispatchContext) theReportSaysHowManyWereRunning(running, cap int) error {
	report, err := c.dispatched()
	if err != nil {
		return err
	}
	want := fmt.Sprintf("%d of %d sessions were already running", running, cap)
	if printed := report.String(); !strings.Contains(printed, want) {
		return fmt.Errorf("expected the report to say %q, got:\n%s", want, printed)
	}
	return nil
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

// theSyncWasTriedWaiting says the sync was tried that many times, with one wait
// between each try and the next of the length the scenario expects.
func (c *dispatchContext) theSyncWasTriedWaiting(tries, seconds int) error {
	if got := c.files.PullTries(); got != tries {
		return fmt.Errorf("expected the sync to be tried %d times, it was tried %d", tries, got)
	}
	want := time.Duration(seconds) * time.Second
	if len(c.waits) != tries-1 {
		return fmt.Errorf("expected %d waits between %d tries, got %v", tries-1, tries, c.waits)
	}
	for _, waited := range c.waits {
		if waited != want {
			return fmt.Errorf("expected every wait to be %s, got %v", want, c.waits)
		}
	}
	return nil
}

func (c *dispatchContext) theSyncWasTriedWaitingNothing(tries int) error {
	if got := c.files.PullTries(); got != tries {
		return fmt.Errorf("expected the sync to be tried %d time, it was tried %d", tries, got)
	}
	if len(c.waits) != 0 {
		return fmt.Errorf("expected no wait, got %v", c.waits)
	}
	return nil
}

func (c *dispatchContext) theReportSaysTheSyncWasRetried(retries int) error {
	if c.report.SyncRetries != retries {
		return fmt.Errorf("expected the report to count %d retries, got %d", retries, c.report.SyncRetries)
	}
	want := fmt.Sprintf("retried the sync %d times", retries)
	if !strings.Contains(c.out.String(), want) {
		return fmt.Errorf("expected what dispatch printed to say %q, got:\n%s", want, c.out.String())
	}
	return nil
}

// dispatchPrintedTheOneLine says the whole of what dispatch printed was that one
// line, and that the error it left with is not to be printed again.
func (c *dispatchContext) dispatchPrintedTheOneLine(line string) error {
	if got := c.out.String(); got != line+"\n" {
		return fmt.Errorf("expected dispatch to print exactly %q, got %q", line+"\n", got)
	}
	return nil
}

func (c *dispatchContext) dispatchLeavesWithStatus(status int) error {
	if got := application.ExitStatus(c.err); got != status {
		return fmt.Errorf("expected dispatch to leave with %d, it leaves with %d (it said: %v)", status, got, c.err)
	}
	return nil
}

// attempts is how many times a story may be started: what the scenario's config
// file said, else what the config says when it says nothing.
func (c *dispatchContext) attempts() int {
	if c.maxAttempts > 0 {
		return c.maxAttempts
	}
	return config.DefaultMaxAttempts
}

// theConfigSaysMaxAttempts writes a config file under a home directory of the
// scenario's own and reads the knob back out of it the way mw does, so that a
// scenario proves the cap comes from the config and not from the step.
func (c *dispatchContext) theConfigSaysMaxAttempts(tries int) error {
	root, err := c.workspace()
	if err != nil {
		return err
	}
	home := filepath.Join(root, "home")
	dir := filepath.Join(home, ".config", "mw")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(fmt.Sprintf("max_attempts = %d\n", tries)), 0o644); err != nil {
		return err
	}
	if os.Getenv(config.MaxAttemptsEnv) != "" {
		return fmt.Errorf("%s is set in the environment, and would answer ahead of the config file", config.MaxAttemptsEnv)
	}
	// HOME is the process's, and other scenarios read it: it is moved only for as
	// long as it takes to read the config, and put back before the step returns.
	before := os.Getenv("HOME")
	os.Setenv("HOME", home)
	defer os.Setenv("HOME", before)

	c.maxAttempts, err = config.MaxAttempts()
	return err
}

func (c *dispatchContext) theStoryHasBeenTried(id string, times int) error {
	return c.tracker.SetStoryMetadata(context.Background(), id, map[string]string{application.AttemptsField: fmt.Sprint(times)})
}

func (c *dispatchContext) theStoryCarriesTheComment(id, text string) error {
	return c.tracker.CommentOnStory(context.Background(), id, text)
}

// theStoryIsGivenBack is what it takes for a story whose session has ended to
// be dispatched again: the claim given back, the dead session left where it
// lies, and the worktree cleared away, which is what the Mayor does before a
// story worked once is worked again.
func (c *dispatchContext) theStoryIsGivenBack(id string) error {
	if err := c.tracker.ReleaseClaim(context.Background(), id); err != nil {
		return err
	}
	c.runner.Exit(application.SessionName(id), 1)
	return rig.New().Remove(context.Background(), c.rig, c.worktreeOf(id), application.StoryBranch(id))
}

func (c *dispatchContext) theCounterIsResetByHand(id string) error {
	return c.theCounterIsResetByHandTo(id, 0)
}

func (c *dispatchContext) theCounterIsResetByHandTo(id string, to int) error {
	return c.tracker.SetStoryMetadata(context.Background(), id, map[string]string{application.AttemptsField: fmt.Sprint(to)})
}

func (c *dispatchContext) theStoryRecordsAttempts(id string, want int) error {
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return err
	}
	if detail.Attempts != want {
		return fmt.Errorf("expected %s to record %d attempts, got %d (dispatch said: %v)", id, want, detail.Attempts, c.err)
	}
	return nil
}

func (c *dispatchContext) theStoryIsRecordedAsBlockedFor(id, reason string) error {
	if got := c.tracker.State(id, application.RunState); got != application.RunBlocked {
		return fmt.Errorf("expected %s to be recorded %s=%s, got %q (dispatch said: %v)", id, application.RunState, application.RunBlocked, got, c.err)
	}
	if said := c.tracker.StateReason(id, application.RunState); !strings.HasPrefix(said, "("+reason+")") {
		return fmt.Errorf("expected the reason to begin with the code (%s), got %q", reason, said)
	}
	return nil
}

func (c *dispatchContext) theStoryIsNotRecordedAsBlocked(id string) error {
	if got := c.tracker.State(id, application.RunState); got == application.RunBlocked {
		return fmt.Errorf("expected %s not to be recorded as blocked, but it is", id)
	}
	return nil
}

// exhaustedComments counts the comments on a story that say it used up its
// attempts, by the code they carry.
func (c *dispatchContext) exhaustedComments(id string) int {
	var said int
	for _, comment := range c.tracker.Comments(id) {
		if strings.Contains(comment, string(application.ReasonAttemptsExhausted)) {
			said++
		}
	}
	return said
}

// formulaNotInstalledComments counts the comments on a story that say its
// formula is not installed, by the code they carry.
func (c *dispatchContext) formulaNotInstalledComments(id string) int {
	var said int
	for _, comment := range c.tracker.Comments(id) {
		if strings.Contains(comment, application.ReasonFormulaNotInstalled) {
			said++
		}
	}
	return said
}

func (c *dispatchContext) theStoryCarriesOneFormulaNotInstalledComment(id string) error {
	if got := c.formulaNotInstalledComments(id); got != 1 {
		return fmt.Errorf("expected exactly one comment on %s saying its formula is not installed, got %d: %q", id, got, c.tracker.Comments(id))
	}
	return nil
}

func (c *dispatchContext) theStoryCarriesExhaustedComments(id string, want int) error {
	if got := c.exhaustedComments(id); got != want {
		return fmt.Errorf("expected %d comments on %s saying it used up its attempts, got %d: %q", want, id, got, c.tracker.Comments(id))
	}
	return nil
}

func (c *dispatchContext) theStoryCarriesOneExhaustedComment(id string) error {
	return c.theStoryCarriesExhaustedComments(id, 1)
}

func (c *dispatchContext) theStoryCarriesNoExhaustedComment(id string) error {
	return c.theStoryCarriesExhaustedComments(id, 0)
}

// mailsToTheMayor is what waits in the Mayor's mailbox, oldest first.
func (c *dispatchContext) mailsToTheMayor() ([]application.Message, error) {
	return c.mail.Inbox(context.Background(), application.MayorMailbox)
}

func (c *dispatchContext) theMayorHasMails(want int) error {
	mails, err := c.mailsToTheMayor()
	if err != nil {
		return err
	}
	if len(mails) != want {
		return fmt.Errorf("expected the Mayor to have %d mails, got %d: %+v", want, len(mails), mails)
	}
	return nil
}

func (c *dispatchContext) theMailSays(words string) error {
	mails, err := c.mailsToTheMayor()
	if err != nil {
		return err
	}
	if len(mails) == 0 {
		return fmt.Errorf("expected a mail to the Mayor saying %q, but there is none", words)
	}
	last := mails[len(mails)-1]
	if !strings.Contains(last.Subject+"\n"+last.Body, words) {
		return fmt.Errorf("expected the mail to the Mayor to say %q, got subject %q and body:\n%s", words, last.Subject, last.Body)
	}
	return nil
}
