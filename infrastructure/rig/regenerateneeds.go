package rig

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// regenerateLinkCap is the most of the rig checkout's ignored paths a
// generator's run is given: a checkout that holds more than this of them is
// holding build output and caches, not the inputs of a generator.
const regenerateLinkCap = 200

// provide gives a generator's run what a landing worktree, cut bare from the
// target, lacks (mw-gq6.1672): the rig's node_modules, for a node rig (one with
// a package-lock.json), linked from the rig checkout as the checkout has them,
// or installed with npm ci when the checkout has none; and a link to each of the
// checkout's ignored paths the landing lacks (a lampas generator reads
// data/lxx/morpheus, which no commit holds). The links are made only for the run
// and returned undone by the func it gives back: CommitLeftovers and
// KeepLeftovers run `git add -A` in a landing, which would take a link that was
// left there into the rig's history. An npm ci is kept, as node_modules is
// ignored and goes with the landing. A refusal says what could not be done, in
// words for a CONFLICT line.
func (w *Worktrees) provide(ctx context.Context, dir string) (undo func(), err error) {
	undo = func() {}
	rigDir := w.rigOf(ctx, dir)
	if rigDir == "" {
		return undo, nil
	}
	var made []string
	undo = func() {
		for _, link := range made {
			_ = os.Remove(link)
		}
	}
	link := func(name string) error {
		from, to := filepath.Join(rigDir, filepath.FromSlash(name)), filepath.Join(dir, filepath.FromSlash(name))
		if _, err := os.Lstat(to); err == nil {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		if err := os.Symlink(from, to); err != nil {
			return err
		}
		made = append(made, to)
		return nil
	}

	if _, err := os.Stat(filepath.Join(dir, "package-lock.json")); err == nil {
		if _, err := os.Lstat(filepath.Join(dir, "node_modules")); err != nil {
			if _, err := os.Stat(filepath.Join(rigDir, "node_modules")); err == nil {
				if err := link("node_modules"); err != nil {
					undo()
					return func() {}, fmt.Errorf("node_modules of %s could not be linked into the landing: %v", rigDir, err)
				}
			} else if said, err := w.npmCi(ctx, dir); err != nil {
				return undo, fmt.Errorf("npm ci in the landing failed, so the generator has no node_modules: %v; its last lines: %s", err, lastLines(said))
			}
		}
	}

	kept, err := w.git(ctx, rigDir, "ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "-z")
	if err != nil {
		return undo, nil // the checkout cannot say; the generator is run as it was
	}
	n := 0
	for _, name := range strings.Split(kept, "\x00") {
		name = strings.TrimSuffix(name, "/")
		if name == "" || name == "node_modules" || strings.HasSuffix(name, "/node_modules") || strings.HasPrefix(name, ".git/") {
			continue
		}
		if n++; n > regenerateLinkCap {
			break
		}
		// A path whose parent the landing lacks is not an input of a tracked tree.
		if _, err := os.Stat(filepath.Dir(filepath.Join(dir, filepath.FromSlash(name)))); err != nil {
			continue
		}
		if err := link(name); err != nil {
			undo()
			return func() {}, fmt.Errorf("%s of %s could not be linked into the landing: %v", name, rigDir, err)
		}
	}
	return undo, nil
}

// rigOf is the directory of the rig checkout a landing worktree belongs to, from
// git's own record, or "" when it cannot be said.
func (w *Worktrees) rigOf(ctx context.Context, dir string) string {
	said, err := w.git(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return ""
	}
	common := filepath.Clean(strings.TrimSpace(said))
	if filepath.Base(common) != ".git" {
		return ""
	}
	return filepath.Dir(common)
}

// npmCi installs the packages of the rig in dir.
func (w *Worktrees) npmCi(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, w.npm, "ci", "--no-audit", "--no-fund")
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), err
}
