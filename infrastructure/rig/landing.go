package rig

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/gitcmd"
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

// MergedInto implements application.Landing. A branch that is not there is not
// merged. The branch's reflog is what says it has work of its own: a branch cut
// and never committed to sits at the commit it was cut from, which every target
// branch contains, so ancestry alone would call it merged. A reflog that is gone
// says nothing, and is taken as not merged.
func (w *Worktrees) MergedInto(ctx context.Context, rigDir, branch, base string) (string, bool, error) {
	switch {
	case branch == "":
		return "", false, fmt.Errorf("asking whether a branch is merged in %s: which branch?", rigDir)
	case base == "":
		return "", false, fmt.Errorf("asking whether %s is merged in %s: into what?", branch, rigDir)
	}
	said, err := w.git(ctx, rigDir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	if err != nil {
		return "", false, nil
	}
	tip := strings.TrimSpace(said)

	if _, err := w.git(ctx, rigDir, "merge-base", "--is-ancestor", tip, base); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return tip, false, nil
		}
		return "", false, err
	}

	reflog, err := w.git(ctx, rigDir, "reflog", "show", "--format=%H", "refs/heads/"+branch)
	if err != nil {
		return tip, false, nil
	}
	tips := strings.Fields(reflog)
	if len(tips) < 2 || tips[len(tips)-1] == tip {
		return tip, false, nil
	}
	return tip, true, nil
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
// its whole message, and the time it was committed.
func (w *Worktrees) Commits(ctx context.Context, rigDir, branch, base string) ([]application.Commit, error) {
	switch {
	case branch == "":
		return nil, fmt.Errorf("reading the commits in %s: which branch?", rigDir)
	case base == "":
		return nil, fmt.Errorf("reading the commits on %s in %s: ahead of what?", branch, rigDir)
	}
	// %B is the whole message, subject and body, exactly as it was written.
	said, err := w.git(ctx, rigDir, "log", "--reverse", "--format=format:%h %ct%n%B"+endDirective, base+".."+branch)
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
		head, message, _ := strings.Cut(record, "\n")
		hash, stamp, _ := strings.Cut(strings.TrimSpace(head), " ")
		commit := application.Commit{Hash: hash, Message: strings.TrimRight(message, "\n")}
		if seconds, err := strconv.ParseInt(stamp, 10, 64); err == nil {
			commit.Committed = time.Unix(seconds, 0)
		}
		commits = append(commits, commit)
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
	if err := w.UnionNotes(ctx, dir); err != nil {
		_ = w.Remove(ctx, rigDir, dir, "")
		return "", err
	}
	return dir, nil
}

