package steps

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// What a status scenario's fixtures hold.
const (
	statusHost = "vps"
	statusSeat = "builder"
)

// statusContext holds a fake work tracker and a fake runner standing in for
// the two ports mw status must never write through, and a real vault
// directory in a temp dir for the one thing it does read: the seat's ledger.
// Nothing here reaches the factory's own vault, beads database or terminal.
type statusContext struct {
	root string

	tracker *apptest.FakeTracker
	runner  *apptest.FakeRunner

	lastEpic string
	now      time.Time
	// silentHoursWas is what $MW_HOST_SILENT_HOURS held before this scenario
	// pinned it, so that the environment is left exactly as it was found.
	silentHoursWas string
	silentHoursSet bool
	// elsewhereWas is what each story pathed to another host looked like just
	// before mw status ran — its host, its status and the note its host left —
	// so that a scenario can say the report re-pathed and recorded nothing.
	elsewhereWas map[string]string
	// askedBefore is how many calls the tracker had logged just before mw
	// status ran, so that a scenario can say what it asked and nothing more.
	askedBefore int

	report application.StatusReport
	err    error
}

// InitializeStatusScenario registers the steps of features/status.feature.
func InitializeStatusScenario(ctx *godog.ScenarioContext) {
	c := &statusContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		was, set := os.LookupEnv(config.HostSilenceEnv)
		*c = statusContext{
			tracker:        apptest.NewFakeTracker(),
			runner:         apptest.NewFakeRunner(),
			now:            time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC),
			silentHoursWas: was,
			silentHoursSet: set,
			elsewhereWas:   map[string]string{},
		}
		// Every status scenario reads its threshold out of the configuration,
		// and pins it in the environment so that no scenario ever reads this
		// machine's own config file. The value is the default the config
		// package would have given anyway.
		return ctx, os.Setenv(config.HostSilenceEnv, strconv.Itoa(config.DefaultHostSilentHours))
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.root != "" {
			_ = os.RemoveAll(c.root)
		}
		if c.silentHoursSet {
			return ctx, os.Setenv(config.HostSilenceEnv, c.silentHoursWas)
		}
		return ctx, os.Unsetenv(config.HostSilenceEnv)
	})

	ctx.Given(`^the status epic "([^"]*)" on the default path:$`, c.theStatusEpicOnTheDefaultPath)
	ctx.Given(`^a status story "([^"]*)" filed under it$`, c.aStatusStoryFiledUnderIt)
	ctx.Given(`^a status story "([^"]*)" titled "([^"]*)" filed under it$`, c.aStatusStoryTitledFiledUnderIt)
	ctx.Given(`^a status story "([^"]*)" filed under it, overriding "([^"]*)" with "([^"]*)"$`, c.aStatusStoryOverriding)
	ctx.Given(`^a status story "([^"]*)" filed under it, waiting on "([^"]*)"$`, c.aStatusStoryWaitingOn)
	ctx.Given(`^the status story "([^"]*)" is claimed with its session running$`, c.theStatusStoryIsClaimedAndRunning)
	ctx.Given(`^the status story "([^"]*)" is marked run=(\S+)$`, c.theStatusStoryIsMarkedRun)
	ctx.Given(`^the formula poured for "([^"]*)" has a step still open$`, c.theFormulaPouredHasAStepStillOpen)
	ctx.Given(`^a status story "([^"]*)" titled "([^"]*)" pathed to the host "([^"]*)"$`, c.aStatusStoryTitledPathedToTheHost)
	ctx.Given(`^the builder's ledger holds a line from (today|\d{4}-\d{2}-\d{2}) burning (\d+) tokens$`,
		c.theBuildersLedgerHoldsALineBurning)
	ctx.Given(`^the host "([^"]*)" last synced (\d+) hours? ago$`, c.theHostLastSyncedHoursAgo)
	ctx.Given(`^the host "([^"]*)" has never synced$`, c.theHostHasNeverSynced)
	ctx.Given(`^the host "([^"]*)" left a last sync note that is not a time$`, c.theHostLeftANoteThatIsNotATime)
	ctx.Given(`^the configuration says a host is asleep after (\d+) hours$`, c.theConfigurationSaysAHostIsAsleepAfter)

	ctx.When(`^mw status reads the host$`, c.mwStatusReadsTheHost)

	ctx.Then(`^reading status succeeds$`, c.readingStatusSucceeds)
	ctx.Then(`^the report shows "([^"]*)" running with session "([^"]*)"$`, c.theReportShowsRunningWithSession)
	ctx.Then(`^the report lists "([^"]*)" as ready$`, c.theReportListsAsReady)
	ctx.Then(`^the report lists "([^"]*)" as blocked$`, c.theReportListsAsBlocked)
	ctx.Then(`^the report shows "([^"]*)" on the rig "([^"]*)"$`, c.theReportShowsOnTheRig)
	ctx.Then(`^the report says the close-out of "([^"]*)" is blocked by an open formula step$`,
		c.theReportSaysCloseOutBlockedByFormula)
	ctx.Then(`^the report shows "([^"]*)" as run=(\S+), not running$`, c.theReportShowsRunState)
	ctx.Then(`^the report says today's fuel is (.+)$`, c.theReportSaysTodaysFuelIs)
	ctx.Then(`^every line of the report is at most 60 columns wide$`, c.everyLineIsAtMost60ColumnsWide)
	ctx.Then(`^nothing was written through the tracker, the ledger or the runner$`, c.nothingWasWritten)
	ctx.Then(`^the report lists "([^"]*)" under the other host "([^"]*)"$`, c.theReportListsUnderTheOtherHost)
	ctx.Then(`^the report lists "([^"]*)" as stranded on the host "([^"]*)"$`, c.theReportListsAsStrandedOn)
	ctx.Then(`^the report does not call the host "([^"]*)" asleep$`, c.theReportDoesNotCallTheHostAsleep)
	ctx.Then(`^the report shows the last sync time of the host "([^"]*)"$`, c.theReportShowsTheLastSyncTimeOf)
	ctx.Then(`^the report says the host "([^"]*)" has never synced$`, c.theReportSaysTheHostHasNeverSynced)
	ctx.Then(`^the report says how to re-path a story stranded on the host "([^"]*)"$`, c.theReportSaysHowToRePathFrom)
	ctx.Then(`^nothing pathed to another host was re-pathed or touched$`, c.nothingElsewhereWasTouched)
}

