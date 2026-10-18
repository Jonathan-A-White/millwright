package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// aPersonBackend is a fake postern backend answering GET /api/register/person:
// every key in keys is one person's (dailyLimit limit), any other is unknown.
// asked counts the lookups it answered.
func aPersonBackend(t *testing.T, limit int, keys ...string) (url string, asked *atomic.Int32) {
	t.Helper()
	asked = &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		if r.URL.Path != "/api/register/person" {
			http.NotFound(w, r)
			return
		}
		asker := r.URL.Query().Get("key")
		for _, k := range keys {
			if k == asker {
				_ = json.NewEncoder(w).Encode(map[string]any{"known": true, "personId": "p1", "keys": keys, "dailyLimit": limit})
				return
			}
		}
		_, _ = w.Write([]byte(`{"known":false}`))
	}))
	t.Cleanup(server.Close)
	return server.URL, asked
}

// sentLines is n answered grist from key, sent earlier on the mill's day.
func sentLines(key string, n int) []application.GrindLine {
	lines := make([]application.GrindLine, n)
	for i := range lines {
		lines[i] = application.GrindLine{
			Time: time.Date(2026, 9, 29, 8, 0, i, 0, time.UTC), Txid: fmt.Sprintf("direct:%s-%d", key[:6], i),
			Sender: application.KeyFingerprint(key), Status: application.GristAnswered,
		}
	}
	return lines
}

// A mill with the day's record already in its state: lines.
func aMillWithADay(t *testing.T, persons *application.PersonCap, limit int, lines ...[]application.GrindLine) (application.GristGrind, *answers) {
	t.Helper()
	mill, backend, grinder := aSmallMill(t)
	mill.Ceilings = application.GristCeilings{DailyLimit: limit}
	mill.Persons = persons
	grinder.Result = application.SessionResult{Subtype: "success", Answer: json.RawMessage(`{"items":[],"placeName":"x"}`)}
	for _, some := range lines {
		for _, line := range some {
			if err := mill.State.Append(context.Background(), line); err != nil {
				t.Fatal(err)
			}
		}
	}
	return mill, &answers{t: t, backend: backend}
}

type answers struct {
	t       *testing.T
	backend interface{ Delivered() [][]byte }
}

func (a *answers) first() application.GristAnswer {
	a.t.Helper()
	delivered := a.backend.Delivered()
	if len(delivered) == 0 {
		a.t.Fatal("expected an answer delivered, got none")
	}
	return openFakeAnswer(a.t, delivered[0])
}

// A person with keys A and B at a cap of 50 who has sent 30 from A and 20 from
// B is refused on the next grist from either, with the daily-cap message; at 49
// in all it passes.
func TestPersonCapCountsEveryDeviceKeyOfAPersonTogether(t *testing.T) {
	url, _ := aPersonBackend(t, 50, gristPhoneKey, gristStrangerKey)
	for name, tc := range map[string]struct {
		fromA, fromB int
		refused      bool
	}{
		"30 from A and 20 from B":        {30, 20, true},
		"none from A and 50 from B":      {0, 50, true},
		"50 from A and none from B":      {50, 0, true},
		"30 from A and 19 from B is 49":  {30, 19, false},
		"none from A and 49 from B":      {0, 49, false},
		"over the person's cap, A alone": {51, 0, true},
	} {
		t.Run(name, func(t *testing.T) {
			// The home's own limit is 250: the person's 50 is what counts.
			mill, got := aMillWithADay(t, application.NewPersonCap(url, &bytes.Buffer{}), 250,
				sentLines(gristPhoneKey, tc.fromA), sentLines(gristStrangerKey, tc.fromB))
			if _, err := mill.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			answer := got.first()
			if tc.refused && (answer.Status != application.GristRefused || answer.Reason != application.GristReasonDaily(50)) {
				t.Fatalf("expected the daily-cap refusal at 50, got %+v", answer)
			}
			if !tc.refused && answer.Status != application.GristAnswered {
				t.Fatalf("expected the grist answered at %d in all, got %+v", tc.fromA+tc.fromB, answer)
			}
		})
	}
}

// A key the backend does not know keeps the per-key rule at the home's limit.
func TestPersonCapLeavesAnUnknownKeyAtTheHomesLimit(t *testing.T) {
	url, asked := aPersonBackend(t, 50, gristStrangerKey) // the phone's key is nobody's
	for name, tc := range map[string]struct {
		sent    int
		refused bool
	}{"60 sent is under 250": {60, false}, "249 sent": {249, false}, "250 sent": {250, true}} {
		t.Run(name, func(t *testing.T) {
			mill, got := aMillWithADay(t, application.NewPersonCap(url, &bytes.Buffer{}), 250, sentLines(gristPhoneKey, tc.sent))
			if _, err := mill.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			answer := got.first()
			if tc.refused && (answer.Status != application.GristRefused || answer.Reason != application.GristReasonDaily(250)) {
				t.Fatalf("expected the daily-cap refusal at 250, got %+v", answer)
			}
			if !tc.refused && answer.Status != application.GristAnswered {
				t.Fatalf("expected the grist answered, got %+v", answer)
			}
		})
	}
	if asked.Load() == 0 {
		t.Error("expected the backend asked about the key")
	}
}

