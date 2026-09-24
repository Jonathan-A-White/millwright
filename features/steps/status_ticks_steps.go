package steps

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/ticklog"

	"github.com/cucumber/godog"
)

// tickKinds are the two timers whose logs mw status counts, as a scenario names
// them, and what the report calls each.
var tickKinds = map[string]string{
	"dispatch":      application.TicksDispatchLabel,
	"Millhand tick": application.TicksMillhandLabel,
}

// tickWords is what each kind of log writes for a good run, a failed one and a
// local network fault, as the timer's own use case writes it: the counts are
// read out of the same words a real host's log holds.
var tickWords = map[string]map[string]string{
	"dispatch": {
		"good":   "ok: 1 started",
		"failed": "failed: dispatching on vps: the hosts could not be brought level",
		"fault":  "local network fault",
	},
	"Millhand tick": {
		"good":   "quiet",
		"failed": "wake failed: starting the Millhand: the runner would not start",
		"fault":  "quiet; local-fault: this host's network is down, so it did not sync",
	},
}

// registerTickSteps registers the steps that fill a host's logs and read the
// TICKS section back.
func (c *statusContext) registerTickSteps(ctx *godog.ScenarioContext) {
	ctx.Given(`^the (dispatch|Millhand tick) log of "([^"]*)" holds a good run at "([^"]*)"$`, c.aLogHoldsAGoodRunAt)
	ctx.Given(`^the (dispatch|Millhand tick) log of "([^"]*)" then holds a line saying "([^"]*)" at "([^"]*)"$`, c.aLogThenHoldsALineSayingAt)
	ctx.Given(`^the (dispatch|Millhand tick) log of "([^"]*)" (?:then )?holds (\d+) (good run|failed run|local network fault)s?$`,
		c.aLogHoldsRuns)
	ctx.Given(`^the host "([^"]*)" syncs$`, c.theHostSyncs)

	ctx.Then(`^the TICKS section says of the (dispatch|Millhand tick): "([^"]*)"$`, c.theTicksSectionSays)
	ctx.Then(`^the TICKS section does not say of the (dispatch|Millhand tick): "([^"]*)"$`, c.theTicksSectionDoesNotSay)
	ctx.Then(`^the TICKS section has nothing on the (dispatch|Millhand tick)$`, c.theTicksSectionHasNothingOn)
	ctx.Then(`^the report has no TICKS section$`, c.theReportHasNoTicksSection)
	ctx.Then(`^under OTHER HOSTS the host "([^"]*)" shows for the (dispatch|Millhand tick): "([^"]*)"$`, c.otherHostShowsForTicks)
	ctx.Then(`^under OTHER HOSTS the host "([^"]*)" shows nothing of its ticks$`, c.otherHostShowsNoTicks)
}

// tickLogDir is where a host's log of one kind is kept in the scenario: inside
// its own temp directory, never under the real home.
func (c *statusContext) tickLogDir(host, kind string) (string, error) {
	root, err := c.workspace()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "state", host, strings.ReplaceAll(kind, " ", "-")), nil
}

// tickLogsOf are a host's two logs, as its use cases are given them.
func (c *statusContext) tickLogsOf(host string) (application.TickLogs, error) {
	dispatch, err := c.tickLogDir(host, "dispatch")
	if err != nil {
		return application.TickLogs{}, err
	}
	millhand, err := c.tickLogDir(host, "Millhand tick")
	if err != nil {
		return application.TickLogs{}, err
	}
	return application.TickLogs{Dispatch: ticklog.New(dispatch), Millhand: ticklog.New(millhand)}, nil
}

// appendTick writes one line into a host's log of one kind.
func (c *statusContext) appendTick(kind, host string, at time.Time, words string) error {
	dir, err := c.tickLogDir(host, kind)
	if err != nil {
		return err
	}
	if c.lastTick == nil {
		c.lastTick = map[string]time.Time{}
	}
	c.lastTick[host+"/"+kind] = at
	return ticklog.New(dir).Append(context.Background(), at.UTC().Format(time.RFC3339)+" "+words)
}

func (c *statusContext) aLogHoldsAGoodRunAt(kind, host, at string) error {
	when, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return fmt.Errorf("the time %q is not RFC 3339: %w", at, err)
	}
	return c.appendTick(kind, host, when, tickWords[kind]["good"])
}

// aLogThenHoldsALineSayingAt appends one line of a scenario's own choosing,
// dated, to a host's log: what mw status shows of a resume grace is read
// straight off the tick's own words, so a scenario writes them verbatim
// rather than through a fixed outcome.
func (c *statusContext) aLogThenHoldsALineSayingAt(kind, host, words, at string) error {
	when, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return fmt.Errorf("the time %q is not RFC 3339: %w", at, err)
	}
	return c.appendTick(kind, host, when, words)
}

