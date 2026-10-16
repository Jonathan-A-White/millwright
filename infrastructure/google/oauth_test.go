package google_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/google"
)

const (
	fakeRefresh = "1//fake-refresh-SECRET-0001"
	fakeAccess  = "ya29.fake-access-SECRET-0001"
	fakeCode    = "4/fake-code-0001"
)

// fakeGoogle is Google's token and revoke endpoints. It hands out fakeRefresh
// for fakeCode once the code_verifier matches the challenge the browser saw.
type fakeGoogle struct {
	mu        sync.Mutex
	challenge string // what the fake browser read from the auth URL
	redirect  string
	refresh   string // what the next exchange hands out (fakeRefresh by default)
	scope     string // what the next exchange grants (contacts.readonly by default)
	exchanged []url.Values
	revoked   []url.Values
	revokeAt  int // status the revoke endpoint answers (200 by default)
	tokenErr  bool
}

func (g *fakeGoogle) serve(t *testing.T) google.Endpoints {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		if err := r.ParseForm(); err != nil || r.Method != http.MethodPost {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		g.exchanged = append(g.exchanged, r.PostForm)
		w.Header().Set("Content-Type", "application/json")
		if g.tokenErr {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Bad Request"}`))
			return
		}
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if r.PostForm.Get("code") != fakeCode || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge ||
			r.PostForm.Get("redirect_uri") != g.redirect || r.PostForm.Get("grant_type") != "authorization_code" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"verifier does not match"}`))
			return
		}
		refresh, scope := g.refresh, g.scope
		if refresh == "" {
			refresh = fakeRefresh
		}
		if scope == "" {
			scope = application.GoogleContactsScope
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": fakeAccess, "refresh_token": refresh, "scope": scope,
			"token_type": "Bearer", "expires_in": 3599,
		})
	})
	mux.HandleFunc("/revoke", func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		if err := r.ParseForm(); err != nil || r.Method != http.MethodPost {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		g.revoked = append(g.revoked, r.PostForm)
		if g.revokeAt == http.StatusBadRequest {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_token","error_description":"Token expired or revoked"}`))
			return
		}
		if g.revokeAt != 0 {
			w.WriteHeader(g.revokeAt)
			_, _ = w.Write([]byte(`{"error":"backend_error","error_description":"echo ` + r.PostForm.Get("token") + `"}`))
		}
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return google.Endpoints{Auth: "https://accounts.example/o/oauth2/v2/auth", Token: server.URL + "/token", Revoke: server.URL + "/revoke"}
}

// browser is the fake browser: it reads the auth URL, keeps what Google would,
// and sends the loopback redirect back with fakeCode and the state.
func (g *fakeGoogle) browser(t *testing.T, seen *url.Values) func(string) {
	return func(authURL string) {
		parsed, err := url.Parse(authURL)
		if err != nil {
			t.Errorf("auth URL %q: %v", authURL, err)
			return
		}
		query := parsed.Query()
		*seen = query
		g.mu.Lock()
		g.challenge = query.Get("code_challenge")
		g.redirect = query.Get("redirect_uri")
		g.mu.Unlock()
		back := query.Get("redirect_uri") + "?code=" + url.QueryEscape(fakeCode) + "&state=" + url.QueryEscape(query.Get("state"))
		resp, err := http.Get(back)
		if err != nil {
			t.Errorf("following the redirect: %v", err)
			return
		}
		resp.Body.Close()
	}
}

