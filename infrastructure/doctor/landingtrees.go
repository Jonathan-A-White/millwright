package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/gitcmd"
)

// LandingTrees is the real application.LandingTrees: it asks git whether a
// landing worktree is clean and its HEAD already on the rig's main, and /proc
// whether a process is running in it (mw-gq6.882).
type LandingTrees struct {
	// Program is the git command; empty reads "git".
	Program string
	// ProcDir is the directory the processes are read from; empty reads "/proc".
	ProcDir string
}

var _ application.LandingTrees = LandingTrees{}

// mainRefs are the branches a landing's HEAD may be an ancestor of to be on the
// rig's main, the remote's copy first.
var mainRefs = []string{"refs/remotes/origin/main", "refs/heads/main", "refs/remotes/origin/master", "refs/heads/master"}

func (l LandingTrees) program() string {
	if l.Program != "" {
		return l.Program
	}
	return "git"
}

func (l LandingTrees) git(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := gitcmd.Command(ctx, l.program(), dir, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// Spare implements application.LandingTrees.
func (l LandingTrees) Spare(ctx context.Context, dir string) (bool, string) {
	if pids := l.runningIn(dir); len(pids) > 0 {
		return false, "a process is running in it (pid " + strings.Join(pids, ", ") + ")"
	}
	status, err := l.git(ctx, dir, "status", "--porcelain")
	if err != nil {
		return false, "git cannot read it as a worktree: " + oneLineOf(status)
	}
	if status != "" {
		return false, "it has uncommitted changes"
	}
	head, err := l.git(ctx, dir, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return false, "its HEAD cannot be read"
	}
	for _, ref := range mainRefs {
		if _, err := l.git(ctx, dir, "rev-parse", "--verify", "--quiet", ref); err != nil {
			continue
		}
		cmd := gitcmd.Command(ctx, l.program(), dir, "merge-base", "--is-ancestor", head, ref)
		if cmd.Run() == nil {
			return true, ""
		}
	}
	return false, "its HEAD has commits not on main"
}

// Remove implements application.LandingTrees: git removes the worktree (no
// force, so git refuses one with changes) and then forgets any stale record.
func (l LandingTrees) Remove(ctx context.Context, dir string) error {
	common, err := l.git(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return fmt.Errorf("finding the rig of %s: %s", dir, oneLineOf(common))
	}
	rig := filepath.Dir(filepath.Clean(common))
	if out, err := l.git(ctx, rig, "worktree", "remove", dir); err != nil {
		return fmt.Errorf("git worktree remove %s: %s", dir, oneLineOf(out))
	}
	_, _ = l.git(ctx, rig, "worktree", "prune")
	return nil
}

// runningIn lists the processes whose working directory is dir or under it: a
// landing's git, build or test runs there. A process that only has a file open
// there (a scan reading the tree) is not running in it.
func (l LandingTrees) runningIn(dir string) []string {
	root := dir
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		root = resolved
	}
	proc := l.ProcDir
	if proc == "" {
		proc = "/proc"
	}
	entries, err := os.ReadDir(proc)
	if err != nil {
		return nil
	}
	var pids []string
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		cwd, err := os.Readlink(filepath.Join(proc, e.Name(), "cwd"))
		if err != nil {
			continue
		}
		cwd = strings.TrimSuffix(cwd, " (deleted)")
		if cwd == root || strings.HasPrefix(cwd, strings.TrimSuffix(root, "/")+"/") {
			pids = append(pids, e.Name())
		}
	}
	return pids
}

func oneLineOf(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
