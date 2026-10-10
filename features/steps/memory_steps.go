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

// memoryHead is the vault's HEAD as the scenarios see it: migrate names its
// first seven characters in the source of a fact it can name no bead for.
const memoryHead = "abc1234def5678901234567890abcdef12345678"

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
	ctx.Given(`^the rig "([^"]*)" holds facts for querying$`, c.theRigHoldsFactsForQuerying)
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
	ctx.When(`^the Builder queries the rig "([^"]*)" for "([^"]*)"$`, func(rig, terms string) error {
		c.err = c.memory().Query(context.Background(), application.MemoryQuery{Rig: rig, Terms: strings.Fields(terms)})
		return nil
	})
	ctx.When(`^mw status reads the host for memory$`, c.statusReads)

	ctx.Given(`^the rig "([^"]*)" keeps its memory in one file:$`, c.theRigKeepsItsMemory)
	ctx.Given(`^the rig "([^"]*)" keeps its archive in one file:$`, c.theRigKeepsItsArchive)
	ctx.Given(`^the rig "([^"]*)" keeps a memory file with a head of (\d+) bytes$`, c.theRigKeepsALongHead)
	ctx.When(`^the Mayor migrates the rig "([^"]*)"$`, func(rig string) error {
		return c.migrate(rig, false)
	})
	ctx.When(`^the Mayor migrates the rig "([^"]*)" with --dry-run$`, func(rig string) error {
		return c.migrate(rig, true)
	})
	ctx.Then(`^the memory command printed "([^"]*)"$`, c.printedTheLine)
	ctx.Then(`^the memory command printed exactly:$`, c.printedExactly)
	ctx.Then(`^the file "([^"]*)" of the rig "([^"]*)" holds:$`, c.rigFileHolds)
	ctx.Then(`^the file "([^"]*)" of the rig "([^"]*)" has a text of (\d+) bytes$`, c.rigFileTextSize)
	ctx.Then(`^the fact file "([^"]*)" of the rig "([^"]*)" holds a fact with source "([^"]*)"$`, c.factHasSource)
	ctx.Then(`^the rig "([^"]*)" keeps no memory file and no archive file$`, func(rig string) error {
		return c.legacyFiles(rig, false, false)
	})
	ctx.Then(`^the rig "([^"]*)" keeps its memory file$`, func(rig string) error {
		return c.legacyFiles(rig, true, false)
	})
	ctx.Then(`^the rig "([^"]*)" keeps its archive file$`, func(rig string) error {
		return c.legacyFiles(rig, true, true)
	})
	ctx.Then(`^the rig "([^"]*)" has no facts folder$`, c.hasNoFactsFolder)
	ctx.Then(`^a boot of the rig "([^"]*)" reads (\d+) current facts after its about text$`, c.bootReads)

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

