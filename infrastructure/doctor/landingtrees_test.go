package doctor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v: %s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// aRigWithALanding makes a rig on main with a commit, and a landing worktree
// of it detached at main, beside it.
func aRigWithALanding(t *testing.T) (rig, landing string) {
	t.Helper()
	parent := t.TempDir()
	rig = filepath.Join(parent, "rig")
	if err := os.MkdirAll(rig, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, rig, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(rig, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, rig, "add", ".")
	gitIn(t, rig, "commit", "-q", "-m", "first")
	landing = filepath.Join(parent, ".mw-landing-123")
	gitIn(t, rig, "worktree", "add", "-q", "--detach", landing, "main")
	return rig, landing
}

func TestLandingTreesRemovesACleanLandingOnMainAndPrunesIt(t *testing.T) {
	rig, landing := aRigWithALanding(t)
	trees := LandingTrees{}

	if spare, why := trees.Spare(context.Background(), landing); !spare {
		t.Fatalf("expected a clean landing on main to be spare, got %q", why)
	}
	if err := trees.Remove(context.Background(), landing); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(landing); !os.IsNotExist(err) {
		t.Errorf("expected %s gone, got %v", landing, err)
	}
	if listed := gitIn(t, rig, "worktree", "list"); strings.Contains(listed, "mw-landing") {
		t.Errorf("expected git's record pruned, got %q", listed)
	}
}

func TestLandingTreesKeepsALandingThatMayHoldSomeonesWork(t *testing.T) {
	t.Run("uncommitted changes", func(t *testing.T) {
		_, landing := aRigWithALanding(t)
		if err := os.WriteFile(filepath.Join(landing, "new.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		spare, why := LandingTrees{}.Spare(context.Background(), landing)
		if spare || !strings.Contains(why, "uncommitted") {
			t.Errorf("expected it kept for uncommitted changes, got %v %q", spare, why)
		}
	})
	t.Run("a commit not on main", func(t *testing.T) {
		_, landing := aRigWithALanding(t)
		if err := os.WriteFile(filepath.Join(landing, "a.txt"), []byte("changed"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, landing, "commit", "-q", "-am", "mine")
		spare, why := LandingTrees{}.Spare(context.Background(), landing)
		if spare || !strings.Contains(why, "not on main") {
			t.Errorf("expected it kept for a commit not on main, got %v %q", spare, why)
		}
	})
	t.Run("a process running in it", func(t *testing.T) {
		_, landing := aRigWithALanding(t)
		sleeper := exec.Command("sleep", "60")
		sleeper.Dir = landing
		if err := sleeper.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = sleeper.Process.Kill(); _ = sleeper.Wait() }()
		spare, why := LandingTrees{}.Spare(context.Background(), landing)
		if spare || !strings.Contains(why, "process is running") {
			t.Errorf("expected it kept for a running process, got %v %q", spare, why)
		}
	})
}
