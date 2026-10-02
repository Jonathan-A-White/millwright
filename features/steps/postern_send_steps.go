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
	threads *apptest.FakePosternThreadIndex

	governorKey string
	floatSats   int64
	channel     string
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
			threads: apptest.NewFakePosternThreadIndex(),
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
	ctx.Given(`^the postern key holds a confirmed utxo of (\d+) satoshis$`, c.thePosternKeyHoldsASpendableUtxoOfSatoshis)
	ctx.Given(`^the postern key holds an unconfirmed utxo of (\d+) satoshis$`, c.thePosternKeyHoldsAnUnconfirmedUtxoOfSatoshis)
	ctx.Given(`^the postern key also holds a confirmed utxo of (\d+) satoshis$`, c.thePosternKeyAlsoHoldsAConfirmedUtxoOfSatoshis)
	ctx.Given(`^the postern key holds (\d+) confirmed utxos of (\d+) satoshis$`, c.thePosternKeyHoldsConfirmedUtxosOfSatoshis)
	ctx.Then(`^the last broadcast pays its change to (\d+) outputs$`, c.theLastBroadcastPaysItsChangeToOutputs)
	ctx.Then(`^there were (\d+) broadcasts$`, c.thereWereBroadcasts)
	ctx.Then(`^no outpoint was spent by two broadcasts$`, c.noOutpointWasSpentByTwoBroadcasts)
	ctx.Then(`^the second broadcast spends only the first one's change$`, c.theSecondBroadcastSpendsOnlyTheFirstOnesChange)
	ctx.Then(`^the last broadcast spends the utxo of (\d+) satoshis$`, c.theLastBroadcastSpendsTheUtxoOfSatoshis)
	ctx.Given(`^the postern backend will report the txid "([^"]*)"$`, c.thePosternBackendWillReportTheTxid)
	ctx.Given(`^the clock reads (\d+) for sending$`, c.theClockReadsForSending)
	ctx.Given(`^the bead "([^"]*)" exists$`, c.theBeadExists)
	ctx.Given(`^the bead "([^"]*)" titled "([^"]*)" exists$`, c.theBeadTitledExists)
	ctx.Given(`^the bead "([^"]*)" titled with (\d+) letters exists$`, c.theBeadTitledWithLettersExists)
	ctx.Given(`^the postern channel is "([^"]*)"$`, c.thePosternChannelIs)
	ctx.When(`^mw postern send "([^"]*)" "([^"]*)" in the channel of bead "([^"]*)" attaching "([^"]*)" and "([^"]*)" is run$`, c.mwPosternSendThreadedAttachingTwoIsRun)
	ctx.Then(`^the delivered record's summary is "([^"]*)"$`, c.theDeliveredRecordsSummaryIs)
	ctx.Then(`^the delivered record's summary does not contain "([^"]*)"$`, c.theDeliveredRecordsSummaryDoesNotContain)
	ctx.Then(`^the delivered record has no summary key$`, c.theDeliveredRecordHasNoSummaryKey)
	ctx.Then(`^the delivered record's summary is 80 runes, ending in an ellipsis$`, c.theDeliveredRecordsSummaryIsCut)
	ctx.Then(`^every delivered record's summary is "([^"]*)"$`, c.everyDeliveredRecordsSummaryIs)
	ctx.Then(`^the broadcast record has no summary key$`, c.theBroadcastRecordHasNoSummaryKey)
	ctx.Given(`^a file "([^"]*)" to attach$`, c.aFileToAttach)
	ctx.Given(`^a file "([^"]*)" of (\d+) MiB to attach$`, c.aFileOfMiBToAttach)
	ctx.When(`^mw postern send "([^"]*)" "([^"]*)" attaching "([^"]*)" is run$`, c.mwPosternSendAttachingIsRun)
	ctx.Then(`^bead "([^"]*)" is commented "([^"]*)"$`, c.beadIsCommented)
	ctx.Then(`^the record was delivered directly, and nothing was broadcast$`, c.theRecordWasDeliveredDirectly)
	ctx.Then(`^the delivered message announces the uploaded "([^"]*)" file, captioned "([^"]*)"$`, c.theDeliveredMessageAnnouncesTheUpload)
	ctx.Then(`^it is refused, and nothing was uploaded or sent$`, c.itIsRefusedAndNothingWasSent)

	ctx.When(`^mw postern send "([^"]*)" "([^"]*)" is run$`, c.mwPosternSendIsRun)
	ctx.When(`^mw postern send "([^"]*)" is run with no --class$`, c.mwPosternSendIsRunWithNoClass)
	ctx.When(`^mw postern send "([^"]*)" "([^"]*)" for bead "([^"]*)" recommending "([^"]*)" with options "([^"]*)" is run$`, c.mwPosternSendAsksAQuestionIsRun)
	ctx.When(`^mw postern send "([^"]*)" "([^"]*)" in the channel of bead "([^"]*)" is run$`, c.mwPosternSendThreadedOnBeadIsRun)
	ctx.When(`^mw postern send "([^"]*)" "([^"]*)" in channel "([^"]*)" is run$`, c.mwPosternSendOnTopicIsRun)
	ctx.When(`^mw postern send "([^"]*)" "([^"]*)" in the channel of bead "([^"]*)" and in channel "([^"]*)" is run$`, c.mwPosternSendThreadedOnBeadAndTopicIsRun)
	ctx.When(`^mw postern send "([^"]*)" "([^"]*)" for bead "([^"]*)" recommending "([^"]*)" with options "([^"]*)" in the channel of bead "([^"]*)" is run$`, c.mwPosternSendAsksAQuestionThreadedOnBeadIsRun)

	ctx.Given(`^the backend still lists what that send spent, and its change$`, c.theBackendStillListsWhatThatSendSpentAndItsChange)
	ctx.Then(`^the second broadcast does not spend what the first one spent$`, c.theSecondBroadcastDoesNotSpendWhatTheFirstSpent)
	ctx.Then(`^sending succeeds$`, c.itSucceeds)
	ctx.Then(`^it prints "([^"]*)"$`, c.itPrints)
	ctx.Then(`^it is refused, naming the excess of (\d+)$`, c.itIsRefusedNamingTheExcessOf)
	ctx.Then(`^it is refused, saying "([^"]*)" is not a class postern knows$`, c.itIsRefusedSayingIsNotAClass)
	ctx.Then(`^it is refused, saying postern_governor_key is not set$`, c.itIsRefusedSayingGovernorKeyNotSet)
	ctx.Then(`^it is refused, saying --bead is only accepted with --class decision-needed$`, c.itIsRefusedSayingBeadOnlyWithDecisionNeeded)
	ctx.Then(`^it is refused, saying --bead-channel <id> posts in a bead's channel$`, c.itIsRefusedSayingBeadChannelPostsInABeadsChannel)
	ctx.When(`^mw postern send "([^"]*)" "([^"]*)" in the channel of bead "([^"]*)" answering "([^"]*)" is run$`, c.mwPosternSendInBeadChannelAnsweringIsRun)
	ctx.When(`^mw postern send "([^"]*)" "([^"]*)" in channel "([^"]*)" answering "([^"]*)" is run$`, c.mwPosternSendInChannelAnsweringIsRun)
	ctx.Then(`^the broadcast record's plaintext is in the channel of bead "([^"]*)" with text "([^"]*)" answering "([^"]*)"$`, c.theBroadcastRecordsPlaintextIsInBeadChannelAnswering)
	ctx.Then(`^the broadcast record's plaintext is in channel "([^"]*)" with text "([^"]*)" answering "([^"]*)"$`, c.theBroadcastRecordsPlaintextIsInChannelAnswering)
	ctx.Then(`^the broadcast record is postern's payload, classed "([^"]*)", stamped (\d+)$`, c.theBroadcastRecordIsPosternsPayload)
	ctx.Then(`^the broadcast record is postern's question for bead "([^"]*)", "([^"]*)" recommending "([^"]*)" with options "([^"]*)"$`, c.theBroadcastRecordIsPosternsQuestion)
	ctx.Then(`^bead "([^"]*)" is commented the QUESTION with txid "([^"]*)", "([^"]*)" recommending "([^"]*)" with options "([^"]*)"$`, c.beadIsCommentedTheQuestion)
	ctx.Then(`^bead "([^"]*)"'s question note holds the txid "([^"]*)"$`, c.beadsQuestionNoteHoldsTheTxid)
	ctx.Then(`^bead "([^"]*)"'s question note expects "([^"]*)" of the option "([^"]*)"$`, c.beadsQuestionNoteExpects)
	ctx.Then(`^bead "([^"]*)"'s question note expects nothing of the option "([^"]*)"$`, c.beadsQuestionNoteExpectsNothing)
	ctx.Then(`^it is refused, saying "([^"]*)"$`, c.itIsRefusedSaying)
	ctx.Then(`^the broadcast record's plaintext is in the channel of bead "([^"]*)" with text "([^"]*)"$`, c.theBroadcastRecordsPlaintextIsThreadedOnBead)
	ctx.Then(`^the broadcast record's plaintext is in channel "([^"]*)" with text "([^"]*)"$`, c.theBroadcastRecordsPlaintextIsOnTopic)
	ctx.Given(`^mw has seen the post "([^"]*)" in the channel of bead "([^"]*)"$`, c.mwHasSeenThePostInBeadChannel)
	ctx.Given(`^mw has seen the post "([^"]*)" in channel "([^"]*)"$`, c.mwHasSeenThePostInChannel)
	ctx.When(`^mw postern send "([^"]*)" "([^"]*)" answering "([^"]*)" is run$`, c.mwPosternSendAnsweringIsRun)
	ctx.Then(`^it is refused, saying --re needs the root's channel and naming --bead-channel and --channel$`, c.itIsRefusedSayingReNeedsTheRootsChannel)
	ctx.Then(`^the broadcast record's plaintext answers "([^"]*)" with text "([^"]*)" in no channel$`, c.theBroadcastRecordsPlaintextAnswersInNoChannel)
	ctx.Then(`^it is refused, saying --channel and --bead-channel cannot both be set$`, c.itIsRefusedSayingThreadAndTopicCannotBothBeSet)
	ctx.Then(`^it is refused, saying --channel and --bead-channel are refused with a decision-needed question$`, c.itIsRefusedSayingThreadRefusedWithQuestion)
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
	c.backend.SetUtxos(c.address, application.PosternUtxo{Txid: posternSendDummyTxid, Vout: 0, Satoshis: sats, Height: 100})
	return nil
}

