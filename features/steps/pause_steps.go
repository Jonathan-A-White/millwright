package steps

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// pauseContext holds everything one mw pause / mw resume scenario runs
// against: a fleet of hosts each with a fake tmux of its own, one fake tracker
// (its notes are keyed by host), a fake git, and in-memory stand-ins for the
// cloud, the config, the vault's book, the timer and mw retry.
type pauseContext struct {
	now    time.Time
	home   string
	hosts  []string
	box    string
	caps   map[string]string
	rigDir string

	tracker *apptest.FakeTracker
	runners map[string]*apptest.FakeRunner
	git     *apptest.FakePauseGit
	fleet   *apptest.FakePauseFleet
	cloud   *apptest.FakePauseCloud
	config  *apptest.FakePauseConfig
	book    *apptest.FakePauseBook
	timer   *apptest.FakePauseTimer
	retry   *apptest.FakePauseRetry
	mail    *apptest.FakeMailbox
	burn    apptest.FakeBurn
	rate    float64

	// builders are the Builders that answer a PAUSE line, by story: the host
	// they run on, and whether they have answered.
	builders map[string]*pauseBuilder
	dirs     map[string]string
	asked    map[string]bool
	out      bytes.Buffer

	before pauseCounts
	failed error

	// nightJobs are the follower's night jobs, by name, once a scenario sets them up.
	nightJobs map[string]application.SpringJob
}

type pauseBuilder struct {
	host     string
	answered bool
}

// pauseCounts are the writes a run is measured against.
type pauseCounts struct{ book, mails, config, pushes int }

