package doctor

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*RigTests)(nil)

// RigTestsName is what the check is called: in the log, and on the command line
// as `mw doctor rig-tests`.
const RigTestsName = "rig-tests"

// The rig-tests check's damper: it has no cure to retry, so one failed cure
// attempt is all an episode ever spends.
const (
	RigTestsDamperWait = 0 * time.Second
	RigTestsDamperCap  = 1
)

var errRigTestsNoCure = errors.New(
	"no cure: a person adds the rig's line to the [tests] table of ~/.config/mw/config.toml " +
		"(the home's has it; the vault's hosts/boost-config.toml is what boost-bootstrap.sh copies from)")

// RigTests is the check that a Boost's config.toml says what each rig's tests
// are (mw-gq6.453). A rig its [tests] table does not name is gated with `make
// test`, which a rig without a Makefile refuses outright; a Boost made by hand
// or by an older boost-bootstrap.sh had no [tests] table at all. The home is
// never judged: its table is the one the Boost's is copied from. The factory
// rig is not named, since `make test` is what its tests are. It never cures.
type RigTests struct {
	// Rigs is what config.Rigs reads: each rig's checkout, by rig name.
	Rigs map[string]string
	// Tests is what config.Tests reads: each rig's tests command, by rig name.
	Tests map[string]string
	// Err is the error either read gave, if any.
	Err error
	// Home and Host are the vault's home file and this host's name. The home is
	// never faulty. A nil Home, or a home that cannot be told, judges as a Boost.
	Home application.HomeFile
	Host string
}

// NewRigTests is the check over rigs and tests, as this host's config reads them.
func NewRigTests(rigs, tests map[string]string, err error) *RigTests {
	return &RigTests{Rigs: rigs, Tests: tests, Err: err}
}

// Name implements application.DoctorCheck.
func (r *RigTests) Name() string { return RigTestsName }

// Probe implements application.DoctorCheck: ok on the home and when every rig
// of [rigs] has a [tests] line; faulty naming each rig that has none.
func (r *RigTests) Probe(ctx context.Context) (application.Verdict, string) {
	if r.Home != nil {
		if home, err := application.IsHome(ctx, r.Home, r.Host); err == nil && home {
			return application.DoctorOK, ""
		}
	}
	if r.Err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("reading the config file: %v", r.Err)
	}
	var lacking []string
	for rig := range r.Rigs {
		if rig == application.FactoryRig {
			continue
		}
		if strings.TrimSpace(r.Tests[rig]) == "" {
			lacking = append(lacking, rig)
		}
	}
	if len(lacking) == 0 {
		return application.DoctorOK, ""
	}
	sort.Strings(lacking)
	return application.DoctorFaulty, fmt.Sprintf("%s in [rigs] with no [tests] line in ~/.config/mw/config.toml: mw next gates it with make test, which a rig with no Makefile refuses",
		strings.Join(lacking, ", "))
}

// Cure implements application.DoctorCheck: there is none.
func (r *RigTests) Cure(context.Context) error { return errRigTestsNoCure }

// Damper implements application.DoctorCheck.
func (r *RigTests) Damper() (time.Duration, int) { return RigTestsDamperWait, RigTestsDamperCap }

// WayBack implements application.DoctorCheck: nothing ever changes.
func (r *RigTests) WayBack() string {
	return "none: no cure runs; a person adds the line by hand"
}