func (c *posternSendContext) thePosternKeyHoldsAnUnconfirmedUtxoOfSatoshis(sats int64) error {
	c.backend.SetUtxos(c.address, application.PosternUtxo{Txid: posternSendDummyTxid, Vout: 0, Satoshis: sats})
	return nil
}

func (c *posternSendContext) thePosternKeyAlsoHoldsAConfirmedUtxoOfSatoshis(sats int64) error {
	held, err := c.backend.Utxos(context.Background(), c.address)
	if err != nil {
		return err
	}
	c.backend.SetUtxos(c.address, append(held, application.PosternUtxo{Txid: posternSendDummyTxid, Vout: len(held), Satoshis: sats, Height: 100})...)
	return nil
}

func (c *posternSendContext) thePosternKeyHoldsConfirmedUtxosOfSatoshis(n int, sats int64) error {
	var held []application.PosternUtxo
	for i := 0; i < n; i++ {
		held = append(held, application.PosternUtxo{Txid: posternSendDummyTxid, Vout: i, Satoshis: sats, Height: 100})
	}
	c.backend.SetUtxos(c.address, held...)
	return nil
}

// broadcastTransactions parses every transaction the backend was asked to
// broadcast, oldest first.
func (c *posternSendContext) broadcastTransactions() ([]*transaction.Transaction, error) {
	var txs []*transaction.Transaction
	for _, raw := range c.backend.Broadcasts() {
		tx, err := transaction.NewTransactionFromHex(raw)
		if err != nil {
			return nil, fmt.Errorf("parsing a broadcast transaction: %w", err)
		}
		txs = append(txs, tx)
	}
	return txs, nil
}

