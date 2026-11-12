package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/gdrive"
)

const gmailScope = "https://www.googleapis.com/auth/gmail.readonly"

// gmailStandIn is Google for mw gmail: token and Gmail v1.
func gmailStandIn(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MW_GOOGLE_CLIENT_FILE", "")
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": gdAccess, "expires_in": 3599, "token_type": "Bearer"})
	})
	mux.HandleFunc("/gmail/v1/users/me/messages", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+gdAccess {
			http.Error(w, `{"error":{"message":"bad bearer"}}`, http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"messages":[{"id":"a1"},{"id":"b2"}]}`))
	})
	mux.HandleFunc("/gmail/v1/users/me/messages/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/gmail/v1/users/me/messages/")
		if r.URL.Query().Get("format") == "full" {
			data := base64.URLEncoding.EncodeToString([]byte("Total $12.00"))
			_, _ = w.Write([]byte(`{"id":"` + id + `","payload":{"mimeType":"text/plain","headers":[{"name":"From","value":"Gas <g@x.example>"},{"name":"Subject","value":"Gas bill"}],"body":{"data":"` + data + `"}}}`))
			return
		}
		date := map[string]string{"a1": "1762000000000", "b2": "1762900000000"}[id]
		_, _ = w.Write([]byte(`{"id":"` + id + `","internalDate":"` + date + `","payload":{"headers":[{"name":"From","value":"Gas <g@x.example>"},{"name":"Subject","value":"Bill ` + id + `"}]}}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	old := gdriveEndpoints
	gdriveEndpoints = gdrive.Endpoints{Auth: "https://accounts.example/auth", Token: server.URL + "/token", Drive: server.URL, Sheets: server.URL, Gmail: server.URL}
	t.Cleanup(func() { gdriveEndpoints = old })
	return home
}

func signedInWith(t *testing.T, home, scope string) {
	t.Helper()
	files := gdrive.Files{Dir: filepath.Join(home, ".config", "mw", "gdrive")}
	if err := files.SaveClient(gdrive.Client{ID: "cid", Secret: gdSecret}); err != nil {
		t.Fatal(err)
	}
	if err := files.SaveToken(gdrive.Token{Access: "old", Refresh: gdRefresh, Expiry: time.Now().Add(-time.Hour), Scope: scope}); err != nil {
		t.Fatal(err)
	}
}

func TestGmailSearchPrintsNewestFirstTabSeparated(t *testing.T) {
	home := gmailStandIn(t)
	signedInWith(t, home, "https://www.googleapis.com/auth/drive "+gmailScope)
	said, err := runGdrive(t, "", "gmail", "search", "from:gas", "--limit", "2")
	if err != nil {
		t.Fatalf("search: %v %s", err, said)
	}
	lines := strings.Split(strings.TrimSpace(said), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "b2\t") || !strings.HasPrefix(lines[1], "a1\t") {
		t.Fatalf("not newest first: %q", said)
	}
	f := strings.Split(lines[0], "\t")
	if len(f) != 4 || f[2] != "Gas <g@x.example>" || f[3] != "Bill b2" || !strings.HasPrefix(f[1], "2025-11-") {
		t.Fatalf("fields %q", f)
	}
}

func TestGmailReadPrintsHeadersThenBody(t *testing.T) {
	home := gmailStandIn(t)
	signedInWith(t, home, gmailScope)
	said, err := runGdrive(t, "", "gmail", "read", "a1")
	if err != nil {
		t.Fatalf("read: %v %s", err, said)
	}
	for _, want := range []string{"From: Gas <g@x.example>", "Subject: Gas bill", "Total $12.00"} {
		if !strings.Contains(said, want) {
			t.Fatalf("read lacks %q: %s", want, said)
		}
	}
}

func TestGmailWithAnOldTokenSaysToRunAuthAgain(t *testing.T) {
	home := gmailStandIn(t)
	signedInWith(t, home, "https://www.googleapis.com/auth/drive https://www.googleapis.com/auth/spreadsheets")
	for _, args := range [][]string{{"gmail", "search", "x"}, {"gmail", "read", "a1"}} {
		said, err := runGdrive(t, "", args...)
		if err == nil || !strings.Contains(said, "run mw gdrive auth again to add Gmail") {
			t.Fatalf("%v: want the add-Gmail message, got %v %q", args, err, said)
		}
		if strings.Contains(said, "goroutine") || strings.Contains(said, ".go:") {
			t.Fatalf("a stack in %q", said)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "mw", "gdrive", "token.json")); err != nil {
		t.Fatal(err)
	}
}

func TestGmailNotSignedInSaysToRunAuth(t *testing.T) {
	gmailStandIn(t)
	said, err := runGdrive(t, "", "gmail", "search", "x")
	if err == nil || !strings.Contains(said, "mw gdrive auth") {
		t.Fatalf("got %v %q", err, said)
	}
}

func TestGdriveAuthAsksForGmailReadOnlyBesideDriveAndSheets(t *testing.T) {
	standInGdrive(t)
	said := runAuthFlow(t, "--client-id", "12345.apps.googleusercontent.com", "--client-secret", gdSecret)
	for _, want := range []string{"gmail.readonly", "auth%2Fdrive", "auth%2Fspreadsheets"} {
		if !strings.Contains(said, want) {
			t.Fatalf("the consent URL lacks %s: %s", want, said)
		}
	}
	for _, bad := range []string{"gmail.modify", "gmail.send", "gmail.compose", "mail.google.com", "gmail.labels"} {
		if strings.Contains(said, bad) {
			t.Fatalf("the consent URL asks for %s", bad)
		}
	}
}
