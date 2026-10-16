package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	millwright "github.com/Jonathan-A-White/millwright"
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// todoRankContext holds the fake tracker, the fake grinder standing in for
// the ranking model, the pass logs written, and what came of the last pass.
type todoRankContext struct {
	tracker *apptest.FakeTracker
	grinder *apptest.FakeGrinder
	runs    *todoRankRuns
	now     time.Time

	// about is the about file's path, in dir, once a scenario writes one.
	dir   string
	about string

	printed bytes.Buffer
	err     error

	// writesBefore and logsBefore are how many tracker writes and pass logs
	// there were before the last pass.
	writesBefore, logsBefore int
}

// todoRankRuns is an in-memory application.TodoRankRuns.
type todoRankRuns struct {
	files map[string]string
	puts  int
}

func (r *todoRankRuns) PutRunFile(_ context.Context, dir, name, contents string) (string, error) {
	r.files[dir+"/"+name] = contents
	r.puts++
	return "runs/" + dir + "/" + name, nil
}

// InitializeTodoRankScenario registers the steps of features/todo-rank.feature.
func InitializeTodoRankScenario(ctx *godog.ScenarioContext) {
	c := &todoRankContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = todoRankContext{
			tracker: apptest.NewFakeTracker(), grinder: &apptest.FakeGrinder{},
			runs: &todoRankRuns{files: map[string]string{}},
		}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.dir != "" {
			os.RemoveAll(c.dir)
		}
		return ctx, nil
	})

	ctx.Given(`^the ranking clock stands at "([^"]*)"$`, c.theClockStandsAt)
	ctx.Given(`^six To do cards are open on the list "([^"]*)"$`, c.sixCardsAreOpen)
	ctx.Given(`^the card "([^"]*)" is starred for the ranking$`, c.theCardIsStarred)
	ctx.Given(`^the card "([^"]*)" is due for the ranking at "([^"]*)"$`, c.theCardIsDueAt)
	ctx.Given(`^the about file says "([^"]*)"$`, c.theAboutFileSays)
	ctx.Given(`^the card "([^"]*)" titled "([^"]*)" was closed (\d+) days ago on the list "([^"]*)"$`, c.aCardWasClosed)
	ctx.Given(`^the ranking model answers:$`, c.theModelAnswers)
	ctx.Given(`^the ranking model answers every card "([^"]*)" because "([^"]*)"$`, c.theModelAnswersEveryCard)
	ctx.Given(`^mw todo rank runs$`, func() error { return c.rankRuns(false) })
	ctx.When(`^mw todo rank runs$`, func() error { return c.rankRuns(false) })
	ctx.When(`^mw todo rank runs with --dry-run$`, func() error { return c.rankRuns(true) })

	ctx.Then(`^the todo rank pass succeeds$`, c.succeeds)
	ctx.Then(`^the todo rank pass is refused, saying: (.+)$`, c.isRefusedSaying)
	ctx.Then(`^nothing was written by the todo rank pass$`, c.nothingWritten)
	ctx.Then(`^the second todo rank pass wrote nothing to the cards$`, c.noCardWritten)
	ctx.Then(`^the card "([^"]*)" is ranked "([^"]*)" because "([^"]*)"$`, c.isRanked)
	ctx.Then(`^at most (\d+) cards are ranked "([^"]*)"$`, c.atMostRanked)
	ctx.Then(`^the todo rank pass printed "([^"]*)"$`, c.printedLine)
	ctx.Then(`^the model's message holds "([^"]*)"$`, func(text string) error { return c.messageHolds(text, true) })
	ctx.Then(`^the model's message does not hold "([^"]*)"$`, func(text string) error { return c.messageHolds(text, false) })
	ctx.Then(`^the system prompt was byte-identical across the two passes$`, c.systemIdentical)
	ctx.Then(`^the system prompt holds neither a card nor the about file$`, c.systemHoldsNoInput)
	ctx.Then(`^the pass log "([^"]*)" holds every card ranked "([^"]*)" because "([^"]*)"$`, c.passLogHolds)
	ctx.Then(`^the card "([^"]*)" is marked slipped at "([^"]*)"$`, c.markedSlipped)
	ctx.Then(`^the card "([^"]*)" is not marked slipped$`, func(id string) error { return c.markedSlipped(id, "") })
}

func (c *todoRankContext) theClockStandsAt(at string) error {
	now, err := time.Parse(time.RFC3339, at)
	c.now = now
	return err
}

func (c *todoRankContext) list(title string) string {
	id := "mw-todo"
	if title != "Errands" {
		id = "mw-list-" + strings.ToLower(strings.ReplaceAll(title, " ", "-"))
	}
	c.tracker.AddEpic(id, domain.Path{})
	c.tracker.DescribeEpic(id, title, application.StatusOpen, application.DefaultPriority)
	return id
}

