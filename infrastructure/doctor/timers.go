package doctor

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*Timers)(nil)

// TimersName is what the check is called: in the log, and on the command
// line as `mw doctor timers`.
const TimersName = "timers"

// The timers check's damper: idle between two cures, and how many it spends
// on one fault episode before it gives up and waits for the escalation note
// to wake the Millhand.
const (
	TimersDamperWait = 30 * time.Minute
	TimersDamperCap  = 3
)

// Timers is the check that starts a factory timer this rig's own machinery
// has left stopped: the hosts watch each other only through their timers, so
// one an edit, a daemon-reload or a hand `systemctl --user stop` left
// inactive is silent — nothing dispatches, nobody is woken. For every unit in
// Units it asks systemctl whether the matching timer is enabled at all — a
// timer not installed on this host, e.g. mw-millhand-review's on a host
// without the review, is skipped rather than faulted — and, only for a timer
// that is enabled, whether it is active. It never touches a timer that is not
// enabled here, and never stops or restarts one: only ever `--user start`.
type Timers struct {
	// Units are the service units this check derives its timers from: for
	// "<name>.service" it asks systemctl about "<name>.timer".
	Units []string
	// Program is the program run for systemctl. Empty reads "systemctl".
	Program string

	// started is what the last Cure actually started, read by WayBack once
	// Cure has run. Empty before any cure runs in this process.
	started []string
}

// NewTimers is the check over the given service units, run through the real
// systemctl.
func NewTimers(units []string) *Timers {
	return &Timers{Units: units}
}

// Name implements application.DoctorCheck.
func (t *Timers) Name() string { return TimersName }

// Probe implements application.DoctorCheck: `systemctl --user is-enabled
// <timer>` for every timer derived from Units, skipping any that is not
// enabled here; `systemctl --user is-active <timer>` for the rest, faulty
// naming every one that is enabled but not active. cannot-tell when systemctl
// itself could not be run at all — no systemctl on PATH, no user manager —
// rather than a plain "not enabled" or "not active" answer. When every timer
// in the list has no unit file on this host at all, ok, with a reason saying
// so, rather than faulty or cannot-tell.
func (t *Timers) Probe(ctx context.Context) (application.Verdict, string) {
	faulty, allSkipped, cannotTell := t.faulty(ctx)
	if cannotTell != "" {
		return application.DoctorCannotTell, cannotTell
	}
	if len(faulty) > 0 {
		return application.DoctorFaulty, "not active: " + strings.Join(faulty, ", ")
	}
	if allSkipped {
		return application.DoctorOK, notInstalledReason
	}
	return application.DoctorOK, ""
}

// Cure implements application.DoctorCheck: `systemctl --user start <timer>`
// for every timer Probe would find faulty right now, and only those —
// re-derived fresh rather than trusted from an earlier Probe call. Starting a
// timer that is already active is a no-op, so a timer another cure or a
// person has since brought up in between costs nothing extra.
func (t *Timers) Cure(ctx context.Context) error {
	faulty, _, cannotTell := t.faulty(ctx)
	if cannotTell != "" {
		return fmt.Errorf("%s", cannotTell)
	}

	var started []string
	for _, timer := range faulty {
		if _, err := t.run(ctx, "start", timer); err != nil {
			return fmt.Errorf("systemctl --user start %s: %w", timer, err)
		}
		started = append(started, timer)
	}
	t.started = started
	return nil
}

// Damper implements application.DoctorCheck.
func (t *Timers) Damper() (time.Duration, int) { return TimersDamperWait, TimersDamperCap }

// WayBack implements application.DoctorCheck: `systemctl --user stop
// <timer>` for every timer the last Cure actually started, so a person undoes
// exactly what was done and nothing else. Before any cure has run in this
// process — a damped or dry-run report, which never runs Cure — there is
// nothing yet to stop.
func (t *Timers) WayBack() string {
	if len(t.started) == 0 {
		return "none started yet: nothing to stop"
	}
	backs := make([]string, len(t.started))
	for i, timer := range t.started {
		backs[i] = "systemctl --user stop " + timer
	}
	return strings.Join(backs, "; ")
}

// faulty is every timer, derived from Units, that is enabled here but not
// active; allSkipped, whether every one of them has no unit file on this host
// at all; or, if systemctl itself could not be asked at all, a cannotTell
// reason naming why. A timer not enabled here is skipped, not faulted: it may
// simply not be installed on this host.
func (t *Timers) faulty(ctx context.Context) (names []string, allSkipped bool, cannotTell string) {
	skipped := 0
	for _, unit := range t.Units {
		timer := timerName(unit)

		enabledOut, err := t.run(ctx, "is-enabled", timer)
		if err != nil && strings.TrimSpace(enabledOut) == "" {
			if unitNotFound(err) {
				skipped++
				continue
			}
			return nil, false, fmt.Sprintf("systemctl is-enabled %s: %v", timer, err)
		}
		if strings.TrimSpace(enabledOut) != "enabled" {
			continue
		}

		activeOut, err := t.run(ctx, "is-active", timer)
		if err != nil && strings.TrimSpace(activeOut) == "" {
			return nil, false, fmt.Sprintf("systemctl is-active %s: %v", timer, err)
		}
		if strings.TrimSpace(activeOut) != "active" {
			names = append(names, timer)
		}
	}
	return names, skipped > 0 && skipped == len(t.Units), ""
}

func (t *Timers) run(ctx context.Context, args ...string) (string, error) {
	program := t.Program
	if program == "" {
		program = "systemctl"
	}
	out, err := exec.CommandContext(ctx, program, append([]string{"--user"}, args...)...).Output()
	return string(out), err
}

// timerName is the timer that matches a service unit: "mw-dispatch.service"
// -> "mw-dispatch.timer".
func timerName(unit string) string {
	return strings.TrimSuffix(unit, ".service") + ".timer"
}
