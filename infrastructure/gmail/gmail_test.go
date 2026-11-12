package gmail_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/gdrive"
	"github.com/Jonathan-A-White/millwright/infrastructure/gmail"
)

const (
	refresh   = "1//gm-refresh-SECRET"
	freshAcc  = "ya29.gm-access-SECRET"
	gmailOnly = "https://www.googleapis.com/auth/gmail.readonly"
)

var now = time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

func b64(s string) string { return base64.URLEncoding.EncodeToString([]byte(s)) }

// fakeGmail is Google's token endpoint and Gmail v1 in one server.
type fakeGmail struct {
	mu       sync.Mutex
	requests []string
	refresh  int
}

func (g *fakeGmail) calls() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.requests...)
}

func header(name, value string) map[string]any { return map[string]any{"name": name, "value": value} }

func (g *fakeGmail) serve(t *testing.T) gdrive.Endpoints {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.refresh++
		g.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": freshAcc, "expires_in": 3599, "token_type": "Bearer"})
	})
	guard := func(w http.ResponseWriter, r *http.Request) bool {
		g.mu.Lock()
		g.requests = append(g.requests, r.Method+" "+r.URL.RequestURI())
		g.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+freshAcc {
			http.Error(w, `{"error":{"message":"bad bearer"}}`, http.StatusUnauthorized)
			return false
		}
		return true
	}
	mux.HandleFunc("/gmail/v1/users/me/messages", func(w http.ResponseWriter, r *http.Request) {
		if !guard(w, r) {
			return
		}
		// Deliberately oldest first: search must put the newest first.
		_ = json.NewEncoder(w).Encode(map[string]any{"messages": []any{
			map[string]any{"id": "old1", "threadId": "t1"},
			map[string]any{"id": "new2", "threadId": "t2"},
			map[string]any{"id": "mid3", "threadId": "t3"},
		}})
	})
	mux.HandleFunc("/gmail/v1/users/me/messages/", func(w http.ResponseWriter, r *http.Request) {
		if !guard(w, r) {
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/gmail/v1/users/me/messages/")
		format := r.URL.Query().Get("format")
		switch id {
		case "old1", "new2", "mid3":
			if format != "metadata" {
				http.Error(w, `{"error":{"message":"want metadata"}}`, http.StatusBadRequest)
				return
			}
			meta := map[string]map[string]any{
				"old1": {"internalDate": "1760000000000", "from": "Water Co <bills@water.example>", "subject": "Your bill"},
				"new2": {"internalDate": "1762900000000", "from": "Power Co <bills@power.example>", "subject": "Bill\nfor October\x1b[31m"},
				"mid3": {"internalDate": "1761000000000", "from": "Gas Co <bills@gas.example>", "subject": "Gas statement"},
			}[id]
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "internalDate": meta["internalDate"], "payload": map[string]any{
				"headers": []any{header("From", meta["from"].(string)), header("Subject", meta["subject"].(string)), header("Date", "x")},
			}})
		case "multi":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "internalDate": "1762900000000", "payload": map[string]any{
				"mimeType": "multipart/mixed",
				"headers": []any{header("From", "Power Co <bills@power.example>"), header("To", "me@example.com"),
					header("Date", "Tue, 11 Nov 2025 08:00:00 +0000"), header("Subject", "October bill"), header("X-Other", "dropped")},
				"parts": []any{
					map[string]any{"mimeType": "multipart/alternative", "parts": []any{
						map[string]any{"mimeType": "text/plain", "body": map[string]any{"data": b64("Amount due: $123.45\nDue 20 Nov")}},
						map[string]any{"mimeType": "text/html", "body": map[string]any{"data": b64("<p>HTML version $999</p>")}},
					}},
					map[string]any{"mimeType": "application/pdf", "filename": "bill.pdf", "body": map[string]any{"attachmentId": "att1", "size": 20480}},
				},
			}})
		case "htmlonly":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "payload": map[string]any{
				"mimeType": "text/html",
				"headers":  []any{header("Subject", "Html bill")},
				"body": map[string]any{"data": b64(`<html><head><style>p{color:red}</style></head><body><script>alert(1)</script>` +
					`<p>Total&nbsp;due: <b>$45.00</b> &amp; tax</p><br>Pay by <a href="x">Friday</a></body></html>`)},
			}})
		default:
			http.Error(w, `{"error":{"code":404,"message":"Requested entity was not found."}}`, http.StatusNotFound)
		}
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return gdrive.Endpoints{Auth: "https://accounts.example/auth", Token: server.URL + "/token", Drive: server.URL, Sheets: server.URL, Gmail: server.URL}
}

