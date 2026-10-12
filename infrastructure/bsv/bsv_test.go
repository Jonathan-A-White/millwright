package bsv_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bsv-blockchain/go-sdk/transaction"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/bsv"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
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

// mempool is a postern backend in front of a node whose mempool already holds
// spends of some listed outputs: WhatsOnChain lists a confirmed output while
// an unconfirmed transaction spends it, and refuses a broadcast spending it
// again in the words the backend passes on.
type mempool struct {
	utxos     []application.PosternUtxo
	spent     map[string]bool
	broadcast []*transaction.Transaction
}

func (m *mempool) Utxos(context.Context, string) ([]application.PosternUtxo, error) {
	return m.utxos, nil
}

func (m *mempool) Balance(context.Context, string) (int64, error) { return 0, nil }

func (m *mempool) Broadcast(_ context.Context, rawtx string) (string, error) {
	tx, err := transaction.NewTransactionFromHex(rawtx)
	if err != nil {
		return "", err
	}
	m.broadcast = append(m.broadcast, tx)
	for _, in := range tx.Inputs {
		if m.spent[fmt.Sprintf("%s:%d", in.SourceTXID, in.SourceTxOutIndex)] {
			return "", errors.New(`the postern backend at http://10.88.0.2:8787 said 502 to POST /api/broadcast: WhatsOnChain said 400: "unexpected response code 500: 258: txn-mempool-conflict"`)
		}
	}
	for _, in := range tx.Inputs {
		m.spent[fmt.Sprintf("%s:%d", in.SourceTXID, in.SourceTxOutIndex)] = true
	}
	return tx.TxID().String(), nil
}

// inputsOf is the outpoints tx spends.
func inputsOf(tx *transaction.Transaction) []string {
	var points []string
	for _, in := range tx.Inputs {
		points = append(points, fmt.Sprintf("%s:%d", in.SourceTXID, in.SourceTxOutIndex))
	}
	return points
}

// mw-gq6.377: the funding chain stops confirming for over two hours, so the
// key file forgets what its sends spent while WhatsOnChain still lists the
// confirmed output the first of them spent. The send builds on that output,
// the confirmed coin first, and the node refuses it as a mempool conflict.
// The send remembers the refused input as spent and builds again on the coin
// that is still spendable, the unconfirmed change; the next send does not try
// the refused input again.
func TestSendRefusedAsAMempoolConflictSpendsAnotherCoin(t *testing.T) {
	const (
		confirmed   = "758339e7d73befcf42d60e6b81fad01e13c98b337ddbd6fe4518aad38feede99"
		unconfirmed = "78d3f0751437b90306e6aa820a4ca29b4c5ae089dd6080111f2d4086e97e99c0"
	)
	keys := postern.New(filepath.Join(t.TempDir(), "postern.key"))
	if err := keys.Generate(); err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	m := &mempool{
		utxos: []application.PosternUtxo{
			{Txid: unconfirmed, Vout: 2, Satoshis: 17693},
			{Txid: confirmed, Vout: 2, Satoshis: 17708, Height: 1762842},
		},
		spent: map[string]bool{confirmed + ":2": true},
	}
	chain := bsv.New(m, keys, nil, nil)

	txid, err := chain.Send(context.Background(), []byte("stamp one"))
	if err != nil {
		t.Fatalf("expected the send to spend the coin still spendable, got %v", err)
	}
	last := m.broadcast[len(m.broadcast)-1]
	if got := inputsOf(last); len(got) != 1 || got[0] != unconfirmed+":2" || txid != last.TxID().String() {
		t.Fatalf("expected the send to go out spending %s:2, got %v as %s", unconfirmed, got, txid)
	}

	// The next send, a minute later, lists the refused coin again (and the
	// first send's change, which it may spend), and does not try it.
	m.utxos = append(m.utxos, application.PosternUtxo{Txid: txid, Vout: 2, Satoshis: 17692})
	tried := len(m.broadcast)
	if _, err := chain.Send(context.Background(), []byte("stamp two")); err != nil {
		t.Fatalf("expected the next send to go out, got %v", err)
	}
	if len(m.broadcast) != tried+1 {
		t.Fatalf("expected the next send to go out first time, it took %d broadcasts", len(m.broadcast)-tried)
	}
	for _, point := range inputsOf(m.broadcast[tried]) {
		if point == confirmed+":2" {
			t.Fatalf("expected the next send not to spend %s again", point)
		}
	}
}

// A send whose every coin the mempool has already spent fails, in the
// backend's words, after one try of each: it does not spin.
func TestSendWhoseEveryCoinIsSpentFailsInTheBackendsWords(t *testing.T) {
	const spent = "885e49e01c27aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	keys := postern.New(filepath.Join(t.TempDir(), "postern.key"))
	if err := keys.Generate(); err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	m := &mempool{
		utxos: []application.PosternUtxo{{Txid: spent, Vout: 2, Satoshis: 17708, Height: 1762842}},
		spent: map[string]bool{spent + ":2": true},
	}
	chain := bsv.New(m, keys, nil, nil)

	_, err := chain.Send(context.Background(), []byte("stamp"))
	if err == nil || !strings.Contains(err.Error(), "txn-mempool-conflict") {
		t.Fatalf("expected the backend's mempool conflict, got %v", err)
	}
	if len(m.broadcast) != 1 {
		t.Fatalf("expected one broadcast, got %d", len(m.broadcast))
	}
}
