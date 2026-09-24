package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"

	"github.com/cucumber/godog"
)

// posternSendDummyTxid stands in for a previous transaction's id: 64 hex
// characters, valid shape, never a real one.
const posternSendDummyTxid = "1111111111111111111111111111111111111111111111111111111111111111"

// posternSendContext holds a throwaway postern key, a fake backend and
// cipher, and the settings mw postern send reads from config, so that it can
// be exercised with no network and no real chain.
type posternSendContext struct {
	home    string
	keys    *postern.KeyFile
	address string
	backend *apptest.FakePostern
	cipher  *apptest.FakeCipher

	governorKey string
	floatSats   int64

	txid string
	err  error
}

// InitializePosternSendScenario registers the steps of
// features/postern_send.feature.
func InitializePosternSendScenario(ctx *godog.ScenarioContext) {
	c := &posternSendContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = posternSendContext{
			backend: apptest.NewFakePostern(),
			cipher:  apptest.NewFakeCipher(),
		}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.home != "" {
			os.RemoveAll(c.home)
		}
		return ctx, nil
	})

	ctx.Given(`^a throwaway postern key for sending$`, c.aThrowawayPosternKey)
	ctx.Given(`^the postern governor key is "([^"]*)"$`, c.thePosternGovernorKeyIs)
	ctx.Given(`^the postern float cap is (\d+) satoshis$`, c.thePosternFloatCapIsSatoshis)
	ctx.Given(`^the postern key's balance is (\d+) satoshis$`, c.thePosternKeysBalanceIsSatoshis)
	ctx.Given(`^the postern key holds a spendable utxo of (\d+) satoshis$`, c.thePosternKeyHoldsASpendableUtxoOfSatoshis)
	ctx.Given(`^the postern backend will report the txid "([^"]*)"$`, c.thePosternBackendWillReportTheTxid)

	ctx.When(`^mw postern send "([^"]*)" "([^"]*)" is run$`, c.mwPosternSendIsRun)

	ctx.Then(`^sending succeeds$`, c.itSucceeds)
	ctx.Then(`^it prints "([^"]*)"$`, c.itPrints)
	ctx.Then(`^it is refused, naming the excess of (\d+)$`, c.itIsRefusedNamingTheExcessOf)
	ctx.Then(`^it is refused, saying "([^"]*)" is not a class postern knows$`, c.itIsRefusedSayingIsNotAClass)
	ctx.Then(`^it is refused, saying postern_governor_key is not set$`, c.itIsRefusedSayingGovernorKeyNotSet)
}

func (c *posternSendContext) aThrowawayPosternKey() error {
	home, err := os.MkdirTemp("", "mw-postern-send-")
	if err != nil {
		return err
	}
	c.home = home
	c.keys = postern.New(filepath.Join(home, "postern.key"))
	if err := c.keys.Generate(); err != nil {
		return err
	}
	_, address, err := c.keys.PublicKey()
	if err != nil {
		return err
	}
	c.address = address
	return nil
}

func (c *posternSendContext) thePosternGovernorKeyIs(key string) error {
	c.governorKey = key
	return nil
}

func (c *posternSendContext) thePosternFloatCapIsSatoshis(sats int64) error {
	c.floatSats = sats
	return nil
}

func (c *posternSendContext) thePosternKeysBalanceIsSatoshis(sats int64) error {
	c.backend.SetBalance(c.address, sats)
	return nil
}

func (c *posternSendContext) thePosternKeyHoldsASpendableUtxoOfSatoshis(sats int64) error {
	c.backend.SetUtxos(c.address, application.PosternUtxo{Txid: posternSendDummyTxid, Vout: 0, Satoshis: sats})
	return nil
}

func (c *posternSendContext) thePosternBackendWillReportTheTxid(txid string) error {
	c.backend.NextTxid = txid
	return nil
}

func (c *posternSendContext) mwPosternSendIsRun(class, text string) error {
	send := application.PosternSend{
		Postern:     c.backend,
		Cipher:      c.cipher,
		Keys:        c.keys,
		GovernorKey: c.governorKey,
		FloatSats:   c.floatSats,
	}
	c.txid, c.err = send.Run(context.Background(), class, text)
	return nil
}

func (c *posternSendContext) itSucceeds() error {
	if c.err != nil {
		return fmt.Errorf("expected it to succeed, got: %w", c.err)
	}
	return nil
}

func (c *posternSendContext) itPrints(want string) error {
	if c.txid != want {
		return fmt.Errorf("expected the txid %q, got %q", want, c.txid)
	}
	return nil
}

func (c *posternSendContext) itIsRefusedNamingTheExcessOf(excess int64) error {
	if c.err == nil {
		return fmt.Errorf("expected send to be refused, but it succeeded")
	}
	if !strings.Contains(c.err.Error(), strconv.FormatInt(excess, 10)) {
		return fmt.Errorf("expected the refusal to name the excess %d, got: %q", excess, c.err.Error())
	}
	return nil
}

func (c *posternSendContext) itIsRefusedSayingIsNotAClass(class string) error {
	if c.err == nil {
		return fmt.Errorf("expected send to be refused, but it succeeded")
	}
	if !strings.Contains(c.err.Error(), class) || !strings.Contains(c.err.Error(), "not a class postern knows") {
		return fmt.Errorf("expected the refusal to name %q as not a class postern knows, got: %q", class, c.err.Error())
	}
	return nil
}

func (c *posternSendContext) itIsRefusedSayingGovernorKeyNotSet() error {
	if c.err == nil {
		return fmt.Errorf("expected send to be refused, but it succeeded")
	}
	if !strings.Contains(c.err.Error(), "postern_governor_key") {
		return fmt.Errorf("expected the refusal to name postern_governor_key, got: %q", c.err.Error())
	}
	return nil
}
