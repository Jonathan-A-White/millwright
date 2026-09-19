package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// The host these scenarios run on, and the text each of the seat's files
// holds, so that a scenario can say which of them reached the session and
// which did not.
const (
	seatUpHost        = "laptop"
	seatUpCharter     = "# Mayor — charter\n\nYou are the Mayor of millwright, and the Governor talks to you.\n"
	seatUpLedger      = "2026-09-01 mw-old was worked and closed here.\n"
	seatUpVision      = "# Vision\n\nwhere this factory is going.\n"
	seatUpRigMemory   = "# Mayor's memory: rig millwright\n\nwhat the mayor knows about millwright.\n"
	seatUpHandoffBody = "What the last session left for the next one.\n"
)

// seatUpFirstHandoff is when the first handoff of a scenario is written; each
// one after it is a minute later, and the window a scenario seeds is opened
// well after all of them.
var seatUpFirstHandoff = time.Date(2026, 9, 19, 6, 0, 0, 0, time.UTC)

// seatUpContext holds the vault a scenario writes its seat into, the terminal
// the seat's window would be opened in, and what starting it produced.
type seatUpContext struct {
	dir     string // the vault
	windows *apptest.FakeWindows
	today   time.Time
	// written is how many handoffs have been written, so that each is newer
	// than the one before it.
	written int
	// lastSeeded is the window a scenario seeded last, for the acting file.
	lastSeeded string

	report application.SeatUpReport
	err    error
}

// InitializeSeatUpScenario registers the steps of features/seat_up.feature.
func InitializeSeatUpScenario(ctx *godog.ScenarioContext) {
	c := &seatUpContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = seatUpContext{windows: apptest.NewFakeWindows(), today: time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.dir != "" {
			_ = os.RemoveAll(c.dir)
		}
		return ctx, nil
	})

	ctx.Given(`^a vault holding the "([^"]*)" seat$`, c.aVaultHoldingTheSeat)
	ctx.Given(`^the "([^"]*)" seat's charter$`, c.theSeatsCharter)
	ctx.Given(`^the "([^"]*)" seat has written the handoffs:$`, c.theSeatHasWrittenTheHandoffs)
	ctx.Given(`^the "([^"]*)" seat also holds a ledger, a vision and a memory of a rig$`, c.theSeatAlsoHolds)
	ctx.Given(`^today is "([^"]*)"$`, c.todayIs)
	ctx.Given(`^the "([^"]*)" seat holds the kickoff text "([^"]*)"$`, c.theSeatHoldsTheKickoffText)
	ctx.Given(`^the "([^"]*)" seat keeps its handoffs on this host, and has written:$`, c.theSeatKeepsItsHandoffsOnThisHost)
	ctx.Given(`^the "([^"]*)" seat has no charter$`, c.theSeatHasNoCharter)
	ctx.Given(`^the "([^"]*)" seat has written no handoff$`, c.theSeatHasWrittenNoHandoff)
	ctx.Given(`^the window "([^"]*)" was opened at "([^"]*)"$`, c.theWindowWasOpenedAt)
	ctx.Given(`^the "([^"]*)" seat's acting file names that window$`, c.theActingFileNamesThatWindow)
	ctx.Given(`^the "([^"]*)" seat's acting file names the window "([^"]*)"$`, c.theActingFileNamesTheWindow)
	ctx.Given(`^the newest handoff was written at "([^"]*)"$`, c.theNewestHandoffWasWrittenAt)

	ctx.When(`^mw seat up starts the "([^"]*)" seat$`, c.mwSeatUpStartsTheSeat)
	ctx.When(`^mw seat up starts the "([^"]*)" seat on "([^"]*)" at "([^"]*)" for the reason "([^"]*)"$`, c.mwSeatUpStartsTheSeatAt)

	ctx.Then(`^seat up succeeds$`, c.seatUpSucceeds)
	ctx.Then(`^exactly one window was opened$`, c.exactlyOneWindowWasOpened)
	ctx.Then(`^no window was opened$`, c.noWindowWasOpened)
	ctx.Then(`^the window is named "([^"]*)"$`, c.theWindowIsNamed)
	ctx.Then(`^seat up says it started the seat in the window "([^"]*)"$`, c.seatUpSaysItStartedTheSeatIn)
	ctx.Then(`^the window's command carries:$`, c.theWindowsCommandCarries)
	ctx.Then(`^the window's command holds none of:$`, c.theWindowsCommandHoldsNoneOf)
	ctx.Then(`^the window's command primes the session from the vault's "([^"]*)"$`, c.theWindowsCommandPrimesFrom)
	ctx.Then(`^the window runs in the vault$`, c.theWindowRunsInTheVault)
	ctx.Then(`^the window's environment holds:$`, c.theWindowsEnvironmentHolds)
	ctx.Then(`^the kickoff prompt of the window holds:$`, c.theKickoffOfTheWindowHolds)
	ctx.Then(`^the kickoff prompt of the window holds none of:$`, c.theKickoffOfTheWindowHoldsNoneOf)
	ctx.Then(`^seat up is refused saying the seat has no charter$`, c.refusedForNoCharter)
	ctx.Then(`^seat up is refused saying the seat has no handoff$`, c.refusedForNoHandoff)
	ctx.Then(`^seat up is refused saying the seat is already acting in "([^"]*)"$`, c.refusedForBeingHeld)
}

