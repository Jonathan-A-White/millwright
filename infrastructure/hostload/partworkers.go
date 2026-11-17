package hostload

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// The flags of the command line cmd/mw's partProcess gives a part worker that
// takes no merge slot: mw check <story> --part N --part-worker --no-slot
// (mw-gq6.1422).
const (
	partWorkerFlag = "--part-worker"
	noSlotFlag     = "--no-slot"
)

// partWorkers is how many of the factory's own part workers that take no merge
// slot are running their part: the mw processes whose command line is mw check
// ... --part-worker --no-slot and that have a child, the part's command. A worker
// with no child is still waiting for a test-runner slot, or done with its command,
// and keeps no load up. What cannot be read counts as no worker.
func (p Proc) partWorkers() int {
	dir := p.ProcDir
	if dir == "" {
		dir = "/proc"
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name(), "cmdline"))
		if err != nil || !isNoSlotPartWorker(strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")) {
			continue
		}
		if hasAChild(filepath.Join(dir, entry.Name(), "task")) {
			count++
		}
	}
	return count
}

// isNoSlotPartWorker says whether args are the command line of a part worker
// that takes no merge slot.
func isNoSlotPartWorker(args []string) bool {
	if len(args) < 2 || filepath.Base(args[0]) != "mw" || args[1] != "check" {
		return false
	}
	return slices.Contains(args, partWorkerFlag) && slices.Contains(args, noSlotFlag)
}

// hasAChild says whether any thread under a process's task directory started a
// child that is still running (task/<tid>/children): Go starts a command from
// whichever thread it runs on.
func hasAChild(tasks string) bool {
	entries, err := os.ReadDir(tasks)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		kids, err := os.ReadFile(filepath.Join(tasks, entry.Name(), "children"))
		if err == nil && strings.TrimSpace(string(kids)) != "" {
			return true
		}
	}
	return false
}
