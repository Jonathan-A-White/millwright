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
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"

	"github.com/bsv-blockchain/go-sdk/transaction"
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
	tracker *apptest.FakeTracker

	governorKey string
	floatSats   int64
	now         func() time.Time

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
			tracker: apptest.NewFakeTracker(),
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
	ctx.Given(`^the clock reads (\d+) for sending$`, c.theClockReadsForSending)
	ctx.Given(`^the bead "([^"]*)" exists$`, c.theBeadExists)

	ctx.When(`^mw postern send "([^"]*)" "([^"]*)" is run$`, c.mwPosternSendIsRun)
	ctx.When(`^mw postern send "([^"]*)" is run with no --class$`, c.mwPosternSendIsRunWithNoClass)
	ctx.When(`^mw postern send "([^"]*)" "([^"]*)" for bead "([^"]*)" recommending "([^"]*)" with options "([^"]*)" is run$`, c.mwPosternSendAsksAQuestionIsRun)

	ctx.Then(`^sending succeeds$`, c.itSucceeds)
	ctx.Then(`^it prints "([^"]*)"$`, c.itPrints)
	ctx.Then(`^it is refused, naming the excess of (\d+)$`, c.itIsRefusedNamingTheExcessOf)
	ctx.Then(`^it is refused, saying "([^"]*)" is not a class postern knows$`, c.itIsRefusedSayingIsNotAClass)
	ctx.Then(`^it is refused, saying postern_governor_key is not set$`, c.itIsRefusedSayingGovernorKeyNotSet)
	ctx.Then(`^it is refused, saying --bead is only accepted with --class decision-needed$`, c.itIsRefusedSayingBeadOnlyWithDecisionNeeded)
	ctx.Then(`^the broadcast record is postern's payload, classed "([^"]*)", stamped (\d+)$`, c.theBroadcastRecordIsPosternsPayload)
	ctx.Then(`^the broadcast record is postern's question for bead "([^"]*)", "([^"]*)" recommending "([^"]*)" with options "([^"]*)"$`, c.theBroadcastRecordIsPosternsQuestion)
	ctx.Then(`^bead "([^"]*)" is commented the QUESTION with txid "([^"]*)", "([^"]*)" recommending "([^"]*)" with options "([^"]*)"$`, c.beadIsCommentedTheQuestion)
	ctx.Then(`^bead "([^"]*)"'s question note holds the txid "([^"]*)"$`, c.beadsQuestionNoteHoldsTheTxid)
}

