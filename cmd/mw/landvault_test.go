package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// mw-gq6.631: mw land built its Next without the error register, so every
// line of a batch's Landed mail 'For common errors' read as a new class with
// the placeholder kinds. The batch lander's Next reads the vault's register
// as mw next's does: the classes it holds and the kinds of the rig.
func TestTheBatchLandersNextReadsTheErrorRegisterOfTheVault(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, vault.SeatsDir, BuilderSeat, application.ErrorRegisterDir)
	if err := os.MkdirAll(reg, 0o755); err != nil {
		t.Fatal(err)
	}
	class := application.ErrorClass{Slug: "double-tap-undoes-toggle", Symptom: "a second tap undoes it", Kinds: []string{"web-app"}, Since: "2026-10-14"}
	if err := os.WriteFile(filepath.Join(reg, class.Slug+application.MemoryExt), []byte(class.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reg, application.ErrorKindsFile), []byte("lampas = [\"web-app\", \"pwa\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	n := withVaultPorts(application.Next{Seat: BuilderSeat}, mwVault(dir, "laptop"))
	if n.Errors == nil {
		t.Fatal("the batch lander's Next has no error register to read: every common-errors line would read as a new class")
	}
	classes, kindsText, err := n.Errors.ReadErrorRegister(context.Background(), BuilderSeat)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := classes[class.Slug]; !ok {
		t.Errorf("the register read holds %v, not %s", classes, class.Slug)
	}
	if !strings.Contains(kindsText, "lampas") {
		t.Errorf("the kinds read were %q", kindsText)
	}
}
