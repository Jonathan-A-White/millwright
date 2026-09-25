package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*MayorGone)(nil)

// MayorGoneName is what the check is called: in the log, and on the command
// line as `mw doctor mayor-gone`.
const MayorGoneName = "mayor-gone"

// The mayor-gone check's damper: idle between two respawns, and how many it
// spends on one fault episode before it gives up and waits for the
// escalation note to wake the Millhand.
const (
	MayorGoneDamperWait = 30 * time.Minute
	MayorGoneDamperCap  = 2
)

// MayorGoneCureTimeout bounds how long Cure waits for the vault's own
// bin/mayor-up to bring up a tmux server and respawn the Mayor.
const MayorGoneCureTimeout = 120 * time.Second

// mayorGoneBareShells are the pane commands contrib/health/mw-health.sh also
// reads as "nobody but a shell is here": a window whose live pane runs one of
// these holds no Mayor, whatever its name.
var mayorGoneBareShells = map[string]bool{
	"": true, "sh": true, "bash": true, "dash": true, "zsh": true,
	"fish": true, "ksh": true, "tcsh": true, "csh": true,
}

// MayorGone is the check that respawns the Mayor when the window its own
// .mayor-acting names is gone, or is open but holds no live process other
// than a bare shell — the same reading of .mayor-acting
// contrib/health/mw-health.sh keeps for its own mayor=gone line, mirrored
// here in Go rather than shelled out to, and application.MatchActingName,
// the one place both this check and `mw seat up` search an acting file's
// text for the name of an open window.
//
// It never invents its own way to bring a Mayor up: its cure is always the
// vault's own bin/mayor-up, which brings up the tmux server if that is what
// is missing, then respawns from the newest handoff with a death note. It is
// inert (cannot-tell) on a host whose vault has no .mayor-acting at all — one
// the Mayor does not sit on — and on one whose vault has no bin/mayor-up, or
// one that is not executable.
type MayorGone struct {
	// Vault is the vault directory this host keeps: .mayor-acting and
	// bin/mayor-up are read from under it.
	Vault string
	// Tmux is the program run for tmux. Empty reads "tmux".
	Tmux string
	// PS is the program run for ps, asked for the whole process table when no
	// window name matches .mayor-acting, to tell a Mayor resumed into a
	// window of another name from one that is really gone. Empty reads "ps".
	PS string
	// Timeout bounds Cure's run of bin/mayor-up. Empty reads
	// MayorGoneCureTimeout.
	Timeout time.Duration

	// window is the id bin/mayor-up's cure printed on its last output line,
	// read by WayBack once Cure has run. Empty before any cure has run in
	// this process.
	window string
}

// NewMayorGone is the check over the given vault, run through the real tmux
// and the vault's own bin/mayor-up.
func NewMayorGone(vault string) *MayorGone { return &MayorGone{Vault: vault} }

// Name implements application.DoctorCheck.
func (m *MayorGone) Name() string { return MayorGoneName }

// Probe implements application.DoctorCheck: cannot-tell naming whichever of
// .mayor-acting or bin/mayor-up is missing or not executable; otherwise
// faulty naming the acting file when no open tmux window matches what it
// names and no window's pane runs a process carrying a session id the acting
// file names either — a Mayor resumed by `claude --resume` into a window of
// another name still shows up there — faulty naming the window when it is
// open but its pane holds nothing live but a bare shell, and ok when a live
// process is found there.
func (m *MayorGone) Probe(ctx context.Context) (application.Verdict, string) {
	actingPath := m.actingPath()
	acting, err := os.ReadFile(actingPath)
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("no %s: nobody names a Mayor on this host", actingPath)
	}

	mayorUp := m.mayorUpPath()
	if info, statErr := os.Stat(mayorUp); statErr != nil || info.Mode()&0o111 == 0 {
		return application.DoctorCannotTell, mayorUp + " is missing or not executable"
	}

	names, byName, err := m.windows(ctx)
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("asking tmux for its windows: %v", err)
	}
	name, found := application.MatchActingName(string(acting), names)
	if !found {
		sessionName, sessionFound, sessionErr := m.matchBySession(ctx, string(acting), byName)
		if sessionErr != nil {
			return application.DoctorCannotTell, fmt.Sprintf("asking the process tree whether a window carries %s's session: %v", actingPath, sessionErr)
		}
		name, found = sessionName, sessionFound
	}
	if !found {
		return application.DoctorFaulty, "no open tmux window matches " + actingPath
	}

	alive, err := m.paneAlive(ctx, byName[name])
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("asking tmux about window %s: %v", byName[name], err)
	}
	if !alive {
		return application.DoctorFaulty, fmt.Sprintf("window %s (%s) holds nothing live but a bare shell", name, byName[name])
	}
	return application.DoctorOK, ""
}