// write puts one file in the vault, making the directories above it.
func (c *seatUpContext) writeSeatFile(contents string, parts ...string) error {
	path := filepath.Join(append([]string{c.dir}, parts...)...)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("making %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// writeHandoffs writes one handoff a row, each a minute newer than the one
// before it, in the directory given.
func (c *seatUpContext) writeHandoffs(table *godog.Table, parts ...string) error {
	for _, row := range table.Rows {
		name := strings.TrimSpace(row.Cells[0].Value)
		at := append(append([]string{}, parts...), name+".md")
		if err := c.writeSeatFile(seatUpHandoffBody, at...); err != nil {
			return err
		}
		written := seatUpFirstHandoff.Add(time.Duration(c.written) * time.Minute)
		c.written++
		path := filepath.Join(append([]string{c.dir}, at...)...)
		if err := os.Chtimes(path, written, written); err != nil {
			return err
		}
	}
	return nil
}

func (c *seatUpContext) aVaultHoldingTheSeat(seat string) error {
	dir, err := os.MkdirTemp("", "mw-seat-up-")
	if err != nil {
		return fmt.Errorf("making a vault: %w", err)
	}
	c.dir = dir
	return os.MkdirAll(filepath.Join(dir, "seats", seat), 0o755)
}

func (c *seatUpContext) theSeatsCharter(seat string) error {
	return c.writeSeatFile(seatUpCharter, "seats", seat, "charter.md")
}

func (c *seatUpContext) theSeatHasWrittenTheHandoffs(seat string, table *godog.Table) error {
	return c.writeHandoffs(table, "seats", seat, "handoffs")
}

func (c *seatUpContext) theSeatAlsoHolds(seat string) error {
	if err := c.writeSeatFile(seatUpLedger, "seats", seat, "ledger.md"); err != nil {
		return err
	}
	if err := c.writeSeatFile(seatUpVision, "seats", seat, "vision.md"); err != nil {
		return err
	}
	return c.writeSeatFile(seatUpRigMemory, "seats", seat, "rigs", "millwright.md")
}

func (c *seatUpContext) todayIs(day string) error {
	today, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return fmt.Errorf("%q is not a date: %w", day, err)
	}
	c.today = today.Add(12 * time.Hour)
	return nil
}

func (c *seatUpContext) theSeatHoldsTheKickoffText(seat, text string) error {
	return c.writeSeatFile(text+"\n", "seats", seat, "kickoff.md")
}

func (c *seatUpContext) theSeatKeepsItsHandoffsOnThisHost(seat string, table *godog.Table) error {
	return c.writeHandoffs(table, "seats", seat, "hosts", seatUpHost, "handoffs")
}

func (c *seatUpContext) theSeatHasNoCharter(seat string) error {
	return os.Remove(filepath.Join(c.dir, "seats", seat, "charter.md"))
}

func (c *seatUpContext) theSeatHasWrittenNoHandoff(seat string) error {
	return os.RemoveAll(filepath.Join(c.dir, "seats", seat, "handoffs"))
}

func (c *seatUpContext) theWindowWasOpenedAt(window, when string) error {
	opened, err := time.Parse(time.RFC3339, when)
	if err != nil {
		return fmt.Errorf("%q is not a time: %w", when, err)
	}
	c.windows.Holds(window, opened)
	c.lastSeeded = window
	return nil
}

// theActingFileNamesThatWindow writes the acting file as a session writes it:
// a line in its own words, with the window's name somewhere in it.
func (c *seatUpContext) theActingFileNamesThatWindow(seat string) error {
	if c.lastSeeded == "" {
		return fmt.Errorf("no window has been opened for the acting file to name")
	}
	return c.theActingFileNamesTheWindow(seat, c.lastSeeded)
}

func (c *seatUpContext) theActingFileNamesTheWindow(seat, window string) error {
	acting := fmt.Sprintf("%s after handoff 02 (tmux window 3 '%s'), since 2026-09-19T08:00:00Z\n", seat, window)
	return c.writeSeatFile(acting, application.ActingFileName(seat))
}

func (c *seatUpContext) theNewestHandoffWasWrittenAt(when string) error {
	written, err := time.Parse(time.RFC3339, when)
	if err != nil {
		return fmt.Errorf("%q is not a time: %w", when, err)
	}
	start, err := c.seatFiles().SeatStart(context.Background(), "mayor", seatUpHost)
	if err != nil {
		return err
	}
	if len(start.Handoffs) == 0 {
		return fmt.Errorf("the seat has written no handoff to date")
	}
	newest := filepath.Join(c.dir, start.Handoffs[len(start.Handoffs)-1].Path)
	return os.Chtimes(newest, written, written)
}

// seatFiles is the real vault adapter on the scenario's own vault.
func (c *seatUpContext) seatFiles() application.SeatFiles {
	return vault.New(c.dir)
}

func (c *seatUpContext) mwSeatUpStartsTheSeat(seat string) error {
	return c.startTheSeat(seat, "", "", "")
}

func (c *seatUpContext) mwSeatUpStartsTheSeatAt(seat, model, effort, reason string) error {
	return c.startTheSeat(seat, model, effort, reason)
}

// startTheSeat runs the use case with the real vault and the real Claude Code
// harness, and only the terminal faked: what a window would be opened with is
// exactly what mw would run.
func (c *seatUpContext) startTheSeat(seat, model, effort, reason string) error {
	c.report, c.err = application.SeatUp{
		Seats:   c.seatFiles(),
		Windows: c.windows,
		Harness: claude.New(),
		Seat:    seat,
		Host:    seatUpHost,
		Model:   domain.Model(model),
		Effort:  domain.Effort(effort),
		Reason:  reason,
		Now:     func() time.Time { return c.today },
	}.Run(context.Background())
	return nil
}

// opened is the one window a scenario started, or an error saying what was
// started instead.
func (c *seatUpContext) opened() (application.WindowSpec, error) {
	if c.err != nil {
		return application.WindowSpec{}, fmt.Errorf("expected a window to have been opened, seat up failed: %w", c.err)
	}
	if len(c.windows.Opened) != 1 {
		return application.WindowSpec{}, fmt.Errorf("expected one window to have been opened, %d were", len(c.windows.Opened))
	}
	return c.windows.Opened[0], nil
}

// kickoff is the first thing the session was told: the last word of its
// command line.
func (c *seatUpContext) kickoff() (string, error) {
	spec, err := c.opened()
	if err != nil {
		return "", err
	}
	return spec.Command[len(spec.Command)-1], nil
}

func (c *seatUpContext) seatUpSucceeds() error {
	if c.err != nil {
		return fmt.Errorf("expected seat up to succeed, got: %w", c.err)
	}
	return nil
}

func (c *seatUpContext) exactlyOneWindowWasOpened() error {
	_, err := c.opened()
	return err
}

func (c *seatUpContext) noWindowWasOpened() error {
	if len(c.windows.Opened) != 0 {
		return fmt.Errorf("expected nothing to have been opened, got %d windows: %+v", len(c.windows.Opened), c.windows.Opened)
	}
	return nil
}

func (c *seatUpContext) theWindowIsNamed(name string) error {
	spec, err := c.opened()
	if err != nil {
		return err
	}
	if spec.Name != name {
		return fmt.Errorf("expected the window to be named %q, got %q", name, spec.Name)
	}
	return nil
}

func (c *seatUpContext) seatUpSaysItStartedTheSeatIn(window string) error {
	if c.err != nil {
		return fmt.Errorf("expected a line naming %s, got: %w", window, c.err)
	}
	said := c.report.String()
	if !strings.Contains(said, window) {
		return fmt.Errorf("expected the line to name the window %s, got %q", window, said)
	}
	return nil
}

func (c *seatUpContext) theWindowsCommandCarries(table *godog.Table) error {
	spec, err := c.opened()
	if err != nil {
		return err
	}
	line := strings.Join(spec.Command, " ")
	for _, row := range table.Rows {
		want := strings.TrimSpace(row.Cells[0].Value)
		if !strings.Contains(line, want) {
			return fmt.Errorf("expected the command to carry %q, got %q", want, line)
		}
	}
	return nil
}

func (c *seatUpContext) theWindowsCommandHoldsNoneOf(table *godog.Table) error {
	spec, err := c.opened()
	if err != nil {
		return err
	}
	line := strings.Join(spec.Command, " ")
	for _, row := range table.Rows {
		unwanted := strings.TrimSpace(row.Cells[0].Value)
		if strings.Contains(line, unwanted) {
			return fmt.Errorf("expected the command to hold nothing of %q, it holds it: %q", unwanted, line)
		}
	}
	return nil
}

func (c *seatUpContext) theWindowsCommandPrimesFrom(file string) error {
	spec, err := c.opened()
	if err != nil {
		return err
	}
	want := filepath.Join(c.dir, filepath.FromSlash(file))
	for i, word := range spec.Command {
		if word != "--append-system-prompt-file" {
			continue
		}
		if i+1 < len(spec.Command) && spec.Command[i+1] == want {
			return nil
		}
		return fmt.Errorf("expected the session to be primed from %s, the command primes it from %q", want, spec.Command[i+1:])
	}
	return fmt.Errorf("expected the session to be primed from the file %s, the command is %q", want, strings.Join(spec.Command, " "))
}

func (c *seatUpContext) theWindowRunsInTheVault() error {
	spec, err := c.opened()
	if err != nil {
		return err
	}
	if spec.Dir != c.dir {
		return fmt.Errorf("expected the window to run in %s, it runs in %q", c.dir, spec.Dir)
	}
	return nil
}

func (c *seatUpContext) theWindowsEnvironmentHolds(table *godog.Table) error {
	spec, err := c.opened()
	if err != nil {
		return err
	}
	for _, row := range table.Rows {
		key, want := strings.TrimSpace(row.Cells[0].Value), strings.TrimSpace(row.Cells[1].Value)
		if got := spec.Env[key]; got != want {
			return fmt.Errorf("expected %s to be %q in the window's environment, got %q", key, want, got)
		}
	}
	return nil
}

func (c *seatUpContext) theKickoffOfTheWindowHolds(table *godog.Table) error {
	told, err := c.kickoff()
	if err != nil {
		return err
	}
	for _, row := range table.Rows {
		want := strings.TrimSpace(row.Cells[0].Value)
		if !strings.Contains(told, want) {
			return fmt.Errorf("expected the kickoff to hold %q, got %q", want, told)
		}
	}
	return nil
}

func (c *seatUpContext) theKickoffOfTheWindowHoldsNoneOf(table *godog.Table) error {
	told, err := c.kickoff()
	if err != nil {
		return err
	}
	for _, row := range table.Rows {
		unwanted := strings.TrimSpace(row.Cells[0].Value)
		if strings.Contains(told, unwanted) {
			return fmt.Errorf("expected the kickoff to hold nothing of %q, it holds it: %q", unwanted, told)
		}
	}
	return nil
}

// refused is the refusal a scenario expected, or an error saying what happened
// instead.
func (c *seatUpContext) refused(what string) (string, error) {
	if c.err == nil {
		return "", fmt.Errorf("expected seat up to be refused for %s, it said %q", what, c.report.String())
	}
	if lines := strings.Split(strings.TrimSpace(c.err.Error()), "\n"); len(lines) != 1 {
		return "", fmt.Errorf("expected the refusal to be one plain line, got %d: %q", len(lines), c.err.Error())
	}
	return c.err.Error(), nil
}

func (c *seatUpContext) refusedForNoCharter() error {
	said, err := c.refused("having no charter")
	if err != nil {
		return err
	}
	if !strings.Contains(said, "charter") {
		return fmt.Errorf("expected the refusal to say there is no charter, got %q", said)
	}
	return nil
}

func (c *seatUpContext) refusedForNoHandoff() error {
	said, err := c.refused("having written no handoff")
	if err != nil {
		return err
	}
	if !strings.Contains(said, "handoff") {
		return fmt.Errorf("expected the refusal to say there is no handoff, got %q", said)
	}
	return nil
}

func (c *seatUpContext) refusedForBeingHeld(window string) error {
	said, err := c.refused("being held already")
	if err != nil {
		return err
	}
	if !strings.Contains(said, window) {
		return fmt.Errorf("expected the refusal to name the window %s, got %q", window, said)
	}
	return nil
}
