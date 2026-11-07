package doctor

import (
	"context"
	"encoding/base64"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*WSLTrim)(nil)

// WSLTrimName is what the check is called: in the log, and on the command
// line as `mw doctor wsl-trim`.
const WSLTrimName = "wsl-trim"

// WSLTrimCureCommand is the command that files the root step this check
// names as its cure.
const WSLTrimCureCommand = "mw host trim-setup"

// WSLTrimMaxAge is how long fstrim.timer may go without running before the
// check is faulty: the timer is weekly, so a day's slack.
const WSLTrimMaxAge = 8 * 24 * time.Hour

// The wsl-trim check's damper: its cure is one note to the Mayor and nothing
// on the host, so a day's wait alone says how often he hears of it, and there
// is no cure to cap.
const (
	WSLTrimDamperWait = 24 * time.Hour
	WSLTrimDamperCap  = 0
)

// WSLTrimDropIns are the two systemd drop-ins that reset the units'
// ConditionVirtualization=!container, which systemd evaluates as unmet on WSL
// (systemd-detect-virt says wsl), so that fstrim.timer never starts and the
// vhdx only grows (mw-gq6.1020, the desktop's D: full, mw-6ww.156).
var WSLTrimDropIns = [2]string{
	"/etc/systemd/system/fstrim.timer.d/wsl.conf",
	"/etc/systemd/system/fstrim.service.d/wsl.conf",
}

// wslTrimDropInText is a drop-in's content: an empty ConditionVirtualization=
// resets the list the unit file set.
const wslTrimDropInText = "[Unit]\nConditionVirtualization=\n"

// WSLTrimDistro is the WSL distribution the PowerShell line runs in.
const WSLTrimDistro = "Ubuntu-24.04"

// WSLTrimRootScript is the shell script run once per WSL host as root: it
// writes both drop-ins, reloads systemd and restarts fstrim.timer (which is
// Persistent, so it catches up the weeks it missed at once). It holds no
// quote, backtick or dollar, so it passes unchanged through a PowerShell
// single-quoted string and wsl.exe: the drop-in text goes in as base64.
func WSLTrimRootScript() string {
	encoded := base64.StdEncoding.EncodeToString([]byte(wslTrimDropInText))
	var b strings.Builder
	b.WriteString("mkdir -p /etc/systemd/system/fstrim.timer.d /etc/systemd/system/fstrim.service.d")
	for _, path := range WSLTrimDropIns {
		fmt.Fprintf(&b, " && echo %s | base64 -d > %s", encoded, path)
	}
	b.WriteString(" && systemctl daemon-reload && systemctl restart fstrim.timer")
	return b.String()
}

// WSLTrimWayBack is the commands that undo WSLTrimRootScript.
func WSLTrimWayBack() string {
	return "rm -f " + WSLTrimDropIns[0] + " " + WSLTrimDropIns[1] + " && systemctl daemon-reload"
}

// WSLTrimPowerShellLine is the one Windows PowerShell line that runs the
// root script in the WSL distribution, as root, once.
func WSLTrimPowerShellLine() string {
	return "wsl -d " + WSLTrimDistro + " -u root -- sh -c '" + WSLTrimRootScript() + "'"
}

// What WSLTrimProbeScript prints.
const (
	WSLTrimNotWSL      = "not-wsl"
	WSLTrimHasDropIn   = "has-dropin"
	WSLTrimLacksDropIn = "lacks-dropin"
)

// WSLTrimProbeScript is the shell script that says, on whatever host runs it
// and with no root, whether that host is WSL and whether both drop-ins are
// there: it prints one of WSLTrimNotWSL, WSLTrimHasDropIn, WSLTrimLacksDropIn.
func WSLTrimProbeScript() string {
	return `[ "$(systemd-detect-virt 2>/dev/null)" = wsl ] || { echo ` + WSLTrimNotWSL + `; exit 0; }; ` +
		`if [ -f ` + WSLTrimDropIns[0] + ` ] && [ -f ` + WSLTrimDropIns[1] + ` ]; then echo ` + WSLTrimHasDropIn +
		`; else echo ` + WSLTrimLacksDropIn + `; fi`
}

// WSLTrim is the check that a WSL host trims its disk. On WSL the weekly
// fstrim.timer is skipped (its unit carries ConditionVirtualization=!container),
// so the vhdx only grows until the Windows drive fills; the root-disk-budget
// check reads ext4 used bytes, which trimming does not change, and so cannot
// see it. This check asks systemd when fstrim.timer last ran: faulty when it
// never did or last did over WSLTrimMaxAge ago, naming WSLTrimCureCommand. On
// a host that is not WSL (systemd-detect-virt) it is inert. It changes nothing
// on the host: the cure needs root, which only the Governor's hands have, so
// its cure is one note to the Mayor.
type WSLTrim struct {
	// Program is the program run for systemctl. Empty reads "systemctl".
	Program string
	// DetectVirt says what systemd-detect-virt prints. Nil runs the real one.
	DetectVirt func(ctx context.Context) (string, error)
	// Now is the clock. Nil reads the wall clock.
	Now func() time.Time
	// Note puts text in the Mayor's way. Nil writes nothing.
	Note func(ctx context.Context, text string) error
}

