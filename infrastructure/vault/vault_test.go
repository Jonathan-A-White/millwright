package vault_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// aVault lays out a vault holding one seat, with the pieces of that seat a
// session must never be booted with alongside the ones it must.
func aVault(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for path, contents := range map[string]string{
		"seats/builder/charter.md":             "the builder's charter",
		"seats/builder/rigs/millwright.md":     "what the builder knows about millwright",
		"seats/builder/rigs/fellowship.md":     "what the builder knows about fellowship",
		"seats/builder/ledger.md":              "every story the builder ever worked",
		"seats/builder/postmortems/2026-09.md": "the story that went wrong",
		"seats/mayor/charter.md":               "the mayor's charter",
		"runs/mw-old.1/result.json":            "{}",
	} {
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("making %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
			t.Fatalf("writing %s: %v", full, err)
		}
	}
	return dir
}

func TestSeatIsTheCharterAndTheMemoryOfOneRig(t *testing.T) {
	v := vault.New(aVault(t))

	seat, err := v.Seat(context.Background(), "builder", "millwright")
	if err != nil {
		t.Fatalf("reading the seat: %v", err)
	}
	if seat.Name != "builder" || seat.Rig != "millwright" {
		t.Errorf("expected the builder seat on millwright, got %q on %q", seat.Name, seat.Rig)
	}
	if seat.Charter != "the builder's charter" {
		t.Errorf("expected the charter, got %q", seat.Charter)
	}
	if seat.Memory != "what the builder knows about millwright" {
		t.Errorf("expected the memory of millwright, got %q", seat.Memory)
	}
}

func TestSeatWithNoMemoryOfTheRigIsNotAnError(t *testing.T) {
	v := vault.New(aVault(t))

	seat, err := v.Seat(context.Background(), "builder", "a-rig-never-worked")
	if err != nil {
		t.Fatalf("a seat with no memory of a rig should still boot: %v", err)
	}
	if seat.Memory != "" {
		t.Errorf("expected no memory, got %q", seat.Memory)
	}
	if seat.Charter == "" {
		t.Error("expected the charter to be read anyway")
	}
}

func TestSeatWithoutACharterIsAnError(t *testing.T) {
	v := vault.New(aVault(t))

	if _, err := v.Seat(context.Background(), "clerk", "millwright"); err == nil {
		t.Error("expected a seat with no charter to be refused: there is nothing to boot into")
	}
}

func TestSeatRefusesANameThatWouldReachOutsideTheVault(t *testing.T) {
	v := vault.New(aVault(t))

	for _, name := range []string{"", "..", "../mayor", "builder/rigs"} {
		if _, err := v.Seat(context.Background(), name, "millwright"); err == nil {
			t.Errorf("expected the seat %q to be refused", name)
		}
		if _, err := v.Seat(context.Background(), "builder", "../../etc/passwd"); err == nil {
			t.Error("expected a rig that climbs out of the vault to be refused")
		}
	}
}

func TestRunFilesLandTogetherUnderTheStory(t *testing.T) {
	dir := aVault(t)
	v := vault.New(dir)

	path, err := v.PutRunFile(context.Background(), "mw-gq6.6", application.BootFileName, "boot me")
	if err != nil {
		t.Fatalf("writing the boot file: %v", err)
	}
	want := filepath.Join(dir, "runs", "mw-gq6.6", application.BootFileName)
	if path != want {
		t.Errorf("expected the boot file at %q, got %q", want, path)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the boot file back: %v", err)
	}
	if string(contents) != "boot me" {
		t.Errorf("expected what was written, got %q", contents)
	}

	// The result belongs beside it, in a directory writing the boot file has
	// already made: the session only redirects into it.
	result := v.RunFile("mw-gq6.6", application.ResultFileName)
	if filepath.Dir(result) != filepath.Dir(path) {
		t.Errorf("expected the result beside the boot file, got %q", result)
	}
	if _, err := os.Stat(filepath.Dir(result)); err != nil {
		t.Errorf("expected the run directory to be there already: %v", err)
	}
}

func TestPutRunFileRefusesAStoryThatWouldReachOutsideTheVault(t *testing.T) {
	v := vault.New(aVault(t))

	if _, err := v.PutRunFile(context.Background(), "../seats/builder", "charter.md", "mine now"); err == nil {
		t.Error("expected a story id that climbs out of the runs directory to be refused")
	}
}

func TestVaultReadsNothingElseInTheSeat(t *testing.T) {
	v := vault.New(aVault(t))

	seat, err := v.Seat(context.Background(), "builder", "millwright")
	if err != nil {
		t.Fatalf("reading the seat: %v", err)
	}
	read := seat.Charter + seat.Memory
	for _, unwanted := range []string{"fellowship", "every story the builder ever worked", "went wrong", "mayor"} {
		if strings.Contains(read, unwanted) {
			t.Errorf("a seat read at boot must not hold %q", unwanted)
		}
	}
}