func (c *posternSendContext) theLastBroadcastPaysItsChangeToOutputs(want int) error {
	txs, err := c.broadcastTransactions()
	if err != nil {
		return err
	}
	if len(txs) == 0 {
		return fmt.Errorf("expected a broadcast, got none")
	}
	// Outputs 0 and 1 are the record and the anchor; the rest are change.
	if got := len(txs[len(txs)-1].Outputs) - 2; got != want {
		return fmt.Errorf("expected %d change outputs, got %d", want, got)
	}
	return nil
}

func (c *posternSendContext) thereWereBroadcasts(want int) error {
	if got := len(c.backend.Broadcasts()); got != want {
		return fmt.Errorf("expected %d broadcasts, got %d", want, got)
	}
	return nil
}

func (c *posternSendContext) noOutpointWasSpentByTwoBroadcasts() error {
	txs, err := c.broadcastTransactions()
	if err != nil {
		return err
	}
	spentBy := map[string]int{}
	for i, tx := range txs {
		for _, in := range tx.Inputs {
			point := fmt.Sprintf("%s:%d", in.SourceTXID, in.SourceTxOutIndex)
			if first, ok := spentBy[point]; ok {
				return fmt.Errorf("broadcasts %d and %d both spend %s", first+1, i+1, point)
			}
			spentBy[point] = i
		}
	}
	return nil
}

