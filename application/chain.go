package application

import (
	"context"
	"time"
)

// Chain is the port to the chain: the one way a use case puts a record on it,
// reads what the postern key holds there, and looks a transaction up. The
// adapter, infrastructure/bsv, is BSV testnet through the postern backend and
// WhatsOnChain; cmd/mw builds it in one place, where another chain would go.
type Chain interface {
	// Send puts payload on the chain as one record, postern's
	// docs/protocol.md section 4, paid for from the postern key's own coins,
	// and reports its txid. A record that went but whose spent coins could
	// not be remembered is sent all the same: the adapter says so on its
	// writer, and the next send within the block may fail.
	Send(ctx context.Context, payload []byte) (txid string, err error)
	// Balance reports the postern key's balance, in satoshis, confirmed and
	// unconfirmed together: what the float cap is measured against.
	Balance(ctx context.Context) (int64, error)
	// Tx reports what a block explorer says of the transaction txid.
	Tx(ctx context.Context, txid string) (ChainTx, error)
}

// ChainTx is what the chain says of one transaction.
type ChainTx struct {
	// Known is false when the explorer has not heard of the transaction.
	Known bool
	// Height is the block the transaction is in; 0 while it waits in the
	// mempool.
	Height int64
	// Time is the block's time, UTC; zero while there is no block.
	Time time.Time
}
