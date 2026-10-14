package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

const suiteRigsDir = "/rigs"

// suiteContext holds the fakes mw suite runs through: a worktree maker that
// makes none, a runner that prints what a scenario told it to, the files the
// result is written to and the vault's commits. Nothing here runs a command.
type suiteContext struct {
	trees   *apptest.FakeSuiteTrees
	runner  *apptest.FakeSuiteRunner
	records *apptest.FakeSuiteRecords
	files   *apptest.FakeVaultFiles
	heads   *apptest.FakeRigHeads
	load    *apptest.FakeHostLoad

	tests    map[string]string
	specs    map[string]string
	main     string
	day      time.Time
	bound    time.Duration
	hasLoad  bool
	whole    map[string]string // what the whole run of tests/specs printed
	failed   map[string]bool   // whether the whole run of tests/specs failed
	alone    map[string]bool   // file -> fails when run alone
	rerun    map[string]string // file -> what a passing re-run of it prints
	readsAt1 int               // load reads made before the first command was run

	// The --file side: the rig's suite epic and the tracker and mailbox the
	// filing goes through.
	tracker   *apptest.FakeTracker
	mailbox   *apptest.FakeMailbox
	rules     *apptest.FakeEpicRules
	epic      string
	epicPath  domain.Path
	preFiled  map[string]bool // ids of the beads the scenario began with
	suiteFile bool

	// sessions and dispatch are what mw dispatch started and reported, in the
	// scenarios that run it (mw-gq6.414).
	sessions *apptest.FakeRunner
	dispatch application.DispatchReport

	out    bytes.Buffer
	report application.SuiteReport
	err    error
}

