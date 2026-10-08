package postern

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// posternBlobHash matches a sha256 hash, hex: 64 hex characters, postern's
// docs/protocol.md section 8.
var posternBlobHash = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

var _ application.Postern = (*HTTP)(nil)
var _ application.PosternStream = (*HTTP)(nil)
var _ application.Prompts = (*HTTP)(nil)

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
	// stream is the client the event stream is read with: no Timeout, which
	// would cut a stream that is meant to stay open; its ctx ends it.
	stream *http.Client
	keys   ChallengeSigner
}

// NewHTTP is the postern backend at base, e.g. http://desktop.mw:8787,
// authenticating every call with keys — the Mayor's postern key.
func NewHTTP(base string, keys ChallengeSigner) *HTTP {
	return &HTTP{base: strings.TrimRight(base, "/"), client: &http.Client{Timeout: httpTimeout}, stream: &http.Client{}, keys: keys}
}

// apiRecord is one record as GET /api/messages returns it. Its payload is
// read from scriptHex, the bytes on chain, not from the backend's own parse.
type apiRecord struct {
	Seq       int64  `json:"seq"`
	Txid      string `json:"txid"`
	ScriptHex string `json:"scriptHex"`
	// Signer is the key that signed the carrying transaction's first input,
	// or — for a record delivered directly (postern's docs/protocol.md
	// section 9) — the key that authenticated its delivery. Absent when the
	// backend cannot say.
	Signer string `json:"signer"`
	// SignerApps are the apps the signer's licences open, stamped on a
	// direct record as it arrived (postern's docs/protocol.md section 18).
	SignerApps []string `json:"signer_apps"`
}

// messagesPageLimit is how many records Messages asks the backend for in a
// page: GET /api/messages?since=&limit=.
const messagesPageLimit = 200

// Messages implements application.Postern: GET /api/messages?since=&limit=,
// drained page by page — each page asked from the previous page's next, until
// the backend says more is false — so a caller keeps one call and its
// signature. A page filtered to no records still moves the cursor on, to its
// next. A backend that answers with no more (an old one) is one page. A record
// that is not a postern message (another kind, or not a version-1 record at
// all) comes back with only its Seq and Txid, addressed to nobody, so the
// inbox's cursor still moves past it.
func (h *HTTP) Messages(ctx context.Context, since int64) ([]application.PosternRecord, error) {
	records := []application.PosternRecord{}
	for {
		var body struct {
			Records []apiRecord `json:"records"`
			Next    int64       `json:"next"`
			More    bool        `json:"more"`
		}
		path := "/api/messages?since=" + strconv.FormatInt(since, 10) + "&limit=" + strconv.Itoa(messagesPageLimit)
		if err := h.authDo(ctx, http.MethodGet, path, nil, &body); err != nil {
			return nil, err
		}
		for _, r := range body.Records {
			record := application.PosternRecord{Seq: r.Seq, Txid: r.Txid, Signer: r.Signer, SignerApps: r.SignerApps}
			if raw, ok := DecodeRecordScript(r.ScriptHex); ok {
				var p application.PosternPayload
				if json.Unmarshal(raw, &p) == nil && p.Kind == application.PosternMessageKind {
					record.Class, record.From, record.To = p.Class, p.From, p.To
					record.Ts, record.Ciphertext = time.Unix(p.Ts, 0).UTC(), p.Ct
				}
			}
			records = append(records, record)
		}
		// A backend that claims more but does not move its cursor would be
		// asked the same page for ever.
		if !body.More || body.Next <= since {
			return records, nil
		}
		since = body.Next
	}
}

// Utxos is one of bsv.Coins: GET /api/utxos/{address}.
func (h *HTTP) Utxos(ctx context.Context, address string) ([]application.PosternUtxo, error) {
	var body struct {
		Utxos []struct {
			Txid     string `json:"txid"`
			Vout     int    `json:"vout"`
			Satoshis int64  `json:"satoshis"`
			Height   int    `json:"height"`
		} `json:"utxos"`
	}
	if err := h.authDo(ctx, http.MethodGet, "/api/utxos/"+url.PathEscape(address), nil, &body); err != nil {
		return nil, err
	}
	utxos := make([]application.PosternUtxo, 0, len(body.Utxos))
	for _, u := range body.Utxos {
		utxos = append(utxos, application.PosternUtxo{Txid: u.Txid, Vout: u.Vout, Satoshis: u.Satoshis, Height: u.Height})
	}
	return utxos, nil
}

// Balance is one of bsv.Coins: GET /api/balance/{address},
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

// Broadcast is one of bsv.Coins: POST /api/broadcast.
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

