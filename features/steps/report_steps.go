package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	tracker    *apptest.FakeTracker
	files      *vault.Vault
	benchmarks *apptest.FakeBenchmarks
	mailbox    *apptest.FakeMailbox
	commits    *reportCommits
	now        time.Time
	// investTarget is the [factory] invest_share the report is given; 0 is the default.
	investTarget float64

	// refused are the ids of the refused stories rigLanded filed, by rig.
	refused map[string][]string
	// stories are the ids addStory filed.
	stories []string
	// wrote is what mw report changed in the tracker or the vault, if anything.
	wrote []string

	printed bytes.Buffer
	err     error
}

// InitializeReportScenario registers the steps of features/report.feature.
func InitializeReportScenario(ctx *godog.ScenarioContext) {
	c := &reportContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = reportContext{tracker: apptest.NewFakeTracker(), benchmarks: &apptest.FakeBenchmarks{}, mailbox: apptest.NewFakeMailbox(),
			commits: &reportCommits{}, refused: map[string][]string{}}
		return ctx, nil
	})

	ctx.Given(`^a report factory whose clock reads "([^"]*)"$`, c.aFactory)
	ctx.Given(`^"([^"]*)" landed (\d+) stories and refused (\d+) for "([^"]*)" in the window$`, c.aRigLandedAndRefused)
	ctx.Given(`^"([^"]*)" landed (\d+) stories and refused (\d+) for "([^"]*)" in the window before$`, c.aRigLandedAndRefusedBefore)
	ctx.Given(`^the report benchmarks:$`, c.theBenchmarks)
	ctx.Given(`^the report result files:$`, c.theResultFiles)
	ctx.Given(`^the report stories:$`, c.theStories)
	ctx.Given(`^the report ledger holds the rows:$`, c.theLedgerRows)
	ctx.Given(`^the report story "([^"]*)" has the cause "([^"]*)"$`, c.theStoryHasTheCause)
	ctx.Given(`^every refused story of "([^"]*)" has the cause "([^"]*)"$`, c.everyRefusedStoryHasTheCause)
	ctx.Given(`^the report story "([^"]*)" carries the comment "([^"]*)"$`, c.theStoryCarriesTheComment)
	ctx.Given(`^the report stories "([^"]*)" carry the label "([^"]*)"$`, c.theStoriesCarryTheLabel)
	ctx.Given(`^the report's investment target is ([0-9.]+)$`, c.theInvestmentTarget)
	ctx.Given(`^the report ledger also holds a landed row for "([^"]*)" on "([^"]*)"$`, c.aLandedRowOn)
	ctx.Given(`^"([^"]*)" landed (\d+) and refused (\d+) on "([^"]*)"$`, c.aRigLandedAndRefusedOn)
	ctx.Given(`^the factory change "([^"]*)" titled "([^"]*)" landed on "([^"]*)" with the acceptance "([^"]*)"$`, c.aFactoryChangeLanded)
	ctx.Given(`^the millwright commit "([^"]*)" is "([^"]*)", made "([^"]*)"$`, c.aMillwrightCommit)
	ctx.When(`^the report factory clock moves on a day$`, c.theClockMovesOn)
	ctx.When(`^mw report runs with "([^"]*)"$`, c.reportRuns)
	ctx.When(`^the writes of mw report send run (\d+) times?$`, c.sendWritesRun)
	ctx.Then(`^the Mayor's inbox holds (\d+) messages? about "([^"]*)"$`, c.theMayorHasMessages)
	ctx.Then(`^the vault holds the metric snapshots of the days "([^"]*)"$`, c.theVaultHoldsSnapshots)
	ctx.Then(`^the report prints the line "([^"]*)"$`, c.printsTheLine)
	ctx.Then(`^the report prints "([^"]*)" under "([^"]*)"$`, c.printsUnder)
	ctx.Then(`^the report prints no line starting "([^"]*)"$`, c.printsNoLineStarting)
	ctx.Then(`^the report wrote nothing to the tracker or the vault$`, c.wroteNothing)
	ctx.Then(`^the "([^"]*)" section prints the line "([^"]*)"$`, c.sectionPrintsTheLine)
	ctx.Then(`^the "([^"]*)" section prints no line starting "([^"]*)"$`, c.sectionPrintsNoLine)
	ctx.Then(`^the (QUALITY|SPEED|FUEL) "([^"]*)" section prints the line "([^"]*)"$`, c.partSectionPrintsTheLine)
	ctx.Then(`^the (QUALITY|SPEED|FUEL) "([^"]*)" block "([^"]*)" prints "([^"]*)"$`, c.partBlockPrints)
	ctx.Then(`^the report puts QUALITY before SPEED before FUEL$`, c.partsAreInOrder)
	ctx.Then(`^no line of the report is wider than (\d+) columns$`, c.noLineIsWiderThan)
	ctx.Then(`^the report JSON has (\S+) as "([^"]*)"$`, c.jsonHas)
	ctx.Then(`^the MODELS columns are "([^"]*)"$`, c.modelsColumnsAre)
	ctx.Then(`^the MODELS line "([^"]*)" reads "([^"]*)"$`, c.modelsLineReads)
	ctx.Then(`^the "([^"]*)" row of the landing reads "([^"]*)" before, "([^"]*)" after, change "([^"]*)"$`, c.theLandingRowReads)
	ctx.Then(`^mw report with "([^"]*)" is refused, saying "([^"]*)"$`, c.reportIsRefused)
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
	c.stories = append(c.stories, id)
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

