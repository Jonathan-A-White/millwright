package vpsnginx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthzReadsTheCommitAndAnAbsentOneIsEmpty(t *testing.T) {
	for body, want := range map[string]string{
		`{"ok":true,"standby":false,"commit":"141f1d3"}`: "141f1d3",
		`{"ok":true,"standby":true,"home":"laptop"}`:     "",
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
		got, err := Healthz{}.Commit(context.Background(), server.URL)
		server.Close()
		if err != nil || got != want {
			t.Errorf("%s: got %q, %v, want %q", body, got, err, want)
		}
	}
}

func TestHealthzErrorsOnANon200AndOnAnUnreachableBackend(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	if _, err := (Healthz{}).Commit(context.Background(), server.URL); err == nil {
		t.Error("expected an error for a 503")
	}
	server.Close()
	if _, err := (Healthz{}).Commit(context.Background(), server.URL); err == nil {
		t.Error("expected an error for a closed server")
	}
}
