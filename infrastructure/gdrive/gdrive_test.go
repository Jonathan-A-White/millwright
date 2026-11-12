package gdrive_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/gdrive"
)

const (
	oldRefresh = "1//old-refresh-SECRET"
	oldAccess  = "ya29.old-access-SECRET"
	newAccess  = "ya29.new-access-SECRET"
	fakeCode   = "4/fake-code"
)

var fixedNow = time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

// fakeGoogle is Google's token endpoint, Drive v3 and Sheets v4 in one server,
// asking for the bearer token it last handed out.
type fakeGoogle struct {
	mu       sync.Mutex
	token    []url.Values
	requests []string // "METHOD path?query" of each Drive and Sheets call
	bodies   []string
	auth     []string
	title    string
	sheets   map[string][][]string // tab -> rows
}

func (g *fakeGoogle) serve(t *testing.T) gdrive.Endpoints {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		g.mu.Lock()
		g.token = append(g.token, r.PostForm)
		g.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.PostForm.Get("grant_type") {
		case "authorization_code":
			if r.PostForm.Get("code") != fakeCode || r.PostForm.Get("code_verifier") == "" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Bad code"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": newAccess, "refresh_token": oldRefresh, "expires_in": 3599,
				"scope": "https://www.googleapis.com/auth/drive https://www.googleapis.com/auth/spreadsheets", "token_type": "Bearer",
			})
		case "refresh_token":
			if r.PostForm.Get("refresh_token") != oldRefresh {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": newAccess, "expires_in": 3599, "token_type": "Bearer"})
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	})
	record := func(w http.ResponseWriter, r *http.Request) bool {
		body := new(strings.Builder)
		buf := make([]byte, 4096)
		for {
			n, err := r.Body.Read(buf)
			body.Write(buf[:n])
			if err != nil {
				break
			}
		}
		g.mu.Lock()
		g.requests = append(g.requests, r.Method+" "+r.URL.RequestURI())
		g.bodies = append(g.bodies, body.String())
		g.auth = append(g.auth, r.Header.Get("Authorization"))
		g.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+newAccess {
			http.Error(w, `{"error":{"code":401,"message":"bad bearer"}}`, http.StatusUnauthorized)
			return false
		}
		w.Header().Set("Content-Type", "application/json")
		return true
	}
	mux.HandleFunc("/drive/v3/files", func(w http.ResponseWriter, r *http.Request) {
		if !record(w, r) {
			return
		}
		_, _ = w.Write([]byte(`{"files":[
			{"id":"id-new","name":"Budget 2026","modifiedTime":"2026-11-10T08:00:00.000Z","mimeType":"application/vnd.google-apps.spreadsheet"},
			{"id":"id-old","name":"Old budget","modifiedTime":"2025-01-02T03:04:05.000Z","mimeType":"application/pdf"}]}`))
	})
	mux.HandleFunc("/v4/spreadsheets/", func(w http.ResponseWriter, r *http.Request) {
		if !record(w, r) {
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/v4/spreadsheets/")
		id, values, hasValues := strings.Cut(rest, "/values/")
		_ = id
		if !hasValues {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"properties": map[string]any{"title": g.title},
				"sheets": []any{
					map[string]any{"properties": map[string]any{"title": "Main", "sheetId": 0, "gridProperties": map[string]any{"rowCount": 100, "columnCount": 8}}},
					map[string]any{"properties": map[string]any{"title": "O'Neil's", "sheetId": 7, "gridProperties": map[string]any{"rowCount": 10, "columnCount": 2}}},
				},
			})
			return
		}
		if r.Method == http.MethodPut {
			var in struct {
				Range  string     `json:"range"`
				Values [][]string `json:"values"`
			}
			_ = json.NewDecoder(strings.NewReader(g.bodies[len(g.bodies)-1])).Decode(&in)
			cells := 0
			for _, row := range in.Values {
				cells += len(row)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"updatedRange": in.Range, "updatedCells": cells, "updatedRows": len(in.Values)})
			return
		}
		tab, _, _ := strings.Cut(values, "!")
		tab = strings.Trim(tab, "'")
		rows := g.sheets[tab]
		_ = json.NewEncoder(w).Encode(map[string]any{"range": values, "majorDimension": "ROWS", "values": rows})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return gdrive.Endpoints{Auth: "https://accounts.example/auth", Token: server.URL + "/token", Drive: server.URL, Sheets: server.URL}
}

