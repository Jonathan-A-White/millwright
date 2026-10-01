package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// promptNow is the moment a prompt scenario's facts are measured at.
var promptNow = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

// promptContext holds the fake backend and tracker a scenario saves and runs
// prompts against, and what the last command said.
type promptContext struct {
	prompts *apptest.FakePrompts
	tracker *apptest.FakeTracker
	out     bytes.Buffer
	err     error
}

// InitializePromptScenario registers the steps of features/prompt.feature.
func InitializePromptScenario(ctx *godog.ScenarioContext) {
	c := &promptContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = promptContext{prompts: apptest.NewFakePrompts(), tracker: apptest.NewFakeTracker()}
		c.tracker.AddEpic("mw-f", domain.Path{})
		c.tracker.DescribeEpic("mw-f", "Epic mw-f", apptest.StatusOpen, 1)
		return ctx, nil
	})

	ctx.Given(`^the backend holds the prompt "([^"]*)" summarised "([^"]*)" with the signature "([^"]*)" and the body "([^"]*)"$`, c.theBackendHolds)
	ctx.Given(`^a story "([^"]*)" waiting for the Governor$`, c.aStoryWaiting)
	ctx.Given(`^a story "([^"]*)" landed with the closing comment "([^"]*)"$`, c.aStoryLanded)
	ctx.Given(`^a story "([^"]*)" with an open card asking "([^"]*)"$`, c.aStoryWithACard)
	ctx.Given(`^a story "([^"]*)" labelled for a demo$`, c.aStoryForADemo)
	ctx.Given(`^a story "([^"]*)" with the hands step "([^"]*)" on "([^"]*)" that has not run$`, c.aStoryWithHandsStep)
	ctx.Given(`^a story "([^"]*)" with the hands step "([^"]*)" on "([^"]*)" that has run$`, c.aStoryWithRunHandsStep)
	ctx.When(`^the Mayor saves the prompt "([^"]*)" summarised "([^"]*)" with the options "([^"]*)" and the body "([^"]*)"$`, c.theMayorSaves)
	ctx.When(`^the Mayor lists the prompts$`, c.theMayorLists)
	ctx.When(`^the Mayor shows the prompt "([^"]*)"$`, c.theMayorShows)
	ctx.When(`^the Mayor runs the prompt "([^"]*)" with "([^"]*)"$`, c.theMayorRuns)
	ctx.Then(`^saving succeeds$`, c.succeeds)
	ctx.Then(`^running succeeds$`, c.succeeds)
	ctx.Then(`^saving is refused, saying "([^"]*)"$`, c.isRefused)
	ctx.Then(`^running is refused, saying "([^"]*)"$`, c.isRefused)
	ctx.Then(`^the backend holds the prompt "([^"]*)" summarised "([^"]*)" with the signature "([^"]*)"$`, c.theBackendNowHolds)
	ctx.Then(`^the backend holds no prompt "([^"]*)"$`, c.theBackendHoldsNo)
	ctx.Then(`^the output has the line "([^"]*)"$`, c.theOutputHasTheLine)
	ctx.Then(`^the output is empty$`, c.theOutputIsEmpty)
	ctx.Then(`^the output has the section "([^"]*)" naming "([^"]*)"$`, c.theOutputHasTheSection)
}

// signature is a scenario's "a, b" as the specs it lists.
func signature(text string) []string {
	var specs []string
	for _, spec := range strings.Split(text, ",") {
		if spec = strings.TrimSpace(spec); spec != "" {
			specs = append(specs, spec)
		}
	}
	return specs
}

func (c *promptContext) theBackendHolds(name, summary, sig, body string) error {
	return c.prompts.Put(context.Background(), domain.Prompt{Name: name, Summary: summary, Signature: signature(sig), Body: body})
}

func (c *promptContext) aStoryWaiting(id string) error {
	c.tracker.AddStory("mw-f", domain.Story{ID: id, Title: "Waits for him"})
	return c.tracker.SetLabels(id, application.LabelHitl)
}

func (c *promptContext) aStoryLanded(id, comment string) error {
	ctx := context.Background()
	c.tracker.AddStory("mw-f", domain.Story{ID: id, Title: "Landed " + id})
	if err := c.tracker.SetStoryState(ctx, id, application.RunState, application.RunLanded, "landed"); err != nil {
		return err
	}
	if err := c.tracker.SetStatus(id, apptest.StatusClosed); err != nil {
		return err
	}
	if err := c.tracker.SetClosedAt(id, promptNow.Add(-time.Hour)); err != nil {
		return err
	}
	return c.tracker.CommentOnStory(ctx, id, comment)
}