// Deliver implements application.Postern: POST /api/messages
// {"scriptHex": <the record script carrying payload>}, postern's
// docs/protocol.md section 9, answered {"txid": "direct:…", "seq": n} — 201
// for a new record, 200 for bytes already delivered.
func (h *HTTP) Deliver(ctx context.Context, payload []byte) (string, error) {
	record, err := RecordScript(payload)
	if err != nil {
		return "", fmt.Errorf("building the record script: %w", err)
	}
	req, err := json.Marshal(struct {
		ScriptHex string `json:"scriptHex"`
	}{hex.EncodeToString(*record)})
	if err != nil {
		return "", err
	}
	var body struct {
		Txid string `json:"txid"`
		Seq  int64  `json:"seq"`
	}
	if err := h.authDo(ctx, http.MethodPost, "/api/messages", req, &body); err != nil {
		return "", err
	}
	if body.Txid == "" {
		return "", fmt.Errorf("the postern backend at %s took the message but reported no txid", h.base)
	}
	return body.Txid, nil
}

// promptWire is a prompt as the backend sends and takes it (postern's
// server/README.md, 'Saved prompts'): the signature is a list of option
// objects, and updatedAt and updatedBy are the backend's own stamps, which
// are read past and never sent.
type promptWire struct {
	Name      string       `json:"name"`
	Summary   string       `json:"summary"`
	Signature []optionWire `json:"signature"`
	Body      string       `json:"body"`
}

// optionWire is one option of a prompt's signature: the flag with its leading
// "--", its type (duration, string, int or bool), its default as a string,
// and whether it must be given.
type optionWire struct {
	Flag     string `json:"flag"`
	Type     string `json:"type"`
	Default  string `json:"default,omitempty"`
	Required bool   `json:"required,omitempty"`
	Help     string `json:"help,omitempty"`
}

// wirePrompt is p as the backend takes it. A spec that does not read is an
// error: the backend would refuse it anyway.
func wirePrompt(p domain.Prompt) (promptWire, error) {
	options, err := p.Options()
	if err != nil {
		return promptWire{}, err
	}
	wire := promptWire{Name: p.Name, Summary: p.Summary, Body: p.Body, Signature: make([]optionWire, len(options))}
	for i, o := range options {
		wire.Signature[i] = optionWire{Flag: "--" + o.Flag, Type: o.Type, Default: o.Default, Required: o.Required}
	}
	return wire, nil
}

// prompt is the wire prompt as the rest of mw holds it: each option as its
// spec. A required option keeps no default, as a spec cannot have both.
func (w promptWire) prompt() domain.Prompt {
	p := domain.Prompt{Name: w.Name, Summary: w.Summary, Body: w.Body, Signature: make([]string, len(w.Signature))}
	for i, o := range w.Signature {
		option := domain.PromptOption{Flag: strings.TrimPrefix(o.Flag, "--"), Type: o.Type, Default: o.Default, Required: o.Required}
		if option.Required {
			option.Default = ""
		}
		p.Signature[i] = option.String()
	}
	return p
}

// List implements application.Prompts: GET /api/prompts, answered a bare JSON
// array of prompts sorted by name ([] when none), authenticated as every call
// here is.
func (h *HTTP) List(ctx context.Context) ([]domain.Prompt, error) {
	var body []promptWire
	if err := h.authDo(ctx, http.MethodGet, "/api/prompts", nil, &body); err != nil {
		return nil, err
	}
	prompts := make([]domain.Prompt, len(body))
	for i, w := range body {
		prompts[i] = w.prompt()
	}
	return prompts, nil
}

// Get implements application.Prompts: GET /api/prompts/{name}, answered the
// prompt itself; a 404 is a prompt not saved, and not an error.
func (h *HTTP) Get(ctx context.Context, name string) (domain.Prompt, bool, error) {
	var wire promptWire
	err := h.authDo(ctx, http.MethodGet, "/api/prompts/"+url.PathEscape(name), nil, &wire)
	var status *statusError
	if errors.As(err, &status) && status.code == http.StatusNotFound {
		return domain.Prompt{}, false, nil
	}
	if err != nil {
		return domain.Prompt{}, false, err
	}
	return wire.prompt(), true, nil
}

// Put implements application.Prompts: PUT /api/prompts/{name} with
// {name, summary, signature, body}, saving the prompt whole in place of any
// of that name.
func (h *HTTP) Put(ctx context.Context, p domain.Prompt) error {
	wire, err := wirePrompt(p)
	if err != nil {
		return err
	}
	req, err := json.Marshal(wire)
	if err != nil {
		return err
	}
	_, err = h.authFetch(ctx, http.MethodPut, "/api/prompts/"+url.PathEscape(p.Name), req)
	return err
}

// Delete implements application.Prompts: DELETE /api/prompts/{name}; a 404 is
// a prompt already gone, and not an error.
func (h *HTTP) Delete(ctx context.Context, name string) error {
	_, err := h.authFetch(ctx, http.MethodDelete, "/api/prompts/"+url.PathEscape(name), nil)
	var status *statusError
	if errors.As(err, &status) && status.code == http.StatusNotFound {
		return nil
	}
	return err
}

