package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// seatReapHost is the host the reaper scenarios run on.
const seatReapHost = "laptop"

// seatReapContext holds the vault a scenario's reaper watches a seat in, the
// terminal it looks at, and the clock it keeps. Time is the scenario's own: a
// sleep does not wait, it moves the clock on by the interval and lets the
// events the scenario scheduled for that look happen, so that a watch of three
// hours runs in no time.
type seatReapContext struct {
	dir     string
	windows *apptest.FakeWindows
	seat    string
	now     time.Time

	// look is how many looks the watch has made, and events what a scenario
	// made happen before each of them.
	look   int
	events map[int][]func() error
	// evented is the first thing an event failed with, which a sleep has nowhere
	// to return but the watch it interrupts.
	evented error
	// closedOnLook is the look each window was closed on.
	closedOnLook map[string]int

	report application.ReapReport
	err    error
}

// seatReapTerminal is the scenario's terminal: the fake, noting on which look
// a window was closed.
type seatReapTerminal struct {
	*apptest.FakeWindows
	c *seatReapContext
}

func (t seatReapTerminal) Close(ctx context.Context, id string) error {
	if err := t.FakeWindows.Close(ctx, id); err != nil {
		return err
	}
	t.c.closedOnLook[id] = t.c.look
	return nil
}

// InitializeSeatReapScenario registers the steps of features/seat_reap.feature.
func InitializeSeatReapScenario(ctx *godog.ScenarioContext) {
	c := &seatReapContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = seatReapContext{
			windows:      apptest.NewFakeWindows(),
			events:       map[int][]func() error{},
			closedOnLook: map[string]int{},
			now:          time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC),
		}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.dir != "" {
			_ = os.RemoveAll(c.dir)
		}
		return ctx, nil
	})

	ctx.Given(`^a vault for the "([^"]*)" seat$`, c.aVaultForTheSeat)
	ctx.Given(`^the clock reads "([^"]*)"$`, c.theClockReads)
	ctx.Given(`^the window "([^"]*)" named "([^"]*)" was opened at "([^"]*)"$`, c.theWindowWasOpenedAt)
	ctx.Given(`^the window "([^"]*)" named "([^"]*)" of no known age$`, c.theWindowHasNoKnownAge)
	ctx.Given(`^the acting file says "([^"]*)"$`, c.theActingFileSays)
	ctx.Given(`^the pane of "([^"]*)" has text on its input line$`, c.thePaneHasText)
	ctx.Given(`^the pane of "([^"]*)" is working$`, c.thePaneIsWorking)
	ctx.Given(`^the seat has written the handoff "([^"]*)" at "([^"]*)"$`, c.theSeatHasWrittenTheHandoff)
	ctx.Given(`^the reaper log already holds the line "([^"]*)"$`, c.theReaperLogAlreadyHolds)
	ctx.Given(`^before look (\d+) the acting file says "([^"]*)"$`, c.beforeLookTheActingFileSays)
	ctx.Given(`^before look (\d+) the window "([^"]*)" named "([^"]*)" is opened$`, c.beforeLookTheWindowIsOpened)
	ctx.Given(`^before look (\d+) the window "([^"]*)" is closed by someone else$`, c.beforeLookTheWindowIsClosed)
	ctx.Given(`^before look (\d+) the pane of "([^"]*)" is (idle|working)$`, c.beforeLookThePaneIs)
	ctx.Given(`^before look (\d+) the seat writes the handoff "([^"]*)"$`, c.beforeLookTheSeatWrites)

	ctx.When(`^the reaper watches "([^"]*)" in (successor|idle) mode, looking every (\d+) seconds for up to (\d+) (minutes|hours)$`, c.theReaperWatches)

	ctx.Then(`^the reaper closed the window "([^"]*)" on look (\d+)$`, c.theReaperClosedOnLook)
	ctx.Then(`^the reaper closed no window$`, c.theReaperClosedNoWindow)
	ctx.Then(`^the window "([^"]*)" is still open$`, c.theWindowIsStillOpen)
	ctx.Then(`^the reaper gave up$`, c.theReaperGaveUp)
	ctx.Then(`^the reaper says "([^"]*)"$`, c.theReaperSays)
	ctx.Then(`^the reaper log holds (\d+) lines$`, c.theLogHoldsLines)
	ctx.Then(`^the reaper log's line (\d+) says "([^"]*)"$`, c.theLogLineSays)
	ctx.Then(`^the reaper log's line (\d+) begins "([^"]*)"$`, c.theLogLineBegins)
	ctx.Then(`^the reaper is refused saying which window it is to watch$`, c.theReaperIsRefused)
}