// InitializePauseScenario registers the steps of features/pause.feature.
func InitializePauseScenario(ctx *godog.ScenarioContext) {
	c := &pauseContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = pauseContext{}
		return ctx, nil
	})

	ctx.Given(`^a factory of the hosts "([^"]*)" and "([^"]*)" and the cloud box "([^"]*)", at 12:00 UTC on 22 October 2026$`, c.aFactory)
	ctx.Given(`^the host "([^"]*)" has the session cap (\d+)$`, c.theHostHasTheCap)
	ctx.Given(`^cloud\.max_boxes is (\d+)$`, c.maxBoxesIs)
	ctx.Given(`^the Builder of "([^"]*)" is running on "([^"]*)" with work committed, and answers the PAUSE line$`, c.anObedientBuilder)
	ctx.Given(`^the Builder of "([^"]*)" is running on "([^"]*)" with uncommitted work, and does not answer the PAUSE line$`, c.aDeafBuilder)
	ctx.Given(`^the story "([^"]*)" is claimed on "([^"]*)" but its session is gone, with work committed and not pushed$`, c.aGoneStory)
	ctx.Given(`^the story "([^"]*)" is claimed on "([^"]*)" but its session is gone, with no worktree$`, c.aGoneStoryWithNoWorktree)
	ctx.Given(`^the host "([^"]*)" cannot be reached$`, c.cannotBeReached)
	ctx.When(`^the host "([^"]*)" can be reached again$`, c.canBeReached)
	ctx.Given(`^the Governor ran mw pause with the reason "([^"]*)"$`, func(reason string) error { return c.pause(reason, "", "", false) })
	ctx.Given(`^the Governor ran mw pause with the reason "([^"]*)" until "([^"]*)"$`, func(reason, until string) error { return c.pause(reason, until, "", false) })
	ctx.Given(`^the Governor ran mw resume$`, c.resume)
	ctx.Given(`^the Governor reported (\d+) percent at "([^"]*)" with the week resetting at "([^"]*)"$`, c.reported)
	ctx.Given(`^the ledgers show (\d+) tokens on each of (\d{4}-\d\d-\d\d), (\d{4}-\d\d-\d\d) and (\d{4}-\d\d-\d\d)$`, c.ledgers)
	ctx.Given(`^the burn at full throttle is given as ([0-9.]+) percent an hour$`, c.rateGiven)

	ctx.When(`^the Governor runs mw pause with the reason "([^"]*)"$`, func(reason string) error { return c.pause(reason, "", "", false) })
	ctx.When(`^the Governor runs mw pause with the reason "([^"]*)" until "([^"]*)"$`, func(reason, until string) error { return c.pause(reason, until, "", false) })
	ctx.When(`^the Governor runs mw pause with the reason "([^"]*)" and the bead "([^"]*)"$`, func(reason, bead string) error { return c.pause(reason, "", bead, false) })
	ctx.When(`^the Governor runs mw pause as a dry run$`, func() error { return c.pause("so we're at 97% usage", "", "", true) })
	ctx.When(`^the Governor runs mw resume$`, c.resume)
	ctx.When(`^the Governor tries mw pause with the reason "([^"]*)"$`, func(reason string) error {
		c.failed = c.pause(reason, "", "", false)
		return nil
	})
	ctx.Then(`^mw refuses, saying "([^"]*)"$`, c.refuses)
	ctx.When(`^the Governor reports a usage of ([0-9.]+) percent with the week resetting at "([^"]*)"$`, c.reportsUsage)

	ctx.Then(`^the hold note of "([^"]*)" is until "([^"]*)" and says "([^"]*)"$`, c.holdNoteIs)
	ctx.Then(`^the hold note of "([^"]*)" is clear$`, c.holdNoteIsClear)
	ctx.Then(`^the paused set records the cap (\d+) for "([^"]*)", (\d+) for "([^"]*)" and (\d+) for "([^"]*)"$`, c.setRecordsCaps)
	ctx.Then(`^the output says "([^"]*)"$`, c.outputSays)
	ctx.Then(`^the output does not say "([^"]*)"$`, c.outputDoesNotSay)
	ctx.Then(`^the session of "([^"]*)" on "([^"]*)" was told "([^"]*)"$`, c.sessionWasTold)
	ctx.Then(`^the session of "([^"]*)" on "([^"]*)" was told nothing$`, c.sessionWasToldNothing)
	ctx.Then(`^the session of "([^"]*)" on "([^"]*)" is still running$`, c.sessionIsRunning)
	ctx.Then(`^the session of "([^"]*)" on "([^"]*)" was ended$`, c.sessionWasEnded)
	ctx.Then(`^the branch of "([^"]*)" is pushed$`, c.branchIsPushed)
	ctx.Then(`^mw did not push the branch of "([^"]*)" itself$`, c.mwDidNotPush)
	ctx.Then(`^mw committed and pushed the branch of "([^"]*)" itself$`, c.mwPushed)
	ctx.Then(`^the story "([^"]*)" has a PAUSED comment written by "([^"]*)"$`, c.pausedCommentBy)
	ctx.Then(`^the paused set lists "([^"]*)" on "([^"]*)" with its session not alive$`, c.listedNotAlive)
	ctx.Then(`^the box "([^"]*)" was brought down$`, c.boxDown)
	ctx.Then(`^the box "([^"]*)" was not brought down$`, c.boxNotDown)
	ctx.Then(`^cloud\.max_boxes is now (\d+)$`, c.maxBoxesNow)
	ctx.Then(`^the vault holds plans/pause-2026-10-22/paused\.json with the story "([^"]*)" on "([^"]*)", branch "([^"]*)", pushed, with a PAUSED comment$`, c.vaultHolds)
	ctx.Then(`^the bead "([^"]*)" carries the paused set$`, c.beadCarries)
	ctx.Then(`^a mail went to the Mayor titled "([^"]*)"$`, c.mailWent)
	ctx.Then(`^nothing more was written$`, c.nothingMore)
	ctx.Then(`^a resume timer "([^"]*)" is scheduled for "([^"]*)"$`, c.timerScheduled)
	ctx.Then(`^no resume timer is scheduled$`, c.noTimer)
	ctx.Then(`^the resume timer "([^"]*)" is cancelled$`, c.timerCancelled)
	ctx.Then(`^mw retry was run for "([^"]*)"$`, c.retried)
	ctx.Then(`^mw retry was not run for "([^"]*)"$`, c.notRetried)
	ctx.Then(`^the paused set under plans/pause-2026-10-22 says it was resumed$`, c.setResumed)
	ctx.Given(`^the follower's sleep pass and To do rank are both due on "([^"]*)"$`, c.nightPassesDue)
	ctx.Then(`^the paused set records the night passes "([^"]*)" and "([^"]*)" as off$`, c.setRecordsNightPasses)
	ctx.Then(`^the sleep pass is (not )?due on "([^"]*)"$`, func(not, host string) error { return c.passDue(application.SleepJobName, not == "", host) })
	ctx.Then(`^the To do rank is (not )?due on "([^"]*)"$`, func(not, host string) error { return c.passDue(application.TodoRankJobName, not == "", host) })
	ctx.Then(`^with ([0-9.]+) hours left the reserve is ([0-9.]+) percent$`, c.reserveIs)
}

