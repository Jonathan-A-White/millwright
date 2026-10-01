package steps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// talkModelContext holds two throwaway keys, as talk_say_steps.go does, so that
// the spoken switch is encrypted and decrypted with the real cipher; a vault
// for the talk log; and a fake terminal with the Mayor's window open, which
// must be handed no key.
type talkModelContext struct {
	home        string
	vault       string
	mayor       *postern.KeyFile
	governor    *postern.KeyFile
	governorKey string
	backend     *apptest.FakePostern
	windows     *apptest.FakeWindows
	window      string

	out strings.Builder
	err error
}

// InitializeTalkModelScenario registers the steps of features/talk_model.feature.
func InitializeTalkModelScenario(ctx *godog.ScenarioContext) {
	c := &talkModelContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = talkModelContext{backend: apptest.NewFakePostern(), windows: apptest.NewFakeWindows()}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		for _, dir := range []string{c.home, c.vault} {
			if dir != "" {
				_ = os.RemoveAll(dir)
			}
		}
		return ctx, nil
	})

	ctx.Given(`^a throwaway Mayor postern key for the model switch$`, c.aThrowawayMayorKey)
	ctx.Given(`^a throwaway Governor key for the model switch$`, c.aThrowawayGovernorKey)
	ctx.Given(`^a vault for the model switch$`, c.aVault)
	ctx.Given(`^the Mayor's window "([^"]*)" is open in a fake terminal$`, c.theMayorsWindowIsOpen)
	ctx.Given(`^the postern backend will not take the switch$`, c.theBackendWillNotTakeIt)

	ctx.When(`^mw talk model "([^"]*)" is run for talk "([^"]*)" turn (\d+)$`, c.run)

	ctx.Then(`^no key was typed into any window$`, c.noKeyTyped)
	ctx.Then(`^the Governor hears "([^"]*)" on talk "([^"]*)" turn (\d+)$`, c.governorHears)
	ctx.Then(`^mw talk model printed "([^"]*)"$`, c.printed)
	ctx.Then(`^mw talk model printed nothing$`, c.printedNothing)
	ctx.Then(`^mw talk model is refused saying "([^"]*)"$`, c.refusedSaying)
	ctx.Then(`^nothing was spoken on the talk$`, c.nothingSpoken)
	ctx.Then(`^the model switch was not logged$`, c.notLogged)
	ctx.Then(`^the talk log's last line says "([^"]*)"$`, c.theTalkLogsLastLineSays)
}

func (c *talkModelContext) aThrowawayMayorKey() error {
	home, err := os.MkdirTemp("", "mw-talk-model-")
	if err != nil {
		return err
	}
	c.home = home
	c.mayor = postern.New(filepath.Join(home, "mayor.key"))
	return c.mayor.Generate()
}

func (c *talkModelContext) aThrowawayGovernorKey() error {
	c.governor = postern.New(filepath.Join(c.home, "governor.key"))
	if err := c.governor.Generate(); err != nil {
		return err
	}
	pub, _, err := c.governor.PublicKey()
	c.governorKey = pub
	return err
}

func (c *talkModelContext) aVault() error {
	dir, err := os.MkdirTemp("", "mw-talk-model-vault-")
	c.vault = dir
	return err
}

func (c *talkModelContext) theMayorsWindowIsOpen(id string) error {
	c.window = id
	c.windows.HoldsWithID(id, "mayor-2026-10-01-141", time.Time{})
	return nil
}

func (c *talkModelContext) theBackendWillNotTakeIt() error {
	c.backend.DeliverErr = errors.New("backend said no")
	return nil
}

func (c *talkModelContext) run(chip, talk string, turn int) error {
	c.out.Reset()
	files := vault.New(c.vault)
	_, c.err = application.TalkModel{
		Say: application.TalkSay{
			Postern:     c.backend,
			Cipher:      postern.NewCipher(c.mayor),
			Keys:        c.mayor,
			GovernorKey: c.governorKey,
		},
		Log:   files,
		Seat:  "mayor",
		Model: domain.Model(chip),
		Out:   &c.out,
	}.Run(context.Background(), application.TalkModelRequest{TalkID: talk, Turn: turn})
	return nil
}

