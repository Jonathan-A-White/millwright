package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// reportSeat is the seat whose ledger the report scenarios fill.
const reportSeat = "builder"

// reportContext holds the fake tracker, a real vault in a temp directory that
// holds the ledger, and what mw report printed.
type reportContext struct {
	tracker *apptest.FakeTracker
	files   *vault.Vault
	now     time.Time

	printed bytes.Buffer
	err     error
}

// InitializeReportScenario registers the steps of features/report.feature.
func InitializeReportScenario(ctx *godog.ScenarioContext) {
	c := &reportContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = reportContext{tracker: apptest.NewFakeTracker()}
		return ctx, nil
	})

	ctx.Given(`^a report factory whose clock reads "([^"]*)"$`, c.aFactory)
	ctx.Given(`^"([^"]*)" landed (\d+) stories and refused (\d+) for "([^"]*)" in the window$`, c.aRigLandedAndRefused)
	ctx.Given(`^the report stories:$`, c.theStories)
	ctx.Given(`^the report ledger holds the rows:$`, c.theLedgerRows)
	ctx.Given(`^the report story "([^"]*)" has the cause "([^"]*)"$`, c.theStoryHasTheCause)
	ctx.Given(`^the report story "([^"]*)" carries the comment "([^"]*)"$`, c.theStoryCarriesTheComment)
	ctx.Given(`^the report ledger also holds a landed row for "([^"]*)" on "([^"]*)"$`, c.aLandedRowOn)
	ctx.When(`^mw report runs with "([^"]*)"$`, c.reportRuns)
	ctx.Then(`^the report prints the line "([^"]*)"$`, c.printsTheLine)
	ctx.Then(`^the report prints "([^"]*)" under "([^"]*)"$`, c.printsUnder)
	ctx.Then(`^the "([^"]*)" section prints the line "([^"]*)"$`, c.sectionPrintsTheLine)
	ctx.Then(`^the "([^"]*)" section prints no line starting "([^"]*)"$`, c.sectionPrintsNoLine)
	ctx.Then(`^no line of the report is wider than (\d+) columns$`, c.noLineIsWiderThan)
	ctx.Then(`^the report JSON has (\S+) as "([^"]*)"$`, c.jsonHas)
}

func (c *reportContext) aFactory(now string) error {
	at, err := time.Parse(time.RFC3339, now)
	if err != nil {
		return err
	}
	c.now = at
	dir, err := os.MkdirTemp("", "mw-report-")
	if err != nil {
		return err
	}
	c.files = vault.New(dir)
	return nil
}

// addStory files a story on the fake tracker with the path the report reads.
func (c *reportContext) addStory(id, title, rig, host, model string, attempts int, filed time.Time) error {
	ctx := context.Background()
	c.tracker.AddStory("", domain.Story{
		ID: id, Title: title,
		Overrides: domain.Path{Rig: rig, Host: host, Model: domain.Model(model)},
	})
	if attempts > 0 {
		if err := c.tracker.SetStoryMetadata(ctx, id, map[string]string{application.AttemptsField: strconv.Itoa(attempts)}); err != nil {
			return err
		}
	}
	if !filed.IsZero() {
		if err := c.tracker.SetCreated(id, filed); err != nil {
			return err
		}
	}
	return c.tracker.SetUpdated(id, c.now)
}

func (c *reportContext) appendLedger(day, id, outcome, worker string) error {
	at, err := time.Parse(application.LedgerDate, day)
	if err != nil {
		return err
	}
	path := domain.Path{}
	model, effort, _ := strings.Cut(worker, "/")
	path.Model, path.Effort = domain.Model(model), domain.Effort(effort)
	line := application.LedgerLine{When: at, StoryID: id, Title: "The story " + id, Outcome: outcome, Path: path}.String()
	return c.files.AppendToLedger(context.Background(), reportSeat, line)
}

func (c *reportContext) aRigLandedAndRefused(rig string, landed, refused int, reason string) error {
	for i := 1; i <= landed+refused; i++ {
		id, outcome := fmt.Sprintf("%s-l%d", rig, i), "landed on main"
		if i > landed {
			id, outcome = fmt.Sprintf("%s-r%d", rig, i-landed), application.NotLanded(application.Reason(reason), "the gate was red")
		}
		if err := c.addStory(id, "The story "+id, rig, "laptop", "sonnet", 0, time.Time{}); err != nil {
			return err
		}
		if err := c.appendLedger("2026-10-08", id, outcome, "sonnet/high"); err != nil {
			return err
		}
	}
	return nil
}

func (c *reportContext) theStories(table *godog.Table) error {
	cols := columns(table)
	for _, row := range table.Rows[1:] {
		cell := func(name string) string { return strings.TrimSpace(row.Cells[cols[name]].Value) }
		attempts, _ := strconv.Atoi(cell("attempts"))
		var filed time.Time
		if day := cell("filed"); day != "" {
			var err error
			if filed, err = time.Parse(application.LedgerDate, day); err != nil {
				return err
			}
		}
		if err := c.addStory(cell("id"), cell("title"), cell("rig"), cell("host"), cell("model"), attempts, filed); err != nil {
			return err
		}
	}
	return nil
}

func (c *reportContext) theLedgerRows(table *godog.Table) error {
	cols := columns(table)
	for _, row := range table.Rows[1:] {
		cell := func(name string) string { return strings.TrimSpace(row.Cells[cols[name]].Value) }
		if err := c.appendLedger(cell("day"), cell("id"), cell("outcome"), cell("worker")); err != nil {
			return err
		}
	}
	return nil
}

