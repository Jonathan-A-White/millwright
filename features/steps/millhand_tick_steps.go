package steps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// The steps of features/millhand_tick.feature. A tick is a thin layer over the
// Millhand wake, so its scenarios are the seat up context's: the same vault on
// disk, the same faked terminal and the same window assertions. What they add
// is the faked mail, sync, sweep and log the tick reads through.

// tickEpic is the epic the stories of a tick scenario are filed under.
const tickEpic = "mw-tk"

// The outside places a tick scenario's watch asks of. Which of them answer is
// the scenario's to say; the [watch] table naming them is only there when a
// step wrote it.
var tickOutside = []string{"https://one.example", "https://two.example"}

// tickWorld is what a tick scenario reads through: a mailbox, a tracker and a
// runner for the sweep, a sync that can fail, a log in memory, and the world
// mw watch looks at.
type tickWorld struct {
	mailbox *apptest.FakeMailbox
	tracker *apptest.FakeTracker
	runner  *apptest.FakeRunner
	sync    *tickSync
	log     *apptest.FakeTickLog
	// watch is the world the tick's watch reaches out to, and watching the
	// [watch] table it was given: the zero value is no table at all.
	watch    *apptest.FakeWatch
	watching application.WatchSettings
	// epicFiled says the epic the stories hang from has been filed.
	epicFiled bool
	// whileWaiting is what happens in the wait between the tick's two checks of
	// a Millhand's pane.
	whileWaiting func()

	report application.MillhandTickReport
	err    error
	said   strings.Builder
}

// tickSync is a sync that says what it was told to, and counts how often it was
// asked. onRun is what happens while it runs.
type tickSync struct {
	err   error
	runs  int
	onRun func()
}

func (s *tickSync) Run(context.Context) (application.SyncReport, error) {
	s.runs++
	if s.onRun != nil {
		s.onRun()
	}
	return application.SyncReport{Host: seatUpHost}, s.err
}

// tick is the scenario's tick world, made the first time a step asks for it.
func (c *seatUpContext) tickWorld() *tickWorld {
	if c.tick == nil {
		c.tick = &tickWorld{
			mailbox: apptest.NewFakeMailbox(),
			tracker: apptest.NewFakeTracker(),
			runner:  apptest.NewFakeRunner(),
			sync:    &tickSync{},
			log:     &apptest.FakeTickLog{},
			watch:   apptest.NewFakeWatch(),
		}
	}
	return c.tick
}

