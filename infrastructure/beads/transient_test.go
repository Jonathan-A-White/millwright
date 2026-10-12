package beads_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// failingBd is a stand-in bd that says words on standard error and exits 1.
func failingBd(t *testing.T, words string) *beads.Gateway {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	program := filepath.Join(t.TempDir(), "bd-failing")
	script := "#!/bin/sh\necho '" + words + "' >&2\nexit 1\n"
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return beads.New(t.TempDir(), beads.WithProgram(program))
}

func TestABdThatCannotReachItsDoltServerFailsAsAPassingFaultInItsOwnWords(t *testing.T) {
	words := "failed to open database: Dolt server unreachable at 10.88.0.2:3307: dial tcp 10.88.0.2:3307: i/o timeout"
	_, err := failingBd(t, words).StoryComments(context.Background(), "mw-x.1")

	fault, passing := application.Faulted(err)
	if !passing {
		t.Fatalf("expected a passing fault, got %v", err)
	}
	if fault.Why != "dolt server unreachable" {
		t.Fatalf("expected the fault named, got %q", fault.Why)
	}
	if !strings.Contains(err.Error(), words) {
		t.Fatalf("expected bd's words in the error, got %v", err)
	}
}

func TestABdThatFailsForAnotherReasonIsNotAPassingFault(t *testing.T) {
	_, err := failingBd(t, `no issue found matching "mw-x.1"`).StoryComments(context.Background(), "mw-x.1")

	if err == nil {
		t.Fatal("expected bd's failure")
	}
	if _, passing := application.Faulted(err); passing {
		t.Fatalf("expected no passing fault, got %v", err)
	}
}
