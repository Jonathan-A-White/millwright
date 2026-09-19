package steps

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// nextContext holds everything one close-out scenario runs against: a real rig
// with a real bare origin and a real second clone standing in for the other
// host, a real vault directory with a real ledger in it, and in-memory stand-ins
// for the two things that would reach outside the temp directory — the beads
// database and the terminal a session runs in. The merging, the pushing, the
// worktrees and the ledger are all real; nothing here touches the factory's own
// vault, rigs, beads database, terminal or remote.
type nextContext struct {
	root  string // holds the origin, both clones and the vault
	vault string
	rig   string
	seed  string // the other host's clone

	tracker *apptest.FakeTracker
	runner  *apptest.FakeRunner
	files   *apptest.FakeVaultFiles
	mailbox *apptest.FakeMailbox

	gitProgram   string // a wrapper that logs what git was asked to do
	gitLog       string
	checkCommand string // what this scenario's "rig tests" are
	checkLog     string
	racer        string   // the other host, landing work mid-push
	refusal      []string // what the origin's hook says when it refuses a push

	lastEpic     string
	ledgerBefore []string
	afterFirst   []string // the ledger as the first close-out of a scenario left it
	originBefore string
	rigBefore    string // the rig checkout's HEAD before mw closed out, in the rig checkout scenarios
	signed       string // the short hash of the commit a scenario signed

	report  application.NextReport
	checked application.CheckReport // what mw check reported, in the check scenarios
	err     error
	printed bytes.Buffer // the report as the person running mw reads it
	stderr  bytes.Buffer // what mw said on stderr

	touchedBefore *touched // what a check scenario found before it ran mw check
	touchedFor    string
}

// What a scenario's fixtures hold.
const (
	nextCharter = "# Builder — charter\n\nYou work one story, and you never push, merge or close it yourself.\n"
	nextLedger  = "# Builder — ledger\n\nAppend-only. One line per story.\n\n" +
		"| date | story | outcome | model/effort | fuel | notes |\n|---|---|---|---|---|---|\n"
	nextEarlier   = "| 2026-09-17 | An earlier story (mw-old.1) | landed on main | opus/high | 1,000 tokens | by hand |"
	nextOtherFile = "other.md"
	nextHost      = "vps"
	nextSeat      = "builder"
)

