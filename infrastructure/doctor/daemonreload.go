package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/userunits"
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

// DaemonReloadFix is the one line that mends a unit file changed on disk and
// not yet read by the user manager: the reason of a faulty probe ends with it.
const DaemonReloadFix = "fix: systemctl --user daemon-reload"

// DaemonReload is the check that keeps systemd --user's view of this rig's
// unit files current: it asks systemctl whether any of Units, or any other
// mw-* user unit the manager has loaded (mw-gq6.565), needs a reload, and, if
// so, runs one. Nothing it does ever starts, stops or restarts a unit.
type DaemonReload struct {
	// Units are the user units this check asks systemctl about.
	Units []string
	// Program is the program run for systemctl. Empty reads "systemctl".
	Program string
	// Root says whether mw runs as root, in which case the system manager's
	// mw-* units are checked too (mw-gq6.1035). Nil reads the effective user id.
	Root func() bool
}

// DaemonReloadSystemFix is the one line that mends a system unit file changed on
// disk and not yet read by the system manager, on a root host.
const DaemonReloadSystemFix = "fix: systemctl daemon-reload"

func (d *DaemonReload) isRoot() bool {
	if d.Root != nil {
		return d.Root()
	}
	return os.Geteuid() == 0
}

// NewDaemonReload is the check over the given units, run through the real
// systemctl.
func NewDaemonReload(units []string) *DaemonReload {
	return &DaemonReload{Units: units}
}

// Name implements application.DoctorCheck.
func (d *DaemonReload) Name() string { return DaemonReloadName }

// Probe implements application.DoctorCheck: `systemctl --user show <unit> -p
// NeedDaemonReload` for every unit installed on this host — a unit with no
// unit file at all is skipped rather than asked. Faulty when any installed
// unit says yes; cannot-tell when systemctl itself could not be asked, or
// answered a unit with neither yes nor no. When every unit in the list is
// skipped, ok, with a reason saying so, rather than faulty or cannot-tell: a
// host that simply does not run any of them. The same, immediately, when
// systemctl cannot reach a user manager to ask at all — a system-scope
// install rather than a fault — since nothing it might otherwise have said
// about any other unit can be trusted either.
func (d *DaemonReload) Probe(ctx context.Context) (application.Verdict, string) {
	verdict, reason := d.probeUser(ctx)
	if !d.isRoot() {
		return verdict, reason
	}
	// A root host's units are system units: the user manager's answer (usually
	// n/a, no user manager) says nothing of them.
	stale, err := userunits.Systemctl{Bin: d.Program, SystemScope: true}.StaleUnits(ctx)
	if err != nil || len(stale) == 0 {
		return verdict, reason
	}
	system := "system units need a reload: " + strings.Join(stale, ", ") + "; " + DaemonReloadSystemFix
	if verdict == application.DoctorFaulty {
		return verdict, reason + "; " + system
	}
	return application.DoctorFaulty, system
}

func (d *DaemonReload) probeUser(ctx context.Context) (application.Verdict, string) {
	var needing, unclear []string
	skipped := 0
	for _, unit := range d.Units {
		skip, busReason := d.notInstalled(ctx, unit)
		if busReason != "" {
			return application.DoctorOK, busReason
		}
		if skip {
			skipped++
			continue
		}
		out, err := d.run(ctx, "show", unit, "-p", "NeedDaemonReload")
		if err != nil {
			if noUserManager(err) {
				return application.DoctorOK, noUserManagerReason
			}
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
	needing = d.withLoadedMwUnits(ctx, needing)
	if len(needing) > 0 {
		return application.DoctorFaulty, "needs a reload: " + strings.Join(needing, ", ") + "; " + DaemonReloadFix
	}
	if len(unclear) > 0 {
		return application.DoctorCannotTell, "systemctl did not say: " + strings.Join(unclear, ", ")
	}
	if skipped > 0 && skipped == len(d.Units) {
		return application.DoctorOK, notInstalledReason
	}
	return application.DoctorOK, ""
}

// withLoadedMwUnits adds to needing every other mw-* user unit the manager has
// loaded that needs a reload, so a unit file the [doctor] table does not list
// (a timer, the mirror, a newly shipped service) is alarmed on all the same. A
// listing that fails adds none: the units asked about one by one have spoken.
func (d *DaemonReload) withLoadedMwUnits(ctx context.Context, needing []string) []string {
	stale, err := userunits.Systemctl{Bin: d.Program}.StaleUnits(ctx)
	if err != nil {
		return needing
	}
	for _, unit := range stale {
		seen := false
		for _, have := range needing {
			seen = seen || have == unit
		}
		if !seen {
			needing = append(needing, unit)
		}
	}
	return needing
}

// notInstalled reports whether unit has no unit file on this host at all:
// `systemctl --user show <unit> -p LoadState` saying "not-found", or failing
// the way systemctl does for one. A genuine failure to ask at all is not
// skipped here: it falls through to the NeedDaemonReload query below, which
// fails the same way and is reported cannot-tell. If instead systemctl cannot
// reach a user manager's bus at all, busReason names the reason the whole
// check should answer ok with, rather than treating this one unit specially.
func (d *DaemonReload) notInstalled(ctx context.Context, unit string) (skip bool, busReason string) {
	out, err := d.run(ctx, "show", unit, "-p", "LoadState")
	if err != nil {
		if noUserManager(err) {
			return false, noUserManagerReason
		}
		return unitNotFound(err), ""
	}
	return loadStateValue(out) == "not-found", ""
}

// Cure implements application.DoctorCheck: `systemctl --user daemon-reload`,
// and on a root host `systemctl daemon-reload` too, a missing user manager
// there being no failure.
func (d *DaemonReload) Cure(ctx context.Context) error {
	_, userErr := d.run(ctx, "daemon-reload")
	if !d.isRoot() {
		return userErr
	}
	if _, err := exec.CommandContext(ctx, d.program(), "daemon-reload").Output(); err != nil {
		return err
	}
	if userErr != nil && !noUserManager(userErr) {
		return userErr
	}
	return nil
}

// Damper implements application.DoctorCheck.
func (d *DaemonReload) Damper() (time.Duration, int) { return DaemonReloadWait, DaemonReloadCap }

// WayBack implements application.DoctorCheck: a reload only tells systemd to
// re-read unit files already on disk, so running it again, by hand or later,
// costs nothing.
func (d *DaemonReload) WayBack() string { return "none needed: daemon-reload is idempotent" }

func (d *DaemonReload) program() string {
	if d.Program == "" {
		return "systemctl"
	}
	return d.Program
}

func (d *DaemonReload) run(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, d.program(), append([]string{"--user"}, args...)...).Output()
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

// loadStateValue reads systemctl show's one line of output down to the value
// after "LoadState=", the same way needDaemonReloadValue reads
// "NeedDaemonReload=".
func loadStateValue(out string) string {
	line := strings.TrimSpace(out)
	key, value, found := strings.Cut(line, "=")
	if !found || key != "LoadState" {
		return ""
	}
	return strings.TrimSpace(value)
}
