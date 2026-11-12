package main

import (
	"bytes"
	"encoding/json"
	"io"
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
	gdRefresh = "1//gd-refresh-SECRET"
	gdAccess  = "ya29.gd-access-SECRET"
	gdSecret  = "GOCSPX-gd-client-SECRET"
)

// gdriveStandIn is Google for mw gdrive: token, Drive and Sheets in one server.
type gdriveStandIn struct {
	mu    sync.Mutex
	calls []string
	title string
}

func standInGdrive(t *testing.T) (*gdriveStandIn, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MW_GOOGLE_CLIENT_FILE", "")
	g := &gdriveStandIn{title: "House chores"}

	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		g.mu.Lock()
		g.calls = append(g.calls, "token "+r.PostForm.Get("grant_type"))
		g.mu.Unlock()
		if r.PostForm.Get("grant_type") == "authorization_code" && (r.PostForm.Get("code") != "4/the-code" || r.PostForm.Get("code_verifier") == "") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": gdAccess, "refresh_token": gdRefresh, "expires_in": 3599, "token_type": "Bearer", "scope": "x"})
	})
	api := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		g.mu.Lock()
		g.calls = append(g.calls, r.Method+" "+r.URL.RequestURI()+" "+string(body))
		g.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+gdAccess {
			http.Error(w, `{"error":{"message":"bad bearer"}}`, http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/drive/v3/files":
			_, _ = w.Write([]byte(`{"files":[{"id":"abc","name":"Chores","modifiedTime":"2026-11-10T08:00:00Z","mimeType":"application/vnd.google-apps.spreadsheet"}]}`))
		case strings.Contains(r.URL.Path, "/values/") && r.Method == http.MethodPut:
			_, _ = w.Write([]byte(`{"updatedRange":"Sheet1!A1:B1","updatedCells":2}`))
		case strings.Contains(r.URL.Path, "/values/"):
			_, _ = w.Write([]byte(`{"values":[["a","b, c"],["1"]]}`))
		default:
			_, _ = w.Write([]byte(`{"properties":{"title":"` + g.title + `"},"sheets":[{"properties":{"title":"Sheet1","sheetId":0,"gridProperties":{"rowCount":10,"columnCount":3}}}]}`))
		}
	}
	mux.HandleFunc("/drive/v3/files", api)
	mux.HandleFunc("/v4/spreadsheets/", api)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	old := gdriveEndpoints
	gdriveEndpoints = gdrive.Endpoints{Auth: "https://accounts.example/auth", Token: server.URL + "/token", Drive: server.URL, Sheets: server.URL}
	t.Cleanup(func() { gdriveEndpoints = old })
	return g, home
}

// syncBuffer is the command's output, readable while the command still runs.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// browser is stdin for mw gdrive auth: once the consent URL has been printed
// it hands back the address the browser would have ended on.
type browser struct {
	out  *syncBuffer
	line *strings.Reader
	t    *testing.T
}

func (b *browser) Read(p []byte) (int, error) {
	if b.line == nil {
		deadline := time.Now().Add(10 * time.Second)
		for !strings.Contains(b.out.String(), "https://accounts.example/auth") && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		b.line = strings.NewReader(pastedBack(b.t, b.out.String()))
	}
	return b.line.Read(p)
}

// runGdrive runs mw with args, stdin being what the test types, and returns
// everything it said anywhere. It fails the test if any secret shows.
func runGdrive(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	return runGdriveWith(t, func(*syncBuffer) io.Reader { return strings.NewReader(stdin) }, args...)
}

