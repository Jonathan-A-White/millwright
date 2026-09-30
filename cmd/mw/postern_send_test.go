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
	if want := strings.TrimSuffix(string(fixture), "}") + `,"summary":"Message"}`; string(sent) != want {
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
