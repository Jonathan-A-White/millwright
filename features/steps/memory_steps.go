package steps

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

const memorySeat = "builder"

// memoryContext is features/memory.feature: a real vault in a temp directory
// (the adapter is what writes the files), a clock the scenario sets, what the
// last verb printed and the error it gave. Built by the first Given, never in
// a Before hook, which would run for every feature's scenarios.
type memoryContext struct {
	root string
	day  time.Time
	out  bytes.Buffer
	err  error

	status application.StatusReport
}

// InitializeMemoryScenario registers the steps of features/memory.feature.
func InitializeMemoryScenario(ctx *godog.ScenarioContext) {
	c := &memoryContext{}

	ctx.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		if c.root != "" {
			_ = os.RemoveAll(c.root)
		}
		*c = memoryContext{}
		return ctx, nil
	})

	ctx.Given(`^the memory clock says "([^"]*)"$`, c.theClockSays)
	ctx.Given(`^the Builder keeps the rig "([^"]*)" as facts$`, c.theBuilderKeepsTheRigAsFacts)
	ctx.Given(`^the fact file "([^"]*)" of the rig "([^"]*)" holds:$`, c.theFactFileHolds)
	ctx.Given(`^the rig "([^"]*)" holds facts for listing$`, c.theRigHoldsFactsForListing)
	ctx.Given(`^the rig "([^"]*)" has an about text of (\d+) bytes$`, c.theRigHasAnAboutText)

	ctx.When(`^the Mayor adds a fact to the rig "([^"]*)":$`, c.theMayorAdds)
	ctx.When(`^the Mayor adds a fact to the rig "([^"]*)" a sentence of two lines$`, c.theMayorAddsTwoLines)
	ctx.When(`^the Mayor supersedes the fact "([^"]*)" of the rig "([^"]*)":$`, c.theMayorSupersedes)
	ctx.When(`^the Mayor retires the fact "([^"]*)" of the rig "([^"]*)" because "([^"]*)"$`, c.theMayorRetires)
	ctx.When(`^the Mayor rechecks the fact "([^"]*)" of the rig "([^"]*)" saying "([^"]*)"$`, c.theMayorRechecks)
	ctx.When(`^the Mayor lists the rig "([^"]*)"$`, func(rig string) error {
		return c.list(application.MemoryList{Rig: rig})
	})
	ctx.When(`^the Mayor lists the rig "([^"]*)" with the status "([^"]*)"$`, func(rig, status string) error {
		return c.list(application.MemoryList{Rig: rig, Status: application.FactStatus(status)})
	})
	ctx.When(`^the Mayor lists the rig "([^"]*)" oldest first$`, func(rig string) error {
		return c.list(application.MemoryList{Rig: rig, Oldest: true})
	})
	ctx.When(`^mw status reads the host for memory$`, c.statusReads)

	ctx.Then(`^the memory command succeeded$`, c.succeeded)
	ctx.Then(`^the memory command was refused saying "((?:[^"\\]|\\.)*)"$`, c.wasRefused)
	ctx.Then(`^the memory command printed the fact file "([^"]*)" of the rig "([^"]*)"$`, c.printedTheFile)
	ctx.Then(`^the fact file "([^"]*)" of the rig "([^"]*)" holds:$`, c.fileHolds)
	ctx.Then(`^the rig "([^"]*)" has no folder in the Builder's seat$`, c.hasNoFolder)
	ctx.Then(`^the rig "([^"]*)" has no fact files$`, func(rig string) error { return c.countFiles(rig, 0) })
	ctx.Then(`^the rig "([^"]*)" has (\d+) fact files?$`, func(rig, n string) error {
		want, _ := strconv.Atoi(n)
		return c.countFiles(rig, want)
	})
	ctx.Then(`^the memory listing is:$`, c.listingIs)
	ctx.Then(`^the memory status line reads "([^"]*)"$`, c.statusSays)
	ctx.Then(`^the memory status line is absent for the rig "([^"]*)"$`, c.statusSaysNothing)
}

