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

var _ application.DoctorCheck = (*TmpLeftovers)(nil)

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

// The tmp-leftovers check's damper: a short idle between two cures, and a
// modest cap — its cure is a plain delete of things already found to have no
// live owner, safe to retry, unlike a check whose cure reaches over the
// network or into another host.
const (
	TmpLeftoversDamperWait = 5 * time.Minute
	TmpLeftoversDamperCap  = 3
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
// one that is live. ~/.cache/go-build is checked against the same budget on
// its own and cleared with `go clean -cache` — a rebuildable cache, never
// deleted by hand — rather than folded into the leftovers it deletes itself.
type TmpLeftovers struct {
	// TmpDir is where this host's dolt spool files, bd's own marker and
	// Claude Code's session directories are kept. Empty reads "/tmp".
	TmpDir string
	// ProcDir is where this host's running processes are read from, to tell
	// a dead leftover from a live one. Empty reads "/proc".
	ProcDir string
	// GoBuildDir is go's build cache, cleared on its own past Budget. Empty
	// (as it is on a host with no home directory to compute it from) skips
	// this half of the check entirely.
	GoBuildDir string
	// Budget is how many bytes of dead leftovers, and separately how many
	// bytes of GoBuildDir, this check allows before it is faulty. The zero
	// value reads application.DefaultTmpLeftoversBudgetBytes.
	Budget int64
	// GoClean runs when GoBuildDir is past its budget. Nil runs the real `go
	// clean -cache`.
	GoClean func(ctx context.Context) error

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
// GoBuildDir's size is past its own share of the budget; ok, naming the dead
// bytes found, when there are some but they are under budget; a plain ok
// otherwise.
func (t *TmpLeftovers) Probe(context.Context) (application.Verdict, string) {
	dead, err := t.deadLeftovers()
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	budget := t.budget()
	total := sumBytes(dead)

	var faults []string
	if total > budget {
		faults = append(faults, fmt.Sprintf("%d bytes of dead leftovers, past the %d byte budget", total, budget))
	}
	if goBuildBytes, err := t.dirSize(t.GoBuildDir); err == nil && goBuildBytes > budget {
		faults = append(faults, fmt.Sprintf("go-build cache is %d bytes, past its own %d byte budget", goBuildBytes, budget))
	}
	if len(faults) > 0 {
		return application.DoctorFaulty, strings.Join(faults, "; ")
	}
	if total > 0 {
		return application.DoctorOK, fmt.Sprintf("%d bytes of dead leftovers found, under the %d byte budget", total, budget)
	}
	return application.DoctorOK, ""
}

// Cure implements application.DoctorCheck: os.RemoveAll on exactly the dead
// leftovers Probe would find, only when their total is past budget, and
// separately `go clean -cache` on GoBuildDir, only when it is past budget on
// its own. Neither runs when its own half is not faulty: a check faulty only
// on GoBuildDir never touches a dead leftover still under budget, and the
// reverse.
func (t *TmpLeftovers) Cure(ctx context.Context) error {
	dead, err := t.deadLeftovers()
	if err != nil {
		return err
	}
	budget := t.budget()

	var cured []string
	var freed int64
	if sumBytes(dead) > budget {
		for _, item := range dead {
			if err := os.RemoveAll(item.path); err != nil {
				return fmt.Errorf("removing %s: %w", item.path, err)
			}
			cured = append(cured, item.path)
			freed += item.size
		}
	}

	if goBuildBytes, err := t.dirSize(t.GoBuildDir); err == nil && goBuildBytes > budget {
		if err := t.goClean(ctx); err != nil {
			return fmt.Errorf("go clean -cache: %w", err)
		}
		cured = append(cured, t.GoBuildDir)
		freed += goBuildBytes
	}

	t.cured = cured
	t.curedBytes = freed
	return nil
}

// Damper implements application.DoctorCheck.
func (t *TmpLeftovers) Damper() (time.Duration, int) { return TmpLeftoversDamperWait, TmpLeftoversDamperCap }

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

// goClean implements the GoBuildDir half of Cure.
func (t *TmpLeftovers) goClean(ctx context.Context) error {
	if t.GoClean != nil {
		return t.GoClean(ctx)
	}
	return exec.CommandContext(ctx, "go", "clean", "-cache").Run()
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

// budget is the byte count Probe faults past, for the dead leftovers' total
// and, on its own, GoBuildDir's size.
func (t *TmpLeftovers) budget() int64 {
	if t.Budget > 0 {
		return t.Budget
	}
	return application.DefaultTmpLeftoversBudgetBytes
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