// registerMillhandTickSteps adds the steps of features/millhand_tick.feature to
// the seat up scenario.
func registerMillhandTickSteps(ctx *godog.ScenarioContext, c *seatUpContext) {
	ctx.Given(`^unread tick mail for "([^"]*)" with the subject "([^"]*)"$`, c.unreadTickMail)
	ctx.Given(`^(\d+) unread tick messages for "([^"]*)" with the subjects "([^"]*)" to "([^"]*)"$`, c.unreadTickMessages)
	ctx.Given(`^the story "([^"]*)" titled "([^"]*)" is claimed here with no session behind it$`, c.aStuckStory)
	ctx.Given(`^(\d+) claimed stories with no session behind them, titled "([^"]*)" to "([^"]*)"$`, c.stuckStories)
	ctx.Given(`^the sync fails saying "([^"]*)"$`, c.theSyncFailsSaying)
	ctx.Given(`^the tick mail cannot be read$`, c.theMailCannotBeRead)
	ctx.Given(`^a Millhand window opens while the tick syncs$`, c.aMillhandOpensDuringTheSync)
	ctx.Given(`^the tick watches the host "([^"]*)" over ssh "([^"]*)", with the outside places "([^"]*)" and "([^"]*)"$`, c.theTickWatches)
	ctx.Given(`^the tick can reach the outside places$`, c.outsidePlacesAnswer(true))
	ctx.Given(`^the tick cannot reach either outside place$`, c.outsidePlacesAnswer(false))
	ctx.Given(`^ssh to the watched host fails$`, c.sshToTheWatchedHostFails)
	ctx.Given(`^the watched host's health line is (\d+) minutes old and ends "([^"]*)"$`, c.theWatchedHostsHealthLine)
	ctx.Given(`^the watched host first failed a check (\d+) minutes ago$`, c.theWatchedHostFirstFailed)
	ctx.Given(`^the tick's watch memory cannot be read$`, c.theWatchMemoryCannotBeRead)
	ctx.Given(`^the pane of the window "([^"]*)" (has text on its input line|is working)$`, c.thePaneOfTheWindow)
	ctx.Given(`^the pane of the window "([^"]*)" turns to text on its input line while the tick waits between its checks$`, c.thePaneTurnsWhileTheTickWaits)
	ctx.Given(`^the terminal cannot say what the pane of the window "([^"]*)" is doing$`, c.theTerminalCannotSayWhatThePaneIsDoing)

	ctx.When(`^mw millhand tick is run$`, func() error { return c.runTheTick(false) })
	ctx.When(`^mw millhand tick is run as a dry run$`, func() error { return c.runTheTick(true) })
	ctx.When(`^the Millhand's window is closed$`, c.theMillhandsWindowIsClosed)

	ctx.Then(`^mw millhand tick succeeds$`, c.theTickSucceeds)
	ctx.Then(`^mw millhand tick fails$`, c.theTickFails)
	ctx.Then(`^mw millhand tick prints one dated line saying "([^"]*)"$`, c.theTickPrintsALineSaying)
	ctx.Then(`^the tick did not sync$`, c.theTickSyncedTimes(0))
	ctx.Then(`^the tick synced once$`, c.theTickSyncedTimes(1))
	ctx.Then(`^the tick log holds that line$`, c.theLogHoldsThatLine)
	ctx.Then(`^the tick log holds (\d+) lines$`, c.theLogHoldsLines)
	ctx.Then(`^no tick mail was marked read$`, c.noTickMailWasMarkedRead)
	ctx.Then(`^the story "([^"]*)" is not recorded as stuck by the tick$`, c.theStoryIsNotRecordedStuck)
	ctx.Then(`^ssh to the watched host was not tried$`, c.sshWasNotTried)
	ctx.Then(`^nothing was asked of the watched host or the outside places$`, c.nothingWasAskedOfTheWorld)
	ctx.Then(`^the kickoff prompt of the window ends with "([^"]*)"$`, c.theKickoffEndsWith)
	ctx.Then(`^the window "([^"]*)" was closed$`, c.theWindowWasClosed)
	ctx.Then(`^the window "([^"]*)" was not closed$`, c.theWindowWasNotClosed)
	ctx.Then(`^the reaper log holds one line saying "([^"]*)"$`, c.theReaperLogHoldsOneLineSaying)
	ctx.Then(`^the reaper log holds no line$`, c.theReaperLogHoldsNoLine)
}

func (c *seatUpContext) unreadTickMail(mailbox, subject string) error {
	_, err := c.tickWorld().mailbox.Send(context.Background(), application.NewMessage{
		From: "mayor", To: mailbox, Subject: subject, Body: "A message a tick test left.",
	})
	return err
}

// letters is the run of subjects or titles from the first to the last: "Mail A"
// to "Mail G" is Mail A, Mail B and so on, count of them.
func letters(count int, first, last string) ([]string, error) {
	if first == "" {
		return nil, fmt.Errorf("%q to %q is not a run of names", first, last)
	}
	prefix := first[:len(first)-1]
	if !strings.HasPrefix(last, prefix) {
		return nil, fmt.Errorf("%q to %q is not a run of names", first, last)
	}
	start := first[len(first)-1]
	names := make([]string, count)
	for i := range names {
		names[i] = prefix + string(rune(int(start)+i))
	}
	if names[count-1] != last {
		return nil, fmt.Errorf("%d names from %q end at %q, not %q", count, first, names[count-1], last)
	}
	return names, nil
}

func (c *seatUpContext) unreadTickMessages(count, mailbox, first, last string) error {
	n, err := strconv.Atoi(count)
	if err != nil {
		return err
	}
	subjects, err := letters(n, first, last)
	if err != nil {
		return err
	}
	for _, subject := range subjects {
		if err := c.unreadTickMail(mailbox, subject); err != nil {
			return err
		}
	}
	return nil
}

