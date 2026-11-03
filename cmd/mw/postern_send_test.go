package main

import (
	"crypto/sha256"
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

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

// directBackend stands in for a postern backend that takes direct
// deliveries and blob uploads, keeping what it was sent.
type directBackend struct {
	scripts []string
	blobs   [][]byte
}

func (d *directBackend) serve(t *testing.T) string {
	t.Helper()
	challenges := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/challenge":
			challenges++
			fmt.Fprintf(w, `{"nonce":"nonce-%d"}`, challenges)
		case r.Method == http.MethodPost && r.URL.Path == "/api/messages":
			var body struct {
				ScriptHex string `json:"scriptHex"`
			}
			_ = json.Unmarshal(raw, &body)
			d.scripts = append(d.scripts, body.ScriptHex)
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"txid":"direct:%064d","seq":%d}`, len(d.scripts), len(d.scripts))
		case r.Method == http.MethodPost && r.URL.Path == "/api/blobs":
			d.blobs = append(d.blobs, raw)
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"hash":%q,"size":%d}`, sha256Hex(raw), len(raw))
		default:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":"no such route: `+r.URL.RequestURI()+`"}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// On the direct channel mw postern send delivers the fixture's very record
// straight to the backend: no balance, no coins, no broadcast. The fixture's
// own fields are the same byte for byte; the direct record adds only the
// clear summary a bare message carries, at the end.
func TestPosternSendDeliversTheFixturesRecordScriptDirectly(t *testing.T) {
	f := loadPosternRecordFixture(t)
	backend := &directBackend{}
	posternHome(t, backend.serve(t), f.SenderWIF, f.RecipientPubKey)
	t.Setenv("MW_POSTERN_CHANNEL", "direct") // the backend here takes direct records; the default is chain
	realCipher, realClock := posternCipher, posternClock
	t.Cleanup(func() { posternCipher, posternClock = realCipher, realClock })
	posternCipher = func(*postern.KeyFile) application.Cipher { return fixedCipher{ct: f.Ct} }
	posternClock = func() time.Time { return time.Unix(f.Ts, 0) }

	out, err := runPostern(t, "send", "--class", f.Class, f.Text)
	if err != nil {
		t.Fatalf("mw postern send failed: %v\n%s", err, out)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "direct:") {
		t.Fatalf("expected the direct id printed, got %q", out)
	}
	if len(backend.scripts) != 1 {
		t.Fatalf("expected the fixture's record delivered once, got %v", backend.scripts)
	}
	fixture, ok := postern.DecodeRecordScript(f.ScriptHex)
	if !ok {
		t.Fatalf("the fixture's script is not a record: %s", f.ScriptHex)
	}
	sent, ok := postern.DecodeRecordScript(backend.scripts[0])
	if !ok {
		t.Fatalf("the delivered script is not a record: %s", backend.scripts[0])
	}
	if want := strings.TrimSuffix(string(fixture), "}") + `,"summary":"In Factory"}`; string(sent) != want {
		t.Fatalf("expected the fixture's record plus its summary\n%s\ngot\n%s", want, sent)
	}
}

// --attach uploads the file encrypted, then delivers the message announcing
// it; the text may be left out.
func TestPosternSendAttachesAFileAndMayLeaveTheTextOut(t *testing.T) {
	f := loadPosternRecordFixture(t)
	backend := &directBackend{}
	posternHome(t, backend.serve(t), f.SenderWIF, f.RecipientPubKey)
	t.Setenv("MW_POSTERN_CHANNEL", "direct") // the backend here takes direct records; the default is chain
	file := filepath.Join(t.TempDir(), "screen.png")
	if err := os.WriteFile(file, []byte("\x89PNG pixels"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runPostern(t, "send", "--attach", file)
	if err != nil {
		t.Fatalf("mw postern send --attach failed: %v\n%s", err, out)
	}
	if len(backend.blobs) != 1 || len(backend.scripts) != 1 {
		t.Fatalf("expected one upload and one delivery, got %d and %d", len(backend.blobs), len(backend.scripts))
	}
	if strings.Contains(string(backend.blobs[0]), "pixels") {
		t.Fatal("expected the upload to be ciphertext, not the file's own bytes")
	}
	if _, err := runPostern(t, "send"); err == nil {
		t.Fatal("expected mw postern send with neither text nor file to be refused")
	}
}

func sha256Hex(b []byte) string {
	return hex.EncodeToString(sha256Sum(b))
}

func sha256Sum(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}

// --file sends the file's bytes as the text, so a dollar sign or a backtick a
// shell would have eaten in a quoted argument arrives as written
// (mw-gq6.928).
func TestPosternSendFileSendsTheFilesBytesExactly(t *testing.T) {
	text := "$0.92 spent, `ticks`, \"quotes\", $HOME and \\n stay as written\nsecond line"
	file := filepath.Join(t.TempDir(), "post.txt")
	if err := os.WriteFile(file, []byte(text+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	plaintexts, _, out, err := channelSend(t, "--file", file)
	if err != nil {
		t.Fatalf("mw postern send --file failed: %v\n%s", err, out)
	}
	if len(plaintexts) != 1 {
		t.Fatalf("expected one message, got %v", plaintexts)
	}
	if plaintexts[0] != text {
		t.Fatalf("expected the file's text\n%q\ngot\n%q", text, plaintexts[0])
	}
}

// --file and a text argument are two texts: refused, and nothing is sent.
func TestPosternSendRefusesFileTogetherWithATextArgument(t *testing.T) {
	file := filepath.Join(t.TempDir(), "post.txt")
	if err := os.WriteFile(file, []byte("from the file"), 0o600); err != nil {
		t.Fatal(err)
	}
	plaintexts, _, out, err := channelSend(t, "--file", file, "from the argument")
	if err == nil {
		t.Fatalf("expected --file with a text argument to be refused, got:\n%s", out)
	}
	if !strings.Contains(err.Error(), "--file") || !strings.Contains(err.Error(), "text") || strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("expected the refusal to say --file and the text argument are two texts, got: %v", err)
	}
	if len(plaintexts) != 0 {
		t.Fatalf("expected nothing sent, got %v", plaintexts)
	}
}

// A file that cannot be read, or holds nothing, is refused and nothing is sent.
func TestPosternSendRefusesAFileItCannotReadOrThatIsEmpty(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(empty, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{filepath.Join(dir, "missing.txt"): "reading", empty: "nothing"} {
		plaintexts, _, out, err := channelSend(t, "--file", path)
		if err == nil {
			t.Fatalf("expected --file %s to be refused, got:\n%s", path, out)
		}
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("--file %s: expected the refusal to say %q, got: %v", path, want, err)
		}
		if len(plaintexts) != 0 {
			t.Fatalf("--file %s: expected nothing sent, got %v", path, plaintexts)
		}
	}
}