func (c *seatReapContext) aVaultForTheSeat(seat string) error {
	dir, err := os.MkdirTemp("", "mw-seat-reap-")
	if err != nil {
		return fmt.Errorf("making a vault: %w", err)
	}
	c.dir, c.seat = dir, seat
	return os.MkdirAll(filepath.Join(dir, "seats", seat, "handoffs"), 0o755)
}

func (c *seatReapContext) theClockReads(when string) error {
	at, err := time.Parse(time.RFC3339, when)
	if err != nil {
		return fmt.Errorf("%q is not a time: %w", when, err)
	}
	c.now = at
	return nil
}

func (c *seatReapContext) theWindowWasOpenedAt(id, name, when string) error {
	opened, err := time.Parse(time.RFC3339, when)
	if err != nil {
		return fmt.Errorf("%q is not a time: %w", when, err)
	}
	c.windows.HoldsWithID(id, name, opened)
	return nil
}

func (c *seatReapContext) theWindowHasNoKnownAge(id, name string) error {
	c.windows.HoldsWithID(id, name, time.Time{})
	return nil
}

func (c *seatReapContext) writeFile(contents string, parts ...string) error {
	path := filepath.Join(append([]string{c.dir}, parts...)...)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(contents), 0o644)
}

func (c *seatReapContext) theActingFileSays(text string) error {
	return c.writeFile(text+"\n", application.ActingFileName(c.seat))
}

func (c *seatReapContext) thePaneHasText(id string) error {
	return c.windows.Pane(id, application.PaneInput)
}

func (c *seatReapContext) thePaneIsWorking(id string) error {
	return c.windows.Pane(id, application.PaneWorking)
}

func (c *seatReapContext) writeHandoff(name string, written time.Time) error {
	if err := c.writeFile("What the session left for the next.\n", "seats", c.seat, "handoffs", name+".md"); err != nil {
		return err
	}
	return os.Chtimes(filepath.Join(c.dir, "seats", c.seat, "handoffs", name+".md"), written, written)
}

func (c *seatReapContext) theSeatHasWrittenTheHandoff(name, when string) error {
	written, err := time.Parse(time.RFC3339, when)
	if err != nil {
		return fmt.Errorf("%q is not a time: %w", when, err)
	}
	return c.writeHandoff(name, written)
}

func (c *seatReapContext) theReaperLogAlreadyHolds(line string) error {
	return c.writeFile(line+"\n", application.ReapLogFileName(c.seat))
}

// before schedules what happens just before a look: after the clock has moved
// on to it, and before the watch reads anything.
func (c *seatReapContext) before(look string, event func() error) error {
	var n int
	if _, err := fmt.Sscanf(look, "%d", &n); err != nil || n < 1 {
		return fmt.Errorf("%q is not a look to schedule something before", look)
	}
	c.events[n] = append(c.events[n], event)
	return nil
}

func (c *seatReapContext) beforeLookTheActingFileSays(look, text string) error {
	return c.before(look, func() error { return c.theActingFileSays(text) })
}

func (c *seatReapContext) beforeLookTheWindowIsOpened(look, id, name string) error {
	return c.before(look, func() error {
		c.windows.HoldsWithID(id, name, c.now)
		return nil
	})
}

func (c *seatReapContext) beforeLookTheWindowIsClosed(look, id string) error {
	return c.before(look, func() error {
		c.windows.Vanish(id)
		return nil
	})
}

func (c *seatReapContext) beforeLookThePaneIs(look, id, state string) error {
	return c.before(look, func() error {
		if state == "idle" {
			return c.windows.Pane(id, application.PaneIdle)
		}
		return c.windows.Pane(id, application.PaneWorking)
	})
}

func (c *seatReapContext) beforeLookTheSeatWrites(look, name string) error {
	return c.before(look, func() error { return c.writeHandoff(name, c.now) })
}

// sleep is the watch's sleep: no waiting, the clock moves on by the interval
// and what the scenario scheduled for the look it leads to happens.
func (c *seatReapContext) sleep(_ context.Context, d time.Duration) error {
	c.look++
	c.now = c.now.Add(d)
	for _, event := range c.events[c.look] {
		if err := event(); err != nil {
			return fmt.Errorf("making something happen before look %d: %w", c.look, err)
		}
	}
	return nil
}

