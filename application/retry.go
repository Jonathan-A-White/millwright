package application

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// BundleFileName is the name a retry's bundle of one attempt's branch is kept
// under, in the story's own run directory: numbered by the attempt it
// bundled, so that a story retried more than once keeps every earlier
// attempt's evidence rather than writing over it.
func BundleFileName(attempt int) string {
	return fmt.Sprintf("attempt-%d.bundle", attempt)
}

// BundleRecord is where a retry's bundle of one attempt belongs, by path from
// the vault's root — what VaultFiles.Commit is given. A story id that would
// reach outside the vault gives no path.
func BundleRecord(storyID string, attempt int) string {
	return runFilePath(storyID, BundleFileName(attempt))
}

// salvagedBranch is what became of a branch's leftover commits once
// salvageBranch kept them before the branch itself was cleared away.
type salvagedBranch struct {
	// CommittedLeftovers says the worktree held work nobody had committed, and
	// it was committed onto the branch before bundling.
	CommittedLeftovers bool
	// NothingAhead says the branch had no commits ahead of base, so bundling,
	// committing and pushing were all skipped.
	NothingAhead bool
	// Bundled says the branch's commits were captured into a bundle that
	// verified, and BranchCommit and BundlePath are meaningful.
	Bundled      bool
	BranchCommit string
	BundlePath   string
	// VaultPushed says the bundle reached the vault's own remote, and
	// VaultCommit is meaningful.
	VaultPushed bool
	VaultCommit string
}

// salvageBranch bundles branch's commits ahead of base into the vault — first
// committing whatever dir holds uncommitted, if anything — and then removes
// the worktree at dir and the branch itself, exactly as mw retry does. rigDir
// must already be fetched. attempt numbers the bundle file, so that a branch
// kept more than once keeps every earlier attempt's evidence rather than
// writing over it.
//
// It is shared between mw retry and a dispatch that finds a leftover
// worktree or branch from an earlier attempt in the way of a fresh cut
// (mw-gq6.107), so that the two never drift apart. What it returns is always
// what actually happened, even when it stops on an error partway through —
// never what was only planned.
func salvageBranch(ctx context.Context, worktrees Worktrees, landing Landing, files VaultFiles, vlt Vault, rigDir, storyID, dir, branch, base string, attempt int) (salvagedBranch, error) {
	var s salvagedBranch

	left, err := landing.Uncommitted(ctx, dir)
	if err != nil {
		return s, fmt.Errorf("reading what %s left uncommitted: %w", dir, err)
	}
	if len(left) > 0 {
		message := fmt.Sprintf("%s attempt %d: uncommitted leftovers", storyID, attempt)
		if _, err := landing.CommitLeftovers(ctx, dir, message); err != nil {
			return s, fmt.Errorf("committing what %s left uncommitted (%s): %w", dir, strings.Join(left, ", "), err)
		}
		s.CommittedLeftovers = true
	}

	ahead, err := landing.Ahead(ctx, rigDir, branch, base)
	if err != nil {
		return s, fmt.Errorf("counting the commits %s has ahead of %s: %w", branch, base, err)
	}
	kept := "there was nothing to keep"
	if ahead == 0 {
		s.NothingAhead = true
	} else {
		dest := vlt.RunFile(storyID, BundleFileName(attempt))
		sha, err := landing.Bundle(ctx, rigDir, branch, base, dest)
		if err != nil {
			return s, fmt.Errorf("bundling %s into %s: %w", branch, dest, err)
		}
		s.BranchCommit = sha
		if err := landing.VerifyBundle(ctx, rigDir, dest); err != nil {
			return s, fmt.Errorf("the bundle %s does not verify, so nothing more was touched: %w", dest, err)
		}
		s.Bundled = true

		relPath := BundleRecord(storyID, attempt)
		s.BundlePath = relPath
		kept = fmt.Sprintf("the bundle %s is safe in the vault", relPath)
		commitMessage := fmt.Sprintf("%s: bundle of attempt %d branch %s, kept before it was cleared away", storyID, attempt, branch)
		if _, err := files.Commit(ctx, commitMessage, []string{relPath}); err != nil {
			return s, fmt.Errorf("committing the bundle %s in the vault: %w", relPath, err)
		}
		if _, err := files.Push(ctx); err != nil {
			return s, fmt.Errorf("the bundle %s is committed in the vault but could not be pushed, so nothing more was touched: %w", relPath, err)
		}
		s.VaultPushed = true
		if head, err := files.Head(ctx); err == nil {
			s.VaultCommit = head
		}
	}

	if err := worktrees.RemoveWithoutForce(ctx, rigDir, dir); err != nil {
		return s, fmt.Errorf("%s, but the worktree %s could not be removed: %w", kept, dir, err)
	}
	if err := worktrees.DeleteBranch(ctx, rigDir, branch); err != nil {
		return s, fmt.Errorf("%s and the worktree is gone, but the branch %s could not be deleted: %w", kept, branch, err)
	}
	return s, nil
}