func (c *talkModelContext) noKeyTyped() error {
	if typed := c.windows.Typed(c.window); len(typed) != 0 {
		return fmt.Errorf("expected nothing typed into the Mayor's window, it was typed %q", typed)
	}
	return nil
}

// spoken is the one turn handed to the backend, as the Governor reads it.
func (c *talkModelContext) spoken() (application.TalkTurn, error) {
	var none application.TalkTurn
	if c.err != nil {
		return none, fmt.Errorf("expected the switch to succeed, got: %w", c.err)
	}
	delivered := c.backend.Delivered()
	if len(delivered) != 1 {
		return none, fmt.Errorf("expected one turn spoken, got %d", len(delivered))
	}
	var record application.PosternPayload
	if err := json.Unmarshal(delivered[0], &record); err != nil {
		return none, err
	}
	wif, err := c.governor.PrivateKeyWIF()
	if err != nil {
		return none, err
	}
	plain, _, err := postern.NewCipher(c.governor).Decrypt(wif, record.Ct)
	if err != nil {
		return none, fmt.Errorf("the Governor could not decrypt the record: %w", err)
	}
	var turn application.TalkTurn
	if err := json.Unmarshal([]byte(plain), &turn); err != nil {
		return none, fmt.Errorf("the plaintext %q is not section 20 JSON: %w", plain, err)
	}
	return turn, nil
}

func (c *talkModelContext) governorHears(text, talk string, turn int) error {
	heard, err := c.spoken()
	if err != nil {
		return err
	}
	if heard.Talk.ID != talk || heard.Talk.Turn != turn || heard.Role != application.TalkRoleAnswer || heard.Text != text {
		return fmt.Errorf("expected talk %q turn %d answer %q, got %+v", talk, turn, text, heard)
	}
	return nil
}

func (c *talkModelContext) printed(want string) error {
	if c.err != nil {
		return fmt.Errorf("expected the switch to succeed, got: %w", c.err)
	}
	if got := strings.TrimSuffix(c.out.String(), "\n"); got != want && !strings.HasSuffix(got, want) {
		return fmt.Errorf("expected mw talk model to print %q, it printed %q", want, c.out.String())
	}
	if strings.Count(c.out.String(), "\n") != 1 {
		return fmt.Errorf("expected one line printed, it printed %q", c.out.String())
	}
	return nil
}

func (c *talkModelContext) printedNothing() error {
	if c.out.Len() != 0 {
		return fmt.Errorf("expected nothing printed, it printed %q", c.out.String())
	}
	return nil
}

func (c *talkModelContext) refusedSaying(want string) error {
	if c.err == nil || !strings.Contains(c.err.Error(), want) {
		return fmt.Errorf("expected mw talk model to be refused saying %q, it ended with %v", want, c.err)
	}
	return nil
}

func (c *talkModelContext) nothingSpoken() error {
	if n := len(c.backend.Delivered()); n != 0 {
		return fmt.Errorf("expected nothing spoken, %d turns were delivered", n)
	}
	return nil
}

func (c *talkModelContext) logLines() ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(c.vault, application.TalkLogFileName("mayor")))
	if err != nil {
		return nil, fmt.Errorf("reading the talk log: %w", err)
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n"), nil
}

func (c *talkModelContext) notLogged() error {
	if _, err := os.Stat(filepath.Join(c.vault, application.TalkLogFileName("mayor"))); !os.IsNotExist(err) {
		return fmt.Errorf("expected no talk log, got %v", err)
	}
	return nil
}

// theTalkLogsLastLineSays checks the last line of the log: dated, named for
// the switch, then saying what it did.
func (c *talkModelContext) theTalkLogsLastLineSays(want string) error {
	lines, err := c.logLines()
	if err != nil {
		return err
	}
	line := lines[len(lines)-1]
	stamp, rest, ok := strings.Cut(line, " talk model ")
	if _, err := time.Parse(time.RFC3339, stamp); !ok || err != nil {
		return fmt.Errorf("expected a dated talk model line, got %q", line)
	}
	if _, said, _ := strings.Cut(rest, ": "); said != want {
		return fmt.Errorf("expected the talk log to say %q, it says %q", want, line)
	}
	return nil
}
