package rig

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// Worktrees lands a story's work as well as cutting the worktree it was done
// in: both are the rig's git, and they share the same command and the same
// remote.
var _ application.Landing = (*Worktrees)(nil)

// Ahead implements application.Landing: the commits branch has that base does
// not.
func (w *Worktrees) Ahead(ctx context.Context, rigDir, branch, base string) (int, error) {
	switch {
	case branch == "":
		return 0, fmt.Errorf("counting commits in %s: which branch?", rigDir)
	case base == "":
		return 0, fmt.Errorf("counting commits on %s in %s: ahead of what?", branch, rigDir)
	}
	said, err := w.git(ctx, rigDir, "rev-list", "--count", base+".."+branch)
	if err != nil {
		return 0, err
	}
	commits, err := strconv.Atoi(strings.TrimSpace(said))
	if err != nil {
		return 0, fmt.Errorf("counting the commits %s has and %s does not: %s said %q", branch, base, w.program, said)
	}
	return commits, nil
}

// Uncommitted implements application.Landing: what `git status` finds changed
// in the worktree at dir. -uall lists each untracked file, not the directory
// that holds it, so the paths are files a person can open; -z keeps a path with
// an odd character in it whole.
func (w *Worktrees) Uncommitted(ctx context.Context, dir string) ([]string, error) {
	if dir == "" {
		return nil, fmt.Errorf("reading uncommitted work: in which worktree?")
	}
	said, err := w.git(ctx, dir, "status", "--porcelain=v1", "-z", "-uall")
	if err != nil {
		return nil, err
	}

	var paths []string
	entries := strings.Split(said, "\x00")
	for i := 0; i < len(entries); i++ {
		entry := entries[i]
		if len(entry) < 4 {
			continue
		}
		paths = append(paths, entry[3:])
		// A rename or a copy is followed by the path it came from, as an entry
		// of its own with no status in front.
		if entry[0] == 'R' || entry[0] == 'C' || entry[1] == 'R' || entry[1] == 'C' {
			i++
		}
	}
	return paths, nil
}

// CommitLeftovers implements application.Landing.
func (w *Worktrees) CommitLeftovers(ctx context.Context, dir, message string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("committing leftovers: in which worktree?")
	}
	if strings.TrimSpace(message) == "" {
		return "", fmt.Errorf("committing leftovers in %s: a commit needs a message", dir)
	}
	if _, err := w.git(ctx, dir, "add", "-A"); err != nil {
		return "", err
	}
	if _, err := w.git(ctx, dir, "commit", "-q", "-m", message); err != nil {
		return "", err
	}
	sha, err := w.git(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(sha), nil
}

// Bundle implements application.Landing.
func (w *Worktrees) Bundle(ctx context.Context, rigDir, branch, base, dest string) (string, error) {
	switch {
	case branch == "":
		return "", fmt.Errorf("bundling %s: which branch?", rigDir)
	case base == "":
		return "", fmt.Errorf("bundling %s in %s: ahead of what?", branch, rigDir)
	case dest == "":
		return "", fmt.Errorf("bundling %s in %s: to where?", branch, rigDir)
	}
	sha, err := w.git(ctx, rigDir, "rev-parse", branch)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("making the directory %s belongs in: %w", dest, err)
	}
	if _, err := w.git(ctx, rigDir, "bundle", "create", dest, base+".."+branch); err != nil {
		return "", err
	}
	return strings.TrimSpace(sha), nil
}

// VerifyBundle implements application.Landing.
func (w *Worktrees) VerifyBundle(ctx context.Context, rigDir, dest string) error {
	if dest == "" {
		return fmt.Errorf("verifying a bundle: which file?")
	}
	_, err := w.git(ctx, rigDir, "bundle", "verify", dest)
	return err
}

// commitEnd terminates each commit git log prints here, and endDirective is how
// git is asked for it: a NUL cannot be passed in an argument, so git writes it
// itself. A NUL is the one byte a commit message cannot hold, so it is the one
// separator a message can never forge — and a message that carried the
// separator could hide everything after it, which is the whole thing this
// reading exists to catch.
const (
	commitEnd    = "\x00"
	endDirective = "%x00"
)

// Commits implements application.Landing: the commits branch has that base does
// not, oldest first — the order they would land in — each with its short hash
// and its whole message.
func (w *Worktrees) Commits(ctx context.Context, rigDir, branch, base string) ([]application.Commit, error) {
	switch {
	case branch == "":
		return nil, fmt.Errorf("reading the commits in %s: which branch?", rigDir)
	case base == "":
		return nil, fmt.Errorf("reading the commits on %s in %s: ahead of what?", branch, rigDir)
	}
	// %B is the whole message, subject and body, exactly as it was written.
	said, err := w.git(ctx, rigDir, "log", "--reverse", "--format=format:%h%n%B"+endDirective, base+".."+branch)
	if err != nil {
		return nil, err
	}

	var commits []application.Commit
	for _, record := range strings.Split(said, commitEnd) {
		// `format:` puts a newline between records, which lands at the head of
		// the next one.
		record = strings.TrimLeft(record, "\r\n")
		if strings.TrimSpace(record) == "" {
			continue
		}
		hash, message, _ := strings.Cut(record, "\n")
		commits = append(commits, application.Commit{
			Hash:    strings.TrimSpace(hash),
			Message: strings.TrimRight(message, "\n"),
		})
	}
	return commits, nil
}