// Retry is mw's answer to a story whose session has ended and whose landing
// was refused (or that a person has otherwise decided to try again): it is
// the hand recovery that used to take a person several steps and a round trip
// through the Millhand, run as one command (friction: mw-gq6.92).
//
// In order, refusing at the first that does not hold: the session named on
// the story must not still be running; the branch's own worktree, fetched
// fresh, is committed clean if a session left it dirty; the branch's commits
// ahead of the target, if there are any, are bundled into the vault and the
// bundle must verify, and the vault must accept and push that bundle — a
// branch with nothing ahead of the target has nothing to bundle, and that
// step is skipped rather than refused. Only once all of that has held are the
// worktree and branch taken away — the worktree never forced, the branch
// deleted safely and only forced when that refuses it — and the claim given
// back with the story set open again, so the next dispatch tick claims it as
// a fresh attempt. A refusal changes nothing at all: nothing is claimed, cut,
// bundled or removed. An error partway through leaves the report naming only
// what actually happened before it, never what was only planned.
//
// It never resets a story's attempts count, and a story already started
// MaxAttempts times is refused rather than retried — that is the Mayor's
// decision to make, not a retry's to take for them.
type Retry struct {
	Tracker   WorkTracker
	Worktrees Worktrees
	Landing   Landing
	Runner    Runner
	Files     VaultFiles
	Vault     Vault

	// Host is which host this retry runs on, and Rigs is where each rig is
	// checked out on it.
	Host string
	Rigs map[string]string

	// Remote is the remote the target branch is read from. Empty is
	// DefaultRemote.
	Remote string

	// MaxAttempts is how many times a story may be started in all; a story
	// already tried that many times is refused, not retried. Fewer than one is
	// DefaultMaxAttempts.
	MaxAttempts int

	// Out is where the report is printed. A nil Out prints nothing.
	Out io.Writer
}

// RetryReport is what one retry did, or why it changed nothing.
type RetryReport struct {
	StoryID string
	Host    string

	// Refused says the retry changed nothing at all, and Why says why: a live
	// session, or a story already tried the most times it may be.
	Refused bool
	Why     string

	Worktree string
	Branch   string
	Target   string
	RigDir   string

	// Attempt is the attempt this retry bundled — the one whose session just
	// ended, not the one the next dispatch will start.
	Attempt int

	// CommittedLeftovers says the worktree held work the session never
	// committed, and this retry committed it onto the branch before bundling.
	CommittedLeftovers bool

	// NothingAhead says the branch had no commits ahead of Target, so there was
	// nothing to bundle: the bundle and the vault commit and push were both
	// skipped, and the retry went straight on to taking the worktree and branch
	// away.
	NothingAhead bool

	// Bundled says the branch's commits were captured into a bundle that
	// verified. BranchCommit and BundlePath are only meaningful when this is
	// true.
	Bundled bool
	// BranchCommit is the branch's tip, the commit the bundle captured.
	BranchCommit string
	// BundlePath is where the bundle was written, by path from the vault's
	// root.
	BundlePath string

	// VaultPushed says the bundle was committed in the vault and the vault
	// pushed. VaultCommit is only meaningful when this is true.
	VaultPushed bool
	// VaultCommit is the commit the bundle landed in the vault as, empty when
	// it could not be read back — the push still went through.
	VaultCommit string

	// WorktreeGone and BranchGone say the worktree and the branch were taken
	// away. ClaimReleased says the claim was given back and the story set open
	// again.
	WorktreeGone  bool
	BranchGone    bool
	ClaimReleased bool
}

