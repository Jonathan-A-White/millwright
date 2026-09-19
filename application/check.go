package application

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// Check is what a Builder runs on its own branch before its session ends: the
// checks mw next makes before it lands a story — the branch has commits, none
// is signed as a machine's work, every step of the formula is closed and the
// rig's own tests pass in the worktree — run now, while a refusal is still
// cheap to fix, and worded as mw next would word it.
//
// It is read-only, and that is the point of it. It writes nothing to the
// tracker, the ledger, the vault or git: no comment, no run state, no fetch, no
// merge slot. It does not fetch, so the commits are counted against the target
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

	// Host is which host this is, and Rigs is where each rig is checked out.
	Host string
	Rigs map[string]string

	// Remote is the remote the target branch is read from. Empty is
	// DefaultRemote.
	Remote string

	// Out is where the report is printed. A nil Out prints nothing.
	Out io.Writer
}

// CheckReport is what one check found.
type CheckReport struct {
	StoryID string
	Title   string
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
	report, err := k.check(ctx, storyID)
	// A check that could not be made has nothing to report, and the error says
	// why; only a branch that was actually read has a verdict to print.
	if k.Out != nil && report.Branch != "" {
		fmt.Fprint(k.Out, report.String())
	}
	return report, err
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

	// The checks are mw next's own, run by the part of it that reads and never
	// writes, so that what a session is told here and what it is told at
	// close-out cannot come apart.
	var read NextReport
	report.Refusals = Next{
		Tracker: k.Tracker,
		Landing: k.Landing,
		Checks:  k.Checks,
		Remote:  k.Remote,
	}.refusals(ctx, c, &read, true)
	report.Commits, report.Notes = read.Commits, read.Notes

	if !report.Passed() {
		return report, fmt.Errorf("checking %s: %d check(s) failed, so mw next would not land it", storyID, len(report.Refusals))
	}
	return report, nil
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
