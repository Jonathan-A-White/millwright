package doctor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var (
	_ application.DoctorCheck           = (*WSLTrim)(nil)
	_ application.DoctorStandsAfterCure = (*WSLTrim)(nil)
	_ application.DoctorStandsAfterCure = (*WSLDriveFree)(nil)
)

// WSLTrimName is what the check is called: in the log, and on the command
// line as `mw doctor wsl-trim`.
const WSLTrimName = "wsl-trim"

// WSLTrimCureCommand is the command that files the root step this check
// names as its cure.
const WSLTrimCureCommand = "mw host trim-setup"

// WSLTrimMaxAge is how long fstrim.timer may go without running before the
// check is faulty: the timer was weekly, so a day's slack.
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
// resets the list the unit file set. The timer's own adds its calendar:
// an empty OnCalendar= resets the weekly one, and daily replaces it, since a
// weekly trim is slower than the desktop's churn (about 60 GB a day) on a vhdx
// that fills its drive (mw-gq6.1614).
const (
	wslTrimDropInText      = "[Unit]\nConditionVirtualization=\n"
	wslTrimTimerDropInText = wslTrimDropInText + "[Timer]\nOnCalendar=\nOnCalendar=daily\n"
)

// WSLTrimDistro is the WSL distribution the PowerShell line runs in.
const WSLTrimDistro = "Ubuntu-24.04"

// WSLTrimRootScript is the shell script run once per WSL host as root: it
// writes both drop-ins, reloads systemd, restarts fstrim.timer and runs one
// trim at once: a Persistent timer that never ran has no stamp to catch up from,
// so a restart alone left the Laptop's never run (mw-gq6.1527). It holds no
// quote, backtick or dollar, so it passes unchanged through a PowerShell
// single-quoted string and wsl.exe, and the drop-in text is written out in it
// as plain text (printf, the opening bracket and each newline's backslash
// escaped for the shell): an antivirus took the base64 form of this paste for
// a threat and quarantined the Governor's PowerShell history (mw-gq6.1431).
func WSLTrimRootScript() string {
	var b strings.Builder
	b.WriteString("mkdir -p /etc/systemd/system/fstrim.timer.d /etc/systemd/system/fstrim.service.d")
	fmt.Fprintf(&b, " && printf %s > %s", wslTrimTimerDropInPrintf, WSLTrimDropIns[0])
	fmt.Fprintf(&b, " && printf %s > %s", wslTrimDropInPrintf, WSLTrimDropIns[1])
	b.WriteString(" && systemctl daemon-reload && systemctl restart fstrim.timer && systemctl start fstrim.service")
	return b.String()
}

// wslTrimDropInPrintf is wslTrimDropInText as printf's argument with no quote:
// the shell reads \[ as [ (so [Unit] is no glob) and \\n as \n, which printf
// makes a newline.
const wslTrimDropInPrintf = `\[Unit]\\nConditionVirtualization=\\n`

// wslTrimTimerDropInPrintf is wslTrimTimerDropInText the same way.
const wslTrimTimerDropInPrintf = wslTrimDropInPrintf + `\[Timer]\\nOnCalendar=\\nOnCalendar=daily\\n`

// WSLTrimWayBack is the commands that undo WSLTrimRootScript.
func WSLTrimWayBack() string {
	return "rm -f " + WSLTrimDropIns[0] + " " + WSLTrimDropIns[1] + " && systemctl daemon-reload"
}

// WSLTrimPowerShellLine is the one Windows PowerShell line that runs the
// root script in the WSL distribution, as root, once.
func WSLTrimPowerShellLine() string {
	return "wsl -d " + WSLTrimDistro + " -u root -- sh -c '" + WSLTrimRootScript() + "'"
}

// What WSLTrimProbeScript prints. WSLTrimOldDropIn is a host that has both
// files but whose timer's holds no OnCalendar=daily: the drop-in as it was
// before mw-gq6.1614, which leaves the timer weekly (mw-6ww.185.17).
const (
	WSLTrimNotWSL      = "not-wsl"
	WSLTrimHasDropIn   = "has-dropin"
	WSLTrimLacksDropIn = "lacks-dropin"
	WSLTrimOldDropIn   = "old-dropin"
)

// wslTrimDailyLine is the line of the timer's drop-in that makes it daily.
const wslTrimDailyLine = "OnCalendar=daily"