// maxAttempts is how many times a story may be started.
func (r Retry) maxAttempts() int {
	if r.MaxAttempts < 1 {
		return DefaultMaxAttempts
	}
	return r.MaxAttempts
}

// remote is the remote the target branch is read from.
func (r Retry) remote() string {
	if r.Remote == "" {
		return DefaultRemote
	}
	return r.Remote
}

// Run retries one story. The error it returns is why nothing more was done;
// the report says what, if anything, was.
func (r Retry) Run(ctx context.Context, storyID string) (RetryReport, error) {
	report, err := r.run(ctx, storyID)
	r.print(report.String())
	return report, err
}

func (r Retry) run(ctx context.Context, storyID string) (RetryReport, error) {
	report := RetryReport{StoryID: storyID, Host: r.Host}
	switch {
	case r.Tracker == nil || r.Worktrees == nil || r.Landing == nil || r.Runner == nil || r.Files == nil || r.Vault == nil:
		return report, fmt.Errorf("retrying %s: a retry needs a work tracker, worktrees, a landing, a runner, the vault's files and the vault", storyID)
	case r.Host == "":
		return report, fmt.Errorf("retrying %s: which host is this? set MW_HOST, or host in the config file", storyID)
	case strings.TrimSpace(storyID) == "":
		return report, fmt.Errorf("retrying: which story?")
	}

	detail, err := r.Tracker.ShowStory(ctx, storyID)
	if err != nil {
		return report, fmt.Errorf("retrying %s: %w", storyID, err)
	}
	if detail.Closed() {
		return report, fmt.Errorf("retrying %s: it is closed already, so there is nothing to retry", storyID)
	}

	path, err := detail.Path()
	if err != nil {
		return report, fmt.Errorf("retrying %s: %w", storyID, err)
	}
	rigDir, checkedOut := r.Rigs[path.Rig]
	if !checkedOut {
		return report, fmt.Errorf("retrying %s: the rig %s is not checked out on %s: add it under [rigs] in the config file",
			storyID, path.Rig, r.Host)
	}

	attempt := detail.Attempts
	if attempt < 1 {
		attempt = 1
	}
	report.Attempt, report.RigDir, report.Target = attempt, rigDir, path.Branch
	report.Worktree, report.Branch = WorktreeDir(rigDir, storyID), StoryBranch(storyID)

	if tried, most := detail.Attempts, r.maxAttempts(); tried >= most {
		return r.refuse(report, fmt.Sprintf(
			"it has been started %d times, the most a story may be (max_attempts is %d): attempts exhausted", tried, most))
	}

	name := SessionName(storyID)
	status, err := r.Runner.Status(ctx, name)
	if err != nil {
		return report, fmt.Errorf("retrying %s: asking whether its session %s is still running: %w", storyID, name, err)
	}
	if status.Running() {
		return r.refuse(report, fmt.Sprintf("its session %s is still running, so nothing was touched", name))
	}

	if err := r.Worktrees.Fetch(ctx, rigDir); err != nil {
		return report, fmt.Errorf("retrying %s: fetching the rig: %w", storyID, err)
	}

	base := StartPoint(r.remote(), path.Branch)
	salvaged, err := salvageBranch(ctx, r.Worktrees, r.Landing, r.Files, r.Vault, rigDir, storyID, report.Worktree, report.Branch, base, attempt)
	report.CommittedLeftovers = salvaged.CommittedLeftovers
	report.NothingAhead = salvaged.NothingAhead
	report.Bundled = salvaged.Bundled
	report.BranchCommit = salvaged.BranchCommit
	report.BundlePath = salvaged.BundlePath
	report.VaultPushed = salvaged.VaultPushed
	report.VaultCommit = salvaged.VaultCommit
	if err != nil {
		return report, fmt.Errorf("retrying %s: %w", storyID, err)
	}
	report.WorktreeGone = true
	report.BranchGone = true

	if err := r.Tracker.ReleaseClaim(ctx, storyID); err != nil {
		return report, fmt.Errorf("retrying %s: the worktree and branch are gone, but the claim could not be given back: %w",
			storyID, err)
	}
	report.ClaimReleased = true

	var comment string
	if report.Bundled {
		comment = fmt.Sprintf(
			"mw retry on %s bundled attempt %d of %s (branch %s at %s) into %s, pushed to the vault as %s. "+
				"The worktree and branch are gone; the claim was given back and the story is open again. "+
				"The next dispatch tick takes it as attempt %d of %d.",
			r.Host, attempt, storyID, report.Branch, shortCommit(report.BranchCommit), report.BundlePath, shortVaultCommit(report.VaultCommit),
			attempt+1, r.maxAttempts())
	} else {
		comment = fmt.Sprintf(
			"mw retry on %s found nothing to keep on attempt %d of %s: %s had no commits ahead of %s, so nothing was bundled. "+
				"The worktree and branch are gone; the claim was given back and the story is open again. "+
				"The next dispatch tick takes it as attempt %d of %d.",
			r.Host, attempt, storyID, report.Branch, report.Target, attempt+1, r.maxAttempts())
	}
	if err := r.Tracker.CommentOnStory(ctx, storyID, comment); err != nil {
		return report, fmt.Errorf("retrying %s: it was retried, but the comment naming what happened could not be written: %w", storyID, err)
	}

	return report, nil
}