func (g *fakeGoogle) calls() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.requests...)
}

func newFake(t *testing.T) (*fakeGoogle, gdrive.Endpoints) {
	t.Helper()
	g := &fakeGoogle{title: "Budget 2026", sheets: map[string][][]string{
		"Main":     {{"item", "cost"}, {"rent, flat", "1,200"}, {"say \"hi\"", ""}},
		"O'Neil's": {{"x"}},
	}}
	return g, g.serve(t)
}

func keptFiles(t *testing.T) gdrive.Files {
	t.Helper()
	return gdrive.Files{Dir: filepath.Join(t.TempDir(), "gdrive")}
}

// session is a signed-in session whose saved access token expired an hour ago.
func session(t *testing.T, ends gdrive.Endpoints, expiry time.Time) (gdrive.Session, gdrive.Files) {
	t.Helper()
	files := keptFiles(t)
	if err := files.SaveToken(gdrive.Token{Access: oldAccess, Refresh: oldRefresh, Expiry: expiry, Scope: "s"}); err != nil {
		t.Fatal(err)
	}
	return gdrive.Session{
		Endpoints: ends, Files: files, Client: gdrive.Client{ID: "cid.apps.googleusercontent.com", Secret: "GOCSPX-x"},
		Now: func() time.Time { return fixedNow },
	}, files
}

func TestAuthorizeExchangesThePastedBackAddressAndKeepsFilesPrivate(t *testing.T) {
	g, ends := newFake(t)
	files := keptFiles(t)
	client := gdrive.Client{ID: "cid", Secret: "sec"}
	acct := gdrive.Account{Endpoints: ends, Now: func() time.Time { return fixedNow }}

	flow, err := acct.Begin(client)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(flow.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	for k, want := range map[string]string{
		"client_id": "cid", "response_type": "code", "access_type": "offline", "prompt": "consent",
		"scope":                 "https://www.googleapis.com/auth/drive https://www.googleapis.com/auth/spreadsheets",
		"code_challenge_method": "S256",
	} {
		if q.Get(k) != want {
			t.Fatalf("consent URL %s=%q, want %q (%s)", k, q.Get(k), want, flow.URL)
		}
	}
	if !strings.HasPrefix(q.Get("redirect_uri"), "http://localhost") || q.Get("state") == "" || q.Get("code_challenge") == "" {
		t.Fatalf("consent URL lacks redirect, state or challenge: %s", flow.URL)
	}

	// A pasted address with the wrong state is refused before any exchange.
	if _, err := acct.Finish(context.Background(), client, flow, "http://localhost/?state=other&code="+url.QueryEscape(fakeCode)); err == nil {
		t.Fatal("expected a wrong state to be refused")
	}
	if len(g.token) != 0 {
		t.Fatalf("expected no exchange for a wrong state, got %d", len(g.token))
	}
	// Google's own refusal is passed on, without the code.
	if _, err := acct.Finish(context.Background(), client, flow, "http://localhost/?state="+q.Get("state")+"&error=access_denied"); err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("expected access_denied, got %v", err)
	}

	tok, err := acct.Finish(context.Background(), client, flow, "http://localhost/?state="+url.QueryEscape(q.Get("state"))+"&code="+url.QueryEscape(fakeCode)+"&scope=x")
	if err != nil {
		t.Fatal(err)
	}
	if tok.Refresh != oldRefresh || tok.Access != newAccess || !tok.Expiry.After(fixedNow) {
		t.Fatalf("unexpected token %+v", tok)
	}
	if g.token[0].Get("redirect_uri") != q.Get("redirect_uri") || g.token[0].Get("client_secret") != "sec" {
		t.Fatalf("exchange lacked the redirect or the secret: %v", g.token[0])
	}

	if err := files.SaveClient(client); err != nil {
		t.Fatal(err)
	}
	if err := files.SaveToken(tok); err != nil {
		t.Fatal(err)
	}
	for path, mode := range map[string]os.FileMode{files.Dir: 0o700, files.ClientPath(): 0o600, files.TokenPath(): 0o600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("%s: mode %v err %v, want %v", path, info, err, mode)
		}
	}
	back, err := files.LoadToken()
	if err != nil || back.Refresh != oldRefresh || back.Access != newAccess {
		t.Fatalf("token did not round trip: %+v %v", back, err)
	}
}

func TestClientFileIsGooglesDesktopJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "google-client.json")
	if err := gdrive.WriteClientFile(path, gdrive.Client{ID: "i", Secret: "s"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", info, err)
	}
	dir, _ := os.Stat(filepath.Dir(path))
	if dir.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", dir.Mode())
	}
	body, _ := os.ReadFile(path)
	var raw struct {
		Installed struct {
			ID     string `json:"client_id"`
			Secret string `json:"client_secret"`
		} `json:"installed"`
	}
	if err := json.Unmarshal(body, &raw); err != nil || raw.Installed.ID != "i" || raw.Installed.Secret != "s" {
		t.Fatalf("not Desktop JSON: %s %v", body, err)
	}
	c, err := gdrive.ReadClientFile(path)
	if err != nil || c.ID != "i" || c.Secret != "s" {
		t.Fatalf("read back %+v %v", c, err)
	}
	web := filepath.Join(t.TempDir(), "web.json")
	_ = os.WriteFile(web, []byte(`{"web":{"client_id":"w"}}`), 0o600)
	if _, err := gdrive.ReadClientFile(web); err == nil {
		t.Fatal("expected a Web client to be refused")
	}
	if _, err := gdrive.ReadClientFile(filepath.Join(t.TempDir(), "none.json")); !os.IsNotExist(errorsUnwrap(err)) {
		t.Fatalf("expected a not-exist error, got %v", err)
	}
}

func errorsUnwrap(err error) error {
	for {
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return err
		}
		err = u.Unwrap()
	}
}

func TestFindRefreshesAnExpiredTokenSavesItAndListsNewestFirst(t *testing.T) {
	g, ends := newFake(t)
	s, files := session(t, ends, fixedNow.Add(-time.Hour))

	found, err := s.Find(context.Background(), "O'Budget\\")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 || found[0].ID != "id-new" || found[0].Name != "Budget 2026" || found[0].MimeType != "application/vnd.google-apps.spreadsheet" || found[0].Modified != "2026-11-10T08:00:00.000Z" {
		t.Fatalf("unexpected files %+v", found)
	}
	if len(g.token) != 1 || g.token[0].Get("grant_type") != "refresh_token" || g.token[0].Get("client_secret") != "GOCSPX-x" {
		t.Fatalf("expected one refresh, got %v", g.token)
	}
	saved, err := files.LoadToken()
	if err != nil || saved.Access != newAccess || saved.Refresh != oldRefresh || !saved.Expiry.After(fixedNow) {
		t.Fatalf("expected the refreshed token saved with the refresh token kept, got %+v %v", saved, err)
	}
	req, err := url.Parse("http://x" + strings.Fields(g.calls()[0])[1])
	if err != nil {
		t.Fatal(err)
	}
	q := req.Query()
	if q.Get("orderBy") != "modifiedTime desc" || q.Get("q") != `name contains 'O\'Budget\\' and trashed = false` || !strings.Contains(q.Get("fields"), "modifiedTime") {
		t.Fatalf("unexpected query %v", q)
	}
}

func TestAFreshTokenIsUsedWithoutRefreshing(t *testing.T) {
	g, ends := newFake(t)
	s, files := session(t, ends, fixedNow.Add(time.Hour))
	// The saved access token is the old one; the fake wants the new one, so the
	// call fails, and no refresh happens for a token that has not expired.
	if _, err := s.Find(context.Background(), "x"); err == nil {
		t.Fatal("expected the fake to refuse the old token")
	}
	if len(g.token) != 0 {
		t.Fatalf("expected no refresh for a fresh token, got %d", len(g.token))
	}
	if _, err := files.LoadToken(); err != nil {
		t.Fatal(err)
	}
}

