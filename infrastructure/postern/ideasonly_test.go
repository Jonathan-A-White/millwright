package postern_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

const ideasOnlyKey = "02ab0000000000000000000000000000000000000000000000000000000000cd"

// GET /api/ideas-only/{pubkey} answers the setting and the state worked out by
// the backend's clock (postern's docs/api.md): mw reads the state and the zone
// of the schedule.
func TestIdeasOnlyReadsTheStateAndTheScheduleZone(t *testing.T) {
	b, backend := serve(t, map[string]answer{
		"GET /api/ideas-only/" + ideasOnlyKey: {200, `{"setting":{"until":1790003600,"schedule":{"tz":"America/New_York","windows":[]},"reaches":{"ring":true,"emergency":true}},"state":{"on":true,"until":1790003600,"by":"until"}}`},
	})

	got, err := backend.IdeasOnly(context.Background(), ideasOnlyKey)
	if err != nil {
		t.Fatalf("reading Ideas only: %v", err)
	}
	want := application.IdeasOnlyNow{On: true, Until: 1790003600, By: "until", Tz: "America/New_York"}
	if got != want {
		t.Fatalf("expected %+v, got %+v", want, got)
	}
	if reqs := requestsFor(b, "/api/ideas-only/"+ideasOnlyKey); len(reqs) != 1 {
		t.Fatalf("expected one request, got %v", reqs)
	}
}

// A key that has stored nothing is off, and has no zone.
func TestIdeasOnlyOfAKeyThatStoredNothingIsOff(t *testing.T) {
	_, backend := serve(t, map[string]answer{
		"GET /api/ideas-only/" + ideasOnlyKey: {200, `{"setting":{"until":0,"reaches":{}},"state":{"on":false,"until":0,"by":""}}`},
	})

	got, err := backend.IdeasOnly(context.Background(), ideasOnlyKey)
	if err != nil || !reflect.DeepEqual(got, application.IdeasOnlyNow{}) {
		t.Fatalf("expected off, got %+v, %v", got, err)
	}
}

// A backend with no store for it answers 501, and one older than the route 404:
// neither keeps the mode, so the key is off and nothing is said of it.
func TestIdeasOnlyFromABackendWithNoStoreIsOff(t *testing.T) {
	for _, status := range []int{404, 501} {
		_, backend := serve(t, map[string]answer{
			"GET /api/ideas-only/" + ideasOnlyKey: {status, `{"error":"this backend keeps no Ideas only settings"}`},
		})

		got, err := backend.IdeasOnly(context.Background(), ideasOnlyKey)
		if err != nil || got != (application.IdeasOnlyNow{}) {
			t.Fatalf("a %d: expected off, got %+v, %v", status, got, err)
		}
	}
}

// Any other refusal is an error: the answer is not known.
func TestIdeasOnlyRefusedIsAnError(t *testing.T) {
	_, backend := serve(t, map[string]answer{
		"GET /api/ideas-only/" + ideasOnlyKey: {500, `{"error":"boom"}`},
	})

	if _, err := backend.IdeasOnly(context.Background(), ideasOnlyKey); err == nil {
		t.Fatal("expected an error for a 500")
	}
}