// InitializeSuiteScenario registers the steps of features/suite.feature.
func InitializeSuiteScenario(ctx *godog.ScenarioContext) {
	c := &suiteContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = suiteContext{
			trees:    &apptest.FakeSuiteTrees{},
			runner:   &apptest.FakeSuiteRunner{},
			records:  &apptest.FakeSuiteRecords{},
			files:    &apptest.FakeVaultFiles{},
			heads:    apptest.NewFakeRigHeads(),
			load:     &apptest.FakeHostLoad{},
			tests:    map[string]string{},
			specs:    map[string]string{},
			whole:    map[string]string{},
			failed:   map[string]bool{},
			alone:    map[string]bool{},
			rerun:    map[string]string{},
			tracker:  apptest.NewFakeTracker(),
			mailbox:  apptest.NewFakeMailbox(),
			rules:    apptest.NewFakeEpicRules(),
			preFiled: map[string]bool{},
		}
		return ctx, nil
	})

	ctx.Given(`^the suite rig "([^"]*)" whose tests are "([^"]*)"$`, c.aRigWhoseTestsAre)
	ctx.Given(`^the suite rig "([^"]*)" with no tests entry$`, c.aRigWithNoTests)
	ctx.Given(`^the suite rig "([^"]*)" has the changed-specs command "([^"]*)"$`, c.aRigHasChangedSpecs)
	ctx.Given(`^the suite finds the origin's main at "([^"]*)"$`, c.theOriginsMainIsAt)
	ctx.Given(`^the suite is run on the day "([^"]*)" at (\d+):00 UTC$`, c.theSuiteIsRunOn)
	ctx.Given(`^the whole run of the (tests|specs) prints:$`, c.theWholeRunPrints)
	ctx.Given(`^the (tests|specs) fail$`, c.theWholeRunFails)
	ctx.Given(`^alone, "([^"]*)" (fails again|passes)$`, c.aloneItGoes)
	ctx.Given(`^the re-run of "([^"]*)" on main prints "([^"]*)" and passes$`, c.aRerunPasses)
	ctx.Given(`^the suite epic "([^"]*)" holds the ready bug "([^"]*)" labelled "([^"]*)", whose files key is "([^"]*)"$`, c.theEpicHoldsAReadyBug)
	ctx.When(`^mw dispatch runs on "([^"]*)" for "([^"]*)"$`, c.mwDispatchRuns)
	ctx.Then(`^the bug "([^"]*)" is closed with the reason "([^"]*)"$`, c.theBugIsClosedWith)
	ctx.Then(`^the bug "([^"]*)" has a comment holding "([^"]*)"$`, c.theBugHasCommentHolding)
	ctx.Then(`^dispatch started no session and "([^"]*)" is claimed by nobody$`, c.dispatchStartedNothing)
	ctx.Then(`^the dispatch printed: (.+)$`, c.theDispatchPrinted)
	ctx.Given(`^the host is at load ([\d.]+) of (\d+) cores, then ([\d.]+), then ([\d.]+)$`, c.theHostCalms)
	ctx.Given(`^the host is always at load ([\d.]+) of (\d+) cores$`, c.theHostStaysBusy)
	ctx.Given(`^the suite waits at most (\d+) hours? for the host to calm$`, c.theCalmBound)

	ctx.Given(`^the rig "([^"]*)" says its suite epic is "([^"]*)", whose path is (\S+)$`, c.theRigSaysItsSuiteEpic)
	ctx.Given(`^the suite epic holds the (held|closed) bug "([^"]*)" titled "([^"]*)", labelled bug and suite$`, c.theSuiteEpicHoldsABug)

	ctx.When(`^mw suite runs "([^"]*)"$`, c.mwSuiteRuns)
	ctx.When(`^mw suite runs "([^"]*)" with --file$`, c.mwSuiteRunsWithFile)

	ctx.Then(`^the suite exits (\d)$`, c.theSuiteExits)
	ctx.Then(`^the suite ran "([^"]*)" in a worktree named "([^"]*)" beside the rig, not in the rig's own checkout$`, c.theSuiteRanInTheWorktree)
	ctx.Then(`^the suite wrote the log and the result of "([^"]*)" for "([^"]*)"$`, c.theSuiteWrote)
	ctx.Then(`^the suite result names the sha "([^"]*)" and no failing file$`, c.theResultNamesTheShaAndNoFailure)
	ctx.Then(`^the suite committed both files$`, c.theSuiteCommitted)
	ctx.Then(`^the suite's worktree is gone$`, c.theWorktreeIsGone)
	ctx.Then(`^the suite prints: (.+)$`, c.theSuitePrints)
	ctx.Then(`^the suite ran "([^"]*)" alone$`, c.theSuiteRanAlone)
	ctx.Then(`^the suite result lists "([^"]*)" as failed twice$`, c.theResultListsFailedTwice)
	ctx.Then(`^the suite result lists "([^"]*)" as flaky$`, c.theResultListsFlaky)
	ctx.Then(`^the suite result names "([^"]*)" as the first failing test of "([^"]*)"$`, c.theResultNamesTheFirstTest)
	ctx.Then(`^the suite result quotes "([^"]*)" for "([^"]*)"$`, c.theResultQuotes)
	ctx.Then(`^the suite read the load (\d+) times before it ran anything$`, c.theSuiteReadTheLoad)
	ctx.Then(`^the suite result records the load at start as ([\d.]+) of (\d+) cores$`, c.theResultRecordsTheLoad)
	ctx.Then(`^the suite result says it did not give up waiting$`, c.theResultDidNotGiveUp)
	ctx.Then(`^the suite result says it gave up waiting after (\d+) hours? at load ([\d.]+) of (\d+) cores$`, c.theResultGaveUp)
	ctx.Then(`^the suite ran "([^"]*)" after "([^"]*)"$`, c.theSuiteRanAfter)
	ctx.Then(`^the suite ran exactly (\d+) command$`, c.theSuiteRanExactly)
	ctx.Then(`^the suite is refused, saying: (.+)$`, c.theSuiteIsRefused)
	ctx.Then(`^the suite made no worktree, ran nothing and wrote nothing$`, c.theSuiteDidNothing)
	ctx.Then(`^the suite epic "([^"]*)" holds (\d+) new stor(?:y|ies)$`, c.theEpicHoldsNew)
	ctx.Then(`^the new story for "([^"]*)" is titled "([^"]*)"$`, c.theNewStoryIsTitled)
	ctx.Then(`^the new story for "([^"]*)" is held and labelled bug and suite$`, c.theNewStoryIsHeld)
	ctx.Then(`^the new story for "([^"]*)" has the files "([^"]*)" and the epic's path keys$`, c.theNewStoryHasFiles)
	ctx.Then(`^the new story for "([^"]*)" describes "([^"]*)", "([^"]*)", "([^"]*)", "([^"]*)" and "([^"]*)"$`, c.theNewStoryDescribes)
	ctx.Then(`^the new story for "([^"]*)" has the acceptance "([^"]*)"$`, c.theNewStoryHasAcceptance)
	ctx.Then(`^"([^"]*)" has one comment dated "([^"]*)" saying "([^"]*)", "([^"]*)" and "([^"]*)"$`, c.theBugHasADatedComment)
	ctx.Then(`^"([^"]*)" has no comment$`, c.theBugHasNoComment)
	ctx.Then(`^the suite sent one mail to the mayor, subject "([^"]*)"$`, c.theSuiteSentOneMail)
	ctx.Then(`^the suite sent no mail$`, c.theSuiteSentNoMail)
	ctx.Then(`^the mail names each new story with its file$`, c.theMailNamesEachNewStory)
	ctx.Then(`^the mail says "([^"]*)" with "([^"]*)" was commented and the other file was new$`, c.theMailSaysWhichIsWhich)
}

