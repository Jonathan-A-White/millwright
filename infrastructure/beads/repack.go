package beads

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// remoteCacheGlob finds every git-remote-cache bare clone Dolt keeps beside a
// vault's embedded database, one per Dolt remote a host has ever pushed to or
// pulled from: .beads/embeddeddolt/<db>/.dolt/git-remote-cache/<hash>/repo.git.
// Each `bd dolt push` or `pull` leaves a new full pack there; it is the one
// thing `bd gc` never touches.
const remoteCacheGlob = "embeddeddolt/*/.dolt/git-remote-cache/*/repo.git"

// repackRemoteCaches repacks every git-remote-cache bare clone under the
// vault's .beads, low-memory (pack.threads=1, a 32m window) to fit a host as
// small as the VPS. Every cache found is tried even after one fails, so that
// one broken cache does not hide another's failure; their errors come back
// joined, to be reported the same way any other GC failure is.
func (g *Gateway) repackRemoteCaches(ctx context.Context) error {
	matches, err := filepath.Glob(filepath.Join(g.vault, beadsDir, remoteCacheGlob))
	if err != nil {
		return fmt.Errorf("finding the git-remote-cache under %s: %w", g.vault, err)
	}
	var failures []error
	for _, repo := range matches {
		if err := repackOneRemoteCache(ctx, repo); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// repackOneRemoteCache collapses every pack a bare git-remote-cache clone has
// accumulated into one, then removes the loose objects that repack leaves
// duplicated on disk.
func repackOneRemoteCache(ctx context.Context, repo string) error {
	if _, err := runGit(ctx, repo, "-c", "pack.threads=1", "-c", "pack.windowMemory=32m", "repack", "-a", "-d", "-q"); err != nil {
		return fmt.Errorf("repacking %s: %w", repo, err)
	}
	if _, err := runGit(ctx, repo, "prune-packed"); err != nil {
		return fmt.Errorf("pruning %s after repacking it: %w", repo, err)
	}
	return nil
}

// runGit runs one git command in dir and returns its combined output, never
// prompting: this may run on a timer with nobody at the keyboard.
func runGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("git %s in %s: %w: %s", strings.Join(args, " "), dir, err, strings.TrimSpace(string(out)))
	}
	return out, nil
}
