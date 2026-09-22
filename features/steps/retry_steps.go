package steps

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// retryContext holds everything one mw retry scenario runs against: a real
// rig with a real bare origin, and a real vault with a bare origin of its
// own — so that a scenario can prove the bundle it makes really verifies and
// the commit it makes in the vault really reaches the vault's remote — with
// in-memory stand-ins for the beads database and the terminal.
type retryContext struct {
	root        string // holds the rig's origin, the rig checkout, the vault's origin and the vault
	rig         string
	vault       string
	vaultOrigin string

	tracker *apptest.FakeTracker
	runner  *apptest.FakeRunner

	lastEpic       string
	commentsBefore map[string]int

	report  application.RetryReport
	err     error
	printed bytes.Buffer
}

// retryRig and retryHost are the rig name and host every scenario of this
// feature uses: only one of each is ever needed to prove the command.
const (
	retryRig  = "millwright"
	retryHost = "vps"
)

// InitializeRetryScenario registers the steps of features/retry.feature.
func InitializeRetryScenario(ctx *godog.ScenarioContext) {
	c := &retryContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = retryContext{
			tracker: apptest.NewFakeTracker(),
			runner:  apptest.NewFakeRunner(),
		}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.root != "" {
			_ = os.RemoveAll(c.root)
		}
		return ctx, nil
	})

	ctx.Given(`^a vault that is a real git clone$`, c.aVaultThatIsARealGitClone)
	ctx.Given(`^the rig "([^"]*)" is checked out here, from a bare origin of its own$`, c.theRigIsCheckedOutHereFromItsOrigin)
	ctx.Given(`^an epic "([^"]*)" whose stories are worked on "([^"]*)" and target "([^"]*)"$`, c.anEpicWorkedOnTarget)
	ctx.Given(`^the story "([^"]*)" was dispatched and worked in its own worktree$`, c.theStoryWasDispatchedAndWorked)
	ctx.Given(`^the worktree of "([^"]*)" also holds uncommitted work$`, c.theWorktreeAlsoHoldsUncommittedWork)
	ctx.Given(`^the session of "([^"]*)" has ended$`, c.theSessionHasEnded)
	ctx.Given(`^the session of "([^"]*)" is still running$`, c.theSessionIsStillRunning)
	ctx.Given(`^the story "([^"]*)" has been tried (\d+) times in all$`, c.theStoryHasBeenTriedTimesInAll)

	ctx.When(`^mw retries "([^"]*)"$`, c.mwRetries)

	ctx.Then(`^the retry succeeds$`, c.theRetrySucceeds)
	ctx.Then(`^the retry refuses, saying: (.+)$`, c.theRetryRefusesSaying)
	ctx.Then(`^the branch of "([^"]*)" was bundled into "([^"]*)" in the vault, and it verifies$`, c.theBranchWasBundledIntoAndVerifies)
	ctx.Then(`^the vault committed and pushed the bundle of "([^"]*)"$`, c.theVaultCommittedAndPushedTheBundle)
	ctx.Then(`^the worktree and branch of "([^"]*)" are both gone$`, c.retryNothingIsLeftOfTheWorktree)
	ctx.Then(`^the worktree of "([^"]*)" was left untouched$`, c.retryTheWorktreeIsStillThere)
	ctx.Then(`^the story "([^"]*)" is open and unassigned$`, c.theStoryIsOpenAndUnassigned)
	ctx.Then(`^the story "([^"]*)" is still claimed$`, c.theStoryIsStillClaimed)
	ctx.Then(`^the story "([^"]*)" still records (\d+) attempts?$`, c.theStoryStillRecordsAttempts)
	ctx.Then(`^the story "([^"]*)" carries a comment naming the branch commit, the bundle path and the vault commit$`, c.theStoryCarriesTheRetryComment)
	ctx.Then(`^the story "([^"]*)" carries no new comment$`, c.theStoryCarriesNoNewComment)
	ctx.Then(`^the leftover work was committed onto the branch of "([^"]*)"$`, c.theLeftoverWorkWasCommittedOntoTheBranch)
}

