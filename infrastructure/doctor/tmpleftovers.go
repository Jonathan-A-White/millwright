package doctor

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var (
	_ application.DoctorCheck           = (*TmpLeftovers)(nil)
	_ application.DoctorCoolingQuiet    = (*TmpLeftovers)(nil)
	_ application.DoctorCureEndsEpisode = (*TmpLeftovers)(nil)
)

// TmpLeftoversName is what the check is called: in the log, and on the
// command line as `mw doctor tmp-leftovers`.
const TmpLeftoversName = "tmp-leftovers"

// tmpLeftoversSpoolGlob matches the dolt spool files a killed bd process can
// leave behind under TmpDir.
const tmpLeftoversSpoolGlob = "nbs-spool-*"

// tmpLeftoversBdName is /tmp/bd, kept open only while a bd process runs.
const tmpLeftoversBdName = "bd"

// tmpLeftoversClaudeDir is the directory under TmpDir holding one
// subdirectory per Claude Code session.
const tmpLeftoversClaudeDir = "claude-0"

// tmpLeftoversLiveGoProcessNames are the process names (as /proc/<pid>/comm
// reports them) that mean a go build, test or vet is under way: go-build's
// cure would race a live one of these even with no file of its own held open
// yet.
var tmpLeftoversLiveGoProcessNames = map[string]bool{
	"go":        true,
	"gotestsum": true,
	"compile":   true,
	"link":      true,
	"vet":       true,
	// make runs a rig's gate (make test, then make lint): between its go steps
	// no go process is live, but make is (mw-gq6.1332).
	"make": true,
}

// tmpLeftoversGateVerbs are the mw subcommands that run a rig's gate, or a part
// of it, or the commands after a landing: the mw process is alive across the
// gaps between the go steps of its gate, and across the gaps between a split
// gate's parts, where the gate lock is given back (mw-gq6.1332). mw dispatch is
// not here: it lives for a whole session, and its gate is taken under the lock.
var tmpLeftoversGateVerbs = map[string]bool{
	"check":         true,
	"next":          true,
	"land":          true,
	"after-landing": true,
	"suite":         true,
	"verify":        true,
}

// The tmp-leftovers check's damper: a short idle between two cures, and a
// modest cap — its cure is a plain delete of things already found to have no
// live owner, safe to retry, unlike a check whose cure reaches over the
// network or into another host.
const (
	TmpLeftoversDamperWait = 5 * time.Minute
	TmpLeftoversDamperCap  = 3

	// TmpLeftoversCureTimeout is the one deadline Cure gives itself, the
	// deletes and the go clean together, or the run's end if that is sooner.
	TmpLeftoversCureTimeout = 2 * time.Minute
)

// tmpLeftoversItem is one leftover this check found dead: its path and the
// bytes it occupies on disk.
type tmpLeftoversItem struct {
	path string
	size int64
}