// WSLTrimProbeScript is the shell script that says, on whatever host runs it
// and with no root, whether that host is WSL and whether both drop-ins are
// there and the timer's makes it daily: it prints one of WSLTrimNotWSL,
// WSLTrimHasDropIn, WSLTrimOldDropIn, WSLTrimLacksDropIn. It holds no quote, so
// it passes through `ssh <host> sh -c '<script>'`.
func WSLTrimProbeScript() string {
	return `[ "$(systemd-detect-virt 2>/dev/null)" = wsl ] || { echo ` + WSLTrimNotWSL + `; exit 0; }; ` +
		`if [ -f ` + WSLTrimDropIns[0] + ` ] && [ -f ` + WSLTrimDropIns[1] + ` ]; then ` +
		`if grep -qx ` + wslTrimDailyLine + ` ` + WSLTrimDropIns[0] + `; then echo ` + WSLTrimHasDropIn + `; else echo ` + WSLTrimOldDropIn + `; fi` +
		`; else echo ` + WSLTrimLacksDropIn + `; fi`
}

// WSLTrim is the check that a WSL host trims its disk. On WSL the weekly
// fstrim.timer is skipped (its unit carries ConditionVirtualization=!container),
// so the vhdx only grows until the Windows drive fills; the root-disk-budget
// check reads ext4 used bytes, which trimming does not change, and so cannot
// see it. This check asks systemd when fstrim.timer last ran: faulty when it
// never did or last did over WSLTrimMaxAge ago, or when its calendar is not
// daily (systemd's TimersCalendar), naming WSLTrimCureCommand. On
// a host that is not WSL (systemd-detect-virt) it is inert. It changes nothing
// on the host: the cure needs root, which only the Governor's hands have, so
// its cure is one note to the Mayor.
type WSLTrim struct {
	// Program is the program run for systemctl. Empty reads "systemctl".
	Program string
	// DetectVirt says what systemd-detect-virt prints. Nil runs the real one.
	DetectVirt func(ctx context.Context) (string, error)
	// DropIns are the files whose absence says the drop-ins are not installed.
	// Nil reads WSLTrimDropIns.
	DropIns []string
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
	out, err := w.systemctl(ctx, "show", "fstrim.timer", "-p", "LoadState", "-p", "LastTriggerUSec", "-p", "TimersCalendar", "--timestamp=unix")
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("systemctl show fstrim.timer: %v", err)
	}
	props := showProperties(out)
	if props["LoadState"] != "loaded" {
		return application.DoctorCannotTell, "fstrim.timer has no unit file on this WSL host (LoadState " + props["LoadState"] + "), so nothing trims its disk"
	}
	last, ran := lastTrigger(props["LastTriggerUSec"])
	if !ran {
		return application.DoctorFaulty, "fstrim.timer has never run on this WSL host, so its vhdx is never trimmed" + w.installSaid() + "; cure: " + WSLTrimCureCommand
	}
	age := w.now().Sub(last)
	if age > WSLTrimMaxAge {
		return application.DoctorFaulty, fmt.Sprintf("fstrim.timer last ran %d days ago on this WSL host%s; cure: %s", int(age/(24*time.Hour)), w.installSaid(), WSLTrimCureCommand)
	}
	if cal := props["TimersCalendar"]; cal != "" && !calendarIsDaily(cal) {
		return application.DoctorFaulty, fmt.Sprintf("fstrim.timer last ran %d days ago but is not daily (systemd lists %s), so a weekly trim is slower than the vhdx grows%s; cure: %s, or as root on this host: %s",
			int(age/(24*time.Hour)), cal, w.installSaid(), WSLTrimCureCommand, WSLTrimRootScript())
	}
	return application.DoctorOK, fmt.Sprintf("fstrim.timer last ran %d days ago", int(age/(24*time.Hour)))
}

// calendarIsDaily says whether a TimersCalendar value of systemctl show holds an
// OnCalendar= that fires every day. systemd prints a timer set to daily as
// '{ OnCalendar=*-*-* 00:00:00 ; next_elapse=... }', never the word daily, so a
// spec is daily when it is the word daily or its date is *-*-* with no
// day-of-week field ('Mon *-*-* 00:00:00' is the weekly default). Any one of
// the timer's OnCalendar= entries being daily counts.
func calendarIsDaily(cal string) bool {
	const key = "OnCalendar="
	for _, part := range strings.Split(cal, key)[1:] {
		spec := part
		if i := strings.IndexAny(spec, ";}"); i >= 0 {
			spec = spec[:i]
		}
		fields := strings.Fields(spec)
		if len(fields) == 0 {
			continue
		}
		if len(fields) == 1 && fields[0] == "daily" {
			return true
		}
		if fields[0] == "*-*-*" {
			return true
		}
	}
	return false
}