func (c *posternSendContext) theSecondBroadcastSpendsOnlyTheFirstOnesChange() error {
	txs, err := c.broadcastTransactions()
	if err != nil {
		return err
	}
	if len(txs) < 2 {
		return fmt.Errorf("expected two broadcasts, got %d", len(txs))
	}
	if len(txs[1].Inputs) != 1 || txs[1].Inputs[0].SourceTXID.String() != txs[0].TxID().String() {
		return fmt.Errorf("expected the second broadcast to spend one output of the first (%s), got %d inputs", txs[0].TxID(), len(txs[1].Inputs))
	}
	if idx := txs[1].Inputs[0].SourceTxOutIndex; idx < 2 {
		return fmt.Errorf("the second broadcast spends output %d of the first, not a change output", idx)
	}
	return nil
}

func (c *posternSendContext) theLastBroadcastSpendsTheUtxoOfSatoshis(sats int64) error {
	held, err := c.backend.Utxos(context.Background(), c.address)
	if err != nil {
		return err
	}
	txs, err := c.broadcastTransactions()
	if err != nil {
		return err
	}
	if len(txs) == 0 {
		return fmt.Errorf("expected a broadcast, got none")
	}
	last := txs[len(txs)-1]
	for _, u := range held {
		if u.Satoshis == sats {
			if len(last.Inputs) != 1 || last.Inputs[0].SourceTXID.String() != u.Txid || int(last.Inputs[0].SourceTxOutIndex) != u.Vout {
				return fmt.Errorf("expected the last broadcast to spend only %s:%d (%d satoshis), got %d inputs", u.Txid, u.Vout, sats, len(last.Inputs))
			}
			return nil
		}
	}
	return fmt.Errorf("no utxo of %d satoshis is held", sats)
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
		Channel:     c.channel,
		Now:         c.now,
		Threads:     c.threads,
	}
}

func (c *posternSendContext) thePosternChannelIs(channel string) error {
	c.channel = channel
	return nil
}

func (c *posternSendContext) aFileToAttach(name string) error {
	return os.WriteFile(filepath.Join(c.home, name), []byte("%PDF-1.7 a report"), 0o600)
}

func (c *posternSendContext) aFileOfMiBToAttach(name string, mib int) error {
	return os.WriteFile(filepath.Join(c.home, name), make([]byte, mib<<20), 0o600)
}

func (c *posternSendContext) mwPosternSendAttachingIsRun(class, text, name string) error {
	c.txid, c.err = c.send().Run(context.Background(), application.PosternSendRequest{
		Class: class, Text: text, Attachments: []string{filepath.Join(c.home, name)},
	})
	return nil
}

func (c *posternSendContext) beadIsCommented(bead, want string) error {
	comments := c.tracker.Comments(bead)
	if len(comments) == 0 || comments[len(comments)-1] != want {
		return fmt.Errorf("expected %s's last comment %q, got %v", bead, want, comments)
	}
	return nil
}

func (c *posternSendContext) theRecordWasDeliveredDirectly() error {
	if n := len(c.backend.Delivered()); n != 1 {
		return fmt.Errorf("expected one direct delivery, got %d", n)
	}
	if n := len(c.backend.Broadcasts()); n != 0 {
		return fmt.Errorf("expected nothing broadcast, got %d", n)
	}
	return nil
}