var seatReapDuration = map[string]time.Duration{"minutes": time.Minute, "hours": time.Hour}

func (c *seatReapContext) theReaperWatches(window, mode string, seconds, limit int, unit string) error {
	files := vault.New(c.dir)
	c.report, c.err = application.SeatReap{
		Seats:    files,
		Terminal: seatReapTerminal{FakeWindows: c.windows, c: c},
		Log:      files,
		Seat:     c.seat,
		Host:     seatReapHost,
		Window:   window,
		WhenIdle: mode == "idle",
		Interval: time.Duration(seconds) * time.Second,
		Limit:    time.Duration(limit) * seatReapDuration[unit],
		Now:      func() time.Time { return c.now },
		Sleep:    c.sleep,
	}.Run(context.Background())
	return nil
}

func (c *seatReapContext) theReaperClosedOnLook(id string, look int) error {
	got, closed := c.closedOnLook[id]
	switch {
	case !closed:
		return fmt.Errorf("expected the reaper to close %s on look %d, it closed nothing (%v, %q)", id, look, c.err, c.report.Said)
	case got != look:
		return fmt.Errorf("expected the reaper to close %s on look %d, it closed it on look %d", id, look, got)
	}
	return nil
}

func (c *seatReapContext) theReaperClosedNoWindow() error {
	if closed := c.windows.ClosedIDs(); len(closed) != 0 {
		return fmt.Errorf("expected the reaper to close nothing, it closed %v", closed)
	}
	return nil
}

func (c *seatReapContext) theWindowIsStillOpen(id string) error {
	if !c.windows.IsOpen(id) {
		return fmt.Errorf("expected the window %s to be open, it is not", id)
	}
	return nil
}

func (c *seatReapContext) theReaperGaveUp() error {
	if c.report.Outcome != application.ReapGaveUp || c.err == nil {
		return fmt.Errorf("expected the reaper to give up, it ended with %q and %v", c.report.Outcome, c.err)
	}
	return nil
}

func (c *seatReapContext) theReaperSays(want string) error {
	if !strings.Contains(c.report.String(), want) {
		return fmt.Errorf("expected the reaper to say %q, it said %q", want, c.report.String())
	}
	return nil
}

// logLines is what the seat's reaper log holds, a string a line.
func (c *seatReapContext) logLines() ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(c.dir, application.ReapLogFileName(c.seat)))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n"), nil
}

func (c *seatReapContext) theLogHoldsLines(want int) error {
	lines, err := c.logLines()
	if err != nil {
		return err
	}
	if len(lines) != want {
		return fmt.Errorf("expected the reaper log to hold %d lines, it holds %d: %q", want, len(lines), lines)
	}
	return nil
}

func (c *seatReapContext) logLine(n int) (string, error) {
	lines, err := c.logLines()
	if err != nil {
		return "", err
	}
	if n < 1 || n > len(lines) {
		return "", fmt.Errorf("expected the reaper log to have a line %d, it holds %d: %q", n, len(lines), lines)
	}
	return lines[n-1], nil
}

func (c *seatReapContext) theLogLineSays(n int, want string) error {
	line, err := c.logLine(n)
	if err != nil {
		return err
	}
	if !strings.Contains(line, want) {
		return fmt.Errorf("expected line %d of the reaper log to say %q, it says %q", n, want, line)
	}
	return nil
}

var seatReapDated = regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ `)

func (c *seatReapContext) theLogLineBegins(n int, want string) error {
	line, err := c.logLine(n)
	if err != nil {
		return err
	}
	if !seatReapDated.MatchString(line) {
		return fmt.Errorf("expected line %d of the reaper log to begin with a date, it is %q", n, line)
	}
	if !strings.HasPrefix(line, want) {
		return fmt.Errorf("expected line %d of the reaper log to begin %q, it is %q", n, want, line)
	}
	return nil
}

func (c *seatReapContext) theReaperIsRefused() error {
	if c.err == nil || c.report.Outcome != "" {
		return fmt.Errorf("expected the reaper to be refused, it ended with %q and %v", c.report.Outcome, c.err)
	}
	if !strings.Contains(c.err.Error(), "window") {
		return fmt.Errorf("expected the refusal to say which window is to be watched, got %q", c.err)
	}
	return nil
}
