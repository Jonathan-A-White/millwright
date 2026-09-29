package postern_test

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/bsv-blockchain/go-sdk/transaction/template/p2pkh"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

const (
	fundingTxid = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	tokenTxid   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func p2pkhScriptHex(t *testing.T, address string) string {
	t.Helper()
	addr, err := script.NewAddressFromString(address)
	if err != nil {
		t.Fatalf("parsing %s: %v", address, err)
	}
	lock, err := p2pkh.Lock(addr)
	if err != nil {
		t.Fatalf("locking to %s: %v", address, err)
	}
	return hex.EncodeToString(*lock)
}

// The transaction is postern's docs/protocol.md section 4: one input per
// utxo but a 1-satoshi one, the record at output 0, 1 satoshi to the anchor
// at output 1, change back to the key's own address at output 2.
func TestSignBuildsTheProtocolsTransaction(t *testing.T) {
	f := loadProtocolFixture(t)
	senderWIF := wifFromHex(t, f.Inputs.SenderPrivateKeyHex)
	senderAddress := addressFromHex(t, f.Inputs.SenderPublicKeyHex)
	keys := postern.New(keyFileHolding(t, senderWIF))

	rawtx, err := keys.Sign([]application.PosternUtxo{
		{Txid: fundingTxid, Vout: 0, Satoshis: 10000},
		{Txid: tokenTxid, Vout: 1, Satoshis: 1},
	}, fixturePayload(t, f))
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	tx, err := transaction.NewTransactionFromHex(rawtx)
	if err != nil {
		t.Fatalf("parsing the signed transaction: %v", err)
	}

	if len(tx.Inputs) != 1 || tx.Inputs[0].SourceTXID.String() != fundingTxid || tx.Inputs[0].SourceTxOutIndex != 0 {
		t.Fatalf("expected one input spending %s:0 and not the 1-satoshi token, got %d inputs", fundingTxid, len(tx.Inputs))
	}
	// One coin left to spend, so the change is split into four outputs.
	if len(tx.Outputs) != 6 {
		t.Fatalf("expected six outputs (record, anchor, four change), got %d", len(tx.Outputs))
	}
	record, anchor, change := tx.Outputs[0], tx.Outputs[1], tx.Outputs[2]
	if got := hex.EncodeToString(*record.LockingScript); got != f.RecordScriptHex || record.Satoshis != 0 {
		t.Errorf("expected output 0 to be the fixture's record script at 0 satoshis, got %d satoshis and\n%s", record.Satoshis, got)
	}
	if got := hex.EncodeToString(*anchor.LockingScript); got != p2pkhScriptHex(t, postern.AnchorAddress) || anchor.Satoshis != 1 {
		t.Errorf("expected output 1 to pay 1 satoshi to the anchor %s, got %d satoshis to %s", postern.AnchorAddress, anchor.Satoshis, got)
	}
	if got := hex.EncodeToString(*change.LockingScript); got != p2pkhScriptHex(t, senderAddress) {
		t.Errorf("expected output 2 to pay change to %s, got %s", senderAddress, got)
	}
	var changeTotal int64
	for _, o := range tx.Outputs[2:] {
		if hex.EncodeToString(*o.LockingScript) != p2pkhScriptHex(t, senderAddress) {
			t.Errorf("expected every output from 2 on to pay change to %s", senderAddress)
		}
		changeTotal += int64(o.Satoshis)
	}
	fee := int64(10000) - 1 - changeTotal
	if fee < 1 || fee > 10 {
		t.Errorf("expected a fee of a few satoshis at 1 sat/kB, got %d", fee)
	}
	if len(*tx.Inputs[0].UnlockingScript) == 0 {
		t.Error("expected the input to be signed")
	}
}

// go-sdk signs deterministically (RFC 6979), the same as @bsv/sdk: spending
// the fixture's own fake UTXO with three more coins to spare (so the change
// is not split), the Go-built, Go-signed transaction is byte-identical to the
// one postern's TypeScript built and signed, raw hex and txid both.
func TestSignReproducesTheFixturesTransactionByteForByte(t *testing.T) {
	f := loadProtocolFixture(t)
	senderWIF := wifFromHex(t, f.Inputs.SenderPrivateKeyHex)
	keys := postern.New(keyFileHolding(t, senderWIF))

	rawtx, err := keys.Sign([]application.PosternUtxo{
		{Txid: f.Inputs.Utxo.Txid, Vout: f.Inputs.Utxo.Vout, Satoshis: f.Inputs.Utxo.Satoshis, Height: 100},
		{Txid: fundingTxid, Vout: 0, Satoshis: 5000, Height: 100},
		{Txid: fundingTxid, Vout: 1, Satoshis: 5000, Height: 100},
		{Txid: fundingTxid, Vout: 2, Satoshis: 5000, Height: 100},
	}, fixturePayload(t, f))
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	if rawtx != f.Transaction.RawtxHex {
		t.Fatalf("expected the fixture's raw transaction\n%s\ngot\n%s", f.Transaction.RawtxHex, rawtx)
	}
	tx, err := transaction.NewTransactionFromHex(rawtx)
	if err != nil {
		t.Fatalf("parsing the signed transaction: %v", err)
	}
	if got := tx.TxID().String(); got != f.Transaction.Txid {
		t.Fatalf("expected txid %s, got %s", f.Transaction.Txid, got)
	}
}

func TestSignRefusesWithNothingButTokensToSpend(t *testing.T) {
	f := loadProtocolFixture(t)
	keys := postern.New(keyFileHolding(t, wifFromHex(t, f.Inputs.SenderPrivateKeyHex)))

	_, err := keys.Sign([]application.PosternUtxo{{Txid: tokenTxid, Vout: 1, Satoshis: 1}}, fixturePayload(t, f))
	if err == nil || !strings.Contains(err.Error(), "no spendable") {
		t.Fatalf("expected a refusal naming no spendable coins, got: %v", err)
	}
}

func TestSignRefusesWhenTheCoinsDoNotCoverTheAnchorAndTheFee(t *testing.T) {
	f := loadProtocolFixture(t)
	keys := postern.New(keyFileHolding(t, wifFromHex(t, f.Inputs.SenderPrivateKeyHex)))

	_, err := keys.Sign([]application.PosternUtxo{{Txid: fundingTxid, Vout: 0, Satoshis: 2}}, fixturePayload(t, f))
	if err == nil || !strings.Contains(err.Error(), "not enough") {
		t.Fatalf("expected a refusal naming not enough satoshis, got: %v", err)
	}
}

// WhatsOnChain lists an outpoint twice while a block confirms it
// (bad-txns-inputs-duplicate): the transaction spends it once.
func TestSignSpendsAnOutpointListedTwiceOnce(t *testing.T) {
	f := loadProtocolFixture(t)
	keys := postern.New(keyFileHolding(t, wifFromHex(t, f.Inputs.SenderPrivateKeyHex)))

	rawtx, err := keys.Sign([]application.PosternUtxo{
		{Txid: fundingTxid, Vout: 2, Satoshis: 10000},
		{Txid: fundingTxid, Vout: 2, Satoshis: 10000},
	}, fixturePayload(t, f))
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	tx, err := transaction.NewTransactionFromHex(rawtx)
	if err != nil {
		t.Fatalf("parsing the signed transaction: %v", err)
	}
	if len(tx.Inputs) != 1 {
		t.Fatalf("expected one input for the outpoint listed twice, got %d", len(tx.Inputs))
	}
}

func spentInputs(t *testing.T, rawtx string) []string {
	t.Helper()
	tx, err := transaction.NewTransactionFromHex(rawtx)
	if err != nil {
		t.Fatalf("parsing the signed transaction: %v", err)
	}
	var spent []string
	for _, in := range tx.Inputs {
		spent = append(spent, fmt.Sprintf("%s:%d", in.SourceTXID.String(), in.SourceTxOutIndex))
	}
	return spent
}

// WhatsOnChain keeps listing an output the last send spent until a block
// confirms the spend (txn-mempool-conflict): once a send is broadcast, the
// next does not spend what it spent.
func TestSignSkipsOutpointsAPriorSendSpent(t *testing.T) {
	f := loadProtocolFixture(t)
	keys := postern.New(keyFileHolding(t, wifFromHex(t, f.Inputs.SenderPrivateKeyHex)))
	spent := application.PosternUtxo{Txid: fundingTxid, Vout: 2, Satoshis: 10000}
	fresh := application.PosternUtxo{Txid: tokenTxid, Vout: 2, Satoshis: 9000}
	if err := keys.MarkSpent([]application.PosternUtxo{spent}); err != nil {
		t.Fatalf("marking spent: %v", err)
	}

	rawtx, err := keys.Sign([]application.PosternUtxo{spent, fresh}, fixturePayload(t, f))
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	if got := spentInputs(t, rawtx); len(got) != 1 || got[0] != tokenTxid+":2" {
		t.Fatalf("expected only %s:2 spent, got %v", tokenTxid, got)
	}

	_, err = keys.Sign([]application.PosternUtxo{spent}, fixturePayload(t, f))
	if err == nil || !strings.Contains(err.Error(), "no spendable") {
		t.Fatalf("expected a refusal naming no spendable coins when only a spent outpoint is listed, got: %v", err)
	}
}

// A remembered spend is forgotten after two hours: by then a block has
// surely confirmed it, and the outpoint is no longer listed.
func TestSignIgnoresSpentOutpointsOlderThanTwoHours(t *testing.T) {
	f := loadProtocolFixture(t)
	now := time.Unix(1758700000, 0)
	keys := postern.New(keyFileHolding(t, wifFromHex(t, f.Inputs.SenderPrivateKeyHex))).
		WithClock(func() time.Time { return now })
	coin := application.PosternUtxo{Txid: fundingTxid, Vout: 2, Satoshis: 10000}
	if err := keys.MarkSpent([]application.PosternUtxo{coin}); err != nil {
		t.Fatalf("marking spent: %v", err)
	}

	now = now.Add(2*time.Hour - time.Minute)
	if _, err := keys.Sign([]application.PosternUtxo{coin}, fixturePayload(t, f)); err == nil {
		t.Fatal("expected the outpoint spent under two hours ago to still be skipped")
	}
	now = now.Add(2 * time.Minute)
	rawtx, err := keys.Sign([]application.PosternUtxo{coin}, fixturePayload(t, f))
	if err != nil {
		t.Fatalf("expected the outpoint spent over two hours ago to be spendable again: %v", err)
	}
	if got := spentInputs(t, rawtx); len(got) != 1 || got[0] != fundingTxid+":2" {
		t.Fatalf("expected %s:2 spent, got %v", fundingTxid, got)
	}
}

// Marking spent adds to what is already remembered.
func TestMarkSpentAddsToWhatIsRemembered(t *testing.T) {
	f := loadProtocolFixture(t)
	keys := postern.New(keyFileHolding(t, wifFromHex(t, f.Inputs.SenderPrivateKeyHex)))
	a := application.PosternUtxo{Txid: fundingTxid, Vout: 0, Satoshis: 10000}
	b := application.PosternUtxo{Txid: fundingTxid, Vout: 1, Satoshis: 10000}
	c := application.PosternUtxo{Txid: tokenTxid, Vout: 0, Satoshis: 10000}
	if err := keys.MarkSpent([]application.PosternUtxo{a}); err != nil {
		t.Fatal(err)
	}
	if err := keys.MarkSpent([]application.PosternUtxo{b}); err != nil {
		t.Fatal(err)
	}
	rawtx, err := keys.Sign([]application.PosternUtxo{a, b, c}, fixturePayload(t, f))
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	if got := spentInputs(t, rawtx); len(got) != 1 || got[0] != tokenTxid+":0" {
		t.Fatalf("expected only %s:0 spent, got %v", tokenTxid, got)
	}
}

func changeOutputs(t *testing.T, rawtx string) []uint64 {
	t.Helper()
	tx, err := transaction.NewTransactionFromHex(rawtx)
	if err != nil {
		t.Fatalf("parsing the signed transaction: %v", err)
	}
	var change []uint64
	for _, o := range tx.Outputs[2:] {
		change = append(change, o.Satoshis)
	}
	return change
}

// A send spends one coin, a confirmed one, largest first, before an
// unconfirmed one, however many are listed.
func TestSignSpendsOneConfirmedCoinBeforeAnUnconfirmedOne(t *testing.T) {
	f := loadProtocolFixture(t)
	keys := postern.New(keyFileHolding(t, wifFromHex(t, f.Inputs.SenderPrivateKeyHex)))

	rawtx, err := keys.Sign([]application.PosternUtxo{
		{Txid: fundingTxid, Vout: 0, Satoshis: 9000},
		{Txid: fundingTxid, Vout: 1, Satoshis: 2000, Height: 100},
		{Txid: fundingTxid, Vout: 2, Satoshis: 3000, Height: 100},
	}, fixturePayload(t, f))
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	if got := spentInputs(t, rawtx); len(got) != 1 || got[0] != fundingTxid+":2" {
		t.Fatalf("expected only the 3000-satoshi confirmed coin %s:2 spent, got %v", fundingTxid, got)
	}
}

// A send takes more coins only when one cannot cover the anchor and the fee.
func TestSignAddsCoinsUntilTheyCoverTheFee(t *testing.T) {
	f := loadProtocolFixture(t)
	keys := postern.New(keyFileHolding(t, wifFromHex(t, f.Inputs.SenderPrivateKeyHex)))

	rawtx, err := keys.Sign([]application.PosternUtxo{
		{Txid: fundingTxid, Vout: 0, Satoshis: 2, Height: 100},
		{Txid: fundingTxid, Vout: 1, Satoshis: 2, Height: 100},
		{Txid: fundingTxid, Vout: 2, Satoshis: 2, Height: 100},
	}, fixturePayload(t, f))
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	if got := spentInputs(t, rawtx); len(got) != 2 {
		t.Fatalf("expected two of the three 2-satoshi coins spent, got %v", got)
	}
}

// One coin left means the change is split so later sends each have a coin;
// coins that already number four mean one change output; and a coin too
// small to split gets one.
func TestSignSplitsTheChangeToLeaveFourCoins(t *testing.T) {
	f := loadProtocolFixture(t)
	keys := postern.New(keyFileHolding(t, wifFromHex(t, f.Inputs.SenderPrivateKeyHex)))
	payload := fixturePayload(t, f)

	rawtx, err := keys.Sign([]application.PosternUtxo{{Txid: fundingTxid, Vout: 0, Satoshis: 10000, Height: 100}}, payload)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	change := changeOutputs(t, rawtx)
	if len(change) != 4 {
		t.Fatalf("expected four change outputs from the only coin, got %v", change)
	}
	for _, sats := range change {
		if sats < 547 {
			t.Errorf("expected each change output above the dust limit plus a fee, got %v", change)
		}
	}

	rawtx, err = keys.Sign([]application.PosternUtxo{
		{Txid: fundingTxid, Vout: 0, Satoshis: 10000, Height: 100},
		{Txid: fundingTxid, Vout: 1, Satoshis: 5000, Height: 100},
	}, payload)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	if change := changeOutputs(t, rawtx); len(change) != 3 {
		t.Fatalf("expected three change outputs to leave four coins with one to spare, got %v", change)
	}

	rawtx, err = keys.Sign([]application.PosternUtxo{{Txid: fundingTxid, Vout: 0, Satoshis: 1000, Height: 100}}, payload)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	if change := changeOutputs(t, rawtx); len(change) != 1 {
		t.Fatalf("expected one change output where a split would go below the dust limit, got %v", change)
	}
}

// Change a send made is spendable at once, though the listing does not show
// it until a block confirms it, and is never spent twice.
func TestSignSpendsChangeMarkSentRemembers(t *testing.T) {
	f := loadProtocolFixture(t)
	now := time.Unix(1758700000, 0)
	keys := postern.New(keyFileHolding(t, wifFromHex(t, f.Inputs.SenderPrivateKeyHex))).
		WithClock(func() time.Time { return now })
	payload := fixturePayload(t, f)
	listing := []application.PosternUtxo{{Txid: fundingTxid, Vout: 0, Satoshis: 10000, Height: 100}}

	first, err := keys.Sign(listing, payload)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	if err := keys.MarkSent(first); err != nil {
		t.Fatalf("marking sent: %v", err)
	}
	firstTx, _ := transaction.NewTransactionFromHex(first)

	// The listing still shows only the spent coin; the first send's four
	// change coins are spent oldest first, one to a send.
	spentBefore := map[string]bool{fundingTxid + ":0": true}
	for i := 0; i < 4; i++ {
		next, err := keys.Sign(listing, payload)
		if err != nil {
			t.Fatalf("send %d: %v", i+2, err)
		}
		got := spentInputs(t, next)
		if len(got) != 1 || spentBefore[got[0]] || !strings.HasPrefix(got[0], firstTx.TxID().String()+":") {
			t.Fatalf("send %d: expected one change output of the first send, not spent before, got %v", i+2, got)
		}
		spentBefore[got[0]] = true
		now = now.Add(time.Second)
		if err := keys.MarkSent(next); err != nil {
			t.Fatalf("marking sent: %v", err)
		}
	}

	now = now.Add(3 * time.Hour)
	if _, err := keys.Sign(nil, payload); err == nil || !strings.Contains(err.Error(), "no spendable") {
		t.Fatalf("expected remembered change to be forgotten after two hours, got: %v", err)
	}
}