func (c *suiteContext) aRigWhoseTestsAre(rig, tests string) error {
	c.tests[rig] = tests
	c.heads.SetHead(path.Join(suiteRigsDir, rig), "unset")
	return nil
}

func (c *suiteContext) aRigWithNoTests(rig string) error {
	c.heads.SetHead(path.Join(suiteRigsDir, rig), "unset")
	return nil
}

func (c *suiteContext) aRigHasChangedSpecs(rig, command string) error {
	c.specs[rig] = command
	return nil
}

func (c *suiteContext) theOriginsMainIsAt(sha string) error {
	c.main = sha
	return nil
}

func (c *suiteContext) theSuiteIsRunOn(day string, hour int) error {
	at, err := time.Parse("2006-01-02", day)
	if err != nil {
		return err
	}
	c.day = at.Add(time.Duration(hour) * time.Hour)
	return nil
}

func (c *suiteContext) theWholeRunPrints(which string, doc *godog.DocString) error {
	c.whole[which] = doc.Content
	return nil
}

func (c *suiteContext) theWholeRunFails(which string) error {
	c.failed[which] = true
	return nil
}

func (c *suiteContext) aloneItGoes(file, how string) error {
	c.alone[file] = how == "fails again"
	return nil
}

func (c *suiteContext) theHostCalms(first float64, cores int, second, third float64) error {
	c.hasLoad = true
	for _, load := range []float64{first, second, third} {
		c.load.Readings = append(c.load.Readings, application.LoadReading{Load: load, Cores: cores})
	}
	return nil
}

func (c *suiteContext) theHostStaysBusy(load float64, cores int) error {
	c.hasLoad = true
	c.load.Reading = application.LoadReading{Load: load, Cores: cores}
	return nil
}

func (c *suiteContext) theCalmBound(hours int) error {
	c.bound = time.Duration(hours) * time.Hour
	return nil
}