// workspace makes the temp directory this scenario keeps its vault in, once.
func (c *statusContext) workspace() (string, error) {
	if c.root != "" {
		return c.root, nil
	}
	root, err := os.MkdirTemp("", "mw-status-")
	if err != nil {
		return "", fmt.Errorf("making a workspace: %w", err)
	}
	c.root = root
	return root, nil
}

func (c *statusContext) theStatusEpicOnTheDefaultPath(id string, table *godog.Table) error {
	defaults := domain.Path{}
	for _, row := range table.Rows {
		if len(row.Cells) != 2 {
			return fmt.Errorf("a default path row needs a field and a value, got %d cells", len(row.Cells))
		}
		if err := defaults.Set(row.Cells[0].Value, row.Cells[1].Value); err != nil {
			return err
		}
	}
	c.tracker.AddEpic(id, defaults)
	c.lastEpic = id
	return nil
}

func (c *statusContext) aStatusStoryFiledUnderIt(id string) error {
	c.tracker.AddStory(c.lastEpic, domain.Story{ID: id, Title: id})
	return nil
}

func (c *statusContext) aStatusStoryTitledFiledUnderIt(id, title string) error {
	c.tracker.AddStory(c.lastEpic, domain.Story{ID: id, Title: title})
	return nil
}

func (c *statusContext) aStatusStoryOverriding(id, field, value string) error {
	story := domain.Story{ID: id, Title: id}
	if err := story.Overrides.Set(field, value); err != nil {
		return err
	}
	c.tracker.AddStory(c.lastEpic, story)
	return nil
}

