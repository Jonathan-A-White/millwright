package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
)

// seatContext runs `mw seat context` with a config file under a fresh home
// directory, so that the transcripts it reads are the ones under that home and
// never the machine's own.
func seatContext(t *testing.T, config string, args ...string) (string, error) {
	t.Helper()
	mwConfig(t, config)
	t.Setenv("MW_HANDOFF_AT", "")
	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"seat", "context"}, args...))
	err := root.Execute()
	return strings.TrimSpace(out.String()), err
}

// aTranscript puts one assistant turn into the transcripts of dir under the
// current home directory.
func aTranscript(t *testing.T, dir, session string, tokens int) {
	t.Helper()
	root, err := claude.DefaultProjectsRoot()
	if err != nil {
		t.Fatal(err)
	}
	project := claude.ProjectDir(root, dir)
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	turn := `{"type":"assistant","message":{"usage":{"input_tokens":` + strconv.Itoa(tokens) + `}}}` + "\n"
	if err := os.WriteFile(filepath.Join(project, session+".jsonl"), []byte(turn), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSeatContextReadsTheVaultUnlessToldOtherwise(t *testing.T) {
	vault := t.TempDir()
	mwConfig(t, "vault = \""+vault+"\"\nhost = \"laptop\"\nhandoff_at = 1000\n")
	t.Setenv("MW_HANDOFF_AT", "")
	aTranscript(t, vault, "5e551011-aaaa", 1500)

	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetArgs([]string{"seat", "context"})
	if err := root.Execute(); err != nil {
		t.Fatalf("mw seat context failed: %v", err)
	}
	if want := "context=1500 handoff_at=1000 handoff session=5e551011\n"; out.String() != want {
		t.Fatalf("expected %q, got %q", want, out.String())
	}
}

func TestSeatContextTakesADirectoryFromTheFlag(t *testing.T) {
	// The config names no vault: with --dir it is not needed.
	dir := t.TempDir()
	mwConfig(t, "host = \"laptop\"\n")
	t.Setenv("MW_HANDOFF_AT", "")
	aTranscript(t, dir, "d1r0d1r0-aaaa", 42)

	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetArgs([]string{"seat", "context", "--dir", dir})
	if err := root.Execute(); err != nil {
		t.Fatalf("mw seat context --dir failed: %v", err)
	}
	if want := "context=42 handoff_at=180000 ok session=d1r0d1r0\n"; out.String() != want {
		t.Fatalf("expected %q, got %q", want, out.String())
	}
}

func TestSeatContextWithNoTranscriptNamesWhereItLooked(t *testing.T) {
	dir := t.TempDir()
	_, err := seatContext(t, "host = \"laptop\"\n", "--dir", dir)
	if err == nil {
		t.Fatal("expected an error for a directory with no transcript")
	}
	if !strings.Contains(err.Error(), filepath.Base(claude.ProjectDir("", dir))) {
		t.Fatalf("expected the error to name the project directory, got %q", err)
	}
}

func TestSeatContextNeedsAVaultOrADirectory(t *testing.T) {
	if _, err := seatContext(t, "host = \"laptop\"\n"); err == nil || !strings.Contains(err.Error(), "MW_VAULT") {
		t.Fatalf("expected the reason to say how to set the vault, got %v", err)
	}
}
