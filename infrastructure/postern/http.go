package postern

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.Postern = (*HTTP)(nil)

// httpTimeout bounds each call to the backend: it sits at the far end of a
// WireGuard tunnel, and a broadcast waits on WhatsOnChain behind it.
const httpTimeout = 60 * time.Second

// HTTP is the postern backend's HTTP API (postern's docs/api.md), at base —
// config postern_backend, the desktop's backend over WireGuard.
type HTTP struct {
	base   string
	client *http.Client
}

// NewHTTP is the postern backend at base, e.g. http://desktop.mw:8787.
func NewHTTP(base string) *HTTP {
	return &HTTP{base: strings.TrimRight(base, "/"), client: &http.Client{Timeout: httpTimeout}}
}

// apiRecord is one record as GET /api/messages returns it. Its payload is
// read from scriptHex, the bytes on chain, not from the backend's own parse.
type apiRecord struct {
	Seq       int64  `json:"seq"`
	Txid      string `json:"txid"`
	ScriptHex string `json:"scriptHex"`
}

// Messages implements application.Postern: GET /api/messages?since=. A
// record that is not a postern message (another kind, or not a version-1
// record at all) comes back with only its Seq and Txid, addressed to nobody,
// so the inbox's cursor still moves past it.
func (h *HTTP) Messages(ctx context.Context, since int64) ([]application.PosternRecord, error) {
	var body struct {
		Records []apiRecord `json:"records"`
	}
	if err := h.do(ctx, http.MethodGet, "/api/messages?since="+strconv.FormatInt(since, 10), nil, &body); err != nil {
		return nil, err
	}
	records := make([]application.PosternRecord, 0, len(body.Records))
	for _, r := range body.Records {
		record := application.PosternRecord{Seq: r.Seq, Txid: r.Txid}
		if raw, ok := DecodeRecordScript(r.ScriptHex); ok {
			var p application.PosternPayload
			if json.Unmarshal(raw, &p) == nil && p.Kind == application.PosternMessageKind {
				record.Class, record.From, record.To = p.Class, p.From, p.To
				record.Ts, record.Ciphertext = time.Unix(p.Ts, 0).UTC(), p.Ct
			}
		}
		records = append(records, record)
	}
	return records, nil
}

// Utxos implements application.Postern: GET /api/utxos/{address}.
func (h *HTTP) Utxos(ctx context.Context, address string) ([]application.PosternUtxo, error) {
	var body struct {
		Utxos []struct {
			Txid     string `json:"txid"`
			Vout     int    `json:"vout"`
			Satoshis int64  `json:"satoshis"`
		} `json:"utxos"`
	}
	if err := h.do(ctx, http.MethodGet, "/api/utxos/"+url.PathEscape(address), nil, &body); err != nil {
		return nil, err
	}
	utxos := make([]application.PosternUtxo, 0, len(body.Utxos))
	for _, u := range body.Utxos {
		utxos = append(utxos, application.PosternUtxo{Txid: u.Txid, Vout: u.Vout, Satoshis: u.Satoshis})
	}
	return utxos, nil
}

// Balance implements application.Postern: GET /api/balance/{address},
// confirmed and unconfirmed together.
func (h *HTTP) Balance(ctx context.Context, address string) (int64, error) {
	var body struct {
		Confirmed   int64 `json:"confirmed"`
		Unconfirmed int64 `json:"unconfirmed"`
	}
	if err := h.do(ctx, http.MethodGet, "/api/balance/"+url.PathEscape(address), nil, &body); err != nil {
		return 0, err
	}
	return body.Confirmed + body.Unconfirmed, nil
}

// Broadcast implements application.Postern: POST /api/broadcast.
func (h *HTTP) Broadcast(ctx context.Context, rawtx string) (string, error) {
	req, err := json.Marshal(struct {
		Rawtx string `json:"rawtx"`
	}{rawtx})
	if err != nil {
		return "", err
	}
	var body struct {
		Txid string `json:"txid"`
	}
	if err := h.do(ctx, http.MethodPost, "/api/broadcast", req, &body); err != nil {
		return "", err
	}
	if body.Txid == "" {
		return "", fmt.Errorf("the postern backend at %s took the broadcast but reported no txid", h.base)
	}
	return body.Txid, nil
}

// do makes one call to the backend and reads its JSON answer into into. An
// answer other than 200 is an error carrying the status and the backend's own
// {"error": ...} message.
func (h *HTTP) do(ctx context.Context, method, path string, body []byte, into any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, h.base+path, reader)
	if err != nil {
		return fmt.Errorf("asking the postern backend at %s: %w", h.base, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("reaching the postern backend at %s: %w", h.base, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading the postern backend's answer to %s %s: %w", method, path, err)
	}
	if resp.StatusCode != http.StatusOK {
		var said struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &said) != nil || said.Error == "" {
			said.Error = strings.TrimSpace(string(raw))
		}
		return fmt.Errorf("the postern backend at %s said %d to %s %s: %s", h.base, resp.StatusCode, method, path, said.Error)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("the postern backend at %s answered %s %s with something that is not its JSON: %w", h.base, method, path, err)
	}
	return nil
}