// respond is what the fake runner prints for a command line: the whole run of
// the tests or of the specs, or one file run alone.
func (c *suiteContext) respond(rig string) func(dir, command string) (application.Checked, error) {
	return func(dir, command string) (application.Checked, error) {
		if c.readsAt1 == 0 {
			c.readsAt1 = c.load.Reads
		}
		for file, out := range c.rerun {
			if command == c.tests[rig]+" "+file || (c.specs[rig] != "" && command == strings.ReplaceAll(c.specs[rig], application.SpecsPlaceholder, file)) {
				return application.Checked{Passed: true, Output: out + "\n"}, nil
			}
		}
		for file, fails := range c.alone {
			if command == c.tests[rig]+" "+file || (c.specs[rig] != "" && command == strings.ReplaceAll(c.specs[rig], application.SpecsPlaceholder, file)) {
				out := "alone: " + file + "\n"
				if fails {
					out += "FAIL  " + file + " > again\n"
				}
				return application.Checked{Passed: !fails, Output: out}, nil
			}
		}
		switch {
		case command == c.tests[rig]:
			return application.Checked{Passed: !c.failed["tests"], Output: c.whole["tests"]}, nil
		case c.specs[rig] != "" && command == strings.ReplaceAll(c.specs[rig], application.SpecsPlaceholder, ""):
			return application.Checked{Passed: !c.failed["specs"], Output: c.whole["specs"]}, nil
		}
		return application.Checked{}, fmt.Errorf("the fake runner was not told of %q", command)
	}
}

func (c *suiteContext) mwSuiteRuns(rig string) error {
	c.suiteFile = false
	return c.run(rig)
}

func (c *suiteContext) mwSuiteRunsWithFile(rig string) error {
	c.suiteFile = true
	return c.run(rig)
}

func (c *suiteContext) newSuite(rig string) application.Suite {
	if c.main == "" {
		c.main = "0000000"
	}
	rigDir := path.Join(suiteRigsDir, rig)
	c.heads.SetHead(rigDir, c.main)
	c.runner.Respond = c.respond(rig)
	suite := application.Suite{
		Trees:        c.trees,
		Heads:        c.heads,
		Runner:       c.runner,
		Records:      c.records,
		Files:        c.files,
		LoadBound:    c.bound,
		LoadWait:     func(context.Context, time.Duration) error { return nil },
		Now:          func() time.Time { return c.day },
		Host:         "desktop",
		Rigs:         map[string]string{rig: rigDir},
		Tests:        c.tests,
		ChangedSpecs: c.specs,
		Out:          &c.out,
	}
	if c.hasLoad {
		suite.Load = c.load
	}
	return suite
}

func (c *suiteContext) run(rig string) error {
	suite := c.newSuite(rig)
	if c.suiteFile {
		suite.Filer = &application.SuiteFiler{Tracker: c.tracker, Rules: c.rules, Mailbox: c.mailbox, Host: "desktop"}
	}
	c.report, c.err = suite.Run(context.Background(), rig)
	return nil
}

func (c *suiteContext) theSuiteExits(want int) error {
	got := 0
	switch {
	case c.err != nil:
		got = 2
	case c.report.Failed:
		got = 1
	}
	if got != want {
		return fmt.Errorf("expected mw suite to exit %d, it exited %d (error: %v, printed:\n%s)", want, got, c.err, c.out.String())
	}
	return nil
}

func (c *suiteContext) ranIn() ([]apptest.SuiteRan, error) {
	ran := c.runner.Ran()
	if len(ran) == 0 {
		return nil, fmt.Errorf("the suite ran nothing (error: %v)", c.err)
	}
	return ran, nil
}

func (c *suiteContext) theSuiteRanInTheWorktree(command, name string) error {
	ran, err := c.ranIn()
	if err != nil {
		return err
	}
	rigDir := suiteRigsDir + "/lampas"
	want := path.Join(suiteRigsDir, application.WorktreesDir, name)
	if ran[0].Command != command || ran[0].Dir != want {
		return fmt.Errorf("expected %q in %s, it ran %q in %s", command, want, ran[0].Command, ran[0].Dir)
	}
	if ran[0].Dir == rigDir {
		return fmt.Errorf("the suite ran in the rig's own checkout")
	}
	added := c.trees.Added()
	if len(added) != 1 || added[0].Dir != want || added[0].Commit != c.main || added[0].RigDir != rigDir {
		return fmt.Errorf("expected one worktree at %s detached at %s, got %+v", want, c.main, added)
	}
	return nil
}

func suitePaths(rig, day string) (log, result string) {
	return path.Join(application.SuiteDir, rig, day+".log"), path.Join(application.SuiteDir, rig, day+".json")
}

