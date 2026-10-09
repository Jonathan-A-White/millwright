package rig

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// GrindBranch is the branch an app's grinds are read from: the checkout's
// own local main, a landed commit, never a branch a session is working. A
// landing made on another host moves the remote's main and not this one, so
// Commit reads the remote's main instead whenever it is ahead of this one.
const GrindBranch = "refs/heads/main"

// Grinds reads an app's grinds from its rig's checkout by asking git: the
// adapter behind application.GrindSource. It reads the object store only,
// never the working tree, so whatever is half-done in the checkout is never
// what the mill runs.
type Grinds struct {
	program string
	remote  string
}

// Grinds satisfies the port.
var _ application.GrindSource = (*Grinds)(nil)

// NewGrinds is a grind source that runs git as this host has it.
func NewGrinds(opts ...Option) *Grinds {
	w := New(opts...)
	return &Grinds{program: w.program, remote: w.remote}
}

// Refresh implements application.GrindSource: it fetches the remote's main
// into the checkout's view of it, and moves nothing else.
func (g *Grinds) Refresh(ctx context.Context, checkout string) error {
	if _, err := g.git(ctx, checkout, "fetch", "--quiet", g.remote, "main"); err != nil {
		return fmt.Errorf("fetching main from %s in %s: %w", g.remote, checkout, err)
	}
	return nil
}

// Commit implements application.GrindSource: the commit the remote's main is
// at, as the last fetch saw it, when the checkout's own main is an ancestor of
// it; otherwise the commit the checkout's local main is at.
func (g *Grinds) Commit(ctx context.Context, checkout string) (string, error) {
	out, err := g.git(ctx, checkout, "rev-parse", "--verify", "--quiet", GrindBranch+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("reading which commit main is at in %s: %w", checkout, err)
	}
	local := strings.TrimSpace(out)
	out, err = g.git(ctx, checkout, "rev-parse", "--verify", "--quiet", "refs/remotes/"+g.remote+"/main^{commit}")
	if err != nil {
		return local, nil
	}
	remote := strings.TrimSpace(out)
	if remote == local {
		return local, nil
	}
	if _, err := g.git(ctx, checkout, "merge-base", "--is-ancestor", local, remote); err != nil {
		return local, nil
	}
	return remote, nil
}

// ReadAt implements application.GrindSource: `git ls-tree` says whether the
// path is a file at commit, and `git cat-file blob` reads it.
func (g *Grinds) ReadAt(ctx context.Context, checkout, commit, path string) ([]byte, bool, error) {
	listed, err := g.git(ctx, checkout, "ls-tree", "--full-tree", commit, "--", path)
	if err != nil {
		return nil, false, fmt.Errorf("looking for %s at %s in %s: %w", path, commit, checkout, err)
	}
	fields := strings.Fields(listed)
	if len(fields) < 2 || fields[1] != "blob" {
		return nil, false, nil
	}
	data, err := g.git(ctx, checkout, "cat-file", "blob", commit+":"+path)
	if err != nil {
		return nil, false, fmt.Errorf("reading %s at %s in %s: %w", path, commit, checkout, err)
	}
	return []byte(data), true, nil
}

// git runs one git command in the checkout, never prompting.
func (g *Grinds) git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, g.program, append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	if err := cmd.Run(); err != nil {
		if said := strings.TrimSpace(errs.String()); said != "" {
			return "", fmt.Errorf("%s %s: %w: %s", g.program, strings.Join(args, " "), err, said)
		}
		return "", fmt.Errorf("%s %s: %w", g.program, strings.Join(args, " "), err)
	}
	return out.String(), nil
}
