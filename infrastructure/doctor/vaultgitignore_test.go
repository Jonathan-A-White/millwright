package doctor_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Jonathan-A-White/millwright"
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// mw-gq6.1689: mw init copies template/.gitignore into a vault once, so a line
// added to the template later never reaches a vault that exists. The check
// names each line of the template the vault's .gitignore lacks, and its cure
// appends them.

const vgTemplate = "# Beads\n.dolt/\n*.db\n\n# A run's logs\nruns/*/boot.md\nruns/*/heartbeat-*.log\n"

func aVaultGitignoreVault(t *testing.T, ignore string) string {
	t.Helper()
	clone, _ := aVaultDirtyVault(t)
	vdWrite(t, clone, ".gitignore", ignore)
	vdRun(t, clone, "git", "add", ".gitignore")
	vdRun(t, clone, "git", "commit", "-qm", "the vault's .gitignore")
	return clone
}

func aVaultGitignoreCheck(dir string) *doctor.VaultGitignore {
	return doctor.NewVaultGitignore(dir, "laptop", fstest.MapFS{".gitignore": {Data: []byte(vgTemplate)}})
}

func TestVaultGitignoreIsOkWhenTheVaultHoldsEveryLine(t *testing.T) {
	clone := aVaultGitignoreVault(t, "# mine\n"+vgTemplate+"extra\n")
	if verdict, reason := aVaultGitignoreCheck(clone).Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("verdict = %v (%s), want ok", verdict, reason)
	}
}

func TestVaultGitignoreNamesTheLineTheVaultLacks(t *testing.T) {
	clone := aVaultGitignoreVault(t, strings.Replace(vgTemplate, "runs/*/heartbeat-*.log\n", "", 1))
	verdict, reason := aVaultGitignoreCheck(clone).Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("verdict = %v (%s), want faulty", verdict, reason)
	}
	if !strings.Contains(reason, "runs/*/heartbeat-*.log") {
		t.Errorf("reason %q does not name the missing line", reason)
	}
	if strings.Contains(reason, ".dolt/") {
		t.Errorf("reason %q names a line the vault holds", reason)
	}
}

func TestVaultGitignoreMissingFileNamesEveryLine(t *testing.T) {
	clone, _ := aVaultDirtyVault(t)
	verdict, reason := aVaultGitignoreCheck(clone).Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, ".dolt/") || !strings.Contains(reason, "*.db") {
		t.Fatalf("verdict = %v (%s), want faulty naming every line", verdict, reason)
	}
}

func TestVaultGitignoreCureAppendsTheMissingLineAndCommitsIt(t *testing.T) {
	clone := aVaultGitignoreVault(t, strings.Replace(vgTemplate, "runs/*/heartbeat-*.log\n", "", 1))
	check := aVaultGitignoreCheck(clone)
	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("Cure: %v", err)
	}
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("after the cure the verdict = %v (%s), want ok", verdict, reason)
	}
	got, err := os.ReadFile(filepath.Join(clone, ".gitignore"))
	if err != nil || !strings.Contains(string(got), "runs/*/heartbeat-*.log\n") {
		t.Fatalf(".gitignore = %q (%v), want the line appended", got, err)
	}
	if out := vdRun(t, clone, "git", "status", "--porcelain", "--untracked-files=no"); out != "" {
		t.Errorf("the cure left the vault dirty: %q", out)
	}
	if way := check.WayBack(); !strings.Contains(way, "git -C "+clone+" revert") {
		t.Errorf("WayBack = %q, want a git revert naming the vault", way)
	}
}

func TestVaultGitignoreCureKeepsWhatTheVaultHolds(t *testing.T) {
	clone := aVaultGitignoreVault(t, ".dolt/\n*.db\nmine/\nruns/*/boot.md")
	if err := aVaultGitignoreCheck(clone).Cure(context.Background()); err != nil {
		t.Fatalf("Cure: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(clone, ".gitignore"))
	if !strings.HasPrefix(string(got), ".dolt/\n*.db\nmine/\nruns/*/boot.md\n") {
		t.Errorf(".gitignore = %q, want the vault's own lines kept first", got)
	}
}

func TestVaultGitignoreTheRealTemplateHoldsToItself(t *testing.T) {
	clone, _ := aVaultDirtyVault(t)
	ignore, _ := millwright.Template().Open(".gitignore")
	_ = ignore.Close()
	check := doctor.NewVaultGitignore(clone, "laptop", millwright.Template())
	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("Cure: %v", err)
	}
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("verdict = %v (%s), want ok", verdict, reason)
	}
}