func (c *posternSendContext) theDeliveredMessageAnnouncesTheUpload(mime, caption string) error {
	delivered, uploaded := c.backend.Delivered(), c.backend.Uploaded()
	if len(delivered) != 1 || len(uploaded) != 1 {
		return fmt.Errorf("expected one upload and one delivery, got %d and %d", len(uploaded), len(delivered))
	}
	var record application.PosternPayload
	if err := json.Unmarshal(delivered[0], &record); err != nil {
		return err
	}
	privKey, err := c.keys.PrivateKeyWIF()
	if err != nil {
		return err
	}
	text, _, err := c.cipher.Decrypt(privKey, record.Ct)
	if err != nil {
		return err
	}
	var body application.PosternThreadedMessage
	if err := json.Unmarshal([]byte(text), &body); err != nil {
		return fmt.Errorf("the plaintext is not the threaded body: %w", err)
	}
	if body.Text != caption || body.Attachment == nil || body.Attachment.Mime != mime || body.Attachment.Size != int64(len(uploaded[0])) {
		return fmt.Errorf("expected a %s attachment captioned %q, got %s", mime, caption, text)
	}
	return nil
}

func (c *posternSendContext) itIsRefusedAndNothingWasSent() error {
	if c.err == nil {
		return fmt.Errorf("expected send to be refused, but it succeeded")
	}
	if len(c.backend.Uploaded()) != 0 || len(c.backend.Delivered()) != 0 || len(c.backend.Broadcasts()) != 0 {
		return fmt.Errorf("expected nothing uploaded or sent")
	}
	return nil
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

func (c *posternSendContext) mwPosternSendThreadedOnBeadIsRun(class, text, thread string) error {
	c.txid, c.err = c.send().Run(context.Background(), application.PosternSendRequest{Class: class, Text: text, Thread: thread})
	return nil
}

func (c *posternSendContext) mwPosternSendOnTopicIsRun(class, text, topic string) error {
	c.txid, c.err = c.send().Run(context.Background(), application.PosternSendRequest{Class: class, Text: text, Topic: topic})
	return nil
}

func (c *posternSendContext) mwPosternSendThreadedOnBeadAndTopicIsRun(class, text, thread, topic string) error {
	c.txid, c.err = c.send().Run(context.Background(), application.PosternSendRequest{Class: class, Text: text, Thread: thread, Topic: topic})
	return nil
}

func (c *posternSendContext) mwPosternSendAsksAQuestionThreadedOnBeadIsRun(class, text, bead, recommend, optionsCSV, thread string) error {
	c.txid, c.err = c.send().Run(context.Background(), application.PosternSendRequest{
		Class: class, Text: text, Bead: bead, Recommend: recommend, Options: splitOptions(optionsCSV), Thread: thread,
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
	text, _, err := c.cipher.Decrypt(privKey, record.Ct)
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
	var note struct {
		Txid string `json:"txid"`
	}
	if err := json.Unmarshal([]byte(saved), &note); err != nil {
		return fmt.Errorf("expected the question note to decode, got %q: %w", saved, err)
	}
	if note.Txid != txid {
		return fmt.Errorf("expected the question note to hold txid %q, got %q", txid, note.Txid)
	}
	return nil
}

// decryptedBroadcastPlaintext reads the one transaction broadcast back off
// the fake backend and decrypts its record's plaintext.
func (c *posternSendContext) decryptedBroadcastPlaintext() (string, error) {
	var payload []byte
	if delivered := c.backend.Delivered(); len(delivered) > 0 {
		payload = delivered[len(delivered)-1]
	} else {
		sent := c.backend.Broadcasts()
		if len(sent) != 1 {
			return "", fmt.Errorf("expected one broadcast, got %d", len(sent))
		}
		tx, err := transaction.NewTransactionFromHex(sent[0])
		if err != nil {
			return "", fmt.Errorf("parsing the broadcast transaction: %w", err)
		}
		var ok bool
		payload, ok = postern.DecodeRecordScript(tx.Outputs[0].LockingScript.String())
		if !ok {
			return "", fmt.Errorf("expected output 0 to be a version-1 record, got %s", tx.Outputs[0].LockingScript.String())
		}
	}
	var record application.PosternPayload
	if err := json.Unmarshal(payload, &record); err != nil {
		return "", fmt.Errorf("the record's payload is not JSON: %w", err)
	}
	privKey, err := c.keys.PrivateKeyWIF()
	if err != nil {
		return "", err
	}
	text, _, err := c.cipher.Decrypt(privKey, record.Ct)
	if err != nil {
		return "", fmt.Errorf("decrypting the record's plaintext: %w", err)
	}
	return text, nil
}

func (c *posternSendContext) theBroadcastRecordsPlaintextIsThreadedOnBead(bead, text string) error {
	got, err := c.decryptedBroadcastPlaintext()
	if err != nil {
		return err
	}
	want, err := json.Marshal(application.PosternThreadedMessage{Thread: application.PosternThread{Bead: bead}, Text: text})
	if err != nil {
		return err
	}
	if got != string(want) {
		return fmt.Errorf("expected the plaintext\n%s\ngot\n%s", want, got)
	}
	return nil
}

func (c *posternSendContext) theBroadcastRecordsPlaintextIsOnTopic(topic, text string) error {
	got, err := c.decryptedBroadcastPlaintext()
	if err != nil {
		return err
	}
	want, err := json.Marshal(application.PosternThreadedMessage{Thread: application.PosternThread{Topic: topic}, Text: text})
	if err != nil {
		return err
	}
	if got != string(want) {
		return fmt.Errorf("expected the plaintext\n%s\ngot\n%s", want, got)
	}
	return nil
}

func (c *posternSendContext) mwPosternSendInBeadChannelAnsweringIsRun(class, text, bead, re string) error {
	c.txid, c.err = c.send().Run(context.Background(), application.PosternSendRequest{Class: class, Text: text, Thread: bead, Re: re})
	return nil
}

func (c *posternSendContext) mwPosternSendInChannelAnsweringIsRun(class, text, channel, re string) error {
	c.txid, c.err = c.send().Run(context.Background(), application.PosternSendRequest{Class: class, Text: text, Topic: channel, Re: re})
	return nil
}

func (c *posternSendContext) theBroadcastRecordsPlaintextIsInBeadChannelAnswering(bead, text, re string) error {
	return c.plaintextIs(application.PosternThreadedMessage{Thread: application.PosternThread{Bead: bead}, Text: text, Re: re})
}

func (c *posternSendContext) theBroadcastRecordsPlaintextIsInChannelAnswering(channel, text, re string) error {
	return c.plaintextIs(application.PosternThreadedMessage{Thread: application.PosternThread{Topic: channel}, Text: text, Re: re})
}

func (c *posternSendContext) plaintextIs(message application.PosternThreadedMessage) error {
	got, err := c.decryptedBroadcastPlaintext()
	if err != nil {
		return err
	}
	want, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if got != string(want) {
		return fmt.Errorf("expected the plaintext\n%s\ngot\n%s", want, got)
	}
	return nil
}

func (c *posternSendContext) itIsRefusedSayingBeadChannelPostsInABeadsChannel() error {
	if c.err == nil {
		return fmt.Errorf("expected send to be refused, but it succeeded")
	}
	if !strings.Contains(c.err.Error(), "--bead-channel <id> posts in a bead's channel") {
		return fmt.Errorf("expected the refusal to name --bead-channel, got: %q", c.err.Error())
	}
	return nil
}

func (c *posternSendContext) itIsRefusedSayingThreadAndTopicCannotBothBeSet() error {
	if c.err == nil {
		return fmt.Errorf("expected send to be refused, but it succeeded")
	}
	if !strings.Contains(c.err.Error(), "--channel") || !strings.Contains(c.err.Error(), "--bead-channel") {
		return fmt.Errorf("expected the refusal to name --channel and --bead-channel, got: %q", c.err.Error())
	}
	return nil
}

func (c *posternSendContext) itIsRefusedSayingThreadRefusedWithQuestion() error {
	if c.err == nil {
		return fmt.Errorf("expected send to be refused, but it succeeded")
	}
	if !strings.Contains(c.err.Error(), "--bead-channel") || !strings.Contains(c.err.Error(), "decision-needed question") {
		return fmt.Errorf("expected the refusal to say --bead-channel is refused with a decision-needed question, got: %q", c.err.Error())
	}
	return nil
}

// theBackendStillListsWhatThatSendSpentAndItsChange makes the fake backend
// lag the way WhatsOnChain does within a block: the utxo the last send spent
// is still listed, beside the unconfirmed change it made.
func (c *posternSendContext) theBackendStillListsWhatThatSendSpentAndItsChange() error {
	sent := c.backend.Broadcasts()
	if len(sent) == 0 {
		return fmt.Errorf("expected a send before the backend lags, got none")
	}
	tx, err := transaction.NewTransactionFromHex(sent[len(sent)-1])
	if err != nil {
		return fmt.Errorf("parsing the broadcast transaction: %w", err)
	}
	c.backend.SetUtxos(c.address,
		application.PosternUtxo{Txid: posternSendDummyTxid, Vout: 0, Satoshis: 5000},
		application.PosternUtxo{Txid: tx.TxID().String(), Vout: 2, Satoshis: int64(tx.Outputs[2].Satoshis)},
	)
	return nil
}

func (c *posternSendContext) theSecondBroadcastDoesNotSpendWhatTheFirstSpent() error {
	sent := c.backend.Broadcasts()
	if len(sent) != 2 {
		return fmt.Errorf("expected two broadcasts, got %d", len(sent))
	}
	first, err := transaction.NewTransactionFromHex(sent[0])
	if err != nil {
		return err
	}
	second, err := transaction.NewTransactionFromHex(sent[1])
	if err != nil {
		return err
	}
	for _, spentByFirst := range first.Inputs {
		for _, in := range second.Inputs {
			if in.SourceTXID.String() == spentByFirst.SourceTXID.String() && in.SourceTxOutIndex == spentByFirst.SourceTxOutIndex {
				return fmt.Errorf("the second broadcast spends %s:%d again", in.SourceTXID, in.SourceTxOutIndex)
			}
		}
	}
	return nil
}

func (c *posternSendContext) theBeadTitledExists(id, title string) error {
	c.tracker.AddStory("epic", domain.Story{ID: id, Title: title})
	return nil
}

func (c *posternSendContext) theBeadTitledWithLettersExists(id string, n int) error {
	return c.theBeadTitledExists(id, strings.Repeat("é", n))
}

func (c *posternSendContext) mwPosternSendThreadedAttachingTwoIsRun(class, text, thread, first, second string) error {
	c.txid, c.err = c.send().Run(context.Background(), application.PosternSendRequest{
		Class: class, Text: text, Thread: thread,
		Attachments: []string{filepath.Join(c.home, first), filepath.Join(c.home, second)},
	})
	return nil
}

// summaryOf reads the summary key of one record's clear JSON, reporting
// whether the key is there at all.
func summaryOf(payload []byte) (string, bool, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return "", false, fmt.Errorf("the record is not JSON: %w", err)
	}
	raw, ok := fields["summary"]
	if !ok {
		return "", false, nil
	}
	var summary string
	if err := json.Unmarshal(raw, &summary); err != nil {
		return "", true, fmt.Errorf("the summary is not a string: %w", err)
	}
	return summary, true, nil
}

// deliveredSummaries is the summary of every record delivered directly.
func (c *posternSendContext) deliveredSummaries() ([]string, error) {
	delivered := c.backend.Delivered()
	if len(delivered) == 0 {
		return nil, fmt.Errorf("nothing was delivered (send error: %v)", c.err)
	}
	out := make([]string, 0, len(delivered))
	for _, payload := range delivered {
		summary, ok, err := summaryOf(payload)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("the record has no summary key: %s", payload)
		}
		out = append(out, summary)
	}
	return out, nil
}

func (c *posternSendContext) lastDeliveredSummary() (string, error) {
	summaries, err := c.deliveredSummaries()
	if err != nil {
		return "", err
	}
	return summaries[len(summaries)-1], nil
}

func (c *posternSendContext) theDeliveredRecordsSummaryIs(want string) error {
	got, err := c.lastDeliveredSummary()
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("expected the summary %q, got %q", want, got)
	}
	return nil
}

