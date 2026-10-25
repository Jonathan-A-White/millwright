package application

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
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

// ContinueKey is the note that says the next attempt of a story continues the
// branch its last attempt left (mw-1ljpo9.7): mw next writes it when an attempt
// is refused at close-out by the gate or the merge, or stopped at its turn cap,
// with the attempt as its value; mw retry keeps the branch for it, and the
// dispatch that cuts the next attempt's worktree reads it and clears it. mw retry
// --from-main clears it, and so starts the next attempt from the target.
func ContinueKey(id string) string { return "continue." + id }

// ContinuesFrom reports whether an attempt stopped for reason hands its branch
// to the next attempt: the gate's and the merge's refusals and the turn cap, the
// stops whose commits are most of the story. A branch refused for what is in its
// commits themselves (a signed commit) or for having none is not continued.
func ContinuesFrom(reason Reason) bool {
	switch reason {
	case ReasonTestsFail, ReasonMergedTestsFail, ReasonMergeConflict, ReasonTurnCap, ReasonOpenSteps, ReasonNoClosingComment, ReasonLockfileSSH, ReasonGristExampleUnproved:
		return true
	}
	return false
}

// ContinuingLine is what the boot of an attempt cut from attempt's kept branch,
// rebased onto onto, tells its session (mw-1ljpo9.7).
func ContinuingLine(attempt int, kept, onto string) string {
	return fmt.Sprintf("You continue attempt %d's branch: read git log first (git log --oneline %s..HEAD), and the story's comments "+
		"for why attempt %d did not land. Its commits were kept as %s and rebased onto %s, so they are already in your worktree: "+
		"finish or fix what is left, rather than building the story again. Close every formula step that the commits already "+
		"answer once you have checked it.", attempt, onto, attempt, kept, onto)
}

