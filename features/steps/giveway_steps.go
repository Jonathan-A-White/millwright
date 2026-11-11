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

// giveWayContext is one give-way scenario: a host's processes, panes, marker and
// log in memory, the retry and the tracker behind a dispatch.
type giveWayContext struct {
	host    *apptest.FakeGiveWayHost
	retry   *apptest.FakeGiveWayRetry
	way     application.GiveWay
	now     time.Time
	out     bytes.Buffer
	failure error

	tracker  *apptest.FakeTracker
	runner   *apptest.FakeRunner
	dispatch bytes.Buffer
	report   application.DispatchReport
}

// giveWayNoWorktrees stands for the rig checkouts a dispatch that gives way, or
// has nothing ready, never touches.
type giveWayNoWorktrees struct{ application.Worktrees }

func gwProc(pid, ppid int, state, start string, argv ...string) application.GiveWayProcess {
	return application.GiveWayProcess{Pid: pid, Ppid: ppid, Sid: ppid, State: state, Start: start, Argv: argv}
}

func (c *giveWayContext) reset() {
	c.failure, c.out, c.dispatch, c.report = nil, bytes.Buffer{}, bytes.Buffer{}, application.DispatchReport{}
	c.now = time.Date(2026, 11, 13, 16, 40, 0, 0, time.UTC)
}

func (c *giveWayContext) aHostWithSessions(name, a, b string) error {
	c.host = &apptest.FakeGiveWayHost{
		Procs: []application.GiveWayProcess{
			gwProc(100, 1, "S", "t100", "tmux: server"),
			gwProc(200, 100, "S", "t200", "/bin/sh", "-c", "run"),
			gwProc(201, 200, "S", "t201", "claude", "--print"),
			gwProc(202, 200, "S", "t202", "/bin/mw", "next", "--heartbeat", "mw-a.1"),
			gwProc(203, 201, "R", "t203", "go", "test", "./..."),
			gwProc(300, 100, "S", "t300", "/bin/sh", "-c", "run"),
			gwProc(301, 300, "S", "t301", "claude", "--print"),
			gwProc(302, 300, "S", "t302", "/bin/mw", "next", "--heartbeat", "mw-b.2"),
			gwProc(500, 1, "S", "t500", "firefox"),
			gwProc(900, 1, "R", "t900", "/bin/mw", "giveway", "on"),
		},
		PaneList: []application.GiveWayPane{{Session: a, Pid: 200}, {Session: b, Pid: 300}},
	}
	land := gwProc(400, 1, "S", "t400", "sh", "-c", "land")
	land.Scope = true
	build := gwProc(401, 400, "R", "t401", "go", "build")
	build.Scope = true
	c.host.Procs = append(c.host.Procs, land, build)
	c.retry = &apptest.FakeGiveWayRetry{Host: c.host}
	c.reset()
	c.way = application.GiveWay{
		Processes: c.host, Panes: c.host, Marker: c.host, Log: c.host, Retry: c.retry, Host: name, Self: 900,
		Now: func() time.Time { return c.now }, Out: &c.out,
	}
	c.tracker = apptest.NewFakeTracker()
	c.runner = apptest.NewFakeRunner()
	return nil
}

func (c *giveWayContext) givenOnHasRun() error {
	return c.way.On(context.Background(), application.GiveWayRequest{})
}

func (c *giveWayContext) whenOnRuns() error {
	c.failure = c.way.On(context.Background(), application.GiveWayRequest{})
	return nil
}

func (c *giveWayContext) whenOffRuns() error {
	c.failure = c.way.Off(context.Background(), application.GiveWayRequest{})
	return nil
}

func (c *giveWayContext) minutesPassed(n int) error {
	c.now = c.now.Add(time.Duration(n) * time.Minute)
	return nil
}

func (c *giveWayContext) everyProcessStopped() error {
	if c.failure != nil {
		return c.failure
	}
	got := fmt.Sprint(c.host.Stopped())
	if want := fmt.Sprint([]int{201, 203, 301, 400, 401}); got != want {
		return fmt.Errorf("stopped %s, want %s", got, want)
	}
	return nil
}

func (c *giveWayContext) noHeartbeatStopped() error {
	for _, pid := range []int{100, 202, 302, 500, 900} {
		if c.host.State(pid) == "T" {
			return fmt.Errorf("process %d is stopped, and must not be", pid)
		}
	}
	return nil
}