func TestADeadRefreshTokenSaysSoWithoutShowingIt(t *testing.T) {
	_, ends := newFake(t)
	s, files := session(t, ends, fixedNow.Add(-time.Hour))
	if err := files.SaveToken(gdrive.Token{Access: "a", Refresh: "1//dead-SECRET", Expiry: fixedNow.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	_, err := s.Find(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "mw gdrive auth") {
		t.Fatalf("expected a pointer to mw gdrive auth, got %v", err)
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("the error shows a token: %v", err)
	}
}

func TestTabsAndReadValues(t *testing.T) {
	g, ends := newFake(t)
	s, _ := session(t, ends, fixedNow.Add(-time.Hour))

	title, tabs, err := s.Tabs(context.Background(), "FILE")
	if err != nil {
		t.Fatal(err)
	}
	if title != "Budget 2026" || len(tabs) != 2 || tabs[0].Title != "Main" || tabs[1].Title != "O'Neil's" || tabs[1].Rows != 10 || tabs[1].Columns != 2 {
		t.Fatalf("unexpected tabs %q %+v", title, tabs)
	}

	rows, err := s.Read(context.Background(), "FILE", "Main", "A1:B3")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[1][0] != "rent, flat" {
		t.Fatalf("unexpected rows %v", rows)
	}
	if _, err := s.Read(context.Background(), "FILE", "O'Neil's", ""); err != nil {
		t.Fatal(err)
	}
	calls := g.calls()
	for i, want := range map[int]string{2: "/v4/spreadsheets/FILE/values/Main!A1:B3", 1: "/v4/spreadsheets/FILE/values/'O''Neil''s'"} {
		u, err := url.Parse("http://x" + strings.Fields(calls[len(calls)-i])[1])
		if err != nil || u.Path != want {
			t.Fatalf("path %q (%v), want %q", u.Path, err, want)
		}
	}
}

func TestWriteUpdatesInPlaceAsUserEntered(t *testing.T) {
	g, ends := newFake(t)
	g.title = "Chores"
	s, _ := session(t, ends, fixedNow.Add(-time.Hour))

	done, err := s.Write(context.Background(), "FILE", "Main!A1:C1", []string{"a", "=1+1", "c"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if done.Range != "Main!A1:C1" || done.Cells != 3 {
		t.Fatalf("unexpected result %+v", done)
	}
	var put string
	for i, c := range g.calls() {
		if strings.HasPrefix(c, "PUT ") {
			put = c
			if !strings.Contains(c, "valueInputOption=USER_ENTERED") {
				t.Fatalf("expected USER_ENTERED, got %q", c)
			}
			if !strings.Contains(g.bodies[i], `"=1+1"`) {
				t.Fatalf("unexpected body %q", g.bodies[i])
			}
		}
	}
	if put == "" {
		t.Fatalf("no PUT among %v", g.calls())
	}
}

func TestWriteToABudgetNeedsYes(t *testing.T) {
	g, ends := newFake(t)
	g.title = "Our BUDGET 2026"
	s, _ := session(t, ends, fixedNow.Add(-time.Hour))

	_, err := s.Write(context.Background(), "FILE", "Main!A1", []string{"x"}, false)
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected a refusal naming --yes, got %v", err)
	}
	for _, c := range g.calls() {
		if strings.HasPrefix(c, "PUT ") {
			t.Fatalf("a write went out despite the refusal: %v", g.calls())
		}
	}
	if _, err := s.Write(context.Background(), "FILE", "Main!A1", []string{"x"}, true); err != nil {
		t.Fatalf("expected --yes to allow it, got %v", err)
	}
}

func TestRemoveDeletesTheFilesAndNothingElse(t *testing.T) {
	files := keptFiles(t)
	_ = files.SaveClient(gdrive.Client{ID: "i"})
	_ = files.SaveToken(gdrive.Token{Refresh: "r"})
	other := filepath.Join(filepath.Dir(files.Dir), "google-client.json")
	_ = os.WriteFile(other, []byte("{}"), 0o600)

	gone, err := files.Remove()
	if err != nil || len(gone) != 2 {
		t.Fatalf("got %v %v", gone, err)
	}
	if _, err := os.Stat(files.Dir); !os.IsNotExist(err) {
		t.Fatalf("expected the empty directory removed, got %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("the shared client file must stay: %v", err)
	}
	if gone, err := files.Remove(); err != nil || len(gone) != 0 {
		t.Fatalf("a second remove: %v %v", gone, err)
	}
}
