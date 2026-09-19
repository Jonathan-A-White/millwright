package steps

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// briefContext holds the fake work tracker a brief is read from, and what came
// of reading it. Nothing here reaches the factory's own beads database.
type briefContext struct {
	tracker *apptest.FakeTracker

	lastEpic     string
	writesBefore int

	printed string
	err     error
}

// InitializeBriefScenario registers the steps of features/brief.feature.
func InitializeBriefScenario(ctx *godog.ScenarioContext) {
	c := &briefContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = briefContext{tracker: apptest.NewFakeTracker()}
		return ctx, nil
	})

	ctx.Given(`^the brief epic "([^"]*)" titled "([^"]*)" at priority (\d) on the default path:$`, c.theBriefEpic)
	ctx.Given(`^a brief story "([^"]*)" titled "([^"]*)" filed under it, which is (closed|in progress|open|held)$`, c.aBriefStory)
	ctx.Given(`^the brief story "([^"]*)" overrides "([^"]*)" with "([^"]*)" and "([^"]*)" with "([^"]*)"$`, c.theBriefStoryOverrides)
	ctx.Given(`^the brief story "([^"]*)" is at priority (\d)$`, c.theBriefStoryIsAtPriority)
	ctx.Given(`^the brief story "([^"]*)" waits on "([^"]*)"$`, c.theBriefStoryWaitsOn)
	ctx.Given(`^the brief epic "([^"]*)" has the comment:$`, c.theBriefEpicHasTheComment)

	ctx.When(`^mw brief reads "([^"]*)"$`, c.mwBriefReads)
	ctx.When(`^mw brief reads "([^"]*)" and "([^"]*)"$`, c.mwBriefReadsTwo)
	ctx.When(`^mw brief reads "([^"]*)" with the newest (\d+) comments$`, c.mwBriefReadsWithComments)

	ctx.Then(`^reading the brief succeeds$`, c.readingTheBriefSucceeds)
	ctx.Then(`^the brief is refused, naming "([^"]*)"$`, c.theBriefIsRefusedNaming)
	ctx.Then(`^the brief printed nothing$`, c.theBriefPrintedNothing)
	ctx.Then(`^the brief opens with the line "([^"]*)"$`, c.theBriefOpensWithTheLine)
	ctx.Then(`^the brief lists "([^"]*)" under the heading "([^"]*)"$`, c.theBriefListsUnderTheHeading)
	ctx.Then(`^the brief has no heading "([^"]*)"$`, c.theBriefHasNoHeading)
	ctx.Then(`^the brief does not list "([^"]*)"$`, c.theBriefDoesNotList)
	ctx.Then(`^the brief says "([^"]*)" on a line of its own$`, c.theBriefSaysOnALineOfItsOwn)
	ctx.Then(`^the line of the brief for "([^"]*)" says "([^"]*)"$`, c.theLineForSays)
	ctx.Then(`^the line of the brief for "([^"]*)" does not say "([^"]*)"$`, c.theLineForDoesNotSay)
	ctx.Then(`^the brief does not name "([^"]*)"$`, c.theBriefDoesNotName)
	ctx.Then(`^the brief prints the comment in full:$`, c.theBriefPrintsTheCommentInFull)
	ctx.Then(`^the brief names "([^"]*)" before "([^"]*)"$`, c.theBriefNamesBefore)
	ctx.Then(`^the tracker recorded no writes for the brief$`, c.theTrackerRecordedNoWrites)
}

func (c *briefContext) theBriefEpic(id, title, priority string, table *godog.Table) error {
	defaults := domain.Path{}
	for _, row := range table.Rows {
		if len(row.Cells) != 2 {
			return fmt.Errorf("a default path row needs a field and a value, got %d cells", len(row.Cells))
		}
		if err := defaults.Set(row.Cells[0].Value, row.Cells[1].Value); err != nil {
			return err
		}
	}
	n, err := strconv.Atoi(priority)
	if err != nil {
		return err
	}
	c.tracker.AddEpic(id, defaults)
	c.tracker.DescribeEpic(id, title, application.StatusOpen, n)
	c.lastEpic = id
	return nil
}

func (c *briefContext) aBriefStory(id, title, status string) error {
	c.tracker.AddStory(c.lastEpic, domain.Story{ID: id, Title: title})
	if status == "in progress" {
		status = application.StatusInProgress
	}
	if status == "held" {
		status = application.StatusHeld
	}
	return c.tracker.SetStatus(id, status)
}

func (c *briefContext) theBriefStoryOverrides(id, field, value, otherField, otherValue string) error {
	return c.tracker.SetStoryMetadata(context.Background(), id, map[string]string{field: value, otherField: otherValue})
}

func (c *briefContext) theBriefStoryIsAtPriority(id, priority string) error {
	n, err := strconv.Atoi(priority)
	if err != nil {
		return err
	}
	return c.tracker.SetPriority(id, n)
}

func (c *briefContext) theBriefStoryWaitsOn(id, blocker string) error {
	detail, err := c.tracker.ShowEpic(context.Background(), c.epicOf(id))
	if err != nil {
		return err
	}
	var needs []string
	for _, story := range detail.Stories {
		if story.Story.ID == id {
			needs = story.Needs
		}
	}
	c.tracker.Needs(id, append(needs, blocker)...)
	return nil
}

// epicOf is the epic a story was filed under: the id up to its last dot.
func (c *briefContext) epicOf(id string) string {
	return id[:strings.LastIndex(id, ".")]
}

func (c *briefContext) theBriefEpicHasTheComment(id string, text *godog.DocString) error {
	c.tracker.AddEpicComment(id, text.Content)
	return nil
}

