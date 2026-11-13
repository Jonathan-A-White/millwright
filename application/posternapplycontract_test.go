package application_test

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// TestPosternContractActionWords holds application.PosternActions equal to the
// action_words of the contract fixture, the copy of postern's
// docs/fixtures/contract.json that domain/events keeps in testdata.
func TestPosternContractActionWords(t *testing.T) {
	raw, err := os.ReadFile("../domain/events/testdata/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var loose map[string]json.RawMessage
	if err := json.Unmarshal(raw, &loose); err != nil {
		t.Fatal(err)
	}
	var want []string
	if err := json.Unmarshal(loose["action_words"], &want); err != nil || len(want) == 0 {
		t.Fatalf("contract.json has no action_words: %v", err)
	}
	for _, v := range application.PosternActions {
		if !slices.Contains(want, v) {
			t.Errorf("millwright applies %q, which docs/fixtures/contract.json lacks", v)
		}
	}
	for _, v := range want {
		if !slices.Contains(application.PosternActions, v) {
			t.Errorf("docs/fixtures/contract.json has %q, which millwright lacks", v)
		}
	}
}
