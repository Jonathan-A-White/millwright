package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

func TestPosternViewJSONPrintsThePlaintextWithoutWritingAnything(t *testing.T) {
	posternHome(t, "http://unused", "", "")
	viewPath := filepath.Join(t.TempDir(), "view.b64")
	t.Setenv("MW_POSTERN_VIEW_PATH", viewPath)

	out, err := runPostern(t, "view", "--json")
	if err != nil {
		t.Fatalf("mw postern view --json failed: %v\n%s", err, out)
	}
	var doc application.PosternViewDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("expected --json to print the view's JSON, got %q: %v", out, err)
	}
	if doc.V != 2 || doc.Host != "laptop" || doc.WrittenAt == "" {
		t.Fatalf("expected a v 2 view written by laptop, got %+v", doc)
	}
	if _, err := os.Stat(viewPath); !os.IsNotExist(err) {
		t.Fatalf("expected --json to write nothing, but %s exists", viewPath)
	}
}

func TestPosternViewRefusesWithoutAGovernorKeyBeforeCallingBd(t *testing.T) {
	posternHome(t, "http://unused", "unused-wif", "")
	callLog := noBdCalls(t)

	out, err := runPostern(t, "view")
	if err == nil || !strings.Contains(err.Error(), "postern_governor_key") {
		t.Fatalf("expected mw postern view to refuse naming postern_governor_key, got %v\n%s", err, out)
	}
	assertNoBdCalls(t, callLog)
}

// The written view is the sealed text: it opens, with the gzip read back, to
// the view --json prints.
func TestPosternViewWritesTheSealedViewAtomically(t *testing.T) {
	posternHome(t, "http://unused", "", "governor-pubkey-hex")
	viewPath := filepath.Join(t.TempDir(), "state", "view.b64")
	t.Setenv("MW_POSTERN_VIEW_PATH", viewPath)

	cipher := apptest.NewFakeCipher()
	realCipher := posternCipher
	t.Cleanup(func() { posternCipher = realCipher })
	posternCipher = func(*postern.KeyFile) application.Cipher { return cipher }

	out, err := runPostern(t, "view")
	if err != nil {
		t.Fatalf("mw postern view failed: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(viewPath)
	if err != nil {
		t.Fatalf("reading the written view: %v", err)
	}
	var doc application.PosternViewDoc
	if err := application.OpenPosternDoc(cipher, "any", string(raw), &doc); err != nil {
		t.Fatalf("opening the written view: %v", err)
	}
	if doc.V != 2 || doc.Host != "laptop" {
		t.Fatalf("expected the sealed view of laptop, got %+v", doc)
	}
	if _, err := os.Stat(viewPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("expected no temp file left behind, stat gave: %v", err)
	}
	if !strings.Contains(out, viewPath) {
		t.Fatalf("expected mw postern view to say where it wrote, got %q", out)
	}
}