// InitializeNextScenario registers the steps of features/next.feature.
func InitializeNextScenario(ctx *godog.ScenarioContext) {
	c := &nextContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = nextContext{
			tracker: apptest.NewFakeTracker(),
			runner:  apptest.NewFakeRunner(),
			files:   &apptest.FakeVaultFiles{},
			mailbox: apptest.NewFakeMailbox(),
		}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.root != "" {
			_ = os.RemoveAll(c.root)
		}
		return ctx, nil
	})

	ctx.Given(`^a factory whose vault holds a builder charter and a ledger$`, c.aFactoryVault)
	ctx.Given(`^a rig "([^"]*)" checked out from a bare origin of its own$`, c.aRigFromABareOrigin)
	ctx.Given(`^a plan "([^"]*)" whose stories are worked on "([^"]*)" and target "([^"]*)"$`, c.aPlanWorkedHere)
	ctx.Given(`^the story "([^"]*)" has been worked in its own worktree$`, c.theStoryHasBeenWorked)
	ctx.Given(`^the story "([^"]*)" has been worked in its own worktree, committing nothing$`, c.theStoryCommittedNothing)
	ctx.Given(`^the session of "([^"]*)" reported this result:$`, c.theSessionReportedThis)
	ctx.Given(`^the session of "([^"]*)" reported a plain success$`, c.theSessionSucceeded)
	ctx.Given(`^the session of "([^"]*)" left no result at all$`, c.theSessionLeftNothing)
	ctx.Given(`^the run of "([^"]*)" left its boot file beside the result$`, c.theRunLeftItsBootFile)
	ctx.Given(`^the rig's tests fail, saying "([^"]*)"$`, c.theRigsTestsFail)
	ctx.Given(`^the rig's tests pass$`, c.theRigsTestsPass)
	ctx.Given(`^the rig's tests cannot be run, saying "([^"]*)"$`, c.theRigsTestsCannotBeRun)
	ctx.Given(`^the story "([^"]*)" is planned and ready to be worked here$`, c.aStoryReadyHere)
	ctx.Given(`^the ledger already holds a line from an earlier story$`, c.theLedgerAlreadyHoldsALine)
	ctx.Given(`^a formula was poured for "([^"]*)" and one of its steps is still open$`, c.aFormulaWithAnOpenStep)
	ctx.Given(`^the story "([^"]*)" is claimed here with no session behind it$`, c.aStoryClaimedWithNoSession)
	ctx.Given(`^the session of "([^"]*)" left these uncommitted in its worktree:$`, c.theSessionLeftUncommittedWork)
	ctx.Given(`^a commit on the branch of "([^"]*)" carries "([^"]*)"$`, c.aCommitCarrying)
	ctx.Given(`^the other host landed its own work on "([^"]*)" while "([^"]*)" was worked$`, c.theOtherHostLandedFirst)
	ctx.Given(`^the other host lands its own work the moment mw first tries to push$`, c.theOtherHostRacesThePush)
	ctx.Given(`^the origin refuses every push, saying:$`, c.theOriginRefusesEveryPush)
	ctx.Given(`^the tracker refuses to close "([^"]*)", saying: (.+)$`, c.theTrackerRefusesToClose)
	ctx.Given(`^the tracker will take a close of "([^"]*)" again$`, c.theTrackerTakesACloseAgain)
	ctx.Given(`^the vault holds work of its own that nobody committed, to "([^"]*)"$`, c.theVaultHoldsOtherWork)
	ctx.Given(`^the vault refuses a commit, saying: (.+)$`, c.theVaultRefusesACommit)
	ctx.Given(`^the vault takes a commit again$`, c.theVaultTakesACommitAgain)
	ctx.Given(`^the terminal still holds the exited session of "([^"]*)"$`, c.theTerminalHoldsAnExitedSession)

	registerNextMailSteps(ctx, c)
	registerNextRigSteps(ctx, c)

	ctx.When(`^mw closes out "([^"]*)"$`, c.mwClosesOut)
	ctx.When(`^mw closes out "([^"]*)" a second time$`, c.mwClosesOut)

	ctx.Then(`^the work of "([^"]*)" is on "([^"]*)" at the rig's origin$`, c.theWorkIsOnTheOrigin)
	ctx.Then(`^the other host's work is still on "([^"]*)" at the rig's origin$`, c.theOtherHostsWorkIsStillThere)
	ctx.Then(`^nothing was landed on "([^"]*)"$`, c.nothingWasLandedOn)
	ctx.Then(`^it landed as a fast-forward$`, c.itLandedAsAFastForward)
	ctx.Then(`^it landed as a merge commit$`, c.itLandedAsAMergeCommit)
	ctx.Then(`^mw pushed twice and forced nothing$`, c.mwPushedTwiceAndForcedNothing)
	ctx.Then(`^the rig's tests were run (\d+) times$`, c.theRigsTestsWereRun)
	ctx.Then(`^the story "([^"]*)" is closed$`, c.theStoryIsClosed)
	ctx.Then(`^the story "([^"]*)" is not closed$`, c.theStoryIsNotClosed)
	ctx.Then(`^the story "([^"]*)" is held blocked$`, c.theStoryIsHeldBlocked)
	ctx.Then(`^the story "([^"]*)" carries a comment quoting: (.+)$`, c.theStoryCarriesACommentQuoting)
	ctx.Then(`^the story "([^"]*)" carries no comment quoting: (.+)$`, c.theStoryCarriesNoCommentQuoting)
	ctx.Then(`^the comment on "([^"]*)" and the report name that commit$`, c.theCommentAndReportNameTheCommit)
	ctx.Then(`^the comment on "([^"]*)" and the report say uncommitted work was left, listing:$`, c.theCommentAndReportSayUncommittedWorkWasLeft)
	ctx.Then(`^the last ledger line holds:$`, c.theLastLedgerLineHolds)
	ctx.Then(`^the last ledger line names "([^"]*)"$`, c.theLastLedgerLineNames)
	ctx.Then(`^the ledger still holds every line it held before$`, c.theLedgerStillHoldsEveryLine)
	ctx.Then(`^nothing is left of the worktree of "([^"]*)"$`, c.nothingIsLeftOfTheWorktree)
	ctx.Then(`^the worktree of "([^"]*)" is still there$`, c.theWorktreeIsStillThere)
	ctx.Then(`^a fresh session is running for "([^"]*)"$`, c.aFreshSessionIsRunningFor)
	ctx.Then(`^no fresh session was started$`, c.noFreshSessionWasStarted)
	ctx.Then(`^the story "([^"]*)" is recorded as stopped$`, c.theStoryIsRecordedAsStopped)
	ctx.Then(`^the merge slot of the rig is free again$`, c.theMergeSlotIsFree)
	ctx.Then(`^the terminal holds no session of "([^"]*)"$`, c.theTerminalHoldsNoSession)
	ctx.Then(`^the session of "([^"]*)" is still on the terminal, exited$`, c.theSessionIsStillThereExited)
	ctx.Then(`^the close-out says the story is landed but still open$`, c.landedButStillOpen)
	ctx.Then(`^the ledger holds exactly one line for "([^"]*)"$`, c.theLedgerHoldsOneLineFor)
	ctx.Then(`^the ledger holds (\d+) lines for "([^"]*)"$`, c.theLedgerHoldsLinesFor)
	ctx.Then(`^the first ledger line for "([^"]*)" holds:$`, c.theFirstLedgerLineForHolds)
	ctx.Then(`^the last ledger line holds no token figure$`, c.theLastLedgerLineHoldsNoTokenFigure)
	ctx.Then(`^the ledger still holds, unchanged, what the first close-out left in it$`, c.theLedgerIsUnchangedSinceTheFirstCloseOut)
	ctx.Then(`^mw status counts today's fuel as (.+)$`, c.mwStatusCountsTodaysFuelAs)
	ctx.Then(`^git was asked to merge once and to push once$`, c.mergedOncePushedOnce)
	ctx.Then(`^mw committed to the vault exactly:$`, c.mwCommittedToTheVaultExactly)
	ctx.Then(`^that vault commit names "([^"]*)" and signs nothing$`, c.theVaultCommitNames)
	ctx.Then(`^the hosts were brought level$`, c.theHostsWereBroughtLevel)
	ctx.Then(`^the close-out says the hosts could not be brought level, naming "([^"]*)"$`, c.theHostsCouldNotBeBroughtLevel)
	ctx.Then(`^the close-out notes that the vault could not be committed$`, c.theVaultCommitFailureIsNoted)
	ctx.Then(`^the vault commit does not hold "([^"]*)"$`, c.theVaultCommitDoesNotHold)
	ctx.Then(`^the report says the run record of "([^"]*)" was missing$`, c.theRunRecordWasMissing)
	ctx.Then(`^that ledger line holds only the first line of what the origin said$`, c.theLedgerLineHoldsOnlyTheFirstRefusalLine)
	ctx.Then(`^the run of "([^"]*)" holds a landing error with every line the origin said$`, c.theRunHoldsTheWholeLandingError)
	ctx.Then(`^the comment on "([^"]*)" quotes every line the origin said$`, c.theCommentQuotesTheWholeRefusal)

	registerCheckSteps(ctx, c)
}

// workspace makes the temp directory a scenario keeps everything in, once.
func (c *nextContext) workspace() (string, error) {
	if c.root != "" {
		return c.root, nil
	}
	root, err := os.MkdirTemp("", "mw-next-")
	if err != nil {
		return "", fmt.Errorf("making a workspace: %w", err)
	}
	c.root = root
	return root, nil
}

func (c *nextContext) aFactoryVault() error {
	root, err := c.workspace()
	if err != nil {
		return err
	}
	c.vault = filepath.Join(root, "vault")
	seat := filepath.Join(c.vault, vault.SeatsDir, nextSeat)
	if err := os.MkdirAll(seat, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(seat, vault.CharterFile), []byte(nextCharter), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(seat, application.LedgerFileName), []byte(nextLedger), 0o644)
}

