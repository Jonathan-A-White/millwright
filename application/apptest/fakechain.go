package apptest

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeChain is an in-memory application.Chain. A send spends the coins Coins,
// a FakePostern, holds for Keys' address, signed by Keys and broadcast to
// Coins, as infrastructure/bsv does over the real backend; so a test sets the
// utxos and balance on the FakePostern and reads its Broadcasts. Tx knows the
// transactions it was told of, in a block or in the mempool; any other txid is
// not known.
type FakeChain struct {
	Coins *FakePostern
	Keys  application.PosternKeyFile
	// Warn is where a send whose spent coins Keys could not remember is said.
	// A nil Warn says nothing.
	Warn io.Writer
	// TxErr, when set, is returned by Tx instead of an answer.
	TxErr error

	mu      sync.Mutex
	txs     map[string]application.ChainTx
	txCalls int
}

// FakeChain satisfies the port.
var _ application.Chain = (*FakeChain)(nil)

// NewFakeChain returns a chain that spends coins' coins with keys, and knows
// no transaction. A chain only looked up in may have neither.
func NewFakeChain(coins *FakePostern, keys application.PosternKeyFile) *FakeChain {
	return &FakeChain{Coins: coins, Keys: keys, txs: map[string]application.ChainTx{}}
}

// Confirm makes txid known, in block height, whose time is at.
func (f *FakeChain) Confirm(txid string, height int64, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.txs[txid] = application.ChainTx{Known: true, Height: height, Time: at}
}

// Mempool makes txid known, in no block yet.
func (f *FakeChain) Mempool(txid string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.txs[txid] = application.ChainTx{Known: true}
}

// TxCalls is how many times Tx was asked, failed or not.
func (f *FakeChain) TxCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.txCalls
}

// Send implements application.Chain.
func (f *FakeChain) Send(ctx context.Context, payload []byte) (string, error) {
	if f.Coins == nil || f.Keys == nil {
		return "", fmt.Errorf("the fake chain has no coins or key file to send with")
	}
	_, address, err := f.Keys.PublicKey()
	if err != nil {
		return "", err
	}
	utxos, err := f.Coins.Utxos(ctx, address)
	if err != nil {
		return "", err
	}
	rawtx, err := f.Keys.Sign(utxos, payload)
	if err != nil {
		return "", err
	}
	txid, err := f.Coins.Broadcast(ctx, rawtx)
	if err != nil {
		return "", err
	}
	if err := f.Keys.MarkSent(rawtx); err != nil && f.Warn != nil {
		fmt.Fprintf(f.Warn, "warning: the record was broadcast, but mw could not remember what it spent, so a send within the next block may fail: %v\n", err)
	}
	return txid, nil
}

// Balance implements application.Chain: what Coins holds for Keys' address.
func (f *FakeChain) Balance(ctx context.Context) (int64, error) {
	if f.Coins == nil || f.Keys == nil {
		return 0, fmt.Errorf("the fake chain has no coins or key file to read a balance with")
	}
	_, address, err := f.Keys.PublicKey()
	if err != nil {
		return 0, err
	}
	return f.Coins.Balance(ctx, address)
}

// Tx implements application.Chain.
func (f *FakeChain) Tx(_ context.Context, txid string) (application.ChainTx, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.txCalls++
	if f.TxErr != nil {
		return application.ChainTx{}, f.TxErr
	}
	return f.txs[txid], nil
}
