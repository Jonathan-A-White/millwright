package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bsv-blockchain/go-sdk/transaction"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/bsv-blockchain/go-sdk/script"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

// posternRecordFixture is infrastructure/postern/testdata/protocol-vectors.json
// (postern's own committed fixture, docs/protocol.md section 5), reshaped
// into the WIF and address forms these tests build an mw home around.
type posternRecordFixture struct {
	SenderWIF       string
	SenderPubKey    string
	SenderAddress   string
	RecipientWIF    string
	RecipientPubKey string
	Text            string
	Class           string
	Ts              int64
	Ct              string
	ScriptHex       string
}

func loadPosternRecordFixture(t *testing.T) posternRecordFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "infrastructure", "postern", "testdata", "protocol-vectors.json"))
	if err != nil {
		t.Fatalf("reading the protocol fixture: %v", err)
	}
	var f struct {
		Inputs struct {
			SenderPrivateKeyHex    string `json:"senderPrivateKeyHex"`
			SenderPublicKeyHex     string `json:"senderPublicKeyHex"`
			RecipientPrivateKeyHex string `json:"recipientPrivateKeyHex"`
			RecipientPublicKeyHex  string `json:"recipientPublicKeyHex"`
			Plaintext              string `json:"plaintext"`
		} `json:"inputs"`
		EncryptMessage struct {
			Class string `json:"class"`
			Ts    int64  `json:"ts"`
			Ct    string `json:"ct"`
		} `json:"encryptMessage"`
		RecordScriptHex string `json:"recordScriptHex"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("parsing the protocol fixture: %v", err)
	}
	senderPriv, err := ec.PrivateKeyFromHex(f.Inputs.SenderPrivateKeyHex)
	if err != nil {
		t.Fatalf("parsing the fixture's sender key: %v", err)
	}
	recipientPriv, err := ec.PrivateKeyFromHex(f.Inputs.RecipientPrivateKeyHex)
	if err != nil {
		t.Fatalf("parsing the fixture's recipient key: %v", err)
	}
	senderAddr, err := script.NewAddressFromPublicKeyString(f.Inputs.SenderPublicKeyHex, false)
	if err != nil {
		t.Fatalf("deriving the fixture sender's testnet address: %v", err)
	}
	return posternRecordFixture{
		SenderWIF:       senderPriv.WifPrefix(byte(ec.TestNet)),
		SenderPubKey:    f.Inputs.SenderPublicKeyHex,
		SenderAddress:   senderAddr.AddressString,
		RecipientWIF:    recipientPriv.WifPrefix(byte(ec.TestNet)),
		RecipientPubKey: f.Inputs.RecipientPublicKeyHex,
		Text:            f.Inputs.Plaintext,
		Class:           f.EncryptMessage.Class,
		Ts:              f.EncryptMessage.Ts,
		Ct:              f.EncryptMessage.Ct,
		ScriptHex:       f.RecordScriptHex,
	}
}

// posternHome sets up a temp HOME whose config points mw at backend and at a
// key file holding wif, and puts a stand-in for bd first on PATH that has no
// notes and takes every note it is given, so the inbox cursor starts at 0.
func posternHome(t *testing.T, backend, wif, governorKey string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	keyFile := filepath.Join(t.TempDir(), "postern.key")
	if err := os.WriteFile(keyFile, []byte(wif+"\n"), 0o600); err != nil {
		t.Fatalf("writing the key file: %v", err)
	}
	mwConfig(t, fmt.Sprintf("vault = %q\nhost = \"laptop\"\npostern_backend = %q\npostern_key_file = %q\npostern_governor_key = %q\n",
		t.TempDir(), backend, keyFile, governorKey))
	for _, env := range []string{"MW_POSTERN_BACKEND", "MW_POSTERN_FLOAT_SATS", "MW_POSTERN_GOVERNOR_KEY", "MW_POSTERN_KEY_FILE"} {
		t.Setenv(env, "")
	}

	bin := t.TempDir()
	script := "#!/bin/sh\nfor a in \"$@\"; do\n  if [ \"$a\" = \"get\" ]; then\n    echo \"postern.inbox.cursor (not set)\" >&2\n    exit 1\n  fi\ndone\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "bd"), []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in for bd: %v", err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// fakePosternBackend stands in for the postern backend's /api (postern's
// docs/api.md): each route a canned JSON answer, and the last broadcast kept.
func fakePosternBackend(t *testing.T, routes map[string]string) (url string, broadcast *string) {
	t.Helper()
	var sent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/broadcast" {
			var body struct {
				Rawtx string `json:"rawtx"`
			}
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			sent = body.Rawtx
			io.WriteString(w, `{"txid":"f00dfeed"}`)
			return
		}
		answer, ok := routes[r.URL.RequestURI()]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":"no such route: `+r.URL.RequestURI()+`"}`)
			return
		}
		io.WriteString(w, answer)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &sent
}