// aStatusStoryTitledPathedToTheHost files a story under the epic with a title
// of its own and a Path that names another host — what the other-hosts section
// is made of.
func (c *statusContext) aStatusStoryTitledPathedToTheHost(id, title, host string) error {
	story := domain.Story{ID: id, Title: title}
	if err := story.Overrides.Set("host", host); err != nil {
		return err
	}
	c.tracker.AddStory(c.lastEpic, story)
	return nil
}

// theHostLastSyncedHoursAgo leaves the note a host writes for itself when a
// sync finishes, dated against this scenario's clock.
func (c *statusContext) theHostLastSyncedHoursAgo(host, hoursText string) error {
	hours, err := strconv.Atoi(hoursText)
	if err != nil {
		return fmt.Errorf("the hours %q are not a number: %w", hoursText, err)
	}
	at := c.now.Add(-time.Duration(hours) * time.Hour).UTC().Format(application.LastSyncFormat)
	return c.tracker.SetNote(context.Background(), application.LastSyncKey(host), at)
}

// theHostHasNeverSynced is the absence of a note, said out loud: a host nobody
// has ever recorded a sync for leaves the key-value store empty.
func (c *statusContext) theHostHasNeverSynced(host string) error {
	if said, err := c.tracker.Note(context.Background(), application.LastSyncKey(host)); err != nil || said != "" {
		return fmt.Errorf("expected %s to have no last sync note, got %q: %v", host, said, err)
	}
	return nil
}

func (c *statusContext) theHostLeftANoteThatIsNotATime(host string) error {
	return c.tracker.SetNote(context.Background(), application.LastSyncKey(host), "a while back")
}

// theConfigurationSaysAHostIsAsleepAfter pins the threshold in the environment
// the config package reads first, so that the scenario really does get its
// threshold out of the configuration and never off this machine's config file.
func (c *statusContext) theConfigurationSaysAHostIsAsleepAfter(hours string) error {
	return os.Setenv(config.HostSilenceEnv, hours)
}

func (c *statusContext) aStatusStoryWaitingOn(id, need string) error {
	c.tracker.AddStory(c.lastEpic, domain.Story{ID: id, Title: id})
	c.tracker.Needs(id, need)
	return nil
}

func (c *statusContext) theStatusStoryIsClaimedAndRunning(id string) error {
	ctx := context.Background()
	if err := c.tracker.ClaimStory(ctx, id); err != nil {
		return err
	}
	return c.tracker.SetStoryState(ctx, id, application.RunState, application.RunRunning, "dispatched by the test")
}

func (c *statusContext) theStatusStoryIsMarkedRun(id, run string) error {
	return c.tracker.SetStoryState(context.Background(), id, application.RunState, run, "recorded by the test")
}

