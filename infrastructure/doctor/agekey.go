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
	"github.com/Jonathan-A-White/millwright/infrastructure/sops"
)

var _ application.DoctorCheck = (*AgeKey)(nil)

// AgeKeyName is what the check is called: in the log, and on the command line
// as `mw doctor age-key`.
const AgeKeyName = "age-key"

// The age-key check's damper: there is no cure to retry, so one failed cure
// attempt is all an episode ever spends, as with postern-channel.
const (
	AgeKeyDamperWait = 0 * time.Second
	AgeKeyDamperCap  = 1
)

// ageKeyNoCure begins what Cure always returns: the doctor never makes,
// moves, copies or deletes a key.
const ageKeyNoCure = "no cure: a person puts the age key right (docs/secrets.md)"

// AgeKey is the check that the age key opening the vault's secrets.enc.yaml is
// where it should be and only there: on the home, present and readable by its
// owner alone (mode 600); on any other host, absent. It warns and never cures:
// what to do with a key is a person's call.
type AgeKey struct {
	// Home and Host are the vault's home file and this host's name.
	Home application.HomeFile
	Host string
	// Vault is the vault directory. A vault with neither .sops.yaml nor
	// secrets.enc.yaml keeps no secrets yet, and the home needs no key.
	Vault string
	// Path is where the age key is kept on this host: config.AgeKeyFile.
	// PathErr, when set, is why that could not be read, and all the check
	// can say is that it cannot tell.
	Path    string
	PathErr error

	// faulty is the reason the last Probe found the key faulty, for Cure to
	// name; empty when it did not.
	faulty string
}

// NewAgeKey is the check for the host named host, against the given home
// file and vault, of the age key at path.
func NewAgeKey(home application.HomeFile, host, vault, path string) *AgeKey {
	return &AgeKey{Home: home, Host: host, Vault: vault, Path: path}
}

// Name implements application.DoctorCheck.
func (a *AgeKey) Name() string { return AgeKeyName }

// Probe implements application.DoctorCheck. On a host that is not home: ok
// with no key, faulty with one. On the home: faulty with no key when the vault
// keeps secrets (ok, n/a, when it keeps none yet), faulty when the key is not
// a plain file of mode 600, ok otherwise. Cannot-tell when the home cannot be
// told or the key cannot be read.
func (a *AgeKey) Probe(ctx context.Context) (application.Verdict, string) {
	verdict, reason := a.probe(ctx)
	a.faulty = ""
	if verdict == application.DoctorFaulty {
		a.faulty = reason
	}
	return verdict, reason
}

func (a *AgeKey) probe(ctx context.Context) (application.Verdict, string) {
	if a.PathErr != nil {
		return application.DoctorCannotTell, fmt.Sprintf("cannot tell where the age key is kept: %v", a.PathErr)
	}
	home, err := application.WhereIsHome(ctx, a.Home)
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("cannot tell which host is home: %v", err)
	}
	info, statErr := os.Lstat(a.Path)
	present := statErr == nil
	if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
		return application.DoctorCannotTell, fmt.Sprintf("cannot read %s: %v", a.Path, statErr)
	}

	if home.Host != a.Host {
		if present {
			return application.DoctorFaulty, fmt.Sprintf(
				"an age key is at %s on %s, which is not home (%s is): the key lives on the home only; make sure the home holds it, then remove it here",
				a.Path, a.Host, home.Host)
		}
		return application.DoctorOK, ""
	}

	if !present {
		if !a.keepsSecrets() {
			return application.DoctorOK, "n/a: the vault keeps no secrets (no " + sops.ConfigFile + ")"
		}
		return application.DoctorFaulty, fmt.Sprintf(
			"no age key at %s on the home: %s cannot be opened here; restore the key from the Governor's offline copy (docs/secrets.md)",
			a.Path, filepath.Join(a.Vault, sops.SecretsFile))
	}
	if !info.Mode().IsRegular() {
		return application.DoctorFaulty, fmt.Sprintf("the age key %s is not a plain file (%s)", a.Path, info.Mode().Type())
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		return application.DoctorFaulty, fmt.Sprintf("the age key %s is mode %o, not 600: chmod 600 %s", a.Path, mode, a.Path)
	}
	return application.DoctorOK, ""
}

// keepsSecrets is whether the vault holds .sops.yaml or secrets.enc.yaml.
func (a *AgeKey) keepsSecrets() bool {
	for _, name := range []string{sops.ConfigFile, sops.SecretsFile} {
		if _, err := os.Stat(filepath.Join(a.Vault, name)); err == nil {
			return true
		}
	}
	return false
}

// Cure implements application.DoctorCheck: there is none. What it returns
// names what the probe found, so the report a person reads says it.
func (a *AgeKey) Cure(context.Context) error {
	if a.faulty == "" {
		return errors.New(ageKeyNoCure)
	}
	return fmt.Errorf("%s: %s", ageKeyNoCure, a.faulty)
}

// Damper implements application.DoctorCheck.
func (a *AgeKey) Damper() (time.Duration, int) { return AgeKeyDamperWait, AgeKeyDamperCap }

// WayBack implements application.DoctorCheck: nothing ever changes, so there
// is nothing to undo.
func (a *AgeKey) WayBack() string {
	return "none: no cure runs; a person puts the key right by hand (docs/secrets.md)"
}