// OpenLanding implements application.Landing. The landing is a worktree of its
// own, detached at the target branch as the remote has it, so that neither the
// rig's own checkout — which a person may be sitting in — nor the story's
// worktree is touched by the landing. Nothing is ever landed on a local branch:
// what is pushed is what was merged and tested, and nothing else.
func (w *Worktrees) OpenLanding(ctx context.Context, rigDir, base string) (string, error) {
	if base == "" {
		return "", fmt.Errorf("opening a landing of %s: landing on what?", rigDir)
	}
	dir, err := os.MkdirTemp(filepath.Dir(filepath.Clean(rigDir)), application.LandingPrefix)
	if err != nil {
		return "", fmt.Errorf("making the directory the landing of %s belongs in: %w", rigDir, err)
	}
	// git worktree add refuses a directory that is already there, and MkdirTemp
	// has just made one: it is taken away again so that the name stays reserved
	// by nobody else while git makes it.
	if err := os.Remove(dir); err != nil {
		return "", fmt.Errorf("making room for the landing of %s: %w", rigDir, err)
	}
	if _, err := w.git(ctx, rigDir, "worktree", "add", "--detach", dir, base); err != nil {
		return "", err
	}
	return dir, nil
}

// Merge implements application.Landing. A merge that conflicts is undone before
// the error comes back: mw resolves nothing for anybody, and a landing worktree
// left half-merged would be a trap for whoever looked at it next.
func (w *Worktrees) Merge(ctx context.Context, landingDir, branch string) (application.Landed, error) {
	if branch == "" {
		return application.Landed{}, fmt.Errorf("merging into %s: which branch?", landingDir)
	}
	before, err := w.git(ctx, landingDir, "rev-parse", "HEAD")
	if err != nil {
		return application.Landed{}, err
	}
	tip, err := w.git(ctx, landingDir, "rev-parse", branch)
	if err != nil {
		return application.Landed{}, err
	}

	// --no-edit so that no editor is ever opened: nobody is at the keyboard.
	if _, err := w.git(ctx, landingDir, "merge", "--no-edit", branch); err != nil {
		if _, abort := w.git(ctx, landingDir, "merge", "--abort"); abort != nil {
			return application.Landed{}, fmt.Errorf("%w: %v (and the half-made merge in %s could not be undone: %v)",
				application.ErrMergeConflict, err, landingDir, abort)
		}
		return application.Landed{}, fmt.Errorf("%w: %v", application.ErrMergeConflict, err)
	}

	after, err := w.git(ctx, landingDir, "rev-parse", "HEAD")
	if err != nil {
		return application.Landed{}, err
	}
	commit := strings.TrimSpace(after)
	return application.Landed{
		Commit: commit,
		// The target branch simply moved onto the story's work: what is being
		// pushed is exactly the commit the story's tests ran on.
		FastForward: commit == strings.TrimSpace(tip) && commit != strings.TrimSpace(before),
	}, nil
}

// Push implements application.Landing: what the landing worktree has checked
// out becomes the branch on the remote. The refspec has no leading plus and
// there is no --force anywhere: a remote that has moved on refuses this push,
// which is the point.
func (w *Worktrees) Push(ctx context.Context, landingDir, remote, branch string) error {
	if remote == "" {
		remote = w.remote
	}
	if branch == "" {
		return fmt.Errorf("pushing from %s: onto which branch?", landingDir)
	}
	if _, err := w.git(ctx, landingDir, "push", remote, "HEAD:refs/heads/"+branch); err != nil {
		switch {
		case rejected(err):
			return fmt.Errorf("%w: %v", application.ErrPushRejected, err)
		case transient(err):
			return fmt.Errorf("%w: %v", application.ErrPushTransient, err)
		}
		return err
	}
	return nil
}

// CloseLanding implements application.Landing.
func (w *Worktrees) CloseLanding(ctx context.Context, rigDir, landingDir string) error {
	if landingDir == "" {
		return nil
	}
	// The branch is empty: a landing is detached, and the branch it landed on
	// lives at the remote.
	return w.Remove(ctx, rigDir, landingDir, "")
}

// rejected reports whether git refused a push because the branch had moved on
// the remote, as against failing for any other reason. git says so in words;
// its exit status is 1 either way.
func rejected(err error) bool {
	said := strings.ToLower(err.Error())
	switch {
	case strings.Contains(said, "non-fast-forward"),
		strings.Contains(said, "fetch first"),
		strings.Contains(said, "! [rejected]"),
		strings.Contains(said, "[rejected]") && strings.Contains(said, "behind"):
		return true
	}
	return false
}

