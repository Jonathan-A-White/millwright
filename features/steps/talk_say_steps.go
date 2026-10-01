package steps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"

	"github.com/cucumber/godog"
)

// talkSayContext holds two throwaway keys, the Mayor's and the Governor's, so
// that the answer is encrypted and decrypted with the real cipher, and a fake
// backend that keeps what it is handed.
type talkSayContext struct {
	home        string
	mayor       *postern.KeyFile
	governor    *postern.KeyFile
	governorKey string
	backend     *apptest.FakePostern

	out    strings.Builder
	report application.TalkSayReport
	// callReport is what the last mw talk call did.
	callReport application.TalkCallReport
	err        error
}

// InitializeTalkSayScenario registers the steps of features/talk_say.feature.
func InitializeTalkSayScenario(ctx *godog.ScenarioContext) {
	c := &talkSayContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = talkSayContext{backend: apptest.NewFakePostern()}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.home != "" {
			os.RemoveAll(c.home)
		}
		return ctx, nil
	})

	ctx.Given(`^a throwaway Mayor postern key for talking$`, c.aThrowawayMayorKey)
	ctx.Given(`^a throwaway Governor key for talking$`, c.aThrowawayGovernorKey)
	ctx.Given(`^the talk Governor's key is not set$`, c.theGovernorsKeyIsNotSet)
	ctx.Given(`^the postern backend will not take a talk record$`, c.theBackendWillNotTakeARecord)

	ctx.When(`^mw talk say "([^"]*)" is run for talk "([^"]*)" turn (\d+)$`, func(text, talk string, turn int) error {
		return c.run(application.TalkSayRequest{Text: text, TalkID: talk, Turn: turn})
	})
	ctx.When(`^mw talk say "([^"]*)" is run holding for talk "([^"]*)" turn (\d+)$`, func(text, talk string, turn int) error {
		return c.run(application.TalkSayRequest{Text: text, TalkID: talk, Turn: turn, Holding: true})
	})
	ctx.When(`^mw talk say "([^"]*)" is run ending for talk "([^"]*)" turn (\d+)$`, func(text, talk string, turn int) error {
		return c.run(application.TalkSayRequest{Text: text, TalkID: talk, Turn: turn, End: true})
	})
	ctx.When(`^mw talk say "([^"]*)" is run holding and ending for talk "([^"]*)" turn (\d+)$`, func(text, talk string, turn int) error {
		return c.run(application.TalkSayRequest{Text: text, TalkID: talk, Turn: turn, Holding: true, End: true})
	})

	ctx.When(`^mw talk say "([^"]*)" is run with links "([^"]*)" and "([^"]*)" for talk "([^"]*)" turn (\d+)$`, func(text, first, second, talk string, turn int) error {
		return c.run(application.TalkSayRequest{Text: text, TalkID: talk, Turn: turn, Links: []string{first, second}})
	})

	ctx.Then(`^the talk record's links are "([^"]*)" and "([^"]*)"$`, c.linksAre)
	ctx.Then(`^the talk record was delivered directly, and nothing was broadcast$`, c.deliveredDirectly)
	ctx.Then(`^the delivered talk record's class is "([^"]*)"$`, c.deliveredClassIs)
	ctx.Then(`^the delivered talk record carries no summary$`, c.deliveredHasNoSummary)
	ctx.Then(`^the delivered talk record is addressed to the Governor from the Mayor$`, c.deliveredAddressing)
	ctx.Then(`^the Governor decrypts the talk record's plaintext to talk "([^"]*)" turn (\d+) role "([^"]*)" saying "([^"]*)"$`, c.governorDecrypts)
	ctx.Then(`^it prints the txid and the elapsed milliseconds$`, c.itPrintsTxidAndElapsed)
	ctx.Then(`^talk say is refused saying "([^"]*)"$`, c.refusedSaying)
	ctx.Then(`^nothing was delivered for talking$`, c.nothingDelivered)

	c.registerCall(ctx)
}

func (c *talkSayContext) aThrowawayMayorKey() error {
	home, err := os.MkdirTemp("", "mw-talk-say-")
	if err != nil {
		return err
	}
	c.home = home
	c.mayor = postern.New(filepath.Join(home, "mayor.key"))
	return c.mayor.Generate()
}

func (c *talkSayContext) aThrowawayGovernorKey() error {
	c.governor = postern.New(filepath.Join(c.home, "governor.key"))
	if err := c.governor.Generate(); err != nil {
		return err
	}
	pub, _, err := c.governor.PublicKey()
	c.governorKey = pub
	return err
}

func (c *talkSayContext) theGovernorsKeyIsNotSet() error {
	c.governorKey = ""
	return nil
}

func (c *talkSayContext) theBackendWillNotTakeARecord() error {
	c.backend.DeliverErr = errors.New("backend said no")
	return nil
}

func (c *talkSayContext) run(request application.TalkSayRequest) error {
	c.out.Reset()
	c.report, c.err = application.TalkSay{
		Postern:     c.backend,
		Cipher:      postern.NewCipher(c.mayor),
		Keys:        c.mayor,
		GovernorKey: c.governorKey,
		Out:         &c.out,
	}.Run(context.Background(), request)
	return nil
}

