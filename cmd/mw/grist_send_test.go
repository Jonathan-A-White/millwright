package main

import (
	"bytes"
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

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

// gristSendVectors are the keys of the grist vectors, as key files, and the
// mill's answers.
type gristSendVectors struct {
	millPub, phonePub string
	millKeys          *postern.KeyFile
	phoneKeyFile      string
	answers           map[string]application.GristAnswer
}

func loadGristSendVectors(t *testing.T) gristSendVectors {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "application", "testdata", "grist-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Keys map[string]struct {
			PrivateKeyHex string `json:"privateKeyHex"`
			PublicKeyHex  string `json:"publicKeyHex"`
		} `json:"keys"`
		Answers map[string]application.GristAnswer `json:"answers"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	keyFile := func(name string) string {
		secret, _ := hex.DecodeString(v.Keys[name].PrivateKeyHex)
		priv, _ := ec.PrivateKeyFromBytes(secret)
		path := filepath.Join(t.TempDir(), name+".key")
		if err := os.WriteFile(path, []byte(priv.WifPrefix(byte(ec.TestNet))+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	return gristSendVectors{
		millPub: v.Keys["mill"].PublicKeyHex, phonePub: v.Keys["cairnPhone"].PublicKeyHex,
		millKeys: postern.New(keyFile("mill")), phoneKeyFile: keyFile("cairnPhone"), answers: v.Answers,
	}
}

// gristBackend stands in for the postern backend a grist is sent to: it
// names the mill at /api/me, takes blobs and grist, and, when answer is not
// empty, holds the mill's answer in that status to the grist it took.
type gristBackend struct {
	v       gristSendVectors
	answer  string
	blobs   int
	grist   int
	txid    string
	records []map[string]any
}

func (g *gristBackend) serve(t *testing.T) string {
	t.Helper()
	challenges := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		switch {
		case r.URL.Path == "/api/challenge":
			challenges++
			fmt.Fprintf(w, `{"nonce":"nonce-%d"}`, challenges)
		case r.URL.Path == "/api/me":
			fmt.Fprintf(w, `{"pubkey":%q,"mill":%q,"network":"testnet","features":["grist"],"apps":["cairn"]}`, g.v.phonePub, g.v.millPub)
		case r.Method == http.MethodPost && r.URL.Path == "/api/blobs":
			g.blobs++
			sum := sha256.Sum256(raw)
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"hash":%q,"size":%d}`, hex.EncodeToString(sum[:]), len(raw))
		case r.Method == http.MethodPost && r.URL.Path == "/api/messages":
			g.grist++
			g.txid = fmt.Sprintf("direct:%064d", g.grist)
			if g.answer != "" {
				g.records = append(g.records, g.answerRecord(t))
			}
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"txid":%q,"seq":%d}`, g.txid, g.grist)
		case r.Method == http.MethodGet && r.URL.Path == "/api/messages":
			json.NewEncoder(w).Encode(map[string]any{"records": g.records, "next": len(g.records)})
		default:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":"no such route: `+r.URL.RequestURI()+`"}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// answerRecord is the mill's answer to the grist just taken, sealed to the
// phone, as the backend indexes it.
func (g *gristBackend) answerRecord(t *testing.T) map[string]any {
	t.Helper()
	answer := g.v.answers[g.answer]
	answer.Re = g.txid
	body, _ := json.Marshal(answer)
	sealed, err := postern.NewCipher(g.v.millKeys).Encrypt(g.v.phonePub, string(body))
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(application.PosternPayload{V: 1, Kind: application.PosternMessageKind, Class: application.GristClass,
		To: g.v.phonePub, From: g.v.millPub, Ts: 1_790_000_000, Ct: sealed})
	record, err := postern.RecordScript(payload)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"seq": g.grist + 100, "txid": "direct:answer", "scriptHex": hex.EncodeToString(*record), "signer": g.v.millPub}
}

// gristSendArgs writes a request and a photo, and is the mw grist send
// arguments that send them as the phone to the backend at url.
func gristSendArgs(t *testing.T, v gristSendVectors, url string) []string {
	t.Helper()
	dir := t.TempDir()
	request := filepath.Join(dir, "request.json")
	photo := filepath.Join(dir, "drawer.jpg")
	if err := os.WriteFile(request, []byte(`{"schemaVersion":"1.1","requestType":"sweep"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(photo, []byte("jpeg"), 0o600); err != nil {
		t.Fatal(err)
	}
	return []string{"send", "--key", v.phoneKeyFile, "--app", "cairn", "--kind", "sweep", "--request", request, "--photo", photo, "--backend", url}
}

