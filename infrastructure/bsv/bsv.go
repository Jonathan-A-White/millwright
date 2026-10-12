// Package bsv is the application.Chain on BSV testnet: a record is signed by
// the postern key file from the key's own coins and broadcast through the
// postern backend, and a transaction is looked up on a block explorer.
package bsv

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/bsv-blockchain/go-sdk/transaction"

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
// nothing. A broadcast the node refuses because a coin it spends is spent
// already has those coins remembered as spent and is built again from the
// others.
func (c *Chain) Send(ctx context.Context, payload []byte) (string, error) {
	_, address, err := c.keys.PublicKey()
	if err != nil {
		return "", err
	}
	utxos, err := c.coins.Utxos(ctx, address)
	if err != nil {
		return "", err
	}
	var refused error
	for range maxRefusals + 1 {
		rawtx, err := c.keys.Sign(utxos, payload)
		if err != nil {
			if refused != nil {
				return "", refused
			}
			return "", err
		}
		txid, err := c.coins.Broadcast(ctx, rawtx)
		if err == nil {
			if err := c.keys.MarkSent(rawtx); err != nil && c.warn != nil {
				fmt.Fprintf(c.warn, "warning: the record was broadcast, but mw could not remember what it spent, so a send within the next block may fail: %v\n", err)
			}
			return txid, nil
		}
		if !spentAlready(err) || !c.remember(rawtx) {
			return "", err
		}
		refused = err
	}
	return "", refused
}

// maxRefusals is how many broadcasts refused for a coin spent already one
// Send builds again from the other coins before it gives the refusal back:
// the key's sends leave it about four coins, each the head of a chain. A
// refused send of two coins has both remembered as spent, the one still
// spendable too, for the two hours the key file remembers.
const maxRefusals = 8

// spentAlready reports whether the node refused a broadcast because a coin it
// spends is spent already by a transaction it holds: in its mempool
// (txn-mempool-conflict), or in a block (bad-txns-inputs-missingorspent).
// WhatsOnChain lists a confirmed output as unspent while an unconfirmed
// transaction spends it, and the key file forgets its own spends after two
// hours, so a chain of sends left unconfirmed longer than that (testnet blocks
// that take no low-fee transactions, mw-gq6.377) has every send build on the
// spent coin again, the confirmed one first.
func spentAlready(err error) bool {
	return strings.Contains(err.Error(), "txn-mempool-conflict") || strings.Contains(err.Error(), "bad-txns-inputs-missingorspent")
}

// remember has the key file remember the coins the refused rawtx spent as
// spent, so that Sign builds the next try, and the sends after it, from the
// others. It reports whether it could.
func (c *Chain) remember(rawtx string) bool {
	tx, err := transaction.NewTransactionFromHex(rawtx)
	if err != nil || len(tx.Inputs) == 0 {
		return false
	}
	spent := make([]application.PosternUtxo, 0, len(tx.Inputs))
	for _, in := range tx.Inputs {
		spent = append(spent, application.PosternUtxo{Txid: in.SourceTXID.String(), Vout: int(in.SourceTxOutIndex)})
	}
	return c.keys.MarkSpent(spent) == nil
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
