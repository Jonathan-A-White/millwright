package doctor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var (
	_ application.DoctorCheck           = (*WindowsState)(nil)
	_ application.DoctorStandsAfterCure = (*WindowsState)(nil)
)

// WindowsStateName is what the check is called: in the log, and on the command
// line as `mw doctor windows-state`.
const WindowsStateName = "windows-state"

// WindowsStateFile is the file, beside windows-drive-free under $HOME, that the
// Windows task (contrib/windows/wsl-flag-watch.ps1) writes every 2 minutes: one
// 'key value' line each of what is true of the Windows side (mw-6ww.185.2). On
// 2026-11-05 and 11-06 the desktop's WSL was down for two days and nothing could
// see Windows, so every guess was made from photos.
const WindowsStateFile = "windows-state"

// WindowsStateMaxAge is how old the file may be: five ticks of the task.
const WindowsStateMaxAge = 10 * time.Minute

// WindowsStateRestartPendingDays is how long a pending restart goes unsaid.
const WindowsStateRestartPendingDays = 3

// The windows-state check's damper: a note to the Mayor at most every 6 hours,
// and nothing on the host to cap.
const (
	WindowsStateDamperWait = 6 * time.Hour
	WindowsStateDamperCap  = 0
)

// windowsStateTime is the layout of the times in the file: UTC, to the second.
const windowsStateTime = "2006-01-02T15:04:05Z"

// WindowsState is the check that the Windows side of a WSL host is as it must be
// for WSL to stay up, read from the file the Windows task writes into WSL. It is
// faulty, naming each fault, when the file is absent or over 10 minutes old (the
// task has stopped, or the PC is off or asleep); when the AC standby timeout is
// not 0; when a 'millwright' scheduled task is disabled or its last result is a
// failure; when a physical disk's health is not Healthy; and when the installed
// script is not the one this mw carries; and for 24 hours after a 'drive full' act, when the watcher freed a full drive itself (mw-6ww.185.4). A restart pending for over 3 days, and the newest
// act of the last 24 hours in windows-acts beside the file, are said in its ok line. Ok, 'not WSL', on a host that is not WSL. Its cure is one
// note to the Mayor: nothing on Windows can be changed from WSL.
type WindowsState struct {
	// ScriptSHA256 is the SHA-256 of the watcher script this mw carries; empty
	// turns the comparison off.
	ScriptSHA256 string
	// Path is the state file. Empty reads WSLDriveFreeDir/WindowsStateFile under
	// the home directory.
	Path string
	// DetectVirt says what systemd-detect-virt prints. Nil runs the real one.
	DetectVirt func(ctx context.Context) (string, error)
	// Now is the clock. Nil reads the wall clock.
	Now func() time.Time
	// Note puts text in the Mayor's way. Nil writes nothing.
	Note func(ctx context.Context, text string) error

	// last is what the latest Probe found, for the note Cure writes.
	last string
}

// NewWindowsState is the check over the real state file and systemd-detect-virt.
func NewWindowsState() *WindowsState { return &WindowsState{} }

// Name implements application.DoctorCheck.
func (w *WindowsState) Name() string { return WindowsStateName }

// Probe implements application.DoctorCheck.
func (w *WindowsState) Probe(ctx context.Context) (application.Verdict, string) {
	w.last = ""
	virt, err := (&WSLTrim{DetectVirt: w.DetectVirt}).detectVirt(ctx)
	if err != nil {
		return application.DoctorOK, "not WSL: systemd-detect-virt could not be run, so this host is not told to be WSL"
	}
	if virt != "wsl" {
		return application.DoctorOK, "not WSL (systemd-detect-virt says " + virt + ")"
	}
	path, err := w.path()
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		w.last = "there is no " + WindowsStateFile + " at " + path
		return application.DoctorFaulty, "there is no " + WindowsStateFile + " at " + path +
			": the Windows task writes it every 2 minutes and nothing on this WSL host sees Windows without it; `mw host wsl-flag --install-line` prints the line that installs or updates the task"
	}
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("%s could not be read: %v", WindowsStateFile, err)
	}
	st := parseWindowsState(string(raw))
	at, err := time.Parse(windowsStateTime, st.one("time"))
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("%s at %s has no 'time <UTC ISO-8601>' line: %q", WindowsStateFile, path, firstLine(string(raw)))
	}
	age := w.now().Sub(at)
	if age > WindowsStateMaxAge {
		w.last = fmt.Sprintf("%s is %s old", WindowsStateFile, minutes(age))
		return application.DoctorFaulty, fmt.Sprintf("%s is %s old (it is written every 2 minutes, last at %s): the Windows task has stopped, or the PC is asleep or off; "+
			"`mw host wsl-flag --install-line` prints the line that installs the task again", WindowsStateFile, minutes(age), st.one("time"))
	}
	faults := w.faults(st)
	if len(faults) > 0 {
		w.last = strings.Join(faults, "; ")
		return application.DoctorFaulty, w.last
	}
	return application.DoctorOK, w.okLine(st, age)
}

