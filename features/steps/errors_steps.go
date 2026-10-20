package steps

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	millwright "github.com/Jonathan-A-White/millwright"
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// errorsContext is features/errors.feature: a real vault in a temp directory
// (the adapter is what writes the register), what the last verb printed and
// the error it gave, and the last status read. Built by the first Given, never
// in a Before hook, which would run for every feature's scenarios.
type errorsContext struct {
	root   string
	out    bytes.Buffer
	err    error
	status string
}

// InitializeErrorsScenario registers the steps of features/errors.feature.
func InitializeErrorsScenario(ctx *godog.ScenarioContext) {
	c := &errorsContext{}

	ctx.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		if c.root != "" {
			_ = os.RemoveAll(c.root)
		}
		*c = errorsContext{}
		return ctx, nil
	})

	ctx.Given(`^an empty error register$`, c.anEmptyRegister)
	ctx.Given(`^the Mayor says the rig "([^"]*)" is of the kinds "([^"]*)"$`, c.theRigIsOfTheKinds)
	ctx.Step(`^the Mayor adds the error class "([^"]*)" of the kinds "([^"]*)" found in "([^"]*)" by "([^"]*)"$`, c.theMayorAddsAClass)

	ctx.When(`^the Mayor records the check of "([^"]*)" in "([^"]*)" as "([^"]*)"$`, c.theMayorRecordsACheck)
	ctx.When(`^the Mayor seeds the error register$`, c.theMayorSeeds)
	ctx.When(`^the Mayor lists the error register$`, c.theMayorLists)
	ctx.When(`^mw status reads the error register$`, c.statusReadsTheRegister)

	ctx.Then(`^the error register holds the class file "([^"]*)"$`, c.theRegisterHoldsTheFile)
	ctx.Then(`^mw errors printed the path of "([^"]*)"$`, c.printedThePathOf)
	ctx.Then(`^mw errors refuses, saying "([^"]*)"$`, c.refusesSaying)
	ctx.Then(`^mw errors says "([^"]*)"$`, c.says)
	ctx.Then(`^mw errors does not say "([^"]*)"$`, c.doesNotSay)
	ctx.Then(`^the error class "([^"]*)" lists the kinds "([^"]*)"$`, c.theClassListsTheKinds)
	ctx.Then(`^mw errors asks the shared component question$`, func() error { return c.says(application.SharedComponentQuestion) })
	ctx.Then(`^the error register holds only class files$`, c.holdsOnlyClassFiles)
	ctx.Then(`^every class in the error register names its kinds$`, c.everyClassNamesItsKinds)
	ctx.Then(`^the status shows "([^"]*)"$`, c.theStatusShows)
	ctx.Then(`^the status does not show "([^"]*)"$`, c.theStatusDoesNotShow)
}

func (c *errorsContext) anEmptyRegister() error {
	root, err := os.MkdirTemp("", "mw-errors-")
	if err != nil {
		return err
	}
	c.root = root
	return nil
}

func (c *errorsContext) register() application.Errors {
	seed, err := fs.Sub(millwright.Template(), application.ErrorRegisterTemplateDir)
	if err != nil {
		panic(err) // the template is embedded, so its register cannot be missing
	}
	return application.Errors{
		Files: vault.New(c.root), Seat: memorySeat, Seed: seed, Out: &c.out,
		Now: func() time.Time { return time.Date(2026, 10, 14, 16, 0, 0, 0, time.UTC) },
	}
}

func (c *errorsContext) dir() string {
	return filepath.Join(c.root, vault.SeatsDir, memorySeat, application.ErrorRegisterDir)
}

func (c *errorsContext) theRigIsOfTheKinds(rig, kinds string) error {
	return c.register().Kinds(context.Background(), rig, strings.Fields(kinds))
}

func (c *errorsContext) theMayorAddsAClass(slug, kinds, rig, bead string) error {
	c.out.Reset()
	c.err = c.register().Add(context.Background(), application.ErrorAdd{
		Slug: slug, Symptom: "what " + slug + " looks like", Kinds: strings.Fields(kinds), Found: rig + ":" + bead,
	})
	return nil
}

