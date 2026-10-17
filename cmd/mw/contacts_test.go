package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/google"
)

// peopleStandIn is Google for mw contacts: a token endpoint that trades the
// refresh token for an access token and a People API that answers searchContacts
// with his whole address book, every field the real API has.
type peopleStandIn struct {
	mu      sync.Mutex
	queries []string
	book    string
}

const addressBook = `[
 {"person":{"resourceName":"people/c1","emailAddresses":[{"value":"steele.private@example.com"}],
   "addresses":[{"formattedValue":"1 Secret Street"}],"biographies":[{"value":"owes me a hose"}],
   "names":[{"displayName":"Matthew Steele","metadata":{"primary":true}}],
   "phoneNumbers":[{"value":"(555) 123-0001","canonicalForm":"+15551230001","type":"mobile"},
                   {"value":"555-999-0000","canonicalForm":"+15559990000","type":"work"}]}},
 {"person":{"names":[{"displayName":"Matthew Jones"}],"phoneNumbers":[{"value":"555-123-0002","canonicalForm":"+15551230002","type":"home"}]}}
]`

func standInPeople(t *testing.T, linked bool) *peopleStandIn {
	t.Helper()
	p := &peopleStandIn{book: addressBook}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MW_GOOGLE_CLIENT_FILE", "")
	conf := filepath.Join(home, ".config", "mw")
	if err := os.MkdirAll(conf, 0o700); err != nil {
		t.Fatal(err)
	}
	client := `{"installed":{"client_id":"123.apps.googleusercontent.com","client_secret":"GOCSPX-public"}}`
	if err := os.WriteFile(filepath.Join(conf, "google-client.json"), []byte(client), 0o600); err != nil {
		t.Fatal(err)
	}
	if linked {
		token := `{"refresh_token":"` + cmdFakeRefresh + `","scope":"https://www.googleapis.com/auth/contacts.readonly"}`
		if err := os.WriteFile(filepath.Join(conf, "google-token.json"), []byte(token), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("refresh_token") != cmdFakeRefresh {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": cmdFakeAccess, "expires_in": 3599})
	})
	mux.HandleFunc("/v1/people:searchContacts", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		p.mu.Lock()
		p.queries = append(p.queries, query)
		p.mu.Unlock()
		switch query {
		case "":
			_, _ = w.Write([]byte(`{}`))
		case "Matthew":
			_, _ = w.Write([]byte(`{"results":` + p.book + `}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	oldEndpoints, oldPeople := googleEndpoints, peopleEndpoint
	googleEndpoints = google.Endpoints{Token: server.URL + "/token"}
	peopleEndpoint = server.URL
	t.Cleanup(func() { googleEndpoints, peopleEndpoint = oldEndpoints, oldPeople })
	return p
}

func TestContactsFindPrintsNameAndMobileAndNothingElseOfTheBook(t *testing.T) {
	standInPeople(t, true)

	said, err := runGoogle(t, "contacts", "find", "Matthew")

	if err != nil {
		t.Fatalf("mw contacts find: %v: %s", err, said)
	}
	want := "Matthew Steele  +15551230001\nMatthew Jones  (no mobile number)\n"
	if said != want {
		t.Fatalf("expected:\n%s\ngot:\n%s", want, said)
	}
	for _, private := range []string{"steele.private", "Secret Street", "owes me a hose", "+15559990000", "people/c1"} {
		if strings.Contains(said, private) {
			t.Fatalf("%q of the address book was shown: %q", private, said)
		}
	}
	mustNotShowTheToken(t, said)
}

func TestContactsFindWithNoMatchExitsZero(t *testing.T) {
	standInPeople(t, true)

	said, err := runGoogle(t, "contacts", "find", "Zed")

	if err != nil || strings.TrimSpace(said) != "no contact named Zed" {
		t.Fatalf("expected 'no contact named Zed' and exit 0, got %q %v", said, err)
	}
}

func TestContactsFindNotLinkedSaysHowToLink(t *testing.T) {
	people := standInPeople(t, false)

	said, err := runGoogle(t, "contacts", "find", "Matthew")

	if err == nil || !strings.Contains(said, "not linked: mw google link") {
		t.Fatalf("expected a failure saying 'not linked: mw google link', got %q %v", said, err)
	}
	if len(people.queries) != 0 {
		t.Fatalf("expected Google never asked while not linked, got %v", people.queries)
	}
}