func (c *pauseContext) clock() time.Time { return c.now }

// sleep is the wait between looks: the clock moves, and each Builder that
// answers a PAUSE line does so.
func (c *pauseContext) sleep(ctx context.Context, d time.Duration) error {
	c.now = c.now.Add(d)
	for id, b := range c.builders {
		if b.answered {
			continue
		}
		told := false
		for _, line := range c.runners[b.host].Input(application.SessionName(id)) {
			if strings.HasPrefix(line, "PAUSE:") {
				told = true
			}
		}
		if !told {
			continue
		}
		b.answered = true
		c.git.BuilderPushes(c.dirs[id])
		if err := c.tracker.CommentOnStory(ctx, id, "PAUSED: pushed mw/"+id); err != nil {
			return err
		}
	}
	return nil
}

func (c *pauseContext) aFactory(home, second, box string) error {
	c.now = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	c.home, c.hosts, c.box = home, []string{home, second, box}, box
	c.caps = map[string]string{}
	c.rigDir = "/work/rig"
	c.tracker = apptest.NewFakeTracker()
	c.runners = map[string]*apptest.FakeRunner{}
	c.git = apptest.NewFakePauseGit()
	c.fleet = apptest.NewFakePauseFleet()
	c.cloud = apptest.NewFakePauseCloud(box)
	c.config = &apptest.FakePauseConfig{}
	c.book = apptest.NewFakePauseBook()
	c.timer = apptest.NewFakePauseTimer()
	c.retry = &apptest.FakePauseRetry{}
	c.mail = apptest.NewFakeMailbox()
	c.burn = apptest.FakeBurn{}
	c.builders, c.dirs, c.asked = map[string]*pauseBuilder{}, map[string]string{}, map[string]bool{}
	for _, h := range c.hosts {
		c.runners[h] = apptest.NewFakeRunner()
	}
	c.tracker.AddEpic("mw-epic", domain.Path{Rig: "rig", Branch: "main", Harness: "claude", Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: home})
	return nil
}

// assemble builds the fleet from the caps as they stand, once the scenario
// has said what the hosts hold.
func (c *pauseContext) assemble() {
	c.fleet = apptest.NewFakePauseFleet()
	for _, h := range c.hosts {
		c.fleet.Add(application.FleetHostPause{
			Host: h, Notes: c.tracker, Stories: c.tracker, Runner: c.runners[h], Git: c.git,
			Rigs: map[string]string{"rig": c.rigDir}, Cap: c.caps[h], Now: c.clock, Sleep: c.sleep,
		})
	}
}

func (c *pauseContext) theHostHasTheCap(host, cap string) error {
	c.caps[host] = cap
	return nil
}

func (c *pauseContext) maxBoxesIs(n string) error {
	c.config.Value, c.config.Set = n, true
	return nil
}

func (c *pauseContext) addStory(id, host string) error {
	c.tracker.AddStory("mw-epic", domain.Story{ID: id, Title: id, Overrides: domain.Path{Host: host}})
	c.dirs[id] = application.WorktreeDir(c.rigDir, id)
	return c.tracker.ClaimAs(id, "mw@"+host, c.now.Add(time.Hour))
}

func (c *pauseContext) startSession(id, host string) error {
	return c.runners[host].Start(context.Background(), application.SessionSpec{Name: application.SessionName(id), Command: []string{"claude"}})
}

func (c *pauseContext) anObedientBuilder(id, host string) error {
	if err := c.addStory(id, host); err != nil {
		return err
	}
	c.git.Worktree(c.dirs[id], "c0-"+id)
	c.git.Leave(c.dirs[id], "work.go")
	c.builders[id] = &pauseBuilder{host: host}
	return c.startSession(id, host)
}