// UploadBlob implements application.Postern: POST /api/blobs with the
// ciphertext as the body, postern's docs/protocol.md section 8, answered
// {"hash", "size"} — 201 for new bytes, 200 for bytes already stored. A hash
// that is not the body's own sha256 is refused: a message must never
// announce a blob other than the one sent.
func (h *HTTP) UploadBlob(ctx context.Context, blob []byte) (string, int64, error) {
	raw, err := h.authFetchAs(ctx, http.MethodPost, "/api/blobs", blob, "application/octet-stream")
	if err != nil {
		return "", 0, err
	}
	var body struct {
		Hash string `json:"hash"`
		Size int64  `json:"size"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return "", 0, fmt.Errorf("the postern backend at %s answered POST /api/blobs with something that is not its JSON: %w", h.base, err)
	}
	sum := sha256.Sum256(blob)
	if want := hex.EncodeToString(sum[:]); !strings.EqualFold(body.Hash, want) {
		return "", 0, fmt.Errorf("the postern backend at %s stored the attachment under the hash %q, not its sha256 %s", h.base, body.Hash, want)
	}
	return strings.ToLower(body.Hash), body.Size, nil
}

// Me implements application.Postern: GET /api/me (postern's
// docs/protocol.md section 15), who the caller is and who the mill is. The
// mill is absent from the answer when the backend has none.
func (h *HTTP) Me(ctx context.Context) (application.PosternMe, error) {
	var body struct {
		Pubkey  string `json:"pubkey"`
		Mill    string `json:"mill"`
		Network string `json:"network"`
	}
	if err := h.authDo(ctx, http.MethodGet, "/api/me", nil, &body); err != nil {
		return application.PosternMe{}, err
	}
	return application.PosternMe{Pubkey: body.Pubkey, Mill: body.Mill, Network: body.Network}, nil
}

// authDo is do, proven to the backend first: it asks for a fresh challenge
// (postern's docs/api.md Authentication section — a nonce is consumed the
// moment it is presented, so every call needs its own), signs it with keys,
// and carries the result as the request's Authorization header. A key the
// backend answers holds no licence is reported plainly, naming the key,
// rather than the backend's own terse 401.
func (h *HTTP) authDo(ctx context.Context, method, path string, body []byte, into any) error {
	raw, err := h.authFetch(ctx, method, path, body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("the postern backend at %s answered %s %s with something that is not its JSON: %w", h.base, method, path, err)
	}
	return nil
}

// authFetch is fetch, proven to the backend first exactly as authDo is, and
// reporting the raw body rather than unmarshaling it.
func (h *HTTP) authFetch(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	return h.authFetchAs(ctx, method, path, body, "application/json")
}

// authFetchAs is authFetch with a body of contentType rather than JSON.
//
// A GET that times out is tried once more, from a fresh challenge (the first
// one was spent): a backend just restarted walks its licence chain on the
// first call, outlasting the timeout, and has the answer cached for the
// second. A POST or DELETE is never repeated, and neither is a call whose
// own ctx is done.
func (h *HTTP) authFetchAs(ctx context.Context, method, path string, body []byte, contentType string) ([]byte, error) {
	pubKeyHex, raw, err := h.authFetchOnce(ctx, method, path, body, contentType)
	if err != nil && method == http.MethodGet && ctx.Err() == nil && isTimeout(err) {
		pubKeyHex, raw, err = h.authFetchOnce(ctx, method, path, body, contentType)
	}
	if err != nil {
		if strings.Contains(err.Error(), posternNoLicenceError) {
			return nil, fmt.Errorf("the postern key %s holds no licence: mint one before mw can use the postern backend at %s", pubKeyHex, h.base)
		}
		return nil, err
	}
	return raw, nil
}

// isTimeout reports whether err, however wrapped, is a network timeout: the
// client's own Timeout running out.
func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// authFetchOnce is one challenge, signature and call, reporting the caller's
// public key beside the raw body.
func (h *HTTP) authFetchOnce(ctx context.Context, method, path string, body []byte, contentType string) (pubKeyHex string, raw []byte, err error) {
	pubKeyHex, header, err := h.authHeader(ctx)
	if err != nil {
		return "", nil, err
	}
	raw, err = h.fetchAs(ctx, method, path, body, header, contentType)
	return pubKeyHex, raw, err
}

// Blob implements application.Postern: GET /api/blobs/{hash} (postern's
// docs/protocol.md section 8), the whole ciphertext body an attachment was
// uploaded as, untouched. A hash that is not 64 hex characters is refused
// before any network call.
func (h *HTTP) Blob(ctx context.Context, hash string) ([]byte, error) {
	if !posternBlobHash.MatchString(hash) {
		return nil, fmt.Errorf("%q is not a sha256 hash, 64 hex characters: refusing to ask the postern backend for it", hash)
	}
	return h.authFetch(ctx, http.MethodGet, "/api/blobs/"+url.PathEscape(hash), nil)
}

// DeleteBlob implements application.Postern: DELETE /api/blobs/{hash}
// (postern's docs/protocol.md section 18), answered 204. A 404 is a blob
// already gone, and not an error. A hash that is not 64 hex characters is
// refused before any network call.
func (h *HTTP) DeleteBlob(ctx context.Context, hash string) error {
	if !posternBlobHash.MatchString(hash) {
		return fmt.Errorf("%q is not a sha256 hash, 64 hex characters: refusing to ask the postern backend to delete it", hash)
	}
	_, err := h.authFetch(ctx, http.MethodDelete, "/api/blobs/"+url.PathEscape(hash), nil)
	var status *statusError
	if errors.As(err, &status) && status.code == http.StatusNotFound {
		return nil
	}
	return err
}

// statusError is the backend answering outside 2xx: its status, and what it
// said.
type statusError struct {
	code int
	said string
}

func (e *statusError) Error() string { return e.said }

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
// empty.
func (h *HTTP) do(ctx context.Context, method, path string, body []byte, into any, authHeader string) error {
	raw, err := h.fetch(ctx, method, path, body, authHeader)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("the postern backend at %s answered %s %s with something that is not its JSON: %w", h.base, method, path, err)
	}
	return nil
}

// fetch makes one call to the backend and reports its raw body, carrying
// authHeader as the request's Authorization header when it is not empty. An
// answer outside 2xx is an error carrying the status and the backend's own
// {"error": ...} message: 201 is how a POST that stored something new
// answers (postern's docs/protocol.md sections 8 and 9).
func (h *HTTP) fetch(ctx context.Context, method, path string, body []byte, authHeader string) ([]byte, error) {
	return h.fetchAs(ctx, method, path, body, authHeader, "application/json")
}

// fetchAs is fetch with a body of contentType.
func (h *HTTP) fetchAs(ctx context.Context, method, path string, body []byte, authHeader, contentType string) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, h.base+path, reader)
	if err != nil {
		return nil, fmt.Errorf("asking the postern backend at %s: %w", h.base, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", contentType)
	}
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reaching the postern backend at %s: %w", h.base, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading the postern backend's answer to %s %s: %w", method, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var said struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &said) != nil || said.Error == "" {
			said.Error = strings.TrimSpace(string(raw))
		}
		return nil, &statusError{code: resp.StatusCode, said: fmt.Sprintf("the postern backend at %s said %d to %s %s: %s", h.base, resp.StatusCode, method, path, said.Error)}
	}
	return raw, nil
}

// Events implements application.PosternStream: GET /api/events, postern's
// docs/protocol.md section 10, read as server-sent events. A hello's head and
// a message's seq both come back as the event's Seq; a comment (the ping) and
// an event it does not know are skipped.
func (h *HTTP) Events(ctx context.Context, onEvent func(application.PosternEvent) error) error {
	_, header, err := h.authHeader(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.base+"/api/events", nil)
	if err != nil {
		return fmt.Errorf("asking the postern backend at %s for its event stream: %w", h.base, err)
	}
	req.Header.Set("Authorization", header)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := h.stream.Do(req)
	if err != nil {
		return fmt.Errorf("reaching the postern backend at %s: %w", h.base, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		said, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("the postern backend at %s said %d to GET /api/events: %s", h.base, resp.StatusCode, strings.TrimSpace(string(said)))
	}

	reader := bufio.NewReader(resp.Body)
	var kind, data string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("the postern backend's event stream at %s ended: %w", h.base, err)
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			event, ok := parseStreamEvent(kind, data)
			kind, data = "", ""
			if !ok {
				continue
			}
			if err := onEvent(event); err != nil {
				return err
			}
		case strings.HasPrefix(line, ":"):
		case strings.HasPrefix(line, "event:"):
			kind = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data += strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}
}

// parseStreamEvent reads one event of the stream: a hello or a message, the
// two TalkWait looks at. ok is false for any other, or one whose data is not
// the JSON the docs show.
func parseStreamEvent(kind, data string) (event application.PosternEvent, ok bool) {
	var body struct {
		Head int64 `json:"head"`
		Seq  int64 `json:"seq"`
	}
	if json.Unmarshal([]byte(data), &body) != nil {
		return application.PosternEvent{}, false
	}
	switch kind {
	case application.PosternEventHello:
		return application.PosternEvent{Kind: kind, Seq: body.Head}, true
	case application.PosternEventMessage:
		return application.PosternEvent{Kind: kind, Seq: body.Seq}, true
	}
	return application.PosternEvent{}, false
}
