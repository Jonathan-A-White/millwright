package doctor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*BeadsSize)(nil)

// BeadsSizeName is what the check is called: in the log, and on the command
// line as `mw doctor beads-size`.
const BeadsSizeName = "beads-size"

// beadsSizeDir is the one directory under the vault this check sizes — the
// same one application/status.go's beadsLine warns against.
const beadsSizeDir = ".beads"

// The beads-size check's damper: there is no cure to retry, so one failed
// cure attempt is all an episode ever spends — the second probe of the same
// episode finds it already capped, and mw doctor never runs Cure again until
// the probe next says ok.
const (
	BeadsSizeDamperWait = 0 * time.Second
	BeadsSizeDamperCap  = 1
)

// errBeadsSizeNoCure is what Cure always returns: a repack that would shrink
// .beads deletes packs, held back by the Doctor-checks approval (mw-6ww.40),
// so this check can only ever report the fault, never fix it.
var errBeadsSizeNoCure = errors.New("no cure: repacking .beads would delete packs; a person clears space by hand")

// BeadsSize is the check that watches this host's own .beads against the
// same budget mw status warns against: past it, an unattended sync or
// dispatch could run the disk full and stop working silently. It never
// cures — there is nothing safe to automate here — so a faulty probe is
// tried once, fails once, and the episode sits damped until the probe next
// says ok; the note that damping writes is what wakes the Millhand to look.
type BeadsSize struct {
	// Dir is the vault's directory.
	Dir string
	// Budget is how many bytes .beads may hold before this check is faulty.
	// The zero value reads application.DefaultBeadsBudgetBytes.
	Budget int64
}

// NewBeadsSize is the check over the vault at dir, against the standard
// budget.
func NewBeadsSize(dir string) *BeadsSize { return &BeadsSize{Dir: dir} }

// Name implements application.DoctorCheck.
func (b *BeadsSize) Name() string { return BeadsSizeName }

// Probe implements application.DoctorCheck: the size of <vault>/.beads,
// walked file by file the way application.TrackerNotes.Size is. Faulty past
// the budget, the reason naming the bytes found and the budget; cannot-tell
// when the vault or .beads itself is not there to size at all — never faulty
// on a fresh or misconfigured vault, only on one this check can actually
// measure.
func (b *BeadsSize) Probe(context.Context) (application.Verdict, string) {
	path := filepath.Join(b.Dir, beadsSizeDir)
	size, err := beadsSizeOf(path)
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	budget := b.budget()
	if size > budget {
		return application.DoctorFaulty, fmt.Sprintf("%s is %d bytes, past the %d byte budget", path, size, budget)
	}
	return application.DoctorOK, ""
}

// Cure implements application.DoctorCheck: there is none.
func (b *BeadsSize) Cure(context.Context) error { return errBeadsSizeNoCure }

// Damper implements application.DoctorCheck.
func (b *BeadsSize) Damper() (time.Duration, int) { return BeadsSizeDamperWait, BeadsSizeDamperCap }

// WayBack implements application.DoctorCheck: nothing ever changes, so there
// is nothing to undo.
func (b *BeadsSize) WayBack() string {
	return "none: no cure runs; a person clears space in .beads by hand"
}

// budget is the byte count Probe faults past.
func (b *BeadsSize) budget() int64 {
	if b.Budget > 0 {
		return b.Budget
	}
	return application.DefaultBeadsBudgetBytes
}

// beadsSizeOf is every byte dir's files occupy on disk, summed the way
// infrastructure/beads's own Gateway.Size does. Unlike that adapter, a dir
// that is not there at all is reported, not read as empty: a vault or
// .beads this check cannot find is cannot-tell, never a clean bill of
// health.
func beadsSizeOf(dir string) (int64, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, fmt.Errorf("%s not found", dir)
		}
		return 0, fmt.Errorf("sizing %s: %w", dir, err)
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("%s is not a directory", dir)
	}

	var total int64
	err = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
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
		return 0, fmt.Errorf("sizing %s: %w", dir, err)
	}
	return total, nil
}