// installSaid says, for a faulty line, that the drop-ins this check's cure
// installs are not there: the fix was never made, not made and lost. It is
// empty when they are there, and the timer then stopped for another reason.
func (w *WSLTrim) installSaid() string {
	dropIns := w.DropIns
	if dropIns == nil {
		dropIns = WSLTrimDropIns[:]
	}
	var missing []string
	for _, path := range dropIns {
		if _, err := os.Stat(path); err != nil {
			missing = append(missing, path)
		}
	}
	if len(missing) == 0 {
		return w.oldDropInSaid(dropIns)
	}
	return fmt.Sprintf(" (the fstrim drop-ins are not installed: %s missing)", strings.Join(missing, ", "))
}

// oldDropInSaid says, when the timer's drop-in is there but holds no
// OnCalendar=daily, that it is the old one that leaves the timer weekly; it is
// empty when the drop-in is daily or is not among dropIns.
func (w *WSLTrim) oldDropInSaid(dropIns []string) string {
	for _, path := range dropIns {
		if path != WSLTrimDropIns[0] {
			continue
		}
		if text, err := os.ReadFile(path); err == nil && !strings.Contains(string(text), wslTrimDailyLine) {
			return " (the timer's drop-in " + path + " is the old one with no " + wslTrimDailyLine + ")"
		}
	}
	return ""
}

// CureLeavesFault implements application.DoctorStandsAfterCure: the cure is a
// note, and the drop-ins stay uninstalled after it.
func (w *WSLTrim) CureLeavesFault() bool { return true }

