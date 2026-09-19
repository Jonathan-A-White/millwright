package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// seatBootContext holds the vault a scenario writes its seat into, the story
// being booted, and what assembling it produced.
type seatBootContext struct {
	dir      string // the vault
	detail   application.StoryDetail
	worktree string
	seat     string
	spec     application.SessionSpec
	err      error
}

// InitializeSeatBootScenario registers the steps of features/seat_boot.feature.
func InitializeSeatBootScenario(ctx *godog.ScenarioContext) {
	c := &seatBootContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = seatBootContext{}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.dir != "" {
			_ = os.RemoveAll(c.dir)
		}
		return ctx, nil
	})

	ctx.Given(`^a vault$`, c.aVault)
	ctx.Given(`^the vault holds the charter of the "([^"]*)" seat$`, c.theVaultHoldsTheCharterOf)
	ctx.Given(`^the vault holds the "([^"]*)" seat's memory of the rig "([^"]*)"$`, c.theVaultHoldsTheMemoryOf)
	ctx.Given(`^the "([^"]*)" seat has no memory of the rig "([^"]*)"$`, c.theSeatHasNoMemoryOf)
	ctx.Given(`^the vault also holds a ledger, a postmortem and a memory of another rig$`, c.theVaultAlsoHoldsTheRest)
	ctx.Given(`^a story "([^"]*)" with the path:$`, c.aStoryWithThePath)
	ctx.Given(`^the story is titled:$`, c.theStoryIsTitled)
	ctx.Given(`^the story's acceptance criteria are:$`, c.theStoryAcceptanceCriteriaAre)
	ctx.Given(`^the worktree "([^"]*)"$`, c.theWorktree)

	ctx.When(`^the session that works the story is assembled for the "([^"]*)" seat$`, c.theSessionIsAssembled)

	ctx.Then(`^the boot file holds, in this order:$`, c.theBootFileHoldsInThisOrder)
	ctx.Then(`^the boot file holds none of:$`, c.theBootFileHoldsNoneOf)
	ctx.Then(`^the command line carries "([^"]*)"$`, c.theCommandLineCarries)
	ctx.Then(`^the settings on the command line are one JSON document that signs nothing and allows bd$`, c.theSettingsAreOneJSONDocument)
	ctx.Then(`^the command line primes the session from the vault's "([^"]*)"$`, c.theCommandLinePrimesFrom)
	ctx.Then(`^the command line writes the result to the vault's "([^"]*)"$`, c.theCommandLineWritesTheResultTo)
	ctx.Then(`^the session runs in "([^"]*)"$`, c.theSessionRunsIn)
	ctx.Then(`^the session's environment holds:$`, c.theEnvironmentHolds)
	ctx.Then(`^the kickoff prompt holds:$`, c.theKickoffPromptHolds)
	ctx.Then(`^the kickoff prompt holds none of:$`, c.theKickoffPromptHoldsNoneOf)
	ctx.Then(`^the kickoff prompt points bd at the vault with "([^"]*)" and its path$`, c.theKickoffPromptPointsBdAtTheVault)
	ctx.Then(`^the command is one shell line$`, c.theCommandIsOneShellLine)
	ctx.Then(`^the command line holds no newline$`, c.theCommandLineHoldsNoNewline)
}

// The text the fixture seat files hold, so that a scenario can say which of
// them reached the boot file and which did not.
const (
	fixtureCharter    = "# Builder — charter\n\nYou are the Builder of millwright, and you work one story.\n"
	fixtureRigMemory  = "# Builder's memory: rig millwright\n\nGo is at /usr/local/go/bin; one go command at a time.\n"
	fixtureOtherRig   = "# Builder's memory: rig fellowship\n\nthe fellowship rig is elsewhere.\n"
	fixtureLedger     = "2026-09-01 mw-old worked and closed here.\n"
	fixturePostmortem = "The story that failed: what went wrong last time.\n"
)

func (c *seatBootContext) aVault() error {
	dir, err := os.MkdirTemp("", "mw-vault-")
	if err != nil {
		return fmt.Errorf("making a vault: %w", err)
	}
	c.dir = dir
	return nil
}

