package postern_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
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
// Authentication section: "Postern2 <pubkeyHex>:<nonceHex>:<sigHex>", the
// pubkey matching wantPubKeyHex, the nonce matching wantNonce, and the
// signature a valid DER-encoded ECDSA signature by that key over
// sha256 of the v2 message for the request that carried it: "postern-v2",
// the method, the request target as sent, hex(sha256(body)) and the nonce,
// joined by single line feeds.
func verifyPosternAuthorization(t *testing.T, req *http.Request, body string, wantPubKeyHex, wantNonce string) {
	t.Helper()
	header := req.Header.Get("Authorization")
	const scheme = "Postern2 "
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
	bodyHash := sha256.Sum256([]byte(body))
	message := strings.Join([]string{"postern-v2", req.Method, req.RequestURI, hex.EncodeToString(bodyHash[:]), nonce}, "\n")
	hash := sha256.Sum256([]byte(message))
	if !sig.Verify(hash[:], pubKey) {
		t.Fatalf("the signature does not verify by %s over sha256 of the v2 message %q", pubKeyHex, message)
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
	_, backend := serve(t, map[string]answer{"GET /api/messages?since=3&limit=200": {200, string(records)}})

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
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("record %d: expected %+v, got %+v", i, want[i], got[i])
		}
	}
}

// pageOf is one GET /api/messages answer: the records with these seqs, the
// cursor to ask from next, and whether more follow.
func pageOf(seqs []int64, next int64, more bool) answer {
	records := []map[string]any{}
	for _, seq := range seqs {
		records = append(records, map[string]any{"seq": seq, "txid": fmt.Sprintf("t%d", seq), "vout": 0, "scriptHex": "", "height": 0})
	}
	body, _ := json.Marshal(map[string]any{"records": records, "next": next, "more": more})
	return answer{200, string(body)}
}

func requestsFor(b *backend, path string) []string {
	var got []string
	for _, r := range b.requests {
		if r.URL.Path == path {
			got = append(got, r.URL.RequestURI())
		}
	}
	return got
}

// One Messages call drains the pages: each asks limit=200 from the previous
// page's next, until more is false, and the records come back in order.
func TestMessagesDrainsEveryPageInOneCall(t *testing.T) {
	b, backend := serve(t, map[string]answer{
		"GET /api/messages?since=0&limit=200": pageOf([]int64{1, 2}, 2, true),
		"GET /api/messages?since=2&limit=200": pageOf([]int64{3, 4}, 4, true),
		"GET /api/messages?since=4&limit=200": pageOf([]int64{5}, 5, false),
	})

	got, err := backend.Messages(context.Background(), 0)
	if err != nil {
		t.Fatalf("reading messages: %v", err)
	}
	var seqs []int64
	for _, r := range got {
		seqs = append(seqs, r.Seq)
	}
	if !reflect.DeepEqual(seqs, []int64{1, 2, 3, 4, 5}) {
		t.Fatalf("expected all five records in order, got %v", seqs)
	}
	want := []string{"/api/messages?since=0&limit=200", "/api/messages?since=2&limit=200", "/api/messages?since=4&limit=200"}
	if reqs := requestsFor(b, "/api/messages"); !reflect.DeepEqual(reqs, want) {
		t.Fatalf("expected three page requests %v, got %v", want, reqs)
	}
}

// A page whose records the backend filtered to none (an app key sees only its
// own) still moves the cursor on, to its next.
func TestMessagesAdvancesPastAPageFilteredToNothing(t *testing.T) {
	b, backend := serve(t, map[string]answer{
		"GET /api/messages?since=0&limit=200":   pageOf(nil, 200, true),
		"GET /api/messages?since=200&limit=200": pageOf([]int64{201}, 201, false),
	})

	got, err := backend.Messages(context.Background(), 0)
	if err != nil || len(got) != 1 || got[0].Seq != 201 {
		t.Fatalf("expected the one record past the empty page, got %+v, %v", got, err)
	}
	if reqs := requestsFor(b, "/api/messages"); len(reqs) != 2 {
		t.Fatalf("expected two page requests, got %v", reqs)
	}
}

