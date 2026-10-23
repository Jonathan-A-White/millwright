package rig

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/gitcmd"
)

// GrindBranch is the branch an app's grinds are read from: the checkout's
// own local main, a landed commit, never a branch a session is working. A
// landing made on another host moves the remote's main and not this one, so
// Commit reads the remote's main instead whenever it is ahead of this one.
const GrindBranch = "refs/heads/main"

// GrindRefreshLimit is the longest the mill lets one refresh of a checkout's
// main run, the fetch and its retries together: a remote that cannot answer
// (GitHub down) must not hold every grist behind it for minutes (mw-gq6.647).
const GrindRefreshLimit = 15 * time.Second

// Grinds reads an app's grinds from its rig's checkout by asking git: the
// adapter behind application.GrindSource. It reads the object store only,
// never the working tree, so whatever is half-done in the checkout is never
// what the mill runs.
type Grinds struct {
	program  string
	remote   string
	raceWait time.Duration
	limit    time.Duration
}

// Grinds satisfies the port.
var _ application.GrindSource = (*Grinds)(nil)

// NewGrinds is a grind source that runs git as this host has it.
func NewGrinds(opts ...Option) *Grinds {
	w := New(opts...)
	return &Grinds{program: w.program, remote: w.remote, raceWait: w.raceWait, limit: w.refreshLimit}
}

// Refresh implements application.GrindSource: it fetches the remote's main
// into the checkout's view of it, and moves nothing else. A fetch that lost a
// ref's lock to another git process in the checkout, a close-out's fetch most
// often, is tried again here RaceRetries times, a growing short wait apart:
// the mill has nobody else to try it again, and keeps a failed refresh for a
// while (mw-gq6.439). Any other failure comes back at once. The whole refresh
// is held to GrindRefreshLimit: a fetch that runs past it is killed and comes
// back wrapping application.ErrGrindRefreshTimedOut (mw-gq6.647).
func (g *Grinds) Refresh(ctx context.Context, checkout string) error {
	parent := ctx
	if g.limit > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, g.limit)
		defer cancel()
	}
	err := g.refresh(ctx, checkout)
	if err != nil && parent.Err() == nil && ctx.Err() != nil {
		return fmt.Errorf("%w: %w after %v", err, application.ErrGrindRefreshTimedOut, g.limit)
	}
	return err
}

func (g *Grinds) refresh(ctx context.Context, checkout string) error {
	for retry := 0; ; retry++ {
		_, err := g.git(ctx, checkout, "fetch", "--quiet", g.remote, "main")
		if err == nil {
			return nil
		}
		err = fmt.Errorf("fetching main from %s in %s: %w", g.remote, checkout, err)
		if _, raced := raceWhy(err); !raced || retry == RaceRetries {
			return err
		}
		if waitErr := sleep(ctx, g.raceWait*time.Duration(retry+1)); waitErr != nil {
			return fmt.Errorf("%w (and waiting to fetch again: %v)", err, waitErr)
		}
	}
}

// sleep waits d, or until ctx ends, whichever is first.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
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

// Grinds satisfies the port that reads a ref.
var _ application.GrindRefs = (*Grinds)(nil)

// ResolveRef implements application.GrindRefs. A ref that is a directory is a
// worktree of the rig: its HEAD is read, from it, and a note says when it holds
// changes that are not committed. Any other ref is resolved in the checkout.
func (g *Grinds) ResolveRef(ctx context.Context, checkout, ref string) (readFrom, commit, note string, err error) {
	readFrom, rev := checkout, ref
	worktree := false
	if info, statErr := os.Stat(ref); statErr == nil && info.IsDir() {
		readFrom, rev, worktree = ref, "HEAD", true
	}
	out, err := g.git(ctx, readFrom, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	if err != nil {
		return "", "", "", fmt.Errorf("the ref %q names no commit in %s: %w", ref, readFrom, err)
	}
	commit = strings.TrimSpace(out)
	if worktree {
		if dirty, derr := g.git(ctx, readFrom, "status", "--porcelain", "--untracked-files=no"); derr == nil && strings.TrimSpace(dirty) != "" {
			note = fmt.Sprintf("%s has changes that are not committed; the grind is read at its commit %s", readFrom, commit[:min(12, len(commit))])
		}
	}
	return readFrom, commit, note, nil
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

// List implements application.GrindSource: `git ls-tree -r` names every file
// under dir at commit.
func (g *Grinds) List(ctx context.Context, checkout, commit, dir string) ([]string, error) {
	listed, err := g.git(ctx, checkout, "ls-tree", "-r", "--name-only", "-z", "--full-tree", commit, "--", dir)
	if err != nil {
		return nil, fmt.Errorf("listing %s at %s in %s: %w", dir, commit, checkout, err)
	}
	var files []string
	for _, name := range strings.Split(listed, "\x00") {
		if name != "" {
			files = append(files, name)
		}
	}
	sort.Strings(files)
	return files, nil
}

// git runs one git command in the checkout, never prompting.
func (g *Grinds) git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := gitcmd.Command(ctx, g.program, "", append([]string{"-C", dir}, args...)...)
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