// Cure implements application.DoctorCheck: one note to the Mayor, nothing on
// the host changed. A note that cannot be written fails the cure, so the
// damper tries again.
func (w *WSLTrim) Cure(ctx context.Context) error {
	if w.Note == nil {
		return nil
	}
	return w.Note(ctx, "wsl-trim: fstrim.timer on this WSL host has not run in over 8 days (or never), or is still weekly (its drop-in has no OnCalendar=daily), so the vhdx only grows until the Windows drive fills. "+
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
	cmd := exec.CommandContext(ctx, program, args...)
	// systemd 255 ignores --timestamp=unix in `systemctl show` and prints its
	// times in $TZ; in UTC, lastTrigger reads them whatever the host's zone.
	cmd.Env = append(os.Environ(), "TZ=UTC")
	out, err := cmd.Output()
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

// lastTrigger is the time of a LastTriggerUSec, and whether the timer ever ran:
// empty, n/a and @0 say it did not. It reads the form --timestamp=unix asks for
// ('@1730000000') and the one systemd 255 prints all the same, run in UTC ('Tue
// 2026-11-17 14:12:19 UTC'): reading only the first, the check said 'never run'
// on the desktop for days after its trim ran (mw-gq6.1527). A time in the
// host's own zone (systemd 255 prints $TZ) is read in time.Local.
func lastTrigger(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if at, err := time.Parse("Mon 2006-01-02 15:04:05 UTC", value); err == nil {
		return at, true
	}
	// Any other zone abbreviation is read in the host's own zone, which is the
	// one systemd printed it in: 'Sat 2026-11-21 20:47:28 EST' on the Laptop
	// (mw-6ww.185.22).
	if at, err := time.ParseInLocation("Mon 2006-01-02 15:04:05 MST", value, time.Local); err == nil {
		return at, true
	}
	if !strings.HasPrefix(value, "@") {
		return time.Time{}, false
	}
	secs, err := strconv.ParseInt(value[1:], 10, 64)
	if err != nil || secs <= 0 {
		return time.Time{}, false
	}
	return time.Unix(secs, 0), true
}

var _ application.DoctorCheck = (*WSLDriveFree)(nil)

// WSLDriveFreeName is what the check is called: in the log, and on the command
// line as `mw doctor wsl-drive-free`.
const WSLDriveFreeName = "wsl-drive-free"

// WSLDriveFreeMin is the free space on the Windows drive holding the vhdx
// under which the check is faulty: 5 GB. The desktop's D: reached 0.01 GB free
// and WSL would not start (E_FAIL) with nothing in the doctor able to see it
// (mw-gq6.1366).
const WSLDriveFreeMin int64 = 5 << 30

// WSLDriveWarnDefault is the free space on the Windows drive under which the
// check warns, 'low, not yet refusing': 30 GB, above the 15 GB under which
// dispatch starts no new Builder (application.DefaultRoomMinDriveFreeGB), so
// the Mayor hears before dispatch stops (mw-gq6.1428, provisional). The
// config's [dispatch] drive_warn_gb changes it, and 0 turns the warning off.
const WSLDriveWarnDefault int64 = 30

// WSLDriveFreeMaxAge is how old the Windows task's report may be before the
// doctor says the task has stopped: the task writes every 2 minutes.
const WSLDriveFreeMaxAge = 24 * time.Hour

// WSLDriveFreeFile and WSLDriveFreeDir are where, under $HOME, the Windows task
// (contrib/windows/wsl-flag-watch.ps1) writes its report from outside WSL: one
// line, "<drive> <free bytes> <unix seconds>", for example "D: 10737418240
// 1763245200". WSL on the desktop is sealed from Windows, so a file written into
// WSL is the one way the host sees its Windows drive.
const (
	WSLDriveFreeFile = "windows-drive-free"
	WSLDriveFreeDir  = ".local/state/mw"
)

// The wsl-drive-free check's damper: a note to the Mayor at most every 6 hours,
// and nothing on the host to cap.
const (
	WSLDriveFreeDamperWait = 6 * time.Hour
	WSLDriveFreeDamperCap  = 0
)

// WSLDriveFree is the check that the Windows drive holding a WSL host's vhdx
// has room: faulty under WSLDriveFreeMin, and faulty with a warning ('low, not
// yet refusing') under the higher WarnGB, so that the Mayor hears before
// dispatch stops at its own floor. It reads the report the Windows task writes
// into WSL; with none, the host's drive is one nothing can see, and the check
// is faulty naming the line that installs the task (mw-gq6.1428); with one it
// cannot read, or a stale one, it cannot tell. Inert on a host that is not WSL.
// Its cure is one note to the Mayor: freeing a Windows drive is the Governor's
// hands.
type WSLDriveFree struct {
	// WarnGB is the free space, in gigabytes, under which the check warns that
	// the drive is low; zero reads WSLDriveWarnDefault and a negative number
	// turns the warning off.
	WarnGB int64
	// DispatchFloorGB is the floor under which dispatch starts no new Builder,
	// said in the warning; zero reads application.DefaultRoomMinDriveFreeGB and a
	// negative number says the guard is off.
	DispatchFloorGB int64
	// last is what the latest Probe found, for the note Cure writes.
	last driveFinding

	// Path is the report file. Empty reads WSLDriveFreeDir/WSLDriveFreeFile
	// under the home directory.
	Path string
	// Mount is where this host sees the Windows drive holding its vhdx (the
	// Laptop's /mnt/c), and MountDrive the name the report gives it ("C:"). With
	// a Mount that is there, the probe writes the report itself from Statfs of it
	// before reading it (mw-gq6.1490); empty, or absent, it reads the Windows
	// task's report as before. Statfs is the free bytes of a path; nil reads the
	// real one.
	Mount      string
	MountDrive string
	Statfs     func(path string) (int64, error)
	// DetectVirt says what systemd-detect-virt prints. Nil runs the real one.
	DetectVirt func(ctx context.Context) (string, error)
	// Now is the clock. Nil reads the wall clock.
	Now func() time.Time
	// Note puts text in the Mayor's way. Nil writes nothing.
	Note func(ctx context.Context, text string) error
}

// driveFinding is what a Probe of the drive found, so that Cure says the same.
type driveFinding int

const (
	driveFine driveFinding = iota
	driveFull
	driveLow
	driveNoReport
)

// NewWSLDriveFree is the check over the real report file and systemd-detect-virt.
func NewWSLDriveFree() *WSLDriveFree { return &WSLDriveFree{} }

// Name implements application.DoctorCheck.
func (w *WSLDriveFree) Name() string { return WSLDriveFreeName }

// Probe implements application.DoctorCheck.
func (w *WSLDriveFree) Probe(ctx context.Context) (application.Verdict, string) {
	virt, err := (&WSLTrim{DetectVirt: w.DetectVirt}).detectVirt(ctx)
	if err != nil {
		return application.DoctorOK, "n/a: systemd-detect-virt could not be run, so this host is not told to be WSL"
	}
	if virt != "wsl" {
		return application.DoctorOK, "n/a: not a WSL host (systemd-detect-virt says " + virt + ")"
	}
	path, err := w.path()
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	w.last = driveFine
	// Best effort: a mount that cannot be read leaves the Windows task's report
	// (or none) to be read below, as on a host with no mount.
	_ = RefreshWSLDriveFree(path, w.Mount, w.MountDrive, w.Statfs, w.now())
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		w.last = driveNoReport
		return application.DoctorFaulty, "no report of the Windows drive's free space at " + path +
			": nothing on this WSL host can see it, so dispatch cannot tell when the drive is nearly full; the Windows task writes it, and `mw host wsl-flag --install-line` prints the line that installs or updates the task"
	}
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("the report of the Windows drive's free space could not be read: %v", err)
	}
	drive, free, at, ok := parseDriveFree(string(raw))
	if !ok {
		return application.DoctorCannotTell, fmt.Sprintf("the report of the Windows drive's free space at %s is not '<drive> <free bytes> <unix seconds>': %q", path, strings.TrimSpace(string(raw)))
	}
	age := w.now().Sub(at)
	if free < WSLDriveFreeMin {
		w.last = driveFull
		return application.DoctorFaulty, fmt.Sprintf("%s has %.1f GB free, under the %d GB the WSL vhdx needs to grow and start (report %s); cure: free space on it, then `wsl --shutdown` and start WSL; `mw host wsl-flag compact` gives back what the trim freed",
			drive, float64(free)/(1<<30), WSLDriveFreeMin>>30, reportAge(age))
	}
	if age > WSLDriveFreeMaxAge {
		// A report over a day old is no report: the task is not installed, or has
		// stopped, and nothing here sees the drive. That is the standing alarm of a
		// missing install, not a thing the doctor cannot tell (mw-gq6.1429).
		w.last = driveNoReport
		return application.DoctorFaulty, fmt.Sprintf("the report of the Windows drive's free space is %s: the Windows task has stopped writing it, so nothing on this WSL host sees its drive; `mw host wsl-flag --install-line` prints the line that installs the task", reportAge(age))
	}
	if warn := w.warnGB(); warn > 0 && free < warn<<30 {
		w.last = driveLow
		return application.DoctorFaulty, fmt.Sprintf("%s has %.1f GB free, under the %d GB warning floor: %s; cure: free space on it, `mw host wsl-flag compact` gives back what the trim freed",
			drive, float64(free)/(1<<30), warn, w.lowSaid(free))
	}
	return application.DoctorOK, fmt.Sprintf("%s has %d GB free", drive, free>>30)
}

