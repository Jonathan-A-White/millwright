package rig

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// BackendWorktreePrefix names the throwaway worktree a backend is built in,
// beside the rig's checkout, as application.LandingPrefix names a landing's.
const BackendWorktreePrefix = "mw-backend-"

// Worktrees satisfies the port a rig's backend is read and built through. That
// port offers git and a build command and nothing else: no install, no restart,
// no live path (application.BackendBuilds).
var _ application.BackendBuilds = (*Worktrees)(nil)

// Changed implements application.BackendBuilds: whether what branch has beyond
// base — the merge base to branch, three dots — touches anything under subdir.
func (w *Worktrees) Changed(ctx context.Context, rigDir, base, branch, subdir string) (bool, error) {
	if !filepath.IsLocal(subdir) {
		return false, fmt.Errorf("the backend directory %q is not a directory inside the rig", subdir)
	}
	out, err := w.git(ctx, rigDir, "diff", "--name-only", base+"..."+branch, "--", subdir)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// Build implements application.BackendBuilds. The commit is fetched when this
// checkout does not have it yet (a landing made on the other host), cut into a
// detached worktree beside the rig, built there, and the worktree taken away
// again whatever the build did. The binary is written beside out and moved into
// place only once the command succeeded, so out is never half a binary.
func (w *Worktrees) Build(ctx context.Context, rigDir, commit, subdir, command, out string) error {
	switch {
	case commit == "" || out == "" || strings.TrimSpace(command) == "":
		return fmt.Errorf("building a backend needs a commit, a command and a place to leave it")
	case !filepath.IsLocal(subdir) && subdir != "":
		return fmt.Errorf("the backend directory %q is not a directory inside the rig", subdir)
	}
	if _, err := os.Stat(out); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return fmt.Errorf("making the directory %s is staged in: %w", filepath.Dir(out), err)
	}
	if _, err := w.git(ctx, rigDir, "cat-file", "-e", commit+"^{commit}"); err != nil {
		if err := w.Fetch(ctx, rigDir); err != nil {
			return fmt.Errorf("the commit %s is not in this checkout and fetching did not bring it: %w", commit, err)
		}
	}

	dir, err := os.MkdirTemp(filepath.Dir(filepath.Clean(rigDir)), BackendWorktreePrefix)
	if err != nil {
		return fmt.Errorf("making the directory the backend is built in: %w", err)
	}
	if err := os.Remove(dir); err != nil {
		return fmt.Errorf("making room for the build of %s: %w", rigDir, err)
	}
	if _, err := w.git(ctx, rigDir, "worktree", "add", "--detach", dir, commit); err != nil {
		return err
	}
	// Taken away even when the build outlived the context that asked for it.
	defer func() {
		_, _ = w.git(context.WithoutCancel(ctx), rigDir, "worktree", "remove", "--force", dir)
		_ = os.RemoveAll(dir)
	}()

	partial := out + ".building"
	_ = os.Remove(partial)
	limited, cancel := context.WithTimeout(ctx, application.AfterLandingLimit)
	defer cancel()
	built := strings.ReplaceAll(command, application.BackendOutPlaceholder, shellWord(partial))
	output, err := runLine(limited, Shell, built, filepath.Join(dir, subdir), 0)
	if err != nil {
		_ = os.Remove(partial)
		return fmt.Errorf("`%s` failed: %w: %s", command, err, application.RecentLines(output, 10))
	}
	if err := os.Rename(partial, out); err != nil {
		_ = os.Remove(partial)
		return fmt.Errorf("`%s` left no binary to stage at %s: %w", command, out, err)
	}
	return nil
}

// shellWord is s as one word of a shell command line.
func shellWord(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// ShipLimit bounds one copy of a staged binary to another host.
const ShipLimit = 5 * time.Minute

// BackendShip satisfies the port a staged binary is copied to another host
// through (application.BackendShip, mw-gq6.189). Reach is [hands_hosts]: how this
// host reaches each other host, an ssh prefix.
type BackendShip struct {
	Reach map[string]string
}

var _ application.BackendShip = BackendShip{}

// Ship implements application.BackendShip: src is streamed over the host's ssh
// prefix into a partial file beside dest, made executable and moved into place, so
// that dest is never half a binary. Nothing live is touched.
func (s BackendShip) Ship(ctx context.Context, host, src, dest string) error {
	prefix := strings.Fields(s.Reach[host])
	if len(prefix) == 0 {
		return fmt.Errorf("no [hands_hosts] entry for %s to copy the binary over", host)
	}
	file, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening the binary to copy: %w", err)
	}
	defer file.Close()

	partial := dest + ".partial"
	remote := fmt.Sprintf("mkdir -p %s && cat > %s && chmod 755 %s && mv -f %s %s",
		shellWord(filepath.Dir(dest)), shellWord(partial), shellWord(partial), shellWord(partial), shellWord(dest))
	ctx, cancel := context.WithTimeout(ctx, ShipLimit)
	defer cancel()
	cmd := exec.CommandContext(ctx, prefix[0], append(prefix[1:], remote)...)
	cmd.Stdin = file
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, application.RecentLines(string(out), 5))
	}
	return nil
}
