package doctor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

var _ application.DoctorCheck = (*BoostConfig)(nil)

// BoostConfigName is what the check is called: in the log, and on the command
// line as `mw doctor boost-config`.
const BoostConfigName = "boost-config"

// The boost-config check's damper: it has no cure to retry, so one failed cure
// attempt is all an episode ever spends, as with rig-tests.
const (
	BoostConfigDamperWait = 0 * time.Second
	BoostConfigDamperCap  = 1
)

const boostConfigNoCure = "no cure: a person runs mw config export-boost on the home"

// BoostConfig is the check, on the home, that the vault's
// hosts/boost-config.toml holds the home's [tests], [changed_specs] and [test_parts.<rig>] tables
// alike (mw-gq6.456). contrib/boost-bootstrap.sh gives a new Boost its lines
// from that file, so a rig's line the file lacks or holds differently gates
// that rig on the next Boost with make test, or with the old command. A host
// that is not home has nothing to compare: its tables are a part of the
// home's. It never cures: the fix is `mw config export-boost`.
type BoostConfig struct {
	// Home and Host are the vault's home file and this host's name. A nil
	// Home judges as the home; a home that cannot be told is cannot-tell.
	Home application.HomeFile
	Host string
	// Config is this host's ~/.config/mw/config.toml, and Target the vault's
	// hosts/boost-config.toml.
	Config, Target application.ConfigFile
	// ConfigErr is why this host's config file could not be found, if it
	// could not: the check cannot tell.
	ConfigErr error

	// faulty is the last probe's finding when it was faulty, which the cure's
	// error carries so the report names each rig.
	faulty string
}

// NewBoostConfig is the check for the host named host, against the given home
// file, comparing config with the vault's target.
func NewBoostConfig(home application.HomeFile, host string, config, target application.ConfigFile) *BoostConfig {
	return &BoostConfig{Home: home, Host: host, Config: config, Target: target}
}

// Name implements application.DoctorCheck.
func (b *BoostConfig) Name() string { return BoostConfigName }

// Probe implements application.DoctorCheck: ok on a host that is not home;
// cannot-tell when the home cannot be told or this host's config does not
// read; faulty when the vault's file is missing, does not read, or holds any
// line of the two tables differently from the config, naming each.
func (b *BoostConfig) Probe(ctx context.Context) (application.Verdict, string) {
	b.faulty = ""
	verdict, reason := b.probe(ctx)
	if verdict == application.DoctorFaulty {
		b.faulty = reason
	}
	return verdict, reason
}

func (b *BoostConfig) probe(ctx context.Context) (application.Verdict, string) {
	if b.Home != nil {
		home, err := application.IsHome(ctx, b.Home, b.Host)
		if err != nil {
			return application.DoctorCannotTell, fmt.Sprintf("cannot tell whether %s is the home: %v", b.Host, err)
		}
		if !home {
			return application.DoctorOK, ""
		}
	}
	if b.ConfigErr != nil {
		return application.DoctorCannotTell, fmt.Sprintf("finding the config file: %v", b.ConfigErr)
	}
	if _, exists, err := b.Config.Read(); err == nil && !exists {
		return application.DoctorCannotTell, fmt.Sprintf("there is no %s to compare", b.Config.Path())
	}
	tables, err := readBoostTables(b.Config)
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	fix := fmt.Sprintf("run mw config export-boost (a Boost made now copies its [tests], [changed_specs] and [test_parts] tables from %s)", application.BoostConfigFile)
	_, exists, err := b.Target.Read()
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("reading %s: %v", b.Target.Path(), err)
	}
	if !exists {
		return application.DoctorFaulty, fmt.Sprintf("the vault has no %s: %s", application.BoostConfigFile, fix)
	}
	vault, err := readBoostTables(b.Target)
	if err != nil {
		return application.DoctorFaulty, fmt.Sprintf("%s; %s", err, fix)
	}
	drift := domain.BoostTablesDrift(tables, vault)
	if len(drift) == 0 {
		return application.DoctorOK, ""
	}
	return application.DoctorFaulty, fmt.Sprintf("%s is not level with %s: %s; %s",
		application.BoostConfigFile, b.Config.Path(), strings.Join(drift, ", "), fix)
}

// readBoostTables is the tables of file a Boost copies.
func readBoostTables(file application.ConfigFile) (domain.BoostTables, error) {
	data, _, err := file.Read()
	if err != nil {
		return domain.BoostTables{}, fmt.Errorf("reading %s: %w", file.Path(), err)
	}
	parsed, err := domain.ParseConfigTOML(string(data))
	if err != nil {
		return domain.BoostTables{}, fmt.Errorf("%s does not parse as TOML: %w", file.Path(), err)
	}
	tables, err := domain.BoostTablesOf(parsed)
	if err != nil {
		return domain.BoostTables{}, fmt.Errorf("%s: %w", file.Path(), err)
	}
	return tables, nil
}

// Cure implements application.DoctorCheck: there is none. Its error carries
// the probe's finding.
func (b *BoostConfig) Cure(context.Context) error {
	if b.faulty == "" {
		return errors.New(boostConfigNoCure)
	}
	return fmt.Errorf("%s: %s", boostConfigNoCure, b.faulty)
}

// Damper implements application.DoctorCheck.
func (b *BoostConfig) Damper() (time.Duration, int) {
	return BoostConfigDamperWait, BoostConfigDamperCap
}

// WayBack implements application.DoctorCheck: nothing ever changes.
func (b *BoostConfig) WayBack() string {
	return "none: no cure runs; a person runs mw config export-boost on the home"
}
