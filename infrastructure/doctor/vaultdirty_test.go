package doctor_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// These tests drive real git against throwaway clones in a temp directory, the
// same way infrastructure/vault/git_test.go does: a bare repository standing
// in for the remote both hosts share, and one clone standing in for this
// host's vault. Nothing reaches the network.

// aVaultDirtyVault makes a bare repository with one commit in it — a run file
// and a rig memory file, both already committed, standing in for a vault a
// story has already landed work into — and a clone of it. It returns the
// clone's directory and the bare repository's directory, so a test can check
// that a cure never pushed.
func aVaultDirtyVault(t *testing.T) (clone, remote string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}

	root := t.TempDir()
	remote = filepath.Join(root, "origin.git")
	vdRun(t, root, "git", "init", "--bare", "-q", "-b", "main", remote)

	clone = filepath.Join(root, "vault")
	vdRun(t, root, "git", "clone", "-q", remote, clone)
	vdRun(t, clone, "git", "config", "user.name", "millwright test")
	vdRun(t, clone, "git", "config", "user.email", "test@millwright.invalid")

	vdWrite(t, clone, "runs/mw-i80dx.3/result.json", `{"ok":true}`+"\n")
	vdWrite(t, clone, "runs/mw-i80dx.3/boot.md", "a session's boot file\n")
	vdWrite(t, clone, "seats/builder/rigs/millwright.md", "the rig's memory\n")
	vdRun(t, clone, "git", "add", "-A")
	vdRun(t, clone, "git", "commit", "-qm", "the vault opens")
	vdRun(t, clone, "git", "push", "-q", "-u", "origin", "main")
	return clone, remote
}

func vdRun(t *testing.T, dir, program string, args ...string) string {
	t.Helper()
	cmd := exec.Command(program, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s in %s: %v: %s", program, strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

func vdWrite(t *testing.T, dir, name, contents string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("making %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// vdCommitted is the paths one commit touched.
func vdCommitted(t *testing.T, dir, revision string) []string {
	t.Helper()
	out := vdRun(t, dir, "git", "show", "--pretty=format:", "--name-only", revision)
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) != "" {
			names = append(names, strings.TrimSpace(line))
		}
	}
	return names
}

func TestVaultDirtyProbeIsOKOnAClean(t *testing.T) {
	clone, _ := aVaultDirtyVault(t)
	check := doctor.NewVaultDirty(clone, "laptop", doctor.New(t.TempDir()))

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
}

func TestVaultDirtyCuresATruncatedRunFileByItsExactPath(t *testing.T) {
	clone, remote := aVaultDirtyVault(t)
	vdWrite(t, clone, "runs/mw-i80dx.3/result.json", "")
	vdWrite(t, clone, "runs/mw-i80dx.3/untracked.txt", "nobody's business\n")

	check := doctor.NewVaultDirty(clone, "laptop", doctor.New(t.TempDir()))
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, "1 modified tracked files") || !strings.Contains(reason, "runs/mw-i80dx.3/result.json") {
		t.Errorf("expected the reason to count and name the file, got %q", reason)
	}

	remoteBefore := strings.TrimSpace(vdRun(t, remote, "git", "rev-parse", "main"))
	headBefore := strings.TrimSpace(vdRun(t, clone, "git", "rev-parse", "HEAD"))

	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("curing: %v", err)
	}

	headAfter := strings.TrimSpace(vdRun(t, clone, "git", "rev-parse", "HEAD"))
	if headAfter == headBefore {
		t.Fatal("expected a new commit")
	}
	if count := strings.TrimSpace(vdRun(t, clone, "git", "rev-list", "--count", headBefore+".."+headAfter)); count != "1" {
		t.Fatalf("expected exactly one commit, got %s", count)
	}
	if held := vdCommitted(t, clone, "HEAD"); len(held) != 1 || held[0] != "runs/mw-i80dx.3/result.json" {
		t.Fatalf("expected the commit to hold only the changed path, got %v", held)
	}
	message := strings.TrimSpace(vdRun(t, clone, "git", "log", "-1", "--format=%B"))
	if !strings.Contains(message, "doctor: runs/mw-i80dx.3/result.json committed as found (modified since") ||
		!strings.Contains(message, "host laptop") {
		t.Fatalf("unexpected commit message %q", message)
	}

	left := vdRun(t, clone, "git", "status", "--porcelain")
	if strings.Contains(left, "result.json") {
		t.Fatalf("expected result.json committed, vault still shows it dirty: %q", left)
	}
	if !strings.Contains(left, "untracked.txt") {
		t.Fatalf("expected the untracked file left exactly as it was, got %q", left)
	}

	remoteAfter := strings.TrimSpace(vdRun(t, remote, "git", "rev-parse", "main"))
	if remoteAfter != remoteBefore {
		t.Fatalf("expected nothing pushed, the origin moved from %s to %s", remoteBefore, remoteAfter)
	}
}