func (c *suiteContext) theSuiteWrote(rig, day string) error {
	log, result := suitePaths(rig, day)
	for _, at := range []string{log, result} {
		if _, there := c.records.File(at); !there {
			return fmt.Errorf("expected %s to be written, got %v", at, c.records.Paths())
		}
	}
	return nil
}

// result reads the .json the suite wrote.
func (c *suiteContext) result() (application.SuiteResult, error) {
	var result application.SuiteResult
	_, at := suitePaths("lampas", c.day.Format("2006-01-02"))
	written, there := c.records.File(at)
	if !there {
		return result, fmt.Errorf("no result written at %s (error: %v)", at, c.err)
	}
	if err := json.Unmarshal([]byte(written), &result); err != nil {
		return result, fmt.Errorf("the result is not JSON: %w:\n%s", err, written)
	}
	return result, nil
}

func (c *suiteContext) theResultNamesTheShaAndNoFailure(sha string) error {
	result, err := c.result()
	if err != nil {
		return err
	}
	if result.Sha != sha || len(result.Failing) != 0 || !result.Passed {
		return fmt.Errorf("expected sha %s, passed, no failing file, got %+v", sha, result)
	}
	return nil
}

func (c *suiteContext) theSuiteCommitted() error {
	log, result := suitePaths("lampas", c.day.Format("2006-01-02"))
	commits := c.files.Commits()
	if len(commits) != 1 || len(commits[0].Paths) != 2 || commits[0].Paths[0] != log || commits[0].Paths[1] != result {
		return fmt.Errorf("expected one vault commit of %s and %s, got %+v", log, result, commits)
	}
	return nil
}

func (c *suiteContext) theWorktreeIsGone() error {
	if c.trees.Live() != 0 || len(c.trees.Removed()) == 0 {
		return fmt.Errorf("expected the suite's worktree to be removed, %d are left (removed %v)", c.trees.Live(), c.trees.Removed())
	}
	return nil
}

func (c *suiteContext) theSuitePrints(want string) error {
	if !strings.Contains(c.out.String(), want) {
		return fmt.Errorf("expected mw suite to print %q, it printed:\n%s", want, c.out.String())
	}
	return nil
}

func (c *suiteContext) theSuiteRanAlone(command string) error {
	for _, ran := range c.runner.Ran() {
		if ran.Command == command {
			return nil
		}
	}
	return fmt.Errorf("expected %q to be run, ran %+v", command, c.runner.Ran())
}

func (c *suiteContext) file(file string) (application.SuiteFile, error) {
	result, err := c.result()
	if err != nil {
		return application.SuiteFile{}, err
	}
	for _, failing := range result.Failing {
		if failing.File == file {
			return failing, nil
		}
	}
	return application.SuiteFile{}, fmt.Errorf("%s is not among the failing files: %+v", file, result.Failing)
}

func (c *suiteContext) theResultListsFailedTwice(file string) error {
	failing, err := c.file(file)
	if err != nil {
		return err
	}
	if failing.Flaky || failing.Alone != "failed" {
		return fmt.Errorf("expected %s to have failed twice, got %+v", file, failing)
	}
	return nil
}

func (c *suiteContext) theResultListsFlaky(file string) error {
	failing, err := c.file(file)
	if err != nil {
		return err
	}
	if !failing.Flaky || failing.Alone != "passed" {
		return fmt.Errorf("expected %s to be flaky, got %+v", file, failing)
	}
	return nil
}

func (c *suiteContext) theResultNamesTheFirstTest(test, file string) error {
	failing, err := c.file(file)
	if err != nil {
		return err
	}
	if failing.Test != test {
		return fmt.Errorf("expected the first failing test of %s to be %q, got %q", file, test, failing.Test)
	}
	return nil
}

func (c *suiteContext) theResultQuotes(quote, file string) error {
	failing, err := c.file(file)
	if err != nil {
		return err
	}
	if !strings.Contains(failing.Error, quote) {
		return fmt.Errorf("expected the error of %s to hold %q, got %q", file, quote, failing.Error)
	}
	return nil
}

