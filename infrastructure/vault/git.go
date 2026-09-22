package vault

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/netfault"
)

// Git is the command this adapter keeps the vault level with, and
// AttributesFile is where git is told how to merge a ledger.
const (
	Git            = "git"
	AttributesFile = ".gitattributes"
)

// marking is the comment written above the ledger mark when the vault has no
// .gitattributes yet, so that whoever reads it next knows why it is there.
const marking = "# Ledgers are appended to by both hosts and edited by neither,\n" +
	"# so two hosts' appends merge by keeping every line rather than conflicting.\n"

// The vault's files are kept level with the other host's by plain git: the
// other host's commits are pulled and this host's are replayed on top, then
// what this host has and the other one does not is pushed. Nothing is forced,
// nothing is committed on a seat's behalf, and a rebase that stops on a
// conflict is undone before the reason comes back.
var _ application.VaultFiles = (*Vault)(nil)

// MarkLedgers implements application.VaultFiles: it makes sure the vault's
// .gitattributes carries application.LedgerMark, and reports whether it had to
// write it.
//
// The mark matters before a pull, not after: git reads it out of the working
// tree while it merges, so a vault marked here is safe to rebase immediately —
// even though the mark is not committed yet. Committing it is a seat's job, and
// until someone does, the other host is still unmarked.
func (v *Vault) MarkLedgers(_ context.Context) (bool, error) {
	path := filepath.Join(v.dir, AttributesFile)
	attributes, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("reading %s: %w", path, err)
	}
	if marked(string(attributes)) {
		return false, nil
	}

	var write strings.Builder
	if len(attributes) > 0 {
		write.Write(attributes)
		if !bytes.HasSuffix(attributes, []byte("\n")) {
			write.WriteString("\n")
		}
		write.WriteString("\n")
	}
	write.WriteString(marking)
	write.WriteString(application.LedgerMark + "\n")

	if err := os.WriteFile(path, []byte(write.String()), 0o644); err != nil {
		return false, fmt.Errorf("writing %s: %w", path, err)
	}
	return true, nil
}

// marked reports whether a .gitattributes already merges the vault's ledgers by
// keeping every line. Comments and the order of the attributes after the
// pattern are none of this adapter's business: what matters is that the ledger
// pattern carries merge=union.
func marked(attributes string) bool {
	for _, line := range strings.Split(attributes, "\n") {
		if comment := strings.Index(line, "#"); comment >= 0 {
			line = line[:comment]
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != application.LedgerPattern {
			continue
		}
		for _, attribute := range fields[1:] {
			if attribute == application.MergeUnion {
				return true
			}
		}
	}
	return false
}

// Uncommitted implements application.VaultFiles. Only tracked files are
// listed: a run directory or a draft nobody has added is in nobody's way, but a
// changed ledger stops a rebase dead.
func (v *Vault) Uncommitted(ctx context.Context) ([]string, error) {
	out, err := v.git(ctx, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return nil, err
	}

	return changedPaths(out), nil
}

// changedPaths reads the paths out of git status --porcelain.
func changedPaths(out string) []string {
	var changed []string
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if len(line) < 4 {
			continue
		}
		// Porcelain v1: two status characters, a space, then the path, and for
		// a rename "old -> new". The new name is the one to commit.
		path := strings.TrimSpace(line[3:])
		if _, to, renamed := strings.Cut(path, " -> "); renamed {
			path = to
		}
		changed = append(changed, strings.Trim(path, `"`))
	}
	return changed
}

// Commit implements application.VaultFiles: it records exactly the paths it is
// given, and nothing else the vault holds.
//
// Three git commands, in this order: `status --porcelain --untracked-files=all
// -- <paths>` to see which of them there is anything to commit in, `add --
// <those>` so that a file git has never seen is committable, and `commit -m
// <message> -- <those>`. The pathspec on the commit is what makes this safe in
// a vault two hosts and several seats write to: a commit with a pathspec takes
// the working tree's version of those paths and nothing else, whatever else is
// changed or even staged. Nothing is added with -A, nothing is committed with
// -a, and a path nobody touched is not committed at all. When the Vault has an
// author (WithAuthor) the commit alone carries it, as -c user.name and -c
// user.email.
func (v *Vault) Commit(ctx context.Context, message string, paths []string) ([]string, error) {
	if strings.TrimSpace(message) == "" {
		return nil, fmt.Errorf("committing in %s: a commit needs a message", v.dir)
	}
	for _, path := range paths {
		if err := insideTheVault(path); err != nil {
			return nil, fmt.Errorf("committing in %s: %w", v.dir, err)
		}
	}
	if len(paths) == 0 {
		return nil, nil
	}

	out, err := v.git(ctx, append([]string{"status", "--porcelain", "--untracked-files=all", "--"}, paths...)...)
	if err != nil {
		return nil, err
	}
	changed := changedPaths(out)
	if len(changed) == 0 {
		return nil, nil
	}

	if _, err := v.git(ctx, append([]string{"add", "--"}, changed...)...); err != nil {
		return nil, err
	}
	commit := append([]string{"commit", "-m", message, "--"}, changed...)
	if v.author != "" {
		commit = append([]string{"-c", "user.name=" + v.author, "-c", "user.email=" + v.author}, commit...)
	}
	if _, err := v.git(ctx, commit...); err != nil {
		return nil, err
	}
	return changed, nil
}