func (c *giveWayContext) markerStands() error {
	if c.host.Marker == nil {
		return fmt.Errorf("no give-way marker")
	}
	return nil
}

func (c *giveWayContext) markerGone() error {
	if c.host.Marker != nil {
		return fmt.Errorf("the give-way marker is still there: %+v", c.host.Marker)
	}
	return nil
}

func (c *giveWayContext) lastEntry() (map[string]any, error) {
	if len(c.host.Lines) == 0 {
		return nil, fmt.Errorf("the give-way log is empty")
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(c.host.Lines[len(c.host.Lines)-1]), &entry); err != nil {
		return nil, fmt.Errorf("the last log line is not JSON: %w", err)
	}
	return entry, nil
}

func (c *giveWayContext) logHas(n int, event string) error {
	if len(c.host.Lines) != n {
		return fmt.Errorf("the log has %d lines, want %d: %q", len(c.host.Lines), n, c.host.Lines)
	}
	entry, err := c.lastEntry()
	if err != nil {
		return err
	}
	if entry["event"] != event {
		return fmt.Errorf("the last log line is event %v, want %s", entry["event"], event)
	}
	return nil
}

func (c *giveWayContext) logHasAfter(n int, event string, minutes int) error {
	if err := c.logHas(n, event); err != nil {
		return err
	}
	entry, _ := c.lastEntry()
	if want := float64((time.Duration(minutes) * time.Minute).Milliseconds()); entry["gave_way_ms"] != want {
		return fmt.Errorf("gave_way_ms is %v, want %v", entry["gave_way_ms"], want)
	}
	return nil
}

func (c *giveWayContext) newProcess(session string) error {
	c.host.Procs = append(c.host.Procs, gwProc(299, 201, "R", "t299", "make"))
	c.host.Calls = nil
	return nil
}

func (c *giveWayContext) onlyNewStopped() error {
	if fmt.Sprint(c.host.Calls) != "[stop 299]" {
		return fmt.Errorf("signals sent: %v, want only [stop 299]", c.host.Calls)
	}
	return nil
}

func (c *giveWayContext) dispatchRuns() error { return c.runDispatch(true) }

func (c *giveWayContext) dispatchRunsIdle() error { return c.runDispatch(false) }

func (c *giveWayContext) runDispatch(ready bool) error {
	c.tracker.AddEpic("mw-gq6", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Formula: "tdd-feature", Host: c.way.Host})
	c.tracker.AddFormula("tdd-feature")
	if ready {
		c.tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	}
	d := application.Dispatch{
		Tracker: c.tracker, Worktrees: giveWayNoWorktrees{}, Runner: c.runner, Host: c.way.Host, Cap: 1,
		Rigs: map[string]string{"millwright": "/rigs/millwright"}, GiveWay: c.way, Out: &c.dispatch,
		Now: func() time.Time { return c.now },
	}
	report, err := d.Run(context.Background())
	c.report, c.failure = report, err
	return nil
}

func (c *giveWayContext) dispatchSays(words string) error {
	if !strings.Contains(c.dispatch.String(), words) {
		return fmt.Errorf("dispatch said %q, want %q in it", c.dispatch.String(), words)
	}
	return nil
}

func (c *giveWayContext) dispatchStartedNothing() error {
	if c.report.GivesWay == nil || len(c.report.Started) != 0 || len(c.runner.Names()) != 0 {
		return fmt.Errorf("report %+v: dispatch did not give way", c.report)
	}
	if d, _ := c.tracker.ShowStory(context.Background(), "mw-gq6.1"); d.Assignee != "" {
		return fmt.Errorf("the story was claimed for %q", d.Assignee)
	}
	return nil
}

func (c *giveWayContext) noneStopped() error {
	if len(c.host.Stopped()) != 0 {
		return fmt.Errorf("still stopped: %v", c.host.Stopped())
	}
	return nil
}

func (c *giveWayContext) paneDied(session string) error {
	c.host.PaneList = []application.GiveWayPane{{Session: "mw-a_1", Pid: 200}, {Session: session, Dead: true}}
	return nil
}