// refuse is a retry that changes nothing at all, saying why.
func (r Retry) refuse(report RetryReport, why string) (RetryReport, error) {
	report.Refused, report.Why = true, why
	return report, fmt.Errorf("retrying %s: %s", report.StoryID, why)
}

// print writes the report, when there is somewhere to write it.
func (r Retry) print(text string) {
	if r.Out == nil {
		return
	}
	fmt.Fprint(r.Out, text)
}

// shortVaultCommit is the vault commit as a person reads it: short, or said
// plainly as unknown when it could not be read back.
func shortVaultCommit(commit string) string {
	if commit == "" {
		return "unknown"
	}
	return shortCommit(commit)
}

// String is the retry as a person reads it: only the steps it actually
// completed, never the ones it only planned — a refusal, or an error partway
// through, shows exactly as far as the retry got and no further.
func (r RetryReport) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "retry on %s: %s\n", r.Host, r.StoryID)
	if r.Refused {
		fmt.Fprintf(&b, "  REFUSED %s\n", r.Why)
		return b.String()
	}
	if r.CommittedLeftovers {
		fmt.Fprintf(&b, "  commit  the worktree's uncommitted work was committed onto %s\n", r.Branch)
	}
	switch {
	case r.NothingAhead:
		fmt.Fprintf(&b, "  bundle  nothing to keep: no commits ahead of %s\n", r.Target)
	case r.Bundled:
		fmt.Fprintf(&b, "  bundle  attempt %d of %s (%s) into %s\n", r.Attempt, r.Branch, shortCommit(r.BranchCommit), r.BundlePath)
		if r.VaultPushed {
			fmt.Fprintf(&b, "  vault   pushed as %s\n", shortVaultCommit(r.VaultCommit))
		}
	}
	if r.WorktreeGone && r.BranchGone {
		fmt.Fprintf(&b, "  gone    the worktree %s and the branch %s\n", r.Worktree, r.Branch)
	}
	if r.ClaimReleased {
		fmt.Fprintf(&b, "  open    the claim was given back; the next dispatch tick takes it as attempt %d\n", r.Attempt+1)
	}
	return b.String()
}