// warnGB is the warning floor in gigabytes, 0 when it is off.
func (w *WSLDriveFree) warnGB() int64 {
	switch {
	case w.WarnGB < 0:
		return 0
	case w.WarnGB == 0:
		return WSLDriveWarnDefault
	}
	return w.WarnGB
}

// lowSaid says what the warning means at this much free: 'low, not yet refusing'
// while dispatch still starts Builders, or that it has stopped.
func (w *WSLDriveFree) lowSaid(free int64) string {
	floor := w.DispatchFloorGB
	if floor == 0 {
		floor = application.DefaultRoomMinDriveFreeGB
	}
	if floor > 0 && free < floor<<30 {
		return "dispatch is refusing now: " + w.dispatchFloorSaid()
	}
	return "low, not yet refusing: " + w.dispatchFloorSaid()
}

// dispatchFloorSaid says where dispatch stops starting Builders, or that its
// guard is off.
func (w *WSLDriveFree) dispatchFloorSaid() string {
	switch {
	case w.DispatchFloorGB < 0:
		return "dispatch's Windows-drive guard is off"
	case w.DispatchFloorGB == 0:
		return fmt.Sprintf("dispatch starts no new Builder under %d GB", application.DefaultRoomMinDriveFreeGB)
	}
	return fmt.Sprintf("dispatch starts no new Builder under %d GB", w.DispatchFloorGB)
}

// reportAge says how old a report is, in days when it is over a day.
func reportAge(age time.Duration) string {
	if age >= 24*time.Hour {
		return fmt.Sprintf("%d days old", int(age/(24*time.Hour)))
	}
	return "current"
}

