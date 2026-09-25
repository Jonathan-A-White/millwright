// Package rig cuts a story its own working copy of a rig by shelling out to
// git. It is the adapter behind application.Worktrees.
//
// A story is worked in a git worktree of the rig rather than in the rig's own
// checkout: two stories may be in flight at once on one host, and neither may
// see the other's half-finished work. The worktree sits beside the rig, on a
// branch of the story's own, cut from the target branch as the rig's origin has
// it — never from a local branch, which on a two-host factory may be behind
// whatever the other host pushed an hour ago.
package rig

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// Program is the command this adapter shells out to, and DefaultRemote is the
// remote a rig's target branch is read from.
const (
	Program       = "git"
	DefaultRemote = "origin"
)

// Worktrees makes and removes the worktrees of the rigs this host has checked
// out. It holds no state: every call names the rig it works in.
type Worktrees struct {
	program string
	remote  string
}

// Worktrees satisfies the port.
var _ application.Worktrees = (*Worktrees)(nil)

// Option is a setting of a Worktrees, given to New.
type Option func(*Worktrees)

// WithProgram names the git command to run, for a host that keeps it somewhere
// unusual — and for a test that needs a stand-in.
func WithProgram(program string) Option {
	return func(w *Worktrees) { w.program = program }
}

// WithRemote names the remote a rig's target branch is read from.
func WithRemote(remote string) Option {
	return func(w *Worktrees) { w.remote = remote }
}

// New returns a Worktrees that runs git as this host has it.
func New(opts ...Option) *Worktrees {
	w := &Worktrees{program: Program, remote: DefaultRemote}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Remote reports the remote this adapter cuts branches from.
func (w *Worktrees) Remote() string { return w.remote }

// Available reports whether git is on PATH. Tests that need a real repository
// skip themselves when it is not.
func Available() bool {
	_, err := exec.LookPath(Program)
	return err == nil
}

// Fetch implements application.Worktrees.
func (w *Worktrees) Fetch(ctx context.Context, rigDir string) error {
	_, err := w.git(ctx, rigDir, "fetch", w.remote)
	return err
}

// Add implements application.Worktrees.
func (w *Worktrees) Add(ctx context.Context, rigDir, dir, branch, start string) error {
	switch {
	case dir == "":
		return fmt.Errorf("a worktree of %s needs a directory", rigDir)
	case branch == "":
		return fmt.Errorf("a worktree of %s needs a branch", rigDir)
	case start == "":
		return fmt.Errorf("the branch %s has nothing to be cut from", branch)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return fmt.Errorf("making the directory the worktree %s belongs in: %w", dir, err)
	}
	_, err := w.git(ctx, rigDir, "worktree", "add", "-b", branch, dir, start)
	return err
}

// Exists implements application.Worktrees.
func (w *Worktrees) Exists(ctx context.Context, rigDir, dir, branch string) (bool, error) {
	if dir != "" {
		if _, err := os.Stat(dir); err == nil {
			return true, nil
		} else if !os.IsNotExist(err) {
			return false, fmt.Errorf("checking whether %s is already there: %w", dir, err)
		}
	}
	if branch != "" {
		if _, err := w.git(ctx, rigDir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
			return true, nil
		}
	}
	return false, nil
}

// Remove implements application.Worktrees: the worktree and its branch go, and
// what was never there is not complained about.
func (w *Worktrees) Remove(ctx context.Context, rigDir, dir, branch string) error {
	if dir != "" {
		if _, err := os.Stat(dir); err == nil {
			// Force, because a dispatch that failed may have left a file in it,
			// and a worktree nobody could start work in holds nothing worth
			// keeping.
			if _, err := w.git(ctx, rigDir, "worktree", "remove", "--force", dir); err != nil {
				return err
			}
		}
	}
	if _, err := w.git(ctx, rigDir, "worktree", "prune"); err != nil {
		return err
	}
	if branch == "" {
		return nil
	}
	if _, err := w.git(ctx, rigDir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err != nil {
		return nil // no such branch: nothing to delete
	}
	_, err := w.git(ctx, rigDir, "branch", "-D", branch)
	return err
}

// RemoveWithoutForce implements application.Worktrees: the same as Remove,
// except that a worktree still holding uncommitted work is refused rather
// than thrown away, and so is its branch.
func (w *Worktrees) RemoveWithoutForce(ctx context.Context, rigDir, dir string) error {
	if dir != "" {
		if _, err := os.Stat(dir); err == nil {
			if _, err := w.git(ctx, rigDir, "worktree", "remove", dir); err != nil {
				return err
			}
		}
	}
	_, err := w.git(ctx, rigDir, "worktree", "prune")
	return err
}

// DeleteBranch implements application.Worktrees: a safe delete first, forcing
// only when that refuses it.
func (w *Worktrees) DeleteBranch(ctx context.Context, rigDir, branch string) error {
	if branch == "" {
		return nil
	}
	if _, err := w.git(ctx, rigDir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err != nil {
		return nil // no such branch: nothing to delete
	}
	if _, err := w.git(ctx, rigDir, "branch", "-d", branch); err == nil {
		return nil
	}
	_, err := w.git(ctx, rigDir, "branch", "-D", branch)
	return err
}

// git runs one git command in a rig and returns its standard output. It never
// prompts: a dispatch may run from a hook with nobody at the keyboard, and a
// command waiting for a password would hang there unnoticed.
func (w *Worktrees) git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, w.program, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")

	var out, errs bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errs

	if err := cmd.Run(); err != nil {
		if said := strings.TrimSpace(errs.String() + "\n" + out.String()); said != "" {
			return "", fmt.Errorf("%s %s in %s: %w: %s", w.program, strings.Join(args, " "), dir, err, said)
		}
		return "", fmt.Errorf("%s %s in %s: %w", w.program, strings.Join(args, " "), dir, err)
	}
	return out.String(), nil
}