func (c *memoryContext) theRigHoldsFactsForQuerying(rig string) error {
	for _, f := range []struct{ slug, subject, status, source, sentence, extra string }{
		{"own-socket", "tmux", "current", "mw-10", "Every test needs its own socket.", "supersedes: old-socket\n"},
		{"old-socket", "tmux", "superseded", "mw-4", "Tests share the default socket.", "superseded-by: own-socket\n"},
		{"vault-flag", "bd", "current", "mw-2", "Point every call at the vault with -C.", ""},
		{"never-sync-in-tests", "bd", "current", "mw-3", "Never run sync from a test.", ""},
		{"gone-clock", "clock", "retired", "mw-5", "The wall clock steps back.", "retired: 2026-10-01\nreason: wsl2 fixed\n"},
		{"gate-make-check", "gate", "current", "mayor:2026-09-01", "The gate is make check.", ""},
	} {
		text := fmt.Sprintf("---\nsubject: %s\nkind: gotcha\nstatus: %s\nsource: %s\nsince: 2026-09-01\n%s---\n\n%s\n",
			f.subject, f.status, f.source, f.extra, f.sentence)
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
	files := vault.New(c.root)
	return application.Memory{
		Files: files, Legacy: files, Seat: memorySeat, Budget: budget,
		Now:  func() time.Time { return c.day },
		Head: func(context.Context) (string, error) { return memoryHead, nil },
		Out:  &c.out,
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

func (c *memoryContext) rigsDir() string {
	return filepath.Join(c.root, application.SeatsDir, memorySeat, application.RigsDir)
}

func (c *memoryContext) writeRigsFile(name, text string) error {
	if err := os.MkdirAll(c.rigsDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(c.rigsDir(), name), []byte(text+"\n"), 0o644)
}

func (c *memoryContext) theRigKeepsItsMemory(rig string, doc *godog.DocString) error {
	return c.writeRigsFile(rig+".md", doc.Content)
}

func (c *memoryContext) theRigKeepsItsArchive(rig string, doc *godog.DocString) error {
	return c.writeRigsFile(rig+"-archive.md", doc.Content)
}

// theRigKeepsALongHead writes a memory file whose head is size bytes of eleven
// byte sentences ("aaaaaaaaa. "), the last one cut short with no full stop.
func (c *memoryContext) theRigKeepsALongHead(rig, size string) error {
	n, _ := strconv.Atoi(size)
	head := strings.Repeat("aaaaaaaaa. ", n/11)
	head += strings.Repeat("b", n-len(head))
	return c.writeRigsFile(rig+".md", "# Rig memory: "+rig+"\n\n"+head+"\n\n- A fact (mw-1).")
}

func (c *memoryContext) migrate(rig string, dry bool) error {
	c.err = c.memory().Migrate(context.Background(), application.MemoryMigrate{Rig: rig, DryRun: dry})
	return nil
}

func (c *memoryContext) printedTheLine(want string) error {
	for _, line := range strings.Split(c.out.String(), "\n") {
		if line == want {
			return nil
		}
	}
	return fmt.Errorf("expected the command to print %q, got:\n%s", want, c.out.String())
}

func (c *memoryContext) printedExactly(doc *godog.DocString) error {
	if got := strings.TrimRight(c.out.String(), "\n"); got != doc.Content {
		return fmt.Errorf("the command printed:\n%s\nwant:\n%s", got, doc.Content)
	}
	return nil
}

func (c *memoryContext) rigFileHolds(name, rig string, doc *godog.DocString) error {
	got, err := os.ReadFile(filepath.Join(c.rigDir(rig), name))
	if err != nil {
		return err
	}
	if want := doc.Content + "\n"; string(got) != want {
		return fmt.Errorf("%s of the rig %s holds:\n%s\nwant:\n%s", name, rig, got, want)
	}
	return nil
}

func (c *memoryContext) rigFileTextSize(name, rig, size string) error {
	got, err := os.ReadFile(filepath.Join(c.rigDir(rig), name))
	if err != nil {
		return err
	}
	if want, _ := strconv.Atoi(size); len(strings.TrimRight(string(got), "\n")) != want {
		return fmt.Errorf("%s of the rig %s has a text of %d bytes, want %d", name, rig, len(strings.TrimRight(string(got), "\n")), want)
	}
	return nil
}

func (c *memoryContext) factHasSource(slug, rig, source string) error {
	text, err := os.ReadFile(filepath.Join(c.rigDir(rig), application.FactsDir, slug+".md"))
	if err != nil {
		return err
	}
	fact, err := application.ParseRigFact(slug, string(text))
	if err != nil {
		return err
	}
	if fact.Source != source {
		return fmt.Errorf("the fact %s has source %q, want %q", slug, fact.Source, source)
	}
	return nil
}

// legacyFiles checks the rig's one memory file; archive too when both is set.
// With neither it checks that both are gone.
func (c *memoryContext) legacyFiles(rig string, memory, archive bool) error {
	for _, f := range []struct {
		name string
		want bool
	}{{rig + ".md", memory}, {rig + "-archive.md", archive}} {
		_, err := os.Stat(filepath.Join(c.rigsDir(), f.name))
		if f.want && err != nil {
			return fmt.Errorf("expected %s to be there: %v", f.name, err)
		}
		if !memory && !archive && err == nil {
			return fmt.Errorf("expected %s to be gone", f.name)
		}
	}
	return nil
}

func (c *memoryContext) hasNoFactsFolder(rig string) error {
	if _, err := os.Stat(filepath.Join(c.rigDir(rig), application.FactsDir)); !os.IsNotExist(err) {
		return fmt.Errorf("expected no facts folder for the rig %s, stat said %v", rig, err)
	}
	return nil
}

// bootReads reads the rig as a Builder's boot does, through the vault.
func (c *memoryContext) bootReads(rig, count string) error {
	dir := filepath.Join(c.root, application.SeatsDir, memorySeat)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, application.CharterFileName), []byte("charter\n"), 0o644); err != nil {
		return err
	}
	seat, err := vault.New(c.root).Seat(context.Background(), memorySeat, rig)
	if err != nil {
		return err
	}
	if !seat.HasFacts || strings.TrimSpace(seat.Memory) == "" {
		return fmt.Errorf("the boot read the rig %s as facts=%v with about %q", rig, seat.HasFacts, seat.Memory)
	}
	render := application.RenderRigMemory(seat.Memory, seat.Facts)
	got := strings.Count(render, "\n- [")
	if want, _ := strconv.Atoi(count); got != want {
		return fmt.Errorf("the boot reads %d facts, want %d:\n%s", got, want, render)
	}
	if !strings.HasPrefix(render, strings.TrimSpace(seat.Memory)) {
		return fmt.Errorf("the boot does not open with the about text:\n%s", render)
	}
	return nil
}
