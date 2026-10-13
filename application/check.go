package application

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// Check is what a Builder runs on its own branch before its session ends: the
// checks mw next makes before it lands a story — the branch has commits, none
// is signed as a machine's work, every step of the formula is closed and the
// rig's own tests pass in the worktree — run now, while a refusal is still
// cheap to fix, and worded as mw next would word it.
//
// It is read-only, and that is the point of it. It writes nothing to the
// tracker, the ledger, the vault or git: no comment, no run state, no fetch. The
// one thing it takes is the rig's merge slot, around the rig's tests only, so
// that a close-out's gate and a check's gate on one rig never run at once and
// starve each other; it waits for the slot, and the slot is given back when the
// tests are done. It does not fetch, so the commits are counted against the target
// branch as the rig last saw the remote's; it does not read the session's
// result, which does not exist until the session ends; and it does not try the
// merge, so a branch it passes can still be stopped by a conflict.
//
// Unlike mw next, which stops at the first refusal, Check reports every check
// that fails, because a session that fixes one and runs it again pays for the
// tests each time.
type Check struct {
	Tracker WorkTracker
	Landing Landing
	Checks  Checks

	// SpecChecks runs the rig's changed-specs command after its tests, on the
	// specs the branch added or changed (mw-gq6.366). Nil runs none.
	SpecChecks SpecChecks

	// Slot is the merge slot of the rig, taken around the rig's tests so that
	// one gate per rig runs at a time on this host. Nil takes no slot.
	Slot MergeSlot

	// Load is how busy this host is, read around the rig's tests as a
	// close-out reads it (mw-gq6.379): the check waits, up to LoadBound, for a
	// busy host to calm before the tests, and runs them once more when they fail
	// while it was busy. A nil Load, or a load that cannot be read, does
	// neither. LoadPoll, LoadBound and LoadWait are Next's own, with the same
	// defaults.
	Load      HostLoad
	LoadPoll  time.Duration
	LoadBound time.Duration
	LoadWait  func(ctx context.Context, d time.Duration) error

	// Host is which host this is, and Rigs is where each rig is checked out.
	Host string
	Rigs map[string]string

	// Remote is the remote the target branch is read from. Empty is
	// DefaultRemote.
	Remote string

	// Logs is where the check's whole output is also kept, as runs/<story>/
	// check-<attempt>.log, so that a check run in the background has a log named
	// for its story that no other story's check can overwrite (mw-gq6.380). Nil
	// keeps none.
	Logs CheckLogs

	// Out is where the report is printed. A nil Out prints nothing.
	Out io.Writer
}

// CheckLogs is the part of the vault that keeps a check's log: one file of a
// story's run, and where it belongs.
type CheckLogs interface {
	PutRunFile(ctx context.Context, storyID, name, contents string) (string, error)
	RunFile(storyID, name string) string
}

// CheckLogFileName is the name of the log a check of a story's attempt is kept
// under, in the story's run directory.
func CheckLogFileName(attempt int) string {
	if attempt < 1 {
		attempt = 1
	}
	return fmt.Sprintf("check-%d.log", attempt)
}

// CheckStarter starts mw check on a story as a process of its own that outlives
// the one that asked, with everything it prints appended to the file at logPath.
// It returns once the check has started, not when it is done.
type CheckStarter interface {
	Start(ctx context.Context, storyID, logPath string) error
}

// CheckBackground is `mw check --background`: it starts the check of a story
// detached, names the log it writes to for the story and its attempt, and returns
// at once with that log's path, so that a session whose tool call is cut off
// after ten minutes can still read the verdict, and no two stories share a log.
type CheckBackground struct {
	Tracker WorkTracker
	Logs    CheckLogs
	Starter CheckStarter
	Out     io.Writer
}