// theFormulaPouredHasAStepStillOpen pours a formula for a story the way a
// dispatch does, and closes every step but the last: a session that stopped
// short of finishing it.
func (c *statusContext) theFormulaPouredHasAStepStillOpen(id string) error {
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

// theBuildersLedgerHoldsALineBurning appends one real ledger line to a real
// vault directory in this scenario's own temp workspace, dated either today
// (the clock mw status is given) or a literal date, burning the tokens given.
func (c *statusContext) theBuildersLedgerHoldsALineBurning(when, tokensText string) error {
	tokens, err := strconv.Atoi(tokensText)
	if err != nil {
		return fmt.Errorf("the token count %q is not a number: %w", tokensText, err)
	}
	date := c.now
	if when != "today" {
		parsed, err := time.Parse(application.LedgerDate, when)
		if err != nil {
			return fmt.Errorf("parsing the date %q: %w", when, err)
		}
		date = parsed
	}
	line := application.LedgerLine{
		When:    date,
		StoryID: "mw-old.1",
		Title:   "an earlier story",
		Outcome: "landed on main",
		Path:    domain.Path{Model: domain.ModelOpus, Effort: domain.EffortHigh},
		Result:  application.SessionResult{Fuel: application.Fuel{Input: tokens}},
	}.String()

	dir, err := c.workspace()
	if err != nil {
		return err
	}
	return vault.New(dir).AppendToLedger(context.Background(), statusSeat, line)
}

func (c *statusContext) mwStatusReadsTheHost() error {
	dir, err := c.workspace()
	if err != nil {
		return err
	}
	hours, err := config.HostSilentHours()
	if err != nil {
		return fmt.Errorf("reading how long a host may be silent: %w", err)
	}
	c.askedBefore = len(c.tracker.Asked())
	if err := c.rememberElsewhere(); err != nil {
		return err
	}

	c.report, c.err = application.Status{
		Tracker:     c.tracker,
		Notes:       c.tracker,
		Vault:       vault.New(dir),
		Host:        statusHost,
		Seat:        statusSeat,
		HostSilence: time.Duration(hours) * time.Hour,
		Now:         func() time.Time { return c.now },
	}.Run(context.Background())
	return nil
}

// rememberElsewhere writes down what every story pathed away from this host
// looks like, and what note its host left, just before mw status runs.
func (c *statusContext) rememberElsewhere() error {
	ctx := context.Background()
	elsewhere, err := c.tracker.WorkElsewhere(ctx, statusHost)
	if err != nil {
		return fmt.Errorf("reading what the other hosts hold: %w", err)
	}
	for _, d := range elsewhere {
		host := d.Merged().Host
		said, err := c.tracker.Note(ctx, application.LastSyncKey(host))
		if err != nil {
			return fmt.Errorf("reading the last sync note of %s: %w", host, err)
		}
		c.elsewhereWas[d.Story.ID] = fmt.Sprintf("%s|%s|%s|%s", host, d.Status, d.Assignee, said)
	}
	return nil
}

func (c *statusContext) readingStatusSucceeds() error {
	if c.err != nil {
		return fmt.Errorf("reading status failed: %w", c.err)
	}
	return nil
}

// storyIn is the detail the report holds for a story, wherever it is listed.
func (c *statusContext) storyIn(id string) (application.StoryDetail, bool) {
	for _, rs := range c.report.Running {
		if rs.Detail.Story.ID == id {
			return rs.Detail, true
		}
	}
	for _, d := range c.report.Ready {
		if d.Story.ID == id {
			return d, true
		}
	}
	for _, d := range c.report.Blocked {
		if d.Story.ID == id {
			return d, true
		}
	}
	return application.StoryDetail{}, false
}

func (c *statusContext) runningIn(id string) (application.RunningStory, bool) {
	for _, rs := range c.report.Running {
		if rs.Detail.Story.ID == id {
			return rs, true
		}
	}
	return application.RunningStory{}, false
}

func (c *statusContext) theReportShowsRunningWithSession(id, session string) error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	rs, ok := c.runningIn(id)
	if !ok {
		return fmt.Errorf("%s is not listed as running:\n%s", id, c.report.String())
	}
	if rs.Session != session {
		return fmt.Errorf("expected %s to run under session %q, got %q", id, session, rs.Session)
	}
	if !strings.Contains(c.report.String(), session) {
		return fmt.Errorf("expected the printed report to name the session %q, got:\n%s", session, c.report.String())
	}
	return nil
}

func (c *statusContext) theReportListsAsReady(id string) error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	for _, d := range c.report.Ready {
		if d.Story.ID == id {
			return nil
		}
	}
	return fmt.Errorf("%s is not listed as ready:\n%s", id, c.report.String())
}

func (c *statusContext) theReportListsAsBlocked(id string) error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	for _, d := range c.report.Blocked {
		if d.Story.ID == id {
			return nil
		}
	}
	return fmt.Errorf("%s is not listed as blocked:\n%s", id, c.report.String())
}

