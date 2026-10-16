package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// investContext holds a real vault in a temp directory, whose investments
// ledger the scenarios fill, the clock, and what the last command printed.
type investContext struct {
	files   *vault.Vault
	now     time.Time
	actor   string
	printed bytes.Buffer
	err     error
}

// InitializeInvestScenario registers the steps of features/invest.feature.
func InitializeInvestScenario(ctx *godog.ScenarioContext) {
	c := &investContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = investContext{}
		return ctx, nil
	})

	ctx.Given(`^an investment ledger whose clock reads "([^"]*)" and whose actor is "([^"]*)"$`, c.aLedger)
	ctx.Given(`^the investment "([^"]*)" was added aiming "([^"]*)"$`, c.wasAdded)
	ctx.Given(`^the investment "([^"]*)" was added on "([^"]*)" aiming "([^"]*)" reviewed in (\d+) days$`, c.wasAddedOn)
	ctx.Given(`^the outcome "([^"]*)" was recorded for "([^"]*)" on "([^"]*)"$`, c.outcomeOn)
	ctx.Given(`^the outcome "([^"]*)" was recorded for "([^"]*)" on "([^"]*)" saying "([^"]*)"$`, c.outcomeOnSaying)
	ctx.Given(`^the outcome "([^"]*)" was recorded for "([^"]*)" on "([^"]*)" saying "([^"]*)" with the lesson "([^"]*)" and next "([^"]*)"$`, c.outcomeOnFully)
	ctx.When(`^the Mayor adds the investment "([^"]*)" aiming "([^"]*)" predicting "([^"]*)" measured by "([^"]*)" and reviewed in (-?\d+) days$`, c.adds)
	ctx.When(`^the Mayor records the outcome "([^"]*)" for "([^"]*)" saying "([^"]*)" with the lesson "([^"]*)"$`, c.records)
	ctx.When(`^the Mayor records the outcome "([^"]*)" for "([^"]*)" saying "([^"]*)" with the lesson "([^"]*)" and next "([^"]*)"$`, c.recordsNext)
	ctx.When(`^mw invest due runs$`, c.dueRuns)
	ctx.When(`^mw invest list runs$`, c.listRuns)
	ctx.When(`^the investment report runs$`, c.reportRuns)
	ctx.Then(`^the investment ledger file holds (\d+) lines?$`, c.fileHolds)
	ctx.Then(`^the investment ledger line (\d+) is an investment of "([^"]*)" made at "([^"]*)" by "([^"]*)" with the review in (\d+) days$`, c.lineIsInvestment)
	ctx.Then(`^the investment ledger line (\d+) is a "([^"]*)" outcome of "([^"]*)" saying "([^"]*)"$`, c.lineIsOutcome)
	ctx.Then(`^the investment command printed "([^"]*)"$`, c.printed_)
	ctx.Then(`^the investment command did not print "([^"]*)"$`, c.notPrinted)
	ctx.Then(`^the investment command is refused, saying "([^"]*)"$`, c.refused)
	ctx.Then(`^the investment report section prints "([^"]*)"$`, c.reportPrints)
	ctx.Then(`^the investment report section prints no "([^"]*)"$`, c.reportPrintsNo)
}

func (c *investContext) aLedger(now, actor string) error {
	at, err := time.Parse(time.RFC3339, now)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "mw-invest-")
	if err != nil {
		return err
	}
	c.files, c.now, c.actor = vault.New(dir), at, actor
	return nil
}

func (c *investContext) use() application.Invest {
	c.printed.Reset()
	return application.Invest{Book: c.files, Actor: c.actor, Now: func() time.Time { return c.now }, Out: &c.printed}
}

func (c *investContext) seed(rec application.InvestmentRecord) error {
	return c.files.AppendInvestment(context.Background(), rec)
}

func (c *investContext) wasAdded(bead, aim string) error {
	return c.wasAddedOn(bead, c.now.Format(time.RFC3339), aim, 7)
}

func (c *investContext) wasAddedOn(bead, when, aim string, days int) error {
	at, err := time.Parse(time.RFC3339, when)
	if err != nil {
		return err
	}
	return c.seed(application.InvestmentRecord{Kind: application.RecordInvestment, Bead: bead, At: at, Actor: c.actor, Aim: aim,
		Predict: "20% fewer tokens per landing", Measure: "tokens per landing in mw report", ReviewDays: days})
}

func (c *investContext) outcomeOn(result, bead, when string) error {
	return c.outcomeOnFully(result, bead, when, "an outcome", "a lesson was learned", "")
}

func (c *investContext) outcomeOnSaying(result, bead, when, actual string) error {
	return c.outcomeOnFully(result, bead, when, actual, "a lesson was learned", "")
}

func (c *investContext) outcomeOnFully(result, bead, when, actual, lesson, next string) error {
	at, err := time.Parse(time.RFC3339, when)
	if err != nil {
		return err
	}
	return c.seed(application.InvestmentRecord{Kind: application.RecordOutcome, Bead: bead, At: at, Actor: c.actor,
		Result: result, Actual: actual, Lesson: lesson, Next: next})
}