func clientFile(t *testing.T, kind string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "google-client.json")
	body := `{"` + kind + `":{"client_id":"123.apps.googleusercontent.com","client_secret":"GOCSPX-public","auth_uri":"https://accounts.google.com/o/oauth2/auth","token_uri":"https://oauth2.googleapis.com/token","redirect_uris":["http://localhost"]}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func signIn(t *testing.T, g *fakeGoogle, seen *url.Values) (application.GoogleGrant, error) {
	t.Helper()
	oauth := google.OAuth{ClientFile: clientFile(t, "installed"), Endpoints: g.serve(t), Wait: 10 * time.Second}
	return oauth.SignIn(context.Background(), application.GoogleContactsScope, g.browser(t, seen))
}

func TestSignInUsesPKCEAndAsksForContactsAlone(t *testing.T) {
	g := &fakeGoogle{}
	var seen url.Values

	grant, err := signIn(t, g, &seen)
	if err != nil {
		t.Fatal(err)
	}

	if seen.Get("code_challenge_method") != "S256" || len(seen.Get("code_challenge")) != 43 {
		t.Fatalf("expected an S256 challenge in the auth URL, got %v", seen)
	}
	if seen.Get("scope") != "https://www.googleapis.com/auth/contacts.readonly" {
		t.Fatalf("expected contacts.readonly alone, got scope %q", seen.Get("scope"))
	}
	if seen.Has("include_granted_scopes") {
		t.Fatalf("expected no earlier grants folded in, got %v", seen)
	}
	if seen.Get("access_type") != "offline" || seen.Get("response_type") != "code" || seen.Get("client_id") != "123.apps.googleusercontent.com" {
		t.Fatalf("expected an offline code request for the Desktop client, got %v", seen)
	}
	redirect, err := url.Parse(seen.Get("redirect_uri"))
	if err != nil || redirect.Hostname() != "127.0.0.1" || redirect.Port() == "" || redirect.Port() == "0" {
		t.Fatalf("expected a loopback redirect on a chosen port, got %q", seen.Get("redirect_uri"))
	}
	if len(seen.Get("state")) < 20 {
		t.Fatalf("expected a random state, got %q", seen.Get("state"))
	}

	if len(g.exchanged) != 1 {
		t.Fatalf("expected one code exchange, got %d", len(g.exchanged))
	}
	sent := g.exchanged[0]
	if verifier := sent.Get("code_verifier"); len(verifier) < 43 || verifier == seen.Get("code_challenge") {
		t.Fatalf("expected a code_verifier sent, got %q", verifier)
	}
	if sent.Get("client_id") != "123.apps.googleusercontent.com" || sent.Get("client_secret") != "GOCSPX-public" {
		t.Fatalf("expected the Desktop client's id and secret sent, got %v", sent)
	}
	if grant.Refresh != fakeRefresh || grant.Scope != application.GoogleContactsScope {
		t.Fatalf("expected the refresh token and scope, got %+v", grant)
	}
}

func TestSignInIgnoresARedirectWithTheWrongState(t *testing.T) {
	g := &fakeGoogle{}
	endpoints := g.serve(t)
	var seen url.Values
	honest := g.browser(t, &seen)
	oauth := google.OAuth{ClientFile: clientFile(t, "installed"), Endpoints: endpoints, Wait: 10 * time.Second}

	grant, err := oauth.SignIn(context.Background(), application.GoogleContactsScope, func(authURL string) {
		parsed, _ := url.Parse(authURL)
		forged := parsed.Query().Get("redirect_uri") + "?code=4/forged&state=not-the-state"
		resp, err := http.Get(forged)
		if err != nil {
			t.Errorf("forged redirect: %v", err)
			return
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected the forged redirect refused, got %d", resp.StatusCode)
		}
		honest(authURL)
	})
	if err != nil {
		t.Fatal(err)
	}
	if grant.Refresh != fakeRefresh || len(g.exchanged) != 1 {
		t.Fatalf("expected only the honest code exchanged, got %+v after %d exchanges", grant, len(g.exchanged))
	}
}

func TestSignInSaysWhatTheTokenEndpointRefusedAndNoMore(t *testing.T) {
	g := &fakeGoogle{tokenErr: true}
	var seen url.Values

	_, err := signIn(t, g, &seen)
	if err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("expected the endpoint's refusal said, got %v", err)
	}
	if strings.Contains(err.Error(), fakeCode) || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("expected no code or token in the error, got %v", err)
	}
}

func TestSignInSaysWhenHeDeclines(t *testing.T) {
	g := &fakeGoogle{}
	oauth := google.OAuth{ClientFile: clientFile(t, "installed"), Endpoints: g.serve(t), Wait: 10 * time.Second}

	_, err := oauth.SignIn(context.Background(), application.GoogleContactsScope, func(authURL string) {
		parsed, _ := url.Parse(authURL)
		q := parsed.Query()
		resp, err := http.Get(q.Get("redirect_uri") + "?error=access_denied&state=" + url.QueryEscape(q.Get("state")))
		if err == nil {
			resp.Body.Close()
		}
	})
	if err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("expected the decline said, got %v", err)
	}
	if len(g.exchanged) != 0 {
		t.Fatal("expected no exchange after a decline")
	}
}

func TestSignInGivesUpWhenNoBrowserComesBack(t *testing.T) {
	g := &fakeGoogle{}
	oauth := google.OAuth{ClientFile: clientFile(t, "installed"), Endpoints: g.serve(t), Wait: time.Millisecond}

	_, err := oauth.SignIn(context.Background(), application.GoogleContactsScope, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "no sign-in came back") {
		t.Fatalf("expected the wait to end, got %v", err)
	}
}

func TestSignInNeedsADesktopClientFile(t *testing.T) {
	g := &fakeGoogle{}
	endpoints := g.serve(t)

	missing := filepath.Join(t.TempDir(), "google-client.json")
	_, err := google.OAuth{ClientFile: missing, Endpoints: endpoints}.SignIn(context.Background(), application.GoogleContactsScope, func(string) {})
	if err == nil || !strings.Contains(err.Error(), missing) || !strings.Contains(err.Error(), "google_client_file") {
		t.Fatalf("expected the missing client file named with its config key, got %v", err)
	}

	_, err = google.OAuth{ClientFile: clientFile(t, "web"), Endpoints: endpoints}.SignIn(context.Background(), application.GoogleContactsScope, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "Desktop") {
		t.Fatalf("expected a Web client refused, got %v", err)
	}
}

func TestRevokePostsTheTokenInTheBody(t *testing.T) {
	g := &fakeGoogle{}
	oauth := google.OAuth{Endpoints: g.serve(t)}

	if err := oauth.Revoke(context.Background(), fakeRefresh); err != nil {
		t.Fatal(err)
	}
	if len(g.revoked) != 1 || g.revoked[0].Get("token") != fakeRefresh {
		t.Fatalf("expected the token posted to the revoke endpoint, got %v", g.revoked)
	}
}

func TestRevokeOfADeadTokenIsErrGoogleTokenDead(t *testing.T) {
	g := &fakeGoogle{revokeAt: http.StatusBadRequest}
	err := google.OAuth{Endpoints: g.serve(t)}.Revoke(context.Background(), fakeRefresh)
	if !errors.Is(err, application.ErrGoogleTokenDead) {
		t.Fatalf("expected ErrGoogleTokenDead, got %v", err)
	}
}

func TestRevokeFailureNeverCarriesTheToken(t *testing.T) {
	g := &fakeGoogle{revokeAt: http.StatusInternalServerError}
	err := google.OAuth{Endpoints: g.serve(t)}.Revoke(context.Background(), fakeRefresh)
	if err == nil || errors.Is(err, application.ErrGoogleTokenDead) {
		t.Fatalf("expected a plain failure, got %v", err)
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("expected no token in the error, got %v", err)
	}
}

func TestTokenFileIsHisAloneAndASecondSaveReplacesTheFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".config", "mw", "google-token.json")
	file := google.TokenFile{File: path}

	if _, err := file.Load(); !errors.Is(err, application.ErrGoogleNotLinked) {
		t.Fatalf("expected not linked before a save, got %v", err)
	}
	if err := file.Save(application.GoogleGrant{Refresh: "1//first-SECRET", Scope: application.GoogleContactsScope}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected mode 0600, got %o", info.Mode().Perm())
	}
	if dir, _ := os.Stat(filepath.Dir(path)); dir.Mode().Perm() != 0o700 {
		t.Fatalf("expected a directory made 0700, got %o", dir.Mode().Perm())
	}

	// A loose file left by hand is replaced by one only he can read.
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := file.Save(application.GoogleGrant{Refresh: fakeRefresh, Scope: application.GoogleContactsScope}); err != nil {
		t.Fatal(err)
	}
	info, _ = os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected mode 0600 after a second save, got %o", info.Mode().Perm())
	}
	body, _ := os.ReadFile(path)
	if strings.Contains(string(body), "first") || !strings.Contains(string(body), fakeRefresh) {
		t.Fatalf("expected the second grant in place of the first, got %s", body)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("expected no temporary file left beside the token, got %d entries", len(entries))
	}

	grant, err := file.Load()
	if err != nil || grant.Refresh != fakeRefresh || grant.Scope != application.GoogleContactsScope {
		t.Fatalf("expected the grant read back, got %+v %v", grant, err)
	}

	if err := file.Remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected the file gone, got %v", err)
	}
	if err := file.Remove(); err != nil {
		t.Fatalf("expected removing nothing to be no error, got %v", err)
	}
}

func TestATokenFileThatIsNotOursIsRefusedWithoutQuotingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "google-token.json")
	if err := os.WriteFile(path, []byte("SECRET-not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := google.TokenFile{File: path}.Load()
	if err == nil || errors.Is(err, application.ErrGoogleNotLinked) || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("expected a refusal that does not quote the file, got %v", err)
	}
}