// RefHead implements application.Landing.
func (w *Worktrees) RefHead(ctx context.Context, rigDir, ref string) (string, error) {
	if ref == "" {
		return "", fmt.Errorf("reading the head of %s: which ref?", rigDir)
	}
	said, err := w.git(ctx, rigDir, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(said), nil
}

// UnionNotes implements application.Landing.
//
// The rule goes in info/attributes, which git keeps once for a repository and
// all of its worktrees, so it is never a tracked file of the rig's. It is
// written only when no line of the file already says it, and takes the place of
// the union rule an older mw wrote (mw-gq6.499).
func (w *Worktrees) UnionNotes(ctx context.Context, dir string) error {
	if dir == "" {
		return fmt.Errorf("setting up the trim-safe merge of CLAUDE.md: in which worktree?")
	}
	said, err := w.git(ctx, dir, "rev-parse", "--git-path", "info/attributes")
	if err != nil {
		return err
	}
	path := strings.TrimSpace(said)
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	kept, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	text := string(kept)
	var lines []string
	if text != "" {
		lines = strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	}
	var rewritten []string
	ruled := false
	for _, line := range lines {
		if said := strings.Join(strings.Fields(line), " "); said == notesRule || said == unionRule {
			if !ruled {
				rewritten = append(rewritten, notesRule)
				ruled = true
			}
			continue
		}
		rewritten = append(rewritten, line)
	}
	if !ruled {
		rewritten = append(rewritten, notesRule)
	}
	next := strings.Join(rewritten, "\n") + "\n"
	if next == text {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("making the directory of %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// notesRule is the line UnionNotes keeps in info/attributes: two stories that
// each append a note to the rig's CLAUDE.md both keep their lines, and one that
// trims it keeps its trim. unionRule is the line it takes the place of: union
// brought back every line a trim took out whenever the other side appended.
const (
	notesRule = "CLAUDE.md merge=" + gitcmd.TrimMergeDriver
	unionRule = "CLAUDE.md merge=union"
)

// Merge implements application.Landing. A merge that conflicts is undone before
// the error comes back: mw resolves nothing for anybody, and a landing worktree
// left half-merged would be a trap for whoever looked at it next.
func (w *Worktrees) Merge(ctx context.Context, landingDir, branch string) (application.Landed, error) {
	return w.MergeUnion(ctx, landingDir, branch, nil)
}

// MergeUnion implements application.Landing. Only when every file the merge
// conflicts in is one union lists (a path relative to the rig, or a path.Match
// glob) is any of them resolved: each by mw's trim-safe driver run on its three
// versions, so that the attributes of the rig, which are shared by all its
// worktrees, are never touched. A conflict in one file more is the whole merge
// undone, as in Merge. Never by git merge-file --union: it kept both sides of
// every hunk, so a trim got back what it took out (mw-gq6.499).
func (w *Worktrees) MergeUnion(ctx context.Context, landingDir, branch string, union []string) (application.Landed, error) {
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

	driver, err := gitcmd.WriteMergeDriver(w.mergeScript)
	if err != nil {
		return application.Landed{}, err
	}
	defer driver.Remove()

	var unioned []string
	// --no-edit so that no editor is ever opened: nobody is at the keyboard.
	merge := append(driver.Config(), "merge", "--no-edit", branch)
	if _, err := w.git(ctx, landingDir, merge...); err != nil {
		// Read before the union rules touch the index and before the merge is
		// undone: afterwards the paths are gone (mw-gq6.559).
		conflicted := w.unmergedPaths(ctx, landingDir)
		unioned, err = w.unionConflicts(ctx, landingDir, driver, union, err)
		if err != nil {
			said := fmt.Sprintf("%v: %v", application.ErrMergeConflict, err)
			if _, abort := w.git(ctx, landingDir, "merge", "--abort"); abort != nil {
				said = fmt.Sprintf("%v: %v (and the half-made merge in %s could not be undone: %v)",
					application.ErrMergeConflict, err, landingDir, abort)
			}
			return application.Landed{}, &application.MergeConflict{Files: conflicted, Said: said}
		}
	}

	after, err := w.git(ctx, landingDir, "rev-parse", "HEAD")
	if err != nil {
		return application.Landed{}, err
	}
	commit := strings.TrimSpace(after)
	return application.Landed{
		Commit: commit,
		Before: strings.TrimSpace(before),
		// The target branch simply moved onto the story's work: what is being
		// pushed is exactly the commit the story's tests ran on.
		FastForward: commit == strings.TrimSpace(tip) && commit != strings.TrimSpace(before),
		Unioned:     unioned,
	}, nil
}

// unmergedPaths are the paths a merge in progress in dir left unmerged, git's
// order; none when git cannot say.
func (w *Worktrees) unmergedPaths(ctx context.Context, dir string) []string {
	said, err := w.git(ctx, dir, "diff", "--name-only", "--diff-filter=U", "-z")
	if err != nil {
		return nil
	}
	var files []string
	for _, file := range strings.Split(said, "\x00") {
		if file != "" {
			files = append(files, file)
		}
	}
	return files
}

// unionConflicts settles a merge that has stopped on conflicts, if all of them
// are in files union lists, by driver, and commits it; it reports the files it
// settled. When it cannot, it returns an error saying why, with the merge left
// as it stopped for the caller to undo; stopped is git's own refusal, which it
// returns when no file is conflicted at all (a merge that failed for another
// reason).
func (w *Worktrees) unionConflicts(ctx context.Context, dir string, driver *gitcmd.MergeDriver, union []string, stopped error) ([]string, error) {
	said, err := w.git(ctx, dir, "diff", "--name-only", "--diff-filter=U", "-z")
	if err != nil || len(union) == 0 {
		return nil, stopped
	}
	var files []string
	for _, file := range strings.Split(said, "\x00") {
		if file != "" {
			files = append(files, file)
		}
	}
	if len(files) == 0 {
		return nil, stopped
	}
	for _, file := range files {
		// git's merge already ran the driver on a file named with it, and the
		// driver could not settle it: running it again would say the same.
		if attr, err := w.git(ctx, dir, "check-attr", "merge", "--", file); err == nil {
			if _, value, _ := strings.Cut(strings.TrimSpace(attr), ": merge: "); gitcmd.IsTrimMerge(value) {
				return nil, fmt.Errorf("%w (%s is merged by mw's trim-safe driver, merge=%s, which could not settle it; union would bring back every line a trim took out)",
					stopped, file, value)
			}
		}
		if !listed(union, file) {
			return nil, fmt.Errorf("%w (%s is in conflict and the rig does not list it to be merged by union)", stopped, file)
		}
	}
	scratch, err := os.MkdirTemp("", "mw-union-")
	if err != nil {
		return nil, fmt.Errorf("%w (and a place to merge %s could not be made: %v)", stopped, strings.Join(files, ", "), err)
	}
	defer os.RemoveAll(scratch)
	merged := make(map[string]string, len(files))
	for _, file := range files {
		text, err := w.trimMergeOf(ctx, dir, scratch, driver, file)
		if err != nil {
			return nil, fmt.Errorf("%w (%s is listed in union_merge_files, but could not be merged: %v)", stopped, file, err)
		}
		merged[file] = text
	}
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(file)), []byte(merged[file]), 0o644); err != nil {
			return nil, fmt.Errorf("%w (writing %s: %v)", stopped, file, err)
		}
		if _, err := w.git(ctx, dir, "add", "--", file); err != nil {
			return nil, fmt.Errorf("%w (%v)", stopped, err)
		}
	}
	if _, err := w.git(ctx, dir, "commit", "--no-edit"); err != nil {
		return nil, fmt.Errorf("%w (%v)", stopped, err)
	}
	return files, nil
}

// trimMergeOf is the file as driver merges it of the two sides of a conflict
// and their common ancestor, which is empty when both sides added the file. A
// side that deleted it is no merge.
func (w *Worktrees) trimMergeOf(ctx context.Context, dir, scratch string, driver *gitcmd.MergeDriver, file string) (string, error) {
	var sides [3]string
	for i, stage := range []string{"1", "2", "3"} { // the base, ours, theirs
		text, err := w.git(ctx, dir, "show", ":"+stage+":"+file)
		if err != nil && stage != "1" {
			return "", fmt.Errorf("one side deleted it")
		}
		sides[i] = filepath.Join(scratch, stage)
		if err := os.WriteFile(sides[i], []byte(text), 0o644); err != nil {
			return "", err
		}
	}
	if err := driver.Merge(ctx, dir, sides[0], sides[1], sides[2]); err != nil {
		return "", fmt.Errorf("mw's trim-safe driver, merge=%s, could not settle it (%v); union would bring back every line a trim took out",
			gitcmd.TrimMergeDriver, err)
	}
	merged, err := os.ReadFile(sides[1])
	return string(merged), err
}

// listed reports whether file is one of the paths or globs union lists.
func listed(union []string, file string) bool {
	for _, pattern := range union {
		if pattern == file {
			return true
		}
		if ok, err := path.Match(pattern, file); err == nil && ok {
			return true
		}
	}
	return false
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
		default:
			if why, passing := transientWhy(err); passing {
				return &application.TransientFault{Why: why, Err: fmt.Errorf("%w: %v", application.ErrPushTransient, err)}
			}
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
// server error answered over HTTP. A fetch is read by the same words
// (mw-gq6.373).
var transientPushFaults = []string{
	"fatal error in commit_refs",
	"connection reset",
	"connection timed out",
	"early eof",
	"unexpected disconnect",
	"the remote end hung up",
	"could not resolve host",
	"temporary failure in name resolution",
	"couldn't connect to server",
	"failed to connect to",
	"rpc failed",
	"http 502", "http/1.1 502", "returned error: 502",
	"http 503", "http/1.1 503", "returned error: 503",
	"http 504", "http/1.1 504", "returned error: 504",
}

// transientWhy reports whether git failed on a fault at the remote or the
// network worth trying again, as against a reason the remote states
// (refusedPushFaults, or rejected's non-fast-forward wording) that trying again
// will never change, and names the fault in a few words for a note. A
// "[remote rejected] ... (failure)" with no stated reason at all — what GitHub
// says for its own server-side faults when nothing more specific comes back —
// counts too.
func transientWhy(err error) (string, bool) {
	said := strings.ToLower(err.Error())
	for _, phrase := range refusedPushFaults {
		if strings.Contains(said, phrase) {
			return "", false
		}
	}
	for _, phrase := range transientPushFaults {
		if strings.Contains(said, phrase) {
			return phrase, true
		}
	}
	if strings.Contains(said, "[remote rejected]") && strings.Contains(said, "(failure)") {
		return "remote rejected (failure)", true
	}
	return "", false
}

// Advance implements application.Landing. A checkout that already holds the
// commit is Level, not Moved. The checkout is left alone unless it
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

	// Already there: nothing to move. It is Level, and a place to build from only
	// on the target branch and clean; on any other branch it is level with the
	// commit but no place to build the target from, and Held says so.
	level, err := w.contains(ctx, rigDir, "HEAD", commit)
	if err != nil {
		return application.Advanced{}, err
	}
	if level {
		on, err := w.git(ctx, rigDir, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return application.Advanced{}, err
		}
		switch on = strings.TrimSpace(on); {
		case on == "HEAD":
			return application.Advanced{Level: true, Held: fmt.Sprintf("it is on no branch, not %s", branch)}, nil
		case on != branch:
			return application.Advanced{Level: true, Held: fmt.Sprintf("it is on %s, not %s", on, branch)}, nil
		}
		dirty, err := w.Uncommitted(ctx, rigDir)
		if err != nil {
			return application.Advanced{}, err
		}
		if len(dirty) > 0 {
			return application.Advanced{Level: true, Held: describeUncommitted(dirty)}, nil
		}
		return application.Advanced{Level: true}, nil
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
		return application.Advanced{Left: describeUncommitted(dirty)}, nil
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

// namedUncommitted is how many uncommitted paths a report names before it
// settles for a count of the rest: a checkout with hundreds of stray files
// must not put them all in a mail.
const namedUncommitted = 10

// describeUncommitted says what a checkout with uncommitted work has: the
// count, the top-level directory every path is under when they share one, and
// the first few paths with "and N more" for the rest.
func describeUncommitted(dirty []string) string {
	named := dirty
	more := ""
	if len(dirty) > namedUncommitted {
		named = dirty[:namedUncommitted]
		more = fmt.Sprintf(", and %d more", len(dirty)-namedUncommitted)
	}
	under := ""
	if top, ok := sharedTopDir(dirty); ok {
		under = fmt.Sprintf(", all under %s", top)
	}
	return fmt.Sprintf("it has %d uncommitted path(s)%s: %s%s", len(dirty), under, strings.Join(named, ", "), more)
}

// sharedTopDir returns the top-level directory (with its trailing slash) that
// every path is inside, or false when a path is at the top or they differ.
func sharedTopDir(paths []string) (string, bool) {
	top := ""
	for _, path := range paths {
		i := strings.Index(path, "/")
		if i <= 0 {
			return "", false
		}
		if dir := path[:i+1]; top == "" {
			top = dir
		} else if dir != top {
			return "", false
		}
	}
	return top, top != ""
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

// Tip implements application.FactoryCheckout: the commit branch is at on the
// remote, as the last fetch saw it.
func (w *Worktrees) Tip(ctx context.Context, rigDir, remote, branch string) (string, error) {
	out, err := w.git(ctx, rigDir, "rev-parse", "--verify", "--quiet", application.StartPoint(remote, branch)+"^{commit}")
	return strings.TrimSpace(out), err
}

// Head implements application.FactoryCheckout: the commit the checkout has
// checked out.
func (w *Worktrees) Head(ctx context.Context, rigDir string) (string, error) {
	out, err := w.git(ctx, rigDir, "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}

// Worktrees satisfies the port a host's own checkout of the factory rig is
// looked at through.
var _ application.FactoryCheckout = (*Worktrees)(nil)