func runPostern(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"postern"}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestPosternInboxPrintsTheTextOfARecordFromTheBackend(t *testing.T) {
	f := loadPosternRecordFixture(t)
	messages, _ := json.Marshal(map[string]any{
		"records": []map[string]any{{"seq": 1, "txid": "3af1", "vout": 0, "scriptHex": f.ScriptHex, "height": 0, "firstSeen": "2026-09-24T12:00:03Z"}},
		"next":    1,
	})
	url, _ := fakePosternBackend(t, map[string]string{"/api/messages?since=0": string(messages)})
	posternHome(t, url, f.RecipientWIF, "")

	out, err := runPostern(t, "inbox")
	if err != nil {
		t.Fatalf("mw postern inbox failed: %v\n%s", err, out)
	}
	for _, want := range []string{f.Text, f.Class, f.SenderPubKey, "3af1"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected mw postern inbox to print %q, got:\n%s", want, out)
		}
	}
}

// fixedCipher encrypts every text to one known ciphertext, so the record a
// send builds can be compared with the fixture byte for byte.
type fixedCipher struct{ ct string }

func (c fixedCipher) Encrypt(string, string) (string, error) { return c.ct, nil }
func (c fixedCipher) Decrypt(string, string) (string, error) {
	return "", fmt.Errorf("fixedCipher does not decrypt")
}

func TestPosternSendBroadcastsTheFixturesRecordScript(t *testing.T) {
	f := loadPosternRecordFixture(t)
	url, broadcast := fakePosternBackend(t, map[string]string{
		"/api/balance/" + f.SenderAddress: `{"confirmed":10000,"unconfirmed":0}`,
		"/api/utxos/" + f.SenderAddress:   `{"utxos":[{"txid":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","vout":0,"satoshis":10000,"height":100}]}`,
	})
	posternHome(t, url, f.SenderWIF, f.RecipientPubKey)
	realCipher, realClock := posternCipher, posternClock
	t.Cleanup(func() { posternCipher, posternClock = realCipher, realClock })
	posternCipher = func(*postern.KeyFile) application.Cipher { return fixedCipher{ct: f.Ct} }
	posternClock = func() time.Time { return time.Unix(f.Ts, 0) }

	out, err := runPostern(t, "send", "--class", f.Class, f.Text)
	if err != nil {
		t.Fatalf("mw postern send failed: %v\n%s", err, out)
	}
	if strings.TrimSpace(out) != "f00dfeed" {
		t.Errorf("expected mw postern send to print the backend's txid, got:\n%s", out)
	}
	tx, err := transaction.NewTransactionFromHex(*broadcast)
	if err != nil {
		t.Fatalf("the backend was sent something that is not a transaction (%q): %v", *broadcast, err)
	}
	if got := hex.EncodeToString(*tx.Outputs[0].LockingScript); got != f.ScriptHex {
		t.Fatalf("expected the record output's script to be the fixture's\n%s\ngot\n%s", f.ScriptHex, got)
	}
}

// TestPosternSendDefaultsClassToMessage checks that mw postern send with no
// --class sends class message, the same record the fixture's class "message"
// vector expects, rather than refusing for want of a class.
func TestPosternSendDefaultsClassToMessage(t *testing.T) {
	f := loadPosternRecordFixture(t)
	url, broadcast := fakePosternBackend(t, map[string]string{
		"/api/balance/" + f.SenderAddress: `{"confirmed":10000,"unconfirmed":0}`,
		"/api/utxos/" + f.SenderAddress:   `{"utxos":[{"txid":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","vout":0,"satoshis":10000,"height":100}]}`,
	})
	posternHome(t, url, f.SenderWIF, f.RecipientPubKey)
	realCipher, realClock := posternCipher, posternClock
	t.Cleanup(func() { posternCipher, posternClock = realCipher, realClock })
	posternCipher = func(*postern.KeyFile) application.Cipher { return fixedCipher{ct: f.Ct} }
	posternClock = func() time.Time { return time.Unix(f.Ts, 0) }

	out, err := runPostern(t, "send", f.Text)
	if err != nil {
		t.Fatalf("mw postern send failed: %v\n%s", err, out)
	}
	if strings.TrimSpace(out) != "f00dfeed" {
		t.Errorf("expected mw postern send to print the backend's txid, got:\n%s", out)
	}
	tx, err := transaction.NewTransactionFromHex(*broadcast)
	if err != nil {
		t.Fatalf("the backend was sent something that is not a transaction (%q): %v", *broadcast, err)
	}
	if got := hex.EncodeToString(*tx.Outputs[0].LockingScript); got != f.ScriptHex {
		t.Fatalf("expected the record output's script to be the fixture's\n%s\ngot\n%s", f.ScriptHex, got)
	}
}