// workspace makes the temp directory a scenario keeps everything in, once.
func (c *retryContext) workspace() (string, error) {
	if c.root != "" {
		return c.root, nil
	}
	root, err := os.MkdirTemp("", "mw-retry-")
	if err != nil {
		return "", fmt.Errorf("making a workspace: %w", err)
	}
	c.root = root
	return root, nil
}

// aVaultThatIsARealGitClone makes a bare repository standing in for the
// vault's remote and a real clone of it tracking that remote, so that a
// scenario can prove mw retry's commit really reaches the vault's origin.
func (c *retryContext) aVaultThatIsARealGitClone() error {
	root, err := c.workspace()
	if err != nil {
		return err
	}
	origin := filepath.Join(root, "vault-origin.git")
	if err := gitRun(root, "git", "init", "--bare", "-q", "-b", "main", origin); err != nil {
		return err
	}
	c.vaultOrigin = origin

	c.vault = filepath.Join(root, "vault")
	if err := gitRun(root, "git", "clone", "-q", origin, c.vault); err != nil {
		return err
	}
	if err := gitIdentify(c.vault); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(c.vault, "README.md"), []byte("# vault\n"), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"add", "-A"}, {"commit", "-qm", "The vault opens"}, {"push", "-q", "-u", "origin", "main"},
	} {
		if err := gitRun(c.vault, "git", args...); err != nil {
			return err
		}
	}
	return nil
}

// theRigIsCheckedOutHereFromItsOrigin makes a bare repository standing in for
// the rig's origin, a clone that seeds it with its first commit, and the
// checkout this host works from.
func (c *retryContext) theRigIsCheckedOutHereFromItsOrigin(name string) error {
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

	seed := filepath.Join(root, "seed")
	if err := gitRun(root, "git", "clone", "-q", origin, seed); err != nil {
		return err
	}
	if err := gitIdentify(seed); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("# "+name+"\n"), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"add", "-A"}, {"commit", "-qm", "The rig opens"}, {"push", "-q", "-u", "origin", "main"},
	} {
		if err := gitRun(seed, "git", args...); err != nil {
			return err
		}
	}

	c.rig = filepath.Join(root, "rigs", name)
	if err := gitRun(root, "git", "clone", "-q", origin, c.rig); err != nil {
		return err
	}
	return gitIdentify(c.rig)
}

func (c *retryContext) anEpicWorkedOnTarget(epic, host, branch string) error {
	c.tracker.AddEpic(epic, domain.Path{
		Rig: retryRig, Branch: branch, Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Host: host,
	})
	c.lastEpic = epic
	return nil
}

// theStoryWasDispatchedAndWorked leaves the world as a dispatch that started a
// session would: the story claimed, its first attempt recorded, its worktree
// cut from the target branch with one commit on it — the session's work.
func (c *retryContext) theStoryWasDispatchedAndWorked(id string) error {
	c.tracker.AddStory(c.lastEpic, domain.Story{ID: id, Title: "The story " + id})
	ctx := context.Background()
	if err := c.tracker.ClaimStory(ctx, id); err != nil {
		return err
	}
	if err := c.tracker.SetStoryMetadata(ctx, id, map[string]string{application.AttemptsField: "1"}); err != nil {
		return err
	}
	if err := c.tracker.SetStoryState(ctx, id, application.RunState, application.RunBlocked,
		"mw next did not close this story out (tests-fail): the rig's tests fail in the worktree"); err != nil {
		return err
	}

	worktrees := rig.New()
	if err := worktrees.Fetch(ctx, c.rig); err != nil {
		return err
	}
	dir := application.WorktreeDir(c.rig, id)
	branch := application.StoryBranch(id)
	if err := worktrees.Add(ctx, c.rig, dir, branch, application.StartPoint(rig.DefaultRemote, "main")); err != nil {
		return err
	}
	if err := gitIdentify(dir); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte("the work of "+id+"\n"), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-qm", "The work of " + id}} {
		if err := gitRun(dir, "git", args...); err != nil {
			return err
		}
	}
	return nil
}

