package steps

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/configfile"

	"github.com/cucumber/godog"
)

// configExportBoostContext holds a home config file and a vault in a temp
// directory of its own, and what came of exporting the one into the other.
type configExportBoostContext struct {
	dir  string
	home string
	// commits is how many commits the vault held before the step under test.
	commits int
	vault   *apptest.FakeVaultFiles

	printed bytes.Buffer
	err     error
}

// InitializeConfigExportBoostScenario registers the steps of
// features/config_export_boost.feature.
func InitializeConfigExportBoostScenario(ctx *godog.ScenarioContext) {
	c := &configExportBoostContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = configExportBoostContext{vault: &apptest.FakeVaultFiles{}}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.dir != "" {
			os.RemoveAll(c.dir)
		}
		return ctx, nil
	})

	ctx.Given(`^the home's config file for the export holding:$`, c.homeConfigHolding)
	ctx.Given(`^the vault's hosts/boost-config\.toml for the export holding:$`, c.vaultFileHolding)
	ctx.Given(`^the home has no config file for the export$`, func() error { return os.Remove(c.config()) })
	ctx.Given(`^the export runs on the home$`, func() error { c.home = seatUpHost; return nil })
	ctx.Given(`^the export runs on a host that is not the home$`, func() error { c.home = "desktop"; return nil })
	ctx.Given(`^mw config export-boost has run once$`, func() error {
		if err := c.run(false); err != nil {
			return err
		}
		return c.err
	})
	ctx.When(`^mw config export-boost runs$`, func() error { return c.run(false) })
	ctx.When(`^mw config export-boost runs dry$`, func() error { return c.run(true) })
	ctx.Then(`^mw config export-boost succeeded$`, func() error {
		if c.err != nil {
			return fmt.Errorf("mw config export-boost failed: %v\n%s", c.err, c.printed.String())
		}
		return nil
	})
	ctx.Then(`^mw config export-boost was refused, saying "([^"]*)"$`, func(want string) error {
		if c.err == nil || !strings.Contains(c.err.Error(), want) {
			return fmt.Errorf("expected a refusal saying %q, got %v", want, c.err)
		}
		return nil
	})
	ctx.Then(`^mw config export-boost printed "([^"]*)"$`, func(want string) error {
		if !strings.Contains(c.printed.String(), want) {
			return fmt.Errorf("expected the export to print %q, it printed:\n%s", want, c.printed.String())
		}
		return nil
	})
	ctx.Then(`^mw config export-boost did not print "([^"]*)"$`, func(want string) error {
		if strings.Contains(c.printed.String(), want) {
			return fmt.Errorf("expected the export not to print %q, it printed:\n%s", want, c.printed.String())
		}
		return nil
	})
	ctx.Then(`^the vault's hosts/boost-config\.toml holds, after its header:$`, c.vaultFileHoldsAfterHeader)
	ctx.Then(`^the vault has no hosts/boost-config\.toml$`, func() error {
		if _, err := os.Stat(c.target()); !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("expected no %s, stat says %v", c.target(), err)
		}
		return nil
	})
	ctx.Then(`^the export committed "([^"]*)" and nothing else$`, func(path string) error {
		commits := c.vault.Commits()
		if len(commits) != 1 || len(commits[0].Paths) != 1 || commits[0].Paths[0] != path {
			return fmt.Errorf("expected one commit of %s alone, got %+v", path, commits)
		}
		return nil
	})
	ctx.Then(`^the export committed nothing more$`, func() error {
		if got := len(c.vault.Commits()); got != c.commits {
			return fmt.Errorf("expected %d commits, there are %d: %+v", c.commits, got, c.vault.Commits())
		}
		return nil
	})
	ctx.Then(`^the export committed nothing$`, func() error {
		if commits := c.vault.Commits(); len(commits) != 0 {
			return fmt.Errorf("expected no commit, got %+v", commits)
		}
		return nil
	})
}

func (c *configExportBoostContext) ensureDir() error {
	if c.dir != "" {
		return nil
	}
	dir, err := os.MkdirTemp("", "mw-config-export-boost-")
	c.dir = dir
	return err
}

func (c *configExportBoostContext) config() string { return filepath.Join(c.dir, "config.toml") }

func (c *configExportBoostContext) target() string {
	return filepath.Join(c.dir, "vault", application.BoostConfigFile)
}

func (c *configExportBoostContext) homeConfigHolding(doc *godog.DocString) error {
	if err := c.ensureDir(); err != nil {
		return err
	}
	return os.WriteFile(c.config(), []byte(doc.Content+"\n"), 0o600)
}

func (c *configExportBoostContext) vaultFileHolding(doc *godog.DocString) error {
	if err := c.ensureDir(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.target()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(c.target(), []byte(doc.Content+"\n"), 0o644)
}

func (c *configExportBoostContext) run(dryRun bool) error {
	c.commits = len(c.vault.Commits())
	c.printed.Reset()
	c.err = application.ConfigExportBoost{
		Config: configfile.New(c.config()),
		Target: configfile.New(c.target()),
		Vault:  c.vault,
		Home:   &apptest.FakeHomeFile{Text: c.home + " 2026-10-15T17:03:00Z mw@" + c.home},
		Host:   seatUpHost,
		Out:    &c.printed,
	}.Run(context.Background(), dryRun)
	return nil
}

func (c *configExportBoostContext) vaultFileHoldsAfterHeader(doc *godog.DocString) error {
	data, err := os.ReadFile(c.target())
	if err != nil {
		return err
	}
	text := string(data)
	at := strings.Index(text, "\n[tests]\n") + 1
	if at == 0 {
		return fmt.Errorf("the file has no [tests] table:\n%s", text)
	}
	for _, line := range strings.Split(text[:at], "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			return fmt.Errorf("the header holds %q, which is not a comment:\n%s", line, text)
		}
	}
	if got, want := text[at:], doc.Content+"\n"; got != want {
		return fmt.Errorf("the file after its header reads\n%s\nwant\n%s", got, want)
	}
	return nil
}