func (c *suiteContext) theSuiteReadTheLoad(times int) error {
	if c.readsAt1 != times {
		return fmt.Errorf("expected %d load readings before the first command, got %d", times, c.readsAt1)
	}
	return nil
}

func (c *suiteContext) theResultRecordsTheLoad(load float64, cores int) error {
	result, err := c.result()
	if err != nil {
		return err
	}
	if result.LoadAtStart == nil || result.LoadAtStart.Load != load || result.LoadAtStart.Cores != cores {
		return fmt.Errorf("expected the load at start %.1f of %d cores, got %+v", load, cores, result.LoadAtStart)
	}
	return nil
}

func (c *suiteContext) theResultDidNotGiveUp() error {
	result, err := c.result()
	if err != nil {
		return err
	}
	if result.GaveUpWaiting {
		return fmt.Errorf("expected the suite not to have given up waiting, got %+v", result)
	}
	return nil
}

func (c *suiteContext) theResultGaveUp(hours int, load float64, cores int) error {
	result, err := c.result()
	if err != nil {
		return err
	}
	want := fmt.Sprintf("load %.1f of %d cores", load, cores)
	if !result.GaveUpWaiting || result.WaitedSeconds != float64(hours*3600) || !strings.Contains(result.Note, want) {
		return fmt.Errorf("expected to have given up after %d hours at %s, got %+v", hours, want, result)
	}
	return nil
}

func (c *suiteContext) theSuiteRanAfter(command, first string) error {
	ran := c.runner.Ran()
	if len(ran) < 2 || ran[0].Command != first || ran[1].Command != command {
		return fmt.Errorf("expected %q and then %q, ran %+v", first, command, ran)
	}
	return nil
}

func (c *suiteContext) theSuiteRanExactly(n int) error {
	if got := len(c.runner.Ran()); got != n {
		return fmt.Errorf("expected %d commands, ran %d: %+v", n, got, c.runner.Ran())
	}
	return nil
}

func (c *suiteContext) theSuiteIsRefused(why string) error {
	if c.err == nil || !strings.Contains(c.err.Error(), why) {
		return fmt.Errorf("expected a refusal saying %q, got %v", why, c.err)
	}
	return nil
}

func (c *suiteContext) theSuiteDidNothing() error {
	if len(c.trees.Added()) != 0 || len(c.runner.Ran()) != 0 || len(c.records.Paths()) != 0 || len(c.files.Commits()) != 0 {
		return fmt.Errorf("expected nothing done, got worktrees %v, commands %v, files %v, commits %v",
			c.trees.Added(), c.runner.Ran(), c.records.Paths(), c.files.Commits())
	}
	return nil
}

func (c *suiteContext) theRigSaysItsSuiteEpic(rig, epic, path string) error {
	fields := strings.Split(path, "/")
	if len(fields) != 7 {
		return fmt.Errorf("a path of seven keys is rig/branch/harness/model/effort/formula/host, got %q", path)
	}
	c.epic = epic
	c.epicPath = domain.Path{Rig: fields[0], Branch: fields[1], Harness: domain.Harness(fields[2]), Model: domain.Model(fields[3]),
		Effort: domain.Effort(fields[4]), Formula: fields[5], Host: fields[6]}
	c.tracker.AddEpic(epic, c.epicPath)
	c.rules.Require(rig, domain.EpicRequirements{NightlySuite: true, SuiteEpic: epic})
	return nil
}

func (c *suiteContext) theSuiteEpicHoldsABug(state, id, title string) error {
	c.tracker.AddStory(c.epic, domain.Story{ID: id, Title: title})
	if err := c.tracker.SetLabels(id, "bug", "suite"); err != nil {
		return err
	}
	status := apptest.StatusDeferred
	if state == "closed" {
		status = apptest.StatusClosed
	}
	c.preFiled[id] = true
	return c.tracker.SetStatus(id, status)
}

// newStories are the stories of the suite epic the run filed.
func (c *suiteContext) newStories() ([]application.StoryDetail, error) {
	epic, err := c.tracker.ShowEpic(context.Background(), c.epic)
	if err != nil {
		return nil, err
	}
	var filed []application.StoryDetail
	for _, story := range epic.Stories {
		if !c.preFiled[story.Story.ID] {
			filed = append(filed, story)
		}
	}
	return filed, nil
}

