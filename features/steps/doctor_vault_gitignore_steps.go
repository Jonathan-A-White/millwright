package steps

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
	"github.com/Jonathan-A-White/millwright/infrastructure/gittest"

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
	ctx.Given(`^a git vault that committed "([^"]*)" before its \.gitignore named it$`, c.aGitVaultThatCommitted)
	ctx.Then(`^git tracks nothing the vault's \.gitignore names$`, c.gitTracksNothingIgnored)
	ctx.Then(`^the file "([^"]*)" is still in the vault$`, c.theFileIsStillInTheVault)
}

// vaultGit runs git in the scenario's vault.
func (c *doctorContext) vaultGit(args ...string) (string, error) {
	cmd := gittest.Command("git", args...)
	cmd.Dir = c.vaultGitignoreDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

// aGitVaultThatCommitted is a git vault whose .gitignore is the template's
// less the line that would have named file, and which holds file committed
// (mw-gq6.1692).
func (c *doctorContext) aGitVaultThatCommitted(file string) error {
	if err := c.aVaultWithoutTheLine("runs/*/check-*"); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Join(c.vaultGitignoreDir, file)), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(c.vaultGitignoreDir, file), []byte("kept\n"), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.name", "mw"},
		{"config", "user.email", "mw@example.invalid"},
		{"add", "-f", "."},
		{"commit", "-qm", "the vault as it was"},
	} {
		if _, err := c.vaultGit(args...); err != nil {
			return err
		}
	}
	return nil
}

func (c *doctorContext) gitTracksNothingIgnored() error {
	out, err := c.vaultGit("ls-files", "--cached", "--ignored", "--exclude-standard")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("git still tracks files the vault's .gitignore names:\n%s", out)
	}
	return nil
}

func (c *doctorContext) theFileIsStillInTheVault(file string) error {
	if _, err := os.Stat(filepath.Join(c.vaultGitignoreDir, file)); err != nil {
		return fmt.Errorf("the vault has lost %s: %w", file, err)
	}
	return nil
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
