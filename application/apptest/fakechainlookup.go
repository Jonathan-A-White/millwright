package apptest

import (
	"context"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeChainLookup is an in-memory application.ChainLookup: the transactions it
// was told of, in a block or in the mempool; any other txid is not known.
type FakeChainLookup struct {
	mu    sync.Mutex
	txs   map[string]application.ChainTx
	calls int

	// Err, when set, is returned by Tx instead of an answer.
	Err error
}

// FakeChainLookup satisfies the port.
var _ application.ChainLookup = (*FakeChainLookup)(nil)

// NewFakeChainLookup returns a lookup that knows nothing.
func NewFakeChainLookup() *FakeChainLookup {
	return &FakeChainLookup{txs: map[string]application.ChainTx{}}
}

// Confirm makes txid known, in block height, whose time is at.
func (f *FakeChainLookup) Confirm(txid string, height int64, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.txs[txid] = application.ChainTx{Known: true, Height: height, Time: at}
}

// Mempool makes txid known, in no block yet.
func (f *FakeChainLookup) Mempool(txid string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.txs[txid] = application.ChainTx{Known: true}
}

// Calls is how many times Tx was asked, failed or not.
func (f *FakeChainLookup) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// Tx implements application.ChainLookup.
func (f *FakeChainLookup) Tx(_ context.Context, txid string) (application.ChainTx, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.Err != nil {
		return application.ChainTx{}, f.Err
	}
	return f.txs[txid], nil
}
