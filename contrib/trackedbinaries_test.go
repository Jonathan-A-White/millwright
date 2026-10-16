package contrib_test

// scripts/check-tracked-binaries.sh, which make lint runs, refuses a repository
// that tracks an executable or a file over 2 MB outside testdata/ (mw-gq6.517:
// a stray `go build` output, mw, rode to main in a commit). These tests run it
// on throwaway repositories in a temporary directory, and on this one.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func checkTrackedBinaries(t *testing.T, repo string) (string, error) {
	t.Helper()
	script, err := filepath.Abs("../scripts/check-tracked-binaries.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", script, repo)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func aRepoTracking(t *testing.T, files map[string][]byte) string {
	t.Helper()
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q")
	for name, content := range files {
		path := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-qm", "files")
	return repo
}

func TestTrackedBinariesPassesARepoOfPlainFiles(t *testing.T) {
	repo := aRepoTracking(t, map[string][]byte{"README.md": []byte("hello\n"), "src/app.go": []byte("package app\n")})
	if out, err := checkTrackedBinaries(t, repo); err != nil {
		t.Fatalf("expected a pass, got %v:\n%s", err, out)
	}
}

func TestTrackedBinariesFailsNamingAnELFExecutable(t *testing.T) {
	repo := aRepoTracking(t, map[string][]byte{"README.md": []byte("hello\n"), "mw": append([]byte("\x7fELF"), make([]byte, 64)...)})
	out, err := checkTrackedBinaries(t, repo)
	if err == nil {
		t.Fatalf("expected a failure, got a pass:\n%s", out)
	}
	if !strings.Contains(out, "mw") || !strings.Contains(out, "executable") {
		t.Fatalf("expected the failure to name mw as an executable, got:\n%s", out)
	}
}

func TestTrackedBinariesFailsNamingAMachOExecutable(t *testing.T) {
	repo := aRepoTracking(t, map[string][]byte{"tools/run": append([]byte{0xcf, 0xfa, 0xed, 0xfe}, make([]byte, 64)...)})
	out, err := checkTrackedBinaries(t, repo)
	if err == nil || !strings.Contains(out, "tools/run") {
		t.Fatalf("expected a failure naming tools/run, got %v:\n%s", err, out)
	}
}

func TestTrackedBinariesFailsNamingAFileOverTwoMegabytes(t *testing.T) {
	repo := aRepoTracking(t, map[string][]byte{"dump.bin": make([]byte, 2*1024*1024+1)})
	out, err := checkTrackedBinaries(t, repo)
	if err == nil || !strings.Contains(out, "dump.bin") {
		t.Fatalf("expected a failure naming dump.bin, got %v:\n%s", err, out)
	}
}

func TestTrackedBinariesLetsAFileOverTwoMegabytesStandUnderTestdata(t *testing.T) {
	repo := aRepoTracking(t, map[string][]byte{"app/testdata/big.json": make([]byte, 3*1024*1024)})
	if out, err := checkTrackedBinaries(t, repo); err != nil {
		t.Fatalf("expected a pass for a large fixture under testdata/, got %v:\n%s", err, out)
	}
}

func TestTrackedBinariesPassesThisRepository(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := checkTrackedBinaries(t, root); err != nil {
		t.Fatalf("this repository tracks something the check refuses: %v\n%s", err, out)
	}
}
