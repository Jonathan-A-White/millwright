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

// A formula installed in the vault is committed in the same step, even when the
// run is cut short in between (mw-gq6.502).
func TestFormulasAreCommittedEvenWhenTheirInstallIsCutShort(t *testing.T) {
	t.Parallel()
	here, _ := twoHosts(t)
	factory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(factory, application.FactoryFormulasDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(factory, application.FactoryFormulasDir, "tdd-feature.formula.json"), []byte(`{"formula":"tdd-feature"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	committed, _, err := vault.NewFormulas(vault.New(here)).Install(ctx, factory, "mw formulas: install")
	if err != nil {
		t.Fatalf("expected the formulas kept though the run was cut short, got %v", err)
	}
	if len(committed) != 1 {
		t.Errorf("expected one formula committed, got %v", committed)
	}
	if status := run(t, here, "git", "status", "--porcelain", "--untracked-files=all"); status != "" {
		t.Errorf("expected nothing left uncommitted for dispatch to refuse on, got %q", status)
	}
	if log := run(t, here, "git", "log", "-1", "--name-only", "--format=%s"); !strings.Contains(log, "tdd-feature.formula.json") {
		t.Errorf("expected the formula committed, got %q", log)
	}
}