// A backend that cannot be reached, or that is slower than the timeout, leaves
// the per-key rule in force, and says so in one line.
func TestPersonCapFallsBackToThePerKeyRuleWhenTheBackendIsDown(t *testing.T) {
	gone := httptest.NewServer(http.NotFoundHandler())
	goneURL := gone.URL
	gone.Close()
	sick := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(sick.Close)

	for name, persons := range map[string]func(log *bytes.Buffer) *application.PersonCap{
		"unreachable": func(log *bytes.Buffer) *application.PersonCap { return application.NewPersonCap(goneURL, log) },
		"an error":    func(log *bytes.Buffer) *application.PersonCap { return application.NewPersonCap(sick.URL, log) },
		"slow": func(log *bytes.Buffer) *application.PersonCap {
			c := application.NewPersonCap("http://backend.invalid", log)
			c.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				<-r.Context().Done() // the call is held to its timeout
				return nil, r.Context().Err()
			})}
			c.Timeout = 5 * time.Millisecond
			return c
		},
	} {
		t.Run(name, func(t *testing.T) {
			var log bytes.Buffer
			// 60 sent from the phone's key: under the home's 250, over a person's 50.
			mill, got := aMillWithADay(t, persons(&log), 250, sentLines(gristPhoneKey, 60))
			if _, err := mill.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			if answer := got.first(); answer.Status != application.GristAnswered {
				t.Fatalf("expected the per-key rule at 250, so answered, got %+v", answer)
			}
			if lines := strings.Split(strings.TrimSpace(log.String()), "\n"); len(lines) != 1 || !strings.Contains(lines[0], "person") {
				t.Fatalf("expected one warning line about the person lookup, got %q", log.String())
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The lookup is held to 2 seconds.
func TestPersonCapAsksWithinTwoSeconds(t *testing.T) {
	persons := application.NewPersonCap("http://backend.invalid", &bytes.Buffer{})
	var held time.Duration
	persons.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		if !ok {
			return nil, errors.New("no deadline")
		}
		held = time.Until(deadline)
		return nil, errors.New("stop here")
	})}
	persons.Lookup(context.Background(), gristPhoneKey)
	if held <= time.Second || held > 2*time.Second {
		t.Fatalf("expected a deadline of 2 s, got %v", held)
	}
}

// What the backend says about a key is kept 5 minutes, a stranger too.
func TestPersonCapKeepsAnAnswerFiveMinutes(t *testing.T) {
	url, asked := aPersonBackend(t, 50, gristPhoneKey)
	now := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	persons := application.NewPersonCap(url, &bytes.Buffer{})
	persons.Now = func() time.Time { return now }

	person, known := persons.Lookup(context.Background(), gristPhoneKey)
	if !known || person.DailyLimit != 50 || len(person.Keys) != 1 || person.Keys[0] != gristPhoneKey {
		t.Fatalf("expected the person with a cap of 50, got %+v %v", person, known)
	}
	if _, known := persons.Lookup(context.Background(), gristStrangerKey); known {
		t.Fatal("expected a stranger unknown")
	}
	now = now.Add(4*time.Minute + 59*time.Second)
	persons.Lookup(context.Background(), gristPhoneKey)
	persons.Lookup(context.Background(), gristStrangerKey)
	if asked.Load() != 2 {
		t.Fatalf("expected two lookups inside 5 minutes, got %d", asked.Load())
	}
	now = now.Add(2 * time.Second)
	persons.Lookup(context.Background(), gristPhoneKey)
	if asked.Load() != 3 {
		t.Fatalf("expected the key asked again after 5 minutes, got %d", asked.Load())
	}
}

// A backend that fails is not asked again for every grist, nor warned about
// for every one.
func TestPersonCapWarnsOncePerFailure(t *testing.T) {
	gone := httptest.NewServer(http.NotFoundHandler())
	url := gone.URL
	gone.Close()
	var log bytes.Buffer
	now := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	persons := application.NewPersonCap(url, &log)
	persons.Now = func() time.Time { return now }
	for range 3 {
		if _, known := persons.Lookup(context.Background(), gristPhoneKey); known {
			t.Fatal("expected unknown")
		}
	}
	if n := strings.Count(log.String(), "\n"); n != 1 {
		t.Fatalf("expected one warning for three grist, got %q", log.String())
	}
}

// With no lookup at all (the knob off), a person's keys are each held to the
// home's limit, as before.
func TestPersonCapOffCountsEachKeyAlone(t *testing.T) {
	mill, got := aMillWithADay(t, nil, 250, sentLines(gristPhoneKey, 30), sentLines(gristStrangerKey, 20))
	if _, err := mill.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if answer := got.first(); answer.Status != application.GristAnswered {
		t.Fatalf("expected the grist answered, got %+v", answer)
	}
}
