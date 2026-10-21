package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/domain/events"

	"github.com/cucumber/godog"
)

// The steps of features/timing_nightly.feature. They run the follower's
// springer over a fake event log and a clock the scenario sets, with the
// nightly timing run as its one job; the rig, its origin, the host's load and
// the tests' output are fakes, so nothing is run.

type timingNightlyContext struct {
	host, home string
	now        time.Time
	rigs       map[string]string
	globs      map[string][]string
	epics      map[string]string
	rules      *apptest.FakeEpicRules
	rig        *apptest.FakeTimingRig
	heads      *apptest.FakeRigHeads
	records    *apptest.FakeTimingRecords
	runner     *apptest.FakeTimingRunner
	load       *apptest.FakeHostLoad
	tracker    *apptest.FakeTracker
	log        *apptest.FakeEventLog
	uptime     *apptest.FakeUptime
	failing    map[string]string // rig -> the file its run fails in
	spring     *application.EventSpring
	ctx        context.Context
	cancel     context.CancelFunc
	lastBug    string
}

// InitializeTimingNightlyScenario registers the steps of features/timing_nightly.feature.
func InitializeTimingNightlyScenario(ctx *godog.ScenarioContext) {
	c := &timingNightlyContext{}

	ctx.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		*c = timingNightlyContext{
			rigs: map[string]string{}, globs: map[string][]string{}, epics: map[string]string{}, failing: map[string]string{},
			rules: apptest.NewFakeEpicRules(), heads: apptest.NewFakeRigHeads(), records: &apptest.FakeTimingRecords{},
			rig:  &apptest.FakeTimingRig{Tests: map[string][]application.TaggedTest{}, Changes: map[string][]string{}},
			load: &apptest.FakeHostLoad{Reading: application.LoadReading{Load: 0.2, Cores: 4}}, tracker: apptest.NewFakeTracker(),
			log: &apptest.FakeEventLog{}, uptime: &apptest.FakeUptime{},
		}
		c.runner = &apptest.FakeTimingRunner{Respond: c.respond}
		c.ctx, c.cancel = context.WithCancel(context.Background())
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		c.cancel()
		if c.spring != nil {
			c.spring.Wait()
		}
		return ctx, nil
	})

	ctx.Given(`^the timing follower is running on "([^"]*)" and the home is "([^"]*)"$`, func(host, home string) error {
		c.host, c.home = host, home
		return nil
	})
	ctx.Given(`^the rig "([^"]*)" says timing_tests = "([^"]*)" and suite_epic = "([^"]*)"$`, c.rigSays)
	ctx.Given(`^the timing tests of "([^"]*)" are "([^"]*)" covering "([^"]*)"$`, func(rig, file, covers string) error {
		c.rig.Tests[c.rigs[rig]] = append(c.rig.Tests[c.rigs[rig]], application.TaggedTest{File: file, Covers: []string{covers}})
		return nil
	})
	ctx.Given(`^main of "([^"]*)" is at "([^"]*)"$`, func(rig, sha string) error {
		c.heads.SetHead(c.rigs[rig], sha)
		return nil
	})
	ctx.Given(`^the last passing timing night of "([^"]*)" was at "([^"]*)" on "([^"]*)"$`, func(rig, sha, day string) error {
		kept, _ := json.Marshal(application.TimingResult{Rig: rig, Sha: sha, Passed: true})
		_, err := c.records.PutTimingFile(c.ctx, rig, day+".json", string(kept))
		return err
	})
	ctx.Given(`^the uptime reads "([^"]*)"$`, func(line string) error {
		c.uptime.Lines = []string{line}
		return nil
	})
	ctx.Given(`^"([^"]*)" changed in "([^"]*)" between "([^"]*)" and "([^"]*)"$`, func(file, rig, from, to string) error {
		c.rig.Changes[from+".."+to] = append(c.rig.Changes[from+".."+to], file)
		return nil
	})
	ctx.Given(`^the timing run of "([^"]*)" fails in "([^"]*)"$`, func(rig, file string) error {
		c.failing[rig] = file
		return nil
	})
	ctx.Given(`^the timing host is at load ([\d.]+) of (\d+) cores$`, func(load string, cores int) error {
		n, err := strconv.ParseFloat(load, 64)
		c.load.Reading = application.LoadReading{Load: n, Cores: cores}
		return err
	})
	ctx.Given(`^the timing clock reads "([^"]*)" UTC$`, c.setClock)
	ctx.When(`^the timing clock reads "([^"]*)" UTC$`, c.setClock)
	ctx.Given(`^the timing follower has taken its bearings$`, c.bearings)
	ctx.When(`^the timing follower runs a cycle$`, c.cycle)

	ctx.Then(`^the timing command of "([^"]*)" was run with MW_TIMING=only (\d+) times?$`, c.wasRun)
	ctx.Then(`^the suite epic "([^"]*)" holds (\d+) bugs?$`, c.epicHolds)
	ctx.Then(`^the new story for "([^"]*)" is held and labelled bug and timing$`, c.bugIsHeld)
	ctx.Then(`^that bug names (.+)$`, c.bugNames)
	ctx.Then(`^the timing result of "([^"]*)" for "([^"]*)" is kept as (passed|failed) at "([^"]*)"$`, c.resultKept)
	ctx.Then(`^nothing is kept for "([^"]*)" on "([^"]*)"$`, func(rig, day string) error {
		if kept, ok := c.records.File("runs/timing/" + rig + "/" + day + ".json"); ok {
			return fmt.Errorf("a result was kept: %s", kept)
		}
		return nil
	})
	ctx.Then(`^the timing log tells the job "([^"]*)" as ([a-z, ]+)$`, c.logTells)
	ctx.Then(`^the log has no event of the job "([^"]*)"$`, func(actor string) error {
		for _, e := range c.log.All() {
			if e.Kind == events.KindJob && e.Actor == actor {
				return fmt.Errorf("the log has %+v", e)
			}
		}
		return nil
	})
	ctx.Then(`^the last event of the job "([^"]*)" is done and says "([^"]*)"$`, c.lastEventSays)
}

