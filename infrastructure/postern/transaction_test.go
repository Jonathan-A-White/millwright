package postern_test

import (
	"encoding/hex"
	"strings"
	"testing"

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
	f := loadRecordFixture(t)
	keys := postern.New(keyFileHolding(t, f.SenderWIF))

	rawtx, err := keys.Sign([]application.PosternUtxo{
		{Txid: fundingTxid, Vout: 0, Satoshis: 10000},
		{Txid: tokenTxid, Vout: 1, Satoshis: 1},
	}, []byte(f.Payload))
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
	if len(tx.Outputs) != 3 {
		t.Fatalf("expected three outputs (record, anchor, change), got %d", len(tx.Outputs))
	}
	record, anchor, change := tx.Outputs[0], tx.Outputs[1], tx.Outputs[2]
	if got := hex.EncodeToString(*record.LockingScript); got != f.ScriptHex || record.Satoshis != 0 {
		t.Errorf("expected output 0 to be the fixture's record script at 0 satoshis, got %d satoshis and\n%s", record.Satoshis, got)
	}
	if got := hex.EncodeToString(*anchor.LockingScript); got != p2pkhScriptHex(t, postern.AnchorAddress) || anchor.Satoshis != 1 {
		t.Errorf("expected output 1 to pay 1 satoshi to the anchor %s, got %d satoshis to %s", postern.AnchorAddress, anchor.Satoshis, got)
	}
	if got := hex.EncodeToString(*change.LockingScript); got != p2pkhScriptHex(t, f.SenderAddress) {
		t.Errorf("expected output 2 to pay change to %s, got %s", f.SenderAddress, got)
	}
	fee := int64(10000) - 1 - int64(change.Satoshis)
	if fee < 1 || fee > 10 {
		t.Errorf("expected a fee of a few satoshis at 1 sat/kB, got %d", fee)
	}
	if len(*tx.Inputs[0].UnlockingScript) == 0 {
		t.Error("expected the input to be signed")
	}
}

func TestSignRefusesWithNothingButTokensToSpend(t *testing.T) {
	f := loadRecordFixture(t)
	keys := postern.New(keyFileHolding(t, f.SenderWIF))

	_, err := keys.Sign([]application.PosternUtxo{{Txid: tokenTxid, Vout: 1, Satoshis: 1}}, []byte(f.Payload))
	if err == nil || !strings.Contains(err.Error(), "no spendable") {
		t.Fatalf("expected a refusal naming no spendable coins, got: %v", err)
	}
}

func TestSignRefusesWhenTheCoinsDoNotCoverTheAnchorAndTheFee(t *testing.T) {
	f := loadRecordFixture(t)
	keys := postern.New(keyFileHolding(t, f.SenderWIF))

	_, err := keys.Sign([]application.PosternUtxo{{Txid: fundingTxid, Vout: 0, Satoshis: 2}}, []byte(f.Payload))
	if err == nil || !strings.Contains(err.Error(), "not enough") {
		t.Fatalf("expected a refusal naming not enough satoshis, got: %v", err)
	}
}
