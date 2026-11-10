package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*UnitDrift)(nil)

// UnitDriftName is what the check is called: in the log, and on the command
// line as `mw doctor unit-drift`.
const UnitDriftName = "unit-drift"

// The unit-drift check's damper: it never cures, so one episode spends one.
const (
	UnitDriftDamperWait = 0
	UnitDriftDamperCap  = 1
)

// unitDriftNoCure begins what Cure always returns: installing a unit is a
// person's step, never the doctor's.
const unitDriftNoCure = "no cure: a person installs the rig's unit (its deploy/ README or install script), then `systemctl --user daemon-reload`"

// UnitDrift is the check that a systemd user unit installed on this host is the
// copy its rig ships (mw-gq6.1056): no step after a landing installs units, so
// a unit changed in deploy/<host>/<name> sits unread until a person installs it
// and the fix looks landed. It compares each Dir/<name> with every
// deploy/*/<name> of the rig checkouts on this host, comment lines and blank
// lines left out, and warns (cannot-tell) naming the rig, the unit and the
// lines that differ. A unit with no installed copy is nothing to compare. It
// reads files and changes nothing.
type UnitDrift struct {
	// UnitDir is where this host's user units are installed: ~/.config/systemd/user.
	UnitDir string
	// Rigs are the rig checkouts on this host, by rig name: config.Rigs.
	Rigs func() (map[string]string, error)
}

// Name implements application.DoctorCheck.
func (u *UnitDrift) Name() string { return UnitDriftName }

// Probe implements application.DoctorCheck: ok when every installed unit
// matches a copy its rig ships (or none is shipped), cannot-tell naming each
// drifted unit when one differs from all of them, or when the rigs or a file
// could not be read.
func (u *UnitDrift) Probe(context.Context) (application.Verdict, string) {
	if u.UnitDir == "" {
		return application.DoctorCannotTell, "cannot tell where this host's user units are installed"
	}
	rigs, err := u.Rigs()
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("cannot tell where the rigs are checked out: %v", err)
	}
	names := make([]string, 0, len(rigs))
	for name := range rigs {
		names = append(names, name)
	}
	sort.Strings(names)
	var found []string
	for _, rig := range names {
		copies, err := filepath.Glob(filepath.Join(rigs[rig], "deploy", "*", "*"))
		if err != nil {
			return application.DoctorCannotTell, fmt.Sprintf("cannot list %s's deploy/: %v", rig, err)
		}
		byUnit := map[string][]string{}
		var units []string
		for _, path := range copies {
			unit := filepath.Base(path)
			if _, seen := byUnit[unit]; !seen {
				units = append(units, unit)
			}
			byUnit[unit] = append(byUnit[unit], path)
		}
		sort.Strings(units)
		for _, unit := range units {
			line, err := u.drift(rig, unit, byUnit[unit])
			if err != nil {
				return application.DoctorCannotTell, err.Error()
			}
			if line != "" {
				found = append(found, line)
			}
		}
	}
	if len(found) == 0 {
		return application.DoctorOK, ""
	}
	return application.DoctorCannotTell, strings.Join(found, "; ")
}

// drift is the one line saying how the installed unit differs from the copy of
// the rig it differs least from, or "" when it matches one, is not installed,
// or is not a file.
func (u *UnitDrift) drift(rig, unit string, shipped []string) (string, error) {
	installed, err := os.ReadFile(filepath.Join(u.UnitDir, unit))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("cannot read the installed %s: %v", unit, err)
	}
	var best []string
	for _, path := range shipped {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("cannot read %s: %v", path, err)
		}
		diff := differingLines(string(body), string(installed))
		if len(diff) == 0 {
			return "", nil
		}
		if best == nil || len(diff) < len(best) {
			best = diff
		}
	}
	if best == nil {
		return "", nil
	}
	return fmt.Sprintf("%s's unit %s installed here differs from the copy it ships (%s); install it and daemon-reload", rig, unit, strings.Join(best, " | ")), nil
}

// differingLines are the lines, comments and blank lines left out, that one
// text has more of than the other: "ships <line>" for the shipped copy's,
// "installed <line>" for the installed copy's.
func differingLines(shipped, installed string) []string {
	count := map[string]int{}
	for _, line := range unitLines(shipped) {
		count[line]++
	}
	for _, line := range unitLines(installed) {
		count[line]--
	}
	var diff []string
	for _, line := range unitLines(shipped) {
		if count[line] > 0 {
			count[line]--
			diff = append(diff, "ships "+line)
		}
	}
	for _, line := range unitLines(installed) {
		if count[line] < 0 {
			count[line]++
			diff = append(diff, "installed "+line)
		}
	}
	return diff
}

// unitLines are a unit file's lines trimmed, with comment lines (# or ;) and
// blank lines dropped.
func unitLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// Cure implements application.DoctorCheck: there is none.
func (u *UnitDrift) Cure(context.Context) error { return errors.New(unitDriftNoCure) }

// Damper implements application.DoctorCheck.
func (u *UnitDrift) Damper() (time.Duration, int) { return UnitDriftDamperWait, UnitDriftDamperCap }

// WayBack implements application.DoctorCheck: nothing ever changes.
func (u *UnitDrift) WayBack() string {
	return "none: no cure runs; a person installs the unit by hand"
}