func (c *memoryContext) theClockSays(day string) error {
	at, err := time.Parse(application.FactDateLayout, day)
	if err != nil {
		return fmt.Errorf("the day %q: %w", day, err)
	}
	c.day = at.Add(9 * time.Hour)
	root, err := os.MkdirTemp("", "mw-memory-")
	if err != nil {
		return err
	}
	c.root = root
	return nil
}

func (c *memoryContext) rigDir(rig string) string {
	return filepath.Join(c.root, application.SeatsDir, memorySeat, application.RigsDir, rig)
}

func (c *memoryContext) theBuilderKeepsTheRigAsFacts(rig string) error {
	return os.MkdirAll(filepath.Join(c.rigDir(rig), application.FactsDir), 0o755)
}

func (c *memoryContext) theFactFileHolds(slug, rig string, doc *godog.DocString) error {
	return os.WriteFile(filepath.Join(c.rigDir(rig), application.FactsDir, slug+".md"), []byte(doc.Content+"\n"), 0o644)
}

func (c *memoryContext) theRigHoldsFactsForListing(rig string) error {
	for _, f := range []struct{ slug, subject, kind, status, since, source, extra string }{
		{"old", "tmux", "gotcha", "superseded", "2026-09-01", "mw-1", "superseded-by: alpha\n"},
		{"zeta", "gate", "decision", "current", "2026-09-02", "mw-2", ""},
		{"alpha", "bd", "gotcha", "current", "2026-09-03", "mw-3", ""},
		{"doubt", "bd", "gotcha", "recheck", "2026-09-05", "mw-5", ""},
	} {
		text := fmt.Sprintf("---\nsubject: %s\nkind: %s\nstatus: %s\nsource: %s\nsince: %s\n%s---\n\nA fact.\n",
			f.subject, f.kind, f.status, f.source, f.since, f.extra)
		if err := os.WriteFile(filepath.Join(c.rigDir(rig), application.FactsDir, f.slug+".md"), []byte(text), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (c *memoryContext) theRigHasAnAboutText(rig, size string) error {
	n, _ := strconv.Atoi(size)
	return os.WriteFile(filepath.Join(c.rigDir(rig), application.AboutFile), []byte(strings.Repeat("x", n)), 0o644)
}

func (c *memoryContext) memory() application.Memory {
	budget, _ := config.RigMemoryBytes()
	c.out.Reset()
	return application.Memory{
		Files: vault.New(c.root), Seat: memorySeat, Budget: budget,
		Now: func() time.Time { return c.day }, Out: &c.out,
	}
}

// pairs reads a two-column table as key and value.
func pairs(table *godog.Table) map[string]string {
	got := map[string]string{}
	for _, row := range table.Rows {
		got[row.Cells[0].Value] = row.Cells[1].Value
	}
	return got
}

func (c *memoryContext) theMayorAdds(rig string, table *godog.Table) error {
	p := pairs(table)
	c.err = c.memory().Add(context.Background(), application.MemoryAdd{
		Rig: rig, Slug: p["slug"], Subject: p["subject"], Source: p["source"],
		Kind: application.FactKind(p["kind"]), Sentence: p["sentence"],
	})
	return nil
}

func (c *memoryContext) theMayorAddsTwoLines(rig string) error {
	c.err = c.memory().Add(context.Background(), application.MemoryAdd{
		Rig: rig, Subject: "bd", Source: "mw-1", Kind: application.FactGotcha,
		Sentence: "First line.\nSecond line.",
	})
	return nil
}

func (c *memoryContext) theMayorSupersedes(old, rig string, table *godog.Table) error {
	p := pairs(table)
	c.err = c.memory().Supersede(context.Background(), application.MemorySupersede{
		Rig: rig, Old: old, Slug: p["slug"], Source: p["source"], Sentence: p["sentence"],
	})
	return nil
}

func (c *memoryContext) theMayorRetires(slug, rig, reason string) error {
	c.err = c.memory().Retire(context.Background(), rig, slug, reason)
	return nil
}

func (c *memoryContext) theMayorRechecks(slug, rig, why string) error {
	c.err = c.memory().Recheck(context.Background(), rig, slug, why)
	return nil
}

func (c *memoryContext) list(req application.MemoryList) error {
	c.err = c.memory().List(context.Background(), req)
	return nil
}

func (c *memoryContext) succeeded() error {
	if c.err != nil {
		return fmt.Errorf("the command failed: %w", c.err)
	}
	return nil
}

func (c *memoryContext) wasRefused(want string) error {
	if c.err == nil {
		return fmt.Errorf("expected a refusal saying %q, but the command succeeded and printed:\n%s", want, c.out.String())
	}
	want = strings.ReplaceAll(want, `\"`, `"`)
	if !strings.Contains(c.err.Error(), want) {
		return fmt.Errorf("expected a refusal saying %q, got %q", want, c.err)
	}
	return nil
}

func (c *memoryContext) printedTheFile(slug, rig string) error {
	want := filepath.Join(c.rigDir(rig), application.FactsDir, slug+".md")
	for _, line := range strings.Split(c.out.String(), "\n") {
		if line == want {
			return nil
		}
	}
	return fmt.Errorf("expected the command to print %s, got:\n%s", want, c.out.String())
}

func (c *memoryContext) fileHolds(slug, rig string, doc *godog.DocString) error {
	got, err := os.ReadFile(filepath.Join(c.rigDir(rig), application.FactsDir, slug+".md"))
	if err != nil {
		return err
	}
	if want := doc.Content + "\n"; string(got) != want {
		return fmt.Errorf("the fact file %s holds:\n%s\nwant:\n%s", slug, got, want)
	}
	return nil
}

func (c *memoryContext) hasNoFolder(rig string) error {
	if _, err := os.Stat(c.rigDir(rig)); !os.IsNotExist(err) {
		return fmt.Errorf("expected no folder for the rig %s, stat said %v", rig, err)
	}
	return nil
}

func (c *memoryContext) countFiles(rig string, want int) error {
	entries, err := os.ReadDir(filepath.Join(c.rigDir(rig), application.FactsDir))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(entries) != want {
		return fmt.Errorf("the rig %s has %d fact files, want %d", rig, len(entries), want)
	}
	return nil
}

func (c *memoryContext) listingIs(doc *godog.DocString) error {
	if got := strings.TrimRight(c.out.String(), "\n"); got != doc.Content {
		return fmt.Errorf("the listing was:\n%s\nwant:\n%s", got, doc.Content)
	}
	return nil
}

func (c *memoryContext) statusReads() error {
	budget, err := config.RigMemoryBytes()
	if err != nil {
		return err
	}
	c.status, c.err = application.Status{
		Tracker:        apptest.NewFakeTracker(),
		Notes:          apptest.NewFakeTracker(),
		Rules:          apptest.NewFakeEpicRules(),
		Vault:          vault.New(c.root),
		Host:           "vps",
		Seat:           memorySeat,
		RigMemoryBytes: budget,
	}.Run(context.Background())
	return c.err
}

func (c *memoryContext) statusSays(line string) error {
	for _, got := range strings.Split(c.status.String(), "\n") {
		if strings.TrimSpace(got) == line {
			return nil
		}
	}
	return fmt.Errorf("expected mw status to say %q, got:\n%s", line, c.status.String())
}

func (c *memoryContext) statusSaysNothing(rig string) error {
	if strings.Contains(c.status.String(), application.RigMemoryHeading) {
		return fmt.Errorf("expected nothing of the memory of %s, got:\n%s", rig, c.status.String())
	}
	return nil
}
