package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"

	"github.com/cucumber/godog"
)

// seatContextContext holds a temporary home directory, holding a config file
// and a Claude Code projects directory that is not ~/.claude: nothing here
// reads the machine's own transcripts or its own config.
type seatContextContext struct {
	home     string
	projects string
	dir      string

	// newest is the modification time given to the last transcript written, so
	// that each is newer than the one before it.
	newest time.Time

	envWas  string
	envSet  bool
	homeWas string

	report application.SeatContextReport
	err    error
}

// InitializeSeatContextScenario registers the steps of features/seat_context.feature.
func InitializeSeatContextScenario(ctx *godog.ScenarioContext) {
	c := &seatContextContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		home, err := os.MkdirTemp("", "mw-seat-context-")
		if err != nil {
			return ctx, err
		}
		envWas, envSet := os.LookupEnv(config.HandoffAtEnv)
		*c = seatContextContext{
			home:     home,
			projects: filepath.Join(home, "projects"),
			dir:      filepath.Join(home, "seat"),
			newest:   time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC),
			envWas:   envWas,
			envSet:   envSet,
			homeWas:  os.Getenv("HOME"),
		}
		// The limit is read out of the configuration, so that no scenario ever
		// reads this machine's own config file or environment.
		if err := os.Setenv("HOME", home); err != nil {
			return ctx, err
		}
		return ctx, os.Unsetenv(config.HandoffAtEnv)
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		_ = os.Setenv("HOME", c.homeWas)
		if c.envSet {
			_ = os.Setenv(config.HandoffAtEnv, c.envWas)
		} else {
			_ = os.Unsetenv(config.HandoffAtEnv)
		}
		return ctx, os.RemoveAll(c.home)
	})

	ctx.Given(`^a transcript "([^"]*)" for the seat directory whose assistant turns used:$`, c.aTranscriptWhoseTurnsUsed)
	ctx.Given(`^a newer transcript "([^"]*)" for the seat directory whose assistant turns used:$`, c.aTranscriptWhoseTurnsUsed)
	ctx.Given(`^a transcript "([^"]*)" for the seat directory with no assistant turn$`, c.aTranscriptWithNoAssistantTurn)
	ctx.Given(`^no transcript for the seat directory$`, c.noTranscript)
	ctx.Given(`^the config file says the handoff limit is (\d+)$`, c.theConfigFileSaysTheLimit)
	ctx.Given(`^the environment says the handoff limit is (\d+)$`, c.theEnvironmentSaysTheLimit)

	ctx.When(`^mw seat context reads the seat directory$`, c.mwSeatContextReadsTheSeatDirectory)

	ctx.Then(`^seat context succeeds$`, c.seatContextSucceeds)
	ctx.Then(`^the seat context line is "([^"]*)"$`, c.theLineIs)
	ctx.Then(`^seat context fails saying it looked in the seat directory's transcripts$`, c.failsNamingTheDirectory)
}

// writeTranscript writes one session's transcript into the projects directory
// of the seat directory, newer than any written before it.
func (c *seatContextContext) writeTranscript(session string, lines []string) error {
	dir := claude.ProjectDir(c.projects, c.dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, session+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	c.newest = c.newest.Add(time.Minute)
	return os.Chtimes(path, c.newest, c.newest)
}

func (c *seatContextContext) aTranscriptWhoseTurnsUsed(session string, table *godog.Table) error {
	if len(table.Rows) < 2 {
		return fmt.Errorf("a transcript's turns need a header row and at least one turn")
	}
	lines := []string{`{"type":"user","message":{"role":"user","content":"begin"}}`}
	for _, row := range table.Rows[1:] {
		usage := map[string]int{}
		for i, cell := range row.Cells {
			n, err := strconv.Atoi(cell.Value)
			if err != nil {
				return fmt.Errorf("%s is %q, not a number of tokens", table.Rows[0].Cells[i].Value, cell.Value)
			}
			usage[table.Rows[0].Cells[i].Value] = n
		}
		turn, err := json.Marshal(map[string]any{
			"type":    "assistant",
			"message": map[string]any{"role": "assistant", "usage": usage},
		})
		if err != nil {
			return err
		}
		lines = append(lines, string(turn), `{"type":"user","message":{"role":"user","content":"go on"}}`)
	}
	// What Claude Code writes that is not the session's own last turn: a
	// subagent's turn, which is another context, and a line cut short.
	lines = append(lines,
		`{"isSidechain":true,"type":"assistant","message":{"role":"assistant","usage":{"input_tokens":999999}}}`,
		`{"type":"assistant","message":{"usage":{"input_to`)
	return c.writeTranscript(session, lines)
}

func (c *seatContextContext) aTranscriptWithNoAssistantTurn(session string) error {
	return c.writeTranscript(session, []string{`{"type":"user","message":{"role":"user","content":"begin"}}`})
}

func (c *seatContextContext) noTranscript() error {
	return os.MkdirAll(c.dir, 0o755)
}

func (c *seatContextContext) theConfigFileSaysTheLimit(limit int) error {
	dir := filepath.Join(c.home, filepath.Dir(config.File))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(c.home, config.File), []byte(fmt.Sprintf("handoff_at = %d\n", limit)), 0o644)
}

func (c *seatContextContext) theEnvironmentSaysTheLimit(limit int) error {
	return os.Setenv(config.HandoffAtEnv, strconv.Itoa(limit))
}

func (c *seatContextContext) mwSeatContextReadsTheSeatDirectory() error {
	limit, err := config.HandoffAt()
	if err != nil {
		return fmt.Errorf("reading the handoff limit: %w", err)
	}
	c.report, c.err = application.SeatContext{
		Transcripts: claude.NewTranscripts(c.projects),
		Dir:         c.dir,
		HandoffAt:   limit,
	}.Run(context.Background())
	return nil
}

func (c *seatContextContext) seatContextSucceeds() error {
	if c.err != nil {
		return fmt.Errorf("expected seat context to succeed, got: %w", c.err)
	}
	return nil
}

func (c *seatContextContext) theLineIs(want string) error {
	if c.err != nil {
		return fmt.Errorf("expected the line %q, got the error: %w", want, c.err)
	}
	if got := c.report.String(); got != want {
		return fmt.Errorf("expected the line %q, got %q", want, got)
	}
	return nil
}

func (c *seatContextContext) failsNamingTheDirectory() error {
	if c.err == nil {
		return fmt.Errorf("expected seat context to fail, it printed %q", c.report.String())
	}
	want := claude.ProjectDir(c.projects, c.dir)
	if !strings.Contains(c.err.Error(), want) {
		return fmt.Errorf("expected the error to name %s, got: %w", want, c.err)
	}
	return nil
}
