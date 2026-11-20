package application

import (
	"context"
	"fmt"
	"io"
	"time"
)

// TmpTidyAge is how long something under the system temp (/tmp) may go
// untouched before mw host tidy tmp deletes it (mw-d40q096). PROVISIONAL, for
// the Governor to confirm; it is the age of the Go-cache tidy.
const TmpTidyAge = 24 * time.Hour

// DefaultTmpWarnBytes is how big /tmp may be before mw doctor's tmp-size check
// names the host: 20 GB.
const DefaultTmpWarnBytes int64 = 20_000_000_000

// TmpEntry is one thing the tmp tidy may delete: a file of Node's compile cache
// or Playwright's transform cache, or the whole directory a Go test left (the
// entry's Used is the newest touch of anything inside it).
type TmpEntry struct {
	Path string
	Size int64
	Used time.Time
}

// TmpDisk is the part of the disk the tmp tidy works through.
type TmpDisk interface {
	// Entries lists what under dir the tidy may delete, owned by the user running
	// it: the files of node-compile-cache and playwright-transform-cache-*, and
	// each directory named Test*, and nothing else. A dir that does not exist has
	// none and is no error.
	Entries(ctx context.Context, dir string) ([]TmpEntry, error)
	// Used totals the bytes of the files under dir that the user owns.
	Used(ctx context.Context, dir string) (int64, error)
	// Remove deletes one entry Entries listed, with what is inside it.
	Remove(path string) error
}

// TmpTidyResult is what one run of TmpTidy found.
type TmpTidyResult struct {
	Removed, Kept    int
	Freed, KeptBytes int64
}

// TmpTidy deletes what a tool left in the system temp and has not touched for
// over Age: Node's compile cache and Playwright's transform cache, which only
// grow, and the directories of Go tests whose process was killed before it
// could remove them. All of it is rebuilt or made again when needed.
type TmpTidy struct {
	Dir    string
	Disk   TmpDisk
	Age    time.Duration // zero reads TmpTidyAge
	Now    func() time.Time
	DryRun bool
	Out    io.Writer // nil says nothing
}

// Run tidies the directory.
func (t TmpTidy) Run(ctx context.Context) (TmpTidyResult, error) {
	var res TmpTidyResult
	age := t.Age
	if age <= 0 {
		age = TmpTidyAge
	}
	now := time.Now
	if t.Now != nil {
		now = t.Now
	}
	entries, err := t.Disk.Entries(ctx, t.Dir)
	if err != nil {
		return res, fmt.Errorf("reading %s: %w", t.Dir, err)
	}
	cutoff := now().Add(-age)
	var failed int
	for _, e := range entries {
		if !e.Used.Before(cutoff) {
			res.Kept++
			res.KeptBytes += e.Size
			continue
		}
		if !t.DryRun {
			if err := t.Disk.Remove(e.Path); err != nil {
				failed++
				continue
			}
		}
		res.Removed++
		res.Freed += e.Size
	}
	verb := "removed"
	if t.DryRun {
		verb = "would remove"
	}
	if t.Out != nil {
		fmt.Fprintf(t.Out, "tmp %s: %s %d entries, %d bytes, untouched for over %s; kept %d entries, %d bytes\n",
			t.Dir, verb, res.Removed, res.Freed, age, res.Kept, res.KeptBytes)
	}
	if failed > 0 {
		return res, fmt.Errorf("%d entries of %s could not be removed", failed, t.Dir)
	}
	return res, nil
}

// TmpSizeName is what the check is called: in the log, and on the command line
// as `mw doctor tmp-size`.
const TmpSizeName = "tmp-size"

// TmpSizeCheck is the mw doctor check that names a host whose /tmp (Dir) holds
// more than Warn bytes (20 GB by default) of the user's files. Its cure is the
// tmp tidy; the daily mw-host-tidy timer keeps it down, and this names a host
// where it has not (mw-d40q096).
type TmpSizeCheck struct {
	Host string
	Dir  string
	Disk TmpDisk
	// Warn is the size past which the check is faulty. Zero reads
	// DefaultTmpWarnBytes.
	Warn int64
	Now  func() time.Time

	cured      int
	curedBytes int64
}

var _ DoctorCheck = (*TmpSizeCheck)(nil)
var _ DoctorCureEndsEpisode = (*TmpSizeCheck)(nil)
var _ DoctorCoolingQuiet = (*TmpSizeCheck)(nil)

// Name implements DoctorCheck.
func (c *TmpSizeCheck) Name() string { return TmpSizeName }

func (c *TmpSizeCheck) warn() int64 {
	if c.Warn > 0 {
		return c.Warn
	}
	return DefaultTmpWarnBytes
}

// Probe implements DoctorCheck: faulty, naming the host, the directory and the
// size, when it is over the limit; cannot-tell when it cannot be read.
func (c *TmpSizeCheck) Probe(ctx context.Context) (Verdict, string) {
	total, err := c.Disk.Used(ctx, c.Dir)
	if err != nil {
		return DoctorCannotTell, err.Error()
	}
	if total > c.warn() {
		return DoctorFaulty, fmt.Sprintf("%s: %s holds %.1f GB, over %.0f GB; the cure deletes what has not been touched for %s of Node's and Playwright's caches and of Go test directories (mw host tidy tmp)",
			c.Host, c.Dir, float64(total)/1e9, float64(c.warn())/1e9, TmpTidyAge)
	}
	return DoctorOK, ""
}

// Cure implements DoctorCheck: the tidy.
func (c *TmpSizeCheck) Cure(ctx context.Context) error {
	res, err := TmpTidy{Dir: c.Dir, Disk: c.Disk, Now: c.Now}.Run(ctx)
	c.cured, c.curedBytes = res.Removed, res.Freed
	return err
}

// Damper implements DoctorCheck: an hour between cures, three to an episode.
func (c *TmpSizeCheck) Damper() (time.Duration, int) { return time.Hour, 3 }

// CureEndedEpisode implements DoctorCureEndsEpisode: a cure that freed entries
// ended the fault; the tools regrow it, and that is a new fault.
func (c *TmpSizeCheck) CureEndedEpisode() bool { return c.cured > 0 }

// QuietWhileCooling implements DoctorCoolingQuiet: tests and tools refill /tmp.
func (c *TmpSizeCheck) QuietWhileCooling() bool { return true }

// WayBack implements DoctorCheck.
func (c *TmpSizeCheck) WayBack() string {
	return fmt.Sprintf("none: the entries deleted (%d, %d bytes freed in the last cure) are caches the tools rebuild and the leftovers of tests that ended before they could clean up", c.cured, c.curedBytes)
}
