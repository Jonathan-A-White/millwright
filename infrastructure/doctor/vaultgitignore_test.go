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
	// the clone tracks a boot file the template ignores: untrack it, so these
	// tests are about the lines alone (mw-gq6.1692 covers tracked files)
	if tracked := strings.Fields(vdRun(t, clone, "git", "ls-files", "--cached", "--ignored", "--exclude-standard")); len(tracked) > 0 {
		vdRun(t, clone, "git", append([]string{"rm", "--cached", "-q", "--"}, tracked...)...)
		vdRun(t, clone, "git", "commit", "-qm", "untrack what is ignored")
	}
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

// mw-gq6.1692: a vault made before a line reached its .gitignore had the files
// that line names committed (a run's check-<n> files, its heartbeat log), and
// adding the line does not untrack them. The check names each file git tracks
// that the vault's .gitignore now ignores, and its cure takes them out of the
// index, leaving them on disk.

func aVaultTrackingIgnoredFiles(t *testing.T) string {
	t.Helper()
	clone, _ := aVaultDirtyVault(t)
	vdWrite(t, clone, "runs/mw-1/check-1-gate.out", "gate\n")
	vdWrite(t, clone, "runs/mw-1/heartbeat-1.log", "beat\n")
	vdWrite(t, clone, "runs/mw-1/result.json", "{}\n")
	vdRun(t, clone, "git", "add", "-f", "runs")
	vdRun(t, clone, "git", "commit", "-qm", "a run's files, some of them scratch")
	vdWrite(t, clone, ".gitignore", "runs/*/check-*\nruns/*/heartbeat-*.log\n")
	vdRun(t, clone, "git", "add", ".gitignore")
	vdRun(t, clone, "git", "commit", "-qm", "the vault's .gitignore")
	return clone
}

func aVaultGitignoreCheckOf(dir, template string) *doctor.VaultGitignore {
	return doctor.NewVaultGitignore(dir, "laptop", fstest.MapFS{".gitignore": {Data: []byte(template)}})
}

func TestVaultGitignoreNamesTheTrackedFilesItIgnores(t *testing.T) {
	clone := aVaultTrackingIgnoredFiles(t)
	verdict, reason := aVaultGitignoreCheckOf(clone, "runs/*/check-*\nruns/*/heartbeat-*.log\n").Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("verdict = %v (%s), want faulty", verdict, reason)
	}
	for _, want := range []string{"runs/mw-1/check-1-gate.out", "runs/mw-1/heartbeat-1.log"} {
		if !strings.Contains(reason, want) {
			t.Errorf("reason %q does not name the tracked file %s", reason, want)
		}
	}
	if strings.Contains(reason, "result.json") {
		t.Errorf("reason %q names a file the vault does not ignore", reason)
	}
}

func TestVaultGitignoreCureUntracksIgnoredFilesAndKeepsThemOnDisk(t *testing.T) {
	clone := aVaultTrackingIgnoredFiles(t)
	check := aVaultGitignoreCheckOf(clone, "runs/*/check-*\nruns/*/heartbeat-*.log\n")
	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("Cure: %v", err)
	}
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("after the cure the verdict = %v (%s), want ok", verdict, reason)
	}
	if out := vdRun(t, clone, "git", "ls-files", "runs/mw-1"); strings.TrimSpace(out) != "runs/mw-1/result.json" {
		t.Errorf("tracked under runs/mw-1 = %q, want only result.json", out)
	}
	if _, err := os.Stat(filepath.Join(clone, "runs/mw-1/check-1-gate.out")); err != nil {
		t.Errorf("the cure removed the file from disk: %v", err)
	}
	if out := vdRun(t, clone, "git", "status", "--porcelain", "--untracked-files=all"); out != "" {
		t.Errorf("the cure left the vault not quiet: %q", out)
	}
}

func TestVaultGitignoreCureUntracksAFileAFreshLineIgnores(t *testing.T) {
	clone := aVaultTrackingIgnoredFiles(t)
	vdWrite(t, clone, ".gitignore", "runs/*/heartbeat-*.log\n")
	vdRun(t, clone, "git", "commit", "-qam", "the vault's .gitignore, before it gained the check line")
	check := aVaultGitignoreCheckOf(clone, "runs/*/check-*\nruns/*/heartbeat-*.log\n")
	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("Cure: %v", err)
	}
	if out := vdRun(t, clone, "git", "ls-files", "runs/mw-1"); strings.Contains(out, "check-") {
		t.Errorf("tracked under runs/mw-1 = %q, want no check- file", out)
	}
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("after the cure the verdict = %v (%s), want ok", verdict, reason)
	}
}