func (c *statusContext) theReportShowsOnTheRig(id, rig string) error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	detail, ok := c.storyIn(id)
	if !ok {
		return fmt.Errorf("%s is not in the report:\n%s", id, c.report.String())
	}
	if got := detail.Merged().Rig; got != rig {
		return fmt.Errorf("expected %s on the rig %q, got %q", id, rig, got)
	}
	if !strings.Contains(c.report.String(), rig) {
		return fmt.Errorf("expected the printed report to name the rig %q, got:\n%s", rig, c.report.String())
	}
	return nil
}

func (c *statusContext) theReportSaysCloseOutBlockedByFormula(id string) error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	rs, ok := c.runningIn(id)
	if !ok {
		return fmt.Errorf("%s is not listed as running:\n%s", id, c.report.String())
	}
	if !rs.FormulaOpen() {
		return fmt.Errorf("expected %s to show an open formula step, got %+v", id, rs)
	}
	if !strings.Contains(c.report.String(), "formula") {
		return fmt.Errorf("expected the printed report to say the formula is open, got:\n%s", c.report.String())
	}
	return nil
}

func (c *statusContext) theReportShowsRunState(id, run string) error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	rs, ok := c.runningIn(id)
	if !ok {
		return fmt.Errorf("%s is not listed as running:\n%s", id, c.report.String())
	}
	if rs.Run != run {
		return fmt.Errorf("expected %s to be recorded run=%s, got %q", id, run, rs.Run)
	}
	if !rs.Stopped() {
		return fmt.Errorf("expected %s to be shown as not running, got %+v", id, rs)
	}
	if !strings.Contains(c.report.String(), run) {
		return fmt.Errorf("expected the printed report to say run=%s, got:\n%s", run, c.report.String())
	}
	return nil
}

func (c *statusContext) theReportSaysTodaysFuelIs(want string) error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	got := fmt.Sprintf("%s tokens", application.Thousands(c.report.FuelToday))
	if got != want {
		return fmt.Errorf("expected today's fuel to be %q, got %q", want, got)
	}
	if !strings.Contains(c.report.String(), got) {
		return fmt.Errorf("expected the printed report to say %q, got:\n%s", got, c.report.String())
	}
	return nil
}

func (c *statusContext) everyLineIsAtMost60ColumnsWide() error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	for _, line := range strings.Split(c.report.String(), "\n") {
		if n := utf8.RuneCountInString(line); n > application.Width {
			return fmt.Errorf("expected every line at most %d columns, got %d in %q", application.Width, n, line)
		}
	}
	return nil
}

// otherHost is the report's block for one host, if it has one.
func (c *statusContext) otherHost(host string) (application.HostWork, bool) {
	for _, w := range c.report.Others {
		if w.Host == host {
			return w, true
		}
	}
	return application.HostWork{}, false
}

// listedUnder reports whether the report's block for a host holds a story.
func listedUnder(w application.HostWork, id string) bool {
	for _, d := range w.Stories {
		if d.Story.ID == id {
			return true
		}
	}
	return false
}

func (c *statusContext) theReportListsUnderTheOtherHost(id, host string) error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	w, ok := c.otherHost(host)
	if !ok {
		return fmt.Errorf("the report holds nothing for the host %s:\n%s", host, c.report.String())
	}
	if !listedUnder(w, id) {
		return fmt.Errorf("%s is not listed under %s:\n%s", id, host, c.report.String())
	}
	if printed := c.report.String(); !strings.Contains(printed, host) || !strings.Contains(printed, id) {
		return fmt.Errorf("expected the printed report to name %s under %s, got:\n%s", id, host, printed)
	}
	return nil
}

func (c *statusContext) theReportListsAsStrandedOn(id, host string) error {
	if err := c.theReportListsUnderTheOtherHost(id, host); err != nil {
		return err
	}
	w, _ := c.otherHost(host)
	if !w.Asleep {
		return fmt.Errorf("expected %s to be called asleep, got %+v", host, w)
	}
	if !strings.Contains(c.report.String(), "stranded") {
		return fmt.Errorf("expected the printed report to call the work stranded, got:\n%s", c.report.String())
	}
	return nil
}