func (c *promptContext) aStoryWithACard(id, question string) error {
	c.tracker.AddStory("mw-f", domain.Story{ID: id, Title: "Asks him"})
	note, err := json.Marshal(map[string]any{
		"txid": "q-" + id, "options": []string{"left", "right"}, "q": question,
		"asked": promptNow.Add(-2 * time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	return c.tracker.SetNote(context.Background(), application.PosternQuestionKey(id), string(note))
}

func (c *promptContext) aStoryForADemo(id string) error {
	c.tracker.AddStory("mw-f", domain.Story{ID: id, Title: "Show him " + id})
	return c.tracker.SetLabels(id, application.LabelDemo)
}

func (c *promptContext) handsStep(bead, id, host string) error {
	c.tracker.AddStory("mw-f", domain.Story{ID: bead, Title: "Needs hands " + bead})
	steps, err := json.Marshal([]application.HandsStepRecord{{
		HandsStep: domain.HandsStep{ID: id, Host: host, As: "root", Run: "loginctl enable-linger jwhite"},
	}})
	if err != nil {
		return err
	}
	return c.tracker.SetNote(context.Background(), application.HandsStepsKey(bead), string(steps))
}

func (c *promptContext) aStoryWithHandsStep(bead, id, host string) error {
	return c.handsStep(bead, id, host)
}

func (c *promptContext) aStoryWithRunHandsStep(bead, id, host string) error {
	if err := c.handsStep(bead, id, host); err != nil {
		return err
	}
	return c.tracker.SetNote(context.Background(), application.HandsRanKey(bead, id),
		`{"at":"2026-09-28T11:50:00Z","exit":0,"host":"`+host+`"}`)
}

func (c *promptContext) theMayorSaves(name, summary, options, body string) error {
	c.out.Reset()
	_, c.err = application.PromptSave{Prompts: c.prompts, Out: &c.out}.Run(context.Background(), application.PromptSaveRequest{
		Name: name, Summary: summary, Options: signature(options), Body: body,
	})
	return nil
}

func (c *promptContext) theMayorLists() error {
	c.out.Reset()
	_, c.err = application.PromptList{Prompts: c.prompts, Out: &c.out}.Run(context.Background())
	return c.err
}

func (c *promptContext) theMayorShows(name string) error {
	c.out.Reset()
	_, c.err = application.PromptShow{Prompts: c.prompts, Out: &c.out}.Run(context.Background(), name)
	return nil
}

func (c *promptContext) theMayorRuns(name, call string) error {
	c.out.Reset()
	c.err = application.PromptRun{
		Prompts: c.prompts, Tracker: c.tracker, Notes: c.tracker, Host: "desktop", Out: &c.out,
		Now: func() time.Time { return promptNow },
	}.Run(context.Background(), name, strings.Fields(call))
	return nil
}

func (c *promptContext) succeeds() error {
	if c.err != nil {
		return fmt.Errorf("expected it to succeed, got: %v", c.err)
	}
	return nil
}

func (c *promptContext) isRefused(want string) error {
	if c.err == nil {
		return fmt.Errorf("expected it to be refused, saying %q; it succeeded, printing:\n%s", want, c.out.String())
	}
	if !strings.Contains(c.err.Error(), want) {
		return fmt.Errorf("expected the refusal to say %q, got: %v", want, c.err)
	}
	return nil
}

func (c *promptContext) theBackendNowHolds(name, summary, sig string) error {
	p, ok, err := c.prompts.Get(context.Background(), name)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("the backend holds no prompt %q", name)
	}
	want := domain.Prompt{Name: name, Summary: summary, Signature: signature(sig)}
	if p.Name != want.Name || p.Summary != want.Summary || strings.Join(p.Signature, "|") != strings.Join(want.Signature, "|") {
		return fmt.Errorf("the backend holds %+v, expected %+v", p, want)
	}
	if p.Body == "" {
		return fmt.Errorf("the prompt %q was saved with no body", name)
	}
	return nil
}

func (c *promptContext) theBackendHoldsNo(name string) error {
	if _, ok, _ := c.prompts.Get(context.Background(), name); ok {
		return fmt.Errorf("the backend holds the prompt %q", name)
	}
	return nil
}

func (c *promptContext) theOutputHasTheLine(want string) error {
	want = strings.ReplaceAll(want, `\t`, "\t")
	for _, line := range strings.Split(c.out.String(), "\n") {
		if line == want {
			return nil
		}
	}
	return fmt.Errorf("expected the line %q in:\n%s", want, c.out.String())
}

func (c *promptContext) theOutputIsEmpty() error {
	if c.out.Len() != 0 {
		return fmt.Errorf("expected nothing printed, got:\n%s", c.out.String())
	}
	return nil
}

// theOutputHasTheSection finds the section headed heading (up to the blank
// line that ends it) and looks for want in it.
func (c *promptContext) theOutputHasTheSection(heading, want string) error {
	var section []string
	in := false
	for _, line := range strings.Split(c.out.String(), "\n") {
		switch {
		case strings.HasPrefix(line, heading):
			in = true
		case in && line == "":
			in = false
		}
		if in {
			section = append(section, line)
		}
	}
	if len(section) == 0 {
		return fmt.Errorf("no section %q in:\n%s", heading, c.out.String())
	}
	if text := strings.Join(section, "\n"); !strings.Contains(text, want) {
		return fmt.Errorf("expected the section %q to name %q, got:\n%s", heading, want, text)
	}
	return nil
}