// Cure implements application.DoctorCheck: bin/mayor-up, given MW_DOCTOR=1 and
// MayorGoneCureTimeout to run in, its combined output condensed to one line
// per line printed and folded into the returned error when it exits nonzero
// — a live Mayor already sitting (exit 3) reads the same as it being unable
// to start one (exit 4): either way, this cure did not do what it set out to.
// Its last output line, on success, is the window id it started, printed the
// way the real script does; WayBack reads it once Cure has set it.
func (m *MayorGone) Cure(ctx context.Context) error {
	cctx, cancel := context.WithTimeout(ctx, m.timeout())
	defer cancel()

	cmd := exec.CommandContext(cctx, m.mayorUpPath())
	cmd.Env = append(os.Environ(), "MW_DOCTOR=1")
	out, err := cmd.CombinedOutput()
	lines := nonEmptyLines(string(out))

	if err != nil {
		return fmt.Errorf("%s: %v (%s)", m.mayorUpPath(), err, strings.Join(lines, "; "))
	}
	if len(lines) > 0 {
		m.window = lines[len(lines)-1]
	}
	return nil
}

// Damper implements application.DoctorCheck.
func (m *MayorGone) Damper() (time.Duration, int) { return MayorGoneDamperWait, MayorGoneDamperCap }

// WayBack implements application.DoctorCheck: before any cure has run in this
// process — a damped or dry-run report, which never runs Cure — the mayor-up
// line itself, since there is no window id yet to know a kill-window from;
// after a cure, `tmux kill-window -t` on the window id it actually started.
func (m *MayorGone) WayBack() string {
	if m.window == "" {
		return fmt.Sprintf("would run: MW_DOCTOR=1 %s; its way back, once it starts a Mayor, is: tmux kill-window -t '<window id it prints>'", m.mayorUpPath())
	}
	return fmt.Sprintf("tmux kill-window -t '%s'", m.window)
}

func (m *MayorGone) actingPath() string {
	return filepath.Join(m.Vault, application.ActingFileName("mayor"))
}

func (m *MayorGone) mayorUpPath() string {
	return filepath.Join(m.Vault, "bin", "mayor-up")
}

func (m *MayorGone) timeout() time.Duration {
	if m.Timeout > 0 {
		return m.Timeout
	}
	return MayorGoneCureTimeout
}

// windows is every open tmux window's name, and the window id each name is
// found under. No server running reads as no windows open — an answer, not a
// failure to ask, the same as infrastructure/tmux's own reading of it.
func (m *MayorGone) windows(ctx context.Context) (names []string, byName map[string]string, err error) {
	out, runErr := m.run(ctx, "list-windows", "-a", "-F", "#{window_id}|#{window_name}")
	byName = map[string]string{}
	if runErr != nil {
		if mayorGoneNoServer(runErr) {
			return nil, byName, nil
		}
		return nil, nil, runErr
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		fields := strings.SplitN(line, "|", 2)
		if len(fields) != 2 || fields[0] == "" || fields[1] == "" {
			continue
		}
		names = append(names, fields[1])
		byName[fields[1]] = fields[0]
	}
	return names, byName, nil
}

// mayorGoneSessionID is a session id-like token in an acting file's free
// text: eight or more hex digits, long enough that a window name's own
// hyphen-separated date fields (never more than four digits between
// hyphens) cannot be mistaken for one. It catches both a bare Claude Code
// session id and the leading segment of one written with its usual dashes.
var mayorGoneSessionID = regexp.MustCompile(`[0-9a-fA-F]{8,}`)