// NotContinuingLine is what the boot of an attempt that was to continue
// attempt's kept branch, but was cut from onto after all, tells its session.
func NotContinuingLine(attempt int, kept, onto, why string) string {
	return fmt.Sprintf("Attempt %d's branch, kept as %s, did not rebase onto %s without conflicts, so your worktree starts from %s, "+
		"clean (%s). Its commits are still on %s: read them first (git log --oneline %s..%s) and take what still applies.",
		attempt, kept, onto, onto, why, kept, onto, kept)
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
	// Kept says the branch was left for the next attempt to continue.
	Kept bool
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

// commitLeftovers commits whatever dir holds uncommitted onto its branch as
// "<id> attempt N: uncommitted leftovers", and says whether there was anything
// to commit. It is the one place a dead attempt's unfinished work is kept, for
// mw retry and for a dispatch that finds it in the way (mw-gq6.669).
func commitLeftovers(ctx context.Context, landing Landing, dir, storyID string, attempt int) (bool, error) {
	left, err := landing.Uncommitted(ctx, dir)
	if err != nil {
		return false, fmt.Errorf("reading what %s left uncommitted: %w", dir, err)
	}
	if len(left) == 0 {
		return false, nil
	}
	message := fmt.Sprintf("%s attempt %d: uncommitted leftovers", storyID, attempt)
	if _, err := landing.CommitLeftovers(ctx, dir, message); err != nil {
		return false, fmt.Errorf("committing what %s left uncommitted (%s): %w", dir, strings.Join(left, ", "), err)
	}
	return true, nil
}

// salvageBranch bundles branch's commits ahead of base into the vault — first
// committing whatever dir holds uncommitted, if anything — and then removes
// the worktree at dir and, unless keepBranch says the next attempt continues
// it, the branch itself, exactly as mw retry does. keepWork keeps the branch
// too when the attempt left work on it, committed or found in the worktree
// (mw-gq6.537), and deletes it when there was none. rigDir
// must already be fetched. attempt numbers the bundle file, so that a branch
// kept more than once keeps every earlier attempt's evidence rather than
// writing over it.
//
// It is shared between mw retry and a dispatch that finds a leftover
// worktree or branch from an earlier attempt in the way of a fresh cut
// (mw-gq6.107), so that the two never drift apart. What it returns is always
// what actually happened, even when it stops on an error partway through —
// never what was only planned.
func salvageBranch(ctx context.Context, worktrees Worktrees, landing Landing, files VaultFiles, vlt Vault, rigDir, storyID, dir, branch, base string, attempt int, keepBranch, keepWork bool) (salvagedBranch, error) {
	var s salvagedBranch

	committed, err := commitLeftovers(ctx, landing, dir, storyID, attempt)
	s.CommittedLeftovers = committed
	if err != nil {
		return s, err
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
		if _, err := commitWritten(ctx, files, commitMessage, []string{relPath}); err != nil {
			return s, fmt.Errorf("committing the bundle %s in the vault: %w", relPath, err)
		}
		if _, err := retryPastASiblingsWrite(ctx, sleepFor, files.Push); err != nil {
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
	// A branch the next attempt continues (mw-1ljpo9.7) is the one thing kept
	// here: the dispatch that takes the story again cuts its worktree from it.
	if keepBranch || (keepWork && !s.NothingAhead) {
		s.Kept = true
		return s, nil
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
// A story worked on another host (named by its claim, mw@<host>, before its
// path's host key) is not read here at all: its worktree is on
// that host, so the claim is given back and a comment says so, and that host's
// next dispatch bundles what it left (mw-gq6.256).
//
// A story with no worktree and no branch here at all — one refused at pour never
// got that far — has nothing to read or bundle either: the claim is given back
// and a comment says so (mw-gq6.296).
//
// It never resets a story's attempts count, and a story whose own attempts
// reach MaxAttempts is refused rather than retried — that is the Mayor's
// decision to make, not a retry's to take for them. Its own are the attempts
// mw cause named code, or named nothing: one named test, tooling or process
// failed for what is not the story's code, and the Mayor retries it (A40,
// mw-gq6.450). The one attempt it takes off the count is one the harness
// stopped for something that is not the work's (a
// usage limit, a rate limit, an overloaded or failing API: HarnessStopOf): the
// count is lowered by it as the claim is given back, and, when the time the stop
// names has not come, the host's hold on new claims is kept until then
// (mw-gq6.434).
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

	// Notes is where the host's hold on new claims is kept when the attempt
	// retried was a harness stop whose time has not come (mw-gq6.434). A nil
	// Notes keeps none.
	Notes SweepNotes

	// Now is the clock a harness stop's time is read against; nil is time.Now.
	Now func() time.Time

	// Out is where the report is printed. A nil Out prints nothing.
	Out io.Writer

	// RePathed, when not empty, is what changed since the Builder handed the
	// story back (mw retry --repathed): it is written as a RE-PATHED: comment,
	// and lets a retry of a handed-back story go on.
	RePathed string

	// FromMain is mw retry --from-main, or next.retry_from_branch = false
	// (mw-1ljpo9.7): the branch is deleted and the note that the next attempt
	// continues it (ContinueKey, in Notes) cleared, so the next attempt starts
	// from the target. Without it, a branch that note names is kept for the
	// next attempt to continue, its worktree taken away all the same.
	FromMain bool

	// Hold is mw retry --hold (mw-gq6.597): the claim is given back and the
	// story left held, never open, so no dispatch takes it again. It is for a
	// story that is to be closed, or looked at, not worked afresh.
	Hold bool

	// Reach asks the host a story was worked on what its worktree holds, when
	// the retry runs on another host (mw-gq6.774). A nil Reach reaches no host,
	// so such a retry is refused unless Force is given.
	Reach WorktreeRemote

	// Force is mw retry --force: a retry run off the story's host goes on although
	// its worktree there holds uncommitted work, or could not be asked, and says
	// that work is left unsaved there.
	Force bool

	// Err is where a work-left-unsaved warning is also printed. A nil Err prints
	// it nowhere but the report and the story's comment.
	Err io.Writer
}

// RetryReport is what one retry did, or why it changed nothing.
type RetryReport struct {
	StoryID string
	Host    string

	// Refused says the retry changed nothing at all, and Why says why: a live
	// session, or a story already tried the most times it may be.
	Refused bool
	Why     string

	// refunded is how many dead-pane refunds the story had when the retry read
	// it, which the retry sets back to 0.
	refunded int

	// holder is who held the claim when the retry read the story, the actor the
	// give-back is guarded on.
	holder string

	// unclaimed says the story had no claim at all when the retry read it, open
	// (one a dispatch passed over) or held (one whose claim an earlier stop gave
	// back and a person then held), so there is none to give back (mw-gq6.611).
	// wasHeld says it was held, and so is opened unless --hold keeps it so.
	unclaimed bool
	wasHeld   bool

	// Held says the claim was given back and the story left held, not open,
	// because Hold was asked.
	Held bool

	// RePathed says a RE-PATHED: comment was written on the story.
	RePathed bool

	// Elsewhere is the other host the story was worked on, when this retry
	// only gave the claim back and left that host's worktree and branch for its
	// own next dispatch; empty when the worktree was here.
	Elsewhere string

	// Unsaved is the warning of a forced retry from another host that left work
	// there unsaved: "work left unsaved on <host>: <path>". Empty when nothing
	// was left (mw-gq6.774).
	Unsaved string

	Worktree string
	Branch   string
	Target   string
	RigDir   string

	// Attempt is the attempt this retry bundled — the one whose session just
	// ended, not the one the next dispatch will start.
	Attempt int

	// NextAttempt is the attempt the next dispatch will record: the story's
	// attempts metadata plus one, which differs from Attempt+1 when the
	// metadata was reset by hand below the attempt that was bundled.
	NextAttempt int

	// Own is how many of the story's attempts count toward max_attempts, and
	// Uncounted the ones that do not, with the class mw cause named for each
	// (mw-gq6.450); an attempt the harness stopped is in neither, being taken
	// off the count. most is the max_attempts the retry read against.
	Own       int
	Uncounted []UncountedAttempt
	most      int

	// CommittedLeftovers says the worktree held work the session never
	// committed, and this retry committed it onto the branch before bundling.
	CommittedLeftovers bool

	// NothingAhead says the branch had no commits ahead of Target, so there was
	// nothing to bundle: the bundle and the vault commit and push were both
	// skipped, and the retry went straight on to taking the worktree and branch
	// away.
	NothingAhead bool

	// NoWorktree says neither the story's worktree nor its branch was ever made
	// here — a story refused at pour never got that far — so there was nothing
	// to read or bundle, and the retry went straight on to giving the claim back.
	NoWorktree bool

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

	// BranchKept says the branch was kept, not deleted, because the next
	// attempt continues it rebased onto Target (mw-1ljpo9.7).
	BranchKept bool

	// Stopped is the harness stop the retried attempt ended in, nil when it
	// ended any other way: that attempt is not counted. HeldUntil is when the
	// host the story was worked on claims something new again, zero when the
	// stop's time had already come.
	Stopped   *HarnessStop
	HeldUntil time.Time

	// NoCause says no cause is recorded on the story (mw cause), which the report
	// reminds of; the retry goes ahead all the same.
	NoCause bool

	// Hitl says the story carries the hitl label, so dispatch will pass it over
	// and the retry will not run until the label is taken off (mw-gq6.261).
	Hitl bool

	// FormulaClosed is what the retry closed of the formula the retried attempt
	// worked, so that the next attempt is poured a fresh one (mw-gq6.438), and
	// FormulaLeft says why what was left of it could not be closed.
	FormulaClosed FormulaClosed
	FormulaLeft   string

	// molecule is the root of the formula the retried attempt worked.
	molecule string

	// handOn says the branch was kept although mw next left no note that the
	// next attempt continues it, so the retry leaves the note itself; NoteLeft
	// says why it could not (mw-gq6.537).
	handOn   bool
	NoteLeft string
}

// maxAttempts is how many times a story may be started.
func (r Retry) maxAttempts() int {
	if r.MaxAttempts < 1 {
		return DefaultMaxAttempts
	}
	return r.MaxAttempts
}

// now is the clock a harness stop's time is read against.
func (r Retry) now() time.Time {
	if r.Now == nil {
		return time.Now()
	}
	return r.Now()
}

// remote is the remote the target branch is read from.
func (r Retry) remote() string {
	if r.Remote == "" {
		return DefaultRemote
	}
	return r.Remote
}

// workedOn is the host a story was worked on: the host its claim was made on
// when the claim is mw's, else the path's host key. Empty means no host is
// named, so the worktree is taken to be here.
func (r Retry) workedOn(path domain.Path, assignee string) string {
	if host := ClaimHost(assignee); host != "" {
		return host
	}
	if path.Host == domain.HostAuto {
		return ""
	}
	return path.Host
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
	attempt := detail.Attempts
	if attempt < 1 {
		attempt = 1
	}
	report.Attempt, report.Target = attempt, path.Branch
	report.NextAttempt = detail.Attempts + 1
	report.Hitl = detail.Hitl()
	report.NoCause = detail.Cause == ""
	report.refunded = detail.AttemptsRefunded
	report.unclaimed = detail.Assignee == "" && (detail.Status == StatusOpen || detail.Held())
	report.wasHeld = detail.Held()
	report.molecule = detail.Molecule.RootID

	// Only the story's own attempts count (mw-gq6.450): one mw cause named
	// test, tooling or process failed for what is not its code. An attempt the
	// harness stopped is not counted either (mw-gq6.434). A story open with no
	// claim was given back already, by whoever gave it back.
	tried := detail.Attempts
	own, uncounted := OwnAttempts(detail)
	report.most, report.Uncounted = r.maxAttempts(), uncounted
	if !report.unclaimed {
		if stop, stopped := harnessStopOfAttempt(ctx, r.Vault, detail, r.now()); stopped {
			report.Stopped = &stop
			report.NextAttempt = detail.Attempts
			tried--
			if len(uncounted) == 0 || uncounted[len(uncounted)-1].Attempt != detail.Attempts {
				own--
				uncounted = append(uncounted, UncountedAttempt{Attempt: detail.Attempts, Cause: "stopped by the harness"})
			} else {
				// The stopped attempt is taken off the count, so its cause is
				// not one of the attempts left.
				report.Uncounted = uncounted[:len(uncounted)-1]
			}
		}
	}
	report.Own = own

	if own >= report.most {
		return r.refuse(report, exhaustedWhy(tried, own, report.most, uncounted))
	}

	// A story its Builder handed back, with nothing said since of what changed,
	// would be handed back the same way again: --repathed says what changed
	// (mw-gq6.418). A refusal changes nothing, so the comment is written later,
	// where the claim is given back.
	if at, handedBack, err := handedBackOf(ctx, r.Tracker, detail); err != nil {
		return report, fmt.Errorf("retrying %s: reading whether its Builder handed it back: %w", storyID, err)
	} else if handedBack && strings.TrimSpace(r.RePathed) == "" {
		return r.refuse(report, fmt.Sprintf(
			"its Builder handed it back on %s and nothing says what changed since, so the next Builder would hand it back the same way: "+
				"say what changed with %s", handedBackDay(at), repathedWith(storyID)))
	}

	// A story worked on another host has its worktree and branch there, not
	// here: reading them would fail before anything was done. The claim is given
	// back without touching any worktree, and the next dispatch on that host
	// bundles whatever it left, as it does for any leftover in the way of a
	// fresh cut. Where it was worked comes from the claim (mw@<host>) before the
	// path's host key, which a person may have changed since (mw-gq6.364).
	report.holder = detail.Assignee
	keepBranch, err := r.continued(ctx, storyID)
	if err != nil {
		return report, fmt.Errorf("retrying %s: %w", storyID, err)
	}
	// An attempt that left no note, its close-out killed or its session dead,
	// still hands its work on, unless mw next refused it for what its commits
	// themselves hold (mw-gq6.537).
	keepWork := false
	if !keepBranch && !r.FromMain && r.Notes != nil {
		refused, err := r.refusedForItsCommits(ctx, detail)
		if err != nil {
			return report, fmt.Errorf("retrying %s: reading why mw next refused its last attempt: %w", storyID, err)
		}
		keepWork = !refused
	}
	if elsewhere := r.workedOn(path, detail.Assignee); elsewhere != "" && elsewhere != r.Host {
		report, err := r.askAway(ctx, report, elsewhere)
		if err != nil {
			return report, err
		}
		return r.handBack(ctx, report, elsewhere)
	}

	rigDir, checkedOut := r.Rigs[path.Rig]
	if !checkedOut {
		return report, fmt.Errorf("retrying %s: the rig %s is not checked out on %s: add it under [rigs] in the config file",
			storyID, path.Rig, r.Host)
	}
	report.RigDir = rigDir
	report.Worktree, report.Branch = WorktreeDir(rigDir, storyID), StoryBranch(storyID)

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

	// A story refused at pour never had a worktree or a branch cut: there is
	// nothing to read, bundle or remove, so only the claim is given back.
	there, err := r.Worktrees.Exists(ctx, rigDir, report.Worktree, report.Branch)
	if err != nil {
		return report, fmt.Errorf("retrying %s: checking whether %s or %s is there: %w", storyID, report.Worktree, report.Branch, err)
	}
	if !there {
		report.NoWorktree = true
		return r.giveBack(ctx, report, "there was no worktree or branch of it on "+r.Host+" to keep: none was ever made")
	}

	base := StartPoint(r.remote(), path.Branch)
	salvaged, err := salvageBranch(ctx, r.Worktrees, r.Landing, r.Files, r.Vault, rigDir, storyID, report.Worktree, report.Branch, base, attempt, keepBranch, keepWork)
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
	report.BranchGone, report.BranchKept = !salvaged.Kept, salvaged.Kept
	report.handOn = salvaged.Kept && !keepBranch

	return r.giveBack(ctx, report, "")
}

// nextSaid is the attempt the next dispatch tick takes the story as, out of
// max_attempts when ofMost says so; when some attempts were not the story's own
// code, it is its own attempt that is out of max_attempts, and the ones not
// counted are named.
func (r RetryReport) nextSaid(ofMost bool) string {
	if len(r.Uncounted) == 0 && ofMost {
		return fmt.Sprintf("attempt %d of %d", r.NextAttempt, r.most)
	}
	if len(r.Uncounted) == 0 {
		return fmt.Sprintf("attempt %d", r.NextAttempt)
	}
	return fmt.Sprintf("attempt %d, its own attempt %d of %d (not counted: %s)",
		r.NextAttempt, r.Own+1, r.most, uncountedSaid(r.Uncounted))
}

// uncountedSaid names the attempts that do not count, with their causes.
func uncountedSaid(uncounted []UncountedAttempt) string {
	named := make([]string, len(uncounted))
	for i, u := range uncounted {
		named[i] = fmt.Sprintf("attempt %d (%s)", u.Attempt, u.Cause)
	}
	return strings.Join(named, ", ")
}

// exhaustedWhy is why a retry of a story whose own attempts reached most is
// refused: how many it was started, how many of those counted, and the ones
// that did not, with their causes.
func exhaustedWhy(tried, own, most int, uncounted []UncountedAttempt) string {
	if len(uncounted) == 0 {
		return fmt.Sprintf("it has been started %d times and all %d count, the most a story may be (max_attempts is %d): attempts exhausted",
			tried, own, most)
	}
	return fmt.Sprintf("it has been started %d times and %d count, the most a story may be (max_attempts is %d): attempts exhausted; "+
		"not counted: %s", tried, own, most, uncountedSaid(uncounted))
}

// continued reports whether the story's next attempt continues the branch its
// last attempt left (mw-1ljpo9.7): the note mw next wrote when it refused or
// capped that attempt says so, and FromMain does not. It writes nothing, so
// that a refusal after it still changes nothing; fromMain clears the note.
func (r Retry) continued(ctx context.Context, storyID string) (bool, error) {
	if r.Notes == nil || r.FromMain {
		return false, nil
	}
	said, err := r.Notes.Note(ctx, ContinueKey(storyID))
	if err != nil {
		return false, fmt.Errorf("reading whether its next attempt continues its branch: %w", err)
	}
	return strings.TrimSpace(said) != "", nil
}

// refusedForItsCommits reports whether the story's last attempt was refused by
// mw next for what is in its commits themselves (ContinuesFrom's exception: a
// signed commit, a tracked binary), which the next attempt must not carry on.
// A story whose run state is not blocked was not refused at all (mw-gq6.537).
func (r Retry) refusedForItsCommits(ctx context.Context, detail StoryDetail) (bool, error) {
	state, err := r.Tracker.StoryState(ctx, detail.Story.ID, RunState)
	if err != nil {
		return false, err
	}
	if state != RunBlocked || detail.CommentCount == 0 {
		return false, nil
	}
	comments, err := r.Tracker.StoryComments(ctx, detail.Story.ID)
	if err != nil {
		return false, err
	}
	for i := len(comments) - 1; i >= 0; i-- {
		refusal, ok := refusalIn(comments[i].Text)
		if !ok {
			continue
		}
		for _, reason := range []Reason{ReasonSignedCommit, ReasonTrackedBinary} {
			if strings.HasPrefix(refusal, "("+string(reason)+")") {
				return true, nil
			}
		}
		return false, nil
	}
	return false, nil
}

// leaveNote leaves the note that the next attempt continues the branch this
// retry kept without one (mw-gq6.537). One that cannot be written is said, not
// fatal: the branch is kept, and the dispatch keeps it under another name.
func (r Retry) leaveNote(ctx context.Context, report *RetryReport) {
	if !report.handOn {
		return
	}
	if err := r.Notes.SetNote(ctx, ContinueKey(report.StoryID), strconv.Itoa(report.Attempt)); err != nil {
		report.NoteLeft = oneLine(err.Error())
	}
}

// fromMain clears, for FromMain, the note that the next attempt continues the
// branch, wherever the branch is, so that no dispatch on any host continues
// it. It runs once the retry is past its refusals, before the claim is given
// back: a note that cannot be cleared leaves the story claimed, as it was.
func (r Retry) fromMain(ctx context.Context, storyID string) error {
	if r.Notes == nil || !r.FromMain {
		return nil
	}
	if err := r.Notes.ClearNote(ctx, ContinueKey(storyID)); err != nil {
		return fmt.Errorf("retrying %s: the note that its next attempt continues its branch could not be cleared, so the claim was not given back: %w", storyID, err)
	}
	return nil
}

// giveBack gives the claim back once whatever worktree and branch there were
// are dealt with, and comments on the story saying what the retry did. nothing,
// when not empty, says why there was nothing to keep.
func (r Retry) giveBack(ctx context.Context, report RetryReport, nothing string) (RetryReport, error) {
	storyID, attempt := report.StoryID, report.Attempt
	if err := r.rePathed(ctx, &report); err != nil {
		return report, err
	}
	if err := r.fromMain(ctx, storyID); err != nil {
		return report, err
	}
	r.leaveNote(ctx, &report)
	r.closeFormula(ctx, &report)
	if err := r.release(ctx, report); err != nil {
		if report.NoWorktree {
			return report, fmt.Errorf("retrying %s: the claim could not be given back: %w", storyID, err)
		}
		return report, fmt.Errorf("retrying %s: the worktree and branch are gone, but the claim could not be given back: %w",
			storyID, err)
	}
	report.ClaimReleased, report.Held = true, r.Hold
	if err := r.settleAttempts(ctx, &report, r.Host); err != nil {
		return report, err
	}

	var comment string
	if report.NoWorktree {
		comment = fmt.Sprintf(
			"mw retry on %s gave back the claim on attempt %d of %s without reading a worktree: %s. "+
				"The story is open again and the next dispatch tick takes it as %s.",
			r.Host, attempt, storyID, nothing, report.nextSaid(true))
	} else if report.Bundled && report.BranchKept && report.NoteLeft != "" {
		comment = fmt.Sprintf(
			"mw retry on %s bundled attempt %d of %s (branch %s at %s) into %s, pushed to the vault as %s. "+
				"The worktree is gone and the branch is kept, but the note that the next attempt continues it could not be written (%s), "+
				"so the next dispatch keeps it under another name and cuts the next attempt from %s. "+
				"The claim was given back and the story is open again. The next dispatch tick takes it as %s.",
			r.Host, attempt, storyID, report.Branch, shortCommit(report.BranchCommit), report.BundlePath, shortVaultCommit(report.VaultCommit),
			report.NoteLeft, report.Target, report.nextSaid(true))
	} else if report.Bundled && report.BranchKept {
		comment = fmt.Sprintf(
			"mw retry on %s bundled attempt %d of %s (branch %s at %s) into %s, pushed to the vault as %s. "+
				"The worktree is gone and the branch is kept: the next attempt continues it, rebased onto %s "+
				"(mw retry --from-main starts it from %s instead). The claim was given back and the story is open again. "+
				"The next dispatch tick takes it as %s.",
			r.Host, attempt, storyID, report.Branch, shortCommit(report.BranchCommit), report.BundlePath, shortVaultCommit(report.VaultCommit),
			report.Target, report.Target, report.nextSaid(true))
	} else if report.Bundled {
		comment = fmt.Sprintf(
			"mw retry on %s bundled attempt %d of %s (branch %s at %s) into %s, pushed to the vault as %s. "+
				"The worktree and branch are gone; the claim was given back and the story is open again. "+
				"The next dispatch tick takes it as %s.",
			r.Host, attempt, storyID, report.Branch, shortCommit(report.BranchCommit), report.BundlePath, shortVaultCommit(report.VaultCommit),
			report.nextSaid(true))
	} else {
		comment = fmt.Sprintf(
			"mw retry on %s found nothing to keep on attempt %d of %s: %s had no commits ahead of %s, so nothing was bundled. "+
				"The worktree and branch are gone; the claim was given back and the story is open again. "+
				"The next dispatch tick takes it as %s.",
			r.Host, attempt, storyID, report.Branch, report.Target, report.nextSaid(true))
	}
	if report.Held {
		comment = r.heldComment(report, nothing)
	}
	comment += report.unclaimedSaid() + report.stopSaid() + report.formulaSaid()
	if report.Held {
		comment = HeldCommentPrefix + " " + comment
	}
	if err := r.Tracker.CommentOnStory(ctx, storyID, comment); err != nil {
		return report, fmt.Errorf("retrying %s: it was retried, but the comment naming what happened could not be written: %w", storyID, err)
	}

	return report, nil
}

// heldComment is the comment of a --hold retry that read this host's worktree
// (or found none): what became of the branch, then that the story is held.
func (r Retry) heldComment(report RetryReport, nothing string) string {
	var kept string
	switch {
	case report.NoWorktree:
		kept = "without reading a worktree: " + nothing
	case report.Bundled && report.BranchKept:
		kept = fmt.Sprintf("after bundling branch %s at %s into %s (pushed to the vault as %s); the worktree is gone and the branch is kept",
			report.Branch, shortCommit(report.BranchCommit), report.BundlePath, shortVaultCommit(report.VaultCommit))
	case report.Bundled:
		kept = fmt.Sprintf("after bundling branch %s at %s into %s (pushed to the vault as %s); the worktree and branch are gone",
			report.Branch, shortCommit(report.BranchCommit), report.BundlePath, shortVaultCommit(report.VaultCommit))
	default:
		kept = fmt.Sprintf("after finding nothing to keep: %s had no commits ahead of %s; the worktree and branch are gone", report.Branch, report.Target)
	}
	return fmt.Sprintf("mw retry --hold on %s gave back the claim on attempt %d of %s %s. %s", r.Host, report.Attempt, report.StoryID, kept, heldSaid)
}

// closeFormula closes what the retried attempt left open of its formula, before
// the claim is given back, so that the dispatch that takes the story again pours
// it a fresh one rather than working what this attempt left (mw-gq6.438). What
// cannot be closed is said, and the retry goes on: mw tidy closes it once the
// story is closed.
func (r Retry) closeFormula(ctx context.Context, report *RetryReport) {
	why := FormulaClosedWhy("mw retry", r.Host, report.StoryID, "was retried, and its next attempt pours its formula afresh")
	closed, err := closeFormula(ctx, r.Tracker, report.molecule, why)
	report.FormulaClosed = closed
	if err != nil {
		report.FormulaLeft = oneLine(err.Error())
	}
}

// release gives the claim back, guarded on whoever held it when the retry read
// the story. A story with no claim, open or held, has none to give back: it is
// held again for --hold, and a held one is opened otherwise (mw-gq6.611).
func (r Retry) release(ctx context.Context, report RetryReport) error {
	if report.unclaimed {
		if r.Hold {
			return r.Tracker.HoldStory(ctx, report.StoryID)
		}
		if report.wasHeld {
			return r.Tracker.ReleaseStory(ctx, report.StoryID)
		}
		return nil
	}
	if r.Hold {
		return r.Tracker.HoldClaimHeldBy(ctx, report.StoryID, report.holder)
	}
	return r.Tracker.ReleaseClaimHeldBy(ctx, report.StoryID, report.holder)
}

// heldSaid is what a --hold retry's comment says of the story: it is held, not
// open, so no dispatch tick takes it until mw release lets it.
const heldSaid = "The story is held, not open: no dispatch tick takes it until mw release lets it through, and a plain bd close can finish it."

// rePathed writes the RE-PATHED: comment, when --repathed said what changed, so
// that nothing holds the story back once it is open again (mw-gq6.418). It is
// written before the claim is given back: a comment that cannot be written
// leaves the story claimed, as it was.
func (r Retry) rePathed(ctx context.Context, report *RetryReport) error {
	what := strings.TrimSpace(r.RePathed)
	if what == "" {
		return nil
	}
	if err := r.Tracker.CommentOnStory(ctx, report.StoryID, RePathedMark+" "+what); err != nil {
		return fmt.Errorf("retrying %s: the RE-PATHED: comment could not be written, so the claim was not given back: %w", report.StoryID, err)
	}
	report.RePathed = true
	return nil
}

// settleAttempts sets the story's dead-pane refund count back to 0, so a retried
// story has its refunds again (mw-gq6.344), and takes an attempt the harness
// stopped off its count (mw-gq6.434). Nothing is written when there is neither.
// A stop whose time has not come holds new claims on host, the host the story
// was worked on, until then.
func (r Retry) settleAttempts(ctx context.Context, report *RetryReport, host string) error {
	fields := map[string]string{}
	if report.refunded > 0 {
		fields[AttemptsRefundedField] = "0"
	}
	if report.Stopped != nil {
		fields[AttemptsField] = strconv.Itoa(report.NextAttempt - 1)
	}
	if len(fields) > 0 {
		if err := r.Tracker.SetStoryMetadata(ctx, report.StoryID, fields); err != nil {
			return fmt.Errorf("retrying %s: the claim was given back, but its attempts could not be set right (%s): %w",
				report.StoryID, metadataSaid(fields), err)
		}
	}
	if report.Stopped == nil || !report.Stopped.Until.After(r.now()) {
		return nil
	}
	if err := keepHarnessHold(ctx, r.Notes, host, *report.Stopped); err != nil {
		return fmt.Errorf("retrying %s: the claim was given back, but the hold on new claims on %s until %s could not be kept: %w",
			report.StoryID, host, report.Stopped.Until.UTC().Format(time.RFC3339), err)
	}
	if r.Notes != nil {
		report.HeldUntil = report.Stopped.Until
	}
	return nil
}

// metadataSaid is metadata fields as a person reads them, in a fixed order.
func metadataSaid(fields map[string]string) string {
	var said []string
	for _, key := range []string{AttemptsField, AttemptsRefundedField} {
		if value, ok := fields[key]; ok {
			said = append(said, key+"="+value)
		}
	}
	return strings.Join(said, ", ")
}

// askAway asks the host a story was worked on, which is not this one, what its
// worktree there holds, before the claim is given back without reading it
// (mw-gq6.774). A clean worktree, or none, lets the retry go on. Uncommitted
// work, or a host that could not be asked, refuses it, naming the host and the
// command to run there, unless Force is given: then the retry goes on and the
// report says what is left unsaved.
func (r Retry) askAway(ctx context.Context, report RetryReport, host string) (RetryReport, error) {
	var state WorktreeState
	var asked error
	if r.Reach == nil {
		asked = fmt.Errorf("this host has no way to reach %s", host)
	} else {
		state, asked = r.Reach.WorktreeState(ctx, host, report.StoryID)
	}
	if asked == nil && state.Clean() {
		return report, nil
	}
	there := fmt.Sprintf("run `mw retry %s` on %s, which bundles it", report.StoryID, host)
	if !r.Force {
		if asked != nil {
			return r.refuse(report, fmt.Sprintf(
				"it was worked on %s, and %s could not be asked whether its worktree there holds uncommitted work (%s): "+
					"%s, or add --force to give the claim back without knowing", host, host, oneLine(asked.Error()), there))
		}
		return r.refuse(report, fmt.Sprintf(
			"it was worked on %s, and its worktree there, %s, holds uncommitted work (%s) that this retry would leave unsaved: "+
				"%s, or add --force to give the claim back and leave it there", host, state.Path, strings.Join(state.Left, ", "), there))
	}
	where := state.Path
	if asked != nil {
		where = fmt.Sprintf("the worktree of %s (%s could not be asked what it holds: %s)", report.StoryID, host, oneLine(asked.Error()))
	}
	report.Unsaved = fmt.Sprintf("work left unsaved on %s: %s", host, where)
	return report, nil
}

// handBack is a retry of a story another host worked: the claim is given back
// and a comment says where the worktree and branch were left. Nothing on this
// host's disk is read or changed, and whether that host's session still runs
// is not asked, so it is for the person to have seen it end.
func (r Retry) handBack(ctx context.Context, report RetryReport, elsewhere string) (RetryReport, error) {
	storyID := report.StoryID
	report.Elsewhere = elsewhere
	// That host's dispatch claimed it under its own actor, so the give-back
	// expects whoever holds it now (report.holder), not this host's actor.
	if err := r.rePathed(ctx, &report); err != nil {
		return report, err
	}
	if err := r.fromMain(ctx, storyID); err != nil {
		return report, err
	}
	r.closeFormula(ctx, &report)
	if err := r.release(ctx, report); err != nil {
		return report, fmt.Errorf("retrying %s: the claim could not be given back: %w", storyID, err)
	}
	report.ClaimReleased, report.Held = true, r.Hold
	if report.Unsaved != "" && r.Err != nil {
		fmt.Fprintln(r.Err, report.Unsaved)
	}
	if err := r.settleAttempts(ctx, &report, elsewhere); err != nil {
		return report, err
	}

	comment := fmt.Sprintf(
		"mw retry on %s gave back the claim on attempt %d of %s without reading a worktree: the story was worked on %s, "+
			"so its worktree and branch are left there, and the next dispatch on %s bundles whatever they hold before it cuts a fresh one. "+
			"The story is open again and the next dispatch tick takes it as %s.",
		r.Host, report.Attempt, storyID, elsewhere, elsewhere, report.nextSaid(true))
	if report.Held {
		comment = fmt.Sprintf(
			"mw retry --hold on %s gave back the claim on attempt %d of %s without reading a worktree: the story was worked on %s, "+
				"so its worktree and branch are left there. %s",
			r.Host, report.Attempt, storyID, elsewhere, heldSaid)
	}
	comment += report.unsavedSaid() + report.unclaimedSaid() + report.stopSaid() + report.formulaSaid()
	if report.Held {
		comment = HeldCommentPrefix + " " + comment
	}
	if err := r.Tracker.CommentOnStory(ctx, storyID, comment); err != nil {
		return report, fmt.Errorf("retrying %s: the claim was given back, but the comment naming what happened could not be written: %w", storyID, err)
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
	if r.NoCause {
		fmt.Fprintf(&b, "  %s\n", NoCauseReminder(r.StoryID))
	}
	if r.RePathed {
		fmt.Fprintf(&b, "  re-path the story was told what changed (%s comment)\n", RePathedMark)
	}
	if r.Stopped != nil {
		fmt.Fprintf(&b, "  stop    attempt %d was stopped by the harness (%s: %s), not by its work: it is not counted\n",
			r.Attempt, r.Stopped.Reason, r.Stopped.Said)
		if !r.HeldUntil.IsZero() {
			fmt.Fprintf(&b, "  hold    no new claims until %s\n", r.HeldUntil.UTC().Format(time.RFC3339))
		}
	}
	if r.Elsewhere != "" {
		fmt.Fprintf(&b, "  there   the story was worked on %s; its worktree and branch are left there, not read from here\n", r.Elsewhere)
		if r.Unsaved != "" {
			fmt.Fprintf(&b, "  unsaved %s\n", r.Unsaved)
		}
		r.writeFormula(&b)
		if r.ClaimReleased {
			r.writeOpen(&b)
		}
		return b.String()
	}
	if r.NoWorktree {
		fmt.Fprintf(&b, "  bundle  nothing to keep: no worktree or branch of the story was ever made here\n")
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
	if r.WorktreeGone && r.BranchKept {
		fmt.Fprintf(&b, "  gone    the worktree %s\n", r.Worktree)
		if r.NoteLeft != "" {
			fmt.Fprintf(&b, "  kept    the branch %s, but the note that the next attempt continues it could not be written (%s): it starts from %s\n",
				r.Branch, r.NoteLeft, r.Target)
		} else {
			fmt.Fprintf(&b, "  kept    the branch %s: the next attempt continues it, rebased onto %s (mw retry --from-main starts clean)\n", r.Branch, r.Target)
		}
	}
	r.writeFormula(&b)
	if r.ClaimReleased {
		r.writeOpen(&b)
	}
	return b.String()
}

// writeOpen says what the claim's give-back left: the story open for the next
// dispatch tick, or held when --hold asked for it.
func (r RetryReport) writeOpen(b *strings.Builder) {
	if r.unclaimed {
		fmt.Fprintf(b, "  claim   the claim was already given back\n")
	}
	if r.Held {
		fmt.Fprintf(b, "  held    the claim was given back; the story is held, not open: no dispatch tick takes it until mw release\n")
		return
	}
	fmt.Fprintf(b, "  open    the claim was given back; the next dispatch tick takes it as %s\n", r.nextSaid(false))
	r.writeHitl(b)
}

// formulaSaid is what a retry's comment adds of the formula it closed.
func (r RetryReport) formulaSaid() string {
	said := ""
	if r.FormulaClosed.Any() {
		said = fmt.Sprintf(" The formula it worked was closed (%s), so the next attempt is poured a fresh one.", r.FormulaClosed)
	}
	if r.FormulaLeft != "" {
		said += fmt.Sprintf(" What was left open of the formula %s could not all be closed: %s.", r.molecule, r.FormulaLeft)
	}
	return said
}

// unsavedSaid is what a retry's comment adds when it was forced from another
// host and left work there unsaved (mw-gq6.774).
func (r RetryReport) unsavedSaid() string {
	if r.Unsaved == "" {
		return ""
	}
	return " " + r.Unsaved + "."
}

// unclaimedSaid is what a retry's comment adds when the story had no claim to
// give back (mw-gq6.611).
func (r RetryReport) unclaimedSaid() string {
	if !r.unclaimed {
		return ""
	}
	return " The claim was already given back."
}

// stopSaid is what a retry's comment adds when the attempt it retried was a
// harness stop: that it is not counted, and how long the host is held.
func (r RetryReport) stopSaid() string {
	if r.Stopped == nil {
		return ""
	}
	said := fmt.Sprintf(" Attempt %d was stopped by the harness (%s: %s), not by its work, so it is not counted.",
		r.Attempt, r.Stopped.Reason, r.Stopped.Said)
	if !r.HeldUntil.IsZero() {
		said += fmt.Sprintf(" No new story is claimed on the host it was worked on until %s.", r.HeldUntil.UTC().Format(time.RFC3339))
	}
	return said
}

// writeFormula says what the retry closed of the attempt's formula.
func (r RetryReport) writeFormula(b *strings.Builder) {
	if r.FormulaClosed.Any() {
		fmt.Fprintf(b, "  formula closed %s: the next attempt is poured a fresh one\n", r.FormulaClosed)
	}
	if r.FormulaLeft != "" {
		fmt.Fprintf(b, "  formula %s could not all be closed: %s\n", r.molecule, r.FormulaLeft)
	}
}

// writeHitl says, for a story that carries hitl, why the next dispatch tick will
// not take it and what lets it.
func (r RetryReport) writeHitl(b *strings.Builder) {
	if !r.Hitl {
		return
	}
	fmt.Fprintf(b, "  %s: dispatch passes this story over while it carries %s; to let it run: bd update %s --remove-label %s\n",
		LabelHitl, LabelHitl, r.StoryID, LabelHitl)
}
