package steps

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"

	"github.com/cucumber/godog"
)

// registerVaultGitignoreSteps registers the steps of the vault-gitignore
// scenarios (mw-gq6.1689): a temp vault, not a git clone, whose .gitignore is
// the real template's less the lines a scenario takes out.
func (c *doctorContext) registerVaultGitignoreSteps(ctx *godog.ScenarioContext) {
	ctx.Given(`^a vault whose \.gitignore is the template's without the line "([^"]*)"$`, c.aVaultWithoutTheLine)
	ctx.Given(`^a vault whose \.gitignore is the template's$`, func() error { return c.aVaultWithoutTheLine("") })
	ctx.When(`^mw doctor's vault-gitignore check runs$`, func() error { return c.run(false) })
	ctx.Then(`^the vault's \.gitignore holds the line "([^"]*)"$`, c.theVaultsGitignoreHolds)
}

func (c *doctorContext) aVaultWithoutTheLine(drop string) error {
	dir, err := scenarioTempDir("", "mw-doctor-vault-gitignore")
	if err != nil {
		return err
	}
	c.vaultGitignoreDir = dir
	text, err := fs.ReadFile(millwright.Template(), ".gitignore")
	if err != nil {
		return err
	}
	var kept []string
	dropped := false
	for _, line := range strings.Split(string(text), "\n") {
		if drop != "" && strings.TrimSpace(line) == drop {
			dropped = true
			continue
		}
		kept = append(kept, line)
	}
	if drop != "" && !dropped {
		return fmt.Errorf("the template's .gitignore has no line %q to take out", drop)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(strings.Join(kept, "\n")), 0o644); err != nil {
		return err
	}
	c.real = doctor.NewVaultGitignore(dir, seatUpHost, millwright.Template())
	return nil
}

func (c *doctorContext) theVaultsGitignoreHolds(line string) error {
	got, err := os.ReadFile(filepath.Join(c.vaultGitignoreDir, ".gitignore"))
	if err != nil {
		return err
	}
	for _, have := range strings.Split(string(got), "\n") {
		if strings.TrimSpace(have) == line {
			return nil
		}
	}
	return fmt.Errorf("the vault's .gitignore does not hold %q", line)
}