// aStuckStory files a story on this host, claimed by it, with no session behind
// it: the one thing a sweep calls stuck straight away.
func (c *seatUpContext) aStuckStory(id, title string) error {
	world := c.tickWorld()
	if !world.epicFiled {
		world.epicFiled = true
		defaults := domain.Path{}
		for field, value := range map[string]string{
			"rig": "millwright", "branch": "main", "harness": "claude", "model": "opus",
			"effort": "high", "formula": "tdd-feature", "host": seatUpHost,
		} {
			if err := defaults.Set(field, value); err != nil {
				return err
			}
		}
		world.tracker.AddEpic(tickEpic, defaults)
	}
	world.tracker.AddStory(tickEpic, domain.Story{ID: id, Title: title})
	return world.tracker.ClaimStory(context.Background(), id)
}

func (c *seatUpContext) stuckStories(count, first, last string) error {
	n, err := strconv.Atoi(count)
	if err != nil {
		return err
	}
	titles, err := letters(n, first, last)
	if err != nil {
		return err
	}
	for i, title := range titles {
		if err := c.aStuckStory(fmt.Sprintf("%s.%d", tickEpic, i+1), title); err != nil {
			return err
		}
	}
	return nil
}

func (c *seatUpContext) theSyncFailsSaying(said string) error {
	// Gherkin does not unescape, so a newline is written as \n.
	c.tickWorld().sync.err = errors.New(strings.ReplaceAll(said, `\n`, "\n"))
	return nil
}

func (c *seatUpContext) theMailCannotBeRead() error {
	c.tickWorld().mailbox.Err = errors.New("the mail store is not answering")
	return nil
}

func (c *seatUpContext) aMillhandOpensDuringTheSync() error {
	c.tickWorld().sync.onRun = func() {
		c.windows.Holds("millhand-2026-09-19-05", time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC))
	}
	return nil
}

// theMillhandsWindowIsClosed closes every window the wake opened, as the reaper
// does once the Millhand has handed off.
func (c *seatUpContext) theMillhandsWindowIsClosed() error {
	for _, spec := range c.windows.Opened {
		if id, open := c.windows.IDOf(spec.Name); open {
			c.windows.Vanish(id)
		}
	}
	return nil
}

// runTheTick runs the use case as `mw millhand tick` does: the real vault, the
// real Claude Code harness and the models the real config reads, with only the
// terminal, the mail, the sync, the tracker and the log faked.
func (c *seatUpContext) runTheTick(dryRun bool) error {
	if err := c.isolateConfig(); err != nil {
		return err
	}
	routine, err := config.MillhandRoutineModel()
	if err != nil {
		return err
	}
	review, err := config.MillhandReviewModel()
	if err != nil {
		return err
	}
	world := c.tickWorld()
	world.said.Reset()
	now := func() time.Time { return c.today }
	world.report, world.err = application.MillhandTick{
		Millhand: application.Millhand{
			Seats:        c.seatFiles(),
			Windows:      c.windows,
			Harness:      claude.New(),
			Terminal:     c.windows,
			Armer:        c.armer,
			Host:         seatUpHost,
			RoutineModel: domain.Model(routine),
			ReviewModel:  domain.Model(review),
			Now:          now,
		},
		ReapLog: vault.New(c.dir),
		// No wait between the two checks of a pane, but for what a scenario
		// makes happen in it.
		Sleep: func(context.Context, time.Duration) error {
			if world.whileWaiting != nil {
				world.whileWaiting()
			}
			return nil
		},
		Sync: world.sync,
		Mail: world.mailbox,
		Sweep: application.Sweep{
			Tracker: world.tracker,
			Memory:  world.tracker,
			Runner:  world.runner,
			Host:    seatUpHost,
			Now:     now,
		},
		Watch: application.Watch{
			Probes: world.watch, Settings: world.watching, Notes: world.tracker, Now: now,
		},
		Log:    world.log,
		Host:   seatUpHost,
		DryRun: dryRun,
		Now:    now,
		Out:    &world.said,
	}.Run(context.Background())
	return nil
}

func (c *seatUpContext) theTickSucceeds() error {
	if err := c.tickWorld().err; err != nil {
		return fmt.Errorf("expected mw millhand tick to succeed, it failed: %w", err)
	}
	return nil
}

