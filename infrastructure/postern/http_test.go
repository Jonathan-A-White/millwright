package postern_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

// backend stands in for the postern backend: each path answers with a
// canned status and body, per docs/api.md, and every request is kept.
// GET /api/challenge is answered automatically, with a fresh nonce each
// time (docs/api.md: a nonce is consumed the moment it is presented), never
// from answers.
type backend struct {
	answers      map[string]answer
	requests     []*http.Request
	bodies       []string
	issuedNonces []string
}

type answer struct {
	status int
	body   string
}

// testKeyAndAddress is a postern key file, in a fresh temp dir, and its
// compressed public key, hex.
func testKeyAndAddress(t *testing.T) (*postern.KeyFile, string) {
	t.Helper()
	keys := postern.New(t.TempDir() + "/postern.key")
	if err := keys.Generate(); err != nil {
		t.Fatalf("generating a postern key: %v", err)
	}
	pubKeyHex, _, err := keys.PublicKey()
	if err != nil {
		t.Fatalf("reading the postern key's public key: %v", err)
	}
	return keys, pubKeyHex
}

func serve(t *testing.T, answers map[string]answer) (*backend, *postern.HTTP) {
	t.Helper()
	keys, _ := testKeyAndAddress(t)
	return serveWithKeys(t, answers, keys)
}

