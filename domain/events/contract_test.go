package events_test

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// contractFile is a copy of postern's docs/fixtures/contract.json, the closed
// lists the PWA, the backend and millwright keep by hand. Postern's own tests
// hold the PWA's and the backend's lists to it; this file holds millwright's.
// A change to the lists there is copied here, and the application package's
// TestPosternContractActionWords reads this same file.
const contractFile = "testdata/contract.json"

// contractList reads one list of the contract by its key.
func contractList(t *testing.T, key string) []string {
	t.Helper()
	raw, err := os.ReadFile(contractFile)
	if err != nil {
		t.Fatal(err)
	}
	// about is a string, not a list: read each value apart.
	var loose map[string]json.RawMessage
	if err := json.Unmarshal(raw, &loose); err != nil {
		t.Fatal(err)
	}
	var list []string
	if err := json.Unmarshal(loose[key], &list); err != nil {
		t.Fatalf("contract.json %s: %v", key, err)
	}
	if len(list) == 0 {
		t.Fatalf("contract.json lists no %s", key)
	}
	return list
}

// sameSet fails when got and want differ, naming what each lacks.
func sameSet(t *testing.T, name string, got, want []string) {
	t.Helper()
	for _, v := range got {
		if !slices.Contains(want, v) {
			t.Errorf("%s: millwright has %q, which docs/fixtures/contract.json lacks", name, v)
		}
	}
	for _, v := range want {
		if !slices.Contains(got, v) {
			t.Errorf("%s: docs/fixtures/contract.json has %q, which millwright lacks", name, v)
		}
	}
}

func TestPosternContractEventKinds(t *testing.T) {
	sameSet(t, "event_kinds", events.Kinds(), contractList(t, "event_kinds"))
}

func TestPosternContractExpectStates(t *testing.T) {
	sameSet(t, "expect_states", domain.ExpectStates, contractList(t, "expect_states"))
}