// runGristSend runs mw grist send, reporting standard output and error
// apart, and the status mw would leave with.
func runGristSend(t *testing.T, args ...string) (stdout, stderr string, status int) {
	t.Helper()
	var out, errs bytes.Buffer
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(&errs)
	root.SetArgs(append([]string{"grist"}, args...))
	err := root.Execute()
	return out.String(), errs.String(), exitStatus(err)
}

// mw grist send, waiting, prints the answer the mill gave and leaves with
// the status its outcome calls for; with no --wait it prints the id alone.
func TestGristSendAsAGivenKeyWaitsForTheAnswer(t *testing.T) {
	mwConfig(t, "host = \"laptop\"\n")
	v := loadGristSendVectors(t)
	for _, tc := range []struct {
		answer string
		status int
	}{{"answered", 0}, {"refused", 1}, {"failed", 1}} {
		t.Run(tc.answer, func(t *testing.T) {
			b := &gristBackend{v: v, answer: tc.answer}
			out, errs, status := runGristSend(t, append(gristSendArgs(t, v, b.serve(t)), "--wait", "1m")...)
			if status != tc.status {
				t.Fatalf("expected status %d, got %d\nstdout: %s\nstderr: %s", tc.status, status, out, errs)
			}
			var printed application.GristAnswer
			if err := json.Unmarshal([]byte(out), &printed); err != nil || printed.Status != tc.answer || printed.Re != b.txid {
				t.Fatalf("expected the %s answer to %s printed as JSON alone, got %q: %v", tc.answer, b.txid, out, err)
			}
			if tc.status == 1 && !strings.Contains(errs, v.answers[tc.answer].Reason) {
				t.Fatalf("expected the reason on standard error, got %q", errs)
			}
			if b.blobs != 1 || b.grist != 1 {
				t.Fatalf("expected one blob and one grist, got %d and %d", b.blobs, b.grist)
			}
		})
	}
}

func TestGristSendWithNoAnswerInTimeLeavesWith2(t *testing.T) {
	mwConfig(t, "host = \"laptop\"\n")
	v := loadGristSendVectors(t)
	b := &gristBackend{v: v}
	out, errs, status := runGristSend(t, append(gristSendArgs(t, v, b.serve(t)), "--wait", "1ms")...)
	if status != 2 || out != "" || !strings.Contains(errs, "no answer to "+b.txid) {
		t.Fatalf("expected status 2, nothing printed and no answer said, got %d\nstdout: %s\nstderr: %s", status, out, errs)
	}
}

func TestGristSendWithoutWaitPrintsTheIDOnly(t *testing.T) {
	mwConfig(t, "host = \"laptop\"\n")
	v := loadGristSendVectors(t)
	b := &gristBackend{v: v}
	out, errs, status := runGristSend(t, gristSendArgs(t, v, b.serve(t))...)
	if status != 0 || strings.TrimSpace(out) != b.txid {
		t.Fatalf("expected status 0 and the id alone, got %d\nstdout: %s\nstderr: %s", status, out, errs)
	}
}

// --backend defaults to postern_backend, and --key, which has no default, is
// asked for by name.
func TestGristSendReadsTheBackendFromConfigAndNeedsAKey(t *testing.T) {
	v := loadGristSendVectors(t)
	b := &gristBackend{v: v}
	mwConfig(t, fmt.Sprintf("host = \"laptop\"\npostern_backend = %q\n", b.serve(t)))
	t.Setenv("MW_POSTERN_BACKEND", "")
	args := gristSendArgs(t, v, "")
	args = args[:len(args)-2] // no --backend
	if out, errs, status := runGristSend(t, args...); status != 0 {
		t.Fatalf("expected the configured backend used, got %d\nstdout: %s\nstderr: %s", status, out, errs)
	}
	if _, errs, status := runGristSend(t, "send", "--app", "cairn", "--kind", "sweep", "--request", "r.json"); status != 1 || !strings.Contains(errs, `"key" not set`) {
		t.Fatalf("expected --key asked for, got %d: %s", status, errs)
	}
}

func TestGristSendHelpDocumentsEveryFlag(t *testing.T) {
	out, _, status := runGristSend(t, "send", "--help")
	if status != 0 {
		t.Fatalf("mw grist send --help left with %d", status)
	}
	for _, flag := range []string{"--key", "--app", "--kind", "--request", "--photo", "--backend", "--wait", "--schema-version"} {
		line := ""
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(l, flag+" ") {
				line = l
			}
		}
		if strings.TrimSpace(strings.SplitN(line, flag, 2)[len(strings.SplitN(line, flag, 2))-1]) == "" {
			t.Errorf("expected --help to describe %s, got:\n%s", flag, out)
		}
	}
}
