package doctor

import (
	"context"
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

// beadsSizeCacheGlobs find the git-remote-cache clones Dolt keeps beside the
// vault's database, under either layout: embeddeddolt for a bd that opens the
// database itself, dolt for a host that serves beads. The same two places
// infrastructure/beads's sync repacks.
var beadsSizeCacheGlobs = []string{
	"embeddeddolt/*/.dolt/git-remote-cache/*/repo.git",
	"dolt/*/.dolt/git-remote-cache/*/repo.git",
}

// beadsSizeNoCure is what Cure always returns: a repack that would shrink
// .beads deletes packs, held back by the Doctor-checks approval (mw-6ww.40),
// so this check can only ever report the fault, never fix it. It says what
// it found, and what already relieves this without a cure, so a person
// reading it is not left thinking nothing is being done: a crowded
// git-remote-cache repacks itself on the next sync, and the tracker's own GC
// keeps to its cadence regardless.
func (b *BeadsSize) beadsSizeNoCure() error {
	if b.Home {
		return fmt.Errorf(
			"no cure: changed nothing; %s; the tracker's own GC runs at least every %dh but frees nothing while every commit is kept; %s",
			b.cachesFound(), int(application.DefaultGCInterval.Hours()), b.squashCure(),
		)
	}
	return fmt.Errorf(
		"no cure: changed nothing; %s; a crowded git-remote-cache now repacks itself with the next sync; "+
			"the tracker's own GC also runs at least every %dh; a person clears space by hand only if both still fall behind",
		b.cachesFound(), int(application.DefaultGCInterval.Hours()),
	)
}

// squashCure names the cure for the home's store past budget: what fills it is
// the database's history, every commit reachable, so no GC, full or not, frees
// it; only the daily collection's squash does (on a copy of the home's store,
// 4.85 GB to 0.85 GB, where a full Dolt GC freed 21 KB: mw-gq6.615). That
// squash cannot be undone, so it runs only on the Governor's word.
func (b *BeadsSize) squashCure() string {
	if b.Squash {
		return "the cure is the daily collection's squash of the beads history (sync.beads_squash is on), which cannot be undone"
	}
	return "the cure is the daily collection's squash of the beads history, off until the Governor turns sync.beads_squash on (mw-gq6.504)"
}

// cachesFound says how many git-remote-cache clones the vault holds and how
// many bytes they take, or that none was found, so the log tells a person
// whether the repack has anything to work on.
func (b *BeadsSize) cachesFound() string {
	var caches []string
	for _, glob := range beadsSizeCacheGlobs {
		found, _ := filepath.Glob(filepath.Join(b.Dir, beadsSizeDir, glob))
		caches = append(caches, found...)
	}
	if len(caches) == 0 {
		return fmt.Sprintf("no git-remote-cache found under %s", filepath.Join(b.Dir, beadsSizeDir))
	}
	var total int64
	for _, cache := range caches {
		if size, err := beadsSizeOf(cache); err == nil {
			total += size
		}
	}
	return fmt.Sprintf("found %d git-remote-cache clone(s) holding %d bytes", len(caches), total)
}

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
	// mw doctor sets it from config.BeadsBudgetBytes; the zero value reads
	// application.DefaultBeadsBudgetBytes.
	Budget int64
	// Home says this host keeps the one beads database, so that its daily
	// collection's squash of the history is the cure for a store past budget
	// (mw-gq6.615). mw doctor sets it on the host that serves beads.
	Home bool
	// Squash is sync.beads_squash: whether that squash may run at all, which
	// is the Governor's word (mw-gq6.504).
	Squash bool
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

// Cure implements application.DoctorCheck: there is none; the error says what
// was found.
func (b *BeadsSize) Cure(context.Context) error { return b.beadsSizeNoCure() }

// Damper implements application.DoctorCheck.
func (b *BeadsSize) Damper() (time.Duration, int) { return BeadsSizeDamperWait, BeadsSizeDamperCap }

// WayBack implements application.DoctorCheck: this check never changes
// anything, so there is nothing to undo; on the home it names the cure that
// does run, elsewhere a person clears space by hand.
func (b *BeadsSize) WayBack() string {
	if b.Home {
		return "none: this check changes nothing; " + b.squashCure()
	}
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
