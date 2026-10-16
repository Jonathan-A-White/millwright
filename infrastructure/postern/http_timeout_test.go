package postern_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

// slowBackend stands in for a postern backend just restarted: the first
// slowFirst requests to path hang until the client gives up, the rest are
// answered with answerBody. Every request to path is counted.
//
// The hang is the client's: those requests never leave it, and fail at once
// with the error its Timeout running out gives. So the client keeps its own
// timeout, a minute, for the challenge and every answer, and no answer races a
// short one on a loaded host (mw-gq6.440).
func slowBackend(t *testing.T, method, path string, slowFirst int32, answerBody string) (*coldNetwork, *postern.HTTP) {
	t.Helper()
	return laggingSlowBackend(t, 0, method, path, slowFirst, answerBody)
}

// laggingSlowBackend is slowBackend on a loaded host: every answer it does
// give, the challenge's included, comes lag late.
func laggingSlowBackend(t *testing.T, lag time.Duration, method, path string, slowFirst int32, answerBody string) (*coldNetwork, *postern.HTTP) {
	t.Helper()
	keys, _ := testKeyAndAddress(t)
	var nonces atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		time.Sleep(lag)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/api/challenge" {
			fmt.Fprintf(w, `{"nonce":"nonce-%d"}`, nonces.Add(1))
			return
		}
		io.WriteString(w, answerBody)
	}))
	t.Cleanup(srv.Close)
	timedOut := clientTimeout(t)
	cold := &coldNetwork{method: method, path: path, slowFirst: slowFirst, next: srv.Client().Transport,
		hang: func(*http.Request) error { return timedOut }}
	h := postern.NewHTTP(srv.URL, keys)
	h.SetTransport(cold)
	return cold, h
}

// coldNetwork is the way to a backend just restarted: the first slowFirst
// requests to method and path meet hang, every other goes on to next. Every
// request to method and path is counted in hits.
type coldNetwork struct {
	method, path string
	slowFirst    int32
	hits         atomic.Int32
	next         http.RoundTripper
	hang         func(*http.Request) error
}

func (c *coldNetwork) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == c.method && r.URL.Path == c.path && c.hits.Add(1) <= c.slowFirst {
		if r.Body != nil {
			r.Body.Close()
		}
		return nil, c.hang(r)
	}
	return c.next.RoundTrip(r)
}

// clientTimeout is the error an http.Client's own Timeout running out puts in
// the url.Error it returns: taken from a real client, a millisecond's timeout,
// asking a server that never answers. Nothing else is asked under that
// timeout, so no load can make it anything but a timeout.
func clientTimeout(t *testing.T) error {
	t.Helper()
	never := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-never:
		}
	}))
	defer srv.Close()
	defer close(never)
	_, err := (&http.Client{Timeout: time.Millisecond}).Get(srv.URL)
	var urlErr *url.Error
	if !errors.As(err, &urlErr) || !urlErr.Timeout() {
		t.Fatalf("expected a client timeout from a server that never answers, got: %v", err)
	}
	return urlErr.Err
}

// A backend just restarted walks its licence chain on the first call: the
// GET times out, the walk finishes in the background, the second answers.
func TestMessagesRetriesAGetThatTimedOutOnce(t *testing.T) {
	cold, backend := slowBackend(t, http.MethodGet, "/api/messages", 1, `{"records":[{"seq":7,"txid":"abc"}]}`)

	records, err := backend.Messages(context.Background(), 0)
	if err != nil {
		t.Fatalf("expected the second try to answer, got: %v", err)
	}
	if len(records) != 1 || records[0].Seq != 7 {
		t.Fatalf("expected the one record the second try answered, got: %+v", records)
	}
	if got := cold.hits.Load(); got != 2 {
		t.Fatalf("expected the GET asked twice, got %d", got)
	}
}

func TestAGetThatTimesOutTwiceIsAnErrorNamingThePath(t *testing.T) {
	cold, backend := slowBackend(t, http.MethodGet, "/api/messages", 2, `{"records":[]}`)

	_, err := backend.Messages(context.Background(), 0)
	if err == nil || !strings.Contains(err.Error(), "/api/messages") || !strings.Contains(err.Error(), "Timeout") {
		t.Fatalf("expected the timeout error naming /api/messages, got: %v", err)
	}
	if got := cold.hits.Load(); got != 2 {
		t.Fatalf("expected the GET tried twice and no more, got %d", got)
	}
}

// A loaded host answers every call late, the challenge included: only the call
// that hangs times out, so the GET is still asked twice (mw-gq6.440: the
// laptop at load 57 of 20 saw it asked once).
func TestAGetThatTimesOutTwiceIsAnErrorNamingThePathThoughEveryAnswerIsLate(t *testing.T) {
	cold, backend := laggingSlowBackend(t, 300*time.Millisecond, http.MethodGet, "/api/messages", 2, `{"records":[]}`)

	_, err := backend.Messages(context.Background(), 0)
	if err == nil || !strings.Contains(err.Error(), "/api/messages") || !strings.Contains(err.Error(), "Timeout") {
		t.Fatalf("expected the timeout error naming /api/messages, got: %v", err)
	}
	if got := cold.hits.Load(); got != 2 {
		t.Fatalf("expected the GET tried twice and no more, got %d", got)
	}
}

func TestAPostIsNeverRetriedOnATimeout(t *testing.T) {
	cold, backend := slowBackend(t, http.MethodPost, "/api/broadcast", 1, `{"txid":"abc"}`)
	if _, err := backend.Broadcast(context.Background(), "0100beef"); err == nil || !strings.Contains(err.Error(), "/api/broadcast") {
		t.Fatalf("expected the timeout error naming /api/broadcast, got: %v", err)
	}
	if got := cold.hits.Load(); got != 1 {
		t.Fatalf("expected the broadcast sent once, got %d", got)
	}

	cold, backend = slowBackend(t, http.MethodPost, "/api/messages", 1, `{"txid":"direct:abc","seq":1}`)
	if _, err := backend.Deliver(context.Background(), []byte(`{"v":1}`)); err == nil || !strings.Contains(err.Error(), "/api/messages") {
		t.Fatalf("expected the timeout error naming /api/messages, got: %v", err)
	}
	if got := cold.hits.Load(); got != 1 {
		t.Fatalf("expected the delivery sent once, got %d", got)
	}
}

// A caller that gave up is not the backend being cold: no second try.
func TestAGetIsNotRetriedOnceTheCallersContextIsDone(t *testing.T) {
	cold, backend := slowBackend(t, http.MethodGet, "/api/messages", 5, `{"records":[]}`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The caller gives up while its GET hangs, whenever that comes.
	cold.hang = func(r *http.Request) error {
		cancel()
		<-r.Context().Done()
		return r.Context().Err()
	}

	if _, err := backend.Messages(ctx, 0); err == nil {
		t.Fatal("expected an error once the caller's context ran out")
	}
	if got := cold.hits.Load(); got != 1 {
		t.Fatalf("expected the GET asked once, got %d", got)
	}
}
