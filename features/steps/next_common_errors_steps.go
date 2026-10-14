package steps

import (
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// registerNextCommonErrorsSteps registers the steps of a landing's 'For common
// errors:' block (mw-gq6.420): the vault's error register as a landing reads it.
func registerNextCommonErrorsSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the error register in the vault holds the class "([^"]*)"$`, c.theRegisterHoldsTheClass)
}

func (c *nextContext) theRegisterHoldsTheClass(slug string) error {
	dir := filepath.Join(c.vault, vault.SeatsDir, nextSeat, application.ErrorRegisterDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	class := application.ErrorClass{Slug: slug, Symptom: "what " + slug + " looks like", Kinds: []string{"go-cli"}, Since: "2026-10-14"}
	return os.WriteFile(filepath.Join(dir, slug+application.MemoryExt), []byte(class.String()), 0o644)
}