// An old backend answers with no more at all: one page, as before.
func TestMessagesFromAnOldBackendIsOnePage(t *testing.T) {
	b, backend := serve(t, map[string]answer{
		"GET /api/messages?since=0&limit=200": {200, `{"records":[{"seq":1,"txid":"t1"},{"seq":2,"txid":"t2"}],"next":2}`},
	})

	got, err := backend.Messages(context.Background(), 0)
	if err != nil || len(got) != 2 {
		t.Fatalf("expected the two records of the one page, got %+v, %v", got, err)
	}
	if reqs := requestsFor(b, "/api/messages"); len(reqs) != 1 {
		t.Fatalf("expected one page request, got %v", reqs)
	}
}

// A backend that says more but does not move next would loop forever: stop.
func TestMessagesStopsWhenAPageDoesNotAdvance(t *testing.T) {
	b, backend := serve(t, map[string]answer{
		"GET /api/messages?since=7&limit=200": pageOf([]int64{8}, 7, true),
	})

	got, err := backend.Messages(context.Background(), 7)
	if err != nil || len(got) != 1 {
		t.Fatalf("expected the one record, got %+v, %v", got, err)
	}
	if reqs := requestsFor(b, "/api/messages"); len(reqs) != 1 {
		t.Fatalf("expected one page request, got %v", reqs)
	}
}

func TestMessagesWithNothingNewIsEmpty(t *testing.T) {
	_, backend := serve(t, map[string]answer{"GET /api/messages?since=0&limit=200": {200, `{"records":null,"next":0}`}})

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
	if len(utxos) != 1 || utxos[0] != (application.PosternUtxo{Txid: "3af1", Vout: 2, Satoshis: 1000, Height: 100}) {
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
	if !errors.Is(err, application.ErrPosternUnreachable) {
		t.Fatalf("expected the error to be ErrPosternUnreachable, got: %v", err)
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
	verifyPosternAuthorization(t, balanceReq, b.bodies[1], pubKeyHex, b.issuedNonces[0])
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
	verifyPosternAuthorization(t, b.requests[1], b.bodies[1], pubKeyHex, b.issuedNonces[0])
	verifyPosternAuthorization(t, b.requests[3], b.bodies[3], pubKeyHex, b.issuedNonces[1])
}

// A key that holds no licence is the backend's own 401 "no licence held"
// (postern's server/internal/api/handlers.go): mw reports it plainly, naming
// the key, rather than the backend's terse status and text.
func TestAnUnlicensedKeyIsReportedPlainly(t *testing.T) {
	keys, pubKeyHex := testKeyAndAddress(t)
	_, backend := serveWithKeys(t, map[string]answer{
		"GET /api/messages?since=0&limit=200": {401, `{"error":"no licence held"}`},
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
		"GET /api/messages?since=0&limit=200": {401, `{"error":"signature does not verify"}`},
	})

	_, err := backend.Messages(context.Background(), 0)
	if err == nil || !strings.Contains(err.Error(), "signature does not verify") || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected the backend's own message and status, got: %v", err)
	}
}

// Deliver posts a message's record script straight to the backend, postern's
// docs/protocol.md section 9: {scriptHex} of the very script a transaction
// would carry, signed like every call, and reports the direct: id the backend
// names it by — on a first delivery (201) and on a harmless retry (200) alike.
func TestDeliverPostsTheRecordScriptAndReportsItsDirectID(t *testing.T) {
	f := loadProtocolFixture(t)
	payload, ok := postern.DecodeRecordScript(f.RecordScriptHex)
	if !ok {
		t.Fatal("the fixture's record script does not decode")
	}
	for _, status := range []int{201, 200} {
		keys, pubKeyHex := testKeyAndAddress(t)
		b, backend := serveWithKeys(t, map[string]answer{"POST /api/messages": {status, `{"txid":"direct:ab12","seq":7}`}}, keys)

		txid, err := backend.Deliver(context.Background(), payload)
		if err != nil {
			t.Fatalf("delivering (answered %d): %v", status, err)
		}
		if txid != "direct:ab12" {
			t.Fatalf("expected the direct id, got %q", txid)
		}
		if len(b.requests) != 2 {
			t.Fatalf("expected a challenge then a signed POST, got %d requests", len(b.requests))
		}
		verifyPosternAuthorization(t, b.requests[1], b.bodies[1], pubKeyHex, b.issuedNonces[0])
		var body struct {
			ScriptHex string `json:"scriptHex"`
		}
		if err := json.Unmarshal([]byte(b.bodies[1]), &body); err != nil {
			t.Fatalf("the body is not JSON: %v", err)
		}
		if body.ScriptHex != f.RecordScriptHex {
			t.Fatalf("expected the fixture's record script\n%s\ngot\n%s", f.RecordScriptHex, body.ScriptHex)
		}
	}
}

func TestDeliverRefusesAnAnswerWithNoTxid(t *testing.T) {
	_, backend := serve(t, map[string]answer{"POST /api/messages": {201, `{"seq":7}`}})
	if _, err := backend.Deliver(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("expected an answer with no txid to be an error")
	}
}

// UploadBlob posts the ciphertext as it is, section 8, and reports the hash
// and size the backend stored it under — after checking the hash is the
// body's own sha256, so a message never announces a blob that is not the one
// sent.
func TestUploadBlobPostsTheBytesAndChecksTheHash(t *testing.T) {
	body := []byte{0x42, 0x42, 0x10, 0x33, 0x00, 0xff}
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	b, backend := serve(t, map[string]answer{"POST /api/blobs": {201, fmt.Sprintf(`{"hash":%q,"size":6}`, hash)}})

	gotHash, size, err := backend.UploadBlob(context.Background(), body)
	if err != nil {
		t.Fatalf("uploading: %v", err)
	}
	if gotHash != hash || size != 6 {
		t.Fatalf("expected %s and 6 bytes, got %s and %d", hash, gotHash, size)
	}
	if b.bodies[1] != string(body) {
		t.Fatalf("expected the raw bytes as the body, got %x", b.bodies[1])
	}
	if got := b.requests[1].Header.Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("expected an octet-stream body, got %q", got)
	}

	_, liar := serve(t, map[string]answer{"POST /api/blobs": {200, `{"hash":"` + strings.Repeat("0", 64) + `","size":6}`}})
	if _, _, err := liar.UploadBlob(context.Background(), body); err == nil || !strings.Contains(err.Error(), "hash") {
		t.Fatalf("expected a hash that is not the body's to be refused, got %v", err)
	}
}