// NewWSLTrim is the check over the real systemctl and systemd-detect-virt.
func NewWSLTrim() *WSLTrim { return &WSLTrim{} }

// Name implements application.DoctorCheck.
func (w *WSLTrim) Name() string { return WSLTrimName }

// Probe implements application.DoctorCheck.
func (w *WSLTrim) Probe(ctx context.Context) (application.Verdict, string) {
	virt, err := w.detectVirt(ctx)
	if err != nil {
		return application.DoctorOK, "n/a: systemd-detect-virt could not be run, so this host is not told to be WSL"
	}
	if virt != "wsl" {
		return application.DoctorOK, "n/a: not a WSL host (systemd-detect-virt says " + virt + ")"
	}
	out, err := w.systemctl(ctx, "show", "fstrim.timer", "-p", "LoadState", "-p", "LastTriggerUSec", "--timestamp=unix")
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("systemctl show fstrim.timer: %v", err)
	}
	props := showProperties(out)
	if props["LoadState"] != "loaded" {
		return application.DoctorCannotTell, "fstrim.timer has no unit file on this WSL host (LoadState " + props["LoadState"] + "), so nothing trims its disk"
	}
	last, ran := lastTrigger(props["LastTriggerUSec"])
	if !ran {
		return application.DoctorFaulty, "fstrim.timer has never run on this WSL host, so its vhdx is never trimmed; cure: " + WSLTrimCureCommand
	}
	age := w.now().Sub(last)
	if age > WSLTrimMaxAge {
		return application.DoctorFaulty, fmt.Sprintf("fstrim.timer last ran %d days ago on this WSL host; cure: %s", int(age/(24*time.Hour)), WSLTrimCureCommand)
	}
	return application.DoctorOK, fmt.Sprintf("fstrim.timer last ran %d days ago", int(age/(24*time.Hour)))
}

// Cure implements application.DoctorCheck: one note to the Mayor, nothing on
// the host changed. A note that cannot be written fails the cure, so the
// damper tries again.
func (w *WSLTrim) Cure(ctx context.Context) error {
	if w.Note == nil {
		return nil
	}
	return w.Note(ctx, "wsl-trim: fstrim.timer on this WSL host has not run in over 8 days (or never), so the vhdx only grows until the Windows drive fills. "+
		"Run `"+WSLTrimCureCommand+" --bead <bead>` to file the root step for the Governor's hands on every WSL host that lacks it, "+
		"or run `"+WSLTrimCureCommand+"` alone for the one Windows PowerShell line to paste once.")
}

// Damper implements application.DoctorCheck.
func (w *WSLTrim) Damper() (time.Duration, int) { return WSLTrimDamperWait, WSLTrimDamperCap }

// WayBack implements application.DoctorCheck: the cure changes nothing, so the
// way back is only how to undo the step it names.
func (w *WSLTrim) WayBack() string {
	return "nothing was changed; the step it names is undone by: " + WSLTrimWayBack()
}

func (w *WSLTrim) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func (w *WSLTrim) detectVirt(ctx context.Context) (string, error) {
	if w.DetectVirt != nil {
		return w.DetectVirt(ctx)
	}
	// systemd-detect-virt exits 1 and prints "none" on a host with no
	// virtualization: the output is the answer, the exit status is not.
	out, err := exec.CommandContext(ctx, "systemd-detect-virt").Output()
	if text := strings.TrimSpace(string(out)); text != "" {
		return text, nil
	}
	return "", err
}

func (w *WSLTrim) systemctl(ctx context.Context, args ...string) (string, error) {
	program := w.Program
	if program == "" {
		program = "systemctl"
	}
	out, err := exec.CommandContext(ctx, program, args...).Output()
	return string(out), err
}

// showProperties is the Key=Value lines of `systemctl show` as a map.
func showProperties(out string) map[string]string {
	props := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if key, value, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			props[key] = value
		}
	}
	return props
}

// lastTrigger is the time of a LastTriggerUSec read with --timestamp=unix
// ('@1730000000'), and whether the timer ever ran: empty, n/a and @0 say it
// did not.
func lastTrigger(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "@") {
		return time.Time{}, false
	}
	secs, err := strconv.ParseInt(value[1:], 10, 64)
	if err != nil || secs <= 0 {
		return time.Time{}, false
	}
	return time.Unix(secs, 0), true
}