// TmpLeftovers is the check that tidies this host's own dead leftovers from
// the factory's own tools — a killed bd's dolt spool files
// (TmpDir/nbs-spool-*), TmpDir/bd, and a stale Claude Code session directory
// (TmpDir/claude-0/<session>) — the same clutter that ran the VPS to 94% disk
// on 2026-09-25. "Dead" is read the way `fuser` or `lsof +D` would answer it:
// no process on this host has the path open, checked by hand over ProcDir so
// no external program has to be on PATH or exist on every host this runs on.
// It never removes a path this check does not know about, and never removes
// one that is live. ~/.cache/go-build is checked against its own, larger
// budget and cleared with `go clean -cache` — a rebuildable cache, never
// deleted by hand — rather than folded into the leftovers it deletes itself.
// Several Builders regrow it under normal load, so a cure that freed
// something ends the fault's episode (application.DoctorCureEndsEpisode):
// a regrowth is cured again when the wait allows, never left damped.
type TmpLeftovers struct {
	// TmpDir is where this host's dolt spool files, bd's own marker and
	// Claude Code's session directories are kept. Empty reads "/tmp".
	TmpDir string
	// ProcDir is where this host's running processes are read from, to tell
	// a dead leftover from a live one. Empty reads "/proc".
	ProcDir string
	// GoBuildDir is go's build cache, cleared on its own past GoBuildBudget.
	// Empty (as it is on a host with no home directory to compute it from)
	// skips this half of the check entirely.
	GoBuildDir string
	// Budget is how many bytes of dead leftovers this check allows before it
	// is faulty. The zero value reads
	// application.DefaultTmpLeftoversBudgetBytes.
	Budget int64
	// GoBuildBudget is how many bytes of GoBuildDir this check allows before
	// it is faulty. The zero value reads
	// application.DefaultGoBuildBudgetBytes.
	GoBuildBudget int64
	// GoClean runs when GoBuildDir is past its budget. Nil runs the real `go
	// clean -cache`.
	GoClean func(ctx context.Context) error

	// CureTimeout is the one deadline Cure gives itself; zero reads
	// TmpLeftoversCureTimeout.
	CureTimeout time.Duration
	// GateHolder says whose gate holds this host's gate lock now, "" for
	// nobody (hostlock.Gate.Holder). A held gate lock is a gate under way,
	// which has gaps between its go steps with no go process live: the cache
	// is left alone while it is held. Nil reads as nobody.
	GateHolder func() string

	// cured is what the last Cure removed, and curedBytes what it freed,
	// read by WayBack once Cure has run. Empty before any cure runs in this
	// process.
	cured      []string
	curedBytes int64
}

// NewTmpLeftovers is the check over tmpDir, read against the real /proc and
// the real ~/.cache/go-build, at the standard budget.
func NewTmpLeftovers(tmpDir string) *TmpLeftovers {
	t := &TmpLeftovers{TmpDir: tmpDir}
	if home, err := os.UserHomeDir(); err == nil {
		t.GoBuildDir = filepath.Join(home, ".cache", "go-build")
	}
	return t
}

// Name implements application.DoctorCheck.
func (t *TmpLeftovers) Name() string { return TmpLeftoversName }

// Probe implements application.DoctorCheck: cannot-tell when this host's own
// leftovers cannot be read at all (ProcDir missing, TmpDir's claude-0
// unreadable); faulty, naming whichever of the dead leftovers' total and
// GoBuildDir's size is past its own budget and not live; ok,
// naming the dead bytes found and any live-but-over-budget GoBuildDir left
// alone, when there are some but nothing is faulty; a plain ok otherwise.
func (t *TmpLeftovers) Probe(context.Context) (application.Verdict, string) {
	dead, err := t.deadLeftovers()
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	budget := t.budget()
	total := sumBytes(dead)

	var faults, oks []string
	if total > budget {
		faults = append(faults, fmt.Sprintf("%d bytes of dead leftovers, past the %d byte budget", total, budget))
	} else if total > 0 {
		oks = append(oks, fmt.Sprintf("%d bytes of dead leftovers found, under the %d byte budget", total, budget))
	}

	goBuildBudget := t.goBuildBudget()
	if goBuildBytes, err := t.dirSize(t.GoBuildDir); err == nil && goBuildBytes > goBuildBudget {
		live, err := t.goBuildLive()
		if err != nil {
			return application.DoctorCannotTell, err.Error()
		}
		if live {
			oks = append(oks, fmt.Sprintf("go-build is %d bytes, past the %d byte budget, but a go process is live: left alone", goBuildBytes, goBuildBudget))
		} else {
			faults = append(faults, fmt.Sprintf("go-build cache is %d bytes, past its own %d byte budget", goBuildBytes, goBuildBudget))
		}
	}

	if len(faults) > 0 {
		return application.DoctorFaulty, strings.Join(faults, "; ")
	}
	return application.DoctorOK, strings.Join(oks, "; ")
}

