package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// wirePrompt is a prompt as the backend holds it (postern's server/README.md,
// 'Saved prompts'): the signature is a list of option objects.
type wirePrompt struct {
	Name      string `json:"name"`
	Summary   string `json:"summary"`
	Signature []struct {
		Flag     string `json:"flag"`
		Type     string `json:"type"`
		Default  string `json:"default"`
		Required bool   `json:"required"`
	} `json:"signature"`
	Body string `json:"body"`
}

// promptBackend stands in for the postern backend's /api/prompts, keeping what
// is PUT, and answering the challenge every call is proven with.
func promptBackend(t *testing.T) (url string, kept map[string]wirePrompt) {
	t.Helper()
	kept = map[string]wirePrompt{}
	var challenges int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/challenge":
			challenges++
			fmt.Fprintf(w, `{"nonce":"nonce-%d"}`, challenges)
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/prompts/"):
			var p wirePrompt
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &p); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			kept[strings.TrimPrefix(r.URL.Path, "/api/prompts/")] = p
			io.WriteString(w, `{}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/prompts":
			list := []wirePrompt{}
			for _, p := range kept {
				list = append(list, p)
			}
			json.NewEncoder(w).Encode(list)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/prompts/"):
			p, ok := kept[strings.TrimPrefix(r.URL.Path, "/api/prompts/")]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				io.WriteString(w, `{"error":"no such prompt"}`)
				return
			}
			json.NewEncoder(w).Encode(p)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL, kept
}

func runPrompt(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"prompt"}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestPromptSaveListShowAndARunWithABadOptionAreRefusedByTheSignature(t *testing.T) {
	f := loadPosternRecordFixture(t)
	url, kept := promptBackend(t)
	posternHome(t, url, f.SenderWIF, f.RecipientPubKey)

	draft := filepath.Join(t.TempDir(), "top5.md")
	if err := os.WriteFile(draft, []byte("Name the top <count> things.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runPrompt(t, "save", "top5", "--summary", "The five next", "--option", "count:int=5", "--body-file", draft)
	if err != nil {
		t.Fatalf("mw prompt save failed: %v\n%s", err, out)
	}
	got := kept["top5"]
	if got.Name != "top5" || got.Summary != "The five next" || got.Body != "Name the top <count> things.\n" ||
		len(got.Signature) != 1 || got.Signature[0].Flag != "--count" || got.Signature[0].Type != "int" || got.Signature[0].Default != "5" {
		t.Fatalf("expected the backend to hold top5 with option --count int 5, got %+v", got)
	}

	out, err = runPrompt(t, "list")
	if err != nil || !strings.Contains(out, "top5\tThe five next\t--count:int=5") {
		t.Fatalf("expected the list to give the prompt on a line, got %v:\n%s", err, out)
	}
	out, err = runPrompt(t, "show", "top5")
	if err != nil || !strings.Contains(out, "Name the top <count> things.") {
		t.Fatalf("expected show to print the body, got %v:\n%s", err, out)
	}

	out, err = runPrompt(t, "run", "top5", "--bogus", "1")
	if err == nil || !strings.Contains(err.Error(), "--bogus is not an option") || !strings.Contains(err.Error(), "its signature is --count:int=5") {
		t.Fatalf("expected a bad option refused, naming the signature, got %v\n%s", err, out)
	}
	if strings.Contains(out, "PROMPT /") {
		t.Fatalf("expected no prompt printed for a refused call, got:\n%s", out)
	}
}
