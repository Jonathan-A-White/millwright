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

func TestTheErrorRegisterWritesAndReadsBackItsClassesAndKinds(t *testing.T) {
	dir := t.TempDir()
	v := vault.New(dir)
	ctx := context.Background()

	classes, kinds, err := v.ReadErrorRegister(ctx, "builder")
	if err != nil || len(classes) != 0 || kinds != "" {
		t.Fatalf("a seat with no register: %v %q %v, want none and no error", classes, kinds, err)
	}

	path, err := v.WriteErrorClass(ctx, "builder", "a-class", "class text\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "seats", "builder", application.ErrorRegisterDir, "a-class.md"); path != want {
		t.Fatalf("wrote %s, want %s", path, want)
	}
	if path, err = v.WriteErrorKinds(ctx, "builder", "lampas = [\"pwa\"]\n"); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "seats", "builder", application.ErrorRegisterDir, application.ErrorKindsFile); path != want {
		t.Fatalf("wrote %s, want %s", path, want)
	}

	classes, kinds, err = v.ReadErrorRegister(ctx, "builder")
	if err != nil {
		t.Fatal(err)
	}
	if len(classes) != 1 || classes["a-class"] != "class text\n" || kinds != "lampas = [\"pwa\"]\n" {
		t.Fatalf("read back %v %q", classes, kinds)
	}
	if _, err := v.WriteErrorClass(ctx, "builder", "../escape", "x"); err == nil {
		t.Fatal("a slug that reaches outside the register was written")
	}
}

// The register is read by need, never at a Builder's boot: its fuel is spent
// only by the session that asks for it.
func TestTheErrorRegisterIsNeverReadAtABuildersBoot(t *testing.T) {
	dir := aVault(t)
	v := vault.New(dir)
	ctx := context.Background()
	if _, err := v.WriteErrorClass(ctx, "builder", "a-class", "the register's own words\n"); err != nil {
		t.Fatal(err)
	}
	seat, err := v.Seat(ctx, "builder", "millwright")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(seat.Charter+seat.Memory, "the register's own words") {
		t.Error("a seat read at boot must not hold the error register")
	}
	if _, err := os.Stat(filepath.Join(dir, "seats", "builder", application.ErrorRegisterDir)); err != nil {
		t.Fatal(err)
	}
}
