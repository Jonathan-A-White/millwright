package doctor_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// formulasWorld is a factory checkout with one formula and a vault holding a
// copy of it, both real directories; the vault needs no git for the probe.
type formulasWorld struct {
	factory string
	vault   string
	check   *doctor.Formulas
}

func newFormulasWorld(t *testing.T, home string) *formulasWorld {
	t.Helper()
	w := &formulasWorld{factory: t.TempDir(), vault: t.TempDir()}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.name", "t"}, {"config", "user.email", "t@t.invalid"}} {
		w.git(t, args...)
	}
	w.write(t, w.factory, "formulas/tdd-feature.formula.json", "v1")
	w.commit(t, "one")
	w.write(t, w.vault, ".beads/formulas/tdd-feature.formula.json", "v1")
	w.check = doctor.NewFormulas(vault.NewFormulas(vault.New(w.vault)), w.factory,
		&apptest.FakeHomeFile{Text: home + " 2026-10-09T00:00:00Z mw@laptop"}, "laptop", apptest.NewFakeDoctorState())
	return w
}

func (w *formulasWorld) git(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", w.factory}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func (w *formulasWorld) write(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (w *formulasWorld) commit(t *testing.T, message string) {
	t.Helper()
	w.git(t, "add", "-A")
	w.git(t, "commit", "-qm", message)
}

func TestTheFormulasCheckIsQuietWhenTheVaultMatches(t *testing.T) {
	w := newFormulasWorld(t, "laptop")
	if verdict, reason := w.check.Probe(context.Background()); verdict != application.DoctorOK || reason != "" {
		t.Fatalf("expected ok and quiet, got %s (%s)", verdict, reason)
	}
}

func TestTheFormulasCheckNamesFormulasOutOfStepAfterMoreThanOneSelfUpdate(t *testing.T) {
	w := newFormulasWorld(t, "laptop")
	ctx := context.Background()

	// One self-update has moved the checkout and the install has not caught up:
	// noted, not yet alarmed.
	w.write(t, w.factory, "formulas/tdd-feature.formula.json", "v2")
	w.commit(t, "two")
	if verdict, reason := w.check.Probe(ctx); verdict != application.DoctorOK {
		t.Fatalf("expected ok for the first self-update, got %s (%s)", verdict, reason)
	}
	if verdict, reason := w.check.Probe(ctx); verdict != application.DoctorOK {
		t.Fatalf("expected ok while the checkout has not moved again, got %s (%s)", verdict, reason)
	}

	// A second self-update, and still out of step.
	w.write(t, w.factory, "README.md", "x")
	w.commit(t, "three")
	verdict, reason := w.check.Probe(ctx)
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "tdd-feature.formula.json") || !strings.Contains(reason, "more than one self-update") {
		t.Fatalf("expected faulty naming the formula, got %s (%s)", verdict, reason)
	}

	// The vault caught up: quiet again, and the memory is cleared.
	w.write(t, w.vault, ".beads/formulas/tdd-feature.formula.json", "v2")
	if verdict, reason := w.check.Probe(ctx); verdict != application.DoctorOK || reason != "" {
		t.Fatalf("expected ok once level, got %s (%s)", verdict, reason)
	}
}

func TestTheFormulasCheckJudgesOnlyTheHome(t *testing.T) {
	w := newFormulasWorld(t, "desktop")
	w.write(t, w.factory, "formulas/tdd-feature.formula.json", "v2")
	w.commit(t, "two")
	w.write(t, w.factory, "README.md", "x")
	w.commit(t, "three")
	w.check.Probe(context.Background())
	if verdict, reason := w.check.Probe(context.Background()); verdict != application.DoctorOK || !strings.Contains(reason, "not home") {
		t.Fatalf("expected ok, not home, got %s (%s)", verdict, reason)
	}
}