func (c *todoRankContext) sixCardsAreOpen(list string) error {
	epic := c.list(list)
	for n := 1; n <= 6; n++ {
		id := fmt.Sprintf("mw-todo.%d", n)
		c.tracker.AddStory(epic, domain.Story{ID: id, Title: "Card " + id})
		if err := c.tracker.SetLabels(id, application.LabelHitl, application.LabelHitlTodo); err != nil {
			return err
		}
		if err := c.tracker.SetCreated(id, c.now.AddDate(0, 0, -20-n)); err != nil {
			return err
		}
		if err := c.tracker.SetUpdated(id, c.now.AddDate(0, 0, -n)); err != nil {
			return err
		}
	}
	return nil
}

func (c *todoRankContext) theCardIsStarred(id string) error {
	return c.tracker.SetStoryMetadata(context.Background(), id, map[string]string{application.TodoImportantField: "true"})
}

func (c *todoRankContext) theCardIsDueAt(id, due string) error {
	return c.tracker.SetStoryMetadata(context.Background(), id, map[string]string{application.TodoDueField: due})
}

func (c *todoRankContext) theAboutFileSays(text string) error {
	dir, err := os.MkdirTemp("", "mw-todo-rank-about-")
	if err != nil {
		return err
	}
	c.dir = dir
	c.about = filepath.Join(dir, "todo-about.md")
	return os.WriteFile(c.about, []byte(text+"\n"), 0o644)
}

func (c *todoRankContext) aCardWasClosed(id, title string, days int, list string) error {
	epic := c.list(list)
	c.tracker.AddStory(epic, domain.Story{ID: id, Title: title})
	if err := c.tracker.SetLabels(id, application.LabelHitl, application.LabelHitlTodo); err != nil {
		return err
	}
	if err := c.tracker.SetCreated(id, c.now.AddDate(0, 0, -days-3)); err != nil {
		return err
	}
	if err := c.tracker.CloseStory(context.Background(), id, "done"); err != nil {
		return err
	}
	return c.tracker.SetClosedAt(id, c.now.AddDate(0, 0, -days))
}

func (c *todoRankContext) answer(rows []application.TodoRankRow) error {
	answer, err := json.Marshal(map[string]any{"cards": rows})
	if err != nil {
		return err
	}
	c.grinder.Result = application.SessionResult{
		Subtype: "success", Answer: answer, Model: "sonnet", Duration: 4 * time.Second,
		Fuel: application.Fuel{Input: 1200, Output: 300, CacheRead: 5000},
	}
	return nil
}

func (c *todoRankContext) theModelAnswers(table *godog.Table) error {
	var rows []application.TodoRankRow
	for _, row := range table.Rows[1:] {
		rows = append(rows, application.TodoRankRow{Bead: row.Cells[0].Value, Row: row.Cells[1].Value, Why: row.Cells[2].Value})
	}
	return c.answer(rows)
}

func (c *todoRankContext) theModelAnswersEveryCard(row, why string) error {
	var rows []application.TodoRankRow
	for n := 1; n <= 6; n++ {
		rows = append(rows, application.TodoRankRow{Bead: fmt.Sprintf("mw-todo.%d", n), Row: row, Why: why})
	}
	return c.answer(rows)
}

func (c *todoRankContext) rankRuns(dryRun bool) error {
	c.printed.Reset()
	c.writesBefore, c.logsBefore = c.tracker.Writes(), c.runs.puts
	pass := application.TodoRankPass{
		Tracker: c.tracker, Closed: c.tracker, Grinder: c.grinder, Runs: c.runs,
		Instructions: millwright.TodoRankInstructions, AboutFile: c.about,
		Model: "sonnet", Effort: "medium", Zone: time.UTC,
		Now: func() time.Time { return c.now }, Out: &c.printed,
	}
	_, c.err = pass.Run(context.Background(), dryRun)
	return nil
}

func (c *todoRankContext) succeeds() error {
	if c.err != nil {
		return fmt.Errorf("the pass failed: %w", c.err)
	}
	return nil
}

func (c *todoRankContext) isRefusedSaying(reason string) error {
	if c.err == nil {
		return fmt.Errorf("expected the pass to be refused, saying %q; it succeeded:\n%s", reason, c.printed.String())
	}
	if !strings.Contains(c.err.Error(), reason) {
		return fmt.Errorf("expected the refusal to say %q, got:\n%s", reason, c.err)
	}
	return nil
}

func (c *todoRankContext) nothingWritten() error {
	if got := c.tracker.Writes() - c.writesBefore; got != 0 {
		return fmt.Errorf("expected nothing written, the tracker took %d writes", got)
	}
	if got := c.runs.puts - c.logsBefore; got != 0 {
		return fmt.Errorf("expected no pass log, %d were written", got)
	}
	return nil
}