// Cure implements application.DoctorCheck: os.RemoveAll on exactly the dead
// leftovers Probe would find, only when their total is past budget, and
// separately `go clean -cache` on GoBuildDir, only when it is past its own
// budget and not live. Neither runs when its own half is not faulty: a check
// faulty only on GoBuildDir never touches a dead leftover still under
// budget, and the reverse; a live GoBuildDir is left for a later tick rather
// than cleaned out from under a running go build, test or vet, or a gate in
// the gap between its go steps (mw-gq6.1332), and so is a
// go clean that fails because one started meanwhile.
func (t *TmpLeftovers) Cure(ctx context.Context) error {
	ctx, cancel := cureContext(ctx, t.cureTimeout())
	defer cancel()
	dead, err := t.deadLeftovers()
	if err != nil {
		return err
	}
	budget := t.budget()

	var cured []string
	var freed int64
	if sumBytes(dead) > budget {
		for _, item := range dead {
			if cut := cutError(ctx, "it removed dead leftovers", "what it removed is gone and the rest is found again"); cut != nil {
				t.cured, t.curedBytes = cured, freed
				return cut
			}
			if err := os.RemoveAll(item.path); err != nil {
				return fmt.Errorf("removing %s: %w", item.path, err)
			}
			cured = append(cured, item.path)
			freed += item.size
		}
	}

	if goBuildBytes, err := t.dirSize(t.GoBuildDir); err == nil && goBuildBytes > t.goBuildBudget() {
		live, err := t.goBuildLive()
		if err != nil {
			return err
		}
		if !live {
			switch err := t.goClean(ctx); {
			case err == nil:
				cured = append(cured, t.GoBuildDir)
				freed += goBuildBytes
			case t.lostRaceToGo():
				// A go process started after the look above, and go clean
				// exited 1 under it: the cure lost a race with the work it
				// tidies after. That is a skip until a later tick, not a failed
				// cure that wakes the Millhand (mw-gq6.318).
			default:
				if cut := cutError(ctx, "go clean -cache ran", "the cache is rebuildable and a half-cleaned one is cleaned again"); cut != nil {
					t.cured, t.curedBytes = cured, freed
					return cut
				}
				return fmt.Errorf("go clean -cache: %w", err)
			}
		}
	}

	t.cured = cured
	t.curedBytes = freed
	return nil
}

// Damper implements application.DoctorCheck.
func (t *TmpLeftovers) Damper() (time.Duration, int) {
	return TmpLeftoversDamperWait, TmpLeftoversDamperCap
}

// QuietWhileCooling implements application.DoctorCoolingQuiet: the cure is a
// real delete or go clean, and go-build regrows fast under several Builders'
// go test, so the fault returning inside the minimum wait after a good cure
// is not news.
func (t *TmpLeftovers) QuietWhileCooling() bool { return true }

// CureEndedEpisode implements application.DoctorCureEndsEpisode: the last
// Cure freed something, so the fault's episode is over and a regrowth starts
// a new one. A cure that freed nothing — go clean losing every race to a live
// go build — is counted against the cap, so it is still damped in the end.
func (t *TmpLeftovers) CureEndedEpisode() bool { return len(t.cured) > 0 }

// WayBack implements application.DoctorCheck: there is nothing to restore —
// every path this check ever removes was already found dead, with no live
// process holding it open, or was go's own rebuildable cache.
func (t *TmpLeftovers) WayBack() string {
	if len(t.cured) == 0 {
		return "none: every leftover this check removes has no live owner, or is go's own rebuildable cache; nothing to restore"
	}
	return fmt.Sprintf("none: %d bytes freed from %s; nothing to restore, none had a live owner",
		t.curedBytes, strings.Join(t.cured, ", "))
}

