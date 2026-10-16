package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/google"
)

const (
	cmdFakeRefresh = "1//cmd-fake-refresh-SECRET"
	cmdFakeAccess  = "ya29.cmd-fake-access-SECRET"
)

// googleStandIn is Google for mw google: a token endpoint that hands out the
// fake tokens for any code, a revoke endpoint that answers revokeStatus, and a
// browser that follows the sign-in address straight back to mw's loopback.
type googleStandIn struct {
	mu           sync.Mutex
	revoked      []string
	revokeStatus int
	authURLs     []string
}

func standInGoogle(t *testing.T, host string) *googleStandIn {
	t.Helper()
	g := &googleStandIn{revokeStatus: http.StatusOK}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MW_GOOGLE_CLIENT_FILE", "")
	vault := t.TempDir()
	t.Setenv("MW_VAULT", vault)
	t.Setenv("MW_HOST", host)
	if err := os.WriteFile(filepath.Join(vault, "home"), []byte("laptop 2026-10-16T00:00:00Z mw@laptop\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".config", "mw"), 0o700); err != nil {
		t.Fatal(err)
	}
	client := `{"installed":{"client_id":"123.apps.googleusercontent.com","client_secret":"GOCSPX-public"}}`
	if err := os.WriteFile(filepath.Join(home, ".config", "mw", "google-client.json"), []byte(client), 0o600); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("code_verifier") == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_request","error_description":"Missing code verifier."}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": cmdFakeAccess, "refresh_token": cmdFakeRefresh,
			"scope": "https://www.googleapis.com/auth/contacts.readonly", "token_type": "Bearer", "expires_in": 3599,
		})
	})
	mux.HandleFunc("/revoke", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		g.mu.Lock()
		g.revoked = append(g.revoked, r.PostForm.Get("token"))
		status := g.revokeStatus
		g.mu.Unlock()
		w.WriteHeader(status)
		if status != http.StatusOK {
			_, _ = w.Write([]byte(`{"error":"backend_error","error_description":"` + r.PostForm.Get("token") + `"}`))
		}
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	oldEndpoints, oldOpen := googleEndpoints, googleOpen
	googleEndpoints = google.Endpoints{Auth: "https://accounts.example/auth", Token: server.URL + "/token", Revoke: server.URL + "/revoke"}
	googleOpen = func(authURL string) error {
		g.mu.Lock()
		g.authURLs = append(g.authURLs, authURL)
		g.mu.Unlock()
		parsed, err := url.Parse(authURL)
		if err != nil {
			return err
		}
		q := parsed.Query()
		resp, err := http.Get(q.Get("redirect_uri") + "?code=4/code&state=" + url.QueryEscape(q.Get("state")))
		if err != nil {
			return err
		}
		return resp.Body.Close()
	}
	t.Cleanup(func() { googleEndpoints, googleOpen = oldEndpoints, oldOpen })
	return g
}

// runGoogle runs mw with args and returns everything it said anywhere: its
// output, its error stream, the process's own stdout and stderr, and the
// error it returned.
func runGoogle(t *testing.T, args ...string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	outFile, err := os.Create(filepath.Join(dir, "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	defer outFile.Close()
	realOut, realErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outFile, outFile

	var said bytes.Buffer
	root := newRootCmd()
	root.SetOut(&said)
	root.SetErr(&said)
	root.SetArgs(args)
	runErr := root.Execute()
	os.Stdout, os.Stderr = realOut, realErr

	all := said.String() + readAll(t, outFile.Name())
	if runErr != nil {
		all += runErr.Error()
	}
	return all, runErr
}

func mustNotShowTheToken(t *testing.T, said string) {
	t.Helper()
	for _, secret := range []string{cmdFakeRefresh, cmdFakeAccess, "SECRET"} {
		if strings.Contains(said, secret) {
			t.Fatalf("mw google showed the token: %q", said)
		}
	}
}

func TestGoogleLinkStatusUnlinkKeepTheTokenToTheFileAlone(t *testing.T) {
	g := standInGoogle(t, "laptop")
	token := filepath.Join(os.Getenv("HOME"), ".config", "mw", "google-token.json")

	said, err := runGoogle(t, "google", "status")
	mustNotShowTheToken(t, said)
	if err != nil || !strings.Contains(said, "not linked") {
		t.Fatalf("expected 'not linked' before a link, got %q %v", said, err)
	}

	said, err = runGoogle(t, "google", "link")
	mustNotShowTheToken(t, said)
	if err != nil {
		t.Fatalf("mw google link: %v: %s", err, said)
	}
	if len(g.authURLs) != 1 || !strings.Contains(said, g.authURLs[0]) {
		t.Fatalf("expected the sign-in address printed, got %q", said)
	}
	info, err := os.Stat(token)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("expected the token file 0600, got %v %v", info, err)
	}
	body, _ := os.ReadFile(token)
	if !strings.Contains(string(body), cmdFakeRefresh) || strings.Contains(string(body), cmdFakeAccess) {
		t.Fatalf("expected the refresh token kept and the access token not, got %s", body)
	}

	said, err = runGoogle(t, "google", "link")
	mustNotShowTheToken(t, said)
	if err != nil || !strings.Contains(said, "replac") {
		t.Fatalf("expected a second link to replace the first, got %q %v", said, err)
	}

	said, err = runGoogle(t, "google", "status")
	mustNotShowTheToken(t, said)
	if err != nil || strings.TrimSpace(said) != "linked (contacts.readonly)" {
		t.Fatalf("expected 'linked (contacts.readonly)', got %q %v", said, err)
	}

	g.revokeStatus = http.StatusServiceUnavailable
	said, err = runGoogle(t, "google", "unlink")
	mustNotShowTheToken(t, said)
	if err == nil {
		t.Fatalf("expected unlink to fail while Google cannot revoke, got %q", said)
	}
	if _, statErr := os.Stat(token); statErr != nil {
		t.Fatalf("expected the token kept while Google still holds it live, got %v", statErr)
	}

	g.revokeStatus = http.StatusOK
	said, err = runGoogle(t, "google", "unlink")
	mustNotShowTheToken(t, said)
	if err != nil {
		t.Fatalf("mw google unlink: %v: %s", err, said)
	}
	if len(g.revoked) != 2 || g.revoked[1] != cmdFakeRefresh {
		t.Fatalf("expected the refresh token posted to the revoke endpoint, got %d posts", len(g.revoked))
	}
	if _, statErr := os.Stat(token); !os.IsNotExist(statErr) {
		t.Fatalf("expected the token file deleted, got %v", statErr)
	}

	said, err = runGoogle(t, "google", "unlink")
	mustNotShowTheToken(t, said)
	if err != nil || !strings.Contains(said, "not linked") {
		t.Fatalf("expected unlink with nothing linked to say so and succeed, got %q %v", said, err)
	}
	said, _ = runGoogle(t, "google", "status")
	if strings.TrimSpace(said) != "not linked" {
		t.Fatalf("expected 'not linked' after unlink, got %q", said)
	}
}

func TestGoogleLinkRefusesOffTheHome(t *testing.T) {
	g := standInGoogle(t, "vps")

	said, err := runGoogle(t, "google", "link")
	if err == nil || !strings.Contains(said, "home") {
		t.Fatalf("expected a refusal on the VPS, got %q %v", said, err)
	}
	if len(g.authURLs) != 0 {
		t.Fatal("expected no sign-in started off the home")
	}
}
