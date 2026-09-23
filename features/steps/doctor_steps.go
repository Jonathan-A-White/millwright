package steps

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"

	"github.com/cucumber/godog"
)

// doctorContext holds a table of fake checks (or, for the daemon-reload
// scenario, the real adapter over a fake systemctl) and a fake state and log.
// Nothing here reaches this host's own doctor state directory or a real
// systemctl.
type doctorContext struct {
	fakes []*apptest.FakeDoctorCheck
	order []string
	real  application.DoctorCheck

	state *apptest.FakeDoctorState
	log   *apptest.FakeDoctorLog
	now   time.Time

	systemctlCalls string

	out    bytes.Buffer
	report application.DoctorReport
	err    error
}

// InitializeDoctorScenario registers the steps of features/doctor.feature.
func InitializeDoctorScenario(ctx *godog.ScenarioContext) {
	c := &doctorContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = doctorContext{
			state: apptest.NewFakeDoctorState(),
			log:   &apptest.FakeDoctorLog{},
			now:   time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC),
		}
		return ctx, nil
	})

	ctx.Given(`^a doctor check "([^"]*)" whose probe says ok$`, c.aCheckWhoseProbeSaysOK)
	ctx.Given(`^a doctor check "([^"]*)" whose probe says faulty "([^"]*)"$`, c.aCheckWhoseProbeSaysFaulty)
	ctx.Given(`^the check "([^"]*)"'s way back is "([^"]*)"$`, c.theChecksWayBackIs)
	ctx.Given(`^the check "([^"]*)"'s damper is (\d+) minutes?, cap (\d+)$`, c.theChecksDamperIs)
	ctx.Given(`^the check "([^"]*)"'s cure fails, saying "([^"]*)"$`, c.theChecksCureFails)
	ctx.Given(`^a fake systemctl that says "([^"]*)" needs a reload$`, c.aFakeSystemctlThatSaysNeedsAReload)

	ctx.When(`^the check "([^"]*)"'s probe says ok$`, c.theChecksProbeSaysOK)
	ctx.When(`^the check "([^"]*)"'s probe says faulty "([^"]*)" again$`, c.theChecksProbeSaysFaultyAgain)
	ctx.When(`^mw doctor runs$`, c.mwDoctorRuns)
	ctx.When(`^mw doctor runs dry$`, c.mwDoctorRunsDry)
	ctx.When(`^mw doctor's daemon-reload check runs for real$`, c.mwDoctorsDaemonReloadCheckRunsForReal)
	ctx.When(`^(\d+) minutes? go(?:es)? by$`, c.minutesPass)
	ctx.When(`^(\d+) hours? go(?:es)? by$`, c.hoursPass)

	ctx.Then(`^mw doctor leaves with the status (\d+)$`, c.mwDoctorLeavesWith)
	ctx.Then(`^the doctor log holds "([^"]*)"$`, c.theDoctorLogHolds)
	ctx.Then(`^the doctor log is empty$`, c.theDoctorLogIsEmpty)
	ctx.Then(`^the check "([^"]*)" was not cured$`, c.theCheckWasNotCured)
	ctx.Then(`^the check "([^"]*)" was cured (\d+) times?$`, c.theCheckWasCuredNTimes)
	ctx.Then(`^mw doctor printed "([^"]*)"$`, c.mwDoctorPrinted)
	ctx.Then(`^systemctl was run with "([^"]*)"$`, c.systemctlWasRunWith)
}

func (c *doctorContext) fake(name string) *apptest.FakeDoctorCheck {
	for _, fake := range c.fakes {
		if fake.CheckName == name {
			return fake
		}
	}
	return nil
}

func (c *doctorContext) addFake(name string) *apptest.FakeDoctorCheck {
	if existing := c.fake(name); existing != nil {
		return existing
	}
	fake := &apptest.FakeDoctorCheck{CheckName: name, Wait: 10 * time.Minute, Cap: 3}
	c.fakes = append(c.fakes, fake)
	c.order = append(c.order, name)
	return fake
}

func (c *doctorContext) aCheckWhoseProbeSaysOK(name string) error {
	c.addFake(name).Verdict = application.DoctorOK
	return nil
}

func (c *doctorContext) aCheckWhoseProbeSaysFaulty(name, reason string) error {
	fake := c.addFake(name)
	fake.Verdict = application.DoctorFaulty
	fake.Reason = reason
	return nil
}

func (c *doctorContext) theChecksWayBackIs(name, way string) error {
	c.addFake(name).Way = way
	return nil
}

func (c *doctorContext) theChecksDamperIs(name, waitText, capText string) error {
	minutes, err := strconv.Atoi(waitText)
	if err != nil {
		return fmt.Errorf("parsing %q as minutes: %w", waitText, err)
	}
	capPerEpisode, err := strconv.Atoi(capText)
	if err != nil {
		return fmt.Errorf("parsing %q as a cap: %w", capText, err)
	}
	fake := c.addFake(name)
	fake.Wait = time.Duration(minutes) * time.Minute
	fake.Cap = capPerEpisode
	return nil
}

func (c *doctorContext) theChecksCureFails(name, saying string) error {
	c.addFake(name).CureErr = errors.New(saying)
	return nil
}

func (c *doctorContext) theChecksProbeSaysOK(name string) error {
	return c.aCheckWhoseProbeSaysOK(name)
}

