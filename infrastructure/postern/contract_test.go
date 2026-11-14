package postern_test

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// contractFile is a copy of postern's docs/fixtures/contract.json, the closed
// lists the PWA, the backend and millwright keep by hand (the README beside it
// names the commit). Postern's own tests hold the PWA's and the backend's lists
// to it; domain/events and application hold millwright's event kinds, expect
// states and action words to a copy of their own; this file holds the message
// classes and the need kinds. scripts/check-postern-fixtures.sh holds every
// copy to postern's.
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

// TestPosternContractMessageClasses holds the classes millwright sends or reads
// to the contract's message_classes. talk and call are the application
// package's unexported talkRecordClass and callRecordClass, said here by hand.
func TestPosternContractMessageClasses(t *testing.T) {
	ours := append([]string{}, application.PosternClasses...)
	ours = append(ours, application.CardClass, application.CardUpdateClass,
		application.PosternClassMoveHome, application.EventsClass, "talk", "call")
	sameSet(t, "message_classes", ours, contractList(t, "message_classes"))
}

// TestPosternContractNeedKinds holds the kinds of need the view lists to the
// contract's need_kinds.
func TestPosternContractNeedKinds(t *testing.T) {
	ours := []string{
		application.PosternNeedQuestion, application.PosternNeedApprove, application.PosternNeedVerify,
		application.PosternNeedStale, application.PosternNeedDemo, application.PosternNeedHands,
		application.PosternNeedReview, application.PosternNeedDecision, application.PosternNeedWaiting,
		application.PosternNeedChase, application.PosternNeedAlarm,
	}
	sameSet(t, "need_kinds", ours, contractList(t, "need_kinds"))
}