func (c *seatUpContext) theTickFails() error {
	if c.tickWorld().err == nil {
		return fmt.Errorf("expected mw millhand tick to fail, it said %q", c.tickWorld().said.String())
	}
	return nil
}

// theTickPrintsALineSaying says the tick printed exactly one line, that it
// begins with the date, and that it holds the words.
func (c *seatUpContext) theTickPrintsALineSaying(words string) error {
	said := c.tickWorld().said.String()
	if strings.Count(said, "\n") != 1 || !strings.HasSuffix(said, "\n") {
		return fmt.Errorf("expected exactly one line, got %q", said)
	}
	if !strings.HasPrefix(said, "2026-09-19T12:00:00Z ") {
		return fmt.Errorf("expected the line to begin with the date and time, got %q", said)
	}
	if !strings.Contains(said, words) {
		return fmt.Errorf("expected the line to say %q, got %q", words, said)
	}
	return nil
}

func (c *seatUpContext) theTickSyncedTimes(want int) func() error {
	return func() error {
		if got := c.tickWorld().sync.runs; got != want {
			return fmt.Errorf("expected the tick to sync %d times, it synced %d", want, got)
		}
		return nil
	}
}

func (c *seatUpContext) theLogHoldsThatLine() error {
	world := c.tickWorld()
	want := strings.TrimSuffix(world.said.String(), "\n")
	lines := world.log.Lines()
	if len(lines) != 1 || lines[0] != want {
		return fmt.Errorf("expected the log to hold %q, it holds %q", want, lines)
	}
	return nil
}

func (c *seatUpContext) theLogHoldsLines(count int) error {
	if lines := c.tickWorld().log.Lines(); len(lines) != count {
		return fmt.Errorf("expected the log to hold %d lines, it holds %d: %q", count, len(lines), lines)
	}
	return nil
}

// noTickMailWasMarkedRead says the mail is as unread as the Givens left it: the
// only writes the mailbox took are the sends that filled it.
func (c *seatUpContext) noTickMailWasMarkedRead() error {
	world := c.tickWorld()
	unread := 0
	for _, mailbox := range []string{"millhand@laptop", "millhand", "mayor", "millhand@vps", "builder@laptop"} {
		messages, err := world.mailbox.Inbox(context.Background(), mailbox)
		if err != nil {
			return err
		}
		unread += len(messages)
	}
	if writes := world.mailbox.Writes(); writes != unread {
		return fmt.Errorf("expected every message to stay unread, but %d were sent and %d are unread", writes, unread)
	}
	return nil
}

func (c *seatUpContext) theStoryIsNotRecordedStuck(id string) error {
	if got := c.tickWorld().tracker.State(id, application.RunState); got == application.RunStuck {
		return fmt.Errorf("expected %s not to be recorded stuck, but it was", id)
	}
	return nil
}

func (c *seatUpContext) theTickWatches(host, ssh, one, two string) error {
	c.tickWorld().watching = application.WatchSettings{SSH: ssh, Host: host, Outside: []string{one, two}}
	return nil
}

func (c *seatUpContext) outsidePlacesAnswer(answer bool) func() error {
	return func() error {
		for _, url := range tickOutside {
			c.tickWorld().watch.Answering[url] = answer
		}
		return nil
	}
}

func (c *seatUpContext) sshToTheWatchedHostFails() error {
	c.tickWorld().watch.SSHFails = true
	return nil
}

// theWatchedHostsHealthLine makes ssh print a line as mw-health.sh writes it,
// stamped that long before the tick's clock and ending in the verdict given.
func (c *seatUpContext) theWatchedHostsHealthLine(minutes int, verdict string) error {
	at := c.today.Add(-time.Duration(minutes) * time.Minute).UTC().Format(time.RFC3339)
	c.tickWorld().watch.Health = at + " load1=0.50 mem_avail_mb=1000 disk_pct=42 services=none " + verdict
	return nil
}

func (c *seatUpContext) theWatchedHostFirstFailed(minutes int) error {
	c.tickWorld().watch.SetMemory(application.WatchMemory{
		FirstFailure: c.today.Add(-time.Duration(minutes) * time.Minute),
	})
	return nil
}

func (c *seatUpContext) theWatchMemoryCannotBeRead() error {
	c.tickWorld().watch.LoadErr = errors.New("the state directory is unreadable")
	return nil
}