func TestVaultDirtyCuresTwoDirtyPathsWithTwoCommits(t *testing.T) {
	clone, _ := aVaultDirtyVault(t)
	vdWrite(t, clone, "runs/mw-i80dx.3/result.json", "")
	vdWrite(t, clone, "runs/mw-i80dx.3/boot.md", "rewritten by a re-dispatch\n")

	check := doctor.NewVaultDirty(clone, "laptop", doctor.New(t.TempDir()))
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, "2 modified tracked files") {
		t.Errorf("expected the reason to count both files, got %q", reason)
	}

	headBefore := strings.TrimSpace(vdRun(t, clone, "git", "rev-parse", "HEAD"))
	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("curing: %v", err)
	}
	headAfter := strings.TrimSpace(vdRun(t, clone, "git", "rev-parse", "HEAD"))
	if count := strings.TrimSpace(vdRun(t, clone, "git", "rev-list", "--count", headBefore+".."+headAfter)); count != "2" {
		t.Fatalf("expected exactly two commits, got %s", count)
	}
	if left := vdRun(t, clone, "git", "status", "--porcelain"); strings.TrimSpace(left) != "" {
		t.Fatalf("expected the vault clean after curing both, got %q", left)
	}
}

func TestVaultDirtyProbeIsCannotTellForAPathOutsideRuns(t *testing.T) {
	clone, _ := aVaultDirtyVault(t)
	vdWrite(t, clone, "seats/builder/rigs/millwright.md", "changed by hand\n")

	check := doctor.NewVaultDirty(clone, "laptop", doctor.New(t.TempDir()))
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected the first sighting to be ok, got %s (%s)", verdict, reason)
	}
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, "modified tracked files outside runs/") || !strings.Contains(reason, "seats/builder/rigs/millwright.md") {
		t.Errorf("expected the reason to name the path outside runs/, got %q", reason)
	}

	headBefore := strings.TrimSpace(vdRun(t, clone, "git", "rev-parse", "HEAD"))
	if headAfter := strings.TrimSpace(vdRun(t, clone, "git", "rev-parse", "HEAD")); headAfter != headBefore {
		t.Fatal("expected probing to change nothing")
	}
	if left := vdRun(t, clone, "git", "status", "--porcelain"); !strings.Contains(left, "millwright.md") {
		t.Fatalf("expected the file still dirty, nobody committed it, got %q", left)
	}
}

func TestVaultDirtyProbeIsCannotTellWhenPathsAreMixed(t *testing.T) {
	clone, _ := aVaultDirtyVault(t)
	vdWrite(t, clone, "runs/mw-i80dx.3/result.json", "")
	vdWrite(t, clone, "seats/builder/rigs/millwright.md", "changed by hand\n")

	check := doctor.NewVaultDirty(clone, "laptop", doctor.New(t.TempDir()))
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorFaulty || strings.Contains(reason, "millwright.md") {
		t.Fatalf("expected the first sighting to count only the run file, got %s (%s)", verdict, reason)
	}
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s (%s)", verdict, reason)
	}
	if strings.Contains(reason, "result.json") {
		t.Errorf("expected only the outside path named, got %q", reason)
	}
	if !strings.Contains(reason, "seats/builder/rigs/millwright.md") {
		t.Errorf("expected the outside path named, got %q", reason)
	}

	left := vdRun(t, clone, "git", "status", "--porcelain")
	if !strings.Contains(left, "result.json") || !strings.Contains(left, "millwright.md") {
		t.Fatalf("expected both files still dirty, nobody committed anything, got %q", left)
	}
}