// A record's signer, when the backend supplies it — a transaction's signing
// key, or the key that authenticated a direct delivery — is read, so the
// sender is checked against it.
func TestMessagesReadsTheSignerTheBackendSupplies(t *testing.T) {
	f := loadProtocolFixture(t)
	records, _ := json.Marshal(map[string]any{
		"records": []map[string]any{{"seq": 1, "txid": "direct:ab", "vout": 0, "scriptHex": f.RecordScriptHex,
			"height": 0, "signer": f.EncryptMessage.From}},
	})
	_, backend := serve(t, map[string]answer{"GET /api/messages?since=0&limit=200": {200, string(records)}})

	got, err := backend.Messages(context.Background(), 0)
	if err != nil || len(got) != 1 {
		t.Fatalf("reading messages: %+v %v", got, err)
	}
	if got[0].Signer != f.EncryptMessage.From || got[0].Txid != "direct:ab" {
		t.Fatalf("expected the signer %s on the direct record, got %+v", f.EncryptMessage.From, got[0])
	}
}

// The apps a direct record's signer holds licences for, stamped by the
// backend (postern's docs/protocol.md section 18), are read with the record.
func TestMessagesReadsTheSignerApps(t *testing.T) {
	f := loadProtocolFixture(t)
	records, _ := json.Marshal(map[string]any{
		"records": []map[string]any{{"seq": 1, "txid": "direct:ab", "scriptHex": f.RecordScriptHex,
			"signer": f.EncryptMessage.From, "signer_apps": []string{"cairn"}}},
	})
	_, backend := serve(t, map[string]answer{"GET /api/messages?since=0&limit=200": {200, string(records)}})
	got, err := backend.Messages(context.Background(), 0)
	if err != nil || len(got) != 1 || strings.Join(got[0].SignerApps, ",") != "cairn" {
		t.Fatalf("expected the record's signer apps [cairn], got %+v %v", got, err)
	}
}