func (c *pauseContext) aDeafBuilder(id, host string) error {
	if err := c.addStory(id, host); err != nil {
		return err
	}
	c.git.Worktree(c.dirs[id], "c0-"+id)
	c.git.Leave(c.dirs[id], "work.go", "work_test.go")
	return c.startSession(id, host)
}

func (c *pauseContext) aGoneStory(id, host string) error {
	if err := c.addStory(id, host); err != nil {
		return err
	}
	c.git.Worktree(c.dirs[id], "c0-"+id)
	return nil
}

func (c *pauseContext) aGoneStoryWithNoWorktree(id, host string) error { return c.addStory(id, host) }

func (c *pauseContext) cannotBeReached(host string) error {
	c.assemble()
	c.fleet.Unreached[host] = true
	return nil
}

func (c *pauseContext) canBeReached(host string) error {
	c.fleet.Unreached[host] = false
	return nil
}

// fleetReady builds the fleet once, on the first command of a scenario, so
// that a host marked unreachable stays so.
func (c *pauseContext) fleetReady() {
	if hosts, _ := c.fleet.Hosts(context.Background()); len(hosts) == 0 {
		unreached := c.fleet.Unreached
		c.assemble()
		for h, v := range unreached {
			c.fleet.Unreached[h] = v
		}
	}
}

func (c *pauseContext) snapshot() {
	inbox, _ := c.mail.Inbox(context.Background(), application.MayorMailbox)
	c.before = pauseCounts{book: c.book.Writes, mails: len(inbox), config: len(c.config.Writes), pushes: len(c.git.Pushed())}
}

func (c *pauseContext) planner() application.ResumePlanner {
	return application.ResumePlanner{Readings: application.UsageReadings{Notes: c.tracker}, Burn: c.burn, Rate: c.rate, Now: c.clock}
}

func (c *pauseContext) pauseUseCase() application.Pause {
	c.fleetReady()
	return application.Pause{
		Fleet: c.fleet, Cloud: c.cloud, Config: c.config, Book: c.book, Timer: c.timer, Mail: c.mail,
		Tracker: c.tracker, Plan: c.planner().Plan, Host: c.home, Now: c.clock, Out: &c.out,
	}
}

func (c *pauseContext) pause(reason, until, bead string, dry bool) error {
	c.out.Reset()
	c.snapshot()
	req := application.PauseRequest{Reason: reason, Bead: bead, DryRun: dry, Wait: application.PauseWait}
	if until != "" {
		at, err := time.Parse(time.RFC3339, until)
		if err != nil {
			return err
		}
		req.Until = at
	}
	if bead != "" {
		c.tracker.AddStory("mw-epic", domain.Story{ID: bead, Title: "the record"})
	}
	_, err := c.pauseUseCase().Run(context.Background(), req)
	return err
}

func (c *pauseContext) resume() error {
	c.out.Reset()
	c.snapshot()
	c.fleetReady()
	return application.Resume{
		Fleet: c.fleet, Config: c.config, Book: c.book, Timer: c.timer, Retry: c.retry, Mail: c.mail,
		Tracker: c.tracker, Host: c.home, Now: c.clock, Out: &c.out,
	}.Run(context.Background(), false)
}

func (c *pauseContext) reported(percent int, at, resets string) error {
	when, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return err
	}
	reset, err := time.Parse(time.RFC3339, resets)
	if err != nil {
		return err
	}
	return application.UsageReadings{Notes: c.tracker}.Add(context.Background(), application.UsageReading{Percent: float64(percent), At: when, Resets: reset})
}

func (c *pauseContext) ledgers(tokens int, a, b, d string) error {
	for _, day := range []string{a, b, d} {
		c.burn[day] = tokens
	}
	return nil
}

func (c *pauseContext) rateGiven(rate string) error {
	v, err := strconv.ParseFloat(rate, 64)
	c.rate = v
	return err
}