func (c *giveWayContext) retried(story string) error {
	for _, r := range c.retry.Retried {
		if r == story {
			return nil
		}
	}
	return fmt.Errorf("retried %v, want %s among them", c.retry.Retried, story)
}

func (c *giveWayContext) notRetried(story string) error {
	for _, r := range c.retry.Retried {
		if r == story {
			return fmt.Errorf("%s was retried", story)
		}
	}
	return nil
}

func (c *giveWayContext) leftStopped() error {
	for i := range c.host.Procs {
		if c.host.Procs[i].Pid == 203 {
			c.host.Procs[i].State = "T"
		}
	}
	return nil
}

func (c *giveWayContext) saysNoMarker() error {
	if !strings.Contains(c.out.String(), "no marker") {
		return fmt.Errorf("mw giveway said %q", c.out.String())
	}
	return nil
}

func (c *giveWayContext) wentOn() error {
	if c.failure != nil {
		return c.failure
	}
	if c.report.GivesWay != nil || strings.Contains(c.dispatch.String(), "gives way") {
		return fmt.Errorf("report %+v: dispatch did not go on", c.report)
	}
	return nil
}

func (c *giveWayContext) isTheHome() error {
	c.way.Home = &apptest.FakeHomeFile{Text: "desktop 2026-11-11T00:00:00Z mw@desktop\n"}
	return nil
}

func (c *giveWayContext) refused() error {
	if c.failure == nil || !strings.Contains(c.failure.Error(), "never gives way") {
		return fmt.Errorf("on returned %v, want a refusal saying the home never gives way", c.failure)
	}
	return nil
}

// InitializeGiveWayScenario registers the steps of features/giveway.feature.
func InitializeGiveWayScenario(ctx *godog.ScenarioContext) {
	c := &giveWayContext{}
	ctx.Given(`^a host "([^"]*)" with the Builder sessions "([^"]*)" and "([^"]*)", each with a heartbeat, and a landing in an apart scope$`, c.aHostWithSessions)
	ctx.Given(`^mw giveway on has run$`, c.givenOnHasRun)
	ctx.Given(`^a new process has started in the session "([^"]*)"$`, c.newProcess)
	ctx.Given(`^(\d+) minutes have passed(?: with no renewal)?$`, c.minutesPassed)
	ctx.Given(`^the session "([^"]*)" died while the host gave way$`, c.paneDied)
	ctx.Given(`^a Builder process was left stopped and no marker stands$`, c.leftStopped)
	ctx.Given(`^this host is the home$`, c.isTheHome)
	ctx.When(`^mw giveway on runs$`, c.whenOnRuns)
	ctx.When(`^mw giveway off runs$`, c.whenOffRuns)
	ctx.When(`^dispatch runs on the host with a story ready$`, c.dispatchRuns)
	ctx.When(`^dispatch runs on the host with nothing ready$`, c.dispatchRunsIdle)
	ctx.Then(`^every process of both sessions and of the apart scope is stopped$`, c.everyProcessStopped)
	ctx.Then(`^no heartbeat process and no tmux server is stopped$`, c.noHeartbeatStopped)
	ctx.Then(`^the give-way marker stands$`, c.markerStands)
	ctx.Then(`^the give-way marker is gone$`, c.markerGone)
	ctx.Then(`^the give-way log has (\d+) lines?, (?:of|the last of) event "([^"]*)"$`, c.logHas)
	ctx.Then(`^the give-way log has (\d+) lines, the last of event "([^"]*)", after (\d+) minutes of giving way$`, c.logHasAfter)
	ctx.Then(`^only the new process was sent a stop$`, c.onlyNewStopped)
	ctx.Then(`^dispatch says "([^"]*)"$`, c.dispatchSays)
	ctx.Then(`^dispatch started nothing and claimed nothing$`, c.dispatchStartedNothing)
	ctx.Then(`^no process is stopped$`, c.noneStopped)
	ctx.Then(`^the story "([^"]*)" was retried$`, c.retried)
	ctx.Then(`^the story "([^"]*)" was not retried$`, c.notRetried)
	ctx.Then(`^mw giveway says there was no marker$`, c.saysNoMarker)
	ctx.Then(`^dispatch went on past the gate$`, c.wentOn)
	ctx.Then(`^mw giveway is refused, saying the home never gives way$`, c.refused)
}
