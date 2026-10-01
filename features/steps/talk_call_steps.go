package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"

	"github.com/cucumber/godog"
)

// registerCall adds the steps of features/talk_call.feature to the talk say
// context: the two features share its throwaway keys, its fake backend and the
// steps that read what was delivered.
func (c *talkSayContext) registerCall(ctx *godog.ScenarioContext) {
	ctx.When(`^mw talk call "([^"]*)" is run$`, func(text string) error {
		return c.runCall(application.TalkCallRequest{Text: text})
	})
	ctx.When(`^mw talk call "([^"]*)" is run with links "([^"]*)" and "([^"]*)"$`, func(text, first, second string) error {
		return c.runCall(application.TalkCallRequest{Text: text, Links: []string{first, second}})
	})
	ctx.Then(`^the Governor decrypts the call record's plaintext to role "([^"]*)" saying "([^"]*)"$`, c.governorDecryptsCall)
	ctx.Then(`^the call record says when it was sent$`, c.callSaysWhen)
	ctx.Then(`^the call record's links are "([^"]*)" and "([^"]*)"$`, c.callLinksAre)
	ctx.Then(`^it prints "([^"]*)" and the elapsed milliseconds$`, c.itPrintsTxidPrefixAndElapsed)
	ctx.Then(`^talk call is refused saying "([^"]*)"$`, c.refusedSaying)
}

func (c *talkSayContext) runCall(request application.TalkCallRequest) error {
	c.out.Reset()
	c.err = nil
	c.callReport, c.err = application.TalkCall{
		Postern:     c.backend,
		Cipher:      postern.NewCipher(c.mayor),
		Keys:        c.mayor,
		GovernorKey: c.governorKey,
		Out:         &c.out,
	}.Run(context.Background(), request)
	return nil
}

// callPlain is the delivered record's plaintext as the Governor reads it.
func (c *talkSayContext) callPlain() (application.CallRecord, error) {
	var none application.CallRecord
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
	var call application.CallRecord
	if err := json.Unmarshal([]byte(plain), &call); err != nil {
		return none, fmt.Errorf("the plaintext %q is not section 21 JSON: %w", plain, err)
	}
	return call, nil
}

func (c *talkSayContext) governorDecryptsCall(role, text string) error {
	call, err := c.callPlain()
	if err != nil {
		return err
	}
	if call.Role != role || call.Text != text {
		return fmt.Errorf("expected role %q text %q, got %+v", role, text, call)
	}
	return nil
}

func (c *talkSayContext) callSaysWhen() error {
	call, err := c.callPlain()
	if err != nil {
		return err
	}
	if age := time.Since(time.Unix(call.At, 0)); call.At == 0 || age < -time.Minute || age > time.Minute {
		return fmt.Errorf("expected at to be the Unix seconds of now, got %d", call.At)
	}
	return nil
}

func (c *talkSayContext) callLinksAre(first, second string) error {
	call, err := c.callPlain()
	if err != nil {
		return err
	}
	if len(call.Links) != 2 || call.Links[0] != first || call.Links[1] != second {
		return fmt.Errorf("expected links [%s %s], got %v", first, second, call.Links)
	}
	return nil
}

func (c *talkSayContext) itPrintsTxidPrefixAndElapsed(prefix string) error {
	if c.err != nil {
		return c.err
	}
	printed := c.out.String()
	if c.callReport.Txid == "" || !strings.Contains(printed, prefix) || !strings.Contains(printed, c.callReport.Txid) {
		return fmt.Errorf("expected %q and the txid %q in %q", prefix, c.callReport.Txid, printed)
	}
	if !regexp.MustCompile(`(?m)^elapsed \d+ ms$`).MatchString(printed) {
		return fmt.Errorf("expected a line 'elapsed <n> ms' in %q", printed)
	}
	return nil
}