func (c *doctorContext) theChecksProbeSaysFaultyAgain(name, reason string) error {
	return c.aCheckWhoseProbeSaysFaulty(name, reason)
}

func (c *doctorContext) checks() application.DoctorChecks {
	if c.real != nil {
		return application.DoctorChecks{c.real}
	}
	checks := make(application.DoctorChecks, len(c.fakes))
	for i, fake := range c.fakes {
		checks[i] = fake
	}
	return checks
}

func (c *doctorContext) run(dryRun bool) error {
	c.out.Reset()
	c.report, c.err = application.Doctor{
		Checks: c.checks(),
		State:  c.state,
		Log:    c.log,
		Now:    func() time.Time { return c.now },
		Out:    &c.out,
	}.Run(context.Background(), "", dryRun)
	return nil
}

func (c *doctorContext) mwDoctorRuns() error { return c.run(false) }

func (c *doctorContext) mwDoctorRunsDry() error { return c.run(true) }

func (c *doctorContext) mwDoctorsDaemonReloadCheckRunsForReal() error { return c.run(false) }

func (c *doctorContext) minutesPass(text string) error {
	minutes, err := strconv.Atoi(text)
	if err != nil {
		return fmt.Errorf("parsing %q as minutes: %w", text, err)
	}
	c.now = c.now.Add(time.Duration(minutes) * time.Minute)
	return nil
}

func (c *doctorContext) hoursPass(text string) error {
	hours, err := strconv.Atoi(text)
	if err != nil {
		return fmt.Errorf("parsing %q as hours: %w", text, err)
	}
	c.now = c.now.Add(time.Duration(hours) * time.Hour)
	return nil
}

func (c *doctorContext) mwDoctorLeavesWith(status int) error {
	if got := application.ExitStatus(c.err); got != status {
		return fmt.Errorf("expected mw doctor to leave with %d, it leaves with %d (%v)", status, got, c.err)
	}
	return nil
}

func (c *doctorContext) theDoctorLogHolds(substr string) error {
	for _, line := range c.log.Lines() {
		if strings.Contains(line, substr) {
			return nil
		}
	}
	return fmt.Errorf("expected the doctor log to hold %q, got %v", substr, c.log.Lines())
}

func (c *doctorContext) theDoctorLogIsEmpty() error {
	if lines := c.log.Lines(); len(lines) != 0 {
		return fmt.Errorf("expected the doctor log empty, got %v", lines)
	}
	return nil
}

func (c *doctorContext) theCheckWasNotCured(name string) error {
	return c.theCheckWasCuredNTimes(name, "0")
}

func (c *doctorContext) theCheckWasCuredNTimes(name, wantText string) error {
	want, err := strconv.Atoi(wantText)
	if err != nil {
		return fmt.Errorf("parsing %q as a number of cures: %w", wantText, err)
	}
	fake := c.fake(name)
	if fake == nil {
		return fmt.Errorf("no such doctor check in this scenario: %s", name)
	}
	if got := fake.Cures(); got != want {
		return fmt.Errorf("expected %s cured %d time(s), got %d", name, want, got)
	}
	return nil
}

func (c *doctorContext) mwDoctorPrinted(substr string) error {
	if !strings.Contains(c.out.String(), substr) {
		return fmt.Errorf("expected mw doctor to print %q, it printed:\n%s", substr, c.out.String())
	}
	return nil
}

// aFakeSystemctlThatSaysNeedsAReload writes a stand-in systemctl that answers
// `--user show <unit> -p NeedDaemonReload` yes for the named unit and no for
// any other, logs every call to a file this scenario reads back, and wires up
// the real infrastructure/doctor.DaemonReload check to run it.
func (c *doctorContext) aFakeSystemctlThatSaysNeedsAReload(unit string) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("the stand-in for systemctl is a shell script; this scenario does not run on windows")
	}
	dir, err := os.MkdirTemp("", "mw-doctor-systemctl")
	if err != nil {
		return err
	}
	program := filepath.Join(dir, "systemctl-stand-in")
	c.systemctlCalls = filepath.Join(dir, "calls")

	script := fmt.Sprintf(`#!/bin/sh
echo "$*" >>%q
if [ "$1" = --user ] && [ "$2" = daemon-reload ]; then
  exit 0
fi
if [ "$1" = --user ] && [ "$2" = show ]; then
  if [ "$3" = %q ]; then
    echo NeedDaemonReload=yes
  else
    echo NeedDaemonReload=no
  fi
  exit 0
fi
exit 1
`, c.systemctlCalls, unit)
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		return fmt.Errorf("writing the systemctl stand-in: %w", err)
	}

	c.real = doctor.NewDaemonReload([]string{unit})
	c.real.(*doctor.DaemonReload).Program = program
	return nil
}

func (c *doctorContext) systemctlWasRunWith(args string) error {
	if c.systemctlCalls == "" {
		return fmt.Errorf("no fake systemctl was set up in this scenario")
	}
	data, err := os.ReadFile(c.systemctlCalls)
	if err != nil {
		return fmt.Errorf("reading the systemctl call log: %w", err)
	}
	if !strings.Contains(string(data), args) {
		return fmt.Errorf("expected systemctl to have been called with %q, it was called with:\n%s", args, data)
	}
	return nil
}
