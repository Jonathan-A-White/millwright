package steps

import (
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"

	"github.com/cucumber/godog"
)

// registerAgeKeySteps registers the steps of the age-key scenarios. Each makes
// a temp vault and a temp key path of its own; the key file is a placeholder,
// never a key, since the check only reads where it is and its mode.
func (c *doctorContext) registerAgeKeySteps(ctx *godog.ScenarioContext) {
	ctx.Given(`^the home host's vault keeps secrets and it holds no age key$`, func() error {
		return c.ageKeyHost(seatUpHost, true, 0)
	})
	ctx.Given(`^the home host's vault keeps secrets and its age key is mode (\d+)$`, func(mode string) error {
		return c.ageKeyHost(seatUpHost, true, octal(mode))
	})
	ctx.Given(`^the home host's vault keeps no secrets and it holds no age key$`, func() error {
		return c.ageKeyHost(seatUpHost, false, 0)
	})
	ctx.Given(`^a host that is not home holding an age key of mode (\d+)$`, func(mode string) error {
		return c.ageKeyHost("desktop", true, octal(mode))
	})
	ctx.Given(`^a host that is not home holding no age key$`, func() error {
		return c.ageKeyHost("desktop", true, 0)
	})
	ctx.When(`^mw doctor's age-key check runs$`, func() error { return c.run(false) })
}

// octal reads a mode as written in a scenario, "644", as the bits it names.
func octal(text string) os.FileMode {
	var mode os.FileMode
	for _, r := range text {
		mode = mode*8 + os.FileMode(r-'0')
	}
	return mode
}

// ageKeyHost wires the real age-key check on the scenario's host (seatUpHost)
// to a home file naming home, a temp vault that keeps secrets (a .sops.yaml)
// or does not, and a temp key path holding a placeholder file of the given
// mode, or nothing when mode is 0.
func (c *doctorContext) ageKeyHost(home string, keepsSecrets bool, mode os.FileMode) error {
	dir, err := os.MkdirTemp("", "mw-doctor-age-key")
	if err != nil {
		return err
	}
	c.ageKeyDir = dir
	vault := filepath.Join(dir, "vault")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		return err
	}
	if keepsSecrets {
		if err := os.WriteFile(filepath.Join(vault, ".sops.yaml"), []byte("creation_rules: []\n"), 0o644); err != nil {
			return err
		}
	}
	key := filepath.Join(dir, ".config", "mw", "age.key")
	if mode != 0 {
		if err := os.MkdirAll(filepath.Dir(key), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(key, []byte("# a placeholder, not a key\n"), mode); err != nil {
			return err
		}
		// WriteFile's mode passes through the umask; say it exactly.
		if err := os.Chmod(key, mode); err != nil {
			return err
		}
	}
	c.real = doctor.NewAgeKey(&apptest.FakeHomeFile{Text: home + " 2026-10-09T00:10:00Z mw@" + home}, seatUpHost, vault, key)
	return nil
}
