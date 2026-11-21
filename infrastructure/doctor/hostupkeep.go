package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*HostUpkeep)(nil)

// HostUpkeepName is what the check is called: in the log, and on the command
// line as `mw doctor host-upkeep`.
const HostUpkeepName = "host-upkeep"

// The marks a real run of the sweep and of the go-cache tidy leave in the state
// directory (~/.local/state/mw): the file's modification time is when the last
// run that acted finished. A listing run leaves HostUpkeepListedMark, which is
// no run of upkeep.
const (
	HostUpkeepSweepMark  = "host-sweep-ran"
	HostUpkeepGoTidyMark = "host-tidy-go-cache-ran"
	// HostUpkeepListedMark is the file mw host sweep --timer keeps for its first
	// run on a host, which only lists (docs/host-sweep.md).
	HostUpkeepListedMark = "host-sweep-listed"
)

// What host upkeep is held to (mw-6ww.185.8). Each is a bound of docs/host-sweep.md.
const (
	// HostUpkeepStale is how old the last real sweep or go-cache tidy may be.
	HostUpkeepStale = 48 * time.Hour
	// HostUpkeepClosedAge is how long a story must have been closed for its
	// worktree to count as left behind.
	HostUpkeepClosedAge = 24 * time.Hour
	// HostUpkeepMostLeft is how many such worktrees a host may hold.
	HostUpkeepMostLeft = 20
	// HostUpkeepFix is the one command that links and enables the units a Boost runs.
	HostUpkeepFix = "sh scripts/install-units.sh --boost --enable"
)

// The check's damper. A cure that is cut at the doctor's own limit is resumed
// by the next run: each act of the sweep stands alone.
const (
	HostUpkeepDamperWait = 15 * time.Minute
	HostUpkeepDamperCap  = 6
)

// hostUpkeepTimers are the timers whose absence the check names.
var hostUpkeepTimers = []string{"mw-host-sweep.timer", "mw-host-tidy.timer"}

// ErrNoUserManager is what a TimerEnabled answers when the host has no user
// manager to ask (a system-scope install): the timers are not the check's to judge.
var ErrNoUserManager = errors.New("no user manager on this host")

// RecordHostUpkeepRun marks, in dir, that the upkeep run named mark finished at
// at. It is called by the commands themselves, after a run that acted.
func RecordHostUpkeepRun(dir, mark string, at time.Time) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, mark)
	if err := os.WriteFile(path, []byte("ran\n"), 0o644); err != nil {
		return err
	}
	return os.Chtimes(path, at, at)
}

// HostUpkeep is the check that this host's upkeep has run: the sweep that takes
// a closed story's worktree away (a story built here and landed from another
// host leaves its worktree here for ever, mw-6ww.185.8), and the tidy of Go's
// build cache. It is faulty when the last real sweep is over two days old, or
// there has only ever been a listing run; when the last go-cache tidy is over
// two days old; when over twenty worktrees are of stories closed for over a
// day; and when the sweep's or the tidy's timer is not enabled. Its cure runs
// the sweep and both tidies itself.
type HostUpkeep struct {
	// StateDir is where the marks are kept.
	StateDir string
	// Worktrees are the directories holding story worktrees, one per rig's
	// .mw-worktrees; each directory with a .git in it is a story's, named by it.
	Worktrees []string
	// Stories is the tracker, read for when each story closed.
	Stories application.HostSweepStories
	// TimerEnabled says whether a user timer is enabled; nil asks systemctl.
	TimerEnabled func(ctx context.Context, timer string) (bool, error)
	// Run runs `mw <args>` and returns what it printed; nil runs this binary.
	Run func(ctx context.Context, args ...string) (string, error)
	// Now is the clock; nil is time.Now.
	Now func() time.Time

	// cured is what the last Cure printed, one summary line per command.
	cured []string
}

// Name implements application.DoctorCheck.
func (h *HostUpkeep) Name() string { return HostUpkeepName }

func (h *HostUpkeep) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// Probe implements application.DoctorCheck.
func (h *HostUpkeep) Probe(ctx context.Context) (application.Verdict, string) {
	now := h.now()
	var faults []string
	if reason := h.sweepFault(now); reason != "" {
		faults = append(faults, reason)
	}
	if reason := h.goTidyFault(now); reason != "" {
		faults = append(faults, reason)
	}
	left, unread := h.leftWorktrees(ctx, now)
	if left > HostUpkeepMostLeft {
		faults = append(faults, fmt.Sprintf("%d worktrees of stories closed for over a day are still here (over %d)", left, HostUpkeepMostLeft))
	}
	off, timerErr := h.timersOff(ctx)
	if len(off) > 0 {
		faults = append(faults, fmt.Sprintf("%s not enabled: no cure runs this, a person runs it from the rig's checkout: %s", strings.Join(off, " and "), HostUpkeepFix))
	}
	switch {
	case len(faults) > 0:
		return application.DoctorFaulty, strings.Join(faults, "; ")
	case unread != nil:
		return application.DoctorCannotTell, "the worktrees' stories could not be read: " + unread.Error()
	case timerErr != nil:
		return application.DoctorCannotTell, timerErr.Error()
	}
	return application.DoctorOK, ""
}

// sweepFault is why the last real sweep is not recent enough, or "".
func (h *HostUpkeep) sweepFault(now time.Time) string {
	at, ok := h.markTime(HostUpkeepSweepMark)
	if !ok {
		if _, listed := h.markTime(HostUpkeepListedMark); listed {
			return "the host sweep has only listed, never removed anything: no real sweep has run here"
		}
		return "the host sweep has never run here"
	}
	if age := now.Sub(at); age > HostUpkeepStale {
		return fmt.Sprintf("the last real host sweep was %s ago (over 2 days)", ageWords(age))
	}
	return ""
}

