package steps

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// seatTakeContext holds the vault a scenario's Mayor takes the seat in, the
// terminal it runs in and what the take answered. The acting file is the real
// vault's, and the probe at the end is the doctor's real mayor-gone check
// over a stand-in tmux that lists the fake terminal's windows.
type seatTakeContext struct {
	dir     string
	seat    string
	now     time.Time
	session string
	windows *apptest.FakeWindows

	out bytes.Buffer
	err error
}

// InitializeSeatTakeScenario registers the steps of features/seat_take.feature.
func InitializeSeatTakeScenario(ctx *godog.ScenarioContext) {
	c := &seatTakeContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = seatTakeContext{windows: apptest.NewFakeWindows()}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.dir != "" {
			_ = os.RemoveAll(c.dir)
		}
		return ctx, nil
	})

	ctx.Given(`^a vault for the "([^"]*)" seat taking by hand$`, c.aVaultForTheSeat)
	ctx.Given(`^the clock for taking reads "([^"]*)"$`, c.theClockReads)
	ctx.Given(`^a take window "([^"]*)" named "([^"]*)" that the command runs in$`, c.aWindowTheCommandRunsIn)
	ctx.Given(`^the session id is "([^"]*)"$`, c.theSessionIDIs)
	ctx.Given(`^the session id is not known$`, c.theSessionIDIsNotKnown)
	ctx.Given(`^the command runs in no window$`, c.theCommandRunsInNoWindow)
	ctx.Given(`^something else renames the take window "([^"]*)" to "([^"]*)"$`, c.somethingElseRenames)

	// A step of either kind: the Mayor takes the seat in a Given as well.
	ctx.Step(`^the Mayor takes the seat$`, func() error { return c.take("", "") })
	ctx.Step(`^the Mayor takes the seat as "([^"]*)"$`, func(name string) error { return c.take(name, "") })
	ctx.Step(`^the Mayor takes the seat as "([^"]*)" with the note "([^"]*)"$`, c.take)
	ctx.When(`^the mayor-gone probe looks$`, c.theProbeLooks)

	ctx.Then(`^the take succeeded$`, c.theTakeSucceeded)
	ctx.Then(`^the take was refused saying "([^"]*)"$`, c.theTakeWasRefused)
	ctx.Then(`^the take says "([^"]*)"$`, c.theTakeSays)
	ctx.Then(`^the take's acting file holds "([^"]*)"$`, c.theActingFileHolds)
	ctx.Then(`^the take's acting file is empty$`, c.theActingFileIsEmpty)
	ctx.Then(`^the take window "([^"]*)" is called "([^"]*)"$`, c.theWindowIsCalled)
	ctx.Then(`^the mayor-gone probe says (ok|faulty)$`, c.theProbeSays)
}

func (c *seatTakeContext) aVaultForTheSeat(seat string) error {
	dir, err := os.MkdirTemp("", "mw-seat-take-")
	if err != nil {
		return fmt.Errorf("making a vault: %w", err)
	}
	c.dir, c.seat = dir, seat
	return nil
}

func (c *seatTakeContext) theClockReads(when string) error {
	at, err := time.Parse(time.RFC3339, when)
	if err != nil {
		return fmt.Errorf("%q is not a time: %w", when, err)
	}
	c.now = at
	return nil
}

func (c *seatTakeContext) aWindowTheCommandRunsIn(id, name string) error {
	c.windows.HoldsWithID(id, name, c.now)
	c.windows.RunsIn(id)
	return nil
}

func (c *seatTakeContext) theSessionIDIs(id string) error {
	c.session = id
	return nil
}

func (c *seatTakeContext) theSessionIDIsNotKnown() error {
	c.session = ""
	return nil
}

func (c *seatTakeContext) theCommandRunsInNoWindow() error {
	c.windows.RunsIn("")
	return nil
}

func (c *seatTakeContext) somethingElseRenames(id, name string) error {
	return c.windows.Rename(context.Background(), id, name)
}

