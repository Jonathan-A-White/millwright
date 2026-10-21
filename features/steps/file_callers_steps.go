package steps

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"

	"github.com/cucumber/godog"
)

// callersContext holds a rig checked out as a real git repository in a temp
// directory, the plan to check or file, the fake work tracker, and what the
// command printed to stdout and to stderr.
type callersContext struct {
	tracker *apptest.FakeTracker
	rigName string
	rigDir  string
	plan    domain.Plan

	out    bytes.Buffer
	errOut bytes.Buffer
	err    error
}

// InitializeFileCallersScenario registers the steps of features/file_callers.feature.
func InitializeFileCallersScenario(ctx *godog.ScenarioContext) {
	c := &callersContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		tracker := apptest.NewFakeTracker()
		tracker.AddFormula("tdd-feature")
		*c = callersContext{tracker: tracker}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.rigDir != "" {
			os.RemoveAll(c.rigDir)
		}
		return ctx, nil
	})

	ctx.Given(`^the callers rig "([^"]*)" is a git checkout whose (\d+) test files name "([^"]*)" and (\d+) do not$`, c.theRigIsACheckout)
	ctx.Given(`^the callers plan:$`, c.thePlan)
	ctx.Given(`^the callers story "([^"]*)" is sized (\S+)$`, c.theStoryIsSized)
	ctx.Given(`^the callers story "([^"]*)" says callers_ok "([^"]*)"$`, c.theStorySaysCallersOK)
	ctx.Given(`^the callers story "([^"]*)" names the files "([^"]*)"$`, c.theStoryNamesTheFiles)
	ctx.Given(`^the callers story "([^"]*)" is open on the rig "([^"]*)"$`, c.theStoryIsOpen)
	ctx.When(`^the callers plan is checked$`, c.thePlanIsChecked)
	ctx.When(`^the callers plan is filed$`, c.thePlanIsFiled)
	ctx.When(`^the callers path of "([^"]*)" is set with (.+)$`, c.thePathIsSet)
	ctx.Then(`^the callers check is refused$`, c.theCheckIsRefused)
	ctx.Then(`^the callers check passes$`, c.theCheckPasses)
	ctx.Then(`^the callers plan is filed$`, c.theFilingSucceeded)
	ctx.Then(`^the callers output has the line: (.+)$`, c.outputHasTheLine)
	ctx.Then(`^the callers output does not mention: (.+)$`, c.outputDoesNotMention)
	ctx.Then(`^the callers warning has the line: (.+)$`, c.warningHasTheLine)
	ctx.Then(`^the callers warning is empty$`, c.warningIsEmpty)
}

func (c *callersContext) theRigIsACheckout(name string, calling int, word string, others int) error {
	dir, err := os.MkdirTemp("", "mw-callers-")
	if err != nil {
		return err
	}
	c.rigName, c.rigDir = name, dir
	write := func(rel, text string) error {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(text), 0o644)
	}
	// Half the callers are Gherkin, half unit tests; the other test files and a
	// source file that names the word are not callers.
	for i := 0; i < calling; i++ {
		rel, text := fmt.Sprintf("features/caller%d.feature", i), "open the "+word+" page\n"
		if i%2 == 1 {
			rel, text = fmt.Sprintf("src/caller%d_test.go", i), "// "+word+"\n"
		}
		if err := write(rel, text); err != nil {
			return err
		}
	}
	for i := 0; i < others; i++ {
		if err := write(fmt.Sprintf("tests/other%d.test.ts", i), "nothing here\n"); err != nil {
			return err
		}
	}
	if err := write("src/screens/"+word+".tsx", "// "+word+" itself is not a test\n"); err != nil {
		return err
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	return nil
}

func (c *callersContext) thePlan(written *godog.DocString) error {
	var err error
	c.plan, err = domain.ParsePlan([]byte(written.Content))
	return err
}

func (c *callersContext) story(key string) (*domain.PlanStory, error) {
	for i := range c.plan.Stories {
		if c.plan.Stories[i].Key == key {
			return &c.plan.Stories[i], nil
		}
	}
	return nil, fmt.Errorf("the plan has no story %q", key)
}

func (c *callersContext) theStoryIsSized(key, size string) error {
	story, err := c.story(key)
	if err != nil {
		return err
	}
	story.Overrides.Size = domain.Size(size)
	return nil
}

func (c *callersContext) theStorySaysCallersOK(key, why string) error {
	story, err := c.story(key)
	if err != nil {
		return err
	}
	story.CallersOK = why
	return nil
}

func (c *callersContext) theStoryNamesTheFiles(key, files string) error {
	story, err := c.story(key)
	if err != nil {
		return err
	}
	story.Files = files
	return nil
}

func (c *callersContext) theStoryIsOpen(id, rigName string) error {
	c.tracker.AddEpic("E", domain.Path{Rig: rigName, Branch: "main", Harness: "claude", Model: "sonnet",
		Effort: "high", Formula: "tdd-feature", Host: "vps"})
	c.tracker.AddStory("E", domain.Story{ID: id, Title: "story " + id})
	return nil
}

func (c *callersContext) filing() application.File {
	return application.File{
		Tracker:  c.tracker,
		Rigs:     map[string]string{c.rigName: c.rigDir},
		RigFiles: rig.Files{},
		Out:      &c.out,
		Err:      &c.errOut,
	}
}

func (c *callersContext) thePlanIsChecked() error {
	c.err = c.filing().Check(context.Background(), c.plan)
	return nil
}

func (c *callersContext) thePlanIsFiled() error {
	_, c.err = c.filing().Run(context.Background(), c.plan)
	return nil
}

func (c *callersContext) thePathIsSet(id, pairs string) error {
	_, c.err = application.PathSet{
		Tracker:  c.tracker,
		Rigs:     map[string]string{c.rigName: c.rigDir},
		RigFiles: rig.Files{},
		Out:      &c.out,
		Err:      &c.errOut,
	}.Run(context.Background(), id, "", strings.Fields(pairs))
	return nil
}

func (c *callersContext) theCheckIsRefused() error {
	if c.err == nil {
		return fmt.Errorf("expected the check to fail; it passed, printing:\n%s", c.out.String())
	}
	return nil
}

func (c *callersContext) theCheckPasses() error {
	if c.err != nil {
		return fmt.Errorf("expected the check to pass: %w", c.err)
	}
	return nil
}

func (c *callersContext) theFilingSucceeded() error {
	if c.err != nil {
		return fmt.Errorf("filing the plan failed: %w", c.err)
	}
	return nil
}

func hasLine(printed, want string) bool {
	for _, line := range strings.Split(printed, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

func (c *callersContext) outputHasTheLine(want string) error {
	if !hasLine(c.out.String(), want) {
		return fmt.Errorf("expected the line %q in the output:\n%s", want, c.out.String())
	}
	return nil
}

func (c *callersContext) outputDoesNotMention(word string) error {
	if strings.Contains(c.out.String(), word) {
		return fmt.Errorf("expected the output not to mention %q:\n%s", word, c.out.String())
	}
	return nil
}

func (c *callersContext) warningHasTheLine(want string) error {
	for _, line := range strings.Split(c.errOut.String(), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), want) {
			return nil
		}
	}
	return fmt.Errorf("expected a line starting %q on stderr:\n%s", want, c.errOut.String())
}

func (c *callersContext) warningIsEmpty() error {
	if c.errOut.Len() > 0 {
		return fmt.Errorf("expected nothing on stderr:\n%s", c.errOut.String())
	}
	return nil
}