// faults names each thing wrong in a current state.
func (w *WindowsState) faults(st windowsStateLines) []string {
	var faults []string
	if v := st.one("ac_standby_timeout"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n != 0 {
			faults = append(faults, fmt.Sprintf("the AC standby timeout is %d seconds, not 0: the PC sleeps on power and WSL goes with it; cure: in an admin PowerShell, `powercfg /change standby-timeout-ac 0`", n))
		}
	}
	for _, t := range st.all("task") {
		name, state, result, _ := splitBar(t)
		if name == "" || name == "unknown" {
			continue
		}
		if strings.EqualFold(state, "disabled") {
			faults = append(faults, fmt.Sprintf("the scheduled task %q is disabled; cure: in an admin PowerShell, Enable-ScheduledTask -TaskName %q", name, name))
		}
		if n, err := strconv.ParseUint(result, 10, 64); err == nil && !taskResultIsFine(n) {
			faults = append(faults, fmt.Sprintf("the scheduled task %q last ended with result %d (0x%X), a failure", name, n, n))
		}
	}
	for _, d := range st.all("disk") {
		name, media, health, _ := splitBar(d)
		if name == "" || health == "" || strings.EqualFold(health, "Healthy") {
			continue
		}
		faults = append(faults, fmt.Sprintf("the disk %q (%s) is %s, not Healthy", name, media, health))
	}
	if want := strings.TrimSpace(w.ScriptSHA256); want != "" {
		if have := st.one("script_sha256"); !strings.EqualFold(have, want) {
			shown := have
			if len(shown) > 12 {
				shown = shown[:12]
			}
			faults = append(faults, fmt.Sprintf("the installed watcher script (SHA-256 %s) is not the one this mw carries (%s): paste `mw host wsl-flag --install-line` again", shown, strings.ToUpper(want[:min(12, len(want))])))
		}
	}
	if f := w.driveFullFault(); f != "" {
		faults = append(faults, f)
	}
	return faults
}

// WindowsActDriveFull is the act the watcher writes when WSL would not start on a
// full drive and it freed the drive itself: 'drive full: freed (...)' or
// 'drive full: failed: ...' (mw-6ww.185.4).
const WindowsActDriveFull = "drive full"

// driveFullFault is the fault that a drive filled and the watcher freed it (or
// could not), for 24 hours after the newest such act: a filled drive is never
// healed in silence, and what filled it is still to be found. "" when there is none.
func (w *WindowsState) driveFullFault() string {
	path, err := w.path()
	if err != nil {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(path), WindowsActsFile))
	if err != nil {
		return ""
	}
	var best time.Time
	var outcome string
	for _, l := range strings.Split(string(raw), "\n") {
		stamp, what, ok := strings.Cut(strings.TrimSpace(l), " ")
		if !ok {
			continue
		}
		rest, isDriveFull := strings.CutPrefix(strings.TrimSpace(what), WindowsActDriveFull+": ")
		if !isDriveFull {
			continue
		}
		at, err := time.Parse(windowsStateTime, stamp)
		if err != nil || w.now().Sub(at) > WindowsStateActsWindow {
			continue
		}
		if at.After(best) {
			best, outcome = at, rest
		}
	}
	if outcome == "" {
		return ""
	}
	when := best.Format(windowsStateTime)
	if strings.HasPrefix(outcome, "failed") {
		return fmt.Sprintf("the drive filled and could not be freed at %s (%s): WSL will not start until it is; find what filled it", when, outcome)
	}
	return fmt.Sprintf("the drive filled and was freed at %s (%s): find what filled it", when, outcome)
}

// taskResultIsFine is whether a scheduled task's last result is not a failure:
// 0, and the 0x41300-0x41305 range that says ready, running, disabled, has not
// yet run, no more runs, not scheduled.
func taskResultIsFine(n uint64) bool { return n == 0 || (n >= 0x41300 && n <= 0x41305) }

