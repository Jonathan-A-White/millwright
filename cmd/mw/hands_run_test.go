package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

// The Governor approves a hands step in Postern, end to end: his real
// signature over the step's real hash, delivered as a run action; mw
// postern inbox --apply checks it all, runs the step here as this host's
// user, records the run, comments the bead and sends the outcome back.
func TestPosternInboxApplyRunsAHandsStepTheGovernorApproved(t *testing.T) {
	f := loadPosternRecordFixture(t)
	governorPriv, err := ec.PrivateKeyFromWif(f.SenderWIF)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "ran")
	step := domain.HandsStep{ID: "touch", Host: "laptop", As: "user", Run: "echo from-hands && touch " + marker}
	steps, _ := json.Marshal([]application.HandsStepRecord{{HandsStep: step, AddedAt: "2026-09-28T11:00:00Z"}})
	sha := domain.HandsSHA256("mw-e.3", step)
	approvedAt := time.Now().Unix()
	sig, err := governorPriv.Sign(domain.HandsApprovalDigest(sha, approvedAt))
	if err != nil {
		t.Fatal(err)
	}
	der, _ := sig.ToDER()
	action, _ := json.Marshal(map[string]any{"action": "run", "bead": "mw-e.3", "step": "touch", "sha256": sha,
		"approved_at": approvedAt, "sig": hex.EncodeToString(der)})

	governorKeyFile := filepath.Join(t.TempDir(), "governor.key")
	if err := os.WriteFile(governorKeyFile, []byte(f.SenderWIF+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	governor := postern.NewCipher(postern.New(governorKeyFile))
	ct, err := governor.Encrypt(f.RecipientPubKey, string(action))
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(application.PosternPayload{V: 1, Kind: "msg", Class: "message",
		To: f.RecipientPubKey, From: f.SenderPubKey, Ts: approvedAt, Ct: ct})
	script, _ := postern.RecordScript(payload)
	messages, _ := json.Marshal(map[string]any{"records": []map[string]any{{
		"seq": 1, "txid": "direct:approve", "vout": 0, "scriptHex": hex.EncodeToString(*script), "signer": f.SenderPubKey,
	}}})
	var delivered []string
	challenges := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		switch {
		case r.URL.Path == "/api/challenge":
			challenges++
			fmt.Fprintf(w, `{"nonce":"n-%d"}`, challenges)
		case r.URL.RequestURI() == "/api/messages?since=0":
			w.Write(messages)
		case r.Method == http.MethodPost && r.URL.Path == "/api/messages":
			delivered = append(delivered, string(raw))
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"txid":"direct:outcome","seq":2}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":"no route"}`)
		}
	}))
	t.Cleanup(srv.Close)
	posternHome(t, srv.URL, f.RecipientWIF, f.SenderPubKey)
	t.Setenv("MW_POSTERN_CHANNEL", "direct") // the backend here takes direct records; the default is chain

	bin := t.TempDir()
	log := filepath.Join(bin, "calls.log")
	bd := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %q
case "$*" in
  *"kv get hands.mw-e.3"*) printf '%%s\n' %s; exit 0;;
  *"kv list"*) echo '{}'; exit 0;;
  *" get "*) echo 'not set' >&2; exit 1;;
  *" create "*) echo 'mw-mail-1'; exit 0;;
esac
exit 0
`, log, shellWord(string(steps)))
	if err := os.WriteFile(filepath.Join(bin, "bd"), []byte(bd), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	out, err := runPostern(t, "inbox", "--apply")
	if err != nil {
		t.Fatalf("mw postern inbox --apply failed: %v\n%s", err, out)
	}
	if strings.TrimSpace(out) != "applied run mw-e.3 txid direct:approve" {
		t.Fatalf("expected the run applied, got %q", out)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected the step to have run here: %v", err)
	}
	calls, _ := os.ReadFile(log)
	for _, want := range []string{
		"kv set hands.approval." + domain.HandsApprovalID(sha, approvedAt) + " direct:approve",
		`kv set hands.ran.mw-e.3.touch {"at":"`,
		"comment mw-e.3 RAN step touch on laptop as user, exit 0 (approved by the Governor via postern, txid direct:approve)",
		"from-hands",
		"create Ran: mw-e.3 touch, exit 0",
	} {
		if !strings.Contains(string(calls), want) {
			t.Errorf("expected bd to be asked %q, got:\n%s", want, calls)
		}
	}
	if strings.Contains(string(calls), "MAYOR via postern") {
		t.Errorf("expected the outcome not commented twice, got:\n%s", calls)
	}
	if len(delivered) != 1 {
		t.Fatalf("expected the outcome delivered back once, got %d", len(delivered))
	}
	var back struct {
		ScriptHex string `json:"scriptHex"`
	}
	_ = json.Unmarshal([]byte(delivered[0]), &back)
	sent, _ := postern.DecodeRecordScript(back.ScriptHex)
	var p application.PosternPayload
	_ = json.Unmarshal(sent, &p)
	text, _, err := governor.Decrypt(f.SenderWIF, p.Ct)
	if err != nil {
		t.Fatalf("the Governor cannot read the outcome: %v", err)
	}
	var body application.PosternThreadedMessage
	_ = json.Unmarshal([]byte(text), &body)
	if body.Thread.Bead != "mw-e.3" || body.Re != "direct:approve" || !strings.Contains(body.Text, "exit 0") || !strings.Contains(body.Text, "from-hands") {
		t.Fatalf("expected the outcome in the bead's thread re the approval, got %s", text)
	}
}