// DeleteBlob asks the backend, proven, to delete a blob; one already gone is
// not an error, anything else the backend refuses is.
func TestDeleteBlobDeletesAndAGoneBlobIsNoError(t *testing.T) {
	hash := strings.Repeat("ab", 32)
	b, backend := serve(t, map[string]answer{"DELETE /api/blobs/" + hash: {204, ""}})
	if err := backend.DeleteBlob(context.Background(), hash); err != nil {
		t.Fatalf("deleting a blob: %v", err)
	}
	last := b.requests[len(b.requests)-1]
	if last.Method != http.MethodDelete || !strings.HasPrefix(last.Header.Get("Authorization"), "Postern2 ") {
		t.Fatalf("expected a proven DELETE, got %s %q", last.Method, last.Header.Get("Authorization"))
	}

	_, gone := serve(t, map[string]answer{})
	if err := gone.DeleteBlob(context.Background(), hash); err != nil {
		t.Fatalf("expected a blob already gone to be no error, got %v", err)
	}
	_, refusing := serve(t, map[string]answer{"DELETE /api/blobs/" + hash: {403, `{"error":"not the mill"}`}})
	if err := refusing.DeleteBlob(context.Background(), hash); err == nil || !strings.Contains(err.Error(), "not the mill") {
		t.Fatalf("expected the backend's refusal, got %v", err)
	}
	if err := refusing.DeleteBlob(context.Background(), "../etc"); err == nil {
		t.Fatal("expected a hash that is not one refused")
	}
}

func TestMeReportsTheCallerAndTheMill(t *testing.T) {
	b, backend := serve(t, map[string]answer{"GET /api/me": {200, `{"pubkey":"02aa","mill":"03bb","network":"testnet","features":["grist"],"apps":["cairn"]}`}})

	me, err := backend.Me(context.Background())
	if err != nil {
		t.Fatalf("reading /api/me: %v", err)
	}
	if want := (application.PosternMe{Pubkey: "02aa", Mill: "03bb", Network: "testnet"}); me != want {
		t.Fatalf("expected %+v, got %+v", want, me)
	}
	if len(b.requests) != 2 || !strings.HasPrefix(b.requests[1].Header.Get("Authorization"), "Postern2 ") {
		t.Fatalf("expected a challenge then a signed GET, got %d requests", len(b.requests))
	}
}

func TestMeWithNoMillNamesNone(t *testing.T) {
	_, backend := serve(t, map[string]answer{"GET /api/me": {200, `{"pubkey":"02aa","mayor":"","network":"testnet","features":["direct"]}`}})

	me, err := backend.Me(context.Background())
	if err != nil || me.Mill != "" {
		t.Fatalf("expected no mill key, got %+v: %v", me, err)
	}
}

// The prompts are kept by the backend: Put sends the prompt whole, proven, to
// its own path; Get reads one back and takes a 404 as none; List reads them
// all; Delete takes a 404 as already gone.
func TestPromptsAreSavedReadAndDeletedThroughTheBackend(t *testing.T) {
	prompt := domain.Prompt{Name: "top5", Summary: "The five next", Signature: []string{"count:int=5"}, Body: "Top <count>."}
	// What the backend stores and answers (postern's server/README.md, 'Saved
	// prompts'): the signature is a list of option objects, the stamps are its own.
	saved := `{"name":"top5","summary":"The five next","signature":[{"flag":"--count","type":"int","default":"5"}],"body":"Top <count>.","updatedAt":"2026-10-01T12:00:00Z","updatedBy":"02aa"}`
	b, backend := serve(t, map[string]answer{
		"PUT /api/prompts/top5":    {200, saved},
		"GET /api/prompts/top5":    {200, saved},
		"GET /api/prompts":         {200, `[` + saved + `]`},
		"DELETE /api/prompts/top5": {204, ""},
	})
	ctx := context.Background()
	if err := backend.Put(ctx, prompt); err != nil {
		t.Fatalf("saving a prompt: %v", err)
	}
	put := b.requests[len(b.requests)-1]
	if put.Method != http.MethodPut || !strings.HasPrefix(put.Header.Get("Authorization"), "Postern2 ") {
		t.Fatalf("expected a proven PUT, got %s %q", put.Method, put.Header.Get("Authorization"))
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(b.bodies[len(b.bodies)-1]), &sent); err != nil {
		t.Fatalf("the PUT body is not JSON: %v", err)
	}
	for _, field := range []string{"name", "summary", "signature", "body"} {
		if _, ok := sent[field]; !ok {
			t.Fatalf("expected the PUT body to carry %q, got %v", field, sent)
		}
	}
	wantSignature := []any{map[string]any{"flag": "--count", "type": "int", "default": "5"}}
	if !reflect.DeepEqual(sent["signature"], wantSignature) {
		t.Fatalf("expected the signature as option objects %v, got %v", wantSignature, sent["signature"])
	}
	got, found, err := backend.Get(ctx, "top5")
	if err != nil || !found || !reflect.DeepEqual(got, prompt) {
		t.Fatalf("expected the saved prompt back, got %+v found %v err %v", got, found, err)
	}
	if _, found, err := backend.Get(ctx, "nope"); err != nil || found {
		t.Fatalf("expected a 404 to be no prompt and no error, got found %v err %v", found, err)
	}
	list, err := backend.List(ctx)
	if err != nil || len(list) != 1 || list[0].Name != "top5" {
		t.Fatalf("expected the one prompt listed, got %+v err %v", list, err)
	}
	if err := backend.Delete(ctx, "top5"); err != nil {
		t.Fatalf("deleting a prompt: %v", err)
	}
	if err := backend.Delete(ctx, "nope"); err != nil {
		t.Fatalf("expected a prompt already gone to be no error, got %v", err)
	}
	_, refusing := serve(t, map[string]answer{"PUT /api/prompts/top5": {403, `{"error":"not the mayor"}`}})
	if err := refusing.Put(ctx, prompt); err == nil || !strings.Contains(err.Error(), "not the mayor") {
		t.Fatalf("expected the backend's refusal, got %v", err)
	}
}