// write puts one file in the vault, making the directories above it.
func (c *seatBootContext) write(contents string, parts ...string) error {
	path := filepath.Join(append([]string{c.dir}, parts...)...)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("making %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func (c *seatBootContext) theVaultHoldsTheCharterOf(seat string) error {
	return c.write(fixtureCharter, "seats", seat, "charter.md")
}

func (c *seatBootContext) theVaultHoldsTheMemoryOf(seat, rig string) error {
	return c.write(fixtureRigMemory, "seats", seat, "rigs", rig+".md")
}

func (c *seatBootContext) theSeatHasNoMemoryOf(seat, rig string) error {
	return os.RemoveAll(filepath.Join(c.dir, "seats", seat, "rigs", rig+".md"))
}

func (c *seatBootContext) theVaultAlsoHoldsTheRest() error {
	if err := c.write(fixtureOtherRig, "seats", "builder", "rigs", "fellowship.md"); err != nil {
		return err
	}
	if err := c.write(fixtureLedger, "seats", "builder", "ledger.md"); err != nil {
		return err
	}
	return c.write(fixturePostmortem, "seats", "builder", "postmortems", "2026-09-01-mw-old.md")
}

func (c *seatBootContext) aStoryWithThePath(id string, table *godog.Table) error {
	var path domain.Path
	for _, row := range table.Rows {
		if len(row.Cells) != 2 {
			return fmt.Errorf("path rows need a field and a value, got %d cells", len(row.Cells))
		}
		if err := path.Set(row.Cells[0].Value, row.Cells[1].Value); err != nil {
			return err
		}
	}
	c.detail = application.StoryDetail{Story: domain.Story{ID: id, Overrides: path}}
	return nil
}

func (c *seatBootContext) theStoryIsTitled(title *godog.DocString) error {
	c.detail.Story.Title = title.Content
	return nil
}

func (c *seatBootContext) theStoryAcceptanceCriteriaAre(acceptance *godog.DocString) error {
	c.detail.Acceptance = acceptance.Content
	return nil
}

func (c *seatBootContext) theWorktree(dir string) error {
	c.worktree = dir
	return nil
}

func (c *seatBootContext) theSessionIsAssembled(seat string) error {
	c.seat = seat
	boot := application.SeatBoot{
		Vault:   vault.New(c.dir),
		Harness: claude.New(),
		Seat:    seat,
		Host:    "vps",
	}
	c.spec, c.err = boot.Boot(context.Background(), c.detail, c.worktree)
	return nil
}

// assembled is the session spec the last When produced, or the reason there is
// none.
func (c *seatBootContext) assembled() (application.SessionSpec, error) {
	if c.err != nil {
		return application.SessionSpec{}, fmt.Errorf("assembling the session failed: %w", c.err)
	}
	return c.spec, nil
}

// line is the shell line the session is started with.
func (c *seatBootContext) line() (string, error) {
	spec, err := c.assembled()
	if err != nil {
		return "", err
	}
	return spec.Command[len(spec.Command)-1], nil
}

// bootFile is what was written to the boot file in the vault.
func (c *seatBootContext) bootFile() (string, error) {
	if _, err := c.assembled(); err != nil {
		return "", err
	}
	path := filepath.Join(c.dir, "runs", c.detail.Story.ID, application.BootFileName)
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading the boot file: %w", err)
	}
	return string(contents), nil
}

func (c *seatBootContext) theBootFileHoldsInThisOrder(table *godog.Table) error {
	contents, err := c.bootFile()
	if err != nil {
		return err
	}
	at := 0
	for _, row := range table.Rows {
		want := row.Cells[0].Value
		found := strings.Index(contents[at:], want)
		if found < 0 {
			if strings.Contains(contents, want) {
				return fmt.Errorf("the boot file holds %q, but out of order", want)
			}
			return fmt.Errorf("the boot file does not hold %q", want)
		}
		at += found + len(want)
	}
	return nil
}

func (c *seatBootContext) theBootFileHoldsNoneOf(table *godog.Table) error {
	contents, err := c.bootFile()
	if err != nil {
		return err
	}
	for _, row := range table.Rows {
		if want := row.Cells[0].Value; strings.Contains(contents, want) {
			return fmt.Errorf("the boot file holds %q, which does not belong in it", want)
		}
	}
	return nil
}

func (c *seatBootContext) theCommandLineCarries(want string) error {
	line, err := c.line()
	if err != nil {
		return err
	}
	if !strings.Contains(line, want) {
		return fmt.Errorf("expected the command line to carry %q, got %q", want, line)
	}
	return nil
}

// theSettingsAreOneJSONDocument reads the word after --settings back off the
// shell line and checks that it is the whole of what the session is told: one
// JSON document, no file on disk, holding both the attribution keys that keep a
// machine's name off the work and the allow rule that lets the session run bd
// without the auto-mode classifier being asked.
func (c *seatBootContext) theSettingsAreOneJSONDocument() error {
	line, err := c.line()
	if err != nil {
		return err
	}
	_, after, found := strings.Cut(line, "--settings ")
	if !found {
		return fmt.Errorf("expected the command line to carry --settings, got %q", line)
	}
	word := after
	if strings.HasPrefix(after, "'") {
		word, _, found = strings.Cut(after[1:], "'")
		if !found {
			return fmt.Errorf("the settings are not one quoted word: %q", after)
		}
	} else {
		word, _, _ = strings.Cut(after, " ")
	}

	var settings struct {
		Attribution struct {
			Commit     *string `json:"commit"`
			PR         *string `json:"pr"`
			SessionURL *bool   `json:"sessionUrl"`
		} `json:"attribution"`
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(word), &settings); err != nil {
		return fmt.Errorf("the settings are not one JSON document (%q): %w", word, err)
	}
	switch {
	case settings.Attribution.Commit == nil || *settings.Attribution.Commit != "":
		return fmt.Errorf("expected attribution.commit to be the empty string, got %v", settings.Attribution.Commit)
	case settings.Attribution.PR == nil || *settings.Attribution.PR != "":
		return fmt.Errorf("expected attribution.pr to be the empty string, got %v", settings.Attribution.PR)
	case settings.Attribution.SessionURL == nil || *settings.Attribution.SessionURL:
		return fmt.Errorf("expected attribution.sessionUrl to be false, got %v", settings.Attribution.SessionURL)
	}
	if !slices.Contains(settings.Permissions.Allow, claude.BeadsAllowRule) {
		return fmt.Errorf("expected permissions.allow to hold %q, got %v", claude.BeadsAllowRule, settings.Permissions.Allow)
	}
	return nil
}