func (c *errorsContext) theMayorRecordsACheck(slug, rig, result string) error {
	c.out.Reset()
	c.err = c.register().Checked(context.Background(), slug, rig, result)
	return nil
}

func (c *errorsContext) theMayorSeeds() error {
	c.out.Reset()
	c.err = c.register().SeedRegister(context.Background())
	return c.err
}

func (c *errorsContext) theMayorLists() error {
	c.out.Reset()
	c.err = c.register().List(context.Background())
	return c.err
}

func (c *errorsContext) statusReadsTheRegister() error {
	report, err := application.Status{
		Tracker: apptest.NewFakeTracker(),
		Notes:   apptest.NewFakeTracker(),
		Vault:   vault.New(c.root),
		Errors:  vault.New(c.root),
		Host:    "vps",
		Seat:    memorySeat,
	}.Run(context.Background())
	if err != nil {
		return err
	}
	c.status = report.String()
	return nil
}

func (c *errorsContext) theRegisterHoldsTheFile(name string) error {
	if c.err != nil {
		return fmt.Errorf("mw errors failed: %v", c.err)
	}
	_, err := os.Stat(filepath.Join(c.dir(), name))
	return err
}

func (c *errorsContext) printedThePathOf(name string) error {
	if want := filepath.Join(c.dir(), name); !strings.Contains(c.out.String(), want) {
		return fmt.Errorf("expected %s printed, got %q", want, c.out.String())
	}
	return nil
}

func (c *errorsContext) refusesSaying(words string) error {
	if c.err == nil || !strings.Contains(c.err.Error(), words) {
		return fmt.Errorf("expected a refusal saying %q, got %v", words, c.err)
	}
	return nil
}

func (c *errorsContext) says(words string) error {
	if c.err != nil {
		return fmt.Errorf("mw errors failed: %v", c.err)
	}
	if !strings.Contains(c.out.String(), words) {
		return fmt.Errorf("expected %q, got:\n%s", words, c.out.String())
	}
	return nil
}

func (c *errorsContext) doesNotSay(words string) error {
	if c.err != nil {
		return fmt.Errorf("mw errors failed: %v", c.err)
	}
	if strings.Contains(c.out.String(), words) {
		return fmt.Errorf("expected no %q, got:\n%s", words, c.out.String())
	}
	return nil
}

func (c *errorsContext) theClassListsTheKinds(slug, kinds string) error {
	files, _, err := vault.New(c.root).ReadErrorRegister(context.Background(), memorySeat)
	if err != nil {
		return err
	}
	class, err := application.ParseErrorClass(slug, files[slug])
	if err != nil {
		return err
	}
	if got, want := strings.Join(class.Kinds, " "), kinds; got != want {
		return fmt.Errorf("the class %s lists the kinds %q, want %q", slug, got, want)
	}
	return nil
}

func (c *errorsContext) holdsOnlyClassFiles() error {
	entries, err := os.ReadDir(c.dir())
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != application.MemoryExt && entry.Name() != application.ErrorKindsFile {
			return fmt.Errorf("the register holds %s, which is no class", entry.Name())
		}
	}
	return nil
}

func (c *errorsContext) everyClassNamesItsKinds() error {
	files, _, err := vault.New(c.root).ReadErrorRegister(context.Background(), memorySeat)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("the register is empty")
	}
	for slug, text := range files {
		class, err := application.ParseErrorClass(slug, text)
		if err != nil {
			return fmt.Errorf("%s is not whole: %v", slug, err)
		}
		if len(class.Kinds) == 0 {
			return fmt.Errorf("%s names no kinds", slug)
		}
	}
	return nil
}

func (c *errorsContext) theStatusShows(words string) error {
	if !strings.Contains(c.status, words) {
		return fmt.Errorf("expected the status to show %q, got:\n%s", words, c.status)
	}
	return nil
}

func (c *errorsContext) theStatusDoesNotShow(words string) error {
	if strings.Contains(c.status, words) {
		return fmt.Errorf("expected the status not to show %q, got:\n%s", words, c.status)
	}
	return nil
}