func (c *posternSendContext) theDeliveredRecordsSummaryDoesNotContain(word string) error {
	got, err := c.lastDeliveredSummary()
	if err != nil {
		return err
	}
	if strings.Contains(got, word) {
		return fmt.Errorf("the summary %q holds %q from the message", got, word)
	}
	return nil
}

func (c *posternSendContext) theDeliveredRecordHasNoSummaryKey() error {
	delivered := c.backend.Delivered()
	if len(delivered) != 1 {
		return fmt.Errorf("expected one delivery, got %d (send error: %v)", len(delivered), c.err)
	}
	if summary, ok, err := summaryOf(delivered[0]); err != nil {
		return err
	} else if ok {
		return fmt.Errorf("expected no summary key, got %q in %s", summary, delivered[0])
	}
	return nil
}

func (c *posternSendContext) theDeliveredRecordsSummaryIsCut() error {
	got, err := c.lastDeliveredSummary()
	if err != nil {
		return err
	}
	runes := []rune(got)
	if len(runes) != 80 || runes[79] != '…' {
		return fmt.Errorf("expected 80 runes ending in an ellipsis, got %d: %q", len(runes), got)
	}
	return nil
}

func (c *posternSendContext) everyDeliveredRecordsSummaryIs(want string) error {
	summaries, err := c.deliveredSummaries()
	if err != nil {
		return err
	}
	if len(summaries) < 2 {
		return fmt.Errorf("expected several records, got %d", len(summaries))
	}
	for _, got := range summaries {
		if got != want {
			return fmt.Errorf("expected every summary %q, got %q", want, got)
		}
	}
	return nil
}

