package google_test

import (
	"context"
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

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/google"
)

// peopleStandIn is Google's token endpoint and People API: searchContacts
// answers the contacts in book, whole, with every field the real API has.
type peopleStandIn struct {
	mu       sync.Mutex
	searches []url.Values
	auths    []string
	book     string // the JSON of the "results" array
	status   int
	refresh  string // what the token endpoint refuses, as an OAuth error
}

const fullBook = `[
 {"person":{"resourceName":"people/c1","emailAddresses":[{"value":"matthew.private@example.com"}],
   "addresses":[{"formattedValue":"1 Secret Street"}],"birthdays":[{"text":"Jan 2"}],
   "names":[{"displayName":"Matthew Steele","metadata":{"primary":true}}],
   "phoneNumbers":[
     {"value":"(555) 123-0001","canonicalForm":"+15551230001","type":"mobile","formattedType":"Mobile"},
     {"value":"555-999-0000","canonicalForm":"+15559990000","type":"work","formattedType":"Work"}]}}
]`

func (s *peopleStandIn) serve(t *testing.T) (google.OAuth, google.People, string) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		s.mu.Lock()
		refused := s.refresh
		s.mu.Unlock()
		if r.PostForm.Get("grant_type") != "refresh_token" || r.PostForm.Get("refresh_token") != fakeRefresh {
			http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
			return
		}
		if refused != "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"` + refused + `","error_description":"Token has been expired or revoked."}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": fakeAccess, "expires_in": 3599, "token_type": "Bearer"})
	})
	mux.HandleFunc("/v1/people:searchContacts", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.searches = append(s.searches, r.URL.Query())
		s.auths = append(s.auths, r.Header.Get("Authorization"))
		if s.status != 0 {
			w.WriteHeader(s.status)
			_, _ = w.Write([]byte(`{"error":{"code":403,"message":"People API has not been used in project 1 before or it is disabled.","status":"PERMISSION_DENIED"}}`))
			return
		}
		if r.URL.Query().Get("query") == "" {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		_, _ = w.Write([]byte(`{"results":` + s.book + `}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	dir := t.TempDir()
	clientFile := filepath.Join(dir, "client.json")
	if err := os.WriteFile(clientFile, []byte(`{"installed":{"client_id":"123.apps.googleusercontent.com","client_secret":"public"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	account := google.OAuth{ClientFile: clientFile, Endpoints: google.Endpoints{Token: server.URL + "/token"}}
	tokens := google.TokenFile{File: filepath.Join(dir, "token.json")}
	if err := tokens.Save(application.GoogleGrant{Refresh: fakeRefresh, Scope: application.GoogleContactsScope}); err != nil {
		t.Fatal(err)
	}
	return account, google.People{Tokens: tokens, Access: account, Endpoint: server.URL}, tokens.File
}

func TestPeopleFindReadsNamesAndMobilesAndNothingElse(t *testing.T) {
	stand := &peopleStandIn{book: fullBook}
	_, people, _ := stand.serve(t)

	got, err := people.Find(context.Background(), "Matthew")
	if err != nil {
		t.Fatal(err)
	}

	want := []application.Contact{{Name: "Matthew Steele", Mobiles: []string{"+15551230001"}}}
	if len(got) != 1 || got[0].Name != want[0].Name || strings.Join(got[0].Mobiles, ",") != "+15551230001" {
		t.Fatalf("expected %+v (the work number left out), got %+v", want, got)
	}
	if len(stand.searches) != 2 {
		t.Fatalf("expected the empty warm-up search and the real one, got %v", stand.searches)
	}
	if stand.searches[0].Get("query") != "" {
		t.Fatalf("expected the first search to warm the cache with an empty query, got %v", stand.searches[0])
	}
	real := stand.searches[1]
	if real.Get("query") != "Matthew" || real.Get("readMask") != "phoneNumbers,names" {
		t.Fatalf("expected query Matthew and readMask phoneNumbers,names, got %v", real)
	}
	for _, auth := range stand.auths {
		if auth != "Bearer "+fakeAccess {
			t.Fatalf("expected the access token as the bearer, got %q", auth)
		}
	}
}

func TestPeopleFindNumbersAreE164AndOnceEach(t *testing.T) {
	stand := &peopleStandIn{book: `[{"person":{"names":[{"displayName":"A"}],"phoneNumbers":[
	  {"value":"(555) 123-0001","type":"mobile"},
	  {"value":"+1 555 123 0001","canonicalForm":"+15551230001","type":"mobile"},
	  {"value":"1-555-123-0002","type":"mobile"},
	  {"value":"12345","type":"mobile"},
	  {"value":"555 123 0003","formattedType":"Mobile"}]}}]`}
	_, people, _ := stand.serve(t)

	got, err := people.Find(context.Background(), "A")
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || strings.Join(got[0].Mobiles, ",") != "+15551230001,+15551230002,+15551230003" {
		t.Fatalf("got %+v", got)
	}
}

func TestPeopleFindNotLinkedWrapsTheSentinel(t *testing.T) {
	people := google.People{Tokens: google.TokenFile{File: filepath.Join(t.TempDir(), "none.json")}}

	_, err := people.Find(context.Background(), "Matthew")

	if !errors.Is(err, application.ErrGoogleNotLinked) {
		t.Fatalf("expected ErrGoogleNotLinked, got %v", err)
	}
}

func TestPeopleFindRevokedLinkSaysSoWithoutAToken(t *testing.T) {
	stand := &peopleStandIn{book: fullBook, refresh: "invalid_grant"}
	_, people, _ := stand.serve(t)

	_, err := people.Find(context.Background(), "Matthew")

	if !errors.Is(err, application.ErrGoogleTokenDead) || !strings.Contains(err.Error(), "mw google link") {
		t.Fatalf("expected the dead-token error asking for mw google link, got %v", err)
	}
	if strings.Contains(err.Error(), fakeRefresh) {
		t.Fatalf("the token is in the error: %v", err)
	}
}

func TestPeopleFindRefusalIsSaidBrieflyWithoutContactsOrTokens(t *testing.T) {
	stand := &peopleStandIn{book: fullBook, status: http.StatusForbidden}
	_, people, _ := stand.serve(t)

	_, err := people.Find(context.Background(), "Matthew")

	if err == nil || !strings.Contains(err.Error(), "HTTP 403") || !strings.Contains(err.Error(), "PERMISSION_DENIED") {
		t.Fatalf("expected the refusal said, got %v", err)
	}
	for _, secret := range []string{fakeRefresh, fakeAccess, "Matthew"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("%q is in the error: %v", secret, err)
		}
	}
}