func TestVaultDirtyProbeIsOKWhenAPathOutsideRunsIsDirtyOnOneRunAndCleanOnTheNext(t *testing.T) {
	clone, _ := aVaultDirtyVault(t)
	check := doctor.NewVaultDirty(clone, "laptop", doctor.New(t.TempDir()))
	ctx := context.Background()

	vdWrite(t, clone, "seats/builder/rigs/millwright.md", "a seat's commit is on its way\n")
	if verdict, reason := check.Probe(ctx); verdict != application.DoctorOK {
		t.Fatalf("expected ok on the first sighting, got %s (%s)", verdict, reason)
	}

	// The seat's commit lands.
	vdRun(t, clone, "git", "commit", "-qam", "the seat commits")
	if verdict, reason := check.Probe(ctx); verdict != application.DoctorOK {
		t.Fatalf("expected ok once the file is clean, got %s (%s)", verdict, reason)
	}

	// Dirty again later is a first sighting again, not the second.
	vdWrite(t, clone, "seats/builder/rigs/millwright.md", "dirty once more\n")
	if verdict, reason := check.Probe(ctx); verdict != application.DoctorOK {
		t.Fatalf("expected ok on a fresh first sighting, got %s (%s)", verdict, reason)
	}
}

func TestVaultDirtyProbeIsCannotTellWhenTheSamePathIsDirtyOnTwoConsecutiveRuns(t *testing.T) {
	clone, _ := aVaultDirtyVault(t)
	// A new check over the same state each run, as separate mw doctor runs are.
	state := doctor.New(t.TempDir())
	vdWrite(t, clone, "seats/builder/rigs/millwright.md", "changed by hand\n")

	if verdict, reason := doctor.NewVaultDirty(clone, "laptop", state).Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok on the first run, got %s (%s)", verdict, reason)
	}
	verdict, reason := doctor.NewVaultDirty(clone, "laptop", state).Probe(context.Background())
	if verdict != application.DoctorCannotTell || !strings.Contains(reason, "seats/builder/rigs/millwright.md") {
		t.Fatalf("expected cannot-tell naming the path on the second run, got %s (%s)", verdict, reason)
	}
}

func TestVaultDirtyWayBackNamesTheCommitAfterACure(t *testing.T) {
	clone, _ := aVaultDirtyVault(t)
	vdWrite(t, clone, "runs/mw-i80dx.3/result.json", "")

	check := doctor.NewVaultDirty(clone, "laptop", doctor.New(t.TempDir()))
	if _, reason := check.Probe(context.Background()); reason == "" {
		t.Fatal("expected a reason from a faulty probe")
	}
	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("curing: %v", err)
	}
	hash := strings.TrimSpace(vdRun(t, clone, "git", "rev-parse", "HEAD"))

	way := check.WayBack()
	if !strings.Contains(way, "git -C "+clone+" revert "+hash+" -- runs/mw-i80dx.3/result.json") {
		t.Fatalf("expected the way back to name the commit %s, got %q", hash, way)
	}
}

func TestVaultDirtyDamperIsFiveMinutesCapFive(t *testing.T) {
	check := doctor.NewVaultDirty("", "", nil)
	wait, capPerEpisode := check.Damper()
	if wait != doctor.VaultDirtyDamperWait || capPerEpisode != doctor.VaultDirtyDamperCap {
		t.Fatalf("expected %s/%d, got %s/%d", doctor.VaultDirtyDamperWait, doctor.VaultDirtyDamperCap, wait, capPerEpisode)
	}
}
