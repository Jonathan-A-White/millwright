package vault_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// These tests drive real git in a temp directory, with a HOME of its own that
// git has never heard of anybody in.

// aBirth is a Birth whose git lives in a throwaway home.
func aBirth(t *testing.T) *vault.Birth {
	t.Helper()
	if _, err := exec.LookPath(vault.Git); err != nil {
		t.Skipf("%s is not on PATH", vault.Git)
	}
	home := t.TempDir()
	return vault.NewBirth("mw@testhost", "HOME="+home, "XDG_CONFIG_HOME="+home, "GIT_CONFIG_NOSYSTEM=1")
}

func TestVacantAcceptsAMissingOrEmptyDirectoryOnly(t *testing.T) {
	birth := aBirth(t)
	root := t.TempDir()
	ctx := context.Background()

	if err := birth.Vacant(ctx, filepath.Join(root, "missing")); err != nil {
		t.Errorf("a directory that is not there was refused: %v", err)
	}
	if err := birth.Vacant(ctx, root); err != nil {
		t.Errorf("an empty directory was refused: %v", err)
	}

	write(t, root, "notes.txt", "mine\n")
	if err := birth.Vacant(ctx, root); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Errorf("a directory with a file in it: got %v, want a refusal saying it is not empty", err)
	}
	if err := birth.Vacant(ctx, filepath.Join(root, "notes.txt")); err == nil {
		t.Error("a file was accepted as a place to make a vault")
	}
}

func TestLayWritesTheWholeTemplateIncludingDotFiles(t *testing.T) {
	birth := aBirth(t)
	dir := filepath.Join(t.TempDir(), "vault")
	template := fstest.MapFS{
		"CLAUDE.md":              {Data: []byte("notes\n")},
		".gitignore":             {Data: []byte("*.db\n")},
		"seats/mayor/charter.md": {Data: []byte("charter\n")},
	}

	if err := birth.Lay(context.Background(), dir, template); err != nil {
		t.Fatalf("laying the template: %v", err)
	}
	for file, want := range map[string]string{"CLAUDE.md": "notes\n", ".gitignore": "*.db\n", "seats/mayor/charter.md": "charter\n"} {
		if got, err := os.ReadFile(filepath.Join(dir, file)); err != nil || string(got) != want {
			t.Errorf("%s holds %q (%v), want %q", file, got, err, want)
		}
	}
}

func TestCommitMakesOneCommitByTheFallbackAuthorWhereGitKnowsNobody(t *testing.T) {
	birth := aBirth(t)
	dir := t.TempDir()
	write(t, dir, "a.txt", "one\n")

	if err := birth.Commit(context.Background(), dir, "A fresh vault"); err != nil {
		t.Fatalf("committing: %v", err)
	}
	// What a later step adds is folded into the same commit, not put in a second.
	write(t, dir, "b.txt", "two\n")
	if err := birth.Commit(context.Background(), dir, "A fresh vault"); err != nil {
		t.Fatalf("committing again: %v", err)
	}

	if count := strings.TrimSpace(run(t, dir, "git", "rev-list", "--count", "HEAD")); count != "1" {
		t.Errorf("the vault has %s commits, want 1", count)
	}
	if files := run(t, dir, "git", "ls-tree", "-r", "--name-only", "HEAD"); files != "a.txt\nb.txt\n" {
		t.Errorf("the commit holds %q, want both files", files)
	}
	if who := strings.TrimSpace(run(t, dir, "git", "log", "--format=%an|%ae|%cn|%s")); who != "mw@testhost|mw@testhost|mw@testhost|A fresh vault" {
		t.Errorf("the commit reads %q", who)
	}
	if branch := strings.TrimSpace(run(t, dir, "git", "branch", "--show-current")); branch != "main" {
		t.Errorf("the branch is %q, want main", branch)
	}
}

func TestWriteIfAbsentNeverTouchesAFileThatIsThere(t *testing.T) {
	birth := aBirth(t)
	path := filepath.Join(t.TempDir(), ".config", "mw", "config.toml")
	ctx := context.Background()

	if wrote, err := birth.WriteIfAbsent(ctx, path, "first\n"); err != nil || !wrote {
		t.Fatalf("with no file there: wrote %v, err %v", wrote, err)
	}
	if wrote, err := birth.WriteIfAbsent(ctx, path, "second\n"); err != nil || wrote {
		t.Fatalf("with a file there: wrote %v, err %v", wrote, err)
	}
	if got, _ := os.ReadFile(path); string(got) != "first\n" {
		t.Errorf("the file holds %q, want it as it was", got)
	}
}
