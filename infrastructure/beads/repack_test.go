package beads

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// remoteCacheFixture makes a bare repo at the path a Dolt git-remote-cache
// would sit at under a vault, and pushes several small commits into it from a
// throwaway working clone — each push its own pack, receive.unpackLimit=0
// forcing git to keep it as one rather than exploding it into loose objects,
// the same shape `bd dolt push` leaves behind on every sync. It reports the
// refs it pushed, so a test can check they still resolve after a repack.
func remoteCacheFixture(t *testing.T, vault string, pushes int) (repo string, refs []string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("this fixture shells out to git")
	}
	repo = filepath.Join(vault, beadsDir, "embeddeddolt", "x", ".dolt", "git-remote-cache", "h", "repo.git")
	if _, err := runGit(context.Background(), "", "init", "--bare", "-q", repo); err != nil {
		t.Fatalf("making the bare remote cache: %v", err)
	}
	if _, err := runGit(context.Background(), repo, "config", "receive.unpackLimit", "0"); err != nil {
		t.Fatalf("configuring the bare remote cache to keep pushes packed: %v", err)
	}
	if _, err := runGit(context.Background(), repo, "config", "gc.auto", "0"); err != nil {
		t.Fatalf("disabling auto gc on the bare remote cache: %v", err)
	}

	work := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"remote", "add", "origin", repo},
	} {
		if _, err := runGit(context.Background(), work, args...); err != nil {
			t.Fatalf("setting up the working clone: %v", err)
		}
	}

	for i := 0; i < pushes; i++ {
		path := filepath.Join(work, fmt.Sprintf("file%d.txt", i))
		if err := os.WriteFile(path, []byte(fmt.Sprintf("commit %d\n", i)), 0o644); err != nil {
			t.Fatalf("writing a file to commit: %v", err)
		}
		ref := fmt.Sprintf("refs/heads/b%d", i)
		for _, args := range [][]string{
			{"add", "."},
			{"commit", "-q", "-m", fmt.Sprintf("commit %d", i)},
			{"push", "-q", "origin", "HEAD:" + ref},
		} {
			if _, err := runGit(context.Background(), work, args...); err != nil {
				t.Fatalf("pushing commit %d: %v", i, err)
			}
		}
		refs = append(refs, ref)
	}
	return repo, refs
}

// mustPackCount counts the pack files a bare repo holds, so a test can say a
// repack collapsed several into one.
func mustPackCount(t *testing.T, repo string) int {
	t.Helper()
	count, err := packCount(repo)
	if err != nil {
		t.Fatalf("listing packs in %s: %v", repo, err)
	}
	return count
}

func TestRepackRemoteCachesCollapsesEveryPackIntoOneAndKeepsEveryRefResolving(t *testing.T) {
	vault := t.TempDir()
	repo, refs := remoteCacheFixture(t, vault, 5)

	if before := mustPackCount(t, repo); before < 2 {
		t.Fatalf("expected the fixture to leave several packs, got %d", before)
	}

	gateway := New(vault)
	if err := gateway.repackRemoteCaches(context.Background()); err != nil {
		t.Fatalf("repacking the remote cache: %v", err)
	}

	if after := mustPackCount(t, repo); after != 1 {
		t.Fatalf("expected exactly one pack left after repacking, got %d", after)
	}
	for _, ref := range refs {
		if _, err := runGit(context.Background(), repo, "rev-parse", ref); err != nil {
			t.Fatalf("expected %s to still resolve after repacking, got %v", ref, err)
		}
	}
}

func TestRepackRemoteCachesFindsEveryDoltDatabaseUnderTheVault(t *testing.T) {
	vault := t.TempDir()
	firstRepo, _ := remoteCacheFixture(t, vault, 2)
	secondRepo := filepath.Join(vault, beadsDir, "embeddeddolt", "y", ".dolt", "git-remote-cache", "h2", "repo.git")
	if _, err := runGit(context.Background(), "", "init", "--bare", "-q", secondRepo); err != nil {
		t.Fatalf("making the second bare remote cache: %v", err)
	}

	gateway := New(vault)
	if err := gateway.repackRemoteCaches(context.Background()); err != nil {
		t.Fatalf("repacking the remote caches: %v", err)
	}
	if after := mustPackCount(t, firstRepo); after != 1 {
		t.Fatalf("expected the first cache to end with one pack, got %d", after)
	}
}

func TestRepackRemoteCachesWithNoneUnderTheVaultDoesNothing(t *testing.T) {
	gateway := New(t.TempDir())
	if err := gateway.repackRemoteCaches(context.Background()); err != nil {
		t.Fatalf("expected a vault with no remote cache yet not to fail, got %v", err)
	}
}

