package steps

import (
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

// backgroundChecks stands in for the process mw check --background starts: each
// check it is asked to start runs in a goroutine of its own, its output going to
// the log it was given, as the detached process's does.
type backgroundChecks struct {
	c     *nextContext
	group sync.WaitGroup
}

func (b *backgroundChecks) Start(_ context.Context, storyID, logPath string) error {
	log, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	k := b.c.newCheck(rig.New(rig.WithProgram(b.c.gitProgram)), rig.NewSlots(), nil, log)
	b.group.Add(1)
	go func() {
		defer b.group.Done()
		defer log.Close()
		_, _ = k.Run(context.Background(), storyID)
	}()
	return nil
}

func registerBackgroundCheckSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the rig's tests wait until they are told to finish$`, c.theRigsTestsWait)
	ctx.When(`^a session starts checks of "([^"]*)" and "([^"]*)" in the background$`, c.aSessionStartsTwoChecksInTheBackground)
	ctx.When(`^a session starts a check of "([^"]*)" in the background$`, c.aSessionStartsACheckInTheBackground)
	ctx.When(`^the rig's tests are told to finish$`, c.theRigsTestsAreToldToFinish)
	ctx.When(`^the background checks finish$`, c.theBackgroundChecksFinish)
	ctx.Then(`^the log "([^"]*)" in the run of "([^"]*)" ends with the verdict that mw next would land it$`, c.theLogEndsWithLanding)
	ctx.Then(`^the log "([^"]*)" in the run of "([^"]*)" ends with the verdict that mw next would not land it$`, c.theLogEndsWithRefusal)
	ctx.Then(`^the log of "([^"]*)" never names "([^"]*)"$`, c.theLogNeverNames)
	ctx.Then(`^the command printed the path of the log "([^"]*)" in the run of "([^"]*)"$`, c.theCommandPrintedTheLogPath)
	ctx.Then(`^the command returned before the rig's tests finished$`, c.theCommandReturnedBeforeTheTests)
	ctx.Then(`^the log of "([^"]*)" has no verdict yet$`, c.theLogHasNoVerdict)
	ctx.Then(`^the log "([^"]*)" in the run of "([^"]*)" holds what the check printed$`, c.theLogHoldsWhatWasPrinted)
}

func (c *nextContext) releaseFile() string { return filepath.Join(c.root, "release-tests") }

func (c *nextContext) theRigsTestsWait() error {
	c.checkCommand = fmt.Sprintf("printf 'run\\n' >> %s; while [ ! -f %s ]; do sleep 0.05; done", c.checkLog, c.releaseFile())
	return nil
}

func (c *nextContext) theRigsTestsAreToldToFinish() error {
	return os.WriteFile(c.releaseFile(), nil, 0o644)
}

func (c *nextContext) aSessionStartsTwoChecksInTheBackground(first, second string) error {
	if err := c.aSessionStartsACheckInTheBackground(first); err != nil {
		return err
	}
	return c.aSessionStartsACheckInTheBackground(second)
}

func (c *nextContext) aSessionStartsACheckInTheBackground(id string) error {
	if c.background == nil {
		c.background = &backgroundChecks{c: c}
	}
	c.printed.Reset()
	_, err := application.CheckBackground{
		Tracker: c.tracker,
		Logs:    vault.New(c.vault),
		Starter: c.background,
		Out:     &c.printed,
	}.Run(context.Background(), id)
	return err
}

func (c *nextContext) theBackgroundChecksFinish() error {
	if c.background == nil {
		return fmt.Errorf("no check was started in the background in this scenario")
	}
	done := make(chan struct{})
	go func() { c.background.group.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-time.After(60 * time.Second):
		return fmt.Errorf("the background checks did not finish in a minute")
	}
}

func (c *nextContext) logOf(name, id string) (string, error) {
	held, err := os.ReadFile(filepath.Join(c.vault, vault.RunsDir, id, name))
	return string(held), err
}

func (c *nextContext) theLogEndsWithLanding(name, id string) error {
	return c.theLogEndsWith(name, id, "mw next would land it")
}

func (c *nextContext) theLogEndsWithRefusal(name, id string) error {
	return c.theLogEndsWith(name, id, "mw next would not land")
}

func (c *nextContext) theLogEndsWith(name, id, verdict string) error {
	log, err := c.logOf(name, id)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSpace(log), "\n")
	if len(lines) > 2 {
		lines = lines[len(lines)-2:]
	}
	if !strings.Contains(strings.Join(lines, "\n"), verdict) {
		return fmt.Errorf("expected the log of %s to end with %q, got:\n%s", id, verdict, log)
	}
	return nil
}

func (c *nextContext) theLogNeverNames(id, other string) error {
	log, err := c.logOf(application.CheckLogFileName(1), id)
	if err != nil {
		return err
	}
	if strings.Contains(log, other) {
		return fmt.Errorf("expected the log of %s not to name %s, got:\n%s", id, other, log)
	}
	return nil
}

func (c *nextContext) theCommandPrintedTheLogPath(name, id string) error {
	want := filepath.Join(c.vault, vault.RunsDir, id, name)
	if !strings.Contains(c.printed.String(), want) {
		return fmt.Errorf("expected the command to print %s, got:\n%s", want, c.printed.String())
	}
	return nil
}

func (c *nextContext) theCommandReturnedBeforeTheTests() error {
	// The tests are under way, and still waiting, when the command is back.
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if held, _ := os.ReadFile(c.checkLog); len(held) > 0 {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the rig's tests never started")
		}
	}
	if _, err := os.Stat(c.releaseFile()); err == nil {
		return fmt.Errorf("the rig's tests had been told to finish already")
	}
	return nil
}

func (c *nextContext) theLogHasNoVerdict(id string) error {
	log, err := c.logOf(application.CheckLogFileName(1), id)
	if err != nil {
		return err
	}
	if strings.Contains(log, "mw next would") {
		return fmt.Errorf("expected no verdict in the log yet, got:\n%s", log)
	}
	return nil
}

func (c *nextContext) theLogHoldsWhatWasPrinted(name, id string) error {
	log, err := c.logOf(name, id)
	if err != nil {
		return err
	}
	if log != c.printed.String() {
		return fmt.Errorf("expected the log to hold what the check printed:\nprinted:\n%s\nlog:\n%s", c.printed.String(), log)
	}
	return nil
}