// Cure implements application.DoctorCheck: one note to the Mayor, nothing on
// the host changed. A note that cannot be written fails the cure, so the
// damper tries again.
func (w *WSLDriveFree) Cure(ctx context.Context) error {
	if w.Note == nil {
		return nil
	}
	switch w.last {
	case driveLow:
		return w.Note(ctx, fmt.Sprintf("wsl-drive-free: the Windows drive holding this WSL host's vhdx is low: under %d GB free, and %s (low, not yet refusing, until it reaches that floor). "+
			"Free space on it; `mw host wsl-flag compact` gives back what the trim freed.", w.warnGB(), w.dispatchFloorSaid()))
	case driveNoReport:
		return w.Note(ctx, "wsl-drive-free: this WSL host has no report of its Windows drive's free space, so nothing here can see the drive fill and dispatch cannot hold back before it does. "+
			"`mw host wsl-flag --install-line` prints the one Windows PowerShell line that installs the task that writes it.")
	}
	return w.Note(ctx, fmt.Sprintf("wsl-drive-free: the Windows drive holding this WSL host's vhdx has under %d GB free; "+
		"at 0 WSL will not start (E_FAIL, the desktop's D: on 2026-11-15). Free space on it, or run `mw host trim-setup --check` to see which WSL hosts have no trim step yet.", WSLDriveFreeMin>>30))
}

// CureLeavesFault implements application.DoctorStandsAfterCure: the cure is a
// note, and the drive stays as the probe found it after it.
func (w *WSLDriveFree) CureLeavesFault() bool { return true }

// Damper implements application.DoctorCheck.
func (w *WSLDriveFree) Damper() (time.Duration, int) {
	return WSLDriveFreeDamperWait, WSLDriveFreeDamperCap
}

// WayBack implements application.DoctorCheck.
func (w *WSLDriveFree) WayBack() string {
	return "nothing was changed on the host; the note it wrote is cleared by the Mayor"
}

func (w *WSLDriveFree) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func (w *WSLDriveFree) path() (string, error) {
	if w.Path != "" {
		return w.Path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("there is no home directory to read the Windows drive's free-space report in: %w", err)
	}
	return filepath.Join(home, WSLDriveFreeDir, WSLDriveFreeFile), nil
}

// RefreshWSLDriveFree writes the Windows drive's report itself from statfs of
// mount, in the format the Windows task writes: "<drive> <free bytes> <unix
// seconds>", for a WSL host that sees its drive there (the Laptop's /mnt/c). It
// writes nothing, and returns nil, when mount is empty or is not there, so that
// a host whose drive is sealed from WSL (the desktop) keeps reading the task's
// report. statfs nil reads the real one. The file is replaced whole, so a
// reader never sees half a line.
func RefreshWSLDriveFree(path, mount, drive string, statfs func(string) (int64, error), now time.Time) error {
	if mount == "" {
		return nil
	}
	if info, err := os.Stat(mount); err != nil || !info.IsDir() {
		return nil
	}
	if statfs == nil {
		statfs = freeBytesOf
	}
	free, err := statfs(mount)
	if err != nil {
		return fmt.Errorf("reading the free space of %s: %w", mount, err)
	}
	if drive == "" {
		drive = "?:"
	}
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("there is no home directory to write the Windows drive's free-space report in: %w", err)
		}
		path = filepath.Join(home, WSLDriveFreeDir, WSLDriveFreeFile)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), WSLDriveFreeFile+".tmp*")
	if err != nil {
		return err
	}
	_, werr := fmt.Fprintf(tmp, "%s %d %d\n", drive, free, now.Unix())
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(tmp.Name())
		return werr
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

// freeBytesOf is the bytes free to an ordinary user on the filesystem at path.
func freeBytesOf(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

// ReadWSLDriveFree reads the Windows task's report at path, or under the home
// directory when path is empty: the drive, its free bytes and when the task
// wrote it. ok is false when there is no report, which is what a host that is
// not WSL has, or one that is not '<drive> <free bytes> <unix seconds>'.
func ReadWSLDriveFree(path string) (drive string, free int64, at time.Time, ok bool) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", 0, time.Time{}, false
		}
		path = filepath.Join(home, WSLDriveFreeDir, WSLDriveFreeFile)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", 0, time.Time{}, false
	}
	return parseDriveFree(string(raw))
}

// parseDriveFree reads "<drive> <free bytes> <unix seconds>".
func parseDriveFree(text string) (drive string, free int64, at time.Time, ok bool) {
	fields := strings.Fields(text)
	if len(fields) != 3 {
		return "", 0, time.Time{}, false
	}
	free, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || free < 0 {
		return "", 0, time.Time{}, false
	}
	secs, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || secs <= 0 {
		return "", 0, time.Time{}, false
	}
	return fields[0], free, time.Unix(secs, 0), true
}