// insideTheVault refuses a path that names something the vault does not hold.
// Every path a commit is given comes from a seat name and a rig name, and both
// come from a bead or a config file rather than from this package.
func insideTheVault(path string) error {
	switch {
	case strings.TrimSpace(path) == "":
		return fmt.Errorf("a path to commit cannot be empty")
	case filepath.IsAbs(path), strings.HasPrefix(path, "/"):
		return fmt.Errorf("%q is not a path in the vault: it is absolute", path)
	case strings.HasPrefix(path, "-"):
		return fmt.Errorf("%q is not a path in the vault: it reads as an option", path)
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return fmt.Errorf("%q is not a path in the vault: it reaches outside it", path)
		}
	}
	return nil
}

// Pull implements application.VaultFiles: git pull --rebase, and the count of
// the other host's commits that came in. A rebase that stops on a conflict is
// aborted, so that the vault is left exactly as it was found and a person can
// look at it without first undoing a half-finished merge.
func (v *Vault) Pull(ctx context.Context) (int, error) {
	before, err := v.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return 0, err
	}
	if _, err := v.upstream(ctx); err != nil {
		return 0, err
	}

	if _, err := v.git(ctx, "pull", "--rebase"); err != nil {
		if _, aborted := v.git(ctx, "rebase", "--abort"); aborted == nil {
			return 0, fmt.Errorf("%w (the rebase was undone: the vault is as it was)", err)
		}
		return 0, err
	}
	return v.commitsBetween(ctx, strings.TrimSpace(before), "@{upstream}")
}

// Push implements application.VaultFiles. A clone with nothing to push does not
// reach the remote at all, so a sync with nothing to do costs one fetch and no
// network write.
func (v *Vault) Push(ctx context.Context) (int, error) {
	outgoing, err := v.commitsBetween(ctx, "@{upstream}", "HEAD")
	if err != nil {
		return 0, err
	}
	if outgoing == 0 {
		return 0, nil
	}
	if _, err := v.git(ctx, "push"); err != nil {
		return 0, err
	}
	return outgoing, nil
}

// Head implements application.VaultFiles: the commit this clone has checked
// out right now.
func (v *Vault) Head(ctx context.Context) (string, error) {
	out, err := v.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// upstream is the branch this clone tracks, and the reason there is none: a
// vault whose branch tracks nothing has no other host to be level with.
func (v *Vault) upstream(ctx context.Context) (string, error) {
	out, err := v.git(ctx, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		return "", fmt.Errorf("the vault's branch tracks no remote branch, so there is nothing to sync with: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// commitsBetween counts the commits reachable from to and not from from.
func (v *Vault) commitsBetween(ctx context.Context, from, to string) (int, error) {
	out, err := v.git(ctx, "rev-list", "--count", from+".."+to)
	if err != nil {
		return 0, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("counting the commits in %s..%s: %w", from, to, err)
	}
	return count, nil
}

// git runs one git command in the vault and returns its standard output. It
// never prompts: a sync may run on a timer with nobody at the keyboard, and a
// command waiting for a password would hang there until someone noticed.
func (v *Vault) git(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, Git, args...)
	cmd.Dir = v.dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")

	var out, errs bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errs

	if err := cmd.Run(); err != nil {
		said := strings.TrimSpace(errs.String() + "\n" + out.String())
		if said == "" {
			return "", fmt.Errorf("%s %s in %s: %w", Git, strings.Join(args, " "), v.dir, err)
		}
		failed := fmt.Errorf("%s %s in %s: %w: %s", Git, strings.Join(args, " "), v.dir, err, said)
		if line, unresolved := netfault.NameNotResolved(said); unresolved {
			return "", &application.NameNotResolved{Said: line, Err: failed}
		}
		return "", failed
	}
	return out.String(), nil
}