func (c *reportContext) aLandedRowOn(rig, day string) error {
	id := rig + "-earlier"
	if err := c.addStory(id, "The story "+id, rig, "laptop", "sonnet", 0, time.Time{}); err != nil {
		return err
	}
	return c.appendLedger(day, id, "landed on main", "sonnet/high")
}

// columns maps a table's header cells to their index.
func columns(table *godog.Table) map[string]int {
	cols := map[string]int{}
	for i, cell := range table.Rows[0].Cells {
		cols[strings.TrimSpace(cell.Value)] = i
	}
	return cols
}

func (c *reportContext) theStoryHasTheCause(id, class string) error {
	return c.tracker.SetStoryMetadata(context.Background(), id, map[string]string{application.CauseField: class})
}

func (c *reportContext) theStoryCarriesTheComment(id, text string) error {
	return c.tracker.CommentOnStory(context.Background(), id, unescape(text))
}

// reportFlags reads the flags a scenario names the way the command line would.
func reportFlags(text string, now time.Time) (application.FactoryReport, error) {
	var report application.FactoryReport
	fields := strings.Fields(text)
	since := ""
	for i := 0; i < len(fields); i++ {
		switch fields[i] {
		case "--since":
			i++
			since = fields[i]
		case "--rig":
			i++
			report.Rig = fields[i]
		case "--host":
			i++
			report.Host = fields[i]
		case "--json":
			report.JSON = true
		default:
			return report, fmt.Errorf("the scenario names a flag mw report does not take: %q", fields[i])
		}
	}
	from, err := application.ParseReportSince(since, now)
	report.Since = from
	return report, err
}

func (c *reportContext) reportRuns(flags string) error {
	c.printed.Reset()
	report, err := reportFlags(flags, c.now)
	if err != nil {
		return err
	}
	report.Tracker, report.Files, report.Seat = c.tracker, c.files, reportSeat
	report.Benchmarks = &apptest.FakeBenchmarks{}
	report.Now = func() time.Time { return c.now }
	report.Out = &c.printed
	_, c.err = report.Run(context.Background())
	if c.err != nil {
		return fmt.Errorf("mw report failed: %w", c.err)
	}
	return nil
}

func (c *reportContext) lines() []string {
	return strings.Split(c.printed.String(), "\n")
}

func (c *reportContext) printsTheLine(want string) error {
	for _, line := range c.lines() {
		if strings.TrimSpace(line) == want {
			return nil
		}
	}
	return fmt.Errorf("expected the report to print the line %q, got:\n%s", want, c.printed.String())
}

func (c *reportContext) printsUnder(want, header string) error {
	return blockHas(c.lines(), header, want, c.printed.String())
}

// blockHas looks in the lines after header, up to the next blank line, for want.
func blockHas(lines []string, header, want, whole string) error {
	for i, line := range lines {
		if strings.TrimSpace(line) != header {
			continue
		}
		for _, next := range lines[i+1:] {
			if strings.TrimSpace(next) == "" {
				break
			}
			if strings.Contains(next, want) {
				return nil
			}
		}
		return fmt.Errorf("expected %q under %q, got:\n%s", want, header, whole)
	}
	return fmt.Errorf("expected the report to print %q, got:\n%s", header, whole)
}

// section is the lines under the heading "by <kind>", up to the next heading.
func (c *reportContext) section(kind string) ([]string, error) {
	var found []string
	in := false
	for _, line := range c.lines() {
		switch {
		case strings.TrimSpace(line) == "by "+kind:
			in = true
		case strings.HasPrefix(line, "by "):
			in = false
		case in:
			found = append(found, strings.TrimSpace(line))
		}
	}
	if len(found) == 0 && !strings.Contains(c.printed.String(), "by "+kind) {
		return nil, fmt.Errorf("expected a %q section, got:\n%s", "by "+kind, c.printed.String())
	}
	return found, nil
}

func (c *reportContext) sectionPrintsTheLine(kind, want string) error {
	lines, err := c.section(kind)
	if err != nil {
		return err
	}
	for _, line := range lines {
		if line == want {
			return nil
		}
	}
	return fmt.Errorf("expected the %s section to print %q, got:\n%s", kind, want, c.printed.String())
}

func (c *reportContext) sectionPrintsNoLine(kind, prefix string) error {
	lines, err := c.section(kind)
	if err != nil {
		return err
	}
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return fmt.Errorf("expected no line starting %q in the %s section, got:\n%s", prefix, kind, c.printed.String())
		}
	}
	return nil
}

func (c *reportContext) noLineIsWiderThan(width int) error {
	for _, line := range c.lines() {
		if n := utf8.RuneCountInString(line); n > width {
			return fmt.Errorf("a line is %d columns wide, over %d: %q", n, width, line)
		}
	}
	return nil
}

func (c *reportContext) jsonHas(path, want string) error {
	var node any
	if err := json.Unmarshal(c.printed.Bytes(), &node); err != nil {
		return fmt.Errorf("the report is not JSON: %w\n%s", err, c.printed.String())
	}
	for _, part := range strings.Split(path, ".") {
		switch typed := node.(type) {
		case map[string]any:
			node = typed[part]
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(typed) {
				return fmt.Errorf("the JSON has nothing at %s", path)
			}
			node = typed[i]
		default:
			return fmt.Errorf("the JSON has nothing at %s", path)
		}
	}
	if got := fmt.Sprint(node); got != want {
		return fmt.Errorf("expected %s to be %q, it is %q in:\n%s", path, want, got, c.printed.String())
	}
	return nil
}