// goTidyFault is why the last go-cache tidy is not recent enough, or "".
func (h *HostUpkeep) goTidyFault(now time.Time) string {
	at, ok := h.markTime(HostUpkeepGoTidyMark)
	if !ok {
		return "the go-cache tidy has never run here"
	}
	if age := now.Sub(at); age > HostUpkeepStale {
		return fmt.Sprintf("the last go-cache tidy was %s ago (over 2 days)", ageWords(age))
	}
	return ""
}

func (h *HostUpkeep) markTime(name string) (time.Time, bool) {
	info, err := os.Stat(filepath.Join(h.StateDir, name))
	if err != nil {
		return time.Time{}, false
	}
	return info.ModTime(), true
}

func ageWords(d time.Duration) string {
	if d >= 48*time.Hour {
		return fmt.Sprintf("%d days", int(d/(24*time.Hour)))
	}
	return fmt.Sprintf("%d hours", int(d/time.Hour))
}

// leftWorktrees counts the worktrees of stories closed for over a day. A story
// the tracker does not know is not counted, and a tracker that cannot be read is
// the second answer.
func (h *HostUpkeep) leftWorktrees(ctx context.Context, now time.Time) (int, error) {
	var ids []string
	seen := map[string]bool{}
	for _, parent := range h.Worktrees {
		entries, err := os.ReadDir(parent)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || seen[e.Name()] {
				continue
			}
			if _, err := os.Lstat(filepath.Join(parent, e.Name(), ".git")); err != nil {
				continue
			}
			seen[e.Name()] = true
			ids = append(ids, e.Name())
		}
	}
	if len(ids) == 0 || h.Stories == nil {
		return 0, nil
	}
	sort.Strings(ids)
	details, err := h.Stories.ShowBeads(ctx, ids)
	if err != nil {
		return 0, err
	}
	left := 0
	for _, d := range details {
		if d.Status == application.StateClosed && !d.ClosedAt.IsZero() && now.Sub(d.ClosedAt) > HostUpkeepClosedAge {
			left++
		}
	}
	return left, nil
}

// timersOff is the timers of the sweep and the tidy this host has not enabled.
func (h *HostUpkeep) timersOff(ctx context.Context) ([]string, error) {
	var off []string
	for _, timer := range hostUpkeepTimers {
		enabled := h.timerEnabled
		if h.TimerEnabled != nil {
			enabled = h.TimerEnabled
		}
		on, err := enabled(ctx, timer)
		switch {
		case errors.Is(err, ErrNoUserManager):
			return nil, nil
		case err != nil:
			return off, err
		case !on:
			off = append(off, timer)
		}
	}
	return off, nil
}

// timerEnabled asks systemctl --user. A timer with no unit file here is not
// enabled; a host with no user manager is ErrNoUserManager.
func (h *HostUpkeep) timerEnabled(ctx context.Context, timer string) (bool, error) {
	out, err := exec.CommandContext(ctx, "systemctl", "--user", "is-enabled", timer).Output()
	state := strings.TrimSpace(string(out))
	if err == nil {
		return state == "enabled", nil
	}
	if state != "" || unitNotFound(err) {
		return false, nil
	}
	if noUserManager(err) {
		return false, ErrNoUserManager
	}
	return false, fmt.Errorf("systemctl is-enabled %s: %w", timer, err)
}

// hostUpkeepCure is what Cure runs, quick ones first so a sweep the doctor's
// own limit cuts short does not keep the tidies from running; each act of the
// sweep stands alone, and the next run goes on.
var hostUpkeepCure = [][]string{
	{"host", "tidy", "tmp"},
	{"host", "tidy", "go-cache"},
	{"host", "sweep"},
}

// Cure implements application.DoctorCheck: it runs the tidies and the sweep, in
// this process's own binary, and goes on past one that fails.
func (h *HostUpkeep) Cure(ctx context.Context) error {
	var cured []string
	var failed []error
	for _, args := range hostUpkeepCure {
		out, err := h.run(ctx, args...)
		line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(out), "\n", 2)[0])
		if err != nil {
			failed = append(failed, fmt.Errorf("mw %s: %w", strings.Join(args, " "), err))
			continue
		}
		if line == "" {
			line = "mw " + strings.Join(args, " ") + " ran and said nothing"
		}
		cured = append(cured, line)
	}
	h.cured = cured
	return errors.Join(failed...)
}

func (h *HostUpkeep) run(ctx context.Context, args ...string) (string, error) {
	if h.Run != nil {
		return h.Run(ctx, args...)
	}
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	out, err := exec.CommandContext(ctx, self, args...).CombinedOutput()
	return string(out), err
}

// Damper implements application.DoctorCheck.
func (h *HostUpkeep) Damper() (time.Duration, int) { return HostUpkeepDamperWait, HostUpkeepDamperCap }

// WayBack implements application.DoctorCheck: what the cure removed is not put
// back (a closed story's worktree, a rebuildable cache), and it says what that was.
func (h *HostUpkeep) WayBack() string {
	if len(h.cured) == 0 {
		return "none: the cure removes only what no live story can need (docs/host-sweep.md); nothing to restore"
	}
	return "none: nothing to restore, each is what no live story needs or a build makes again. Removed: " + strings.Join(h.cured, "; ")
}
