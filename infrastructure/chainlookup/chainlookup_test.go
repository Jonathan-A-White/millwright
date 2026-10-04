package chainlookup_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/chainlookup"
)

func serve(t *testing.T, status int, body string) (*chainlookup.WhatsOnChain, *string) {
	t.Helper()
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return chainlookup.New(srv.URL), &path
}

func TestATransactionInABlockHasItsHeightAndTime(t *testing.T) {
	l, path := serve(t, 200, `{"txid":"abc","blockhash":"00ff","blockheight":1650123,"blocktime":1791105450,"confirmations":4}`)
	tx, err := l.Tx(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	if *path != "/tx/hash/abc" {
		t.Fatalf("asked %q", *path)
	}
	if !tx.Known || tx.Height != 1650123 || !tx.Time.Equal(time.Unix(1791105450, 0)) {
		t.Fatalf("tx = %+v", tx)
	}
}

func TestATransactionWithNoBlockIsInTheMempool(t *testing.T) {
	l, _ := serve(t, 200, `{"txid":"abc","size":250}`)
	tx, err := l.Tx(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	if !tx.Known || tx.Height != 0 || !tx.Time.IsZero() {
		t.Fatalf("tx = %+v", tx)
	}
}

func TestA404IsATransactionNotKnownYetNotAnError(t *testing.T) {
	l, _ := serve(t, 404, `Not Found`)
	tx, err := l.Tx(context.Background(), "abc")
	if err != nil || tx.Known {
		t.Fatalf("tx = %+v, err = %v", tx, err)
	}
}

func TestAServerErrorIsAnError(t *testing.T) {
	l, _ := serve(t, 500, `boom`)
	if _, err := l.Tx(context.Background(), "abc"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
}

func TestAnAnswerThatIsNotJSONIsAnError(t *testing.T) {
	l, _ := serve(t, 200, `<html>`)
	if _, err := l.Tx(context.Background(), "abc"); err == nil {
		t.Fatal("no error")
	}
}
