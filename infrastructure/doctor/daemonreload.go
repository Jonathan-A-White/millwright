package doctor

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*DaemonReload)(nil)

// DaemonReloadName is what the check is called: in the log, and on the
// command line as `mw doctor daemon-reload`.
const DaemonReloadName = "daemon-reload"

// The daemon-reload check's damper: idle between two reloads, and how many it
// spends on one fault episode before it gives up and waits for the escalation
// note (a later story) to wake the Millhand.
const (
	DaemonReloadWait = 10 * time.Minute
	DaemonReloadCap  = 3
)

// DaemonReload is the check that keeps systemd --user's view of this rig's
// unit files current: it asks systemctl whether any of Units needs a reload,
// and, if so, runs one. Nothing it does ever starts, stops or restarts a
// unit.
type DaemonReload struct {
	// Units are the user units this check asks systemctl about.
	Units []string
	// Program is the program run for systemctl. Empty reads "systemctl".
	Program string
}

// NewDaemonReload is the check over the given units, run through the real
// systemctl.
func NewDaemonReload(units []string) *DaemonReload {
	return &DaemonReload{Units: units}
}

// Name implements application.DoctorCheck.
func (d *DaemonReload) Name() string { return DaemonReloadName }

// Probe implements application.DoctorCheck: `systemctl --user show <unit> -p
// NeedDaemonReload` for every unit. Faulty when any says yes; cannot-tell
// when systemctl itself could not be asked, or answered a unit with neither
// yes nor no — a unit systemd has never loaded, among them.
func (d *DaemonReload) Probe(ctx context.Context) (application.Verdict, string) {
	var needing, unclear []string
	for _, unit := range d.Units {
		out, err := d.run(ctx, "show", unit, "-p", "NeedDaemonReload")
		if err != nil {
			unclear = append(unclear, fmt.Sprintf("%s: %v", unit, err))
			continue
		}
		switch needDaemonReloadValue(out) {
		case "yes":
			needing = append(needing, unit)
		case "no":
		default:
			unclear = append(unclear, unit)
		}
	}
	if len(needing) > 0 {
		return application.DoctorFaulty, "needs a reload: " + strings.Join(needing, ", ")
	}
	if len(unclear) > 0 {
		return application.DoctorCannotTell, "systemctl did not say: " + strings.Join(unclear, ", ")
	}
	return application.DoctorOK, ""
}

// Cure implements application.DoctorCheck: `systemctl --user daemon-reload`.
func (d *DaemonReload) Cure(ctx context.Context) error {
	_, err := d.run(ctx, "daemon-reload")
	return err
}

// Damper implements application.DoctorCheck.
func (d *DaemonReload) Damper() (time.Duration, int) { return DaemonReloadWait, DaemonReloadCap }

// WayBack implements application.DoctorCheck: a reload only tells systemd to
// re-read unit files already on disk, so running it again, by hand or later,
// costs nothing.
func (d *DaemonReload) WayBack() string { return "none needed: daemon-reload is idempotent" }

func (d *DaemonReload) run(ctx context.Context, args ...string) (string, error) {
	program := d.Program
	if program == "" {
		program = "systemctl"
	}
	out, err := exec.CommandContext(ctx, program, append([]string{"--user"}, args...)...).Output()
	return string(out), err
}

// needDaemonReloadValue reads systemctl show's one line of output down to the
// value after "NeedDaemonReload=". Anything else — a blank line, a property
// with no value, output that is not that property at all — reads as "",
// which Probe treats as cannot-tell rather than guessing either way.
func needDaemonReloadValue(out string) string {
	line := strings.TrimSpace(out)
	key, value, found := strings.Cut(line, "=")
	if !found || key != "NeedDaemonReload" {
		return ""
	}
	return strings.TrimSpace(value)
}
