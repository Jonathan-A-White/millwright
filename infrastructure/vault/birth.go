package vault

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// Birth is the adapter behind application.VaultBirth: it lays a template into a
// directory, makes the directory a git repository and commits it, and writes a
// host's config file where there is none.
type Birth struct {
	author string
	env    []string
}

var _ application.VaultBirth = (*Birth)(nil)

// NewBirth returns a Birth whose first commit is made by author, as both name
// and email, but only for whichever of the two git knows nobody by: a person
// who has told git who they are makes the first commit as themselves. Any
// environment given is added to git's, after the process's own, which is how a
// test gives git a home of its own.
func NewBirth(author string, env ...string) *Birth {
	return &Birth{author: strings.TrimSpace(author), env: env}
}

// Vacant implements application.VaultBirth.
func (b *Birth) Vacant(_ context.Context, dir string) error {
	entries, err := os.ReadDir(dir)
	switch {
	case os.IsNotExist(err):
		return nil
	case err != nil:
		return fmt.Errorf("looking in %s: %w", dir, err)
	case len(entries) > 0:
		return fmt.Errorf("%s is not empty: mw init makes a vault only where there is nothing, and touches nothing that is there", dir)
	}
	return nil
}

// Lay implements application.VaultBirth.
func (b *Birth) Lay(_ context.Context, dir string, template fs.FS) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("making %s: %w", dir, err)
	}
	return fs.WalkDir(template, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(path))
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		content, err := fs.ReadFile(template, path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, content, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", target, err)
		}
		return nil
	})
}

// Commit implements application.VaultBirth.
func (b *Birth) Commit(ctx context.Context, dir, message string) error {
	if _, err := b.git(ctx, dir, nil, "init", "-q", "-b", "main"); err != nil {
		return err
	}
	_, hasCommit := b.git(ctx, dir, nil, "rev-parse", "--verify", "-q", "HEAD")
	if _, err := b.git(ctx, dir, nil, "add", "-A"); err != nil {
		return err
	}

	var who []string
	for _, key := range []string{"name", "email"} {
		if known, _ := b.git(ctx, dir, nil, "config", "user."+key); strings.TrimSpace(known) == "" && b.author != "" {
			who = append(who, "-c", "user."+key+"="+b.author)
		}
	}
	if hasCommit == nil {
		// Whatever was committed after the first commit (bd init commits its
		// files on some hosts) is put back in the index, to be folded in with
		// the rest.
		if err := b.backToRoot(ctx, dir); err != nil {
			return err
		}
		_, err := b.git(ctx, dir, who, "commit", "-q", "--amend", "--no-edit")
		return err
	}
	_, err := b.git(ctx, dir, who, "commit", "-q", "-m", message)
	return err
}

// backToRoot moves the branch back to its first commit, keeping every later
// commit's changes staged, when there is more than one commit.
func (b *Birth) backToRoot(ctx context.Context, dir string) error {
	count, err := b.git(ctx, dir, nil, "rev-list", "--count", "HEAD")
	if err != nil || strings.TrimSpace(count) == "1" {
		return err
	}
	roots, err := b.git(ctx, dir, nil, "rev-list", "--max-parents=0", "HEAD")
	if err != nil {
		return err
	}
	fields := strings.Fields(roots)
	if len(fields) != 1 {
		return fmt.Errorf("the history in %s has %d first commits, want 1", dir, len(fields))
	}
	_, err = b.git(ctx, dir, nil, "reset", "-q", "--soft", fields[0])
	return err
}

// Clone implements application.VaultBirth: it makes dir a git clone of url,
// the same clone a person's own `git clone` would make. Nothing here forces,
// migrates or rewrites what the clone brings with it — that is the vault's
// own history, from wherever it already lives.
func (b *Birth) Clone(ctx context.Context, url, dir string) error {
	cmd := exec.CommandContext(ctx, Git, "clone", "-q", url, dir)
	cmd.Env = append(append(os.Environ(), "GIT_TERMINAL_PROMPT=0"), b.env...)

	var out, errs bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errs
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s clone %s %s: %w: %s", Git, url, dir, err, strings.TrimSpace(errs.String()))
	}
	return nil
}

// RestoreBootstrapNewline implements application.VaultBirth. Telling a
// newline-only change from any other kind is a convenience, not a
// precondition of the join: anything that keeps it from being told — the
// file missing from HEAD, say — is treated the same as "changed some other
// way" and left alone, rather than failing a join whose clone and bootstrap
// both already succeeded. Only a checkout that was actually called for and
// then failed is reported as an error.
func (b *Birth) RestoreBootstrapNewline(ctx context.Context, dir string) (bool, error) {
	const path = ".beads/config.yaml"

	status, err := b.git(ctx, dir, nil, "status", "--porcelain", "--", path)
	if err != nil || strings.TrimSpace(status) == "" {
		return false, nil
	}

	committed, err := b.git(ctx, dir, nil, "show", "HEAD:"+path)
	if err != nil {
		return false, nil
	}
	working, err := os.ReadFile(filepath.Join(dir, path))
	if err != nil {
		return false, nil
	}
	if string(working) == committed || strings.TrimRight(string(working), "\n") != strings.TrimRight(committed, "\n") {
		// Either nothing changed, or it changed by more than the trailing
		// newline: left exactly as it is, for mw sync to report as today.
		return false, nil
	}

	if _, err := b.git(ctx, dir, nil, "checkout", "--", path); err != nil {
		return false, err
	}
	return true, nil
}

// WriteIfAbsent implements application.VaultBirth. The file is made with
// O_EXCL, so that a file that turns up between the look and the write is still
// not overwritten.
func (b *Birth) WriteIfAbsent(_ context.Context, path, text string) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("making the directory of %s: %w", path, err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if os.IsExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("writing %s: %w", path, err)
	}
	if _, err := file.WriteString(text); err != nil {
		file.Close()
		return false, fmt.Errorf("writing %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return false, fmt.Errorf("writing %s: %w", path, err)
	}
	return true, nil
}

// git runs one git command in dir, with any -c settings ahead of the command,
// and never prompts.
func (b *Birth) git(ctx context.Context, dir string, settings []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, Git, append(settings, args...)...)
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), "GIT_TERMINAL_PROMPT=0"), b.env...)

	var out, errs bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errs
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s in %s: %w: %s", Git, strings.Join(args, " "), dir, err, strings.TrimSpace(errs.String()))
	}
	return out.String(), nil
}