func (c *statusContext) theReportDoesNotCallTheHostAsleep(host string) error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	w, ok := c.otherHost(host)
	if !ok {
		return fmt.Errorf("the report holds nothing for the host %s:\n%s", host, c.report.String())
	}
	if w.Asleep {
		return fmt.Errorf("expected %s to be taken as awake, got %+v", host, w)
	}
	if strings.Contains(c.report.String(), "stranded") {
		return fmt.Errorf("expected nothing to be called stranded, got:\n%s", c.report.String())
	}
	return nil
}

func (c *statusContext) theReportShowsTheLastSyncTimeOf(host string) error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	w, ok := c.otherHost(host)
	if !ok {
		return fmt.Errorf("the report holds nothing for the host %s:\n%s", host, c.report.String())
	}
	if w.LastSync.IsZero() {
		return fmt.Errorf("expected the report to know when %s last synced, got %+v", host, w)
	}
	when := w.LastSync.UTC().Format(application.LastSyncFormat)
	if !strings.Contains(c.report.String(), when) {
		return fmt.Errorf("expected the printed report to say %s, got:\n%s", when, c.report.String())
	}
	return nil
}

func (c *statusContext) theReportSaysTheHostHasNeverSynced(host string) error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	w, ok := c.otherHost(host)
	if !ok {
		return fmt.Errorf("the report holds nothing for the host %s:\n%s", host, c.report.String())
	}
	if !w.NeverSynced() {
		return fmt.Errorf("expected %s to have never synced, got %+v", host, w)
	}
	if !strings.Contains(c.report.String(), "never synced") {
		return fmt.Errorf("expected the printed report to say never synced, got:\n%s", c.report.String())
	}
	return nil
}

// theReportSaysHowToRePathFrom checks the hint a person acts on: the line that
// re-paths a story stranded on a sleeping host onto the host reading the
// report. mw status only ever prints it.
func (c *statusContext) theReportSaysHowToRePathFrom(host string) error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	if _, ok := c.otherHost(host); !ok {
		return fmt.Errorf("the report holds nothing for the host %s:\n%s", host, c.report.String())
	}
	hint := application.RepathHint + statusHost
	if !strings.Contains(c.report.String(), hint) {
		return fmt.Errorf("expected the printed report to say %q, got:\n%s", hint, c.report.String())
	}
	return nil
}

// nothingElsewhereWasTouched says the section changed nothing it read: no
// story was re-pathed, claimed or released, and no host's last sync note was
// written — the whole point of a read-only report of somebody else's work.
func (c *statusContext) nothingElsewhereWasTouched() error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	if len(c.elsewhereWas) == 0 {
		return fmt.Errorf("no story was pathed to another host, so this scenario proves nothing")
	}
	ctx := context.Background()
	for id, was := range c.elsewhereWas {
		detail, err := c.tracker.ShowStory(ctx, id)
		if err != nil {
			return fmt.Errorf("reading %s back: %w", id, err)
		}
		host := detail.Merged().Host
		said, err := c.tracker.Note(ctx, application.LastSyncKey(host))
		if err != nil {
			return fmt.Errorf("reading the last sync note of %s: %w", host, err)
		}
		now := fmt.Sprintf("%s|%s|%s|%s", host, detail.Status, detail.Assignee, said)
		if now != was {
			return fmt.Errorf("expected %s to be left exactly as it was (%s), got %s", id, was, now)
		}
	}
	if syncs := c.tracker.Syncs(); syncs != 0 {
		return fmt.Errorf("expected mw status to synchronise nothing, got %d cycles", syncs)
	}
	return nil
}

func (c *statusContext) nothingWasWritten() error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	for _, call := range c.tracker.Asked()[c.askedBefore:] {
		if call != "RunningStories" && call != "ReadyForHost" {
			return fmt.Errorf("expected mw status to only read the tracker, but it called %s", call)
		}
	}
	if names := c.runner.Names(); len(names) != 0 {
		return fmt.Errorf("expected nothing started through the runner, got %v", names)
	}
	return nil
}
