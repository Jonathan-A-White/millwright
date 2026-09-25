package postern_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

// backend stands in for the postern backend: each path answers with a
// canned status and body, per docs/api.md, and every request is kept.
type backend struct {
	answers  map[string]answer
	requests []*http.Request
	bodies   []string
}

type answer struct {
	status int
	body   string
}

func serve(t *testing.T, answers map[string]answer) (*backend, *postern.HTTP) {
	t.Helper()
	b := &backend{answers: answers}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		b.requests = append(b.requests, r)
		b.bodies = append(b.bodies, string(body))
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
	return b, postern.NewHTTP(srv.URL + "/")
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
	if b.bodies[0] != `{"rawtx":"0100beef"}` {
		t.Fatalf("expected the body {\"rawtx\":\"0100beef\"}, got %s", b.bodies[0])
	}
	if got := b.requests[0].Header.Get("Content-Type"); got != "application/json" {
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

	_, err := postern.NewHTTP(url).Balance(context.Background(), "mtAddr")
	if err == nil || !strings.Contains(err.Error(), url) {
		t.Fatalf("expected an error naming %s, got: %v", url, err)
	}
}
