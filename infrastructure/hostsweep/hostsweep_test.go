package hostsweep

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.org", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.org",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// rigOnDisk makes a rig with an origin and a main, and a worktree a story, each
// as a session leaves it: a clean one on main, a dirty one, one with a commit
// kept on this host only, and one with its commit pushed.
func rigOnDisk(t *testing.T) (Disk, string) {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	git(t, root, "init", "--bare", "-b", "main", origin)
	rigDir := filepath.Join(root, "work", "rig")
	git(t, root, "clone", origin, rigDir)
	git(t, rigDir, "checkout", "-b", "main")
	write(t, filepath.Join(rigDir, ".gitignore"), "node_modules/\n")
	write(t, filepath.Join(rigDir, "a.txt"), "a\n")
	git(t, rigDir, "add", "-A")
	git(t, rigDir, "commit", "-m", "first")
	git(t, rigDir, "push", "-u", "origin", "main")

	trees := filepath.Join(root, "work", application.WorktreesDir)
	for _, story := range []string{"s-clean", "s-dirty", "s-ahead", "s-pushed"} {
		git(t, rigDir, "worktree", "add", "-b", "mw/"+story, filepath.Join(trees, story), "main")
	}
	write(t, filepath.Join(trees, "s-clean", "node_modules", "pkg", "index.js"), "x")
	write(t, filepath.Join(trees, "s-dirty", "new.txt"), "uncommitted")
	for _, story := range []string{"s-ahead", "s-pushed"} {
		write(t, filepath.Join(trees, story, "work.txt"), story)
		git(t, filepath.Join(trees, story), "add", "-A")
		git(t, filepath.Join(trees, story), "commit", "-m", "work")
	}
	git(t, filepath.Join(trees, "s-pushed"), "push", "origin", "mw/s-pushed")
	// A directory beside them that is no worktree: a merge slot's.
	write(t, filepath.Join(trees, "rig.merge-slot.queue", "ticket"), "1")
	// A branch whose worktree is long gone.
	git(t, rigDir, "branch", "mw/s-branch-only", "main")
	return Disk{Rigs: map[string]string{"rig": rigDir}, Home: filepath.Join(root, "work"), Vault: filepath.Join(root, "vault")}, trees
}

func TestTreesSaysWhatKeepsEachWorktree(t *testing.T) {
	disk, trees := rigOnDisk(t)
	found, err := disk.Trees(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]application.HostSweepTree{}
	for _, tree := range found {
		by[tree.Story] = tree
	}
	if len(by) != 5 {
		t.Fatalf("found %d trees, want 5: %v", len(by), found)
	}
	clean := by["s-clean"]
	if clean.Uncommitted || clean.Unsaved != "" || clean.Busy != "" || clean.Branch != "mw/s-clean" || clean.Rig != "rig" {
		t.Errorf("the clean worktree is not plainly spare: %+v", clean)
	}
	if len(clean.Caches) != 1 || filepath.Base(clean.Caches[0].Path) != "node_modules" {
		t.Errorf("the clean worktree's caches: %+v", clean.Caches)
	}
	if !by["s-dirty"].Uncommitted {
		t.Error("the worktree with an uncommitted file is not kept")
	}
	if !strings.Contains(by["s-ahead"].Unsaved, "1 commit on mw/s-ahead is neither on main nor pushed") {
		t.Errorf("the worktree with a commit kept here: %q", by["s-ahead"].Unsaved)
	}
	if by["s-pushed"].Unsaved != "" {
		t.Errorf("a pushed commit is called unsaved: %q", by["s-pushed"].Unsaved)
	}
	if b := by["s-branch-only"]; b.Dir != "" || b.Unsaved != "" || b.Branch != "mw/s-branch-only" {
		t.Errorf("the branch with no worktree: %+v", b)
	}

	// A process running in a worktree keeps it.
	cmd := exec.Command("sleep", "30")
	cmd.Dir = filepath.Join(trees, "s-clean")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	found, _ = disk.Trees(context.Background())
	for _, tree := range found {
		if tree.Story == "s-clean" && !strings.Contains(tree.Busy, "a process is running in it") {
			t.Errorf("a worktree with a process in it is spare: %+v", tree)
		}
	}
}

func TestRemoveTreeTakesTheWorktreeAndBranchAndRefusesAChange(t *testing.T) {
	disk, trees := rigOnDisk(t)
	ctx := context.Background()
	found, _ := disk.Trees(ctx)
	by := map[string]application.HostSweepTree{}
	for _, tree := range found {
		by[tree.Story] = tree
	}
	for _, story := range []string{"s-clean", "s-pushed", "s-branch-only"} {
		if err := disk.RemoveTree(ctx, by[story]); err != nil {
			t.Fatalf("removing %s: %v", story, err)
		}
	}
	if _, err := os.Stat(filepath.Join(trees, "s-clean")); !os.IsNotExist(err) {
		t.Error("the clean worktree is still there")
	}
	rigDir := disk.Rigs["rig"]
	if out := git(t, rigDir, "branch", "--list", "mw/s-clean", "mw/s-branch-only", "mw/s-pushed"); out != "" {
		t.Errorf("branches left: %q", out)
	}
	if err := disk.RemoveTree(ctx, by["s-dirty"]); err == nil {
		t.Error("git removed a worktree with an uncommitted file")
	}
	if _, err := os.Stat(filepath.Join(trees, "s-dirty", "new.txt")); err != nil {
		t.Errorf("the uncommitted file is gone: %v", err)
	}
}

func TestLandingsRunsAndAsidesAreFoundWithTheirAge(t *testing.T) {
	disk, trees := rigOnDisk(t)
	home := disk.Home
	old := time.Now().Add(-3 * 24 * time.Hour)
	landing := filepath.Join(home, ".mw-landing-x")
	git(t, disk.Rigs["rig"], "worktree", "add", "--detach", landing, "main")
	if err := os.Chtimes(landing, old, old); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(disk.Vault, "runs", "mw-old", "result.json"), "{}")
	write(t, filepath.Join(home, "beads-dolt-aside-2026"), "") // a file, not a directory: not listed
	write(t, filepath.Join(home, "beads-dolt-aside-1", "x"), "data")
	write(t, filepath.Join(home, "other", "x"), "data")

	ctx := context.Background()
	landings, _ := disk.Landings(ctx)
	if len(landings) != 1 || landings[0].Live != "" || landings[0].Modified.After(old.Add(time.Second)) {
		t.Errorf("landings: %+v", landings)
	}
	runs, _ := disk.Runs(ctx)
	if len(runs) != 1 || runs[0].Bytes != 2 {
		t.Errorf("runs: %+v", runs)
	}
	asides, _ := disk.Asides(ctx)
	if len(asides) != 1 || filepath.Base(asides[0].Path) != "beads-dolt-aside-1" || asides[0].Bytes != 4 {
		t.Errorf("asides: %+v", asides)
	}
	if err := disk.RemoveDir(ctx, asides[0].Path); err != nil {
		t.Fatal(err)
	}
	if err := disk.RemoveDir(ctx, "relative/dir"); err == nil {
		t.Error("a relative path was removed")
	}
	_ = trees
}