// deadLeftovers is every leftover this check knows about — a dolt spool
// file, TmpDir/bd, a claude-0 session directory — that exists and has no
// live owner.
func (t *TmpLeftovers) deadLeftovers() ([]tmpLeftoversItem, error) {
	procDir := t.procDir()
	tmpDir := t.tmpDir()
	var items []tmpLeftoversItem

	spoolMatches, err := filepath.Glob(filepath.Join(tmpDir, tmpLeftoversSpoolGlob))
	if err != nil {
		return nil, fmt.Errorf("globbing %s: %w", filepath.Join(tmpDir, tmpLeftoversSpoolGlob), err)
	}
	for _, path := range spoolMatches {
		item, err := t.deadItem(procDir, path)
		if err != nil {
			return nil, err
		}
		if item != nil {
			items = append(items, *item)
		}
	}

	if item, err := t.deadItem(procDir, filepath.Join(tmpDir, tmpLeftoversBdName)); err != nil {
		return nil, err
	} else if item != nil {
		items = append(items, *item)
	}

	claudeDir := filepath.Join(tmpDir, tmpLeftoversClaudeDir)
	sessions, err := os.ReadDir(claudeDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading %s: %w", claudeDir, err)
	}
	for _, session := range sessions {
		item, err := t.deadItem(procDir, filepath.Join(claudeDir, session.Name()))
		if err != nil {
			return nil, err
		}
		if item != nil {
			items = append(items, *item)
		}
	}

	return items, nil
}

// deadItem reports path, sized, when it exists and no process on this host
// has it open; nil, no error, for a path that does not exist at all — not a
// leftover to report — or one that is live.
func (t *TmpLeftovers) deadItem(procDir, path string) (*tmpLeftoversItem, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("looking at %s: %w", path, err)
	}
	open, err := openedByAnyProcess(procDir, path)
	if err != nil {
		return nil, err
	}
	if open {
		return nil, nil
	}
	size, err := t.pathSize(path, info)
	if err != nil {
		return nil, err
	}
	return &tmpLeftoversItem{path: path, size: size}, nil
}

// pathSize is a file's size, or a directory's files' sizes summed, the way
// beadsSizeOf sums .beads.
func (t *TmpLeftovers) pathSize(path string, info os.FileInfo) (int64, error) {
	if !info.IsDir() {
		return info.Size(), nil
	}
	var total int64
	err := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		total += fi.Size()
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("sizing %s: %w", path, err)
	}
	return total, nil
}

// goBuildLive reports whether GoBuildDir should be left alone rather than
// faulted on or cleaned: something on this host has a file open under it, a
// go build/test/vet process of its own is running, the gate lock is held, or a
// mw check, next or land is running — any is reason
// enough to leave a live cache alone even past budget.
func (t *TmpLeftovers) goBuildLive() (bool, error) {
	procDir := t.procDir()
	open, err := openedByAnyProcess(procDir, t.GoBuildDir)
	if err != nil {
		return false, err
	}
	if open {
		return true, nil
	}
	// A held gate lock is a gate under way, whether or not a go process is live
	// at this instant (mw-gq6.1332).
	if t.GateHolder != nil && t.GateHolder() != "" {
		return true, nil
	}
	if live, err := anyProcessNamed(procDir, tmpLeftoversLiveGoProcessNames); err != nil || live {
		return live, err
	}
	return anyMwGateProcess(procDir)
}

// dirSize is dir's size, the way pathSize sums a directory, or an error for a
// dir that is empty, missing, or not there to size at all — a caller that
// only acts on a nil error skips it either way.
func (t *TmpLeftovers) dirSize(dir string) (int64, error) {
	if dir == "" {
		return 0, fmt.Errorf("no directory to size")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return 0, err
	}
	return t.pathSize(dir, info)
}

// lostRaceToGo reports whether a go process is live now: a failed go clean
// -cache with one running is the cure racing it, not the cure being broken.
// A /proc that cannot be read says no, so the failure is still reported.
func (t *TmpLeftovers) lostRaceToGo() bool {
	live, err := t.goBuildLive()
	return err == nil && live
}

// goClean implements the GoBuildDir half of Cure.
func (t *TmpLeftovers) goClean(ctx context.Context) error {
	if t.GoClean != nil {
		return t.GoClean(ctx)
	}
	return exec.CommandContext(ctx, "go", "clean", "-cache").Run()
}

