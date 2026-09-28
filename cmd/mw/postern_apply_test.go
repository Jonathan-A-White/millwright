package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

// bdRecording puts a stand-in for bd first on PATH that answers bd show with
// shown, lists no notes, mints an id for whatever it creates, takes every
// write, and logs every call; it returns the log's path.
func bdRecording(t *testing.T, shown string) string {
	t.Helper()
	bin := t.TempDir()
	log := filepath.Join(bin, "calls.log")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %q
for a in "$@"; do
  case "$a" in
    show) printf '%%s\n' %s; exit 0;;
    list) echo '{}'; exit 0;;
    create) echo 'mw-mail-1'; exit 0;;
    get) echo 'not set' >&2; exit 1;;
    update|comment|set|clear) exit 0;;
  esac
done
exit 0
`, log, shellWord(shown))
	if err := os.WriteFile(filepath.Join(bin, "bd"), []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in for bd: %v", err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

// mw postern inbox --apply releases a held story on the Governor's tap, in
// one pass, with the real cipher and record script end to end: it updates
// the story, comments it, mails the Mayor and marks the txid applied, and
// moves no cursor.
func TestPosternInboxApplyReleasesAHeldStoryOnTheGovernorsTap(t *testing.T) {
	f := loadPosternRecordFixture(t)
	governorKeyFile := filepath.Join(t.TempDir(), "governor.key")
	if err := os.WriteFile(governorKeyFile, []byte(f.SenderWIF+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	governor := postern.NewCipher(postern.New(governorKeyFile))
	ct, err := governor.Encrypt(f.RecipientPubKey, `{"action":"release","bead":"mw-e.1"}`)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(application.PosternPayload{V: 1, Kind: "msg", Class: "message",
		To: f.RecipientPubKey, From: f.SenderPubKey, Ts: 1758800000, Ct: ct})
	script, err := postern.RecordScript(payload)
	if err != nil {
		t.Fatal(err)
	}
	messages, _ := json.Marshal(map[string]any{"records": []map[string]any{{
		"seq": 1, "txid": "direct:tap", "vout": 0, "scriptHex": hex.EncodeToString(*script), "signer": f.SenderPubKey,
	}}})
	url, _ := fakePosternBackend(t, map[string]string{"/api/messages?since=0": string(messages)})
	posternHome(t, url, f.RecipientWIF, f.SenderPubKey)
	log := bdRecording(t, `[{"id": "mw-e.1", "title": "Held", "status": "deferred", "issue_type": "task", "parent": "mw-e"}]`)

	out, err := runPostern(t, "inbox", "--apply")
	if err != nil {
		t.Fatalf("mw postern inbox --apply failed: %v\n%s", err, out)
	}
	if strings.TrimSpace(out) != "applied release mw-e.1 txid direct:tap" {
		t.Fatalf("expected one applied line, got %q", out)
	}
	calls, _ := os.ReadFile(log)
	for _, want := range []string{
		"update mw-e.1 --status open",
		"comment mw-e.1 RELEASED by the Governor via postern, txid direct:tap",
		"create Released: mw-e.1",
		"kv set postern.applied.direct:tap applied release mw-e.1 txid direct:tap",
	} {
		if !strings.Contains(string(calls), want) {
			t.Errorf("expected bd to be asked %q, got:\n%s", want, calls)
		}
	}
	if strings.Contains(string(calls), "kv set postern.inbox.cursor") {
		t.Errorf("expected the cursor left where it was, got:\n%s", calls)
	}
}