// delivered is the one record handed to the backend.
func (c *talkSayContext) delivered() ([]byte, error) {
	if c.err != nil {
		return nil, fmt.Errorf("expected it to succeed, got: %w", c.err)
	}
	delivered := c.backend.Delivered()
	if len(delivered) != 1 {
		return nil, fmt.Errorf("expected one delivery, got %d", len(delivered))
	}
	return delivered[0], nil
}

func (c *talkSayContext) deliveredDirectly() error {
	if _, err := c.delivered(); err != nil {
		return err
	}
	if n := len(c.backend.Broadcasts()); n != 0 {
		return fmt.Errorf("expected nothing broadcast, got %d broadcasts", n)
	}
	return nil
}

func (c *talkSayContext) deliveredClassIs(want string) error {
	payload, err := c.delivered()
	if err != nil {
		return err
	}
	var record application.PosternPayload
	if err := json.Unmarshal(payload, &record); err != nil {
		return err
	}
	if record.Class != want {
		return fmt.Errorf("expected class %q, got %q", want, record.Class)
	}
	return nil
}

func (c *talkSayContext) deliveredHasNoSummary() error {
	payload, err := c.delivered()
	if err != nil {
		return err
	}
	summary, ok, err := summaryOf(payload)
	if err != nil {
		return err
	}
	if ok {
		return fmt.Errorf("expected no summary key, got %q in %s", summary, payload)
	}
	return nil
}

func (c *talkSayContext) deliveredAddressing() error {
	payload, err := c.delivered()
	if err != nil {
		return err
	}
	var record application.PosternPayload
	if err := json.Unmarshal(payload, &record); err != nil {
		return err
	}
	mayor, _, err := c.mayor.PublicKey()
	if err != nil {
		return err
	}
	if record.To != c.governorKey || record.From != mayor {
		return fmt.Errorf("expected from %s to %s, got from %s to %s", mayor, c.governorKey, record.From, record.To)
	}
	return nil
}

func (c *talkSayContext) governorDecrypts(talk string, turn int, role, text string) error {
	turnPlain, err := c.plaintext()
	if err != nil {
		return err
	}
	if turnPlain.Talk.ID != talk || turnPlain.Talk.Turn != turn || turnPlain.Role != role || turnPlain.Text != text {
		return fmt.Errorf("expected talk %q turn %d role %q text %q, got %+v", talk, turn, role, text, turnPlain)
	}
	return nil
}

func (c *talkSayContext) linksAre(first, second string) error {
	turnPlain, err := c.plaintext()
	if err != nil {
		return err
	}
	if len(turnPlain.Links) != 2 || turnPlain.Links[0] != first || turnPlain.Links[1] != second {
		return fmt.Errorf("expected links [%s %s], got %v", first, second, turnPlain.Links)
	}
	return nil
}

// plaintext is the delivered record's plaintext as the Governor reads it.
func (c *talkSayContext) plaintext() (application.TalkTurn, error) {
	var none application.TalkTurn
	payload, err := c.delivered()
	if err != nil {
		return none, err
	}
	var record application.PosternPayload
	if err := json.Unmarshal(payload, &record); err != nil {
		return none, err
	}
	wif, err := c.governor.PrivateKeyWIF()
	if err != nil {
		return none, err
	}
	plain, from, err := postern.NewCipher(c.governor).Decrypt(wif, record.Ct)
	if err != nil {
		return none, fmt.Errorf("the Governor could not decrypt the record: %w", err)
	}
	mayor, _, err := c.mayor.PublicKey()
	if err != nil {
		return none, err
	}
	if from != mayor {
		return none, fmt.Errorf("expected the envelope from the Mayor's key %s, got %s", mayor, from)
	}
	var turnPlain application.TalkTurn
	if err := json.Unmarshal([]byte(plain), &turnPlain); err != nil {
		return none, fmt.Errorf("the plaintext %q is not section 20 JSON: %w", plain, err)
	}
	return turnPlain, nil
}

func (c *talkSayContext) itPrintsTxidAndElapsed() error {
	if c.err != nil {
		return c.err
	}
	printed := c.out.String()
	if c.report.Txid == "" || !strings.Contains(printed, c.report.Txid) {
		return fmt.Errorf("expected the txid %q in %q", c.report.Txid, printed)
	}
	if !regexp.MustCompile(`(?m)^elapsed \d+ ms$`).MatchString(printed) {
		return fmt.Errorf("expected a line 'elapsed <n> ms' in %q", printed)
	}
	return nil
}

func (c *talkSayContext) refusedSaying(want string) error {
	if c.err == nil {
		return fmt.Errorf("expected it to be refused saying %q, but it succeeded", want)
	}
	if !strings.Contains(c.err.Error(), want) {
		return fmt.Errorf("expected the refusal to say %q, got: %w", want, c.err)
	}
	return nil
}

func (c *talkSayContext) nothingDelivered() error {
	if n := len(c.backend.Delivered()); n != 0 {
		return fmt.Errorf("expected nothing delivered, got %d records", n)
	}
	return nil
}
