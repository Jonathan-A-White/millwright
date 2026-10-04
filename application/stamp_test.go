package application_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

const (
	stampTestKey    = "02aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	stampTestSender = "03bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func stampForTest() domain.Stamp {
	return domain.Stamp{
		Rig:    "millwright",
		Branch: "main",
		Commit: "7b4430c1f2d9a8e3b5c6d7e8f9a0b1c2d3e4f506",
		Story:  "mw-oowxzy.1",
		Title:  "a chain stamp record",
		Host:   "laptop",
		At:     time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC),
	}
}

func sealedStamp(t *testing.T) (application.StampPayload, []byte, *apptest.FakeCipher) {
	t.Helper()
	cipher := apptest.NewFakeCipher()
	cipher.From = stampTestSender
	at := time.Date(2026, 10, 3, 12, 0, 5, 0, time.UTC)
	payload, raw, err := application.SealStamp(cipher, stampTestKey, stampTestSender, stampForTest(), at)
	if err != nil {
		t.Fatalf("SealStamp: %v", err)
	}
	return payload, raw, cipher
}

func TestAStampPayloadsKindIsStampAndItsClearPartIsTheCommitment(t *testing.T) {
	payload, raw, _ := sealedStamp(t)
	if payload.Kind != "stamp" {
		t.Fatalf("kind = %q, want stamp", payload.Kind)
	}
	if want := stampForTest().Commitment(); payload.Commitment != want {
		t.Fatalf("commitment = %q, want %q", payload.Commitment, want)
	}
	if payload.V != 1 || payload.To != stampTestKey || payload.From != stampTestSender || payload.Ts != 1791028805 {
		t.Fatalf("envelope = %+v", payload)
	}
	var onWire application.StampPayload
	if err := json.Unmarshal(raw, &onWire); err != nil || onWire != payload {
		t.Fatalf("the bytes are not the payload as JSON: %v, %+v", err, onWire)
	}
}

func TestAStampPayloadsClearBytesNameNeitherRigNorCommit(t *testing.T) {
	_, raw, _ := sealedStamp(t)
	// Everything but the sealed ct is in the clear: strip it, as the chain
	// reader sees the rest, and look for what must not be there.
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "ct")
	clear, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	s := stampForTest()
	for _, secret := range []string{s.Rig, s.Commit, s.Branch, s.Story, s.Title, s.Host} {
		if bytes.Contains(clear, []byte(secret)) {
			t.Errorf("the clear part %s names %q", clear, secret)
		}
	}
	if !strings.Contains(string(clear), s.Commitment()) {
		t.Errorf("the clear part %s lacks the commitment", clear)
	}
}

func TestASealedStampOpensToTheSameBody(t *testing.T) {
	_, raw, cipher := sealedStamp(t)
	got, from, err := application.OpenStamp(cipher, "a-private-key", raw)
	if err != nil {
		t.Fatalf("OpenStamp: %v", err)
	}
	if got != stampForTest() {
		t.Fatalf("opened %+v, want %+v", got, stampForTest())
	}
	if from != stampTestSender {
		t.Fatalf("envelope sender = %q, want %q", from, stampTestSender)
	}
}

func TestOpenStampRefusesABodyThatDoesNotMatchTheClearCommitment(t *testing.T) {
	payload, _, cipher := sealedStamp(t)
	other := stampForTest()
	other.Commit = strings.Repeat("0", 40)
	_, forged, err := application.SealStamp(cipher, stampTestKey, stampTestSender, other, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	var swapped application.StampPayload
	if err := json.Unmarshal(forged, &swapped); err != nil {
		t.Fatal(err)
	}
	swapped.Commitment = payload.Commitment // the original's commitment over another body
	raw, _ := json.Marshal(swapped)
	if _, _, err := application.OpenStamp(cipher, "a-private-key", raw); err == nil {
		t.Fatal("OpenStamp opened a body whose commitment is not the clear one")
	}
}

func TestOpenStampRefusesAnotherKind(t *testing.T) {
	_, raw, cipher := sealedStamp(t)
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	fields["kind"] = "msg"
	other, _ := json.Marshal(fields)
	if _, _, err := application.OpenStamp(cipher, "a-private-key", other); err == nil {
		t.Fatal("OpenStamp opened a record that is not a stamp")
	}
}

func TestSealStampReportsACipherFailure(t *testing.T) {
	cipher := apptest.NewFakeCipher()
	cipher.Err = errors.New("the key is gone")
	if _, _, err := application.SealStamp(cipher, stampTestKey, stampTestSender, stampForTest(), time.Unix(1, 0)); err == nil {
		t.Fatal("SealStamp hid a cipher failure")
	}
}
