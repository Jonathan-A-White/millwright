package bsv_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/bsv"
)

// coins is a postern backend's coin calls in memory: the utxos and balance it
// holds by address, and the raw transactions broadcast to it.
type coins struct {
	utxos     map[string][]application.PosternUtxo
	balance   map[string]int64
	broadcast []string
	err       error
}

func (c *coins) Utxos(_ context.Context, address string) ([]application.PosternUtxo, error) {
	if c.err != nil {
		return nil, c.err
	}
	return c.utxos[address], nil
}

func (c *coins) Balance(_ context.Context, address string) (int64, error) {
	return c.balance[address], nil
}

func (c *coins) Broadcast(_ context.Context, rawtx string) (string, error) {
	c.broadcast = append(c.broadcast, rawtx)
	return "txid-of-" + rawtx, nil
}

// keys signs a transaction as the utxos it spent and the payload, and
// remembers what it was told was sent.
type keys struct {
	marked  []string
	markErr error
}

func (k *keys) Path() string          { return "/keys/postern.key" }
func (k *keys) Exists() (bool, error) { return true, nil }
func (k *keys) Generate() error       { return nil }
func (k *keys) PublicKey() (string, string, error) {
	return "02pub", "mAddress", nil
}
func (k *keys) PrivateKeyWIF() (string, error) { return "priv", nil }
func (k *keys) Sign(utxos []application.PosternUtxo, payload []byte) (string, error) {
	spent := make([]string, 0, len(utxos))
	for _, u := range utxos {
		spent = append(spent, u.Txid)
	}
	return strings.Join(spent, ",") + "|" + string(payload), nil
}
func (k *keys) MarkSpent([]application.PosternUtxo) error { return nil }
func (k *keys) MarkSent(rawtx string) error {
	k.marked = append(k.marked, rawtx)
	return k.markErr
}

type lookup struct{ asked []string }

func (l *lookup) Tx(_ context.Context, txid string) (application.ChainTx, error) {
	l.asked = append(l.asked, txid)
	return application.ChainTx{Known: true, Height: 7}, nil
}

func newChain(warn io.Writer) (*bsv.Chain, *coins, *keys, *lookup) {
	c := &coins{
		utxos:   map[string][]application.PosternUtxo{"mAddress": {{Txid: "u1", Satoshis: 1000}}},
		balance: map[string]int64{"mAddress": 4321},
	}
	k, l := &keys{}, &lookup{}
	return bsv.New(c, k, l, warn), c, k, l
}

// Send spends the postern key's own coins on a record transaction carrying
// the payload, broadcasts it, remembers it as sent, and reports its txid.
func TestSendSignsTheKeysCoinsBroadcastsAndRemembers(t *testing.T) {
	var warn strings.Builder
	chain, c, k, _ := newChain(&warn)

	txid, err := chain.Send(context.Background(), []byte(`{"v":1}`))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(c.broadcast) != 1 || c.broadcast[0] != `u1|{"v":1}` || txid != `txid-of-u1|{"v":1}` {
		t.Fatalf("expected one transaction of the key's coin and the payload, got %q and %q", c.broadcast, txid)
	}
	if len(k.marked) != 1 || k.marked[0] != c.broadcast[0] || warn.Len() != 0 {
		t.Fatalf("expected the broadcast remembered and nothing said, got %q and %q", k.marked, warn.String())
	}
}

// A send it could not remember is still sent: the txid is reported, and the
// warning is said on the writer, in the words mw has always said it.
func TestSendThatCannotRememberWarnsAndSucceeds(t *testing.T) {
	var warn strings.Builder
	chain, _, k, _ := newChain(&warn)
	k.markErr = errors.New("disk full")

	txid, err := chain.Send(context.Background(), []byte("p"))
	if err != nil || txid == "" {
		t.Fatalf("expected the send to succeed, got %q, %v", txid, err)
	}
	want := "warning: the record was broadcast, but mw could not remember what it spent, so a send within the next block may fail: disk full\n"
	if warn.String() != want {
		t.Fatalf("expected %q, got %q", want, warn.String())
	}

	// No writer says nothing, and fails nothing.
	quiet := bsv.New(&coins{}, k, nil, nil)
	if _, err := quiet.Send(context.Background(), []byte("p")); err != nil {
		t.Fatalf("expected a send with no writer to succeed, got %v", err)
	}
}

// Coins that cannot be read broadcast nothing.
func TestSendWithoutCoinsBroadcastsNothing(t *testing.T) {
	chain, c, _, _ := newChain(nil)
	c.err = errors.New("backend down")
	if _, err := chain.Send(context.Background(), []byte("p")); err == nil || !strings.Contains(err.Error(), "backend down") {
		t.Fatalf("expected the backend's error, got %v", err)
	}
	if len(c.broadcast) != 0 {
		t.Fatalf("expected nothing broadcast, got %q", c.broadcast)
	}
}

// Balance is the postern key's address's, and Tx is the explorer's answer.
func TestBalanceIsTheKeysAndTxIsTheLookups(t *testing.T) {
	chain, _, _, l := newChain(nil)
	balance, err := chain.Balance(context.Background())
	if err != nil || balance != 4321 {
		t.Fatalf("expected the key's balance 4321, got %d, %v", balance, err)
	}
	tx, err := chain.Tx(context.Background(), "abc")
	if err != nil || !tx.Known || tx.Height != 7 || len(l.asked) != 1 || l.asked[0] != "abc" {
		t.Fatalf("expected the lookup asked once, got %+v, %v, %q", tx, err, l.asked)
	}
}
