package rig

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// Worktrees makes a checkout whose git objects are corrupt whole again through
// the same git it reads it with (mw-gq6.1125).
var _ application.CheckoutHealer = (*Worktrees)(nil)

// Unpushed implements application.CheckoutHealer: the commits a local branch or
// HEAD holds that no remote-tracking ref holds. A local ref at the very commit a
// remote ref is at is counted without reading an object, so that a checkout whose
// pack is corrupt can still be asked; any other is walked with rev-list, and a
// walk that fails is an error, not a count of none.
func (w *Worktrees) Unpushed(ctx context.Context, dir string) (int, error) {
	remoteOut, err := w.git(ctx, dir, "for-each-ref", "--format=%(objectname)", "refs/remotes/")
	if err != nil {
		return 0, err
	}
	onRemote := map[string]bool{}
	for _, sha := range strings.Fields(remoteOut) {
		onRemote[sha] = true
	}
	localOut, err := w.git(ctx, dir, "for-each-ref", "--format=%(objectname)", "refs/heads/")
	if err != nil {
		return 0, err
	}
	locals := strings.Fields(localOut)
	if head, err := w.git(ctx, dir, "rev-parse", "HEAD"); err == nil {
		locals = append(locals, strings.TrimSpace(head))
	}
	total := 0
	seen := map[string]bool{}
	for _, sha := range locals {
		if onRemote[sha] || seen[sha] {
			continue
		}
		seen[sha] = true
		said, err := w.git(ctx, dir, "rev-list", "--count", sha, "--not", "--remotes")
		if err != nil {
			return 0, err
		}
		n, err := strconv.Atoi(strings.TrimSpace(said))
		if err != nil {
			return 0, fmt.Errorf("counting the commits of %s that origin lacks: %s said %q", dir, w.program, said)
		}
		total += n
	}
	return total, nil
}

// Reclone implements application.CheckoutHealer: dir is renamed to aside (to
// aside-2, aside-3 and so on when that is taken), and the remote's branch is
// cloned into dir from the URL the old checkout's config names. A clone that
// fails removes what it made and puts the old checkout back.
func (w *Worktrees) Reclone(ctx context.Context, dir, aside, branch string) (string, error) {
	switch {
	case dir == "" || aside == "":
		return "", fmt.Errorf("cloning a checkout again: which one?")
	case branch == "":
		return "", fmt.Errorf("cloning %s again: which branch?", dir)
	}
	url, err := w.git(ctx, dir, "config", "--get", "remote."+w.remote+".url")
	if err != nil {
		return "", err
	}
	url = strings.TrimSpace(url)
	if url == "" {
		return "", fmt.Errorf("cloning %s again: it has no %s URL", dir, w.remote)
	}
	moved := aside
	for n := 2; ; n++ {
		if _, err := os.Lstat(moved); os.IsNotExist(err) {
			break
		}
		moved = fmt.Sprintf("%s-%d", aside, n)
	}
	if err := os.Rename(dir, moved); err != nil {
		return "", err
	}
	parent := filepath.Dir(dir)
	if _, err := w.git(ctx, parent, "clone", "--quiet", "--branch", branch, "--origin", w.remote, url, dir); err != nil {
		_ = os.RemoveAll(dir)
		if back := os.Rename(moved, dir); back != nil {
			return "", fmt.Errorf("%w (and the old checkout could not be put back from %s: %v)", err, moved, back)
		}
		return "", err
	}
	return moved, nil
}
