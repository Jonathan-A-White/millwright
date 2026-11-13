package doctor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

var _ application.DoctorCheck = (*RigTests)(nil)

// RigTestsName is what the check is called: in the log, and on the command line
// as `mw doctor rig-tests`.
const RigTestsName = "rig-tests"

// The rig-tests check's damper: a cure that failed (a rig the vault has no line
// for, a config that will not write) does not get likelier by a retry, so one
// failed cure attempt is all an episode ever spends.
const (
	RigTestsDamperWait = 0 * time.Second
	RigTestsDamperCap  = 1
)

var errRigTestsNoCure = errors.New(
	"no cure: a person adds the rig's line to the [tests] table of ~/.config/mw/config.toml " +
		"(the home's has it; the vault's hosts/boost-config.toml is what boost-bootstrap.sh copies from)")

// rigTestsSetKey is the key `mw config set` takes for a rig's tests line.
const rigTestsSetKey = "tests."

// RigTests is the check that a Boost's config.toml says what each rig's tests
// are (mw-gq6.453). A rig its [tests] table does not name is gated with `make
// test`, which a rig without a Makefile refuses outright; a Boost made by hand
// or by an older boost-bootstrap.sh had no [tests] table at all. The home is
// never judged: its table is the one the Boost's is copied from. The factory
// rig is not named, since `make test` is what its tests are. Its cure is what a
// person would do: for each such rig the vault's hosts/boost-config.toml has a
// [tests] line for, `mw config set tests.<rig> '<that line>'` (ConfigSet, with a
// backup of the config); a rig the vault does not name has no cure, and the
// error names it (mw-gq6.1254).
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
	// Config is this host's ~/.config/mw/config.toml and Target the vault's
	// hosts/boost-config.toml. With either nil the check has no cure.
	Config, Target application.ConfigFile
	// Out is where the cure's `mw config set` says what it changed; nil is
	// discarded.
	Out io.Writer

	// lacking is the rigs the last probe found with no line.
	lacking []string
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
	r.lacking = nil
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
	r.lacking = lacking
	return application.DoctorFaulty, fmt.Sprintf("%s in [rigs] with no [tests] line in ~/.config/mw/config.toml: mw next gates it with make test, which a rig with no Makefile refuses",
		strings.Join(lacking, ", "))
}

// Cure implements application.DoctorCheck: for each rig the last probe found
// with no line, if the vault's hosts/boost-config.toml has a [tests] line for it,
// sets that line in this host's config as `mw config set tests.<rig> '<line>'`
// does. A rig the vault has no line for is left, and the error names it; so is
// every rig, with no config or vault file to work from.
func (r *RigTests) Cure(ctx context.Context) error {
	if r.Config == nil || r.Target == nil || len(r.lacking) == 0 {
		return errRigTestsNoCure
	}
	data, exists, err := r.Target.Read()
	if err != nil {
		return fmt.Errorf("%w: reading %s: %v", errRigTestsNoCure, r.Target.Path(), err)
	}
	if !exists {
		return fmt.Errorf("%w: the vault has no %s", errRigTestsNoCure, application.BoostConfigFile)
	}
	parsed, err := domain.ParseConfigTOML(string(data))
	if err != nil {
		return fmt.Errorf("%w: %s does not parse as TOML: %v", errRigTestsNoCure, r.Target.Path(), err)
	}
	vault, err := domain.BoostTablesOf(parsed)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", errRigTestsNoCure, r.Target.Path(), err)
	}
	out := r.Out
	if out == nil {
		out = io.Discard
	}
	var unnamed []string
	for _, rig := range r.lacking {
		line := vault.Lines["tests"][rig]
		if strings.TrimSpace(line) == "" {
			unnamed = append(unnamed, rig)
			continue
		}
		if err := (application.ConfigSet{File: r.Config, Out: out}).Run(ctx, rigTestsSetKey+rig, line, false); err != nil {
			return fmt.Errorf("mw config set %s%s failed: %w", rigTestsSetKey, rig, err)
		}
		if r.Tests == nil {
			r.Tests = map[string]string{}
		}
		r.Tests[rig] = line
	}
	if len(unnamed) > 0 {
		return fmt.Errorf("%w; the vault's %s has no [tests] line for %s", errRigTestsNoCure, application.BoostConfigFile, strings.Join(unnamed, ", "))
	}
	return nil
}

// Damper implements application.DoctorCheck.
func (r *RigTests) Damper() (time.Duration, int) { return RigTestsDamperWait, RigTestsDamperCap }

// WayBack implements application.DoctorCheck: the backup mw config set made.
func (r *RigTests) WayBack() string {
	return "mw config set backs the config up first: cp the newest ~/.config/mw/config.toml.bak-<stamp> over ~/.config/mw/config.toml (a cure with nothing to set changes nothing)"
}