// respond is what the tests print: green, or a failure naming the file a
// scenario said its run fails in.
func (c *timingNightlyContext) respond(dir, _ string, _ map[string]string) (application.Checked, error) {
	rig := strings.TrimPrefix(dir[strings.LastIndex(dir, "/")+1:], application.TimingTreePrefix)
	if file, fails := c.failing[rig]; fails {
		return application.Checked{Output: " FAIL  " + file + " > backs off\nAssertionError: expected 3 to be 2\n"}, nil
	}
	return application.Checked{Passed: true}, nil
}

func (c *timingNightlyContext) rigSays(rig, globs, epic string) error {
	c.rigs[rig] = "/rigs/" + rig
	c.globs[rig] = strings.Split(globs, ",")
	c.epics[rig] = epic
	c.rules.Require(rig, domain.EpicRequirements{TimingTests: c.globs[rig], SuiteEpic: epic})
	c.tracker.AddEpic(epic, domain.Path{Rig: rig, Branch: "main"})
	return nil
}

func (c *timingNightlyContext) setClock(at string) error {
	t, err := time.Parse("2006-01-02 15:04", at)
	if err != nil {
		return fmt.Errorf("the time %q: %w", at, err)
	}
	c.now = t.UTC()
	return nil
}

// start makes the springer over the fakes, once a scenario has said what they hold.
func (c *timingNightlyContext) start() {
	if c.spring != nil {
		return
	}
	tests := map[string]string{}
	for rig := range c.rigs {
		tests[rig] = "npm test"
	}
	night := &application.NightlyTiming{
		Hour: application.DefaultNightlySuiteHour, Rigs: c.rigs, Tests: tests,
		Rules: c.rules, Suites: &apptest.FakeSuiteRecords{}, History: c.records, Records: c.records,
		Source: c.rig, Trees: &apptest.FakeSuiteTrees{}, Heads: c.heads, Runner: c.runner, Uptime: c.uptime,
		Filer: application.TimingFiler{Tracker: c.tracker, Rules: c.rules},
		Load:  c.load, LoadPoll: time.Second, LoadBound: 3 * time.Second,
		LoadWait: func(context.Context, time.Duration) error { return nil },
		Home:     func(context.Context) (bool, error) { return c.host == c.home, nil },
		Host:     c.host,
		Now:      func() time.Time { return c.now },
	}
	c.spring = &application.EventSpring{Log: c.log, Host: c.host, Jobs: []application.SpringJob{night.Job()}, Now: func() time.Time { return c.now }}
}