// noBdCalls puts a stand-in bd ahead of PATH that logs every call it gets to
// callLog, so a test can say bd was never called.
func noBdCalls(t *testing.T) (callLog string) {
	t.Helper()
	callLog = filepath.Join(t.TempDir(), "bd-calls.log")
	bin := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\necho \"$@\" >>%q\nexit 0\n", callLog)
	if err := os.WriteFile(filepath.Join(bin, "bd"), []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in for bd: %v", err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return callLog
}

// assertNoBdCalls fails the test if the stand-in bd from noBdCalls was ever
// called.
func assertNoBdCalls(t *testing.T, callLog string) {
	t.Helper()
	if data, err := os.ReadFile(callLog); err == nil {
		t.Fatalf("expected no bd call, but bd was called:\n%s", data)
	} else if !os.IsNotExist(err) {
		t.Fatalf("reading the bd call log: %v", err)
	}
}

func TestPosternSnapshotRefusesWithoutAGovernorKeyBeforeCallingBd(t *testing.T) {
	posternHome(t, "http://unused", "unused-wif", "")
	callLog := noBdCalls(t)

	out, err := runPostern(t, "snapshot")
	if err == nil {
		t.Fatalf("expected mw postern snapshot to refuse without a governor key, got:\n%s", out)
	}
	if !strings.Contains(err.Error(), "postern_governor_key") {
		t.Fatalf("expected the refusal to name postern_governor_key, got: %v", err)
	}
	assertNoBdCalls(t, callLog)
}

func TestPosternSnapshotRefusesWithoutAKeyFileBeforeCallingBd(t *testing.T) {
	f := loadPosternRecordFixture(t)
	posternHome(t, "http://unused", "unused-wif", f.RecipientPubKey)
	t.Setenv("MW_POSTERN_KEY_FILE", filepath.Join(t.TempDir(), "missing.key"))
	callLog := noBdCalls(t)

	out, err := runPostern(t, "snapshot")
	if err == nil {
		t.Fatalf("expected mw postern snapshot to refuse without a key file, got:\n%s", out)
	}
	if !strings.Contains(err.Error(), "no postern key") {
		t.Fatalf("expected the refusal to name the missing postern key, got: %v", err)
	}
	assertNoBdCalls(t, callLog)
}

func TestPosternSnapshotJSONPrintsThePlaintextWithoutWritingAnything(t *testing.T) {
	posternHome(t, "http://unused", "", "")

	snapshotPath := filepath.Join(t.TempDir(), "snapshot.bin")
	t.Setenv("MW_POSTERN_SNAPSHOT_PATH", snapshotPath)

	out, err := runPostern(t, "snapshot", "--json")
	if err != nil {
		t.Fatalf("mw postern snapshot --json failed: %v\n%s", err, out)
	}
	var doc struct {
		WrittenAt string `json:"written_at"`
		Epics     []any  `json:"epics"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("expected --json to print valid JSON, got %q: %v", out, err)
	}
	if doc.WrittenAt == "" {
		t.Errorf("expected written_at to be set, got %q", out)
	}
	if len(doc.Epics) != 0 {
		t.Errorf("expected no live epics from the empty stand-in, got %v", doc.Epics)
	}
	if _, err := os.Stat(snapshotPath); !os.IsNotExist(err) {
		t.Fatalf("expected --json to write nothing, but %s exists", snapshotPath)
	}
}

func TestPosternSnapshotWritesTheEncryptedFileAtomically(t *testing.T) {
	posternHome(t, "http://unused", "", "governor-pubkey-hex")

	snapshotPath := filepath.Join(t.TempDir(), "state", "snapshot.bin")
	t.Setenv("MW_POSTERN_SNAPSHOT_PATH", snapshotPath)

	realCipher := posternCipher
	t.Cleanup(func() { posternCipher = realCipher })
	posternCipher = func(*postern.KeyFile) application.Cipher { return fixedCipher{ct: "ZmFrZS1jaXBoZXJ0ZXh0"} }

	out, err := runPostern(t, "snapshot")
	if err != nil {
		t.Fatalf("mw postern snapshot failed: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("reading the written snapshot: %v", err)
	}
	if string(raw) != "ZmFrZS1jaXBoZXJ0ZXh0" {
		t.Fatalf("expected the encrypted ciphertext on disk, got %q", raw)
	}
	if _, err := os.Stat(snapshotPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("expected no temp file left behind, stat gave: %v", err)
	}
}
