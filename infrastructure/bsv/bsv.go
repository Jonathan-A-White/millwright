// Package bsv is the application.Chain on BSV testnet: a record is signed by
// the postern key file from the key's own coins and broadcast through the
// postern backend, and a transaction is looked up on a block explorer.
package bsv

import (
	"context"
	"fmt"
	"io"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/chainlookup"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

// Chain satisfies the port, from the postern backend's HTTP client and
// WhatsOnChain.
var (
	_ application.Chain = (*Chain)(nil)
	_ Coins             = (*postern.HTTP)(nil)
	_ Lookup            = (*chainlookup.WhatsOnChain)(nil)
)

// Coins is the postern backend's coin calls a send spends through: postern's
// /api/utxos, /api/balance and /api/broadcast. infrastructure/postern's HTTP
// is the one.
type Coins interface {
	// Utxos reports address's unspent outputs.
	Utxos(ctx context.Context, address string) ([]application.PosternUtxo, error)
	// Balance reports address's balance, in satoshis, confirmed and
	// unconfirmed together.
	Balance(ctx context.Context, address string) (int64, error)
	// Broadcast forwards a raw signed transaction and reports its txid.
	Broadcast(ctx context.Context, rawtx string) (string, error)
}

// Lookup asks a block explorer about a transaction: infrastructure/chainlookup's
// WhatsOnChain.
type Lookup interface {
	Tx(ctx context.Context, txid string) (application.ChainTx, error)
}

// Chain is BSV testnet, reached through coins with the postern key file keys
// and looked up on lookup.
type Chain struct {
	coins  Coins
	keys   application.PosternKeyFile
	lookup Lookup
	warn   io.Writer
}

// New is the chain whose records keys signs and coins broadcasts, whose
// transactions lookup reads, and which says on warn a send it could not
// remember. A nil warn says nothing.
func New(coins Coins, keys application.PosternKeyFile, lookup Lookup, warn io.Writer) *Chain {
	return &Chain{coins: coins, keys: keys, lookup: lookup, warn: warn}
}

// Send implements application.Chain: it signs a record transaction from the
// postern key's unspent outputs and has the backend broadcast it, reporting
// its txid. A failure to remember what it spent is said on warn, and fails
// nothing.
func (c *Chain) Send(ctx context.Context, payload []byte) (string, error) {
	_, address, err := c.keys.PublicKey()
	if err != nil {
		return "", err
	}
	utxos, err := c.coins.Utxos(ctx, address)
	if err != nil {
		return "", err
	}
	rawtx, err := c.keys.Sign(utxos, payload)
	if err != nil {
		return "", err
	}
	txid, err := c.coins.Broadcast(ctx, rawtx)
	if err != nil {
		return "", err
	}
	if err := c.keys.MarkSent(rawtx); err != nil && c.warn != nil {
		fmt.Fprintf(c.warn, "warning: the record was broadcast, but mw could not remember what it spent, so a send within the next block may fail: %v\n", err)
	}
	return txid, nil
}

// Balance implements application.Chain: the postern key's address's balance.
func (c *Chain) Balance(ctx context.Context) (int64, error) {
	_, address, err := c.keys.PublicKey()
	if err != nil {
		return 0, err
	}
	return c.coins.Balance(ctx, address)
}

// Tx implements application.Chain: what the explorer says of txid.
func (c *Chain) Tx(ctx context.Context, txid string) (application.ChainTx, error) {
	if c.lookup == nil {
		return application.ChainTx{}, fmt.Errorf("no block explorer is configured to look %s up", txid)
	}
	return c.lookup.Tx(ctx, txid)
}