func (c *seatBootContext) theCommandLinePrimesFrom(relative string) error {
	return c.theCommandLineCarries("--append-system-prompt-file " + filepath.Join(c.dir, relative))
}

func (c *seatBootContext) theCommandLineWritesTheResultTo(relative string) error {
	return c.theCommandLineCarries("> " + filepath.Join(c.dir, relative))
}

func (c *seatBootContext) theSessionRunsIn(dir string) error {
	spec, err := c.assembled()
	if err != nil {
		return err
	}
	if spec.Dir != dir {
		return fmt.Errorf("expected the session to run in %q, got %q", dir, spec.Dir)
	}
	return nil
}

func (c *seatBootContext) theEnvironmentHolds(table *godog.Table) error {
	spec, err := c.assembled()
	if err != nil {
		return err
	}
	for _, row := range table.Rows {
		if len(row.Cells) != 2 {
			return fmt.Errorf("environment rows need a name and a value, got %d cells", len(row.Cells))
		}
		name, want := row.Cells[0].Value, row.Cells[1].Value
		if got := spec.Env[name]; got != want {
			return fmt.Errorf("expected %s to be %q, got %q", name, want, got)
		}
	}
	return nil
}

func (c *seatBootContext) theKickoffPromptHolds(table *godog.Table) error {
	line, err := c.line()
	if err != nil {
		return err
	}
	prompt := application.KickoffPrompt(c.seat, c.detail.Story.ID, c.dir)
	for _, row := range table.Rows {
		if want := row.Cells[0].Value; !strings.Contains(prompt, want) {
			return fmt.Errorf("expected the kickoff prompt to hold %q, got %q", want, prompt)
		}
	}
	// The prompt is one word of the line; how a quote inside it is written is
	// the harness adapter's business, so this looks only as far as the first
	// character quoting would change.
	head, _, _ := strings.Cut(prompt, "'")
	if !strings.Contains(line, head) {
		return fmt.Errorf("expected the command line to carry the kickoff prompt, got %q", line)
	}
	return nil
}

func (c *seatBootContext) theKickoffPromptHoldsNoneOf(table *godog.Table) error {
	if _, err := c.assembled(); err != nil {
		return err
	}
	prompt := application.KickoffPrompt(c.seat, c.detail.Story.ID, c.dir)
	for _, row := range table.Rows {
		if unwanted := row.Cells[0].Value; strings.Contains(prompt, unwanted) {
			return fmt.Errorf("the kickoff prompt holds %q, which does not belong in it: %q", unwanted, prompt)
		}
	}
	return nil
}

// theKickoffPromptPointsBdAtTheVault checks the command the session is told to
// type: the flag, then the path of the vault this scenario assembled from — the
// literal path, so that no session has to work one out — and that the session is
// really given that prompt.
func (c *seatBootContext) theKickoffPromptPointsBdAtTheVault(flag string) error {
	line, err := c.line()
	if err != nil {
		return err
	}
	want := flag + c.dir + " "
	prompt := application.KickoffPrompt(c.seat, c.detail.Story.ID, c.dir)
	if !strings.Contains(prompt, want) {
		return fmt.Errorf("expected the kickoff prompt to hold %q, got %q", want, prompt)
	}
	if !strings.Contains(line, want) {
		return fmt.Errorf("expected the command line to carry %q, got %q", want, line)
	}
	return nil
}

func (c *seatBootContext) theCommandIsOneShellLine() error {
	spec, err := c.assembled()
	if err != nil {
		return err
	}
	if len(spec.Command) != 3 || spec.Command[1] != "-c" {
		return fmt.Errorf("expected a shell and one line, got %q", spec.Command)
	}
	return nil
}

func (c *seatBootContext) theCommandLineHoldsNoNewline() error {
	line, err := c.line()
	if err != nil {
		return err
	}
	if strings.ContainsAny(line, "\n\r") {
		return fmt.Errorf("expected one line, got %q", line)
	}
	return nil
}
