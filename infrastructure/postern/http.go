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

// posternAuthScheme is the Authorization header's scheme token, postern's
// docs/api.md Authentication section: "Postern <pubkeyHex>:<nonceHex>:<sigHex>".
const posternAuthScheme = "Postern "

// posternNoLicenceError is the postern backend's own error text (its
// server/internal/api/handlers.go, requireLicence) when the signed, verified
// key it was asked to prove holds no licence.
const posternNoLicenceError = "no licence held"

// ChallengeSigner proves control of a key to the postern backend, postern's
// docs/api.md Authentication section: its own compressed public key, and a
// signature over a challenge nonce the backend issued. KeyFile is the real
// implementation.
type ChallengeSigner interface {
	// PublicKey reports the caller's compressed public key, hex.
	PublicKey() (pubKeyHex string, address string, err error)
	// SignNonce signs nonce, reporting a DER-encoded ECDSA signature, hex.
	SignNonce(nonce string) (sigHex string, err error)
}

// HTTP is the postern backend's HTTP API (postern's docs/api.md), at base —
// config postern_backend, the desktop's backend over WireGuard. Every
// endpoint but GET /api/challenge and /healthz requires proof that the
// caller holds a licensed key, so every call here first asks the backend for
// a fresh challenge nonce and signs it with keys.
type HTTP struct {
	base   string
	client *http.Client
	keys   ChallengeSigner
}

// NewHTTP is the postern backend at base, e.g. http://desktop.mw:8787,
// authenticating every call with keys — the Mayor's postern key.
func NewHTTP(base string, keys ChallengeSigner) *HTTP {
	return &HTTP{base: strings.TrimRight(base, "/"), client: &http.Client{Timeout: httpTimeout}, keys: keys}
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
	if err := h.authDo(ctx, http.MethodGet, "/api/messages?since="+strconv.FormatInt(since, 10), nil, &body); err != nil {
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
	if err := h.authDo(ctx, http.MethodGet, "/api/utxos/"+url.PathEscape(address), nil, &body); err != nil {
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
	if err := h.authDo(ctx, http.MethodGet, "/api/balance/"+url.PathEscape(address), nil, &body); err != nil {
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
	if err := h.authDo(ctx, http.MethodPost, "/api/broadcast", req, &body); err != nil {
		return "", err
	}
	if body.Txid == "" {
		return "", fmt.Errorf("the postern backend at %s took the broadcast but reported no txid", h.base)
	}
	return body.Txid, nil
}

// authDo is do, proven to the backend first: it asks for a fresh challenge
// (postern's docs/api.md Authentication section — a nonce is consumed the
// moment it is presented, so every call needs its own), signs it with keys,
// and carries the result as the request's Authorization header. A key the
// backend answers holds no licence is reported plainly, naming the key,
// rather than the backend's own terse 401.
func (h *HTTP) authDo(ctx context.Context, method, path string, body []byte, into any) error {
	pubKeyHex, header, err := h.authHeader(ctx)
	if err != nil {
		return err
	}
	if err := h.do(ctx, method, path, body, into, header); err != nil {
		if strings.Contains(err.Error(), posternNoLicenceError) {
			return fmt.Errorf("the postern key %s holds no licence: mint one before mw can use the postern backend at %s", pubKeyHex, h.base)
		}
		return err
	}
	return nil
}

// authHeader asks the backend for a fresh challenge nonce and signs it with
// keys, reporting the caller's own public key alongside the Authorization
// header value it built, postern's docs/api.md Authentication section:
// "Postern <pubkeyHex>:<nonceHex>:<sigHex>".
func (h *HTTP) authHeader(ctx context.Context) (pubKeyHex, header string, err error) {
	pubKeyHex, _, err = h.keys.PublicKey()
	if err != nil {
		return "", "", fmt.Errorf("reading the postern key to authenticate to the backend: %w", err)
	}
	nonce, err := h.challenge(ctx)
	if err != nil {
		return "", "", err
	}
	sigHex, err := h.keys.SignNonce(nonce)
	if err != nil {
		return "", "", fmt.Errorf("signing the postern backend's challenge: %w", err)
	}
	return pubKeyHex, posternAuthScheme + pubKeyHex + ":" + nonce + ":" + sigHex, nil
}

// challenge asks the backend for a nonce to sign: GET /api/challenge, the one
// endpoint besides /healthz that needs no proof of its own.
func (h *HTTP) challenge(ctx context.Context) (string, error) {
	var body struct {
		Nonce string `json:"nonce"`
	}
	if err := h.do(ctx, http.MethodGet, "/api/challenge", nil, &body, ""); err != nil {
		return "", fmt.Errorf("asking the postern backend for a challenge: %w", err)
	}
	if body.Nonce == "" {
		return "", fmt.Errorf("the postern backend at %s issued an empty challenge nonce", h.base)
	}
	return body.Nonce, nil
}

// do makes one call to the backend and reads its JSON answer into into,
// carrying authHeader as the request's Authorization header when it is not
// empty. An answer other than 200 is an error carrying the status and the
// backend's own {"error": ...} message.
func (h *HTTP) do(ctx context.Context, method, path string, body []byte, into any, authHeader string) error {
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
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
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