// matchBySession is the name of a window in byName whose pane runs a process
// — or has one among its descendants — carrying a session id acting names,
// when acting names no window found by MatchActingName. It is how a Mayor
// resumed by `claude --resume` into a window of a name .mayor-acting never
// gave is still found: tmux opens the new window under the harness's own
// name, not the acting file's, but the resumed session's process still
// carries the id the acting file recorded when it was first started.
func (m *MayorGone) matchBySession(ctx context.Context, acting string, byName map[string]string) (string, bool, error) {
	tokens := mayorGoneSessionID.FindAllString(acting, -1)
	if len(tokens) == 0 {
		return "", false, nil
	}

	panePIDs, err := m.panePIDs(ctx)
	if err != nil {
		return "", false, err
	}
	if len(panePIDs) == 0 {
		return "", false, nil
	}
	procs, err := m.processes(ctx)
	if err != nil {
		return "", false, err
	}

	for name, window := range byName {
		for _, pid := range panePIDs[window] {
			if processTreeCarries(procs, pid, tokens) {
				return name, true, nil
			}
		}
	}
	return "", false, nil
}

// panePIDs is the pid of each open window's pane, by window id.
func (m *MayorGone) panePIDs(ctx context.Context) (map[string][]string, error) {
	out, err := m.run(ctx, "list-panes", "-a", "-F", "#{window_id} #{pane_pid}")
	pids := map[string][]string{}
	if err != nil {
		if mayorGoneNoServer(err) {
			return pids, nil
		}
		return nil, err
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		pids[fields[0]] = append(pids[fields[0]], fields[1])
	}
	return pids, nil
}

// mayorGoneProcess is one line of the process table matchBySession reads.
type mayorGoneProcess struct {
	ppid string
	args string
}

// processes is the whole process table, by pid, read in one ps.
func (m *MayorGone) processes(ctx context.Context) (map[string]mayorGoneProcess, error) {
	program := m.PS
	if program == "" {
		program = "ps"
	}
	out, err := exec.CommandContext(ctx, program, "-eo", "pid=,ppid=,args=").Output()
	if err != nil {
		return nil, err
	}
	procs := map[string]mayorGoneProcess{}
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		procs[fields[0]] = mayorGoneProcess{ppid: fields[1], args: strings.Join(fields[2:], " ")}
	}
	return procs, nil
}

// processTreeCarries reports whether pid, or any of its descendants in
// procs, has one of tokens in its command line.
func processTreeCarries(procs map[string]mayorGoneProcess, pid string, tokens []string) bool {
	seen := map[string]bool{}
	queue := []string{pid}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true

		if proc, ok := procs[id]; ok {
			for _, token := range tokens {
				if strings.Contains(proc.args, token) {
					return true
				}
			}
		}
		for candidate, proc := range procs {
			if proc.ppid == id && !seen[candidate] {
				queue = append(queue, candidate)
			}
		}
	}
	return false
}

// paneAlive reports whether window holds a live pane running anything but a
// bare shell.
func (m *MayorGone) paneAlive(ctx context.Context, window string) (bool, error) {
	out, err := m.run(ctx, "list-panes", "-t", window, "-F", "#{pane_dead} #{pane_current_command}")
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "0" {
			continue
		}
		if !mayorGoneBareShells[strings.TrimPrefix(fields[1], "-")] {
			return true, nil
		}
	}
	return false, nil
}

func (m *MayorGone) run(ctx context.Context, args ...string) (string, error) {
	program := m.Tmux
	if program == "" {
		program = "tmux"
	}
	out, err := exec.CommandContext(ctx, program, args...).Output()
	return string(out), err
}

// mayorGoneNoServer reports whether tmux failed for want of a server to ask:
// none running, or no socket to reach one by.
func mayorGoneNoServer(err error) bool {
	said := err.Error()
	return strings.Contains(said, "no server running") || strings.Contains(said, "error connecting to")
}

// nonEmptyLines is text's lines, trimmed, blank ones dropped.
func nonEmptyLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