// Run starts the check of one story and returns the path of its log.
func (b CheckBackground) Run(ctx context.Context, storyID string) (string, error) {
	switch {
	case b.Tracker == nil || b.Logs == nil || b.Starter == nil:
		return "", fmt.Errorf("checking a story in the background: it needs a work tracker, a place for the log and a way to start the check")
	case strings.TrimSpace(storyID) == "":
		return "", fmt.Errorf("checking a story in the background: which story?")
	}
	detail, err := b.Tracker.ShowStory(ctx, storyID)
	if err != nil {
		return "", fmt.Errorf("checking %s: %w", storyID, err)
	}
	name := CheckLogFileName(detail.Attempts)
	if _, err := b.Logs.PutRunFile(ctx, storyID, name, "check of "+storyID+" started in the background: its verdict is the last lines of this log\n"); err != nil {
		return "", fmt.Errorf("checking %s: the log could not be made: %w", storyID, err)
	}
	logPath := b.Logs.RunFile(storyID, name)
	if err := b.Starter.Start(ctx, storyID, logPath); err != nil {
		return "", fmt.Errorf("checking %s: %w", storyID, err)
	}
	if b.Out != nil {
		fmt.Fprintf(b.Out, "check of %s started in the background; its log is %s\n", storyID, logPath)
	}
	return logPath, nil
}

// CheckReport is what one check found.
type CheckReport struct {
	StoryID string
	Title   string
	// Attempt is which attempt of the story was checked, 0 when the story could
	// not be read.
	Attempt int
	Host    string
	Branch  string
	Target  string
	// Commits is how many commits the branch holds that the target branch does
	// not.
	Commits int
	// Refusals is every reason mw next would not land the branch, in the order
	// mw next asks. Empty means every check passed.
	Refusals []Refusal
	// Notes are the things that went sideways without changing the outcome.
	Notes []string
}

// Passed reports whether every check passed.
func (r CheckReport) Passed() bool { return len(r.Refusals) == 0 }

// Run checks one story's branch and reports what it found. The error it
// returns is what makes mw check exit non-zero: any check that failed, and
// anything that stopped the checking at all.
func (k Check) Run(ctx context.Context, storyID string) (CheckReport, error) {
	// Everything printed is also kept, to be written to the story's log when the
	// check is done.
	var kept bytes.Buffer
	shown := k.Out
	if k.Logs != nil {
		k.Out = teeTo(shown, &kept)
	}
	report, err := k.check(ctx, storyID)
	// A check that could not be made has nothing to report, and the error says
	// why; only a branch that was actually read has a verdict to print.
	if k.Out != nil && report.Branch != "" {
		fmt.Fprint(k.Out, report.String())
	} else if err != nil {
		// Nothing was printed, so the log would end with no verdict at all.
		fmt.Fprintf(&kept, "mw check: %v\n", err)
	}
	if k.Logs != nil && report.Attempt > 0 {
		name := CheckLogFileName(report.Attempt)
		if _, werr := k.Logs.PutRunFile(ctx, storyID, name, kept.String()); werr != nil && shown != nil {
			fmt.Fprintf(shown, "  note    the log of this check could not be kept in the run of %s: %v\n", storyID, werr)
		}
	}
	return report, err
}

// teeTo writes to both, or to one when the other is nil.
func teeTo(out io.Writer, kept *bytes.Buffer) io.Writer {
	if out == nil {
		return kept
	}
	return io.MultiWriter(out, kept)
}