func (c *briefContext) mwBriefReads(id string) error {
	return c.read(0, id)
}

func (c *briefContext) mwBriefReadsTwo(first, second string) error {
	return c.read(0, first, second)
}

func (c *briefContext) mwBriefReadsWithComments(id, count string) error {
	n, err := strconv.Atoi(count)
	if err != nil {
		return err
	}
	return c.read(n, id)
}

// read runs the use case, remembering how many writes the tracker had taken
// beforehand so that a scenario can say the brief took none.
func (c *briefContext) read(comments int, ids ...string) error {
	var out strings.Builder
	c.writesBefore = c.tracker.Writes()
	_, c.err = application.Brief{Tracker: c.tracker, Comments: comments, Out: &out}.Run(context.Background(), ids...)
	c.printed = out.String()
	return nil
}

func (c *briefContext) readingTheBriefSucceeds() error {
	if c.err != nil {
		return fmt.Errorf("reading the brief failed: %w", c.err)
	}
	return nil
}

func (c *briefContext) theBriefIsRefusedNaming(id string) error {
	if c.err == nil {
		return fmt.Errorf("expected the brief to be refused, naming %q; it succeeded", id)
	}
	if !strings.Contains(c.err.Error(), id) {
		return fmt.Errorf("expected the refusal to name %q, got: %s", id, c.err)
	}
	return nil
}

func (c *briefContext) theBriefPrintedNothing() error {
	if c.printed != "" {
		return fmt.Errorf("expected nothing printed, got:\n%s", c.printed)
	}
	return nil
}

func (c *briefContext) lines() []string {
	return strings.Split(strings.TrimRight(c.printed, "\n"), "\n")
}

func (c *briefContext) theBriefOpensWithTheLine(line string) error {
	if got := c.lines()[0]; got != line {
		return fmt.Errorf("expected the brief to open with %q, got %q in:\n%s", line, got, c.printed)
	}
	return nil
}

// theBriefListsUnderTheHeading finds the heading, and then the story among the
// indented lines that follow it and before the next line that is not indented.
func (c *briefContext) theBriefListsUnderTheHeading(id, heading string) error {
	lines := c.lines()
	for i, line := range lines {
		if !strings.HasPrefix(line, heading+" (") {
			continue
		}
		for _, under := range lines[i+1:] {
			if !strings.HasPrefix(under, "  ") {
				break
			}
			if strings.Contains(under, " · "+id+" · ") {
				return nil
			}
		}
	}
	return fmt.Errorf("expected %s under the heading %q, got:\n%s", id, heading, c.printed)
}

func (c *briefContext) theBriefHasNoHeading(heading string) error {
	for _, line := range c.lines() {
		if strings.HasPrefix(line, heading+" (") {
			return fmt.Errorf("expected no heading %q, got:\n%s", heading, c.printed)
		}
	}
	return nil
}

func (c *briefContext) theBriefDoesNotList(id string) error {
	if strings.Contains(c.printed, " · "+id+" · ") {
		return fmt.Errorf("expected the brief not to list %s, got:\n%s", id, c.printed)
	}
	return nil
}

func (c *briefContext) theBriefSaysOnALineOfItsOwn(text string) error {
	for _, line := range c.lines() {
		if line == text {
			return nil
		}
	}
	return fmt.Errorf("expected a line %q, got:\n%s", text, c.printed)
}

// lineFor is the line of the brief that lists a story.
func (c *briefContext) lineFor(id string) (string, error) {
	for _, line := range c.lines() {
		if strings.HasPrefix(line, "  ") && strings.Contains(line, " · "+id+" · ") {
			return line, nil
		}
	}
	return "", fmt.Errorf("expected the brief to list %s, got:\n%s", id, c.printed)
}

func (c *briefContext) theLineForSays(id, text string) error {
	line, err := c.lineFor(id)
	if err != nil {
		return err
	}
	if !strings.Contains(line, text) {
		return fmt.Errorf("expected the line for %s to say %q, got %q", id, text, line)
	}
	return nil
}

func (c *briefContext) theLineForDoesNotSay(id, text string) error {
	line, err := c.lineFor(id)
	if err != nil {
		return err
	}
	if strings.Contains(line, text) {
		return fmt.Errorf("expected the line for %s not to say %q, got %q", id, text, line)
	}
	return nil
}

func (c *briefContext) theBriefDoesNotName(text string) error {
	if strings.Contains(c.printed, text) {
		return fmt.Errorf("expected the brief not to name %q, got:\n%s", text, c.printed)
	}
	return nil
}

func (c *briefContext) theBriefPrintsTheCommentInFull(text *godog.DocString) error {
	if !strings.Contains(c.printed, text.Content+"\n") {
		return fmt.Errorf("expected the brief to print, in full:\n%s\ngot:\n%s", text.Content, c.printed)
	}
	return nil
}

func (c *briefContext) theBriefNamesBefore(first, second string) error {
	i, j := strings.Index(c.printed, first), strings.Index(c.printed, second)
	if i < 0 || j < 0 || i >= j {
		return fmt.Errorf("expected %q before %q, got:\n%s", first, second, c.printed)
	}
	return nil
}

func (c *briefContext) theTrackerRecordedNoWrites() error {
	if err := c.readingTheBriefSucceeds(); err != nil {
		return err
	}
	if got := c.tracker.Writes(); got != c.writesBefore {
		return fmt.Errorf("expected the brief to write nothing, the tracker took %d writes", got-c.writesBefore)
	}
	return nil
}