func (c *posternSendContext) theBroadcastRecordHasNoSummaryKey() error {
	sent := c.backend.Broadcasts()
	if len(sent) != 1 {
		return fmt.Errorf("expected one broadcast, got %d (send error: %v)", len(sent), c.err)
	}
	tx, err := transaction.NewTransactionFromHex(sent[0])
	if err != nil {
		return fmt.Errorf("parsing the broadcast transaction: %w", err)
	}
	payload, ok := postern.DecodeRecordScript(tx.Outputs[0].LockingScript.String())
	if !ok {
		return fmt.Errorf("expected output 0 to be a version-1 record")
	}
	if summary, ok, err := summaryOf(payload); err != nil {
		return err
	} else if ok {
		return fmt.Errorf("the chain record carries a summary %q: %s", summary, payload)
	}
	return nil
}

func (c *posternSendContext) mwHasSeenThePostInBeadChannel(txid, bead string) error {
	return c.threads.Remember(txid, application.PosternThread{Bead: bead})
}

func (c *posternSendContext) mwHasSeenThePostInChannel(txid, channel string) error {
	return c.threads.Remember(txid, application.PosternThread{Topic: channel})
}

func (c *posternSendContext) mwPosternSendAnsweringIsRun(class, text, re string) error {
	c.txid, c.err = c.send().Run(context.Background(), application.PosternSendRequest{Class: class, Text: text, Re: re})
	return nil
}