// aRigFromABareOrigin makes the bare repository standing in for the rig's
// origin, the clone standing in for the other host, and this host's checkout —
// and the two scripts a scenario needs: a git that logs what it was asked to do,
// and the rig's "tests", which are trivial and count their own runs.
func (c *nextContext) aRigFromABareOrigin(name string) error {
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
	if err := gitIdentify(c.rig); err != nil {
		return err
	}

	c.checkLog = filepath.Join(root, "checks.log")
	c.checkCommand = fmt.Sprintf("printf 'run\\n' >> %s", c.checkLog)
	c.racer = filepath.Join(root, "racer.sh")
	return c.writeGitWrapper()
}

// writeGitWrapper puts a git in front of git: it writes down every command mw
// asks for, and — when a scenario has left a racer script beside it — lets the
// other host land its work in the instant before mw's first push.
func (c *nextContext) writeGitWrapper() error {
	c.gitLog = filepath.Join(c.root, "git.log")
	c.gitProgram = filepath.Join(c.root, "git-wrapper.sh")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %s
if [ "$1" = push ] && [ -x %s ] && [ ! -f %s.done ]; then
  : > %s.done
  %s
fi
exec git "$@"
`, c.gitLog, c.racer, c.racer, c.racer, c.racer)
	return os.WriteFile(c.gitProgram, []byte(script), 0o755)
}

func (c *nextContext) aPlanWorkedHere(epic, host, branch string) error {
	c.tracker.AddEpic(epic, domain.Path{
		Rig: "millwright", Branch: branch, Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Host: host,
	})
	c.lastEpic = epic
	return nil
}

func (c *nextContext) aStoryReadyHere(id string) error {
	c.tracker.AddStory(c.lastEpic, domain.Story{ID: id, Title: "The story " + id})
	return nil
}

// theStoryHasBeenWorked leaves the world as a finished session leaves it: the
// story claimed and running, its worktree cut from the target branch, and a
// commit on its branch that nothing else has.
func (c *nextContext) theStoryHasBeenWorked(id string) error {
	if err := c.worked(id); err != nil {
		return err
	}
	dir := application.WorktreeDir(c.rig, id)
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

func (c *nextContext) theStoryCommittedNothing(id string) error {
	return c.worked(id)
}

// theSessionLeftUncommittedWork is a session that ended its turn with work in
// its worktree and never committed it: the first path is a file the rig already
// tracks, changed in place, and the rest are new files, so that both kinds of
// dirty are in the worktree — a subdirectory among them, which git would
// otherwise fold into one line.
func (c *nextContext) theSessionLeftUncommittedWork(id string, table *godog.Table) error {
	dir := application.WorktreeDir(c.rig, id)
	for _, row := range table.Rows {
		file := row.Cells[0].Value
		full := filepath.Join(dir, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte("left undone by the session of "+id+"\n"), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// worked claims a story and cuts it the worktree a dispatch would have.
func (c *nextContext) worked(id string) error {
	if err := c.aStoryReadyHere(id); err != nil {
		return err
	}
	ctx := context.Background()
	if err := c.tracker.ClaimStory(ctx, id); err != nil {
		return err
	}
	if err := c.tracker.SetStoryState(ctx, id, application.RunState, application.RunRunning, "dispatched"); err != nil {
		return err
	}

	worktrees := rig.New()
	if err := worktrees.Fetch(ctx, c.rig); err != nil {
		return err
	}
	dir := application.WorktreeDir(c.rig, id)
	if err := worktrees.Add(ctx, c.rig, dir, application.StoryBranch(id), application.StartPoint("origin", "main")); err != nil {
		return err
	}
	return gitIdentify(dir)
}

// aStoryClaimedWithNoSession is a session that went away without closing its
// story out: the claim is there, the runner has nothing.
func (c *nextContext) aStoryClaimedWithNoSession(id string) error {
	if err := c.aStoryReadyHere(id); err != nil {
		return err
	}
	return c.tracker.ClaimStory(context.Background(), id)
}

// aCommitCarrying puts one more commit on a story's branch whose message
// carries the line given — an AI signing the work, which is the one thing this
// factory's commits never do. The short hash is kept, because what a close-out
// that refuses to land it must say is which commit.
func (c *nextContext) aCommitCarrying(id, line string) error {
	dir := application.WorktreeDir(c.rig, id)
	if err := os.WriteFile(filepath.Join(dir, "signed.md"), []byte("more of the work of "+id+"\n"), 0o644); err != nil {
		return err
	}
	if err := gitRun(dir, "git", "add", "-A"); err != nil {
		return err
	}
	if err := gitRun(dir, "git", "commit", "-qm", "More of the work of "+id+"\n\n"+line); err != nil {
		return err
	}
	hash, err := gitSay(dir, "rev-parse", "--short", "HEAD")
	if err != nil {
		return err
	}
	c.signed = hash
	return nil
}

// theCommentAndReportNameTheCommit is the whole point of refusing: a person
// told only that "a commit is signed" has to go looking, so the story's comment
// and the report both name the commit by its short hash.
func (c *nextContext) theCommentAndReportNameTheCommit(id string) error {
	if c.signed == "" {
		return fmt.Errorf("no commit was signed in this scenario")
	}
	named := false
	for _, comment := range c.tracker.Comments(id) {
		if strings.Contains(comment, c.signed) {
			named = true
		}
	}
	if !named {
		return fmt.Errorf("expected a comment on %s naming the commit %s, got %q", id, c.signed, c.tracker.Comments(id))
	}
	if said := c.printed.String(); !strings.Contains(said, c.signed) {
		return fmt.Errorf("expected the report to name the commit %s, got:\n%s", c.signed, said)
	}
	return nil
}

// theCommentAndReportSayUncommittedWorkWasLeft is the whole point of telling
// "committed nothing" from "committed nothing, and left the work lying there":
// the comment on the story and the report both say so, and both name every path.
func (c *nextContext) theCommentAndReportSayUncommittedWorkWasLeft(id string, table *godog.Table) error {
	comments := strings.Join(c.tracker.Comments(id), "\n")
	said := c.printed.String()
	for where, text := range map[string]string{"the comment on " + id: comments, "the report": said} {
		if !strings.Contains(text, "uncommitted work") {
			return fmt.Errorf("expected %s to say uncommitted work was left, got:\n%s", where, text)
		}
		for _, row := range table.Rows {
			if file := row.Cells[0].Value; !strings.Contains(text, file) {
				return fmt.Errorf("expected %s to list %s, got:\n%s", where, file, text)
			}
		}
	}
	return nil
}

func (c *nextContext) theStoryIsRecordedAsStopped(id string) error {
	if got := c.tracker.State(id, application.RunState); got != application.RunStopped {
		return fmt.Errorf("expected %s to be recorded %s=%s, got %q", id, application.RunState, application.RunStopped, got)
	}
	for _, said := range c.report.Abandoned {
		if said == id {
			return nil
		}
	}
	return fmt.Errorf("expected the report to name %s as claimed with no session behind it, got %q", id, c.report.Abandoned)
}

func (c *nextContext) theSessionReportedThis(id string, result *godog.DocString) error {
	return c.putResult(id, result.Content)
}

func (c *nextContext) theSessionSucceeded(id string) error {
	return c.putResult(id, `{"type":"result","subtype":"success","is_error":false,"num_turns":9,`+
		`"duration_ms":600000,"session_id":"s-plain","total_cost_usd":1.5,`+
		`"usage":{"input_tokens":100,"output_tokens":2000,"cache_read_input_tokens":50000,"cache_creation_input_tokens":3000}}`)
}

func (c *nextContext) theSessionLeftNothing(id string) error {
	return os.RemoveAll(filepath.Join(c.vault, vault.RunsDir, id))
}

// theRunLeftItsBootFile is the file mw wrote to prime the session, sitting in
// the run's directory beside the result. It is untracked in the vault and stays
// that way: a close-out commits the result and never it.
func (c *nextContext) theRunLeftItsBootFile(id string) error {
	dir := filepath.Join(c.vault, vault.RunsDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, application.BootFileName), []byte("the boot prompt"), 0o644)
}

// putResult writes what the session reported where the harness would have
// redirected it.
func (c *nextContext) putResult(id, contents string) error {
	dir := filepath.Join(c.vault, vault.RunsDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, application.ResultFileName), []byte(contents), 0o644)
}

func (c *nextContext) theRigsTestsPass() error {
	c.checkCommand = fmt.Sprintf("printf 'run\\n' >> %s", c.checkLog)
	return nil
}

func (c *nextContext) theRigsTestsFail(saying string) error {
	c.checkCommand = fmt.Sprintf("printf 'run\\n' >> %s; echo '%s'; exit 1", c.checkLog, saying)
	return nil
}

// theRigsTestsCannotBeRun is the toolchain missing: the shell finds no program
// to run, and says so with exit status 127.
func (c *nextContext) theRigsTestsCannotBeRun(saying string) error {
	c.checkCommand = fmt.Sprintf("printf 'run\\n' >> %s; echo '%s'; exit 127", c.checkLog, saying)
	return nil
}

func (c *nextContext) theLedgerAlreadyHoldsALine() error {
	path := filepath.Join(c.vault, vault.SeatsDir, nextSeat, application.LedgerFileName)
	held, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(held, []byte(nextEarlier+"\n")...), 0o644); err != nil {
		return err
	}
	return c.rememberTheLedger()
}

// rememberTheLedger writes down every line the ledger holds now, so that a
// scenario can say afterwards that none of them went anywhere.
func (c *nextContext) rememberTheLedger() error {
	lines, err := c.ledgerLines()
	if err != nil {
		return err
	}
	c.ledgerBefore = lines
	return nil
}

func (c *nextContext) ledgerLines() ([]string, error) {
	held, err := os.ReadFile(filepath.Join(c.vault, vault.SeatsDir, nextSeat, application.LedgerFileName))
	if err != nil {
		return nil, fmt.Errorf("reading the ledger: %w", err)
	}
	var lines []string
	for _, line := range strings.Split(string(held), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

// aFormulaWithAnOpenStep pours a formula for a story the way a dispatch does,
// and closes every step but the last: a session that stopped halfway.
func (c *nextContext) aFormulaWithAnOpenStep(id string) error {
	ctx := context.Background()
	c.tracker.AddFormula("tdd-feature",
		application.FormulaStep{Title: "Understand the story"},
		application.FormulaStep{Title: "Write the failing feature"},
		application.FormulaStep{Title: "Implement until green"},
	)
	molecule, err := c.tracker.PourFormula(ctx, "tdd-feature", id, "The story "+id)
	if err != nil {
		return err
	}
	if err := c.tracker.SetStoryMetadata(ctx, id, map[string]string{application.MoleculeField: molecule.RootID}); err != nil {
		return err
	}
	for _, step := range molecule.Steps[:len(molecule.Steps)-1] {
		c.tracker.CloseStep(step.ID)
	}
	return nil
}

// theOtherHostLandedFirst puts the other host's own story on the target branch
// at the origin, so that this story cannot land as a fast-forward.
func (c *nextContext) theOtherHostLandedFirst(branch, _ string) error {
	return c.otherHostLands(branch)
}

func (c *nextContext) otherHostLands(branch string) error {
	if err := os.WriteFile(filepath.Join(c.seed, nextOtherFile), []byte("the other host was here\n"), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"add", "-A"}, {"commit", "-qm", "The other host lands its own work"}, {"push", "-q", "origin", branch},
	} {
		if err := gitRun(c.seed, "git", args...); err != nil {
			return err
		}
	}
	return nil
}

// theOtherHostRacesThePush leaves a script beside the git wrapper that lands the
// other host's work in the instant before mw's first push — so that the push mw
// makes is refused by a remote that really has moved.
func (c *nextContext) theOtherHostRacesThePush() error {
	script := fmt.Sprintf(`#!/bin/sh
cd %s || exit 1
printf 'the other host was here\n' > %s
git add -A
git commit -qm "The other host lands its own work"
git push -q origin main
`, c.seed, nextOtherFile)
	return os.WriteFile(c.racer, []byte(script), 0o755)
}

// theOriginRefusesEveryPush is a hook on the bare origin that says what it is
// given, line by line, and declines: the push GitHub refused with a server
// error of several lines, which is not a lost race and so is never retried.
func (c *nextContext) theOriginRefusesEveryPush(said *godog.DocString) error {
	for _, line := range strings.Split(said.Content, "\n") {
		if strings.TrimSpace(line) != "" {
			c.refusal = append(c.refusal, strings.TrimSpace(line))
		}
	}
	said1 := filepath.Join(c.root, "refusal.txt")
	if err := os.WriteFile(said1, []byte(strings.Join(c.refusal, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	hook := fmt.Sprintf("#!/bin/sh\ncat %s >&2\nexit 1\n", said1)
	return os.WriteFile(filepath.Join(c.origin(), "hooks", "pre-receive"), []byte(hook), 0o755)
}

// mwClosesOut runs the use case the way mw does: the real worktrees, the real
// landing, the real merge slot, the real vault and the real ledger, with
// stand-ins only for the beads database and the terminal.
func (c *nextContext) mwClosesOut(id string) error {
	if c.ledgerBefore == nil {
		if err := c.rememberTheLedger(); err != nil {
			return err
		}
	}
	before, err := gitSay(filepath.Join(c.root, "origin.git"), "rev-parse", "main")
	if err != nil {
		return err
	}
	c.originBefore = before

	worktrees := rig.New(rig.WithProgram(c.gitProgram))
	rigs := map[string]string{"millwright": c.rig}
	files := vault.New(c.vault)

	c.report, c.err = application.Next{
		Tracker:   c.tracker,
		Worktrees: worktrees,
		Landing:   worktrees,
		Checks:    rig.NewChecks(rig.WithCommand(c.checkCommand)),
		Slot:      rig.NewSlots(rig.WithSlotWait(5*time.Second), rig.WithSlotPoll(20*time.Millisecond)),
		Vault:     files,
		Files:     c.files,
		Mailbox:   c.mailbox,
		Runner:    c.runner,
		Sync:      application.Sync{Vault: c.files, Tracker: c.tracker, Host: nextHost},
		Dispatch: application.Dispatch{
			Tracker:   c.tracker,
			Worktrees: worktrees,
			Runner:    c.runner,
			Boot: application.SeatBoot{
				Vault: files, Harness: claude.New(), Seat: nextSeat, Host: nextHost,
				After: []string{"mw", "next"},
			},
			Host: nextHost,
			Cap:  1,
			Rigs: rigs,
		},
		Seat: nextSeat,
		Host: nextHost,
		Rigs: rigs,
		Now:  func() time.Time { return time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC) },
		Out:  &c.printed,
		Err:  &c.stderr,
	}.Run(context.Background(), id)

	if c.afterFirst == nil {
		lines, err := c.ledgerLines()
		if err != nil {
			return err
		}
		c.afterFirst = lines
	}
	return nil
}

func (c *nextContext) origin() string { return filepath.Join(c.root, "origin.git") }

func (c *nextContext) theWorkIsOnTheOrigin(id, branch string) error {
	if _, err := gitSay(c.origin(), "show", branch+":"+id+".md"); err != nil {
		return fmt.Errorf("expected the work of %s on %s at the origin (the close-out said: %v): %w", id, branch, c.err, err)
	}
	if !c.report.Landed {
		return fmt.Errorf("expected the report to say it landed, got %+v (err %v)", c.report, c.err)
	}
	return nil
}

func (c *nextContext) theOtherHostsWorkIsStillThere(branch string) error {
	if _, err := gitSay(c.origin(), "show", branch+":"+nextOtherFile); err != nil {
		return fmt.Errorf("expected the other host's work to still be on %s: %w", branch, err)
	}
	return nil
}

func (c *nextContext) nothingWasLandedOn(branch string) error {
	now, err := gitSay(c.origin(), "rev-parse", branch)
	if err != nil {
		return err
	}
	if now != c.originBefore {
		return fmt.Errorf("expected %s at the origin to be untouched at %s, got %s", branch, c.originBefore, now)
	}
	if c.report.Landed {
		return fmt.Errorf("expected the report to say nothing landed, got %+v", c.report)
	}
	return nil
}

func (c *nextContext) itLandedAsAFastForward() error {
	if !c.report.How.FastForward {
		return fmt.Errorf("expected a fast-forward, got %+v", c.report.How)
	}
	return nil
}

func (c *nextContext) itLandedAsAMergeCommit() error {
	if c.report.How.FastForward {
		return fmt.Errorf("expected a merge commit, got a fast-forward: %+v", c.report.How)
	}
	return nil
}

func (c *nextContext) mwPushedTwiceAndForcedNothing() error {
	asked, err := os.ReadFile(c.gitLog)
	if err != nil {
		return fmt.Errorf("reading what mw asked git for: %w", err)
	}
	pushes := 0
	for _, line := range strings.Split(string(asked), "\n") {
		if !strings.HasPrefix(line, "push ") {
			continue
		}
		pushes++
		for _, forced := range []string{"--force", " -f ", "+refs/", "--mirror"} {
			if strings.Contains(line+" ", forced) {
				return fmt.Errorf("mw forced a push: %q", line)
			}
		}
	}
	if pushes != 2 {
		return fmt.Errorf("expected two pushes — one refused, one accepted — got %d in:\n%s", pushes, asked)
	}
	if c.report.Pushes != 2 {
		return fmt.Errorf("expected the report to say two pushes, got %d", c.report.Pushes)
	}
	return nil
}

func (c *nextContext) theRigsTestsWereRun(times int) error {
	ran := 0
	if held, err := os.ReadFile(c.checkLog); err == nil {
		ran = strings.Count(string(held), "run\n")
	}
	if ran != times {
		return fmt.Errorf("expected the rig's tests to have been run %d times, got %d", times, ran)
	}
	return nil
}

func (c *nextContext) theStoryIsClosed(id string) error {
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return err
	}
	if detail.Status != apptest.StatusClosed {
		return fmt.Errorf("expected %s to be closed, got %q (the close-out said: %v)", id, detail.Status, c.err)
	}
	if !c.report.Closed {
		return fmt.Errorf("expected the report to say %s was closed, got %+v", id, c.report)
	}
	return nil
}

func (c *nextContext) theStoryIsNotClosed(id string) error {
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return err
	}
	if detail.Status == apptest.StatusClosed {
		return fmt.Errorf("expected %s not to be closed, but it is", id)
	}
	return nil
}

func (c *nextContext) theStoryIsHeldBlocked(id string) error {
	if got := c.tracker.State(id, application.RunState); got != application.RunBlocked {
		return fmt.Errorf("expected %s to be recorded %s=%s, got %q", id, application.RunState, application.RunBlocked, got)
	}
	if c.err == nil {
		return fmt.Errorf("expected the close-out to say it stopped, but it reported no failure")
	}
	return nil
}

func (c *nextContext) theStoryCarriesACommentQuoting(id, words string) error {
	want := strings.TrimSpace(words)
	comments := c.tracker.Comments(id)
	for _, comment := range comments {
		if strings.Contains(comment, want) {
			return nil
		}
	}
	return fmt.Errorf("expected a comment on %s quoting %q, got %q", id, want, comments)
}

func (c *nextContext) theStoryCarriesNoCommentQuoting(id, words string) error {
	want := strings.TrimSpace(words)
	for _, comment := range c.tracker.Comments(id) {
		if strings.Contains(comment, want) {
			return fmt.Errorf("expected no comment on %s quoting %q, got %q", id, want, comment)
		}
	}
	return nil
}

func (c *nextContext) theLastLedgerLineHolds(table *godog.Table) error {
	lines, err := c.ledgerLines()
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		return fmt.Errorf("the ledger is empty")
	}
	last := lines[len(lines)-1]
	for _, row := range table.Rows {
		want := strings.TrimSpace(row.Cells[0].Value)
		if !strings.Contains(last, want) {
			return fmt.Errorf("expected the last ledger line to hold %q, got:\n%s", want, last)
		}
	}
	return nil
}

func (c *nextContext) theLastLedgerLineNames(id string) error {
	lines, err := c.ledgerLines()
	if err != nil {
		return err
	}
	if len(lines) == 0 || !strings.Contains(lines[len(lines)-1], id) {
		return fmt.Errorf("expected the last ledger line to name %s, got %q", id, lines)
	}
	return nil
}

func (c *nextContext) theLedgerStillHoldsEveryLine() error {
	lines, err := c.ledgerLines()
	if err != nil {
		return err
	}
	held := strings.Join(lines, "\n")
	for _, before := range c.ledgerBefore {
		if !strings.Contains(held, before) {
			return fmt.Errorf("the ledger no longer holds the line %q", before)
		}
	}
	if len(lines) != len(c.ledgerBefore)+1 {
		return fmt.Errorf("expected exactly one line to have been added to the %d there were, got %d",
			len(c.ledgerBefore), len(lines))
	}
	return nil
}

func (c *nextContext) nothingIsLeftOfTheWorktree(id string) error {
	dir := application.WorktreeDir(c.rig, id)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("expected the worktree %s to have been taken away, but it is still there", dir)
	}
	if _, err := gitSay(c.rig, "rev-parse", "--verify", "--quiet", "refs/heads/"+application.StoryBranch(id)); err == nil {
		return fmt.Errorf("expected the branch %s to have been taken away too", application.StoryBranch(id))
	}
	return nil
}

func (c *nextContext) theWorktreeIsStillThere(id string) error {
	dir := application.WorktreeDir(c.rig, id)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("expected the worktree %s to be left where it was: %w", dir, err)
	}
	return nil
}

func (c *nextContext) aFreshSessionIsRunningFor(id string) error {
	for _, name := range c.runner.Names() {
		if name == application.SessionName(id) {
			return nil
		}
	}
	return fmt.Errorf("expected a session for %s, got %q (the close-out said: %v)", id, c.runner.Names(), c.err)
}

// theTerminalHoldsAnExitedSession is the session a story was worked in, as it is
// when mw next runs: its command has ended, and remain-on-exit keeps it on screen.
func (c *nextContext) theTerminalHoldsAnExitedSession(id string) error {
	name := application.SessionName(id)
	err := c.runner.Start(context.Background(), application.SessionSpec{Name: name, Command: []string{"claude"}})
	if err != nil {
		return err
	}
	c.runner.Exit(name, 0)
	return nil
}

func (c *nextContext) theTerminalHoldsNoSession(id string) error {
	status, err := c.runner.Status(context.Background(), application.SessionName(id))
	if err != nil {
		return err
	}
	if status.State != application.StateGone {
		return fmt.Errorf("expected the session of %s to have been closed, got %+v (the close-out said: %v)", id, status, c.err)
	}
	return nil
}

func (c *nextContext) theSessionIsStillThereExited(id string) error {
	status, err := c.runner.Status(context.Background(), application.SessionName(id))
	if err != nil {
		return err
	}
	if !status.Finished() {
		return fmt.Errorf("expected the session of %s to be left exited on the terminal, got %+v", id, status)
	}
	return nil
}

func (c *nextContext) noFreshSessionWasStarted() error {
	if names := c.runner.Names(); len(names) != 0 {
		return fmt.Errorf("expected nothing new to have been started, got %q", names)
	}
	if c.report.Dispatched {
		return fmt.Errorf("expected no dispatch at all, but one was run: %+v", c.report.Dispatch)
	}
	return nil
}

// theTrackerRefusesToClose makes the tracker turn a close down the way beads
// turns down a close by an actor that is not the story's assignee — which is
// how a close-out comes to land a story it cannot close.
func (c *nextContext) theTrackerRefusesToClose(id, why string) error {
	c.tracker.RefuseToClose(id, strings.TrimSpace(why))
	return nil
}

func (c *nextContext) theTrackerTakesACloseAgain(id string) error {
	c.tracker.RefuseToClose(id, "")
	return nil
}

// landedButStillOpen is the state a person has to be told about plainly: the
// work is on the target branch and nothing will undo it, but the story is still
// open, and running mw next again is what closes it.
func (c *nextContext) landedButStillOpen() error {
	if !c.report.Landed || c.report.Closed {
		return fmt.Errorf("expected a report saying landed and not closed, got %+v", c.report)
	}
	if c.err == nil {
		return fmt.Errorf("expected the close-out to report a failure, it reported none")
	}
	said := c.printed.String()
	for _, want := range []string{"landed but still open", "mw next " + c.report.StoryID} {
		if !strings.Contains(said, want) {
			return fmt.Errorf("expected the report to say %q, got:\n%s", want, said)
		}
	}
	return nil
}

func (c *nextContext) theLedgerHoldsOneLineFor(id string) error {
	lines, err := c.ledgerLines()
	if err != nil {
		return err
	}
	held := 0
	for _, line := range lines {
		if strings.Contains(line, id) {
			held++
		}
	}
	if held != 1 {
		return fmt.Errorf("expected exactly one ledger line for %s, got %d in:\n%s", id, held, strings.Join(lines, "\n"))
	}
	return nil
}

// mergedOncePushedOnce reads what git was really asked to do, across every run
// of the close-out in this scenario: a story landed once is merged once and
// pushed once, however many times mw next is run afterwards. The landing's own
// merge is the one counted (`merge --no-edit`, made in the landing worktree):
// the `merge --ff-only` that brings the rig checkout level is not a landing.
func (c *nextContext) mergedOncePushedOnce() error {
	asked, err := os.ReadFile(c.gitLog)
	if err != nil {
		return fmt.Errorf("reading what mw asked git for: %w", err)
	}
	merges, pushes := 0, 0
	for _, line := range strings.Split(string(asked), "\n") {
		switch {
		case strings.HasPrefix(line, "merge --no-edit "):
			merges++
		case strings.HasPrefix(line, "push "):
			pushes++
		}
	}
	if merges != 1 || pushes != 1 {
		return fmt.Errorf("expected one merge and one push, got %d and %d in:\n%s", merges, pushes, asked)
	}
	return nil
}

// theVaultHoldsOtherWork is a vault file nobody committed that is none of mw's
// business: not the seat's ledger and not the rig memory, so a close-out leaves
// it exactly where it is and the sync that follows refuses because of it.
func (c *nextContext) theVaultHoldsOtherWork(path string) error {
	c.files.Dirty = append(c.files.Dirty, path)
	return nil
}

func (c *nextContext) theVaultRefusesACommit(why string) error {
	c.files.CommitErr = fmt.Errorf("%s", strings.TrimSpace(why))
	return nil
}

func (c *nextContext) theVaultTakesACommitAgain() error {
	c.files.CommitErr = nil
	return nil
}

// mwCommittedToTheVaultExactly is the whole of the rule: a close-out commits
// the seat's ledger and that seat's memory of the story's rig, by path, and
// nothing else — never everything the vault happens to hold.
func (c *nextContext) mwCommittedToTheVaultExactly(table *godog.Table) error {
	commit, err := c.lastVaultCommit()
	if err != nil {
		return err
	}
	var want []string
	for _, row := range table.Rows {
		want = append(want, strings.TrimSpace(row.Cells[0].Value))
	}
	if strings.Join(commit.Paths, "\n") != strings.Join(want, "\n") {
		return fmt.Errorf("expected mw to commit exactly %q, got %q", want, commit.Paths)
	}
	if said := c.printed.String(); !strings.Contains(said, want[0]) {
		return fmt.Errorf("expected the report to say what was committed, got:\n%s", said)
	}
	return nil
}

// theVaultCommitNames is the message: it says which story the work belongs to,
// and it is signed by nobody — least of all by a machine.
func (c *nextContext) theVaultCommitNames(id string) error {
	commit, err := c.lastVaultCommit()
	if err != nil {
		return err
	}
	for _, want := range []string{id, "The story " + id} {
		if !strings.Contains(commit.Message, want) {
			return fmt.Errorf("expected the commit message to hold %q, got %q", want, commit.Message)
		}
	}
	if signature := application.AIAttribution(commit.Message); signature != "" {
		return fmt.Errorf("the commit message is signed by a machine: %q", signature)
	}
	return nil
}

// lastVaultCommit is the commit mw made in the vault most recently, which is
// the one the scenario running now is asking about.
func (c *nextContext) lastVaultCommit() (apptest.VaultCommit, error) {
	made := c.files.Commits()
	if len(made) == 0 {
		return apptest.VaultCommit{}, fmt.Errorf("mw committed nothing to the vault (the close-out said: %v)", c.err)
	}
	return made[len(made)-1], nil
}

func (c *nextContext) theHostsWereBroughtLevel() error {
	if !c.report.Synced {
		return fmt.Errorf("expected the hosts to have been brought level, got %+v (the close-out said: %v)", c.report, c.err)
	}
	return nil
}

// theHostsCouldNotBeBroughtLevel is the refusal that stands: a vault file mw
// may not commit is still a sync that stops, naming the file — and the story is
// landed and closed regardless, because none of that is undone.
func (c *nextContext) theHostsCouldNotBeBroughtLevel(path string) error {
	if c.err == nil {
		return fmt.Errorf("expected the close-out to say the hosts could not be brought level, it reported no failure")
	}
	for _, want := range []string{"could not be brought level", path} {
		if !strings.Contains(c.err.Error(), want) {
			return fmt.Errorf("expected the failure to hold %q, got %v", want, c.err)
		}
	}
	return nil
}

func (c *nextContext) theVaultCommitFailureIsNoted() error {
	said := c.printed.String()
	if !strings.Contains(said, "could not be committed") {
		return fmt.Errorf("expected the report to note that the vault could not be committed, got:\n%s", said)
	}
	return nil
}

func (c *nextContext) theVaultCommitDoesNotHold(path string) error {
	commit, err := c.lastVaultCommit()
	if err != nil {
		return err
	}
	for _, committed := range commit.Paths {
		if committed == path {
			return fmt.Errorf("expected mw not to commit %s, but it committed %q", path, commit.Paths)
		}
	}
	return nil
}

// theRunRecordWasMissing is the close-out saying, in its report, that the one
// file the story's fuel is evidenced by was not there to commit.
func (c *nextContext) theRunRecordWasMissing(id string) error {
	record := path.Join(vault.RunsDir, id, application.ResultFileName)
	said := c.printed.String()
	for _, want := range []string{record, "missing"} {
		if !strings.Contains(said, want) {
			return fmt.Errorf("expected the report to say the run record %s was missing, got:\n%s", record, said)
		}
	}
	return nil
}

// theLedgerLineHoldsOnlyTheFirstRefusalLine is the ledger's rule: a row is one
// line, so the rest of what git said is nowhere in it.
func (c *nextContext) theLedgerLineHoldsOnlyTheFirstRefusalLine() error {
	lines, err := c.ledgerLines()
	if err != nil {
		return err
	}
	if len(lines) != len(c.ledgerBefore)+1 {
		return fmt.Errorf("expected the close-out to add one ledger line, got %d before and %d after", len(c.ledgerBefore), len(lines))
	}
	last := lines[len(lines)-1]
	if !strings.Contains(last, "not landed") || !strings.Contains(last, c.refusal[0]) {
		return fmt.Errorf("expected the ledger line to say not landed and hold %q, got:\n%s", c.refusal[0], last)
	}
	for _, later := range c.refusal[1:] {
		if strings.Contains(last, later) {
			return fmt.Errorf("expected the ledger line to hold only the first line of the refusal, but it holds %q:\n%s", later, last)
		}
	}
	return nil
}

// theRunHoldsTheWholeLandingError reads the file beside the result, which is
// the one place the whole of git's stderr is kept.
func (c *nextContext) theRunHoldsTheWholeLandingError(id string) error {
	kept, err := os.ReadFile(filepath.Join(c.vault, vault.RunsDir, id, "landing-error.txt"))
	if err != nil {
		return fmt.Errorf("expected the run of %s to hold the landing error: %w", id, err)
	}
	for _, line := range c.refusal {
		if !strings.Contains(string(kept), line) {
			return fmt.Errorf("expected the landing error to hold %q, got:\n%s", line, kept)
		}
	}
	return nil
}

func (c *nextContext) theCommentQuotesTheWholeRefusal(id string) error {
	comments := strings.Join(c.tracker.Comments(id), "\n")
	for _, line := range c.refusal {
		if !strings.Contains(comments, line) {
			return fmt.Errorf("expected a comment on %s quoting %q, got:\n%s", id, line, comments)
		}
	}
	return nil
}

// theMergeSlotIsFree says the slot can be taken, which is the only honest way
// to ask: an advisory lock is held by the kernel, not by what the file says.
func (c *nextContext) theMergeSlotIsFree() error {
	held, err := rig.NewSlots(rig.WithSlotWait(time.Second), rig.WithSlotPoll(20*time.Millisecond)).
		Take(context.Background(), c.rig, "the scenario")
	if err != nil {
		return fmt.Errorf("expected the merge slot of the rig to be free: %w", err)
	}
	return held.Release(context.Background())
}

// linesFor are the ledger lines that name the story, in the order they were written.
func (c *nextContext) linesFor(id string) ([]string, error) {
	lines, err := c.ledgerLines()
	if err != nil {
		return nil, err
	}
	var named []string
	for _, line := range lines {
		if application.LedgerNamesStory(line, id) {
			named = append(named, line)
		}
	}
	return named, nil
}

func (c *nextContext) theLedgerHoldsLinesFor(count int, id string) error {
	named, err := c.linesFor(id)
	if err != nil {
		return err
	}
	if len(named) != count {
		return fmt.Errorf("expected %d ledger lines for %s, got %d in:\n%s", count, id, len(named), strings.Join(named, "\n"))
	}
	return nil
}

func (c *nextContext) theFirstLedgerLineForHolds(id string, table *godog.Table) error {
	named, err := c.linesFor(id)
	if err != nil {
		return err
	}
	if len(named) == 0 {
		return fmt.Errorf("the ledger holds no line for %s", id)
	}
	for _, row := range table.Rows {
		want := strings.TrimSpace(row.Cells[0].Value)
		if !strings.Contains(named[0], want) {
			return fmt.Errorf("expected the first ledger line for %s to hold %q, got:\n%s", id, want, named[0])
		}
	}
	return nil
}

// theLastLedgerLineHoldsNoTokenFigure is what keeps a report that sums the
// ledger from adding a session's fuel a second time: nothing in the line reads
// back as tokens.
func (c *nextContext) theLastLedgerLineHoldsNoTokenFigure() error {
	lines, err := c.ledgerLines()
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		return fmt.Errorf("the ledger is empty")
	}
	last := lines[len(lines)-1]
	if row, ok := application.ParseLedgerRow(last); ok {
		return fmt.Errorf("expected no token figure on the last ledger line, but it reads back as %d tokens:\n%s", row.Tokens, last)
	}
	if strings.Contains(last, "tokens") {
		return fmt.Errorf("expected no token figure on the last ledger line, got:\n%s", last)
	}
	return nil
}

func (c *nextContext) theLedgerIsUnchangedSinceTheFirstCloseOut() error {
	lines, err := c.ledgerLines()
	if err != nil {
		return err
	}
	if len(lines) < len(c.afterFirst) {
		return fmt.Errorf("the ledger lost lines: it held %d after the first close-out and holds %d now", len(c.afterFirst), len(lines))
	}
	for i, was := range c.afterFirst {
		if lines[i] != was {
			return fmt.Errorf("line %d of the ledger was rewritten:\nwas: %s\nnow: %s", i+1, was, lines[i])
		}
	}
	return nil
}

// mwStatusCountsTodaysFuelAs runs mw status against the vault the scenario has
// been closing stories out in, on the day the close-outs were dated.
func (c *nextContext) mwStatusCountsTodaysFuelAs(want string) error {
	report, err := application.Status{
		Tracker: c.tracker,
		Vault:   vault.New(c.vault),
		Host:    nextHost,
		Seat:    nextSeat,
		Now:     func() time.Time { return time.Date(2026, 9, 18, 18, 0, 0, 0, time.UTC) },
	}.Run(context.Background())
	if err != nil {
		return fmt.Errorf("reading status: %w", err)
	}
	if got := fmt.Sprintf("%s tokens", application.Thousands(report.FuelToday)); got != want {
		return fmt.Errorf("expected mw status to count today's fuel as %q, got %q", want, got)
	}
	return nil
}