func (c *pauseContext) reportsUsage(percent, resets string) error {
	c.out.Reset()
	c.snapshot()
	p, err := strconv.ParseFloat(percent, 64)
	if err != nil {
		return err
	}
	reset, err := time.Parse(time.RFC3339, resets)
	if err != nil {
		return err
	}
	pause := c.pauseUseCase()
	return application.UsageSet{
		Readings: application.UsageReadings{Notes: c.tracker}, Zone: time.UTC, Now: c.clock, Out: &c.out,
		Pause: func(ctx context.Context, reason string) error {
			_, err := pause.Run(ctx, application.PauseRequest{Reason: reason, Wait: application.PauseWait})
			return err
		},
	}.Run(context.Background(), p, reset)
}

func (c *pauseContext) holdNote(host string) (until, reason string, held bool) {
	value, _ := c.tracker.Note(context.Background(), application.HarnessHoldKey(host))
	fields := strings.Fields(value)
	if len(fields) < 2 {
		return "", "", false
	}
	return fields[0], fields[1], true
}

func (c *pauseContext) holdNoteIs(host, until, reason string) error {
	gotUntil, gotReason, held := c.holdNote(host)
	if !held {
		return fmt.Errorf("%s has no hold note", host)
	}
	if gotUntil != until || gotReason != reason {
		return fmt.Errorf("the hold note of %s reads %s %s, want %s %s", host, gotUntil, gotReason, until, reason)
	}
	return nil
}

func (c *pauseContext) holdNoteIsClear(host string) error {
	if until, reason, held := c.holdNote(host); held {
		return fmt.Errorf("the hold note of %s is still there: %s %s", host, until, reason)
	}
	return nil
}

func (c *pauseContext) currentSet() (application.PausedSet, error) {
	set, ok := c.book.Set(application.PauseDir(time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)))
	if !ok {
		return set, fmt.Errorf("the vault holds no paused set")
	}
	return set, nil
}

func (c *pauseContext) setRecordsCaps(a string, ah string, b string, bh string, d string, dh string) error {
	set, err := c.currentSet()
	if err != nil {
		return err
	}
	want := map[string]string{ah: a, bh: b, dh: d}
	for _, h := range set.Hosts {
		if want[h.Host] != h.Cap {
			return fmt.Errorf("the set records the cap %q for %s, want %q", h.Cap, h.Host, want[h.Host])
		}
		delete(want, h.Host)
	}
	if len(want) > 0 {
		return fmt.Errorf("the set records no cap for %v", want)
	}
	return nil
}

func (c *pauseContext) outputSays(want string) error {
	if !strings.Contains(c.out.String(), want) {
		return fmt.Errorf("the output does not say %q:\n%s", want, c.out.String())
	}
	return nil
}

func (c *pauseContext) outputDoesNotSay(not string) error {
	if strings.Contains(c.out.String(), not) {
		return fmt.Errorf("the output says %q:\n%s", not, c.out.String())
	}
	return nil
}

func (c *pauseContext) sessionWasTold(id, host, prefix string) error {
	for _, line := range c.runners[host].Input(application.SessionName(id)) {
		if strings.HasPrefix(line, prefix) {
			return nil
		}
	}
	return fmt.Errorf("the session of %s on %s was told %q, not a line beginning %q", id, host, c.runners[host].Input(application.SessionName(id)), prefix)
}

func (c *pauseContext) sessionWasToldNothing(id, host string) error {
	if told := c.runners[host].Input(application.SessionName(id)); len(told) > 0 {
		return fmt.Errorf("the session of %s on %s was told %q", id, host, told)
	}
	return nil
}

func (c *pauseContext) sessionIsRunning(id, host string) error {
	status, _ := c.runners[host].Status(context.Background(), application.SessionName(id))
	if !status.Running() {
		return fmt.Errorf("the session of %s on %s is %s", id, host, status.State)
	}
	return nil
}

func (c *pauseContext) sessionWasEnded(id, host string) error {
	for _, name := range c.runners[host].Closed() {
		if name == application.SessionName(id) {
			return nil
		}
	}
	return fmt.Errorf("the session of %s on %s was not closed (closed: %v)", id, host, c.runners[host].Closed())
}

func (c *pauseContext) branchIsPushed(id string) error {
	dir := c.dirs[id]
	head, herr := c.git.Head(context.Background(), dir)
	tip, terr := c.git.Tip(context.Background(), dir, "origin", "")
	if herr != nil || terr != nil || head != tip {
		return fmt.Errorf("the branch of %s is at %q, the remote at %q (%v %v)", id, head, tip, herr, terr)
	}
	return nil
}