// TestGCAlsoRepacksTheGitRemoteCache is the friction this story fixes: bd's
// own gc never touches the git-remote-cache Dolt leaves behind at
// .beads/embeddeddolt/<db>/.dolt/git-remote-cache/<hash>/repo.git, one full
// pack per `bd dolt push`/pull, so it is repacked here beside it.
func TestGCAlsoRepacksTheGitRemoteCache(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nexit 0\n"
	path := filepath.Join(dir, "bd-stand-in")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	gateway := New(dir, WithProgram(path))

	repo, refs := remoteCacheFixture(t, dir, 5)
	if before := mustPackCount(t, repo); before < 2 {
		t.Fatalf("expected the fixture to leave several packs, got %d", before)
	}

	if err := gateway.GC(context.Background()); err != nil {
		t.Fatalf("collecting: %v", err)
	}

	if after := mustPackCount(t, repo); after != 1 {
		t.Fatalf("expected GC to leave exactly one pack in the remote cache, got %d", after)
	}
	for _, ref := range refs {
		if _, err := runGit(context.Background(), repo, "rev-parse", ref); err != nil {
			t.Fatalf("expected %s to still resolve after GC repacked the cache, got %v", ref, err)
		}
	}
}

// standInBD writes a stand-in for bd that always exits 0, so a test can call
// Gateway.Sync without a real beads database.
func standInBD(t *testing.T, dir string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	path := filepath.Join(dir, "bd-stand-in")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return path
}

// TestSyncAlsoRepacksACacheThatHasGrownPastTheThreshold is mw-gq6.104's
// friction: bd's own gc runs once a day, but the git-remote-cache Dolt keeps
// beside the database gains a full pack on every `bd dolt push`/pull — far
// faster than the daily GC can keep up on a small disk. So every sync, not
// only the once-a-day GC, checks each cache's pack count and repacks the ones
// that have grown past remoteCacheRepackThreshold.
func TestSyncAlsoRepacksACacheThatHasGrownPastTheThreshold(t *testing.T) {
	dir := t.TempDir()
	path := standInBD(t, dir)
	gateway := New(dir, WithProgram(path))

	repo, refs := remoteCacheFixture(t, dir, remoteCacheRepackThreshold+1)
	if before := mustPackCount(t, repo); before != remoteCacheRepackThreshold+1 {
		t.Fatalf("expected the fixture to leave %d packs, got %d", remoteCacheRepackThreshold+1, before)
	}

	if err := gateway.Sync(context.Background()); err != nil {
		t.Fatalf("syncing: %v", err)
	}

	if after := mustPackCount(t, repo); after != 1 {
		t.Fatalf("expected a sync to leave exactly one pack in a crowded cache, got %d", after)
	}
	for _, ref := range refs {
		if _, err := runGit(context.Background(), repo, "rev-parse", ref); err != nil {
			t.Fatalf("expected %s to still resolve after the sync repacked the cache, got %v", ref, err)
		}
	}
}

// TestSyncLeavesACacheAtTheThresholdAlone is the other half: a cache that has
// not yet grown past remoteCacheRepackThreshold packs costs nothing extra on
// a sync that runs every few minutes.
func TestSyncLeavesACacheAtTheThresholdAlone(t *testing.T) {
	dir := t.TempDir()
	path := standInBD(t, dir)
	gateway := New(dir, WithProgram(path))

	repo, _ := remoteCacheFixture(t, dir, remoteCacheRepackThreshold)
	before := mustPackCount(t, repo)
	if before != remoteCacheRepackThreshold {
		t.Fatalf("expected the fixture to leave %d packs, got %d", remoteCacheRepackThreshold, before)
	}

	if err := gateway.Sync(context.Background()); err != nil {
		t.Fatalf("syncing: %v", err)
	}

	if after := mustPackCount(t, repo); after != before {
		t.Fatalf("expected a cache at the threshold to be left untouched, had %d packs, now %d", before, after)
	}
}

// TestSyncSwallowsAFailureRepackingACrowdedCache is mw-gq6.104's third
// acceptance criterion: a repack that fails on the pack-count path must never
// turn a sync itself into a halt — the sync already got through, and this
// housekeeping is not worth losing that over.
func TestSyncSwallowsAFailureRepackingACrowdedCache(t *testing.T) {
	dir := t.TempDir()
	path := standInBD(t, dir)
	gateway := New(dir, WithProgram(path))

	repo, _ := remoteCacheFixture(t, dir, remoteCacheRepackThreshold+1)
	// Take away the write permission repacking needs, so repacking this one
	// cache fails, without touching whether `bd sync` itself succeeded.
	if err := os.Chmod(filepath.Join(repo, "objects", "pack"), 0o500); err != nil {
		t.Fatalf("making the pack directory read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(repo, "objects", "pack"), 0o700) })

	if err := gateway.Sync(context.Background()); err != nil {
		t.Fatalf("expected a repack failure not to fail the sync, got %v", err)
	}
}