func newClient(t *testing.T, scope string) (gmail.Client, *fakeGmail) {
	t.Helper()
	g := &fakeGmail{}
	files := gdrive.Files{Dir: filepath.Join(t.TempDir(), "gdrive")}
	if err := files.SaveToken(gdrive.Token{Access: "old", Refresh: refresh, Expiry: now.Add(-time.Hour), Scope: scope}); err != nil {
		t.Fatal(err)
	}
	s := gdrive.Session{Endpoints: g.serve(t), Files: files, Client: gdrive.Client{ID: "cid", Secret: "GOCSPX-x"}, Now: func() time.Time { return now }}
	return gmail.Client{Session: s}, g
}

func TestSearchListsNewestFirstWithIdDateFromSubject(t *testing.T) {
	c, g := newClient(t, "https://www.googleapis.com/auth/drive "+gmailOnly)
	got, err := c.Search(context.Background(), "from:bills newer_than:30d", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != "new2" || got[1].ID != "mid3" || got[2].ID != "old1" {
		t.Fatalf("not newest first: %+v", got)
	}
	if got[0].From != "Power Co <bills@power.example>" || got[0].Subject != "Bill for October[31m" {
		t.Fatalf("unexpected headers (control characters must go): %+v", got[0])
	}
	if got[0].Date != "2025-11-11 22:26" {
		t.Fatalf("unexpected date %q", got[0].Date)
	}
	list, _ := url.Parse("http://x" + strings.Fields(g.calls()[0])[1])
	if list.Query().Get("q") != "from:bills newer_than:30d" || list.Query().Get("maxResults") != "3" {
		t.Fatalf("unexpected list query %v", list.Query())
	}
	for _, call := range g.calls() {
		if !strings.HasPrefix(call, "GET ") {
			t.Fatalf("a Gmail call that is not a GET: %s", call)
		}
	}
}

func TestSearchWithAnOldTokenWithoutGmailSaysToSignInAgain(t *testing.T) {
	c, g := newClient(t, "https://www.googleapis.com/auth/drive https://www.googleapis.com/auth/spreadsheets")
	_, err := c.Search(context.Background(), "x", 5)
	if err == nil || !errors.Is(err, gmail.ErrNoGmailScope) || !strings.Contains(err.Error(), "run mw gdrive auth again to add Gmail") {
		t.Fatalf("expected the add-Gmail message, got %v", err)
	}
	if strings.Contains(err.Error(), refresh) || strings.Contains(err.Error(), "old") && strings.Contains(err.Error(), "ya29") {
		t.Fatalf("token in the error: %v", err)
	}
	if len(g.calls()) != 0 || g.refresh != 0 {
		t.Fatalf("no call should leave the machine: %v refresh=%d", g.calls(), g.refresh)
	}
	if _, err := c.Read(context.Background(), "multi"); !errors.Is(err, gmail.ErrNoGmailScope) {
		t.Fatalf("read should say the same, got %v", err)
	}
}

func TestAGoogle403ForScopeSaysToSignInAgain(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/gmail/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"code":403,"message":"Request had insufficient authentication scopes."}}`, http.StatusForbidden)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	files := gdrive.Files{Dir: filepath.Join(t.TempDir(), "gdrive")}
	// A token with no scope recorded is tried anyway; Google's 403 then decides.
	if err := files.SaveToken(gdrive.Token{Access: freshAcc, Refresh: refresh, Expiry: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	c := gmail.Client{Session: gdrive.Session{Endpoints: gdrive.Endpoints{Gmail: server.URL}, Files: files, Now: func() time.Time { return now }}}
	_, err := c.Search(context.Background(), "x", 5)
	if !errors.Is(err, gmail.ErrNoGmailScope) || !strings.Contains(err.Error(), "run mw gdrive auth again to add Gmail") {
		t.Fatalf("got %v", err)
	}
}

func TestSearchWithNothingFoundIsEmptyNotAnError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/gmail/", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"resultSizeEstimate":0}`)) })
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	files := gdrive.Files{Dir: filepath.Join(t.TempDir(), "gdrive")}
	_ = files.SaveToken(gdrive.Token{Access: freshAcc, Refresh: refresh, Expiry: now.Add(time.Hour), Scope: gmailOnly})
	c := gmail.Client{Session: gdrive.Session{Endpoints: gdrive.Endpoints{Gmail: server.URL}, Files: files, Now: func() time.Time { return now }}}
	got, err := c.Search(context.Background(), "x", 5)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestReadPrintsHeadersPlainBodyAndNamesAttachments(t *testing.T) {
	c, g := newClient(t, gmailOnly)
	m, err := c.Read(context.Background(), "multi")
	if err != nil {
		t.Fatal(err)
	}
	if m.Body != "Amount due: $123.45\nDue 20 Nov" {
		t.Fatalf("expected the text part, got %q", m.Body)
	}
	if len(m.Attachments) != 1 || m.Attachments[0].Name != "bill.pdf" || m.Attachments[0].MimeType != "application/pdf" || m.Attachments[0].Size != 20480 {
		t.Fatalf("attachments %+v", m.Attachments)
	}
	want := map[string]string{"From": "Power Co <bills@power.example>", "To": "me@example.com", "Date": "Tue, 11 Nov 2025 08:00:00 +0000", "Subject": "October bill"}
	got := map[string]string{}
	for _, h := range m.Headers {
		got[h.Name] = h.Value
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("header %s = %q, want %q (all %v)", k, got[k], v, got)
		}
	}
	if _, ok := got["X-Other"]; ok {
		t.Fatalf("only the plain headers are shown: %v", got)
	}
	for _, call := range g.calls() {
		if strings.Contains(call, "attachments") {
			t.Fatalf("an attachment was downloaded: %s", call)
		}
	}
}

