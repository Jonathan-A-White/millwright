package doctor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*PaceSource)(nil)

// PaceSourceName is what the check is called: in the log, and on the command
// line as `mw doctor pace-source`.
const PaceSourceName = "pace-source"

// The pace-source check's damper: it has no cure to retry, so one failed cure
// attempt is all an episode ever spends.
const (
	PaceSourceDamperWait = 0 * time.Second
	PaceSourceDamperCap  = 1
)

var errPaceSourceNoCure = errors.New(
	"no cure: a person deletes the key from the [pace] table of ~/.config/mw/config.toml on the host")

// PaceSource is the check that no host's own [pace] table says a key (usage_line,
// max_at_once) otherwise than the vault's pace.toml, the one source every host
// reads for the factory's pace (mw-gq6.1698). The host's key is ignored while the
// vault says its own, so it is a pace the host's reader thinks it keeps and does
// not. It never cures.
type PaceSource struct {
	// Host is the host the check runs on, named in what it says.
	Host string
	// Differences is what config.PaceDifferences reads: one line for each key of
	// the host's own table that the vault's file says otherwise.
	Differences []string
	// ReadErr is why the two could not be read, when they could not.
	ReadErr error
}

// NewPaceSource is the check over host's differences, as config reads them.
func NewPaceSource(host string, differences []string, readErr error) *PaceSource {
	return &PaceSource{Host: host, Differences: differences, ReadErr: readErr}
}

// Name implements application.DoctorCheck.
func (p *PaceSource) Name() string { return PaceSourceName }

// Probe implements application.DoctorCheck: ok when the host's [pace] table
// agrees with the vault's or says nothing, faulty naming the host and each key
// that differs, and the way to cure it.
func (p *PaceSource) Probe(context.Context) (application.Verdict, string) {
	if p.ReadErr != nil {
		return application.DoctorCannotTell, fmt.Sprintf("the [pace] tables of %s and the vault's pace.toml could not be read: %v", p.Host, p.ReadErr)
	}
	if len(p.Differences) == 0 {
		return application.DoctorOK, ""
	}
	return application.DoctorFaulty, fmt.Sprintf(
		"host %s has a [pace] that differs from the vault's pace.toml, which every host reads and which wins: %s. To cure: delete those keys from the [pace] table of ~/.config/mw/config.toml on %s; set the factory's pace once, in pace.toml in the vault",
		p.Host, strings.Join(p.Differences, "; "), p.Host)
}

// Cure implements application.DoctorCheck: there is none.
func (p *PaceSource) Cure(context.Context) error { return errPaceSourceNoCure }

// Damper implements application.DoctorCheck.
func (p *PaceSource) Damper() (time.Duration, int) { return PaceSourceDamperWait, PaceSourceDamperCap }

// WayBack implements application.DoctorCheck: nothing ever changes.
func (p *PaceSource) WayBack() string {
	return "none: no cure runs; a person deletes the host's key by hand"
}