// GET /api/prompts answers a bare JSON array, [] when none are saved
// (postern's server/README.md): List reads one prompt and none.
func TestPromptListReadsTheBackendsBareArray(t *testing.T) {
	one := `[{"name":"sweep","summary":"Sweep a place","signature":[{"flag":"--duration","type":"duration","default":"30m"},{"flag":"--who","type":"string","required":true,"help":"whom"}],"body":"Sweep <who>.","updatedAt":"2026-10-01T12:00:00Z","updatedBy":"02aa"}]`
	_, backend := serve(t, map[string]answer{"GET /api/prompts": {200, one}})
	list, err := backend.List(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("expected the one prompt listed, got %+v err %v", list, err)
	}
	want := domain.Prompt{Name: "sweep", Summary: "Sweep a place", Signature: []string{"duration:duration=30m", "who:string:required"}, Body: "Sweep <who>."}
	if !reflect.DeepEqual(list[0], want) {
		t.Fatalf("expected %+v, got %+v", want, list[0])
	}
	if _, err := list[0].Options(); err != nil {
		t.Fatalf("expected the listed signature to read, got %v", err)
	}

	_, empty := serve(t, map[string]answer{"GET /api/prompts": {200, `[]`}})
	list, err = empty.List(context.Background())
	if err != nil || len(list) != 0 {
		t.Fatalf("expected no prompts and no error, got %+v err %v", list, err)
	}
}

// The v2 message covers the request's own target, query included, and the
// body's exact bytes (docs/api.md): a GET with a query is signed with it, and
// a GET with no body hashes the empty string.
func TestAGetWithAQueryAndNoBodyIsSignedOverItsTargetAndTheEmptyBody(t *testing.T) {
	keys, pubKeyHex := testKeyAndAddress(t)
	b, backend := serveWithKeys(t, map[string]answer{
		"GET /api/messages?since=3&limit=200": {200, `{"records":[],"next":3}`},
	}, keys)

	if _, err := backend.Messages(context.Background(), 3); err != nil {
		t.Fatalf("reading the messages: %v", err)
	}

	if len(b.requests) != 2 || b.requests[1].RequestURI != "/api/messages?since=3&limit=200" {
		t.Fatalf("expected a challenge then GET /api/messages?since=3&limit=200, got %d requests", len(b.requests))
	}
	verifyPosternAuthorization(t, b.requests[1], "", pubKeyHex, b.issuedNonces[0])
	if got := b.requests[1].Header.Get("Authorization"); !strings.HasPrefix(got, "Postern2 ") {
		t.Fatalf("expected the Postern2 scheme, got %q", got)
	}
}

