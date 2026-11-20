package application

import (
	"context"
	"fmt"
	"io"
	"time"
)

// GoCacheTidyAge is how long a file of Go's build cache may go unused before mw
// host tidy go-cache deletes it. Go itself keeps an entry for 5 days after its
// last use; a factory host's Builders write about 16 GB of build cache a day, so
// five days settles near 110 GB (mw-gq6.1537). PROVISIONAL, for the Governor to
// confirm.
const GoCacheTidyAge = 24 * time.Hour

// DefaultGoCacheWarnBytes is how big a host's Go build cache may be before mw
// doctor's go-cache-size check names the host: 30 GB.
const DefaultGoCacheWarnBytes int64 = 30_000_000_000

// GoCacheEntry is one file of Go's build cache as the tidy reads it: its path,
// its size, and when Go last used it (Go touches a file's modification time each
// time it uses it).
type GoCacheEntry struct {
	Path string
	Size int64
	Used time.Time
}

// GoCacheDisk is the part of the disk the Go-cache tidy works through.
type GoCacheDisk interface {
	// Entries lists every file of the cache under dir that Go itself could
	// recreate (its action and data files), and no other. A dir that does not
	// exist has none and is no error.
	Entries(ctx context.Context, dir string) ([]GoCacheEntry, error)
	// Remove deletes one file Entries listed.
	Remove(path string) error
}

// GoCacheTidyResult is what one run of GoCacheTidy found.
type GoCacheTidyResult struct {
	// Removed and Freed are the files deleted (or, in a dry run, that would be)
	// and their bytes; Kept and KeptBytes are the files used within the age.
	Removed, Kept    int
	Freed, KeptBytes int64
}

// GoCacheTidy deletes the files of Go's build cache that have not been used for
// over Age. It touches nothing else under Dir, and a file Go needs again is
// rebuilt by Go.
type GoCacheTidy struct {
	Dir    string
	Disk   GoCacheDisk
	Age    time.Duration // zero reads GoCacheTidyAge
	Now    func() time.Time
	DryRun bool
	Out    io.Writer // nil says nothing
}

// Run tidies the cache.
func (t GoCacheTidy) Run(ctx context.Context) (GoCacheTidyResult, error) {
	var res GoCacheTidyResult
	age := t.Age
	if age <= 0 {
		age = GoCacheTidyAge
	}
	now := time.Now
	if t.Now != nil {
		now = t.Now
	}
	entries, err := t.Disk.Entries(ctx, t.Dir)
	if err != nil {
		return res, fmt.Errorf("reading the Go build cache %s: %w", t.Dir, err)
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
		fmt.Fprintf(t.Out, "go cache %s: %s %d files, %d bytes, unused for over %s; kept %d files, %d bytes\n",
			t.Dir, verb, res.Removed, res.Freed, age, res.Kept, res.KeptBytes)
	}
	if failed > 0 {
		return res, fmt.Errorf("%d files of the Go build cache %s could not be removed", failed, t.Dir)
	}
	return res, nil
}

// GoCacheSizeName is what the check is called: in the log, and on the command
// line as `mw doctor go-cache-size`.
const GoCacheSizeName = "go-cache-size"

// GoCacheSizeCheck is the mw doctor check that names a host whose Go build
// cache is over Warn bytes (30 GB by default). Its cure is the tidy: it deletes
// what has not been used for a day, which is rebuildable, never a thing a Builder
// keeps. The daily mw-host-tidy timer keeps it down; this names a host where it
// has not (mw-gq6.1537).
type GoCacheSizeCheck struct {
	Host string
	Dir  string
	Disk GoCacheDisk
	// Warn is the size past which the check is faulty. Zero reads
	// DefaultGoCacheWarnBytes.
	Warn int64
	Now  func() time.Time

	cured      int
	curedBytes int64
}

var _ DoctorCheck = (*GoCacheSizeCheck)(nil)
var _ DoctorCureEndsEpisode = (*GoCacheSizeCheck)(nil)
var _ DoctorCoolingQuiet = (*GoCacheSizeCheck)(nil)

// Name implements DoctorCheck.
func (c *GoCacheSizeCheck) Name() string { return GoCacheSizeName }

func (c *GoCacheSizeCheck) warn() int64 {
	if c.Warn > 0 {
		return c.Warn
	}
	return DefaultGoCacheWarnBytes
}

// Probe implements DoctorCheck: faulty, naming the host and the size, when the
// cache is over the limit; cannot-tell when it cannot be read.
func (c *GoCacheSizeCheck) Probe(ctx context.Context) (Verdict, string) {
	entries, err := c.Disk.Entries(ctx, c.Dir)
	if err != nil {
		return DoctorCannotTell, err.Error()
	}
	var total int64
	for _, e := range entries {
		total += e.Size
	}
	if total > c.warn() {
		return DoctorFaulty, fmt.Sprintf("%s: the Go build cache %s is %.1f GB, over %.0f GB; the cure deletes what has not been used for %s (mw host tidy go-cache)",
			c.Host, c.Dir, float64(total)/1e9, float64(c.warn())/1e9, GoCacheTidyAge)
	}
	return DoctorOK, ""
}

// Cure implements DoctorCheck: the tidy.
func (c *GoCacheSizeCheck) Cure(ctx context.Context) error {
	res, err := GoCacheTidy{Dir: c.Dir, Disk: c.Disk, Now: c.Now}.Run(ctx)
	c.cured, c.curedBytes = res.Removed, res.Freed
	return err
}

// Damper implements DoctorCheck: an hour between cures, three to an episode.
func (c *GoCacheSizeCheck) Damper() (time.Duration, int) { return time.Hour, 3 }

// CureEndedEpisode implements DoctorCureEndsEpisode: a cure that freed files
// ended the fault; Builders regrow the cache, and that is a new fault.
func (c *GoCacheSizeCheck) CureEndedEpisode() bool { return c.cured > 0 }

// QuietWhileCooling implements DoctorCoolingQuiet: Builders refill the cache.
func (c *GoCacheSizeCheck) QuietWhileCooling() bool { return true }

// WayBack implements DoctorCheck.
func (c *GoCacheSizeCheck) WayBack() string {
	return fmt.Sprintf("none: the files deleted (%d, %d bytes freed in the last cure) are Go's own rebuildable cache; the next go build makes them again", c.cured, c.curedBytes)
}
