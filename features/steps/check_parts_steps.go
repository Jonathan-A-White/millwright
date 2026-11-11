package steps

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// registerCheckPartsSteps registers the steps of features/check_parts.feature: a
// rig whose gate the host names as parts, each part a shell command that writes
// one word to the scenario's log so that what ran, and in what order, can be read.
func registerCheckPartsSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the rig's gate is split into these parts:$`, c.theGateIsSplit)
	ctx.Given(`^the rig's gate is not split into parts$`, c.theGateIsNotSplit)
	ctx.Given(`^the part "([^"]*)" fails, saying "([^"]*)"$`, c.aPartFails)
	ctx.Given(`^the part "([^"]*)" fails its first run only, saying "([^"]*)"$`, c.aPartFailsOnce)

	ctx.Given(`^the call's BASH_MAX_TIMEOUT_MS is "([^"]*)"$`, func(value string) error { c.callEnv = value; return nil })
	ctx.Given(`^the part "([^"]*)" runs for (\d+) seconds on the call's clock$`, func(_ string, seconds int) error {
		c.partRunsFor = time.Duration(seconds) * time.Second
		return nil
	})

	ctx.When(`^a session checks "([^"]*)" part (\d+)$`, c.aSessionChecksAPart)
	ctx.When(`^a session checks "([^"]*)" part (\d+) in one foreground call$`, c.aSessionChecksAPartInOneCall)
	ctx.When(`^the session of "([^"]*)" commits another change$`, c.theSessionCommitsAgain)

	ctx.Then(`^the check part passes$`, c.theCheckPartPasses)
	ctx.Then(`^the check part fails$`, c.theCheckPartFails)
	ctx.Then(`^the check part error says: (.+)$`, c.theCheckPartErrorSays)
	ctx.Then(`^the parts that ran, in order, were: (.+)$`, c.thePartsThatRan)
}

func (c *nextContext) theGateIsSplit(table *godog.Table) error {
	c.gateParts = nil
	for _, row := range table.Rows[1:] {
		command := strings.ReplaceAll(row.Cells[1].Value, "{log}", c.checkLog)
		c.gateParts = append(c.gateParts, application.GatePart{Name: row.Cells[0].Value, Command: command})
	}
	return nil
}

func (c *nextContext) theGateIsNotSplit() error {
	c.gateParts = nil
	return nil
}

func (c *nextContext) aPartFails(name, saying string) error {
	for i, part := range c.gateParts {
		if part.Name == name {
			c.gateParts[i].Command = fmt.Sprintf("%s; echo '%s'; exit 1", part.Command, saying)
			return nil
		}
	}
	return fmt.Errorf("the gate has no part %q", name)
}

// aPartFailsOnce is a part as a loaded host runs it: the first run, which is the
// first line the part's command writes to the scenario's log, fails.
func (c *nextContext) aPartFailsOnce(name, saying string) error {
	for i, part := range c.gateParts {
		if part.Name == name {
			c.gateParts[i].Command = fmt.Sprintf("%s; if [ \"$(wc -l < %s)\" -le 1 ]; then echo '%s'; exit 1; fi", part.Command, c.checkLog, saying)
			return nil
		}
	}
	return fmt.Errorf("the gate has no part %q", name)
}

func (c *nextContext) aSessionChecksAPart(id string, part int) error {
	c.partToCheck = part
	defer func() { c.partToCheck = 0 }()
	return c.checkWith(id, rig.NewSlots())
}

func (c *nextContext) theSessionCommitsAgain(id string) error {
	dir := application.WorktreeDir(c.rig, id)
	if err := os.WriteFile(filepath.Join(dir, id+"-again.md"), []byte("more work on "+id+"\n"), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-qm", "More work on " + id}} {
		if err := gitRun(dir, "git", args...); err != nil {
			return err
		}
	}
	return nil
}

func (c *nextContext) theCheckPartPasses() error {
	if c.err != nil {
		return fmt.Errorf("expected the part to pass, got %v\n%s", c.err, c.printed.String())
	}
	return nil
}

func (c *nextContext) theCheckPartFails() error { return c.theCheckFails() }

func (c *nextContext) theCheckPartErrorSays(words string) error {
	if c.err == nil || !strings.Contains(c.err.Error(), strings.TrimSpace(words)) {
		return fmt.Errorf("expected the error to say %q, got %v", words, c.err)
	}
	return nil
}

func (c *nextContext) thePartsThatRan(words string) error {
	var ran []string
	if held, err := os.ReadFile(c.checkLog); err == nil {
		ran = strings.Fields(string(held))
	}
	if got, want := strings.Join(ran, ", "), strings.TrimSpace(words); got != want {
		return fmt.Errorf("expected the parts %q to have run, got %q", want, got)
	}
	return nil
}

// callClock is the clock of one foreground tool call: every look at it is ten
// seconds later than the last, so a part that outlasts 540 seconds of the call
// does so in a few dozen looks and no real wait.
type callClock struct {
	mu   sync.Mutex
	now  time.Time
	step time.Duration
}

func (c *callClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	at := c.now
	c.now = c.now.Add(c.step)
	return at
}

func (c *callClock) peek() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// clockedPart stands in for the process mw check --part starts for a part that
// takes no merge slot: it runs the same check as a part worker, at once, and is
// alive until the call's clock has gone runsFor past the start.
type clockedPart struct {
	clock   *callClock
	runsFor time.Duration
	run     func()
	began   time.Time
	done    chan struct{}
}

func (p *clockedPart) Start(context.Context, string, int, string) (int, error) {
	p.began = p.clock.peek()
	p.done = make(chan struct{})
	go func() {
		defer close(p.done)
		p.run()
	}()
	return 4242, nil
}

func (p *clockedPart) Kill(int) error { return nil }

func (p *clockedPart) Alive(int) bool {
	if p.clock.peek().Sub(p.began) < p.runsFor {
		return true
	}
	<-p.done
	return false
}

// aSessionChecksAPartInOneCall is a foreground mw check --part N as the harness's
// tool call runs it: the environment says BASH_MAX_TIMEOUT_MS as the scenario
// gave it, and the part is run by a process of its own that outlasts the call's
// clock by as long as the scenario says.
func (c *nextContext) aSessionChecksAPartInOneCall(id string, part int) error {
	c.partToCheck = part
	defer func() { c.partToCheck = 0 }()
	limits := application.CheckCallLimitsFor(c.callEnv)
	clock := &callClock{now: time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC), step: 10 * time.Second}
	worktrees := rig.New(rig.WithProgram(c.gitProgram))
	build := func(worker bool, out *bytes.Buffer) application.Check {
		k := c.newCheck(worktrees, nil, nil, out)
		k.Logs, k.Records = vault.New(c.vault), vault.New(c.vault)
		k.PartPoll, k.GateNoticeEvery = time.Millisecond, time.Hour
		k.KeepPlace, k.PartSlotWait, k.CallBudget = limits.Keep, limits.Wait, limits.Budget
		k.CallNow = clock.Now
		k.PartWorker = worker
		return k
	}
	starter := &clockedPart{clock: clock, runsFor: c.partRunsFor}
	starter.run = func() {
		said := &bytes.Buffer{}
		_, _ = build(true, said).Run(context.Background(), id)
		_, _ = vault.New(c.vault).PutRunFile(context.Background(), id, application.CheckPartOutFileName(1, c.partToCheck), said.String())
	}
	c.printed.Reset()
	k := build(false, &c.printed)
	k.PartStarter = starter
	c.checked, c.err = k.Run(context.Background(), id)
	return nil
}