// A percent-encoding in the target is signed as it goes on the request line.
func TestATargetIsSignedAsItIsWrittenOnTheRequestLine(t *testing.T) {
	keys, pubKeyHex := testKeyAndAddress(t)
	b, backend := serveWithKeys(t, map[string]answer{
		"GET /api/prompts":              {200, `[]`},
		"DELETE /api/prompts/a%20b%2Fc": {204, ``},
	}, keys)

	if err := backend.Delete(context.Background(), "a b/c"); err != nil {
		t.Fatalf("deleting the prompt: %v", err)
	}

	last := b.requests[len(b.requests)-1]
	if last.RequestURI != "/api/prompts/a%20b%2Fc" {
		t.Fatalf("expected the encoded target on the request line, got %q", last.RequestURI)
	}
	verifyPosternAuthorization(t, last, "", pubKeyHex, b.issuedNonces[0])
}

// nonceBackend stands in for a backend that refuses the first refuseFirst
// signed requests with 401 reason "nonce" (a restart, or the standby's
// challenge reaching the home) and answers the rest 200 with {}.
func nonceBackend(t *testing.T, refuseFirst int) (*backend, *postern.HTTP) {
	t.Helper()
	keys, _ := testKeyAndAddress(t)
	b := &backend{}
	var signed int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		b.requests = append(b.requests, r)
		b.bodies = append(b.bodies, string(body))
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/api/challenge" {
			nonce := fmt.Sprintf("nonce-%d", len(b.issuedNonces)+1)
			b.issuedNonces = append(b.issuedNonces, nonce)
			fmt.Fprintf(w, `{"nonce":%q}`, nonce)
			return
		}
		signed++
		if signed <= refuseFirst {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"error":"nonce unknown, expired or used","reason":"nonce"}`)
			return
		}
		io.WriteString(w, `{"txid":"direct:ab12","seq":1}`)
	}))
	t.Cleanup(srv.Close)
	return b, postern.NewHTTP(srv.URL, keys)
}

// A nonce refusal acted on nothing, so the call is tried once more from a
// fresh challenge, whatever its method.
func TestANonceRefusalIsRetriedOnceFromAFreshChallenge(t *testing.T) {
	for _, call := range []struct {
		name string
		do   func(*postern.HTTP) error
	}{
		{"a POST", func(h *postern.HTTP) error { _, err := h.Deliver(context.Background(), []byte(`{"v":1}`)); return err }},
		{"a GET", func(h *postern.HTTP) error { _, err := h.Messages(context.Background(), 0); return err }},
	} {
		b, backend := nonceBackend(t, 1)

		if err := call.do(backend); err != nil {
			t.Fatalf("%s: expected the second try to answer, got: %v", call.name, err)
		}
		if len(b.requests) != 4 || len(b.issuedNonces) != 2 {
			t.Fatalf("%s: expected two challenges and two signed requests, got %d requests and %d nonces", call.name, len(b.requests), len(b.issuedNonces))
		}
		for i, wantNonce := range b.issuedNonces {
			if got := b.requests[2*i+1].Header.Get("Authorization"); !strings.Contains(got, ":"+wantNonce+":") {
				t.Fatalf("%s: expected try %d to carry %s, got %q", call.name, i+1, wantNonce, got)
			}
		}
	}
}

func TestASecondNonceRefusalIsTheError(t *testing.T) {
	b, backend := nonceBackend(t, 2)

	_, err := backend.Deliver(context.Background(), []byte(`{"v":1}`))
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "nonce unknown") {
		t.Fatalf("expected the backend's second nonce refusal, got: %v", err)
	}
	if len(b.requests) != 4 {
		t.Fatalf("expected two challenges and two signed requests and no more, got %d requests", len(b.requests))
	}
}

// Only the reason "nonce" is retried: a bad signature is the same bad
// signature the next time.
func TestAnotherUnauthorizedReasonIsNotRetried(t *testing.T) {
	keys, _ := testKeyAndAddress(t)
	b, backend := serveWithKeys(t, map[string]answer{
		"GET /api/messages?since=0&limit=200": {401, `{"error":"signature does not verify","reason":"signature"}`},
	}, keys)

	if _, err := backend.Messages(context.Background(), 0); err == nil {
		t.Fatal("expected the refusal to be returned")
	}
	if len(b.requests) != 2 {
		t.Fatalf("expected one challenge and one signed request, got %d requests", len(b.requests))
	}
}
