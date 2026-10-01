package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestPosternViewFollowRefusesJSONAndAnEveryWithoutFollowBeforeCallingBd(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"view", "--follow", "--json"}, "--json"},
		{[]string{"view", "--every", "2s"}, "needs --follow"},
		{[]string{"view", "--follow", "--every", "0s"}, "positive"},
	} {
		posternHome(t, "http://unused", "unused-wif", "governor-pubkey-hex")
		callLog := noBdCalls(t)
		out, err := runPostern(t, c.args...)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("mw postern %v: expected a refusal naming %q, got %v\n%s", c.args, c.want, err, out)
		}
		assertNoBdCalls(t, callLog)
	}
}

// --follow publishes on its first pass and then stops cleanly when told to:
// the stand-in bd prints nothing, so the head read fails, which is said and
// does not end the loop.
func TestPosternViewFollowSaysAFailedReadAndStopsCleanlyWhenToldTo(t *testing.T) {
	posternHome(t, "http://unused", "", "governor-pubkey-hex")
	callLog := noBdCalls(t)

	ctx, stop := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer stop()
	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"postern", "view", "--follow", "--every", "100ms"})
	if err := root.ExecuteContext(ctx); err != nil {
		t.Fatalf("expected --follow to stop cleanly when its context ended, got %v\n%s", err, out)
	}
	if !strings.Contains(out.String(), "mw postern view --follow:") {
		t.Fatalf("expected the failure to be said, got %q", out)
	}
	if data, _ := os.ReadFile(callLog); !strings.Contains(string(data), "sql --json") {
		t.Fatalf("expected the loop to read the head with bd sql, bd was asked:\n%s", data)
	}
}