// bearings is the follower's first cycle, which springs nothing from what it
// finds: the clock is before the hour, so nothing is due in it either.
func (c *timingNightlyContext) bearings() error {
	c.now = time.Date(2026, 10, 21, 6, 0, 0, 0, time.UTC)
	return c.cycle()
}

func (c *timingNightlyContext) cycle() error {
	c.start()
	if err := c.spring.Spring(c.ctx); err != nil {
		return err
	}
	c.spring.Wait()
	return nil
}

func (c *timingNightlyContext) wasRun(rig string, n int) error {
	got := 0
	for _, ran := range c.runner.Ran() {
		if ran.Rig == rig && ran.Env["MW_TIMING"] == "only" && ran.Env["MW_TIMING_TESTS"] == strings.Join(c.globs[rig], ",") {
			got++
		}
	}
	if got != n {
		return fmt.Errorf("expected the tests of %s run with MW_TIMING=only %d times, got %d (all: %+v)", rig, n, got, c.runner.Ran())
	}
	return nil
}

func (c *timingNightlyContext) epicHolds(epic string, n int) error {
	detail, err := c.tracker.ShowEpic(c.ctx, epic)
	if err != nil {
		return err
	}
	if len(detail.Stories) != n {
		return fmt.Errorf("expected %d bugs under %s, got %d", n, epic, len(detail.Stories))
	}
	return nil
}

func (c *timingNightlyContext) bugIsHeld(file string) error {
	for epic := range c.epicSet() {
		detail, err := c.tracker.ShowEpic(c.ctx, epic)
		if err != nil {
			return err
		}
		for _, story := range detail.Stories {
			if !strings.Contains(story.Story.Title, file) {
				continue
			}
			if story.Status != apptest.StatusDeferred || strings.Join(story.Labels, ",") != "bug,timing" {
				return fmt.Errorf("expected %s held with the labels bug,timing, got %s with %v", story.Story.ID, story.Status, story.Labels)
			}
			c.lastBug = story.Story.ID
			return nil
		}
	}
	return fmt.Errorf("no bug names %s", file)
}

func (c *timingNightlyContext) epicSet() map[string]bool {
	set := map[string]bool{}
	for _, epic := range c.epics {
		set[epic] = true
	}
	return set
}

var quoted = regexp.MustCompile(`"([^"]*)"`)

func (c *timingNightlyContext) bugNames(list string) error {
	story, err := c.tracker.ShowStory(c.ctx, c.lastBug)
	if err != nil {
		return err
	}
	text := story.Story.Title + "\n" + story.Description
	for _, found := range quoted.FindAllStringSubmatch(list, -1) {
		if !strings.Contains(text, found[1]) {
			return fmt.Errorf("the bug %s does not name %q: %s", c.lastBug, found[1], text)
		}
	}
	return nil
}

func (c *timingNightlyContext) resultKept(rig, day, how, sha string) error {
	kept, ok := c.records.File("runs/timing/" + rig + "/" + day + ".json")
	if !ok {
		return fmt.Errorf("no result kept for %s on %s (kept: %v)", rig, day, c.records.Paths())
	}
	record, err := application.ParseTimingRecord(day, kept)
	if err != nil {
		return err
	}
	if record.Result.Passed != (how == "passed") || record.Result.Sha != sha || record.Result.UptimeBefore == "" {
		return fmt.Errorf("expected a result %s at %s with the uptime, got %+v", how, sha, record.Result)
	}
	return nil
}

func (c *timingNightlyContext) logTells(actor, states string) error {
	var got []string
	for _, e := range c.log.All() {
		if e.Kind == events.KindJob && e.Actor == actor {
			got = append(got, e.To)
		}
	}
	if want := strings.ReplaceAll(states, " ", ""); strings.Join(got, ",") != want {
		return fmt.Errorf("expected %s to be told as %s, got %s", actor, want, strings.Join(got, ","))
	}
	return nil
}

func (c *timingNightlyContext) lastEventSays(actor, said string) error {
	var last events.Event
	found := false
	for _, e := range c.log.All() {
		if e.Kind == events.KindJob && e.Actor == actor {
			last, found = e, true
		}
	}
	if !found || last.To != "done" || !strings.Contains(last.Detail, said) {
		return fmt.Errorf("expected the last event of %s to be done and say %q, got %+v", actor, said, last)
	}
	return nil
}