func TestReadStripsHTMLToTextWhenThereIsNoTextPart(t *testing.T) {
	c, _ := newClient(t, gmailOnly)
	m, err := c.Read(context.Background(), "htmlonly")
	if err != nil {
		t.Fatal(err)
	}
	if m.Body != "Total due: $45.00 & tax\nPay by Friday" {
		t.Fatalf("got %q", m.Body)
	}
}

func TestReadOfAnUnknownIdSaysGooglesWords(t *testing.T) {
	c, _ := newClient(t, gmailOnly)
	_, err := c.Read(context.Background(), "nope")
	if err == nil || !strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("got %v", err)
	}
}

// The code that talks to Gmail asks for the read-only scope and has no way to
// send, modify, delete or label.
func TestOnlyTheReadOnlyScopeAndOnlyGets(t *testing.T) {
	if gdrive.GmailScope != gmailOnly {
		t.Fatalf("GmailScope = %q", gdrive.GmailScope)
	}
	if !strings.Contains(gdrive.Scope, gmailOnly) || !strings.Contains(gdrive.Scope, gdrive.DriveScope) || !strings.Contains(gdrive.Scope, gdrive.SheetsScope) {
		t.Fatalf("the sign-in asks for %q", gdrive.Scope)
	}
	scopes := regexp.MustCompile(`googleapis\.com/auth/[a-z._]+|mail\.google\.com`)
	for _, dir := range []string{".", "../gdrive", "../../cmd/mw"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			name := e.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			if dir == "../../cmd/mw" && !strings.HasPrefix(name, "gmail") && !strings.HasPrefix(name, "gdrive") {
				continue
			}
			body, _ := os.ReadFile(filepath.Join(dir, name))
			for _, m := range scopes.FindAllString(string(body), -1) {
				switch m {
				case "googleapis.com/auth/gmail.readonly", "googleapis.com/auth/drive", "googleapis.com/auth/spreadsheets":
				default:
					t.Errorf("%s/%s names the scope %s", dir, name, m)
				}
			}
			if dir == "." || strings.HasPrefix(name, "gmail") {
				for _, verb := range []string{"MethodPost", "MethodPut", "MethodPatch", "MethodDelete", `"POST"`, `"PUT"`, `"PATCH"`, `"DELETE"`, "/send", "/modify", "/trash", "/labels"} {
					if strings.Contains(string(body), verb) {
						t.Errorf("%s/%s has %s: Gmail is read-only here", dir, name, verb)
					}
				}
			}
		}
	}
}