// theWorktreeAlsoHoldsUncommittedWork is what a session left in its worktree
// without ever committing it: the shape a headless session's turn ending mid-
// edit leaves behind.
func (c *retryContext) theWorktreeAlsoHoldsUncommittedWork(id string) error {
	dir := application.WorktreeDir(c.rig, id)
	return os.WriteFile(filepath.Join(dir, "leftover.md"), []byte("left uncommitted by the session\n"), 0o644)
}

func (c *retryContext) theSessionHasEnded(id string) error {
	name := application.SessionName(id)
	if err := c.runner.Start(context.Background(), application.SessionSpec{Name: name, Command: []string{"claude"}}); err != nil {
		return err
	}
	c.runner.Exit(name, 1)
	return nil
}

func (c *retryContext) theSessionIsStillRunning(id string) error {
	name := application.SessionName(id)
	return c.runner.Start(context.Background(), application.SessionSpec{Name: name, Command: []string{"claude"}})
}

func (c *retryContext) theStoryHasBeenTriedTimesInAll(id string, times int) error {
	return c.tracker.SetStoryMetadata(context.Background(), id, map[string]string{application.AttemptsField: strconv.Itoa(times)})
}

// mwRetries runs the use case the way mw retry does: the real worktrees, the
// real landing and the real vault, with stand-ins only for the beads database
// and the terminal.
func (c *retryContext) mwRetries(id string) error {
	if c.commentsBefore == nil {
		c.commentsBefore = map[string]int{}
	}
	c.commentsBefore[id] = len(c.tracker.Comments(id))

	worktrees := rig.New()
	files := vault.New(c.vault)
	c.report, c.err = application.Retry{
		Tracker:   c.tracker,
		Worktrees: worktrees,
		Landing:   worktrees,
		Runner:    c.runner,
		Files:     files,
		Vault:     files,
		Host:      retryHost,
		Rigs:      map[string]string{retryRig: c.rig},
		Out:       &c.printed,
	}.Run(context.Background(), id)
	return nil
}

func (c *retryContext) theRetrySucceeds() error {
	if c.err != nil {
		return fmt.Errorf("expected the retry to succeed, got: %v\n%s", c.err, c.printed.String())
	}
	return nil
}

func (c *retryContext) theRetryRefusesSaying(want string) error {
	if c.err == nil {
		return fmt.Errorf("expected the retry to refuse, but it succeeded: %+v", c.report)
	}
	if !c.report.Refused {
		return fmt.Errorf("expected the report to say it refused, got %+v (err: %v)", c.report, c.err)
	}
	if !strings.Contains(c.err.Error(), want) && !strings.Contains(c.report.Why, want) {
		return fmt.Errorf("expected the refusal to say %q, got error %q / why %q", want, c.err, c.report.Why)
	}
	return nil
}

func (c *retryContext) theBranchWasBundledIntoAndVerifies(id, relPath string) error {
	abs := filepath.Join(c.vault, filepath.FromSlash(relPath))
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("expected a bundle at %s: %w", abs, err)
	}
	if _, err := gitSay(c.rig, "bundle", "verify", abs); err != nil {
		return fmt.Errorf("the bundle at %s does not verify: %w", abs, err)
	}
	if c.report.BundlePath != relPath {
		return fmt.Errorf("expected the report to name the bundle path %s, got %s", relPath, c.report.BundlePath)
	}
	if c.report.BranchCommit == "" {
		return fmt.Errorf("expected the report to name the branch commit the bundle captured")
	}
	return nil
}

func (c *retryContext) theVaultCommittedAndPushedTheBundle(id string) error {
	local, err := gitSay(c.vault, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	remote, err := gitSay(c.vaultOrigin, "rev-parse", "main")
	if err != nil {
		return err
	}
	if strings.TrimSpace(local) != strings.TrimSpace(remote) {
		return fmt.Errorf("expected the vault's push to reach its origin: local %s, origin %s", local, remote)
	}
	if c.report.VaultCommit == "" {
		return fmt.Errorf("expected the report to name the vault commit")
	}
	return nil
}

func (c *retryContext) retryNothingIsLeftOfTheWorktree(id string) error {
	dir := application.WorktreeDir(c.rig, id)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("expected the worktree %s to have been taken away, but it is still there", dir)
	}
	if _, err := gitSay(c.rig, "rev-parse", "--verify", "--quiet", "refs/heads/"+application.StoryBranch(id)); err == nil {
		return fmt.Errorf("expected the branch %s to have been taken away too", application.StoryBranch(id))
	}
	return nil
}

