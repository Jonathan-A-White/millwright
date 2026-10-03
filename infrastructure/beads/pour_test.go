package beads_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// mw-gq6.244: bd refusing a step title for its length is the story's refusal,
// and a bd that failed for any other reason is not.
func TestPourFormulaTellsARefusalOfTheBeadsFromAFaultOfBd(t *testing.T) {
	refused := standIn(t, "title must be 500 characters or less (got 512)", 1)
	_, err := refused.PourFormula(context.Background(), "tdd-feature", "mw-1", "A title")
	var refusal *application.PourRefused
	if !errors.As(err, &refusal) {
		t.Fatalf("expected a PourRefused, got %v", err)
	}
	if refusal.Story != "mw-1" || refusal.Formula != "tdd-feature" || refusal.Reason != "title must be 500 characters or less (got 512)" {
		t.Errorf("got %+v", refusal)
	}

	faulty := standIn(t, "connection refused", 1)
	_, err = faulty.PourFormula(context.Background(), "tdd-feature", "mw-1", "A title")
	if err == nil || errors.As(err, &refusal) {
		t.Errorf("expected a plain failure for a fault of bd, got %v", err)
	}
}

func TestFormulaStepTitlesReadsTheTitlesOffTheFormula(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "bd-stand-in")
	script := "#!/bin/sh\necho '{\"formula\":\"tdd-feature\",\"steps\":[{\"id\":\"a\",\"title\":\"Understand {{story}}: {{title}}\"},{\"id\":\"b\",\"title\":\"Commit\"}]}'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	titles, err := beads.New(dir, beads.WithProgram(path)).FormulaStepTitles(context.Background(), "tdd-feature")
	if err != nil {
		t.Fatalf("reading the titles: %v", err)
	}
	if want := []string{"Understand {{story}}: {{title}}", "Commit"}; !reflect.DeepEqual(titles, want) {
		t.Errorf("titles = %q, want %q", titles, want)
	}
}