// aLogHoldsRuns adds runs a quarter of an hour apart, the first a quarter of an
// hour after the last line the log holds — or at nine o'clock when it holds none.
func (c *statusContext) aLogHoldsRuns(kind, host, countText, what string) error {
	count, err := strconv.Atoi(countText)
	if err != nil {
		return fmt.Errorf("the count %q is not a number: %w", countText, err)
	}
	outcome := map[string]string{"good run": "good", "failed run": "failed", "local network fault": "fault"}[what]

	next := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	if last, ok := c.lastTick[host+"/"+kind]; ok {
		next = last.Add(15 * time.Minute)
	}
	for i := 0; i < count; i++ {
		if err := c.appendTick(kind, host, next, tickWords[kind][outcome]); err != nil {
			return err
		}
		next = next.Add(15 * time.Minute)
	}
	return nil
}

// theHostSyncs is another host finishing a sync, which is what carries the
// counts of its logs out as a note: its vault and its beads are stand-ins.
func (c *statusContext) theHostSyncs(host string) error {
	logs, err := c.tickLogsOf(host)
	if err != nil {
		return err
	}
	_, err = application.Sync{
		Vault:   &apptest.FakeVaultFiles{},
		Tracker: c.tracker,
		Host:    host,
		Ticks:   logs,
		Now:     func() time.Time { return c.now },
	}.Run(context.Background())
	if err != nil {
		return fmt.Errorf("the host %s could not sync: %w", host, err)
	}
	return nil
}

// printedLines is the report as a person reads it, a line at a time.
func (c *statusContext) printedLines() ([]string, error) {
	if err := c.readingStatusSucceeds(); err != nil {
		return nil, err
	}
	return strings.Split(c.report.String(), "\n"), nil
}

// sectionOf is the lines under the one that begins with heading, up to the
// blank line that ends the section; nil when there is no such section.
func sectionOf(lines []string, heading string) []string {
	for i, line := range lines {
		if !strings.HasPrefix(line, heading) {
			continue
		}
		var section []string
		for _, under := range lines[i+1:] {
			if strings.TrimSpace(under) == "" {
				break
			}
			section = append(section, under)
		}
		return section
	}
	return nil
}

// blockAt is the first line that begins with prefix and the lines under it that
// are indented deeper than it is; nil when no line begins so.
func blockAt(lines []string, prefix string) []string {
	for i, line := range lines {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		block := []string{line}
		for _, under := range lines[i+1:] {
			if indentOf(under) <= indentOf(line) {
				break
			}
			block = append(block, under)
		}
		return block
	}
	return nil
}

func indentOf(line string) int { return len(line) - len(strings.TrimLeft(line, " ")) }

func (c *statusContext) theTicksSectionSays(kind, words string) error {
	lines, err := c.printedLines()
	if err != nil {
		return err
	}
	block := blockAt(sectionOf(lines, application.TicksHeading), "  "+tickKinds[kind]+" ")
	if !strings.Contains(strings.Join(block, "\n"), words) {
		return fmt.Errorf("expected the TICKS section to say of the %s %q, got:\n%s", kind, words, c.report.String())
	}
	return nil
}

func (c *statusContext) theTicksSectionDoesNotSay(kind, words string) error {
	lines, err := c.printedLines()
	if err != nil {
		return err
	}
	block := blockAt(sectionOf(lines, application.TicksHeading), "  "+tickKinds[kind]+" ")
	if strings.Contains(strings.Join(block, "\n"), words) {
		return fmt.Errorf("expected the TICKS section not to say of the %s %q, got:\n%s", kind, words, c.report.String())
	}
	return nil
}

func (c *statusContext) theTicksSectionHasNothingOn(kind string) error {
	lines, err := c.printedLines()
	if err != nil {
		return err
	}
	if block := blockAt(sectionOf(lines, application.TicksHeading), "  "+tickKinds[kind]+" "); block != nil {
		return fmt.Errorf("expected the TICKS section to say nothing of the %s, got:\n%s", kind, c.report.String())
	}
	return nil
}

func (c *statusContext) theReportHasNoTicksSection() error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	if strings.Contains(c.report.String(), application.TicksHeading) {
		return fmt.Errorf("expected no %s section, got:\n%s", application.TicksHeading, c.report.String())
	}
	return nil
}

func (c *statusContext) otherHostShowsForTicks(host, kind, words string) error {
	lines, err := c.printedLines()
	if err != nil {
		return err
	}
	hostBlock := blockAt(sectionOf(lines, "OTHER HOSTS"), "  "+host+" ")
	block := blockAt(hostBlock, "    "+tickKinds[kind]+" ")
	if !strings.Contains(strings.Join(block, "\n"), words) {
		return fmt.Errorf("expected %s under OTHER HOSTS to show of the %s %q, got:\n%s", host, kind, words, c.report.String())
	}
	return nil
}

func (c *statusContext) otherHostShowsNoTicks(host string) error {
	lines, err := c.printedLines()
	if err != nil {
		return err
	}
	hostBlock := blockAt(sectionOf(lines, "OTHER HOSTS"), "  "+host+" ")
	if hostBlock == nil {
		return fmt.Errorf("expected %s to be listed under OTHER HOSTS, got:\n%s", host, c.report.String())
	}
	for _, kind := range tickKinds {
		if blockAt(hostBlock, "    "+kind+" ") != nil {
			return fmt.Errorf("expected %s to show nothing of its ticks, got:\n%s", host, c.report.String())
		}
	}
	return nil
}
