package beads

import (
	"context"
	"errors"
	"fmt"
	"os"
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

// remoteCacheRepackThreshold is how many packs a git-remote-cache clone may
// hold before a sync repacks it on its own, between the daily GCs: on a host
// ticking every few minutes, each carrying a fresh `bd dolt push`/pull, the
// cache can gain one full pack every cycle — far faster than a once-a-day
// collection can keep the disk in check.
const remoteCacheRepackThreshold = 8

// remoteCacheRepackThresholdBytes is how many bytes a git-remote-cache
// clone's packs may hold in total before a sync repacks it early, even under
// remoteCacheRepackThreshold packs: a handful of large `bd dolt push`/pull
// cycles can carry the cache well past this long before it has piled up
// enough packs to trip the count-based rule, which is what let it cross the
// doctor's own byte budget between GCs before this existed.
const remoteCacheRepackThresholdBytes = 256_000_000

// repackRemoteCaches repacks every git-remote-cache bare clone under the
// vault's .beads, low-memory (pack.threads=1, a 32m window) to fit a host as
// small as the VPS. Every cache found is tried even after one fails, so that
// one broken cache does not hide another's failure; their errors come back
// joined, to be reported the same way any other GC failure is.
func (g *Gateway) repackRemoteCaches(ctx context.Context) error {
	return g.repackRemoteCachesMatching(ctx, nil)
}

// repackCrowdedRemoteCaches repacks only the git-remote-cache clones that
// have grown past remoteCacheRepackThreshold packs, or whose packs' total
// size has grown past remoteCacheRepackThresholdBytes, so that a sync run
// every few minutes keeps the disk in check without paying a repack's cost
// on a cache with nothing worth collapsing yet.
func (g *Gateway) repackCrowdedRemoteCaches(ctx context.Context) error {
	return g.repackRemoteCachesMatching(ctx, isRemoteCacheCrowded)
}

// repackRemoteCachesMatching repacks every git-remote-cache bare clone under
// the vault's .beads for which crowded reports true; a nil crowded repacks
// every cache found, whatever its size, which is what the daily GC asks for.
// Every cache found is tried even after one fails, so that one broken cache
// does not hide another's failure; their errors come back joined.
func (g *Gateway) repackRemoteCachesMatching(ctx context.Context, crowded func(repo string) (bool, error)) error {
	matches, err := filepath.Glob(filepath.Join(g.vault, beadsDir, remoteCacheGlob))
	if err != nil {
		return fmt.Errorf("finding the git-remote-cache under %s: %w", g.vault, err)
	}
	var failures []error
	for _, repo := range matches {
		if crowded != nil {
			ok, err := crowded(repo)
			if err != nil {
				failures = append(failures, fmt.Errorf("checking whether %s is crowded: %w", repo, err))
				continue
			}
			if !ok {
				continue
			}
		}
		if err := repackOneRemoteCache(ctx, repo); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// isRemoteCacheCrowded reports whether a git-remote-cache clone has grown
// past remoteCacheRepackThreshold packs or remoteCacheRepackThresholdBytes of
// packs — either is enough on its own, since a handful of large pushes can
// cross the byte threshold long before the pack count does, and a great many
// small ones can cross the pack-count threshold long before the byte total
// does.
func isRemoteCacheCrowded(repo string) (bool, error) {
	count, err := packCount(repo)
	if err != nil {
		return false, err
	}
	if count > remoteCacheRepackThreshold {
		return true, nil
	}
	size, err := packBytes(repo)
	if err != nil {
		return false, err
	}
	return size > remoteCacheRepackThresholdBytes, nil
}

// packCount counts the pack files a bare git-remote-cache clone holds.
func packCount(repo string) (int, error) {
	entries, err := os.ReadDir(filepath.Join(repo, "objects", "pack"))
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".pack" {
			count++
		}
	}
	return count, nil
}

// packBytes totals the size of the pack files a bare git-remote-cache clone
// holds.
func packBytes(repo string) (int64, error) {
	entries, err := os.ReadDir(filepath.Join(repo, "objects", "pack"))
	if err != nil {
		return 0, err
	}
	var total int64
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".pack" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return 0, err
		}
		total += info.Size()
	}
	return total, nil
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