// refusedPushFaults are the words of a push failure that say the remote is
// refusing the push for a reason of its own that trying again will never
// change. Checked ahead of transientPushFaults, so that a hook which happens
// to print a transient-looking word of its own — the pre-receive hook's own
// wording is git's, not the rig's, and is never inside the rig's control —
// still stops the story at once rather than being retried into the ground.
var refusedPushFaults = []string{
	"refusing to update",
	"protected branch",
	"pre-receive hook declined",
	"permission denied",
	"permission to",
	"not authorized",
}

// transientPushFaults are the words of a push failure that say the fault was
// at the remote itself, not a reason it is refusing the push: GitHub's own
// known server-side hiccup (mw-gq6.86, "fatal error in commit_refs"), a
// connection that dropped mid-push, a name that would not resolve, and a
// server error answered over HTTP.
var transientPushFaults = []string{
	"fatal error in commit_refs",
	"connection reset",
	"connection timed out",
	"early eof",
	"unexpected disconnect",
	"the remote end hung up",
	"could not resolve host",
	"rpc failed",
	"http 502", "http/1.1 502",
	"http 503", "http/1.1 503",
	"http 504", "http/1.1 504",
}

// transient reports whether git failed a push on a fault at the remote worth
// trying again, as against a reason the remote states (refusedPushFaults, or
// rejected's non-fast-forward wording) that trying again will never change.
// A "[remote rejected] ... (failure)" with no stated reason at all — what
// GitHub says for its own server-side faults when nothing more specific
// comes back — counts too.
func transient(err error) bool {
	said := strings.ToLower(err.Error())
	for _, phrase := range refusedPushFaults {
		if strings.Contains(said, phrase) {
			return false
		}
	}
	for _, phrase := range transientPushFaults {
		if strings.Contains(said, phrase) {
			return true
		}
	}
	return strings.Contains(said, "[remote rejected]") && strings.Contains(said, "(failure)")
}

// Advance implements application.Landing. The checkout is left alone unless it
// is on branch and clean, and it is only ever fast-forwarded: `merge --ff-only`
// onto a commit the checkout is an ancestor of cannot lose anybody's work, and
// what is not an ancestor is said, not merged. Untracked files count as work
// here, because a person's notes in the checkout are theirs as much as an edit.
func (w *Worktrees) Advance(ctx context.Context, rigDir, branch, commit string) (application.Advanced, error) {
	switch {
	case branch == "":
		return application.Advanced{}, fmt.Errorf("advancing the checkout of %s: onto which branch?", rigDir)
	case commit == "":
		return application.Advanced{}, fmt.Errorf("advancing the checkout of %s: to which commit?", rigDir)
	}

	// Already there, on any branch: nothing to move and nothing to say.
	if level, err := w.contains(ctx, rigDir, "HEAD", commit); err != nil {
		return application.Advanced{}, err
	} else if level {
		return application.Advanced{}, nil
	}

	// "HEAD" is what git says when no branch is checked out.
	on, err := w.git(ctx, rigDir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return application.Advanced{}, err
	}
	switch on = strings.TrimSpace(on); {
	case on == "HEAD":
		return application.Advanced{Left: fmt.Sprintf("it is on no branch, not %s", branch)}, nil
	case on != branch:
		return application.Advanced{Left: fmt.Sprintf("it is on %s, not %s", on, branch)}, nil
	}

	dirty, err := w.Uncommitted(ctx, rigDir)
	if err != nil {
		return application.Advanced{}, err
	}
	if len(dirty) > 0 {
		return application.Advanced{Left: fmt.Sprintf("it has %d uncommitted path(s): %s", len(dirty), strings.Join(dirty, ", "))}, nil
	}

	behind, err := w.contains(ctx, rigDir, commit, "HEAD")
	if err != nil {
		return application.Advanced{}, err
	}
	if !behind {
		return application.Advanced{Left: fmt.Sprintf("%s here has commits of its own that %s does not", branch, shortCommit(commit))}, nil
	}
	if _, err := w.git(ctx, rigDir, "merge", "--ff-only", "--quiet", commit); err != nil {
		return application.Advanced{}, err
	}
	return application.Advanced{Moved: true}, nil
}

// contains reports whether the commit that outer names has the one inner names
// in its history, itself included. `merge-base --is-ancestor` answers in its
// exit status: 0 yes, 1 no, anything else a failure.
func (w *Worktrees) contains(ctx context.Context, dir, outer, inner string) (bool, error) {
	_, err := w.git(ctx, dir, "merge-base", "--is-ancestor", inner, outer)
	var exit *exec.ExitError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		return false, nil
	}
	return false, err
}

// shortCommit is a commit as a person names it, in a sentence.
func shortCommit(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}