func (c *seatUpContext) sshWasNotTried() error {
	if reads := c.tickWorld().watch.Reads(); len(reads) != 0 {
		return fmt.Errorf("expected ssh not to be tried, it was asked to read the health line of %q", reads)
	}
	return nil
}

// nothingWasAskedOfTheWorld says mw watch was not run: it reached nowhere, read
// nothing, kept nothing and logged nothing.
func (c *seatUpContext) nothingWasAskedOfTheWorld() error {
	watch := c.tickWorld().watch
	if reached, reads, log := watch.Reached(), watch.Reads(), watch.Log(); len(reached)+len(reads)+len(log) != 0 {
		return fmt.Errorf("expected mw watch not to be run, but it reached %q, read %q and logged %q", reached, reads, log)
	}
	if watch.Saves() != 0 {
		return fmt.Errorf("expected mw watch to keep nothing, it saved its memory %d times", watch.Saves())
	}
	return nil
}

func (c *seatUpContext) theKickoffEndsWith(want string) error {
	told, err := c.kickoff()
	if err != nil {
		return err
	}
	if !strings.HasSuffix(told, want) {
		return fmt.Errorf("expected the kickoff to end with %q, got %q", want, told)
	}
	return nil
}

func (c *seatUpContext) thePaneOfTheWindow(name, state string) error {
	id, open := c.windows.IDOf(name)
	if !open {
		return fmt.Errorf("the window %s is not open", name)
	}
	pane := application.PaneInput
	if state == "is working" {
		pane = application.PaneWorking
	}
	return c.windows.Pane(id, pane)
}

func (c *seatUpContext) thePaneTurnsWhileTheTickWaits(name string) error {
	id, open := c.windows.IDOf(name)
	if !open {
		return fmt.Errorf("the window %s is not open", name)
	}
	c.tickWorld().whileWaiting = func() { _ = c.windows.Pane(id, application.PaneInput) }
	return nil
}

func (c *seatUpContext) theTerminalCannotSayWhatThePaneIsDoing(name string) error {
	if _, open := c.windows.IDOf(name); !open {
		return fmt.Errorf("the window %s is not open", name)
	}
	c.windows.PaneErr = errors.New("the terminal is not answering")
	return nil
}

// theWindowWasClosed says the tick closed the window: it is gone, and the
// terminal was asked to close it.
func (c *seatUpContext) theWindowWasClosed(name string) error {
	if _, open := c.windows.IDOf(name); open {
		return fmt.Errorf("expected the window %s to be closed, it is open", name)
	}
	if len(c.windows.ClosedIDs()) == 0 {
		return fmt.Errorf("expected the tick to close the window %s, but the terminal was never asked to close one", name)
	}
	return nil
}

func (c *seatUpContext) theWindowWasNotClosed(name string) error {
	if _, open := c.windows.IDOf(name); !open {
		return fmt.Errorf("expected the window %s to stay open, it is gone", name)
	}
	if closed := c.windows.ClosedIDs(); len(closed) != 0 {
		return fmt.Errorf("expected the tick to close nothing, it closed %q", closed)
	}
	return nil
}

// reaperLog is the lines of the Millhand's reaper log, none when it was never
// written.
func (c *seatUpContext) reaperLog() ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(c.dir, application.ReapLogFileName(application.MillhandSeat)))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n"), nil
}

func (c *seatUpContext) theReaperLogHoldsOneLineSaying(words string) error {
	lines, err := c.reaperLog()
	if err != nil {
		return err
	}
	if len(lines) != 1 {
		return fmt.Errorf("expected the reaper log to hold one line, it holds %q", lines)
	}
	if !strings.HasPrefix(lines[0], "2026-09-19T12:00:00Z ") || !strings.Contains(lines[0], words) {
		return fmt.Errorf("expected a dated line saying %q, got %q", words, lines[0])
	}
	return nil
}

func (c *seatUpContext) theReaperLogHoldsNoLine() error {
	lines, err := c.reaperLog()
	if err != nil {
		return err
	}
	if len(lines) != 0 {
		return fmt.Errorf("expected the reaper log to hold nothing, it holds %q", lines)
	}
	return nil
}