func (c *posternSendContext) itIsRefusedSayingReNeedsTheRootsChannel() error {
	want := "--re <root> needs the root's channel: give --bead-channel <id> or --channel <name>"
	if c.err == nil {
		return fmt.Errorf("expected send to be refused, but it succeeded")
	}
	if !strings.Contains(c.err.Error(), want) {
		return fmt.Errorf("expected the refusal to say %q, got: %q", want, c.err.Error())
	}
	return nil
}

func (c *posternSendContext) theBroadcastRecordsPlaintextAnswersInNoChannel(re, text string) error {
	return c.plaintextIs(application.PosternThreadedMessage{Text: text, Re: re})
}

// questionNoteExpect reads the expectations a bead's question note holds of
// option, as "<bead>:<state>" joined by commas; "" when it holds none.
func (c *posternSendContext) questionNoteExpect(bead, option string) (string, error) {
	saved, err := c.tracker.Note(context.Background(), application.PosternQuestionKey(bead))
	if err != nil {
		return "", err
	}
	var note struct {
		Expect map[string][]domain.Expectation `json:"expect"`
	}
	if err := json.Unmarshal([]byte(saved), &note); err != nil {
		return "", fmt.Errorf("expected the question note to decode, got %q: %w", saved, err)
	}
	var specs []string
	for _, e := range note.Expect[option] {
		specs = append(specs, e.Bead+":"+e.State)
	}
	return strings.Join(specs, ","), nil
}

func (c *posternSendContext) beadsQuestionNoteExpects(bead, want, option string) error {
	got, err := c.questionNoteExpect(bead, option)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("expected the note to expect %q of %q, got %q", want, option, got)
	}
	return nil
}

func (c *posternSendContext) beadsQuestionNoteExpectsNothing(bead, option string) error {
	got, err := c.questionNoteExpect(bead, option)
	if err != nil {
		return err
	}
	if got != "" {
		return fmt.Errorf("expected the note to expect nothing of %q, got %q", option, got)
	}
	return nil
}

func (c *posternSendContext) itIsRefusedSaying(words string) error {
	if c.err == nil {
		return fmt.Errorf("expected send to be refused, but it succeeded")
	}
	if !strings.Contains(c.err.Error(), words) {
		return fmt.Errorf("expected the refusal to say %q, got: %q", words, c.err.Error())
	}
	return nil
}