func (k Check) check(ctx context.Context, storyID string) (CheckReport, error) {
	report := CheckReport{StoryID: storyID, Host: k.Host}
	switch {
	case k.Tracker == nil || k.Landing == nil || k.Checks == nil:
		return report, fmt.Errorf("checking a story: it needs a work tracker, a landing and the rig's checks")
	case strings.TrimSpace(storyID) == "":
		return report, fmt.Errorf("checking a story: which story?")
	}

	detail, err := k.Tracker.ShowStory(ctx, storyID)
	if err != nil {
		return report, fmt.Errorf("checking %s: %w", storyID, err)
	}
	report.Title = detail.Story.Title
	report.Attempt = max(detail.Attempts, 1)
	if detail.Closed() {
		return report, fmt.Errorf("checking %s: it is closed already, so there is nothing left to land", storyID)
	}
	path, err := detail.Path()
	if err != nil {
		return report, fmt.Errorf("checking %s: %w", storyID, err)
	}
	rigDir, checkedOut := k.Rigs[path.Rig]
	if !checkedOut {
		return report, fmt.Errorf("checking %s: the rig %s is not checked out on %s: add it under [rigs] in the config file",
			storyID, path.Rig, k.Host)
	}

	c := &closeOut{
		id:       storyID,
		detail:   detail,
		path:     path,
		rigDir:   rigDir,
		worktree: WorktreeDir(rigDir, storyID),
		branch:   StoryBranch(storyID),
		target:   path.Branch,
	}
	report.Branch, report.Target = c.branch, c.target

	// The rig's tests are run holding the rig's merge slot, as a close-out's
	// are, so that one gate per rig runs at a time on this host.
	checks := k.Checks
	if k.Slot != nil {
		checks = slottedChecks{
			checks: k.Checks,
			slot:   k.Slot,
			rigDir: rigDir,
			holder: fmt.Sprintf("%s checking %s", SeatIdentity("", k.Host), storyID),
		}
	}

	var specs SpecChecks
	if k.SpecChecks != nil {
		specs = k.SpecChecks
		if k.Slot != nil {
			specs = slottedSpecs{
				specs:  k.SpecChecks,
				slot:   k.Slot,
				rigDir: rigDir,
				holder: fmt.Sprintf("%s checking %s", SeatIdentity("", k.Host), storyID),
			}
		}
	}

	// The checks are mw next's own, run by the part of it that reads and never
	// writes, so that what a session is told here and what it is told at
	// close-out cannot come apart.
	var read NextReport
	report.Refusals = Next{
		Tracker:    k.Tracker,
		Landing:    k.Landing,
		Checks:     checks,
		SpecChecks: specs,
		Remote:     k.Remote,
		Load:       k.Load,
		LoadPoll:   k.LoadPoll,
		LoadBound:  k.LoadBound,
		LoadWait:   k.LoadWait,
		Host:       k.Host,
		Out:        k.Out,
	}.refusals(ctx, c, &read, true)
	report.Commits, report.Notes = read.Commits, read.Notes

	if !report.Passed() {
		return report, fmt.Errorf("checking %s: %d check(s) failed, so mw next would not land it", storyID, len(report.Refusals))
	}
	return report, nil
}

// slottedChecks runs the rig's tests while holding the rig's merge slot.
type slottedChecks struct {
	checks Checks
	slot   MergeSlot
	rigDir string
	holder string
}

func (s slottedChecks) Run(ctx context.Context, rig, dir string) (Checked, error) {
	holding, err := s.slot.Take(ctx, s.rigDir, s.holder)
	if err != nil {
		return Checked{}, fmt.Errorf("the merge slot of %s could not be taken: %w", rig, err)
	}
	defer holding.Release(ctx)
	return s.checks.Run(ctx, rig, dir)
}

// String is the check as a person reads it.
func (r CheckReport) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "check on %s: %s\n", r.Host, r.StoryID)
	for _, refusal := range r.Refusals {
		fmt.Fprintf(&b, "  REFUSED %s\n", refusal.Why)
		if refusal.Said != "" {
			for _, line := range strings.Split(refusal.Said, "\n") {
				fmt.Fprintf(&b, "            %s\n", line)
			}
		}
	}
	for _, note := range r.Notes {
		fmt.Fprintf(&b, "  note    %s\n", note)
	}
	switch {
	case r.Passed():
		fmt.Fprintf(&b, "  passed  %d commit(s) on %s, none signed, every formula step closed, the rig's tests pass: mw next would land it\n",
			r.Commits, r.Branch)
		fmt.Fprintf(&b, "          (mw next also needs the session to finish, and %s to merge into %s without conflicts)\n", r.Branch, r.Target)
	default:
		fmt.Fprintf(&b, "  failed  %d check(s): mw next would not land %s as it stands\n", len(r.Refusals), r.Branch)
	}
	return b.String()
}
