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

// lengthSend runs mw postern send with args on the direct channel, keeping
// stdout and stderr apart.
func lengthSend(t *testing.T, args ...string) (plaintexts []string, stderr string, err error) {
	t.Helper()
	sent, _ := channelHome(t)
	var out, errOut strings.Builder
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(append([]string{"postern", "send"}, args...))
	err = root.Execute()
	return *sent, errOut.String(), err
}

func nWords(n int) string { return strings.TrimSpace(strings.Repeat("word ", n)) }

// A message over 200 words is refused with the count, the limit and --long
// named, and nothing is sent (mw-t5shinh.5).
func TestPosternSendRefusesAMessageOver200Words(t *testing.T) {
	plaintexts, _, err := lengthSend(t, nWords(201))
	if err == nil {
		t.Fatal("expected 201 words to be refused")
	}
	for _, want := range []string{"201 words", "200", "--long"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the refusal to name %q, got: %v", want, err)
		}
	}
	if len(plaintexts) != 0 {
		t.Fatalf("expected nothing sent, got %d", len(plaintexts))
	}
}

func TestPosternSendWarnsOnceOver80WordsAndSendsAnyway(t *testing.T) {
	plaintexts, stderr, err := lengthSend(t, nWords(81))
	if err != nil {
		t.Fatalf("expected 81 words to be sent: %v", err)
	}
	if len(plaintexts) != 1 {
		t.Fatalf("expected one send, got %d", len(plaintexts))
	}
	if strings.Count(stderr, "\n") != 1 || !strings.Contains(stderr, "81 words") || !strings.HasPrefix(stderr, "mw postern send:") {
		t.Fatalf("expected one warning line naming 81 on stderr, got %q", stderr)
	}
}

func TestPosternSendSays80WordsWithoutAWarning(t *testing.T) {
	plaintexts, stderr, err := lengthSend(t, nWords(80))
	if err != nil || len(plaintexts) != 1 {
		t.Fatalf("expected 80 words sent, got %d, %v", len(plaintexts), err)
	}
	if stderr != "" {
		t.Fatalf("expected no warning, got %q", stderr)
	}
}

func TestPosternSendCountsACardsQuestionAlone(t *testing.T) {
	opts := []string{"--option", nWords(90), "--option", nWords(90), "--option", nWords(90)}
	if _, stderr, err := lengthSend(t, append([]string{"--class", "decision-needed", nWords(60)}, opts...)...); err != nil || stderr != "" {
		t.Fatalf("expected a 60-word question with long options sent unwarned, got %q, %v", stderr, err)
	}
	if plaintexts, stderr, err := lengthSend(t, "--class", "decision-needed", nWords(61)); err != nil || len(plaintexts) != 1 || strings.Count(stderr, "\n") != 1 || !strings.Contains(stderr, "61 words") {
		t.Fatalf("expected a 61-word question sent with one warning naming 61, got %q, %v", stderr, err)
	}
	if plaintexts, _, err := lengthSend(t, "--class", "decision-needed", nWords(121)); err == nil || len(plaintexts) != 0 {
		t.Fatalf("expected a 121-word question refused, got %d sent, %v", len(plaintexts), err)
	}
}

func TestPosternSendLongSendsPastBothLimits(t *testing.T) {
	if plaintexts, stderr, err := lengthSend(t, "--long", nWords(500)); err != nil || len(plaintexts) != 1 {
		t.Fatalf("expected 500 words sent with --long, got %d, %v (%q)", len(plaintexts), err, stderr)
	}
	if plaintexts, _, err := lengthSend(t, "--long", "--class", "decision-needed", nWords(121)); err != nil || len(plaintexts) != 1 {
		t.Fatalf("expected a 121-word question sent with --long, got %d, %v", len(plaintexts), err)
	}
}

func TestPosternSendCountsAFilesTextLikeAnArgument(t *testing.T) {
	long := filepath.Join(t.TempDir(), "long.txt")
	if err := os.WriteFile(long, []byte(nWords(201)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if plaintexts, _, err := lengthSend(t, "--file", long); err == nil || !strings.Contains(err.Error(), "201 words") || len(plaintexts) != 0 {
		t.Fatalf("expected a 201-word file refused, got %d sent, %v", len(plaintexts), err)
	}
	warned := filepath.Join(t.TempDir(), "warn.txt")
	if err := os.WriteFile(warned, []byte(nWords(81)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if plaintexts, stderr, err := lengthSend(t, "--file", warned); err != nil || len(plaintexts) != 1 || !strings.Contains(stderr, "81 words") {
		t.Fatalf("expected an 81-word file sent with a warning, got %d, %q, %v", len(plaintexts), stderr, err)
	}
}

func TestPosternSendHelpNamesTheFourLimitsAndLong(t *testing.T) {
	var out strings.Builder
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"postern", "send", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--long", "80", "200", "60", "120"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("expected --help to name %q:\n%s", want, out.String())
		}
	}
}