func runGdriveWith(t *testing.T, stdin func(*syncBuffer) io.Reader, args ...string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	outFile, err := os.Create(filepath.Join(dir, "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	defer outFile.Close()
	realOut, realErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outFile, outFile

	said := &syncBuffer{}
	root := newRootCmd()
	root.SetIn(stdin(said))
	root.SetOut(said)
	root.SetErr(said)
	root.SetArgs(args)
	runErr := root.Execute()
	os.Stdout, os.Stderr = realOut, realErr

	all := said.String() + readAll(t, outFile.Name())
	if runErr != nil {
		all += runErr.Error()
	}
	for _, secret := range []string{gdRefresh, gdAccess, gdSecret, "SECRET"} {
		if strings.Contains(all, secret) {
			t.Fatalf("mw %v showed a secret (%q): %q", args, secret, all)
		}
	}
	return all, runErr
}

func modeOf(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

// pastedBack is what the browser's address bar holds after consent: the state
// from the consent URL printed in said, and the code.
func pastedBack(t *testing.T, said string) string {
	t.Helper()
	for _, field := range strings.Fields(said) {
		if strings.HasPrefix(field, "https://accounts.example/auth") {
			u, err := url.Parse(field)
			if err != nil {
				t.Fatal(err)
			}
			return "http://localhost/?state=" + url.QueryEscape(u.Query().Get("state")) + "&code=4%2Fthe-code&scope=x\n"
		}
	}
	t.Fatalf("no consent URL in %q", said)
	return ""
}

// runAuthFlow runs mw gdrive auth, answering the consent URL it prints.
func runAuthFlow(t *testing.T, args ...string) string {
	t.Helper()
	said, err := runGdriveWith(t, func(out *syncBuffer) io.Reader { return &browser{out: out, t: t} }, append([]string{"gdrive", "auth"}, args...)...)
	if err != nil {
		t.Fatalf("mw gdrive auth: %v: %s", err, said)
	}
	return said
}

func TestGdriveAuthWritesClientAndTokenPrivately(t *testing.T) {
	_, home := standInGdrive(t)
	said := runAuthFlow(t, "--client-id", "12345.apps.googleusercontent.com", "--client-secret", gdSecret)

	if !strings.Contains(said, "https://accounts.example/auth?") {
		t.Fatalf("expected the consent URL printed, got %q", said)
	}
	u := strings.Fields(said[strings.Index(said, "https://accounts.example/auth"):])[0]
	parsed, _ := url.Parse(u)
	if parsed.Query().Get("scope") != "https://www.googleapis.com/auth/drive https://www.googleapis.com/auth/spreadsheets" || parsed.Query().Get("access_type") != "offline" || parsed.Query().Get("prompt") != "consent" {
		t.Fatalf("unexpected consent URL %s", u)
	}

	dir := filepath.Join(home, ".config", "mw", "gdrive")
	shared := filepath.Join(home, ".config", "mw", "google-client.json")
	for path, mode := range map[string]os.FileMode{
		dir: 0o700, filepath.Join(dir, "token.json"): 0o600, filepath.Join(dir, "client.json"): 0o600, shared: 0o600,
	} {
		if got := modeOf(t, path); got != mode {
			t.Fatalf("%s: mode %v, want %v", path, got, mode)
		}
	}
	body, _ := os.ReadFile(filepath.Join(dir, "token.json"))
	if !strings.Contains(string(body), gdRefresh) {
		t.Fatalf("expected the refresh token kept, got %s", body)
	}
	c, err := gdrive.ReadClientFile(shared)
	if err != nil || c.ID != "12345.apps.googleusercontent.com" || c.Secret != gdSecret {
		t.Fatalf("expected the flags written to google-client.json, got %+v %v", c, err)
	}
}

func TestGdriveAuthReadsTheSharedClientFileWhenThere(t *testing.T) {
	_, home := standInGdrive(t)
	shared := filepath.Join(home, ".config", "mw", "google-client.json")
	if err := os.MkdirAll(filepath.Dir(shared), 0o700); err != nil {
		t.Fatal(err)
	}
	original := `{"installed":{"client_id":"file-id","client_secret":"` + gdSecret + `"}}`
	if err := os.WriteFile(shared, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	said := runAuthFlow(t)
	if !strings.Contains(said, "client_id=file-id") {
		t.Fatalf("expected the consent URL for the file's client, got %q", said)
	}
	if body, _ := os.ReadFile(shared); string(body) != original {
		t.Fatalf("the shared client file must be left as it was, got %s", body)
	}
	c, err := gdrive.ReadClientFile(filepath.Join(home, ".config", "mw", "gdrive", "client.json"))
	if err != nil || c.ID != "file-id" {
		t.Fatalf("expected a copy in the gdrive directory, got %+v %v", c, err)
	}
}

func TestGdriveAuthWithNoClientSaysHowToGiveOne(t *testing.T) {
	standInGdrive(t)
	said, err := runGdrive(t, "", "gdrive", "auth")
	if err == nil || !strings.Contains(said, "--client-id") {
		t.Fatalf("expected a pointer to --client-id, got %q %v", said, err)
	}
}

func TestGdriveAuthRefusesAWrongStateAndKeepsNothing(t *testing.T) {
	standInGdrive(t)
	_ = os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".config", "mw"), 0o700)
	said, err := runGdrive(t, "http://localhost/?state=wrong&code=4%2Fthe-code\n", "gdrive", "auth", "--client-id", "i", "--client-secret", gdSecret)
	if err == nil {
		t.Fatalf("expected a refusal, got %q", said)
	}
	if _, statErr := os.Stat(filepath.Join(os.Getenv("HOME"), ".config", "mw", "gdrive", "token.json")); !os.IsNotExist(statErr) {
		t.Fatalf("expected no token kept, got %v", statErr)
	}
}

// signedIn writes the files an auth would, with an expired access token.
func signedIn(t *testing.T, home string) {
	t.Helper()
	files := gdrive.Files{Dir: filepath.Join(home, ".config", "mw", "gdrive")}
	if err := files.SaveClient(gdrive.Client{ID: "cid", Secret: gdSecret}); err != nil {
		t.Fatal(err)
	}
	if err := files.SaveToken(gdrive.Token{Access: "old", Refresh: gdRefresh, Expiry: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
}

func TestGdriveFindReadWriteRevoke(t *testing.T) {
	g, home := standInGdrive(t)
	signedIn(t, home)

	said, err := runGdrive(t, "", "gdrive", "find", "chore")
	if err != nil {
		t.Fatalf("find: %v %s", err, said)
	}
	for _, want := range []string{"abc", "Chores", "2026-11-10T08:00:00Z", "application/vnd.google-apps.spreadsheet"} {
		if !strings.Contains(said, want) {
			t.Fatalf("find lacks %q: %s", want, said)
		}
	}
	saved, _ := os.ReadFile(filepath.Join(home, ".config", "mw", "gdrive", "token.json"))
	if !strings.Contains(string(saved), gdAccess) {
		t.Fatalf("expected the refreshed token saved, got %s", saved)
	}

	said, err = runGdrive(t, "", "gdrive", "read", "abc")
	if err != nil || !strings.Contains(said, "Sheet1") {
		t.Fatalf("read with no tab should list the tabs: %q %v", said, err)
	}
	said, err = runGdrive(t, "", "gdrive", "read", "abc", "--sheet", "Sheet1", "--range", "A1:B2")
	if err != nil || strings.TrimSpace(said) != "a,\"b, c\"\n1" {
		t.Fatalf("expected CSV, got %q %v", said, err)
	}

	said, err = runGdrive(t, "", "gdrive", "write", "abc", "Sheet1!A1:B1", "x", "y z")
	if err != nil || !strings.Contains(said, "Sheet1!A1:B1") || !strings.Contains(said, "2") {
		t.Fatalf("write: %q %v", said, err)
	}
	var put string
	for _, c := range g.calls {
		if strings.HasPrefix(c, "PUT ") {
			put = c
		}
	}
	if !strings.Contains(put, "valueInputOption=USER_ENTERED") || !strings.Contains(put, `["x","y z"]`) {
		t.Fatalf("unexpected PUT %q", put)
	}

	g.title = "Family Budget"
	n := len(g.calls)
	said, err = runGdrive(t, "", "gdrive", "write", "abc", "Sheet1!A1", "x")
	if err == nil || !strings.Contains(said, "--yes") {
		t.Fatalf("expected a Budget refusal, got %q %v", said, err)
	}
	for _, c := range g.calls[n:] {
		if strings.HasPrefix(c, "PUT ") {
			t.Fatal("a write went out for a Budget without --yes")
		}
	}
	if _, err := runGdrive(t, "", "gdrive", "write", "abc", "Sheet1!A1", "x", "--yes"); err != nil {
		t.Fatalf("--yes should allow it: %v", err)
	}

	said, err = runGdrive(t, "", "gdrive", "revoke")
	if err != nil || !strings.Contains(said, "myaccount.google.com/permissions") {
		t.Fatalf("revoke: %q %v", said, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "mw", "gdrive")); !os.IsNotExist(err) {
		t.Fatalf("expected the files gone, got %v", err)
	}
	said, err = runGdrive(t, "", "gdrive", "find", "chore")
	if err == nil || !strings.Contains(said, "mw gdrive auth") {
		t.Fatalf("expected a pointer to auth after revoke, got %q %v", said, err)
	}
}
