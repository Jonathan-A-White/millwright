package steps

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/configfile"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"

	"github.com/cucumber/godog"
)

// registerBoostConfigSteps registers the steps of the boost-config scenarios.
// Each writes a home config file and a vault in a temp dir of its own, which
// the After hook removes.
func (c *doctorContext) registerBoostConfigSteps(ctx *godog.ScenarioContext) {
	ctx.Given(`^the home host's config\.toml for boost-config holding:$`, func(doc *godog.DocString) error {
		return c.boostConfigWrite("config.toml", doc.Content)
	})
	ctx.Given(`^the vault's hosts/boost-config\.toml for boost-config holding:$`, func(doc *godog.DocString) error {
		return c.boostConfigWrite(filepath.Join("vault", application.BoostConfigFile), doc.Content)
	})
	ctx.Given(`^the vault will not commit$`, func() error {
		c.boostConfigVault.CommitErr = errors.New("hosts/boost-config.toml has local changes")
		return nil
	})
	ctx.Then(`^the vault's hosts/boost-config\.toml was committed by that path alone$`, func() error {
		commits := c.boostConfigVault.Commits()
		if len(commits) != 1 || len(commits[0].Paths) != 1 || commits[0].Paths[0] != application.BoostConfigFile {
			return fmt.Errorf("expected one commit of %s by that path alone, got %+v", application.BoostConfigFile, commits)
		}
		return nil
	})
	ctx.Then(`^the vault's hosts/boost-config\.toml for boost-config holds "([^"]*)"$`, func(want string) error {
		data, err := os.ReadFile(filepath.Join(c.boostConfigDir, "vault", application.BoostConfigFile))
		if err != nil {
			return err
		}
		if !strings.Contains(string(data), want) {
			return fmt.Errorf("the vault's file does not hold %q:\n%s", want, data)
		}
		return nil
	})
	ctx.Then(`^the home host's config\.toml for boost-config holds "([^"]*)"$`, func(want string) error {
		data, err := os.ReadFile(filepath.Join(c.boostConfigDir, "config.toml"))
		if err != nil {
			return err
		}
		if !strings.Contains(string(data), want) {
			return fmt.Errorf("the config file does not hold %q:\n%s", want, data)
		}
		return nil
	})
	ctx.When(`^mw doctor's boost-config check runs on the home$`, func() error {
		return c.boostConfigRun(seatUpHost)
	})
	ctx.When(`^mw doctor's boost-config check runs on a host that is not home$`, func() error {
		return c.boostConfigRun("desktop")
	})
}

func (c *doctorContext) boostConfigWrite(name, content string) error {
	if c.boostConfigDir == "" {
		dir, err := os.MkdirTemp("", "mw-doctor-boost-config")
		if err != nil {
			return err
		}
		c.boostConfigDir = dir
	}
	path := filepath.Join(c.boostConfigDir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content+"\n"), 0o644)
}

// boostConfigRun runs the real boost-config check on the scenario's host
// (seatUpHost), against a home file naming home.
func (c *doctorContext) boostConfigRun(home string) error {
	check := doctor.NewBoostConfig(
		&apptest.FakeHomeFile{Text: home + " 2026-10-15T17:03:00Z mw@" + home}, seatUpHost,
		configfile.New(filepath.Join(c.boostConfigDir, "config.toml")),
		configfile.New(filepath.Join(c.boostConfigDir, "vault", application.BoostConfigFile)))
	check.Vault = c.boostConfigVault
	c.real = check
	return c.run(false)
}