func (c *suiteContext) newStoryFor(file string) (application.StoryDetail, error) {
	filed, err := c.newStories()
	if err != nil {
		return application.StoryDetail{}, err
	}
	for _, story := range filed {
		if strings.Contains(story.Story.Title, " "+file+" ") {
			return story, nil
		}
	}
	return application.StoryDetail{}, fmt.Errorf("no new story names %s (error: %v): %+v", file, c.err, filed)
}

func (c *suiteContext) theEpicHoldsNew(epic string, n int) error {
	filed, err := c.newStories()
	if err != nil {
		return err
	}
	if len(filed) != n {
		return fmt.Errorf("expected %d new stories under %s, got %d (error: %v): %+v", n, epic, len(filed), c.err, filed)
	}
	return nil
}

func (c *suiteContext) theNewStoryIsTitled(file, title string) error {
	story, err := c.newStoryFor(file)
	if err != nil {
		return err
	}
	if story.Story.Title != title {
		return fmt.Errorf("expected the title %q, got %q", title, story.Story.Title)
	}
	return nil
}

func (c *suiteContext) theNewStoryIsHeld(file string) error {
	story, err := c.newStoryFor(file)
	if err != nil {
		return err
	}
	if story.Status != apptest.StatusDeferred || strings.Join(story.Labels, ",") != "bug,suite" {
		return fmt.Errorf("expected %s held with the labels bug,suite, got %s with %v", story.Story.ID, story.Status, story.Labels)
	}
	return nil
}

func (c *suiteContext) theNewStoryHasFiles(file, files string) error {
	story, err := c.newStoryFor(file)
	if err != nil {
		return err
	}
	want := c.epicPath
	want.Files = files
	if story.Story.Overrides != want {
		return fmt.Errorf("expected the path %+v, got %+v", want, story.Story.Overrides)
	}
	return nil
}

func (c *suiteContext) theNewStoryDescribes(file, test, errorLine, sha, log, foundBy string) error {
	parts := []string{test, errorLine, sha, log, foundBy}
	story, err := c.newStoryFor(file)
	if err != nil {
		return err
	}
	for _, part := range parts {
		if !strings.Contains(story.Description, part) {
			return fmt.Errorf("expected the description to hold %q, got:\n%s", part, story.Description)
		}
	}
	return nil
}

func (c *suiteContext) theNewStoryHasAcceptance(file, acceptance string) error {
	story, err := c.newStoryFor(file)
	if err != nil {
		return err
	}
	if story.Acceptance != acceptance {
		return fmt.Errorf("expected the acceptance %q, got %q", acceptance, story.Acceptance)
	}
	return nil
}

func (c *suiteContext) theBugHasADatedComment(id, day, sha, how, errorLine string) error {
	parts := []string{sha, how, errorLine}
	comments := c.tracker.Comments(id)
	if len(comments) != 1 {
		return fmt.Errorf("expected one comment on %s, got %d: %v", id, len(comments), comments)
	}
	for _, part := range append([]string{day}, parts...) {
		if !strings.Contains(comments[0], part) {
			return fmt.Errorf("expected the comment on %s to hold %q, got:\n%s", id, part, comments[0])
		}
	}
	return nil
}

func (c *suiteContext) theBugHasNoComment(id string) error {
	if comments := c.tracker.Comments(id); len(comments) != 0 {
		return fmt.Errorf("expected no comment on %s, got %v", id, comments)
	}
	return nil
}

func (c *suiteContext) mailed() ([]application.Message, error) {
	return c.mailbox.Inbox(context.Background(), application.MayorMailbox)
}

func (c *suiteContext) theSuiteSentOneMail(subject string) error {
	mail, err := c.mailed()
	if err != nil {
		return err
	}
	if len(mail) != 1 || mail[0].Subject != subject {
		return fmt.Errorf("expected one mail to the mayor with the subject %q (error: %v), got %+v", subject, c.err, mail)
	}
	return nil
}