// take runs the use case; a refusal is kept for the Then that expects it.
func (c *seatTakeContext) take(name, note string) error {
	c.out.Reset()
	c.err = application.SeatTake{
		Seat:     c.seat,
		Terminal: c.windows,
		Acting:   vault.New(c.dir),
		Session:  c.session,
		Now:      func() time.Time { return c.now },
		Out:      &c.out,
	}.Run(context.Background(), application.SeatTakeRequest{Name: name, Note: note})
	return nil
}

func (c *seatTakeContext) theTakeSucceeded() error {
	if c.err != nil {
		return fmt.Errorf("expected the take to succeed, it failed: %v", c.err)
	}
	return nil
}

func (c *seatTakeContext) theTakeWasRefused(want string) error {
	if c.err == nil {
		return fmt.Errorf("expected the take to be refused, it printed %q", c.out.String())
	}
	if !strings.Contains(c.err.Error(), want) {
		return fmt.Errorf("expected the refusal to say %q, it says %q", want, c.err.Error())
	}
	return nil
}

func (c *seatTakeContext) theTakeSays(want string) error {
	if !strings.Contains(c.out.String(), want) {
		return fmt.Errorf("expected the take to say %q, it said %q", want, c.out.String())
	}
	return nil
}

func (c *seatTakeContext) actingFile() string {
	text, _ := os.ReadFile(filepath.Join(c.dir, application.ActingFileName(c.seat)))
	return string(text)
}

func (c *seatTakeContext) theActingFileHolds(want string) error {
	if got := c.actingFile(); !strings.Contains(got, want) {
		return fmt.Errorf("expected the acting file to hold %q, it holds %q", want, got)
	}
	return nil
}

func (c *seatTakeContext) theActingFileIsEmpty() error {
	if got := c.actingFile(); got != "" {
		return fmt.Errorf("expected no acting file, it holds %q", got)
	}
	return nil
}

func (c *seatTakeContext) theWindowIsCalled(id, want string) error {
	open, err := c.windows.OpenWindows(context.Background())
	if err != nil {
		return err
	}
	for _, w := range open {
		if w.ID == id {
			if w.Name != want {
				return fmt.Errorf("expected the window %s to be called %q, it is called %q", id, want, w.Name)
			}
			return nil
		}
	}
	return fmt.Errorf("no window %s is open", id)
}

// probe runs the doctor's real mayor-gone check over this scenario's vault,
// with a stand-in tmux that lists the fake terminal's windows, each with a
// live claude process in its pane.
func (c *seatTakeContext) probe() (application.Verdict, string, error) {
	if runtime.GOOS == "windows" {
		return application.DoctorFaulty, "", fmt.Errorf("the stand-in for tmux is a shell script; this scenario does not run on windows")
	}
	bin := filepath.Join(c.dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		return application.DoctorFaulty, "", err
	}
	if err := os.WriteFile(filepath.Join(bin, "mayor-up"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		return application.DoctorFaulty, "", err
	}

	open, err := c.windows.OpenWindows(context.Background())
	if err != nil {
		return application.DoctorFaulty, "", err
	}
	var listing strings.Builder
	for _, w := range open {
		fmt.Fprintf(&listing, "%s|%s\\n", w.ID, w.Name)
	}
	program := filepath.Join(c.dir, "tmux-stand-in")
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
list-windows) printf '%s' ;;
list-panes) if [ "$2" = "-t" ]; then printf '0 claude\n'; fi ;;
esac
exit 0
`, listing.String())
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		return application.DoctorFaulty, "", err
	}

	check := doctor.NewMayorGone(c.dir)
	check.Tmux = program
	check.PS = "true"
	verdict, why := check.Probe(context.Background())
	return verdict, why, nil
}

var probeVerdict = map[string]application.Verdict{"ok": application.DoctorOK, "faulty": application.DoctorFaulty}

func (c *seatTakeContext) theProbeLooks() error {
	_, _, err := c.probe()
	return err
}

func (c *seatTakeContext) theProbeSays(want string) error {
	verdict, why, err := c.probe()
	if err != nil {
		return err
	}
	if verdict != probeVerdict[want] {
		return fmt.Errorf("expected the mayor-gone probe to say %s, it said %s (%s); the acting file holds %q", want, verdict, why, c.actingFile())
	}
	return nil
}
