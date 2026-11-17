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

// The beads-size check's damper: the one cure is the daily collection, run once;
// if .beads is still over after it, or it failed, that attempt is all an episode
// ever spends — the second probe of the same episode finds it already capped,
// and mw doctor never runs Cure again until the probe next says ok.
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
// dispatch could run the disk full and stop working silently. Its one cure is
// the collection the daily job runs, when the last is older than CollectEvery
// (never the squash, which is the Governor's word); a probe still faulty after
// it, or with no cure to run, is tried once, fails once, and the episode sits
// damped until the probe next says ok; the note that damping writes is what
// wakes the Millhand to look.
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

	// Collect runs the collection the daily job runs (the tracker's GC, never
	// the squash), through the quiet wait every bd writer uses, and records when
	// it ran, so that the next LastCollected reads it. With it nil the check has
	// no cure to run and says so, as it always did.
	Collect func(context.Context) error
	// LastCollected is when this host's last collection ran (the daily job's
	// own note); the zero time says never, or not readable, and reads as stale.
	LastCollected func(context.Context) (time.Time, error)
	// CollectEvery is how old the last collection must be before the cure runs
	// another; zero reads DefaultBeadsCollectEvery.
	CollectEvery time.Duration
	// Now is the clock; nil reads the real one.
	Now func() time.Time
	// Sizer reads the size of .beads; nil walks it, as Probe always did.
	Sizer func(context.Context) (int64, error)

	// outcome is what the last cure that ran the collection found, for WayBack.
	outcome string
}

// DefaultBeadsCollectEvery is how old the last collection must be before the
// cure runs one: far enough from the daily cadence that the cure is not the
// daily job twice, near enough that a store which crossed its budget between
// two collections is not left alarmed for most of a day (mw-gq6.1403).
// PROVISIONAL: the Governor is to confirm six hours.
const DefaultBeadsCollectEvery = 6 * time.Hour

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
func (b *BeadsSize) Probe(ctx context.Context) (application.Verdict, string) {
	path := filepath.Join(b.Dir, beadsSizeDir)
	size, err := b.size(ctx)
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	budget := b.budget()
	if size > budget {
		return application.DoctorFaulty, fmt.Sprintf("%s is %d bytes, past the %d byte budget", path, size, budget)
	}
	return application.DoctorOK, ""
}

// size reads .beads: through Sizer when one is given, else by walking it.
func (b *BeadsSize) size(ctx context.Context) (int64, error) {
	if b.Sizer != nil {
		return b.Sizer(ctx)
	}
	return beadsSizeOf(filepath.Join(b.Dir, beadsSizeDir))
}

func (b *BeadsSize) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

func (b *BeadsSize) collectEvery() time.Duration {
	if b.CollectEvery > 0 {
		return b.CollectEvery
	}
	return DefaultBeadsCollectEvery
}

// Cure implements application.DoctorCheck. Past budget with the last
// collection older than the interval, it runs the collection the daily job
// runs, once, and reads .beads again: it is cured when that is under budget,
// and says both sizes and when the collection ran when it is not. Inside the
// interval it runs nothing, and the error says what was found. With no
// collector (a check built without one) there is no cure to run.
func (b *BeadsSize) Cure(ctx context.Context) error {
	if b.Collect == nil || b.LastCollected == nil {
		return b.beadsSizeNoCure()
	}
	now := b.now().UTC()
	last, _ := b.LastCollected(ctx)
	if !last.IsZero() && now.Sub(last) < b.collectEvery() {
		return fmt.Errorf("no cure: the last collection ran at %s, %s ago, inside the %s before another; %w",
			last.UTC().Format(time.RFC3339), now.Sub(last).Round(time.Minute), b.collectEvery(), b.beadsSizeNoCure())
	}
	before, err := b.size(ctx)
	if err != nil {
		return err
	}
	at := now.Format(time.RFC3339)
	if err := b.Collect(ctx); err != nil {
		return fmt.Errorf("the collection at %s failed with .beads at %d bytes: %w", at, before, err)
	}
	after, err := b.size(ctx)
	if err != nil {
		return fmt.Errorf("the collection ran at %s with .beads at %d bytes; reading it again failed: %w", at, before, err)
	}
	b.outcome = fmt.Sprintf("the collection ran at %s: .beads %d bytes before, %d after", at, before, after)
	if after > b.budget() {
		return fmt.Errorf("%s, still past the %d byte budget; %w", b.outcome, b.budget(), b.beadsSizeNoCure())
	}
	return nil
}

// Damper implements application.DoctorCheck.
func (b *BeadsSize) Damper() (time.Duration, int) { return BeadsSizeDamperWait, BeadsSizeDamperCap }

// WayBack implements application.DoctorCheck: this check never changes
// anything, so there is nothing to undo; on the home it names the cure that
// does run, elsewhere a person clears space by hand.
func (b *BeadsSize) WayBack() string {
	if b.outcome != "" {
		return "none: the collection only reclaims space and deletes no work; " + b.outcome
	}
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
