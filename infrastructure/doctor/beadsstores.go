package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*BeadsStores)(nil)

// BeadsStoresName is what the check is called: in the log, and on the command
// line as `mw doctor beads-stores`.
const BeadsStoresName = "beads-stores"

// The two stores bd keeps under .beads: dolt is the one a vault in server mode
// holds, embeddeddolt the one a bd run without BEADS_DOLT_* makes beside it.
const (
	beadsStoresServer   = "dolt"
	beadsStoresEmbedded = "embeddeddolt"
)

// The beads-stores check's damper: as with beads-size there is no cure to
// retry, so the second faulty run finds the episode capped and writes the
// note that wakes the Millhand.
const (
	BeadsStoresDamperWait = 0 * time.Second
	BeadsStoresDamperCap  = 1
)

// BeadsStores is the check for a second, empty database beside the vault's
// own: a bd run in a server-mode vault without BEADS_DOLT_* (a shell that
// skipped beads.env, a unit) exits 0 and makes .beads/embeddeddolt beside
// .beads/dolt, which nobody notices. It looks only at the files of the vault,
// never at the environment — mw doctor may itself lack BEADS_DOLT_*. It never
// cures: removing a store is a person's call.
type BeadsStores struct {
	// Dir is the vault's directory.
	Dir string
}

// NewBeadsStores is the check over the vault at dir.
func NewBeadsStores(dir string) *BeadsStores { return &BeadsStores{Dir: dir} }

// Name implements application.DoctorCheck.
func (b *BeadsStores) Name() string { return BeadsStoresName }

// Probe implements application.DoctorCheck: faulty when both stores are
// there, the reason naming the stray one and what to do about it; cannot-tell
// when .beads itself is not there.
func (b *BeadsStores) Probe(context.Context) (application.Verdict, string) {
	beads := filepath.Join(b.Dir, beadsSizeDir)
	if info, err := os.Stat(beads); err != nil || !info.IsDir() {
		return application.DoctorCannotTell, fmt.Sprintf("%s not found", beads)
	}
	stray := filepath.Join(beads, beadsStoresEmbedded)
	if !isDir(filepath.Join(beads, beadsStoresServer)) || !isDir(stray) {
		return application.DoctorOK, ""
	}
	return application.DoctorFaulty, fmt.Sprintf(
		"%s sits beside %s: a bd run without BEADS_DOLT_* made a second database; "+
			"check the embeddeddolt store is empty, remove it, and source ~/.config/mw/beads.env before any bd",
		stray, filepath.Join(beads, beadsStoresServer))
}

// errBeadsStoresNoCure is what Cure always returns: deleting a database is
// not something to automate.
var errBeadsStoresNoCure = fmt.Errorf(
	"no cure: a person checks the embeddeddolt store is empty and removes it by hand, " +
		"then sources ~/.config/mw/beads.env before any bd")

// Cure implements application.DoctorCheck: there is none.
func (b *BeadsStores) Cure(context.Context) error { return errBeadsStoresNoCure }

// Damper implements application.DoctorCheck.
func (b *BeadsStores) Damper() (time.Duration, int) {
	return BeadsStoresDamperWait, BeadsStoresDamperCap
}

// WayBack implements application.DoctorCheck: nothing ever changes.
func (b *BeadsStores) WayBack() string {
	return "none: no cure runs; a person removes the stray store by hand"
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
