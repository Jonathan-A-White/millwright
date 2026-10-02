package steps

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"

	"github.com/cucumber/godog"
)

// boostReachFixture is the real boost-reach check over a fake ssh runner, a
// fake wg verdict and an alarm that only writes down what it was told.
type boostReachFixture struct {
	sshUp    bool
	wgFaulty bool
	ran      []string
	alarms   []string
}

// registerBoostReachSteps registers the steps of the boost-reach scenarios.
func (c *doctorContext) registerBoostReachSteps(ctx *godog.ScenarioContext) {
	ctx.Given(`^the home host's boost-reach check, reaching the Boost by "([^"]*)"$`, c.theHomeHostsBoostReachCheck)
	ctx.Given(`^the home host's boost-reach check, with no hands_hosts entry for the Boost$`, c.theHomeHostsBoostReachCheckWithNoEntry)
	ctx.Given(`^the boost-reach check run on the Boost itself, reaching the other by "([^"]*)"$`, c.theBoostReachCheckOnTheBoostItself)
	ctx.Given(`^the Boost does not answer ssh$`, c.theBoostDoesNotAnswerSSH)
	ctx.Given(`^the home's wg hub check is faulty$`, c.theHomesWgHubCheckIsFaulty)

	ctx.When(`^mw doctor's boost-reach check runs$`, c.mwDoctorsBoostReachCheckRuns)
	ctx.When(`^the Boost answers ssh$`, c.theBoostAnswersSSH)

	ctx.Then(`^no boost-reach alarm was sent$`, c.noBoostReachAlarmWasSent)
	ctx.Then(`^exactly (\d+) boost-reach alarms? (?:was|were) sent$`, c.exactlyNBoostReachAlarmsWereSent)
	ctx.Then(`^the boost-reach alarm says "([^"]*)"$`, c.theBoostReachAlarmSays)
	ctx.Then(`^the boost-reach check keeps nothing$`, c.theBoostReachCheckKeepsNothing)
	ctx.Then(`^the boost-reach probe ran "([^"]*)"$`, c.theBoostReachProbeRan)
}

// boostReachCheck builds the check on the first call of a scenario: this host
// is the given host, the home file names home, and reach is its [hands_hosts].
func (c *doctorContext) boostReachCheck(host, home string, reach map[string]string) {
	fixture := &boostReachFixture{sshUp: true}
	c.boost = fixture
	check := doctor.NewBoostReach(&apptest.FakeHomeFile{Text: home + " 2026-09-29T00:10:00Z mw@" + home}, host, reach, c.state)
	check.Now = func() time.Time { return c.now }
	check.Ssh = func(_ context.Context, argv []string) error {
		fixture.ran = append(fixture.ran, strings.Join(argv, " "))
		if fixture.sshUp {
			return nil
		}
		return errors.New("ssh: connect to host: Connection timed out")
	}
	check.WgFaulty = func(context.Context) bool { return fixture.wgFaulty }
	check.Alarm = func(_ context.Context, text string) error {
		fixture.alarms = append(fixture.alarms, text)
		return nil
	}
	c.real = check
}

func (c *doctorContext) theHomeHostsBoostReachCheck(prefix string) error {
	c.boostReachCheck("laptop", "laptop", map[string]string{"desktop": prefix})
	return nil
}

func (c *doctorContext) theHomeHostsBoostReachCheckWithNoEntry() error {
	c.boostReachCheck("laptop", "laptop", map[string]string{"laptop": "ssh laptop"})
	return nil
}

func (c *doctorContext) theBoostReachCheckOnTheBoostItself(prefix string) error {
	c.boostReachCheck("desktop", "laptop", map[string]string{"laptop": prefix})
	return nil
}

func (c *doctorContext) theBoostDoesNotAnswerSSH() error {
	c.boost.sshUp = false
	return nil
}

func (c *doctorContext) theBoostAnswersSSH() error {
	c.boost.sshUp = true
	return nil
}

func (c *doctorContext) theHomesWgHubCheckIsFaulty() error {
	c.boost.wgFaulty = true
	return nil
}

func (c *doctorContext) mwDoctorsBoostReachCheckRuns() error { return c.run(false) }

func (c *doctorContext) noBoostReachAlarmWasSent() error {
	return c.exactlyNBoostReachAlarmsWereSent("0")
}

func (c *doctorContext) exactlyNBoostReachAlarmsWereSent(wantText string) error {
	want, err := strconv.Atoi(wantText)
	if err != nil {
		return fmt.Errorf("parsing %q as a number of alarms: %w", wantText, err)
	}
	if got := len(c.boost.alarms); got != want {
		return fmt.Errorf("expected %d boost-reach alarm(s), got %d: %q", want, got, c.boost.alarms)
	}
	return nil
}

func (c *doctorContext) theBoostReachAlarmSays(substr string) error {
	for _, alarm := range c.boost.alarms {
		if strings.Contains(alarm, substr) {
			return nil
		}
	}
	return fmt.Errorf("expected a boost-reach alarm saying %q, got %q", substr, c.boost.alarms)
}

func (c *doctorContext) theBoostReachCheckKeepsNothing() error {
	episode, err := c.state.Load(context.Background(), doctor.BoostReachSeenStateName)
	if err != nil {
		return err
	}
	if !episode.FirstFaulty.IsZero() || len(episode.SeenPaths) != 0 {
		return fmt.Errorf("expected the boost-reach check to keep nothing, it keeps %+v", episode)
	}
	return nil
}

func (c *doctorContext) theBoostReachProbeRan(want string) error {
	for _, ran := range c.boost.ran {
		if ran == want {
			return nil
		}
	}
	return fmt.Errorf("expected the boost-reach probe to run %q, it ran %q", want, c.boost.ran)
}