func (c *todoRankContext) noCardWritten() error {
	if c.err != nil {
		return fmt.Errorf("the pass failed: %w", c.err)
	}
	if got := c.tracker.Writes() - c.writesBefore; got != 0 {
		return fmt.Errorf("expected no card written again, the tracker took %d writes", got)
	}
	return nil
}

func (c *todoRankContext) isRanked(id, row, why string) error {
	meta := c.tracker.Metadata(id)
	if meta[application.TodoRowField] != row || meta[application.TodoRowWhyField] != why {
		return fmt.Errorf("expected %s ranked %q because %q, got %q because %q", id, row, why,
			meta[application.TodoRowField], meta[application.TodoRowWhyField])
	}
	return nil
}

func (c *todoRankContext) atMostRanked(most int, row string) error {
	n := 0
	for i := 1; i <= 6; i++ {
		if c.tracker.Metadata(fmt.Sprintf("mw-todo.%d", i))[application.TodoRowField] == row {
			n++
		}
	}
	if n > most {
		return fmt.Errorf("expected at most %d cards ranked %q, got %d", most, row, n)
	}
	return nil
}

func (c *todoRankContext) printedLine(line string) error {
	for _, got := range strings.Split(c.printed.String(), "\n") {
		if got == line {
			return nil
		}
	}
	return fmt.Errorf("expected the line %q, the pass printed:\n%s", line, c.printed.String())
}

func (c *todoRankContext) lastCall() (application.GrindCall, error) {
	calls := c.grinder.Calls()
	if len(calls) == 0 {
		return application.GrindCall{}, fmt.Errorf("the ranking model was never asked")
	}
	return calls[len(calls)-1].Call, nil
}

func (c *todoRankContext) messageHolds(text string, holds bool) error {
	call, err := c.lastCall()
	if err != nil {
		return err
	}
	if strings.Contains(call.Prompt, text) != holds {
		return fmt.Errorf("expected the message to hold %q: %v; it is:\n%s", text, holds, call.Prompt)
	}
	return nil
}

func (c *todoRankContext) systemIdentical() error {
	calls := c.grinder.Calls()
	if len(calls) != 2 {
		return fmt.Errorf("expected two passes to ask the model, got %d", len(calls))
	}
	if calls[0].Call.System != calls[1].Call.System {
		return fmt.Errorf("the system prompts differ:\n%s\n---\n%s", calls[0].Call.System, calls[1].Call.System)
	}
	if calls[0].Call.System == "" || !strings.Contains(calls[0].Call.System, strings.TrimSpace(millwright.TodoRankInstructions)) {
		return fmt.Errorf("the system prompt does not hold the instructions:\n%s", calls[0].Call.System)
	}
	return nil
}

func (c *todoRankContext) systemHoldsNoInput() error {
	call, err := c.lastCall()
	if err != nil {
		return err
	}
	for _, text := range []string{"mw-todo.1", "Card mw-todo", "The train is my best time", "Mow the lawn"} {
		if strings.Contains(call.System, text) {
			return fmt.Errorf("the system prompt holds %q:\n%s", text, call.System)
		}
	}
	return nil
}

func (c *todoRankContext) passLogHolds(name, row, why string) error {
	if c.err != nil {
		return fmt.Errorf("the pass failed: %w", c.err)
	}
	body, ok := c.runs.files[application.TodoRankRunDir+"/"+name]
	if !ok {
		return fmt.Errorf("no pass log %s; the logs written are %v", name, c.runs.files)
	}
	var log application.TodoRankLog
	if err := json.Unmarshal([]byte(body), &log); err != nil {
		return fmt.Errorf("the pass log is not JSON: %v\n%s", err, body)
	}
	if len(log.Cards) != 6 {
		return fmt.Errorf("expected the log to hold 6 cards, it holds %d:\n%s", len(log.Cards), body)
	}
	for _, card := range log.Cards {
		if card.Row != row || card.Why != why {
			return fmt.Errorf("expected %s logged %q because %q, got %q because %q", card.Bead, row, why, card.Row, card.Why)
		}
	}
	if log.Tokens.Total == 0 || log.Elapsed == "" {
		return fmt.Errorf("expected the log to hold the model's tokens and the elapsed time:\n%s", body)
	}
	return nil
}

func (c *todoRankContext) markedSlipped(id, due string) error {
	if c.err != nil {
		return fmt.Errorf("the pass failed: %w", c.err)
	}
	if got := c.tracker.Metadata(id)[application.TodoSlippedField]; got != due {
		return fmt.Errorf("expected %s marked slipped at %q, got %q", id, due, got)
	}
	return nil
}