func serveWithKeys(t *testing.T, answers map[string]answer, keys *postern.KeyFile) (*backend, *postern.HTTP) {
	t.Helper()
	b := &backend{answers: answers}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		b.requests = append(b.requests, r)
		b.bodies = append(b.bodies, string(body))
		if r.Method == http.MethodGet && r.URL.Path == "/api/challenge" {
			nonce := fmt.Sprintf("nonce-%d", len(b.issuedNonces)+1)
			b.issuedNonces = append(b.issuedNonces, nonce)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"nonce":%q}`, nonce)
			return
		}
		a, ok := b.answers[r.Method+" "+r.URL.RequestURI()]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":"no such route"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(a.status)
		io.WriteString(w, a.body)
	}))
	t.Cleanup(srv.Close)
	return b, postern.NewHTTP(srv.URL+"/", keys)
}

// verifyPosternAuthorization checks header against docs/api.md's
// Authentication section: "Postern <pubkeyHex>:<nonceHex>:<sigHex>", the
// pubkey matching wantPubKeyHex, the nonce matching wantNonce, and the
// signature a valid DER-encoded ECDSA signature by that key over
// sha256(nonce).
func verifyPosternAuthorization(t *testing.T, header, wantPubKeyHex, wantNonce string) {
	t.Helper()
	const scheme = "Postern "
	if !strings.HasPrefix(header, scheme) {
		t.Fatalf("expected an Authorization header starting %q, got %q", scheme, header)
	}
	parts := strings.Split(strings.TrimPrefix(header, scheme), ":")
	if len(parts) != 3 {
		t.Fatalf("expected pubkey:nonce:sig, got %q", header)
	}
	pubKeyHex, nonce, sigHex := parts[0], parts[1], parts[2]
	if pubKeyHex != wantPubKeyHex {
		t.Fatalf("expected the public key %s, got %s", wantPubKeyHex, pubKeyHex)
	}
	if nonce != wantNonce {
		t.Fatalf("expected the nonce %s, got %s", wantNonce, nonce)
	}
	sigBytes, err := hex.DecodeString(sigHex)
	if err != nil {
		t.Fatalf("the signature %q is not hex: %v", sigHex, err)
	}
	sig, err := ec.ParseDERSignature(sigBytes)
	if err != nil {
		t.Fatalf("the signature is not valid DER: %v", err)
	}
	pubKey, err := ec.PublicKeyFromString(pubKeyHex)
	if err != nil {
		t.Fatalf("parsing the public key: %v", err)
	}
	hash := sha256.Sum256([]byte(nonce))
	if !sig.Verify(hash[:], pubKey) {
		t.Fatalf("the signature does not verify over sha256(%q) by %s", nonce, pubKeyHex)
	}
}

func TestMessagesReadsTheRecordsSinceTheCursorFromTheirScripts(t *testing.T) {
	f := loadProtocolFixture(t)
	records, _ := json.Marshal(map[string]any{
		"records": []map[string]any{
			{"seq": 4, "txid": "t4", "vout": 0, "scriptHex": f.RecordScriptHex, "height": 0, "firstSeen": "2026-09-24T12:00:03.512Z"},
			{"seq": 5, "txid": "t5", "vout": 0, "scriptHex": "006a076e667467617465010102" + "7b7d", "height": 0, "firstSeen": "2026-09-24T12:00:04Z",
				"payload": map[string]any{}},
		},
		"next": 5,
	})
	_, backend := serve(t, map[string]answer{"GET /api/messages?since=3": {200, string(records)}})

	got, err := backend.Messages(context.Background(), 3)
	if err != nil {
		t.Fatalf("reading messages: %v", err)
	}
	want := []application.PosternRecord{
		{Seq: 4, Txid: "t4", Class: f.EncryptMessage.Class, From: f.EncryptMessage.From, To: f.EncryptMessage.To, Ts: time.Unix(f.EncryptMessage.Ts, 0).UTC(), Ciphertext: f.EncryptMessage.Ct},
		{Seq: 5, Txid: "t5"},
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d records, got %d: %+v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("record %d: expected %+v, got %+v", i, want[i], got[i])
		}
	}
}

func TestMessagesWithNothingNewIsEmpty(t *testing.T) {
	_, backend := serve(t, map[string]answer{"GET /api/messages?since=0": {200, `{"records":null,"next":0}`}})

	got, err := backend.Messages(context.Background(), 0)
	if err != nil || len(got) != 0 {
		t.Fatalf("expected no records and no error, got %+v, %v", got, err)
	}
}

func TestUtxosAndBalanceReadTheAddress(t *testing.T) {
	_, backend := serve(t, map[string]answer{
		"GET /api/utxos/mtAddr":   {200, `{"utxos":[{"txid":"3af1","vout":2,"satoshis":1000,"height":100}]}`},
		"GET /api/balance/mtAddr": {200, `{"confirmed":100000,"unconfirmed":-250}`},
	})

	utxos, err := backend.Utxos(context.Background(), "mtAddr")
	if err != nil {
		t.Fatalf("reading utxos: %v", err)
	}
	if len(utxos) != 1 || utxos[0] != (application.PosternUtxo{Txid: "3af1", Vout: 2, Satoshis: 1000}) {
		t.Fatalf("expected the one utxo, got %+v", utxos)
	}
	balance, err := backend.Balance(context.Background(), "mtAddr")
	if err != nil {
		t.Fatalf("reading the balance: %v", err)
	}
	if balance != 99750 {
		t.Fatalf("expected confirmed and unconfirmed together, 99750, got %d", balance)
	}
}

func TestBroadcastPostsTheRawTransaction(t *testing.T) {
	b, backend := serve(t, map[string]answer{"POST /api/broadcast": {200, `{"txid":"3af1"}`}})

	txid, err := backend.Broadcast(context.Background(), "0100beef")
	if err != nil {
		t.Fatalf("broadcasting: %v", err)
	}
	if txid != "3af1" {
		t.Fatalf("expected the txid 3af1, got %q", txid)
	}
	if len(b.requests) != 2 {
		t.Fatalf("expected a challenge request then the broadcast, got %d requests", len(b.requests))
	}
	if b.bodies[1] != `{"rawtx":"0100beef"}` {
		t.Fatalf("expected the body {\"rawtx\":\"0100beef\"}, got %s", b.bodies[1])
	}
	if got := b.requests[1].Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected a JSON body, got Content-Type %q", got)
	}
}

func TestTheBackendsOwnErrorIsWhatIsReported(t *testing.T) {
	_, backend := serve(t, map[string]answer{
		"POST /api/broadcast": {502, `{"error":"WhatsOnChain said 400: tx rejected: bad-txns-inputs-missingorspent"}`},
	})

	_, err := backend.Broadcast(context.Background(), "0100beef")
	if err == nil || !strings.Contains(err.Error(), "bad-txns-inputs-missingorspent") || !strings.Contains(err.Error(), "502") {
		t.Fatalf("expected the backend's status and error, got: %v", err)
	}
}

func TestAnUnreachableBackendIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	keys, _ := testKeyAndAddress(t)
	_, err := postern.NewHTTP(url, keys).Balance(context.Background(), "mtAddr")
	if err == nil || !strings.Contains(err.Error(), url) {
		t.Fatalf("expected an error naming %s, got: %v", url, err)
	}
}

// Every call the postern backend gates behind a licence proves it: mw asks
// for a fresh challenge and signs it with the postern key, docs/api.md's
// Authentication section.
func TestEveryCallSignsAFreshChallengeWithThePosternKey(t *testing.T) {
	keys, pubKeyHex := testKeyAndAddress(t)
	b, backend := serveWithKeys(t, map[string]answer{
		"GET /api/balance/mtAddr": {200, `{"confirmed":100000,"unconfirmed":0}`},
	}, keys)

	if _, err := backend.Balance(context.Background(), "mtAddr"); err != nil {
		t.Fatalf("reading the balance: %v", err)
	}

	if len(b.requests) != 2 {
		t.Fatalf("expected a challenge request then the balance request, got %d", len(b.requests))
	}
	challengeReq, balanceReq := b.requests[0], b.requests[1]
	if challengeReq.Method != http.MethodGet || challengeReq.URL.Path != "/api/challenge" {
		t.Fatalf("expected the first request to be GET /api/challenge, got %s %s", challengeReq.Method, challengeReq.URL.Path)
	}
	if got := challengeReq.Header.Get("Authorization"); got != "" {
		t.Fatalf("expected /api/challenge to carry no Authorization header, got %q", got)
	}
	verifyPosternAuthorization(t, balanceReq.Header.Get("Authorization"), pubKeyHex, b.issuedNonces[0])
}

// A nonce is consumed the moment it is presented (docs/api.md), so two calls
// must each fetch and sign their own.
func TestEachCallFetchesAndSignsItsOwnChallenge(t *testing.T) {
	keys, pubKeyHex := testKeyAndAddress(t)
	b, backend := serveWithKeys(t, map[string]answer{
		"GET /api/balance/mtAddr": {200, `{"confirmed":1,"unconfirmed":0}`},
	}, keys)

	if _, err := backend.Balance(context.Background(), "mtAddr"); err != nil {
		t.Fatalf("reading the balance (1st): %v", err)
	}
	if _, err := backend.Balance(context.Background(), "mtAddr"); err != nil {
		t.Fatalf("reading the balance (2nd): %v", err)
	}

	if len(b.issuedNonces) != 2 || b.issuedNonces[0] == b.issuedNonces[1] {
		t.Fatalf("expected two distinct issued nonces, got %v", b.issuedNonces)
	}
	verifyPosternAuthorization(t, b.requests[1].Header.Get("Authorization"), pubKeyHex, b.issuedNonces[0])
	verifyPosternAuthorization(t, b.requests[3].Header.Get("Authorization"), pubKeyHex, b.issuedNonces[1])
}

// A key that holds no licence is the backend's own 401 "no licence held"
// (postern's server/internal/api/handlers.go): mw reports it plainly, naming
// the key, rather than the backend's terse status and text.
func TestAnUnlicensedKeyIsReportedPlainly(t *testing.T) {
	keys, pubKeyHex := testKeyAndAddress(t)
	_, backend := serveWithKeys(t, map[string]answer{
		"GET /api/messages?since=0": {401, `{"error":"no licence held"}`},
	}, keys)

	_, err := backend.Messages(context.Background(), 0)
	if err == nil {
		t.Fatal("expected an error for an unlicensed key")
	}
	if !strings.Contains(err.Error(), pubKeyHex) {
		t.Fatalf("expected the error to name the key %s, got: %v", pubKeyHex, err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "no licence") {
		t.Fatalf("expected the error to say plainly that it holds no licence, got: %v", err)
	}
}

// A 401 for any other reason (a bad signature, an expired nonce) is left as
// the backend's own message: only "no licence held" is rewritten plainly.
func TestAnyOtherUnauthorizedIsLeftAsTheBackendSaidIt(t *testing.T) {
	_, backend := serve(t, map[string]answer{
		"GET /api/messages?since=0": {401, `{"error":"signature does not verify"}`},
	})

	_, err := backend.Messages(context.Background(), 0)
	if err == nil || !strings.Contains(err.Error(), "signature does not verify") || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected the backend's own message and status, got: %v", err)
	}
}
