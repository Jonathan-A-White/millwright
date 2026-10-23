package doctor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var (
	_ application.UnitSystem   = (*UserUnits)(nil)
	_ application.UnitFailures = (*UnitFailureFiles)(nil)
)

// UnitFailedLines is how many lines of a unit's journal a failure keeps and
// mails.
const UnitFailedLines = 20

// UserUnits is application.UnitSystem over this user's systemd, read through
// systemctl --user and journalctl --user.
type UserUnits struct {
	// Systemctl and Journalctl are the programs run; empty reads the name on PATH.
	Systemctl, Journalctl string
}

// RunByTimer implements application.UnitSystem: the unit's own timer, named
// after it (mw-dispatch.service is run by mw-dispatch.timer), is a unit
// systemd has loaded. A unit that is not a service has none.
func (s UserUnits) RunByTimer(ctx context.Context, unit string) (bool, error) {
	base, ok := strings.CutSuffix(unit, ".service")
	if !ok {
		return false, nil
	}
	out, err := exec.CommandContext(ctx, orDefault(s.Systemctl, "systemctl"),
		"--user", "show", base+".timer", "-p", "LoadState", "--value").Output()
	if err != nil {
		return false, fmt.Errorf("asking systemctl about %s.timer: %w", base, err)
	}
	return strings.TrimSpace(string(out)) == "loaded", nil
}

// JournalTail implements application.UnitSystem.
func (s UserUnits) JournalTail(ctx context.Context, unit string) (string, error) {
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, orDefault(s.Journalctl, "journalctl"),
		"--user", "-u", unit, "-n", strconv.Itoa(UnitFailedLines), "--no-pager")
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return strings.TrimRight(out.String(), "\n"), err
}

// RanCleanSince implements application.UnitSystem: the journal since then holds
// the line systemd logs when a oneshot service finishes without failing.
func (s UserUnits) RanCleanSince(ctx context.Context, unit string, since time.Time) (bool, error) {
	out, err := exec.CommandContext(ctx, orDefault(s.Journalctl, "journalctl"),
		"--user", "-u", unit, "--since", "@"+strconv.FormatInt(since.Unix(), 10), "--no-pager", "-o", "cat").Output()
	if err != nil {
		return false, fmt.Errorf("reading the journal of %s: %w", unit, err)
	}
	return journalShowsCleanRun(string(out), unit), nil
}

// journalShowsCleanRun says whether journal text (message lines only) holds
// systemd's word that unit ended without failing: "Finished <unit> - ...", or,
// from a systemd that logs it, "<unit>: Deactivated successfully." (older ones:
// "<unit>: Succeeded.").
func journalShowsCleanRun(text, unit string) bool {
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(line, "Finished "+unit):
			return true
		case strings.HasPrefix(line, unit+": Deactivated successfully"), strings.HasPrefix(line, unit+": Succeeded"):
			return true
		}
	}
	return false
}

func orDefault(program, fallback string) string {
	if program != "" {
		return program
	}
	return fallback
}

// UnitFailureFiles keeps each unit's first failure in a file of its own in Dir
// named for the unit: the failure's time on the first line, the journal's
// lines after it.
type UnitFailureFiles struct{ Dir string }

// Load implements application.UnitFailures. A file that cannot be read as a
// failure is an error, not a missing one.
func (f UnitFailureFiles) Load(_ context.Context, unit string) (application.UnitFailure, bool, error) {
	data, err := os.ReadFile(f.path(unit))
	if errors.Is(err, os.ErrNotExist) {
		return application.UnitFailure{}, false, nil
	}
	if err != nil {
		return application.UnitFailure{}, false, err
	}
	first, journal, _ := strings.Cut(string(data), "\n")
	at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(first))
	if err != nil {
		return application.UnitFailure{}, false, fmt.Errorf("%s does not start with a time: %w", f.path(unit), err)
	}
	return application.UnitFailure{At: at, Journal: journal}, true, nil
}

// Save implements application.UnitFailures.
func (f UnitFailureFiles) Save(_ context.Context, unit string, failure application.UnitFailure) error {
	if err := os.MkdirAll(f.Dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(f.path(unit), []byte(failure.At.UTC().Format(time.RFC3339Nano)+"\n"+failure.Journal), 0o644)
}

// Clear implements application.UnitFailures.
func (f UnitFailureFiles) Clear(_ context.Context, unit string) error {
	if err := os.Remove(f.path(unit)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (f UnitFailureFiles) path(unit string) string { return filepath.Join(f.Dir, unit) }