func (c *investContext) adds(bead, aim, predict, measure string, days int) error {
	c.err = c.use().Add(context.Background(), bead, aim, predict, measure, days)
	return nil
}

func (c *investContext) records(result, bead, actual, lesson string) error {
	return c.recordsNext(result, bead, actual, lesson, "")
}

func (c *investContext) recordsNext(result, bead, actual, lesson, next string) error {
	c.err = c.use().Outcome(context.Background(), bead, result, actual, lesson, next)
	return nil
}

func (c *investContext) dueRuns() error {
	c.err = c.use().Due(context.Background())
	return c.err
}

func (c *investContext) listRuns() error {
	c.err = c.use().List(context.Background())
	return c.err
}

// fileLines are the lines of the ledger file as it stands on disk.
func (c *investContext) fileLines() ([]string, error) {
	held, err := os.ReadFile(c.files.InvestmentsPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	text := strings.TrimRight(string(held), "\n")
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}

func (c *investContext) fileHolds(want int) error {
	lines, err := c.fileLines()
	if err != nil {
		return err
	}
	if len(lines) != want {
		return fmt.Errorf("the ledger file holds %d lines, not %d: %q", len(lines), want, lines)
	}
	return nil
}

func (c *investContext) line(n int) (application.InvestmentRecord, error) {
	lines, err := c.fileLines()
	if err != nil {
		return application.InvestmentRecord{}, err
	}
	if n < 1 || n > len(lines) {
		return application.InvestmentRecord{}, fmt.Errorf("the ledger file has no line %d (it holds %d)", n, len(lines))
	}
	var rec application.InvestmentRecord
	if err := json.Unmarshal([]byte(lines[n-1]), &rec); err != nil {
		return rec, fmt.Errorf("line %d is not a record: %w", n, err)
	}
	return rec, nil
}

func (c *investContext) lineIsInvestment(n int, bead, at, actor string, days int) error {
	rec, err := c.line(n)
	if err != nil {
		return err
	}
	if rec.Kind != application.RecordInvestment || rec.Bead != bead || rec.At.UTC().Format(time.RFC3339) != at || rec.Actor != actor || rec.ReviewDays != days {
		return fmt.Errorf("line %d is %+v", n, rec)
	}
	if rec.Aim == "" || rec.Predict == "" || rec.Measure == "" {
		return fmt.Errorf("line %d lacks an aim, a prediction or a measure: %+v", n, rec)
	}
	return nil
}

func (c *investContext) lineIsOutcome(n int, result, bead, actual string) error {
	rec, err := c.line(n)
	if err != nil {
		return err
	}
	if rec.Kind != application.RecordOutcome || rec.Result != result || rec.Bead != bead || rec.Actual != actual {
		return fmt.Errorf("line %d is %+v", n, rec)
	}
	return nil
}

func (c *investContext) printed_(want string) error {
	if !strings.Contains(c.printed.String(), want) {
		return fmt.Errorf("it printed %q, which lacks %q", c.printed.String(), want)
	}
	return nil
}

func (c *investContext) notPrinted(not string) error {
	if strings.Contains(c.printed.String(), not) {
		return fmt.Errorf("it printed %q, which has %q", c.printed.String(), not)
	}
	return nil
}

func (c *investContext) refused(saying string) error {
	if c.err == nil {
		return fmt.Errorf("it was not refused")
	}
	if !strings.Contains(c.err.Error(), saying) {
		return fmt.Errorf("it was refused, but with %q, which lacks %q", c.err.Error(), saying)
	}
	return nil
}

// reportRuns runs mw report over the window of 7 days with the investments ledger.
func (c *investContext) reportRuns() error {
	c.printed.Reset()
	now := c.now
	_, c.err = application.FactoryReport{
		Tracker: apptest.NewFakeTracker(), Files: c.files, Seat: "builder", Investments: c.files,
		Now: func() time.Time { return now }, Out: &c.printed,
	}.Run(context.Background())
	return c.err
}

// section is the INVESTMENTS part of what the report printed.
func (c *investContext) section() (string, error) {
	text := c.printed.String()
	start := strings.Index(text, "INVESTMENTS\n")
	if start < 0 {
		return "", fmt.Errorf("the report has no INVESTMENTS part: %q", text)
	}
	text = text[start:]
	if end := strings.Index(text, "CHEAP WINS"); end >= 0 {
		text = text[:end]
	}
	return text, nil
}

func (c *investContext) reportPrints(want string) error {
	section, err := c.section()
	if err != nil {
		return err
	}
	if !strings.Contains(section, want) {
		return fmt.Errorf("the INVESTMENTS part is %q, which lacks %q", section, want)
	}
	return nil
}

func (c *investContext) reportPrintsNo(not string) error {
	section, err := c.section()
	if err != nil {
		return err
	}
	if strings.Contains(section, not) {
		return fmt.Errorf("the INVESTMENTS part is %q, which has %q", section, not)
	}
	return nil
}