func (c *suiteContext) theSuiteSentNoMail() error {
	mail, err := c.mailed()
	if err != nil {
		return err
	}
	if len(mail) != 0 || c.mailbox.Writes() != 0 {
		return fmt.Errorf("expected no mail, got %+v", mail)
	}
	return nil
}

func (c *suiteContext) theMailNamesEachNewStory() error {
	mail, err := c.mailed()
	if err != nil || len(mail) != 1 {
		return fmt.Errorf("expected one mail, got %v (%v)", mail, err)
	}
	filed, err := c.newStories()
	if err != nil {
		return err
	}
	for _, story := range filed {
		file := strings.Fields(story.Story.Title)[3]
		if line := story.Story.ID + "  " + file + "  new"; !strings.Contains(mail[0].Body, line) {
			return fmt.Errorf("expected the mail to hold %q, got:\n%s", line, mail[0].Body)
		}
	}
	return nil
}

func (c *suiteContext) theMailSaysWhichIsWhich(id, file string) error {
	mail, err := c.mailed()
	if err != nil || len(mail) != 1 {
		return fmt.Errorf("expected one mail, got %v (%v)", mail, err)
	}
	var commented, fresh bool
	for _, line := range strings.Split(mail[0].Body, "\n") {
		switch {
		case strings.HasPrefix(line, id+"  "+file+"  commented"):
			commented = true
		case strings.Contains(line, "  new"):
			fresh = true
		}
	}
	if !commented || !fresh {
		return fmt.Errorf("expected the mail to say %s with %s was commented and the other new, got:\n%s", id, file, mail[0].Body)
	}
	return nil
}

func (c *suiteContext) aRerunPasses(file, printed string) error {
	c.rerun[file] = printed
	return nil
}

func (c *suiteContext) theEpicHoldsAReadyBug(epic, id, label, files string) error {
	c.tracker.AddEpic(epic, domain.Path{
		Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelSonnet, Effort: domain.EffortHigh, Host: "desktop",
	})
	c.tracker.AddStory(epic, domain.Story{
		ID: id, Title: "[bug] lampas main: " + files + " fails on main", Overrides: domain.Path{Files: files},
	})
	return c.tracker.SetLabels(id, "bug", label)
}

func (c *suiteContext) mwDispatchRuns(host, rig string) error {
	c.sessions = apptest.NewFakeRunner()
	c.dispatch, c.err = application.Dispatch{
		Tracker:   c.tracker,
		Worktrees: noWorktrees{},
		Runner:    c.sessions,
		Host:      host,
		Cap:       2,
		Rigs:      map[string]string{rig: path.Join(suiteRigsDir, rig)},
		Rechecks:  c.newSuite(rig),
		Out:       &c.out,
	}.Run(context.Background())
	return c.err
}

func (c *suiteContext) theBugIsClosedWith(id, reason string) error {
	if got := c.tracker.CloseReason(id); got != reason {
		return fmt.Errorf("expected %s to be closed with %q, the reason is %q (dispatch printed:\n%s)", id, reason, got, c.out.String())
	}
	return nil
}

func (c *suiteContext) theBugHasCommentHolding(id, text string) error {
	said := strings.Join(c.tracker.Comments(id), "\n")
	if !strings.Contains(said, text) {
		return fmt.Errorf("expected a comment on %s holding %q, got %q", id, text, said)
	}
	return nil
}

func (c *suiteContext) dispatchStartedNothing(id string) error {
	if started := c.sessions.Started(); len(started) != 0 || len(c.dispatch.Started) != 0 {
		return fmt.Errorf("expected no session, started %v / %+v", started, c.dispatch.Started)
	}
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return err
	}
	if detail.Assignee != "" {
		return fmt.Errorf("expected %s to be claimed by nobody, it is claimed by %s", id, detail.Assignee)
	}
	return nil
}

func (c *suiteContext) theDispatchPrinted(want string) error {
	return c.theSuitePrints(want)
}
