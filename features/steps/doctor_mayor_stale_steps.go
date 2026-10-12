package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"

	"github.com/cucumber/godog"
)

// staleFixture is the real mayor-stale check over a stand-in tmux whose pane
// is a file the steps write, and an alarm that only writes down what it was
// told. Its tmux log and alarms are kept in the mayor-stuck fixture, so the
// steps that read them ("the Mayor's window was closed", "the alarm says")
// serve both checks; the fixture's check stays nil.
// registerMayorStaleSteps registers the steps of the mayor-stale scenarios.
func (c *doctorContext) registerMayorStaleSteps(ctx *godog.ScenarioContext) {
	ctx.Given(`^a stand-in tmux showing the Mayor's pane from a file$`, c.aStandInTmuxShowingThePane)
	ctx.Given(`^the Mayor's pane shows a confirmation prompt with three choices$`, c.thePaneShowsAConfirmationPrompt)
	ctx.Given(`^the Mayor's pane shows a turn whose clock has stopped$`, c.thePaneShowsAFrozenTurn)

	ctx.When(`^mw doctor's mayor-stale check runs$`, func() error { return c.run(false) })
}

// staleFixture builds the check on the first call of a scenario, over the
// vault the .mayor-acting step made, and gives the pane's path.
func (c *doctorContext) staleFixture() (*doctor.MayorStale, string, error) {
	if c.stale != nil {
		return c.stale, c.stalePane, nil
	}
	vault, err := c.ensureMayorGoneVault()
	if err != nil {
		return nil, "", err
	}
	root, err := os.MkdirTemp("", "mw-doctor-mayor-stale")
	if err != nil {
		return nil, "", err
	}
	f := &mayorStuckFixture{vault: vault, tmuxLog: filepath.Join(root, "tmux-calls")}
	check := doctor.NewMayorStale(vault, c.state)
	check.Host = seatUpHost
	check.Limit = 15 * time.Minute
	check.Now = func() time.Time { return c.now }
	check.Settle = time.Millisecond
	check.Alarm = func(_ context.Context, text string) (uint64, error) {
		f.alarms = append(f.alarms, text)
		return uint64(len(f.alarms)), nil
	}
	check.Clear = func(context.Context, string, uint64) error { return nil }
	c.stuck, c.stale, c.stalePane, c.real = f, check, filepath.Join(root, "pane"), check
	return check, c.stalePane, nil
}

func (c *doctorContext) aStandInTmuxShowingThePane() error {
	check, pane, err := c.staleFixture()
	if err != nil {
		return err
	}
	program := filepath.Join(filepath.Dir(pane), "tmux-stand-in")
	script := fmt.Sprintf(`#!/bin/sh
echo "$*" >>%q
case "$1" in
list-windows) printf '@7|mayor-2026-10-11-333\n' ;;
list-panes) printf '0 claude\n' ;;
capture-pane) cat %q ;;
esac
exit 0
`, c.stuck.tmuxLog, pane)
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		return err
	}
	check.Tmux = program
	return nil
}

func (c *doctorContext) writeStalePane(text string) error {
	_, pane, err := c.staleFixture()
	if err != nil {
		return err
	}
	return os.WriteFile(pane, []byte(text), 0o644)
}

func (c *doctorContext) thePaneShowsAConfirmationPrompt() error {
	return c.writeStalePane("● Update(config.toml)\n  ⎿  Error: blocked by the classifier\n\n" +
		"Auto mode classifier requires confirmation for this action.\n" +
		"3 consecutive actions were blocked.\n" +
		"Do you want to proceed?\n" +
		" ❯ 1. Yes\n   2. Yes, allow reading from /home/jwhite/.config/mw\n   3. No\n")
}

func (c *doctorContext) thePaneShowsAFrozenTurn() error {
	return c.writeStalePane("● Working on it...\n\n✻ Pondering… (41s · esc to interrupt)\n\n❯ \n")
}