func (c *retryContext) retryTheWorktreeIsStillThere(id string) error {
	dir := application.WorktreeDir(c.rig, id)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("expected the worktree %s to be left where it was: %w", dir, err)
	}
	return nil
}

func (c *retryContext) theStoryIsOpenAndUnassigned(id string) error {
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return err
	}
	if detail.Status != application.StatusOpen {
		return fmt.Errorf("expected %s to be open, got %q", id, detail.Status)
	}
	if detail.Assignee != "" {
		return fmt.Errorf("expected %s to be unassigned, got %q", id, detail.Assignee)
	}
	return nil
}

func (c *retryContext) theStoryIsStillClaimed(id string) error {
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return err
	}
	if detail.Assignee == "" {
		return fmt.Errorf("expected %s to still be claimed, but it has no assignee", id)
	}
	return nil
}

func (c *retryContext) theStoryStillRecordsAttempts(id string, want int) error {
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return err
	}
	if detail.Attempts != want {
		return fmt.Errorf("expected %s to record %d attempt(s), got %d", id, want, detail.Attempts)
	}
	return nil
}

// theStoryCarriesTheRetryComment checks the one comment mw retry is required
// to leave: naming the branch commit it bundled, the bundle's path in the
// vault, and the commit the vault pushed it as — the same three facts a
// person used to have to copy out of several hand steps by themselves.
func (c *retryContext) theStoryCarriesTheRetryComment(id string) error {
	comments := c.tracker.Comments(id)
	joined := strings.Join(comments, "\n")
	for what, want := range map[string]string{
		"the branch commit": short(c.report.BranchCommit),
		"the bundle path":   c.report.BundlePath,
		"the vault commit":  short(c.report.VaultCommit),
	} {
		if want == "" || !strings.Contains(joined, want) {
			return fmt.Errorf("expected a comment on %s naming %s (%q), got %q", id, what, want, comments)
		}
	}
	return nil
}

func (c *retryContext) theStoryCarriesNoNewComment(id string) error {
	was, ok := c.commentsBefore[id]
	if !ok {
		return fmt.Errorf("no comments were counted on %s before the retry", id)
	}
	if got := len(c.tracker.Comments(id)); got != was {
		return fmt.Errorf("expected %s to hold the %d comment(s) it held before, got %d: %q", id, was, got, c.tracker.Comments(id))
	}
	return nil
}

// theLeftoverWorkWasCommittedOntoTheBranch proves the worktree's uncommitted
// file really reached the branch the bundle captured — not just that the
// report says so — by fetching the bundle back into a scratch ref (the
// worktree and branch are gone by the time this runs) and reading the file
// out of it.
func (c *retryContext) theLeftoverWorkWasCommittedOntoTheBranch(id string) error {
	if !c.report.CommittedLeftovers {
		return fmt.Errorf("expected the report to say the worktree's leftover work was committed, got %+v", c.report)
	}
	bundle := filepath.Join(c.vault, filepath.FromSlash(c.report.BundlePath))
	scratch := "refs/mw-retry-check/" + id
	if err := gitRun(c.rig, "git", "fetch", "-q", bundle, application.StoryBranch(id)+":"+scratch); err != nil {
		return fmt.Errorf("fetching the bundle back to check it: %w", err)
	}
	defer func() { _ = gitRun(c.rig, "git", "update-ref", "-d", scratch) }()

	out, err := gitSay(c.rig, "show", scratch+":leftover.md")
	if err != nil {
		return fmt.Errorf("expected the bundle to hold the worktree's leftover file: %w", err)
	}
	if !strings.Contains(out, "left uncommitted") {
		return fmt.Errorf("expected the leftover file's content in the bundle, got %q", out)
	}
	return nil
}

// short is a commit sha as a person names it, the same truncation
// application.RetryReport's own comment uses.
func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
