package main

import (
	"encoding/base64"
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

// A Governor's voice note, end to end: the audio is fetched from the blob
// store and decrypted, postern_transcribe_cmd hears it, the transcript is
// written on the bead and delivered back to the Governor in the same thread.
func TestPosternInboxApplyHearsAVoiceNote(t *testing.T) {
	f := loadPosternRecordFixture(t)
	governorKeyFile := filepath.Join(t.TempDir(), "governor.key")
	if err := os.WriteFile(governorKeyFile, []byte(f.SenderWIF+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	governor := postern.NewCipher(postern.New(governorKeyFile))
	audio := []byte("OggS the voice of the Governor")
	sealed, err := governor.EncryptBytes(f.RecipientPubKey, audio)
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := base64.StdEncoding.DecodeString(sealed)
	hash := sha256Hex(blob)
	body, _ := json.Marshal(application.PosternThreadedMessage{
		Thread:     application.PosternThread{Bead: "mw-e.3"},
		Attachment: &application.PosternAttachment{Hash: hash, Size: int64(len(blob)), Mime: "audio/ogg"},
	})
	ct, err := governor.Encrypt(f.RecipientPubKey, string(body))
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(application.PosternPayload{V: 1, Kind: "msg", Class: "message",
		To: f.RecipientPubKey, From: f.SenderPubKey, Ts: 1758800000, Ct: ct})
	script, _ := postern.RecordScript(payload)
	messages, _ := json.Marshal(map[string]any{"records": []map[string]any{{
		"seq": 1, "txid": "direct:voice", "vout": 0, "scriptHex": hex.EncodeToString(*script), "signer": f.SenderPubKey,
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
		case r.Method == http.MethodGet && r.URL.Path == "/api/blobs/"+hash:
			w.Write(blob)
		case r.Method == http.MethodPost && r.URL.Path == "/api/messages":
			delivered = append(delivered, string(raw))
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"txid":"direct:back","seq":2}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":"no route"}`)
		}
	}))
	t.Cleanup(srv.Close)
	posternHome(t, srv.URL, f.RecipientWIF, f.SenderPubKey)
	t.Setenv("MW_POSTERN_CHANNEL", "direct") // the backend here takes direct records; the default is chain
	log := bdRecording(t, `[{"id": "mw-e.3", "status": "open", "issue_type": "task"}]`)
	hear := filepath.Join(t.TempDir(), "hear")
	if err := os.WriteFile(hear, []byte("#!/bin/sh\ncase \"$(cat \"$1\")\" in *Governor*) echo '  ship it  ';; *) exit 3;; esac\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MW_POSTERN_TRANSCRIBE_CMD", hear)

	out, err := runPostern(t, "inbox", "--apply")
	if err != nil {
		t.Fatalf("mw postern inbox --apply failed: %v\n%s", err, out)
	}
	if strings.TrimSpace(out) != "applied voice mw-e.3 txid direct:voice" {
		t.Fatalf("expected the voice note applied, got %q", out)
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "comment mw-e.3 GOVERNOR (voice) via postern, txid direct:voice: ship it") {
		t.Fatalf("expected the transcript written on the bead, got:\n%s", calls)
	}
	if len(delivered) != 1 {
		t.Fatalf("expected the transcript delivered back once, got %d", len(delivered))
	}
	var back struct {
		ScriptHex string `json:"scriptHex"`
	}
	_ = json.Unmarshal([]byte(delivered[0]), &back)
	sentPayload, ok := postern.DecodeRecordScript(back.ScriptHex)
	if !ok {
		t.Fatalf("expected a record script delivered, got %s", delivered[0])
	}
	var p application.PosternPayload
	_ = json.Unmarshal(sentPayload, &p)
	text, _, err := governor.Decrypt(f.SenderWIF, p.Ct)
	if err != nil {
		t.Fatalf("the Governor cannot read the transcript sent back: %v", err)
	}
	if text != `{"thread":{"bead":"mw-e.3"},"text":"ship it","re":"direct:voice","role":"transcript"}` {
		t.Fatalf("expected section 14's transcript body, got %s", text)
	}
}