// okLine says what is right, and a restart pending for over 3 days.
func (w *WindowsState) okLine(st windowsStateLines, age time.Duration) string {
	parts := []string{fmt.Sprintf("%s current (%s old)", WindowsStateFile, minutes(age))}
	if v := st.one("ac_standby_timeout"); v != "" {
		parts = append(parts, "AC standby timeout "+v)
	}
	if n := len(st.all("task")); n > 0 {
		parts = append(parts, fmt.Sprintf("%d millwright tasks ok", n))
	}
	if st.one("restart_pending") == "yes" {
		if since, err := time.Parse(windowsStateTime, st.one("restart_pending_since")); err == nil {
			if d := w.now().Sub(since); d > WindowsStateRestartPendingDays*24*time.Hour {
				parts = append(parts, fmt.Sprintf("a Windows restart pending for %d days", int(d/(24*time.Hour))))
			}
		}
	}
	if act := w.newestAct(); act != "" {
		parts = append(parts, "newest act "+act)
	}
	return strings.Join(parts, "; ")
}

// WindowsActsFile is the file beside windows-state in which the Windows task
// writes one line per act it took to keep WSL up: '<UTC time> <what>: <outcome>',
// such as '2026-11-21T12:00:00Z hung: restarted' (mw-6ww.185.3).
const WindowsActsFile = "windows-acts"

// WindowsStateActsWindow is how far back the ok line looks for an act.
const WindowsStateActsWindow = 24 * time.Hour

// newestAct is the newest act of the last 24 hours as 'what: outcome at time',
// or "" when the file is absent, unreadable or holds none. A line that is not
// '<UTC time> <text>' is skipped.
func (w *WindowsState) newestAct() string {
	path, err := w.path()
	if err != nil {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(path), WindowsActsFile))
	if err != nil {
		return ""
	}
	var best time.Time
	var text string
	for _, l := range strings.Split(string(raw), "\n") {
		stamp, what, ok := strings.Cut(strings.TrimSpace(l), " ")
		if !ok || strings.TrimSpace(what) == "" {
			continue
		}
		at, err := time.Parse(windowsStateTime, stamp)
		if err != nil || w.now().Sub(at) > WindowsStateActsWindow {
			continue
		}
		if at.After(best) {
			best, text = at, strings.TrimSpace(what)
		}
	}
	if text == "" {
		return ""
	}
	return text + " at " + best.Format(windowsStateTime)
}

// Cure implements application.DoctorCheck: one note to the Mayor, nothing on
// the host changed. A note that cannot be written fails the cure, so the damper
// tries again.
func (w *WindowsState) Cure(ctx context.Context) error {
	if w.Note == nil {
		return nil
	}
	return w.Note(ctx, "windows-state: the Windows side of this WSL host is not as it must be: "+w.last+
		". Nothing in WSL can change Windows; `mw host wsl-flag inventory` has the Windows task write the long report (windows-inventory.txt) and prints it.")
}

// CureLeavesFault implements application.DoctorStandsAfterCure: the cure is a
// note, and Windows stays as the probe found it after it.
func (w *WindowsState) CureLeavesFault() bool { return true }

// Damper implements application.DoctorCheck.
func (w *WindowsState) Damper() (time.Duration, int) {
	return WindowsStateDamperWait, WindowsStateDamperCap
}

// WayBack implements application.DoctorCheck.
func (w *WindowsState) WayBack() string {
	return "nothing was changed on the host; the note it wrote is cleared by the Mayor"
}

func (w *WindowsState) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func (w *WindowsState) path() (string, error) {
	if w.Path != "" {
		return w.Path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("there is no home directory to read %s in: %w", WindowsStateFile, err)
	}
	return filepath.Join(home, WSLDriveFreeDir, WindowsStateFile), nil
}

// windowsStateLines is the file's 'key value' lines, in order.
type windowsStateLines [][2]string

func parseWindowsState(text string) windowsStateLines {
	var lines windowsStateLines
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		key, val, ok := strings.Cut(l, " ")
		if !ok {
			continue
		}
		lines = append(lines, [2]string{key, strings.TrimSpace(val)})
	}
	return lines
}

// one is the value of the first line with key, or "".
func (l windowsStateLines) one(key string) string {
	for _, kv := range l {
		if kv[0] == key {
			return kv[1]
		}
	}
	return ""
}

// all are the values of every line with key.
func (l windowsStateLines) all(key string) []string {
	var out []string
	for _, kv := range l {
		if kv[0] == key {
			out = append(out, kv[1])
		}
	}
	return out
}

// splitBar splits 'a|b|c|d' into its first four fields, the last of which keeps
// any rest. Windows forbids '|' in a task's name, so it never splits one.
func splitBar(s string) (a, b, c, d string) {
	f := strings.SplitN(s, "|", 4)
	for len(f) < 4 {
		f = append(f, "")
	}
	return f[0], f[1], f[2], f[3]
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// minutes says an age in whole minutes.
func minutes(d time.Duration) string {
	n := int(d / time.Minute)
	if n == 1 {
		return "1 minute"
	}
	return fmt.Sprintf("%d minutes", n)
}
