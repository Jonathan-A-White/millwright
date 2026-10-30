package doctor

import (
	"context"
	"fmt"
	"os/exec"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*UnitEnv)(nil)

// UnitEnvName is what the check is called: in the log, and on the command line
// as `mw doctor unit-env`.
const UnitEnvName = "unit-env"

// The unit-env check's damper: the alarm is sent again after a day, three times.
const (
	UnitEnvDamperWait = 24 * time.Hour
	UnitEnvDamperCap  = 3
)

// beadsEnvTail ends the path of ~/.config/mw/beads.env as `systemctl show` prints
// it, home expanded.
const beadsEnvTail = "/.config/mw/beads.env"

// unitEnvFix is the cure the alarm names, a person's to do.
const unitEnvFix = "Add `EnvironmentFile=-%h/.config/mw/beads.env` to the unit's file " +
	"(or `--property=EnvironmentFile=-<home>/.config/mw/beads.env` to the systemd-run that made it), then `systemctl --user daemon-reload`"

// UnitEnvLetOff are the user units that run mw and touch no tracker, so need no
// beads.env: the same names scripts/check-timer-units.sh lets off (NO_TRACKER).
var UnitEnvLetOff = []string{"mw-postern-mirror.service"}

// UnitEnv is the check that no user unit runs mw (or bd) without
// ~/.config/mw/beads.env (mw-gq6.849): bd reaches the home's served beads only
// through BEADS_DOLT_* in that file, so a unit without it fails on its first bd
// call (mw-resume-2026-10-29.service did, a transient unit mw pause made).
// scripts/check-timer-units.sh holds the installed unit files to it; this holds
// the units systemd has loaded or installed on this host, transient ones
// included. It is faulty naming each unit whose ExecStart runs mw, mw-<name> or
// bd and whose EnvironmentFiles name no beads.env. Its cure is the alarm to the
// Governor: the unit's file is a person's to edit.
type UnitEnv struct {
	// Program is the program run for systemctl. Empty reads "systemctl".
	Program string
	// LetOff are the units not asked; nil reads UnitEnvLetOff.
	LetOff []string
	// Alarm sends the Governor the text and gives the seq of the emergency
	// event it was written as, 0 for none. A nil Alarm cannot cure.
	Alarm func(ctx context.Context, text string) (uint64, error)
}

// NewUnitEnv is the check run through the real systemctl.
func NewUnitEnv() *UnitEnv { return &UnitEnv{} }

// Name implements application.DoctorCheck.
func (u *UnitEnv) Name() string { return UnitEnvName }

func (u *UnitEnv) run(ctx context.Context, args ...string) (string, error) {
	program := u.Program
	if program == "" {
		program = "systemctl"
	}
	out, err := exec.CommandContext(ctx, program, append([]string{"--user"}, args...)...).Output()
	return string(out), err
}

// units are the mw-*.service units this host has loaded or installed, sorted.
func (u *UnitEnv) units(ctx context.Context) ([]string, error) {
	seen := map[string]bool{}
	for _, args := range [][]string{
		{"list-units", "--all", "--type=service", "--plain", "--no-legend", "--no-pager"},
		{"list-unit-files", "--type=service", "--plain", "--no-legend", "--no-pager"},
	} {
		out, err := u.run(ctx, args...)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(out, "\n") {
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			// A template (mw-unit-failed@.service) runs nothing until it has an instance.
			if name := fields[0]; strings.HasPrefix(name, "mw-") && strings.HasSuffix(name, ".service") && !strings.HasSuffix(name, "@.service") {
				seen[name] = true
			}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// missing are the units, of those the host has, that run mw without beads.env.
func (u *UnitEnv) missing(ctx context.Context) (missing []string, cannotTell error) {
	names, err := u.units(ctx)
	if err != nil {
		if noUserManager(err) {
			return nil, nil
		}
		return nil, err
	}
	letOff := u.LetOff
	if letOff == nil {
		letOff = UnitEnvLetOff
	}
	for _, name := range names {
		if containsString(letOff, name) {
			continue
		}
		out, err := u.run(ctx, "show", "-p", "ExecStart", "-p", "EnvironmentFiles", name)
		if err != nil {
			cannotTell = fmt.Errorf("%s: %w", name, err)
			continue
		}
		if runsMw(out) && !carriesBeadsEnv(out) {
			missing = append(missing, name)
		}
	}
	return missing, cannotTell
}

func containsString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// runsMw reports whether `systemctl show -p ExecStart` output has a command
// that is mw, mw-<name> or bd, directly or as the word after `env`.
func runsMw(show string) bool {
	for _, line := range strings.Split(show, "\n") {
		if !strings.HasPrefix(line, "ExecStart=") {
			continue
		}
		_, rest, found := strings.Cut(line, "argv[]=")
		if !found {
			continue
		}
		argv, _, _ := strings.Cut(rest, " ; ")
		words := strings.Fields(argv)
		if len(words) > 0 && path.Base(words[0]) == "env" {
			words = words[1:]
			for len(words) > 0 && strings.Contains(words[0], "=") {
				words = words[1:]
			}
		}
		if len(words) == 0 {
			continue
		}
		if base := path.Base(words[0]); base == "mw" || base == "bd" || strings.HasPrefix(base, "mw-") {
			return true
		}
	}
	return false
}

// carriesBeadsEnv reports whether `systemctl show -p EnvironmentFiles` output
// names a beads.env under ~/.config/mw, optional or not.
func carriesBeadsEnv(show string) bool {
	for _, line := range strings.Split(show, "\n") {
		value, found := strings.CutPrefix(line, "EnvironmentFiles=")
		if !found {
			continue
		}
		file, _, _ := strings.Cut(strings.TrimSpace(value), " (")
		if strings.HasSuffix(file, beadsEnvTail) {
			return true
		}
	}
	return false
}

// Probe implements application.DoctorCheck. A host with no user manager reads
// ok: its units are not --user ones.
func (u *UnitEnv) Probe(ctx context.Context) (application.Verdict, string) {
	missing, cannotTell := u.missing(ctx)
	if len(missing) > 0 {
		return application.DoctorFaulty, alarmUnitEnv(missing)
	}
	if cannotTell != nil {
		return application.DoctorCannotTell, cannotTell.Error()
	}
	return application.DoctorOK, ""
}

func alarmUnitEnv(missing []string) string {
	return "Unit without beads.env: " + strings.Join(missing, ", ") + " runs mw and reads no ~/.config/mw/beads.env, so its bd cannot reach the served beads. " + unitEnvFix
}

// Cure implements application.DoctorCheck: send the alarm.
func (u *UnitEnv) Cure(ctx context.Context) error {
	missing, _ := u.missing(ctx)
	if len(missing) == 0 {
		return nil
	}
	if u.Alarm == nil {
		return fmt.Errorf("there is no way to send the unit-env alarm")
	}
	if _, err := u.Alarm(ctx, alarmUnitEnv(missing)); err != nil {
		return fmt.Errorf("the unit-env alarm could not be sent: %w", err)
	}
	return nil
}

// Damper implements application.DoctorCheck.
func (u *UnitEnv) Damper() (time.Duration, int) { return UnitEnvDamperWait, UnitEnvDamperCap }

// WayBack implements application.DoctorCheck.
func (u *UnitEnv) WayBack() string {
	return "none needed: the alarm only tells the Governor; add the EnvironmentFile line to the unit"
}