// cureTimeout is the one deadline Cure gives itself on top of the run's.
func (t *TmpLeftovers) cureTimeout() time.Duration {
	if t.CureTimeout > 0 {
		return t.CureTimeout
	}
	return TmpLeftoversCureTimeout
}

func (t *TmpLeftovers) tmpDir() string {
	if t.TmpDir != "" {
		return t.TmpDir
	}
	return "/tmp"
}

func (t *TmpLeftovers) procDir() string {
	if t.ProcDir != "" {
		return t.ProcDir
	}
	return "/proc"
}

// budget is the byte count Probe faults past for the dead leftovers' total.
func (t *TmpLeftovers) budget() int64 {
	if t.Budget > 0 {
		return t.Budget
	}
	return application.DefaultTmpLeftoversBudgetBytes
}

// goBuildBudget is the byte count Probe faults past for GoBuildDir's size.
func (t *TmpLeftovers) goBuildBudget() int64 {
	if t.GoBuildBudget > 0 {
		return t.GoBuildBudget
	}
	return application.DefaultGoBuildBudgetBytes
}

// sumBytes is every item's size, added up.
func sumBytes(items []tmpLeftoversItem) int64 {
	var total int64
	for _, item := range items {
		total += item.size
	}
	return total
}

// openedByAnyProcess reports whether any process under procDir has target, or
// (target being a directory) anything under it, open — the same question
// `fuser` or `lsof +D` answers, asked by hand over /proc's own shape
// (<procDir>/<pid>/fd/<fd> is a symlink to what that file descriptor names)
// so this check needs no external program on PATH, and is exercised in tests
// against a plain directory tree standing in for /proc, no real process
// required.
func openedByAnyProcess(procDir, target string) (bool, error) {
	entries, err := os.ReadDir(procDir)
	if err != nil {
		return false, fmt.Errorf("listing %s: %w", procDir, err)
	}
	target = filepath.Clean(target)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		fdDir := filepath.Join(procDir, entry.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			// A process that exited mid-scan, or whose fd directory this
			// check may not read, holds nothing open as far as it can tell.
			continue
		}
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			link = filepath.Clean(link)
			if link == target || strings.HasPrefix(link, target+string(filepath.Separator)) {
				return true, nil
			}
		}
	}
	return false, nil
}

// anyProcessNamed reports whether any process under procDir is one of names,
// read from <procDir>/<pid>/comm the way `ps` reads a process's own name. A
// process this check cannot read the comm file of, exited mid-scan or
// otherwise, is treated as not one of names.
func anyProcessNamed(procDir string, names map[string]bool) (bool, error) {
	entries, err := os.ReadDir(procDir)
	if err != nil {
		return false, fmt.Errorf("listing %s: %w", procDir, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		comm, err := os.ReadFile(filepath.Join(procDir, entry.Name(), "comm"))
		if err != nil {
			continue
		}
		if names[strings.TrimSpace(string(comm))] {
			return true, nil
		}
	}
	return false, nil
}

// anyMwGateProcess reports whether any process under procDir is a mw running one
// of tmpLeftoversGateVerbs, read from <procDir>/<pid>/comm and cmdline: a gate's
// parts, and the gaps between them, are one such process's. A process whose
// files this check cannot read is treated as not one.
func anyMwGateProcess(procDir string) (bool, error) {
	entries, err := os.ReadDir(procDir)
	if err != nil {
		return false, fmt.Errorf("listing %s: %w", procDir, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		comm, err := os.ReadFile(filepath.Join(procDir, entry.Name(), "comm"))
		if err != nil || strings.TrimSpace(string(comm)) != "mw" {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join(procDir, entry.Name(), "cmdline"))
		if err != nil {
			continue
		}
		args := strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")
		if len(args) > 1 && tmpLeftoversGateVerbs[args[1]] {
			return true, nil
		}
	}
	return false, nil
}
