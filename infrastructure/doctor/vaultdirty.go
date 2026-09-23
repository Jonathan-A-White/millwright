package doctor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*VaultDirty)(nil)

// VaultDirtyName is what the check is called: in the log, and on the command
// line as `mw doctor vault-dirty`.
const VaultDirtyName = "vault-dirty"

// vaultDirtyRunsPrefix is the one directory this check ever commits in: a
// story's own run files, the ones a re-dispatch can leave modified without
// anyone meaning to (a truncated result.json, a rewritten boot.md). A
// modified path anywhere else in the vault — seats/, plans/, .beads — is
// never this check's to commit.
const vaultDirtyRunsPrefix = "runs/"

// The vault-dirty check's damper: idle between two commits, and how many it
// spends on one fault episode before it gives up and waits for the
// escalation note (a later story) to wake the Millhand.
const (
	VaultDirtyDamperWait = 5 * time.Minute
	VaultDirtyDamperCap  = 5
)

// VaultDirty is the check that keeps a re-dispatched story's own leftovers
// from blocking every later `mw sync` and `mw dispatch` on this host: when a
// tracked file under runs/<story>/ is modified but not committed — the
// vault's own history shows this happens when a story is re-dispatched and
// truncates its result.json or rewrites its boot.md — it commits that path
// exactly as found, one commit per path, and pushes nothing: the next sync
// does that. A modified tracked file outside runs/ is cannot-tell, never
// cured here.
type VaultDirty struct {
	// Dir is the vault's directory.
	Dir string
	// Host names this host, written into every cure's commit message.
	Host string
	// Program is the program run for git. Empty reads "git".
	Program string

	// cured is what the last Cure committed, path to commit hash, read by
	// WayBack once Cure has run. Empty before any cure runs in this process.
	cured []vaultDirtyCommit
}

// vaultDirtyCommit is one path this check has committed and the commit that
// holds it.
type vaultDirtyCommit struct {
	path string
	hash string
}

// NewVaultDirty is the check over the vault at dir, its cures attributed to
// host, run through the real git.
func NewVaultDirty(dir, host string) *VaultDirty {
	return &VaultDirty{Dir: dir, Host: host}
}

// Name implements application.DoctorCheck.
func (v *VaultDirty) Name() string { return VaultDirtyName }

// Probe implements application.DoctorCheck: `git -C <vault> status
// --porcelain --untracked-files=no`. ok when nothing is changed; faulty,
// naming every changed path, when every one of them is under runs/;
// cannot-tell, naming only the paths outside runs/, when any changed path is
// not under runs/ — never this check's to commit, whatever else changed
// alongside it.
func (v *VaultDirty) Probe(ctx context.Context) (application.Verdict, string) {
	out, err := v.git(ctx, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("git status: %v", err)
	}
	paths := vaultDirtyChangedPaths(out)
	if len(paths) == 0 {
		return application.DoctorOK, ""
	}

	var outside []string
	for _, path := range paths {
		if !strings.HasPrefix(path, vaultDirtyRunsPrefix) {
			outside = append(outside, path)
		}
	}
	if len(outside) > 0 {
		return application.DoctorCannotTell, "modified tracked files outside runs/: " + strings.Join(outside, ", ")
	}

	return application.DoctorFaulty, fmt.Sprintf("%d modified tracked files: %s", len(paths), strings.Join(paths, ", "))
}

// Cure implements application.DoctorCheck: one `git -C <vault> commit -q -m
// '...' -- <path>` per modified path under runs/, never `git add -A` and
// never a push — the next sync pushes. Each commit's message names the path,
// the time the file was last modified, and this host, so the log and the way
// back both read "committed as found" rather than claiming anyone reviewed
// it.
func (v *VaultDirty) Cure(ctx context.Context) error {
	out, err := v.git(ctx, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}

	var cured []vaultDirtyCommit
	for _, path := range vaultDirtyChangedPaths(out) {
		if !strings.HasPrefix(path, vaultDirtyRunsPrefix) {
			continue
		}
		mtime, err := v.mtime(path)
		if err != nil {
			return fmt.Errorf("stating %s: %w", path, err)
		}
		message := fmt.Sprintf("doctor: %s committed as found (modified since %s, host %s)",
			path, mtime.UTC().Format(time.RFC3339), v.Host)
		if _, err := v.git(ctx, "commit", "-q", "-m", message, "--", path); err != nil {
			return fmt.Errorf("committing %s: %w", path, err)
		}
		hash, err := v.git(ctx, "rev-parse", "HEAD")
		if err != nil {
			return fmt.Errorf("reading the commit made for %s: %w", path, err)
		}
		cured = append(cured, vaultDirtyCommit{path: path, hash: strings.TrimSpace(hash)})
	}

	v.cured = cured
	return nil
}

// Damper implements application.DoctorCheck.
func (v *VaultDirty) Damper() (time.Duration, int) { return VaultDirtyDamperWait, VaultDirtyDamperCap }

// WayBack implements application.DoctorCheck: one `git -C <vault> revert
// <commit> -- <path>` per path the last Cure committed, so the log line
// beside a cure names the exact commit a person undoes it with. Before any
// cure has run in this process — a damped or dry-run report, which never
// runs Cure — there is no commit yet to name.
func (v *VaultDirty) WayBack() string {
	if len(v.cured) == 0 {
		return "none committed yet: nothing to revert"
	}
	backs := make([]string, len(v.cured))
	for i, c := range v.cured {
		backs[i] = fmt.Sprintf("git -C %s revert %s -- %s", v.Dir, c.hash, c.path)
	}
	return strings.Join(backs, "; ")
}

// mtime is when path, relative to the vault, was last modified.
func (v *VaultDirty) mtime(path string) (time.Time, error) {
	info, err := os.Stat(filepath.Join(v.Dir, path))
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

// git runs one git command against the vault and returns its standard
// output. It never prompts: doctoring may run on a timer with nobody at the
// keyboard.
func (v *VaultDirty) git(ctx context.Context, args ...string) (string, error) {
	program := v.Program
	if program == "" {
		program = "git"
	}
	full := append([]string{"-C", v.Dir}, args...)
	cmd := exec.CommandContext(ctx, program, full...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	var out, errs bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errs
	if err := cmd.Run(); err != nil {
		said := strings.TrimSpace(errs.String() + "\n" + out.String())
		return "", fmt.Errorf("%s %s: %w: %s", program, strings.Join(full, " "), err, said)
	}
	return out.String(), nil
}

// vaultDirtyChangedPaths reads the paths out of `git status --porcelain`
// output, the same way infrastructure/vault/git.go's own changedPaths does:
// two status characters, a space, then the path, and for a rename "old ->
// new", where the new name is the one that matters here.
func vaultDirtyChangedPaths(out string) []string {
	var changed []string
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if _, to, renamed := strings.Cut(path, " -> "); renamed {
			path = to
		}
		changed = append(changed, strings.Trim(path, `"`))
	}
	return changed
}