// splitOptions reads a comma-space-joined options list back into a slice, the
// same shape strings.Join(options, ", ") produces.
func splitOptions(csv string) []string {
	return strings.Split(csv, ", ")
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

func (c *posternSendContext) theClockReadsForSending(unix int64) error {
	c.now = func() time.Time { return time.Unix(unix, 0) }
	return nil
}

func (c *posternSendContext) theBeadExists(id string) error {
	c.tracker.AddStory("epic", domain.Story{ID: id})
	return nil
}

func (c *posternSendContext) send() application.PosternSend {
	return application.PosternSend{
		Postern:     c.backend,
		Cipher:      c.cipher,
		Keys:        c.keys,
		Tracker:     c.tracker,
		Notes:       c.tracker,
		GovernorKey: c.governorKey,
		FloatSats:   c.floatSats,
		Now:         c.now,
	}
}

func (c *posternSendContext) mwPosternSendIsRun(class, text string) error {
	c.txid, c.err = c.send().Run(context.Background(), application.PosternSendRequest{Class: class, Text: text})
	return nil
}

func (c *posternSendContext) mwPosternSendIsRunWithNoClass(text string) error {
	c.txid, c.err = c.send().Run(context.Background(), application.PosternSendRequest{Text: text})
	return nil
}

func (c *posternSendContext) mwPosternSendAsksAQuestionIsRun(class, text, bead, recommend, optionsCSV string) error {
	c.txid, c.err = c.send().Run(context.Background(), application.PosternSendRequest{
		Class: class, Text: text, Bead: bead, Recommend: recommend, Options: splitOptions(optionsCSV),
	})
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

// theBroadcastRecordIsPosternsPayload reads the one transaction broadcast
// back off the fake backend and checks its record output byte for byte: the
// payload is postern's docs/protocol.md section 1, fields in its order, from
// this key to the governor key.
func (c *posternSendContext) theBroadcastRecordIsPosternsPayload(class string, ts int64) error {
	sent := c.backend.Broadcasts()
	if len(sent) != 1 {
		return fmt.Errorf("expected one broadcast, got %d", len(sent))
	}
	tx, err := transaction.NewTransactionFromHex(sent[0])
	if err != nil {
		return fmt.Errorf("parsing the broadcast transaction: %w", err)
	}
	payload, ok := postern.DecodeRecordScript(tx.Outputs[0].LockingScript.String())
	if !ok {
		return fmt.Errorf("expected output 0 to be a version-1 record, got %s", tx.Outputs[0].LockingScript.String())
	}
	from, _, err := c.keys.PublicKey()
	if err != nil {
		return err
	}
	ct, err := c.cipher.Encrypt(c.governorKey, "Ship it?")
	if err != nil {
		return err
	}
	want := fmt.Sprintf(`{"v":1,"kind":"msg","class":%q,"to":%q,"from":%q,"ts":%d,"ct":%q}`, class, c.governorKey, from, ts, ct)
	if string(payload) != want {
		return fmt.Errorf("expected the payload\n%s\ngot\n%s", want, payload)
	}
	return nil
}

func (c *posternSendContext) itIsRefusedSayingBeadOnlyWithDecisionNeeded() error {
	if c.err == nil {
		return fmt.Errorf("expected send to be refused, but it succeeded")
	}
	if !strings.Contains(c.err.Error(), "--bead") || !strings.Contains(c.err.Error(), "decision-needed") {
		return fmt.Errorf("expected the refusal to say --bead is only accepted with --class decision-needed, got: %q", c.err.Error())
	}
	return nil
}

// theBroadcastRecordIsPosternsQuestion checks the one transaction broadcast
// carries postern's docs/protocol.md section 6 question as its plaintext.
func (c *posternSendContext) theBroadcastRecordIsPosternsQuestion(bead, q, rec, optionsCSV string) error {
	sent := c.backend.Broadcasts()
	if len(sent) != 1 {
		return fmt.Errorf("expected one broadcast, got %d", len(sent))
	}
	tx, err := transaction.NewTransactionFromHex(sent[0])
	if err != nil {
		return fmt.Errorf("parsing the broadcast transaction: %w", err)
	}
	payload, ok := postern.DecodeRecordScript(tx.Outputs[0].LockingScript.String())
	if !ok {
		return fmt.Errorf("expected output 0 to be a version-1 record, got %s", tx.Outputs[0].LockingScript.String())
	}
	var record application.PosternPayload
	if err := json.Unmarshal(payload, &record); err != nil {
		return fmt.Errorf("the record's payload is not JSON: %w", err)
	}
	privKey, err := c.keys.PrivateKeyWIF()
	if err != nil {
		return err
	}
	text, err := c.cipher.Decrypt(privKey, record.Ct)
	if err != nil {
		return fmt.Errorf("decrypting the record's plaintext: %w", err)
	}
	wantQuestion, err := json.Marshal(application.PosternQuestion{Bead: bead, Q: q, Rec: rec, Options: splitOptions(optionsCSV)})
	if err != nil {
		return err
	}
	if text != string(wantQuestion) {
		return fmt.Errorf("expected the plaintext\n%s\ngot\n%s", wantQuestion, text)
	}
	return nil
}

func (c *posternSendContext) beadIsCommentedTheQuestion(bead, txid, q, rec, optionsCSV string) error {
	comments, err := c.tracker.StoryComments(context.Background(), bead)
	if err != nil {
		return err
	}
	if len(comments) == 0 {
		return fmt.Errorf("expected a comment on %s, found none", bead)
	}
	want := fmt.Sprintf("QUESTION %s asked by postern, txid %s: %s (recommended %s; options %s)",
		c.now().UTC().Format(time.RFC3339), txid, q, rec, strings.Join(splitOptions(optionsCSV), ", "))
	got := comments[len(comments)-1].Text
	if got != want {
		return fmt.Errorf("expected the comment\n%s\ngot\n%s", want, got)
	}
	return nil
}

func (c *posternSendContext) beadsQuestionNoteHoldsTheTxid(bead, txid string) error {
	saved, err := c.tracker.Note(context.Background(), application.PosternQuestionKey(bead))
	if err != nil {
		return err
	}
	if saved != txid {
		return fmt.Errorf("expected the question note to hold txid %q, got %q", txid, saved)
	}
	return nil
}