func (c *pauseContext) pushedByMw(id string) bool {
	for _, dir := range c.git.Pushed() {
		if dir == c.dirs[id] {
			return true
		}
	}
	return false
}

func (c *pauseContext) mwDidNotPush(id string) error {
	if c.pushedByMw(id) {
		return fmt.Errorf("mw pushed the branch of %s itself", id)
	}
	return nil
}

func (c *pauseContext) mwPushed(id string) error {
	if !c.pushedByMw(id) {
		return fmt.Errorf("mw did not push the branch of %s", id)
	}
	if dirty, _ := c.git.Uncommitted(context.Background(), c.dirs[id]); len(dirty) > 0 {
		return fmt.Errorf("%s still has uncommitted %v", id, dirty)
	}
	return nil
}

func (c *pauseContext) setStory(id string) (application.PausedStory, error) {
	set, err := c.currentSet()
	if err != nil {
		return application.PausedStory{}, err
	}
	for _, ps := range set.Stories {
		if ps.Story == id {
			return ps, nil
		}
	}
	return application.PausedStory{}, fmt.Errorf("the paused set does not list %s", id)
}

func (c *pauseContext) pausedCommentBy(id, by string) error {
	ps, err := c.setStory(id)
	if err != nil {
		return err
	}
	if !ps.PausedComment || ps.CommentBy != by {
		return fmt.Errorf("the set has the PAUSED comment of %s as %v by %q, want by %q", id, ps.PausedComment, ps.CommentBy, by)
	}
	for _, text := range c.tracker.Comments(id) {
		if strings.HasPrefix(text, application.PausedComment) {
			return nil
		}
	}
	return fmt.Errorf("%s carries no comment that begins PAUSED: %v", id, c.tracker.Comments(id))
}

func (c *pauseContext) listedNotAlive(id, host string) error {
	ps, err := c.setStory(id)
	if err != nil {
		return err
	}
	if ps.Host != host || ps.Alive {
		return fmt.Errorf("%s is listed on %s alive=%v", id, ps.Host, ps.Alive)
	}
	return nil
}

func (c *pauseContext) boxDown(box string) error {
	for _, down := range c.cloud.BoxesDown() {
		if down == box {
			return nil
		}
	}
	return fmt.Errorf("the box %s was not brought down", box)
}

func (c *pauseContext) boxNotDown(box string) error {
	if err := c.boxDown(box); err == nil {
		return fmt.Errorf("the box %s was brought down", box)
	}
	return nil
}

func (c *pauseContext) maxBoxesNow(want string) error {
	if c.config.Value != want {
		return fmt.Errorf("cloud.max_boxes is %s, want %s", c.config.Value, want)
	}
	return nil
}

func (c *pauseContext) vaultHolds(id, host, branch string) error {
	ps, err := c.setStory(id)
	if err != nil {
		return err
	}
	if ps.Host != host || ps.Branch != branch || !ps.Pushed || !ps.PausedComment || ps.SHA == "" || ps.Window != application.SessionName(id) {
		return fmt.Errorf("the set holds %+v", ps)
	}
	return nil
}

func (c *pauseContext) beadCarries(bead string) error {
	for _, text := range c.tracker.Comments(bead) {
		if strings.Contains(text, "story mw-aaa.1 on laptop") {
			return nil
		}
	}
	return fmt.Errorf("%s carries %v", bead, c.tracker.Comments(bead))
}

func (c *pauseContext) mailWent(subject string) error {
	inbox, err := c.mail.Inbox(context.Background(), application.MayorMailbox)
	if err != nil {
		return err
	}
	for _, m := range inbox {
		if m.Subject == subject {
			return nil
		}
	}
	return fmt.Errorf("no mail titled %q went to the Mayor (%d did)", subject, len(inbox))
}

func (c *pauseContext) nothingMore() error {
	inbox, _ := c.mail.Inbox(context.Background(), application.MayorMailbox)
	now := pauseCounts{book: c.book.Writes, mails: len(inbox), config: len(c.config.Writes), pushes: len(c.git.Pushed())}
	if now != c.before {
		return fmt.Errorf("writes before %+v, after %+v", c.before, now)
	}
	return nil
}

