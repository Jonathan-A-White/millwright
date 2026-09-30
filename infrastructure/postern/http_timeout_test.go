package postern_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

// slowBackend stands in for a postern backend just restarted: the first
// slowFirst requests to path hang until the client gives up, the rest are
// answered with answerBody. Every request to path is counted.
func slowBackend(t *testing.T, method, path string, slowFirst int32, answerBody string) (*atomic.Int32, *postern.HTTP) {
	t.Helper()
	keys, _ := testKeyAndAddress(t)
	var hits, nonces atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/api/challenge" {
			fmt.Fprintf(w, `{"nonce":"nonce-%d"}`, nonces.Add(1))
			return
		}
		if r.Method == method && r.URL.Path == path {
			if hits.Add(1) <= slowFirst {
				<-r.Context().Done()
				return
			}
		}
		io.WriteString(w, answerBody)
	}))
	t.Cleanup(srv.Close)
	h := postern.NewHTTP(srv.URL, keys)
	h.SetTimeout(200 * time.Millisecond)
	return &hits, h
}

// A backend just restarted walks its licence chain on the first call: the
// GET times out, the walk finishes in the background, the second answers.
func TestMessagesRetriesAGetThatTimedOutOnce(t *testing.T) {
	hits, backend := slowBackend(t, http.MethodGet, "/api/messages", 1, `{"records":[{"seq":7,"txid":"abc"}]}`)

	records, err := backend.Messages(context.Background(), 0)
	if err != nil {
		t.Fatalf("expected the second try to answer, got: %v", err)
	}
	if len(records) != 1 || records[0].Seq != 7 {
		t.Fatalf("expected the one record the second try answered, got: %+v", records)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("expected the GET asked twice, got %d", got)
	}
}

func TestAGetThatTimesOutTwiceIsAnErrorNamingThePath(t *testing.T) {
	hits, backend := slowBackend(t, http.MethodGet, "/api/messages", 2, `{"records":[]}`)

	_, err := backend.Messages(context.Background(), 0)
	if err == nil || !strings.Contains(err.Error(), "/api/messages") || !strings.Contains(err.Error(), "Timeout") {
		t.Fatalf("expected the timeout error naming /api/messages, got: %v", err)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("expected the GET tried twice and no more, got %d", got)
	}
}

func TestAPostIsNeverRetriedOnATimeout(t *testing.T) {
	hits, backend := slowBackend(t, http.MethodPost, "/api/broadcast", 1, `{"txid":"abc"}`)
	if _, err := backend.Broadcast(context.Background(), "0100beef"); err == nil || !strings.Contains(err.Error(), "/api/broadcast") {
		t.Fatalf("expected the timeout error naming /api/broadcast, got: %v", err)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("expected the broadcast sent once, got %d", got)
	}

	hits, backend = slowBackend(t, http.MethodPost, "/api/messages", 1, `{"txid":"direct:abc","seq":1}`)
	if _, err := backend.Deliver(context.Background(), []byte(`{"v":1}`)); err == nil || !strings.Contains(err.Error(), "/api/messages") {
		t.Fatalf("expected the timeout error naming /api/messages, got: %v", err)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("expected the delivery sent once, got %d", got)
	}
}

// A caller that gave up is not the backend being cold: no second try.
func TestAGetIsNotRetriedOnceTheCallersContextIsDone(t *testing.T) {
	hits, backend := slowBackend(t, http.MethodGet, "/api/messages", 5, `{"records":[]}`)
	backend.SetTimeout(time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := backend.Messages(ctx, 0); err == nil {
		t.Fatal("expected an error once the caller's context ran out")
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("expected the GET asked once, got %d", got)
	}
}
