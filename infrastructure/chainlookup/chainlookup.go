// Package chainlookup asks WhatsOnChain's testnet API about a transaction: the
// block it is in, and when.
package chainlookup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// DefaultBase is WhatsOnChain's testnet API.
const DefaultBase = "https://api.whatsonchain.com/v1/bsv/test"

const timeout = 30 * time.Second

// WhatsOnChain is the bsv.Lookup that reads
// GET <base>/tx/hash/<txid>.
type WhatsOnChain struct {
	base   string
	client *http.Client
}

// New is a lookup against base; an empty base is DefaultBase.
func New(base string) *WhatsOnChain {
	if base == "" {
		base = DefaultBase
	}
	return &WhatsOnChain{base: strings.TrimRight(base, "/"), client: &http.Client{Timeout: timeout}}
}

// txWire is the part of WhatsOnChain's transaction answer that is read. A
// transaction in the mempool has no blockheight, or 0.
type txWire struct {
	BlockHeight int64 `json:"blockheight"`
	BlockTime   int64 `json:"blocktime"`
}

// Tx implements bsv.Lookup, application.Chain's Tx. A 404 is a transaction
// the explorer has not heard of, not an error.
func (w *WhatsOnChain) Tx(ctx context.Context, txid string) (application.ChainTx, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.base+"/tx/hash/"+url.PathEscape(txid), nil)
	if err != nil {
		return application.ChainTx{}, err
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return application.ChainTx{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return application.ChainTx{}, err
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return application.ChainTx{}, nil
	case resp.StatusCode != http.StatusOK:
		return application.ChainTx{}, fmt.Errorf("WhatsOnChain answered %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var wire txWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return application.ChainTx{}, fmt.Errorf("WhatsOnChain's answer is not a transaction: %w", err)
	}
	tx := application.ChainTx{Known: true, Height: wire.BlockHeight}
	if wire.BlockHeight > 0 && wire.BlockTime > 0 {
		tx.Time = time.Unix(wire.BlockTime, 0).UTC()
	}
	return tx, nil
}