func (c *pauseContext) timerScheduled(unit, at string) error {
	got, ok := c.timer.Scheduled[unit]
	if !ok {
		return fmt.Errorf("no timer %s is scheduled (%v)", unit, c.timer.Scheduled)
	}
	if got.UTC().Format(time.RFC3339) != at {
		return fmt.Errorf("the timer %s is for %s, want %s", unit, got.UTC().Format(time.RFC3339), at)
	}
	return nil
}

func (c *pauseContext) noTimer() error {
	if len(c.timer.Scheduled) > 0 {
		return fmt.Errorf("timers are scheduled: %v", c.timer.Scheduled)
	}
	return nil
}

func (c *pauseContext) timerCancelled(unit string) error {
	for _, got := range c.timer.Cancelled {
		if got == unit {
			return nil
		}
	}
	return fmt.Errorf("the timer %s was not cancelled (%v)", unit, c.timer.Cancelled)
}

func (c *pauseContext) wasRetried(id string) bool {
	for _, got := range c.retry.Retried {
		if got == id {
			return true
		}
	}
	return false
}

func (c *pauseContext) retried(id string) error {
	if !c.wasRetried(id) {
		return fmt.Errorf("mw retry was not run for %s (ran for %v)", id, c.retry.Retried)
	}
	return nil
}

func (c *pauseContext) notRetried(id string) error {
	if c.wasRetried(id) {
		return fmt.Errorf("mw retry was run for %s", id)
	}
	return nil
}

func (c *pauseContext) setResumed() error {
	set, err := c.currentSet()
	if err != nil {
		return err
	}
	if set.ResumedAt.IsZero() {
		return fmt.Errorf("the paused set does not say it was resumed")
	}
	return nil
}

func (c *pauseContext) reserveIs(hours, want string) error {
	h, err := strconv.ParseFloat(hours, 64)
	if err != nil {
		return err
	}
	got := fmt.Sprintf("%.2f", application.UsageReserve(time.Duration(h*float64(time.Hour))))
	if got != want {
		return fmt.Errorf("with %s hours left the reserve is %s, want %s", hours, got, want)
	}
	return nil
}

func (c *pauseContext) refuses(want string) error {
	if c.failed == nil {
		return fmt.Errorf("mw did not refuse")
	}
	if !strings.Contains(c.failed.Error(), want) {
		return fmt.Errorf("mw refused with %q, not saying %q", c.failed, want)
	}
	return nil
}

// nightPassesDue sets up the follower's two night jobs on the home, at the
// clock's hour, which is past both of their hours, asking the pause book
// whether the factory is paused.
func (c *pauseContext) nightPassesDue(host string) error {
	paused := func(ctx context.Context) (bool, error) {
		_, held, err := c.book.Current(ctx)
		return held, err
	}
	home := func(context.Context) (bool, error) { return true, nil }
	ok := func(context.Context) error { return nil }
	c.nightJobs = map[string]application.SpringJob{
		application.SleepJobName: (&application.SleepNightly{
			Hour: 6, Notes: c.tracker, Run: ok, Home: home, Now: c.clock, Paused: paused,
		}).Job(),
		application.TodoRankJobName: (&application.TodoNightly{
			Hour: 7, Rank: ok, Notes: c.tracker, Home: home, Now: c.clock, Paused: paused,
		}).Job(),
	}
	return nil
}

func (c *pauseContext) setRecordsNightPasses(a, b string) error {
	set, err := c.currentSet()
	if err != nil {
		return err
	}
	if len(set.NightPasses) != 2 || set.NightPasses[0] != a || set.NightPasses[1] != b {
		return fmt.Errorf("the set records the night passes %v off, want %s and %s", set.NightPasses, a, b)
	}
	return nil
}

func (c *pauseContext) passDue(name string, wantDue bool, host string) error {
	job, ok := c.nightJobs[name]
	if !ok {
		return fmt.Errorf("no %s job was set up on %s", name, host)
	}
	if _, due := job.Probe(context.Background()); due != wantDue {
		return fmt.Errorf("the %s job is due on %s: %v, want %v", name, host, due, wantDue)
	}
	return nil
}