func (c *reportContext) appendLedger(day, id, outcome, worker string, tokens int) error {
	at, err := time.Parse(application.LedgerDate, day)
	if err != nil {
		return err
	}
	path := domain.Path{}
	model, effort, _ := strings.Cut(worker, "/")
	path.Model, path.Effort = domain.Model(model), domain.Effort(effort)
	line := application.LedgerLine{When: at, StoryID: id, Title: "The story " + id, Outcome: outcome, Path: path,
		Result: application.SessionResult{Fuel: application.Fuel{Input: tokens}}}.String()
	return c.files.AppendToLedger(context.Background(), reportSeat, line)
}

func (c *reportContext) aRigLandedAndRefused(rig string, landed, refused int, reason string) error {
	return c.rigLanded("2026-10-08", "", rig, landed, refused, reason)
}

func (c *reportContext) aRigLandedAndRefusedBefore(rig string, landed, refused int, reason string) error {
	return c.rigLanded("2026-09-30", "before-", rig, landed, refused, reason)
}

// rigLanded files landed and refused stories of a rig, with a ledger row each on day.
func (c *reportContext) rigLanded(day, infix, rig string, landed, refused int, reason string) error {
	for i := 1; i <= landed+refused; i++ {
		id, outcome := fmt.Sprintf("%s-%sl%d", rig, infix, i), "landed on main"
		if i > landed {
			id, outcome = fmt.Sprintf("%s-%sr%d", rig, infix, i-landed), application.NotLanded(application.Reason(reason), "the gate was red")
			c.refused[rig] = append(c.refused[rig], id)
		}
		if err := c.addStory(id, "The story "+id, rig, "laptop", "sonnet", 0, time.Time{}); err != nil {
			return err
		}
		if err := c.appendLedger(day, id, outcome, "sonnet/high", 0); err != nil {
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
		if _, has := cols["description"]; has && cell("description") != "" {
			if err := c.tracker.SetDescription(cell("id"), cell("description")); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *reportContext) theLedgerRows(table *godog.Table) error {
	cols := columns(table)
	for _, row := range table.Rows[1:] {
		cell := func(name string) string { return strings.TrimSpace(row.Cells[cols[name]].Value) }
		tokens := 0
		if _, has := cols["tokens"]; has {
			tokens, _ = strconv.Atoi(cell("tokens"))
		}
		if err := c.appendLedger(cell("day"), cell("id"), cell("outcome"), cell("worker"), tokens); err != nil {
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
	return c.appendLedger(day, id, "landed on main", "sonnet/high", 0)
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

func (c *reportContext) everyRefusedStoryHasTheCause(rig, class string) error {
	for _, id := range c.refused[rig] {
		if err := c.theStoryHasTheCause(id, class); err != nil {
			return err
		}
	}
	return nil
}

// theStoriesCarryTheLabel labels each of a comma-separated list of stories.
func (c *reportContext) theStoriesCarryTheLabel(ids, label string) error {
	for _, id := range strings.Split(ids, ",") {
		if err := c.tracker.AddLabel(context.Background(), strings.TrimSpace(id), label); err != nil {
			return err
		}
	}
	return nil
}

func (c *reportContext) theInvestmentTarget(share string) error {
	target, err := strconv.ParseFloat(share, 64)
	c.investTarget = target
	return err
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
	from, around, err := application.ReadReportSince(since, now)
	report.Since, report.Around = from, around
	return report, err
}

// aRigLandedAndRefusedOn files landed and refused stories of a rig with a
// ledger row each on a day of the scenario's choosing, inside the window or not.
func (c *reportContext) aRigLandedAndRefusedOn(rig string, landed, refused int, day string) error {
	return c.rigLanded(day, day+"-", rig, landed, refused, "tests-fail")
}

// aFactoryChangeLanded files a millwright story with acceptance criteria and a
// landed row for it on day.
func (c *reportContext) aFactoryChangeLanded(id, title, day, acceptance string) error {
	if err := c.addStory(id, title, "millwright", "laptop", "sonnet", 0, time.Time{}); err != nil {
		return err
	}
	if err := c.tracker.SetAcceptance(id, unescape(acceptance)); err != nil {
		return err
	}
	return c.appendLedger(day, id, "landed on main", "sonnet/high", 0)
}

func (c *reportContext) theClockMovesOn() error {
	c.now = c.now.AddDate(0, 0, 1)
	return nil
}

// sendWritesRun runs the report the way mw report send does, with the means to
// tell the Mayor and to keep the snapshot, n times.
func (c *reportContext) sendWritesRun(n int) error {
	for i := 0; i < n; i++ {
		from, err := application.ParseReportSince("7d", c.now)
		if err != nil {
			return err
		}
		report := application.FactoryReport{Tracker: c.tracker, Files: c.files, Seat: reportSeat, Benchmarks: c.benchmarks,
			Since: from, Now: func() time.Time { return c.now },
			Notes: c.tracker, Mailbox: c.mailbox, Snapshots: c.files}
		if _, err := report.Run(context.Background()); err != nil {
			return fmt.Errorf("the report send writes failed: %w", err)
		}
	}
	return nil
}

func (c *reportContext) theMayorHasMessages(want int, about string) error {
	inbox, err := c.mailbox.Inbox(context.Background(), application.MayorMailbox)
	if err != nil {
		return err
	}
	got := 0
	for _, m := range inbox {
		if strings.Contains(m.Subject+m.Body, about) {
			got++
		}
	}
	if got != want {
		return fmt.Errorf("expected %d messages to the Mayor about %q, found %d in %+v", want, about, got, inbox)
	}
	return nil
}

func (c *reportContext) theVaultHoldsSnapshots(days string) error {
	kept, err := c.files.Snapshots(context.Background())
	if err != nil {
		return err
	}
	var got []string
	for _, s := range kept {
		got = append(got, s.Day)
	}
	if strings.Join(got, ",") != days {
		return fmt.Errorf("expected the snapshots of the days %q, the vault holds %q", days, strings.Join(got, ","))
	}
	return nil
}

func (c *reportContext) reportRuns(flags string) error {
	c.printed.Reset()
	report, err := reportFlags(flags, c.now)
	if err != nil {
		return err
	}
	report.Tracker, report.Files, report.Seat = c.tracker, c.files, reportSeat
	report.Benchmarks = c.benchmarks
	report.Commits, report.FactoryDir = c.commits, reportFactoryDir
	report.InvestShare = c.investTarget
	report.Now = func() time.Time { return c.now }
	report.Out = &c.printed
	before := c.footprint()
	_, c.err = report.Run(context.Background())
	if after := c.footprint(); after != before {
		c.wrote = append(c.wrote, "before: "+before, "after: "+after)
	}
	if c.err != nil {
		return fmt.Errorf("mw report failed: %w", c.err)
	}
	return nil
}

// footprint is what a run could have changed: every file in the vault with its
// size, what the tracker was asked and the comments on its stories.
func (c *reportContext) footprint() string {
	var b strings.Builder
	_ = filepath.Walk(c.files.Dir(), func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			fmt.Fprintf(&b, "%s %d\n", path, info.Size())
		}
		return nil
	})
	fmt.Fprintf(&b, "asked %v\n", c.tracker.Asked())
	for _, id := range c.stories {
		fmt.Fprintf(&b, "%s %v %v\n", id, c.tracker.Comments(id), c.tracker.Metadata(id))
	}
	return b.String()
}

func (c *reportContext) wroteNothing() error {
	if len(c.wrote) > 0 {
		return fmt.Errorf("mw report changed something:\n%s", strings.Join(c.wrote, "\n"))
	}
	return nil
}

func (c *reportContext) printsNoLineStarting(prefix string) error {
	for _, line := range c.lines() {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return fmt.Errorf("expected no line starting %q, got:\n%s", prefix, c.printed.String())
		}
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

// theBenchmarks records the close-outs of landed stories: a ratio is the story's
// actual over its par, a gate the seconds its tests ran.
func (c *reportContext) theBenchmarks(table *godog.Table) error {
	cols := columns(table)
	for _, row := range table.Rows[1:] {
		cell := func(name string) string {
			if at, has := cols[name]; has {
				return strings.TrimSpace(row.Cells[at].Value)
			}
			return ""
		}
		day, err := time.Parse(application.LedgerDate, cell("day"))
		if err != nil {
			return err
		}
		b := application.Benchmark{Story: cell("story"), Rig: cell("rig"), Host: cell("host"), Kind: cell("kind"), At: day.Add(12 * time.Hour), Landed: true}
		b.ActualS, _ = strconv.ParseFloat(cell("actual"), 64)
		b.GateSeconds, _ = strconv.ParseFloat(cell("gate"), 64)
		if ratio, _ := strconv.ParseFloat(cell("ratio"), 64); ratio > 0 && b.ActualS > 0 {
			b.ParS = b.ActualS / ratio
		}
		c.benchmarks.Records = append(c.benchmarks.Records, b)
	}
	return nil
}

// theResultFiles writes the result file of a story's attempt with the cost it reports.
func (c *reportContext) theResultFiles(table *godog.Table) error {
	cols := columns(table)
	for _, row := range table.Rows[1:] {
		cell := func(name string) string { return strings.TrimSpace(row.Cells[cols[name]].Value) }
		attempt, _ := strconv.Atoi(cell("attempt"))
		file := c.files.RunFile(cell("story"), application.ResultFileNameForAttempt(attempt))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(file, []byte(fmt.Sprintf(`{"subtype":"success","total_cost_usd":%s}`, cell("cost"))), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// parts splits the report at the lines that head QUALITY, SPEED and FUEL.
func (c *reportContext) parts() map[string][]string {
	found := map[string][]string{}
	current := ""
	for _, line := range c.lines() {
		for _, part := range []string{"QUALITY", "SPEED", "FUEL", "MODELS"} {
			if line == part || strings.HasPrefix(line, part+" ") {
				current = part
			}
		}
		if current != "" {
			found[current] = append(found[current], line)
		}
	}
	return found
}

// partSection is the lines under "by <kind>" in one part of the report.
func (c *reportContext) partSection(part, kind string) ([]string, error) {
	lines, ok := c.parts()[part]
	if !ok {
		return nil, fmt.Errorf("expected the report to have a %s part, got:\n%s", part, c.printed.String())
	}
	var found []string
	in := false
	for _, line := range lines {
		switch {
		case strings.TrimSpace(line) == "by "+kind:
			in = true
		case strings.HasPrefix(line, "by "):
			in = false
		case in:
			found = append(found, line)
		}
	}
	return found, nil
}

func (c *reportContext) partSectionPrintsTheLine(part, kind, want string) error {
	lines, err := c.partSection(part, kind)
	if err != nil {
		return err
	}
	for _, line := range lines {
		if strings.TrimSpace(line) == want {
			return nil
		}
	}
	return fmt.Errorf("expected the %s part's %s section to print %q, got:\n%s", part, kind, want, c.printed.String())
}

// partBlockPrints looks, in the block that begins with name, for a line holding want.
func (c *reportContext) partBlockPrints(part, kind, name, want string) error {
	lines, err := c.partSection(part, kind)
	if err != nil {
		return err
	}
	for i, line := range lines {
		if strings.TrimSpace(line) != name {
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
	}
	return fmt.Errorf("expected %q in the %s block %q of %s, got:\n%s", want, kind, name, part, c.printed.String())
}

func (c *reportContext) partsAreInOrder() error {
	text := c.printed.String()
	last := -1
	for _, part := range []string{"QUALITY", "SPEED", "FUEL"} {
		at := strings.Index(text, "\n"+part)
		if part == "QUALITY" {
			at = strings.Index(text, part)
		}
		if at < 0 || at <= last {
			return fmt.Errorf("expected QUALITY, then SPEED, then FUEL, got:\n%s", text)
		}
		last = at
	}
	return nil
}

// reportFactoryDir is where the report scenarios say the factory's rig is
// checked out; the fake commits answer only for it.
const reportFactoryDir = "/rigs/millwright"

// reportCommits are the commits of the factory's rig a scenario made, by the
// name it gave them.
type reportCommits struct {
	commits map[string]application.RigCommit
}

// CommitOf implements application.FactoryCommits.
func (f *reportCommits) CommitOf(_ context.Context, rigDir, rev string) (application.RigCommit, error) {
	if rigDir != reportFactoryDir {
		return application.RigCommit{}, fmt.Errorf("no rig is checked out at %s", rigDir)
	}
	commit, ok := f.commits[rev]
	if !ok {
		return application.RigCommit{}, fmt.Errorf("%s is not a commit", rev)
	}
	return commit, nil
}

func (c *reportContext) aMillwrightCommit(name, subject, made string) error {
	at, err := time.Parse(time.RFC3339, made)
	if err != nil {
		return err
	}
	if c.commits.commits == nil {
		c.commits.commits = map[string]application.RigCommit{}
	}
	c.commits.commits[name] = application.RigCommit{Branch: "main", Commit: name, Subject: subject, At: at}
	return nil
}

// theLandingRowReads finds the row of mw report --since <landing> whose first
// 22 columns hold the measure, and reads its before, after and change.
func (c *reportContext) theLandingRowReads(measure, before, after, change string) error {
	for _, line := range c.lines() {
		if !strings.HasPrefix(line, "  ") || len(line) < 22 || strings.TrimSpace(line[:22]) != measure {
			continue
		}
		fields := strings.Fields(line[22:])
		if len(fields) < 3 {
			return fmt.Errorf("the %q row has no before, after and change: %q", measure, line)
		}
		got := fmt.Sprintf("%s|%s|%s", fields[0], fields[1], strings.Join(fields[2:], " "))
		if want := fmt.Sprintf("%s|%s|%s", before, after, change); got != want {
			return fmt.Errorf("the %q row reads %q, want %q:\n%s", measure, got, want, c.printed.String())
		}
		return nil
	}
	return fmt.Errorf("the report has no %q row:\n%s", measure, c.printed.String())
}

func (c *reportContext) reportIsRefused(flags, saying string) error {
	err := c.reportRuns(flags)
	if err == nil {
		return fmt.Errorf("mw report %s was not refused; it printed:\n%s", flags, c.printed.String())
	}
	if !strings.Contains(err.Error(), saying) {
		return fmt.Errorf("mw report %s was refused saying %q, want it to say %q", flags, err, saying)
	}
	return nil
}

// models is the MODELS part: its heading line, then its lines up to the next
// part, each trimmed.
func (c *reportContext) models() ([]string, error) {
	lines, ok := c.parts()["MODELS"]
	if !ok {
		return nil, fmt.Errorf("expected the report to have a MODELS part, got:\n%s", c.printed.String())
	}
	var found []string
	for i, line := range lines {
		if i > 0 && line != "" && line == strings.ToUpper(line) && !strings.HasPrefix(line, " ") && strings.ContainsAny(line, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
			break
		}
		found = append(found, strings.TrimSpace(line))
	}
	return found, nil
}

func (c *reportContext) modelsColumnsAre(want string) error {
	lines, err := c.models()
	if err != nil {
		return err
	}
	if got := strings.Join(strings.Fields(lines[0])[1:], " "); got != want {
		return fmt.Errorf("expected the MODELS columns %q, got %q in:\n%s", want, got, c.printed.String())
	}
	return nil
}

// modelsLineReads finds the MODELS line that begins with label and compares
// the cells after it, spaces between them made one.
func (c *reportContext) modelsLineReads(label, want string) error {
	lines, err := c.models()
	if err != nil {
		return err
	}
	for _, line := range lines {
		if rest, ok := strings.CutPrefix(line, label+" "); ok {
			if got := strings.Join(strings.Fields(rest), " "); got != want {
				return fmt.Errorf("expected the MODELS line %q to read %q, got %q in:\n%s", label, want, got, c.printed.String())
			}
			return nil
		}
	}
	return fmt.Errorf("expected a MODELS line %q, got:\n%s", label, c.printed.String())
}
