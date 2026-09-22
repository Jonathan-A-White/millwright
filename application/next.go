package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// The values a story's run state takes once the session that worked it has
// ended: landed and closed, stopped short of landing, or a session that went
// away without saying anything.
const (
	RunLanded  = "landed"
	RunBlocked = "blocked"
	RunStopped = "stopped"
	// RunStuck is recorded by `mw sweep` (a separate story) for a story claimed
	// here whose session has gone quiet without exiting. It is named here,
	// alongside the run states mw next itself writes, because `mw status`
	// reads it too: a story marked run=stopped or run=stuck is shown as that,
	// never as running.
	RunStuck = "stuck"
)

// RebaseState is the state dimension that says a story's branch was sent back
// to a fresh Builder session to rebase, after it would not merge into its
// target branch without conflicts. RebaseSentBack is its one value. It is on
// the story, not in a note, because it is what keeps the send-back to once: a
// story carrying it that conflicts again is stopped, never sent back again.
const (
	RebaseState    = "rebase"
	RebaseSentBack = "sent-back"
)

// Reason is the short, stable code a close-out that landed nothing is filed
// under, so that a person, `mw status` and the Mayor can tell a branch that needs
// fixing from a factory that does — the free text of the reason says what
// happened, and this says which kind of thing it was. The vocabulary is small and
// fixed on purpose: a code is written into the ledger, and a reader of an old
// ledger must find it still means what it meant.
type Reason string

// The reasons a close-out lands nothing. The first group is the branch itself
// turned away by the checks — the session's work to amend; the second is
// something else stopping the close-out before or during the landing — the
// factory's, or the other host's, to look at.
const (
	// The branch.
	ReasonNoCommits       Reason = "no-commits"
	ReasonUncommittedWork Reason = "uncommitted-work"
	ReasonSignedCommit    Reason = "signed-commit"
	ReasonOpenSteps       Reason = "open-steps"
	ReasonTestsFail       Reason = "tests-fail"
	ReasonMergeConflict   Reason = "merge-conflict"
	ReasonMergedTestsFail Reason = "merged-tests-fail"

	// The factory.
	ReasonNoResult      Reason = "no-result"
	ReasonSessionFailed Reason = "session-failed"
	ReasonTestsNotRun   Reason = "tests-not-run"
	ReasonGitFailed     Reason = "git-failed"
	ReasonTrackerFailed Reason = "tracker-failed"
	ReasonMergeSlot     Reason = "merge-slot"
	ReasonPushLost      Reason = "push-lost"
	ReasonLandingFailed Reason = "landing-failed"
)

// landingFailure is an error of the landing that knows which Reason it stops a
// close-out for. It is transparent to errors.Is and errors.As, so that
// Rejected and Conflicted see through it.
type landingFailure struct {
	reason Reason
	err    error
}

func (f landingFailure) Error() string { return f.err.Error() }
func (f landingFailure) Unwrap() error { return f.err }

// failedFor is err, marked as stopping a close-out for reason.
func failedFor(reason Reason, err error) error { return landingFailure{reason, err} }

// reasonOf is the Reason an error of the landing stops a close-out for: the one
// it was marked with, else ReasonLandingFailed.
func reasonOf(err error) Reason {
	var marked landingFailure
	if errors.As(err, &marked) {
		return marked.reason
	}
	return ReasonLandingFailed
}

// MayorMailbox is whose mailbox a close-out writes to, and the verdicts it
// starts the subject with: landed, refused by the checks, or blocked by
// something else that stopped it.
const (
	MayorMailbox = "mayor"

	MailLanded   = "Landed"
	MailRefused  = "Refused"
	MailBlocked  = "Blocked"
	MailSentBack = "Sent back"
)

// CheckLines is how much of a failed test run is written onto the story. Enough
// to see what broke, not so much that a bead becomes a log file.
const CheckLines = 40

// UncommittedShort is how many of the paths a session left uncommitted are
// named in the one-line reason a story is stopped for, which is what the ledger
// and the run state carry; UncommittedListed is how many the comment and the
// report list. Past either, the rest are counted, not named.
const (
	UncommittedShort  = 5
	UncommittedListed = 50
)

// Next closes out one finished story and carries the baton on. It is the use
// case that ends what dispatch began: the dispatch command line chains it after
// the harness exits, whatever the harness exited with, so that a session that
// died is closed out as truthfully as one that finished (ADR 0004).
//
// What it will and will not do is the whole point of it. It lands a story only
// when the session said it finished, the branch has commits, the formula's steps
// are closed and the rig's own tests pass — first in the story's worktree and
// again on the merged result whenever merging made something new. It never
// forces a push, never resolves a merge for anybody, and never closes a story it
// did not land. A branch that will not merge is sent back, once, to a fresh
// session of the seat to rebase; a second conflict stops there. Everything it
// refuses to do is written on the story and in the seat's ledger, so that a
// story that did not land says so in both places.
type Next struct {
	Tracker   WorkTracker
	Worktrees Worktrees
	Landing   Landing
	Checks    Checks
	Slot      MergeSlot
	Vault     Vault

	// AfterLanding runs the command a host names for a rig once a landing has
	// moved this host's checkout of it, so that a host rebuilds what it runs
	// from the rig by itself. A nil AfterLanding runs nothing.
	AfterLanding AfterLanding

	// Files is the vault as a git clone: what a close-out commits its own
	// ledger line and the session's rig memory through, so that the sync it
	// then runs is not stopped by the work it has just done. A nil Files leaves
	// them uncommitted, and the sync will say so.
	Files VaultFiles

	// Runner is how a story claimed here is told from a story really being
	// worked here: a claim with no session behind it is a session that went
	// away. A nil Runner skips that check.
	Runner Runner

	// Memory is where mw sweep remembers what it saw of a story's last session,
	// cleared when a close-out sends the story back to a fresh session of its
	// own (mw-gq6.86): that fresh session's pane starts blank too, and a note
	// the ended session left behind would otherwise be read as its silence,
	// since the old session's clock. A nil Memory clears nothing.
	Memory SweepNotes

	// Sync brings the hosts level after the story is closed and before anything
	// new is dispatched, so that the other host sees the closed story before it
	// picks its own next work. A nil Sync skips it.
	Sync HostSync

	// Dispatch is the baton: what is ready on this host becomes a running
	// session. A nil Dispatch closes the story out and stops there.
	Dispatch Dispatcher

	// Boot assembles the fresh session a branch that would not merge is sent
	// back to, to rebase, through Runner. One with no harness, or a nil Runner,
	// sends nothing back: a conflict stops the close-out.
	Boot SeatBoot

	// Seat is whose ledger the line is written in, Host is which host this is,
	// and Rigs is where each rig is checked out.
	Seat string
	Host string
	Rigs map[string]string

	// Remote is the remote the target branch is read from and pushed to. Empty
	// is DefaultRemote.
	Remote string

	// Tries is how many times the landing will fetch, merge and push again after
	// losing the race to the other host. Zero is MergeTries.
	Tries int

	// PushTries is how many times a push is tried again after it fails on a
	// fault at the remote itself, worth trying again — never a stated refusal,
	// and never the race with the other host, which Tries governs on its own.
	// Zero is DefaultPushTries. PushWait is how long it waits between those
	// tries; the zero value is no wait, so that a test never sleeps.
	PushTries int
	PushWait  time.Duration

	// Now is the clock the ledger line is dated by. The zero value reads the
	// real one.
	Now func() time.Time

	// Mailbox is how the Mayor is told what became of the story: one mail at the
	// end of a close-out that did something. A nil Mailbox sends none.
	Mailbox Mailbox

	// Out is where the report is printed. A nil Out prints nothing.
	Out io.Writer

	// Err is where what could not be done, and changes nothing about the outcome,
	// is said: a mail that could not be sent. A nil Err says nothing.
	Err io.Writer
}

// NextReport is what one close-out did.
type NextReport struct {
	StoryID string
	Title   string
	Host    string
	// Landed says the story's work is on its target branch at the remote, and
	// How is the way it got there. LandedEarlier says an earlier run of mw next
	// landed it and this one only closed it out, so How is empty and nothing
	// was merged, tested or pushed again.
	Landed        bool
	LandedEarlier bool
	How           Landed
	Target        string
	// Commits is how many commits the session left on the story's branch.
	Commits int
	// Uncommitted is the paths the session left changed and not committed in
	// its worktree, when it committed nothing: the work a person has to look at
	// before the worktree is thrown away. Empty when the worktree was clean or
	// the session did commit.
	Uncommitted []string
	// Pushes is how many times the push was attempted: more than one means the
	// other host landed something while this story was being landed.
	Pushes int
	// Rig is what became of this host's own checkout of the rig once the story
	// was landed: fast-forwarded onto the landed commit, or left as it was with
	// the reason. RigDir is where that checkout is. A landing made by an earlier
	// run says nothing about it.
	Rig    Advanced
	RigDir string
	// Why is the reason nothing landed, empty when something did, and Reason is
	// the code it is filed under. Refused says it is the branch itself the
	// checks turned away — no commits, a signed commit, open formula steps,
	// failing tests — rather than something that stopped the close-out before
	// or during the landing.
	Why     string
	Reason  Reason
	Refused bool
	// SentBack names the fresh session a branch that would not merge was sent
	// back to, to rebase onto the target branch, empty when it was not. A story
	// sent back is not landed and not stopped: it is being worked again, and
	// the mw next its session ends with lands it.
	SentBack string
	// Aside is the session this close-out ran in, renamed out of the way of the
	// one it sent the story back to. It is closed last, as a landed story's is.
	Aside string
	// NotClosed is what the tracker said when it would not close a story that
	// had landed, empty when the story was closed. A story with this set is
	// landed and still open, and mw next run again is what closes it.
	NotClosed string
	// Assignee is who the tracker says holds the story, as far as it said. It
	// is printed only when a landed story could not be closed, because the
	// usual reason for that is that somebody else holds the claim.
	Assignee string
	// Result is what the session reported, as far as it could be read.
	Result SessionResult
	// Ledger is the line that was appended, empty when none was.
	Ledger string
	// Committed is what was committed in the vault, empty when there was
	// nothing to commit.
	Committed []string
	Closed    bool
	// Abandoned names the stories claimed here whose session is not there any
	// more — claims a person or a later sweep has to settle.
	Abandoned []string
	// Notes are the things that went sideways without changing the outcome.
	Notes []string

	Synced     bool
	Sync       SyncReport
	Dispatched bool
	Dispatch   DispatchReport
}

// closeOut is the one story being closed out, as the steps of a close-out pass
// it between themselves.
type closeOut struct {
	id       string
	detail   StoryDetail
	path     domain.Path
	rigDir   string
	worktree string
	branch   string
	target   string
	result   SessionResult

	// landingError says this run kept the whole error of a failed landing in the
	// vault, so the close-out commits it.
	landingError bool
}

// attempt is which attempt of the story this close-out is closing: what the
// tracker recorded when the session it is closing was started, or the first
// when nothing was recorded at all. It is what a close-out reads and commits
// the run record under (mw-gq6.87): the same number a re-dispatch's own boot
// computed its file names from, so that a close-out always finds the result
// the attempt it is closing actually wrote.
func (c *closeOut) attempt() int {
	if c.detail.Attempts < 1 {
		return 1
	}
	return c.detail.Attempts
}

// Run closes out one story and reports what it did. The error it returns is the
// reason the story did not land; the report says what was recorded anyway,
// because a close-out that lands nothing still writes down what happened.
func (n Next) Run(ctx context.Context, storyID string) (NextReport, error) {
	report, err := n.closeOut(ctx, storyID)
	n.print(report.String())
	n.closeSession(ctx, report)
	return report, err
}

// closeSession takes away the runner session the story was worked in, once the
// story is landed and closed. It is the last thing Run does, after the report is
// printed, because mw next usually runs inside that very session: closing it
// hangs up this process, and nothing after it is sure to run. A story that was
// not closed keeps its session, exited, on screen — that is what it is kept for.
// A session that is already gone is no error, and a session that cannot be
// closed is said, not returned: the story is closed either way.
func (n Next) closeSession(ctx context.Context, report NextReport) {
	if n.Runner == nil {
		return
	}
	name := SessionName(report.StoryID)
	switch {
	case report.Aside != "":
		// Sent back: the story's name is the fresh session's now, and the one
		// this close-out ran in has been moved aside to be closed.
		name = report.Aside
	case !report.Closed:
		return
	}
	if err := n.Runner.Close(ctx, name); err != nil {
		n.print(fmt.Sprintf("  note    the session %s could not be closed: %v\n", name, err))
	}
}

// closeOut is Run without the printing.
func (n Next) closeOut(ctx context.Context, storyID string) (NextReport, error) {
	report := NextReport{StoryID: storyID, Host: n.Host}
	switch {
	case n.Tracker == nil || n.Vault == nil || n.Worktrees == nil || n.Landing == nil || n.Checks == nil || n.Slot == nil:
		return report, fmt.Errorf("closing out a story: it needs a work tracker, a vault, worktrees, a landing, the rig's checks and a merge slot")
	case n.Host == "":
		return report, fmt.Errorf("closing out a story: which host is this? set MW_HOST, or host in the config file")
	case n.Seat == "":
		return report, fmt.Errorf("closing out a story: which seat's ledger is the line written in?")
	case strings.TrimSpace(storyID) == "":
		return report, fmt.Errorf("closing out a story: which story?")
	}

	detail, err := n.Tracker.ShowStory(ctx, storyID)
	if err != nil {
		return report, fmt.Errorf("closing out %s: %w", storyID, err)
	}
	report.Title, report.Assignee = detail.Story.Title, detail.Assignee
	if detail.Closed() {
		return report, fmt.Errorf("closing out %s: it is closed already, so nothing was landed, ledgered or dispatched", storyID)
	}

	path, err := detail.Path()
	if err != nil {
		return report, fmt.Errorf("closing out %s: %w", storyID, err)
	}
	rigDir, checkedOut := n.Rigs[path.Rig]
	if !checkedOut {
		return report, fmt.Errorf("closing out %s: the rig %s is not checked out on %s: add it under [rigs] in the config file",
			storyID, path.Rig, n.Host)
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
	report.Target = c.target

	// A story this host has already landed, and could not close, is finished
	// with everything but the close. Running mw next again must close it —
	// without merging, testing, pushing or ledgering anything a second time.
	if was, err := n.Tracker.StoryState(ctx, storyID, RunState); err == nil && was == RunLanded {
		return n.closeALanding(ctx, c, &report)
	}
	// The run=landed write can fail after the push, and then the story carries
	// no marker. What a landing does leave is its ledger line, written before
	// the close was tried, and the branch merged into the target.
	if n.ledgeredAsLanded(ctx, c, &report) {
		return n.closeALanding(ctx, c, &report)
	}
	return n.land(ctx, c, &report)
}

// ledgeredAsLanded reports whether the seat's ledger holds this story's line as
// landed while its branch has nothing left to land: a landing is finished by
// removing the worktree and its branch, so a branch that is gone, or one with no
// commits the target lacks, is the branch merged. A branch that still has
// commits beyond the target is new work on a story landed once before, and is
// landed like any other. A ledger that cannot be read says no, with a note: the
// story is then treated as it was before this check existed.
func (n Next) ledgeredAsLanded(ctx context.Context, c *closeOut, report *NextReport) bool {
	lines, err := n.Vault.ReadLedger(ctx, n.Seat)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the %s seat's ledger could not be read to see whether %s landed already: %v", n.Seat, c.id, err))
		return false
	}
	landed := false
	for _, line := range lines {
		if LedgerLandsStory(line, c.id) {
			landed = true
			break
		}
	}
	if !landed {
		return false
	}
	ahead, err := n.Landing.Ahead(ctx, c.rigDir, c.branch, StartPoint(n.remote(), c.target))
	return err != nil || ahead == 0
}

// land takes one story from "its session has ended" to "it is on the target
// branch and closed", stopping at the first thing that says it should not be.
func (n Next) land(ctx context.Context, c *closeOut, report *NextReport) (NextReport, error) {
	// What the session itself reported. A session that wrote nothing is not a
	// session that succeeded quietly: it is one that died.
	resultName := ResultFileNameForAttempt(c.attempt())
	resultPath := n.Vault.RunFile(c.id, resultName)
	printed, err := n.Vault.ReadRunFile(ctx, c.id, resultName)
	if errors.Is(err, fs.ErrNotExist) {
		return n.stop(ctx, c, report, ReasonNoResult, fmt.Sprintf("the session left no result at %s, so it never started or it died before it could write one",
			resultPath), "")
	}
	if err != nil {
		return n.stop(ctx, c, report, ReasonNoResult, fmt.Sprintf("the session's result could not be read: %v", err), "")
	}
	if strings.TrimSpace(printed) == "" {
		why, said := n.emptyResult(ctx, c, resultName, resultPath)
		return n.stop(ctx, c, report, ReasonNoResult, why, said)
	}
	result, err := ReadSessionResult(printed)
	if err != nil {
		return n.stop(ctx, c, report, ReasonNoResult, fmt.Sprintf("the session's result could not be read: %v", err), "")
	}
	c.result, report.Result = result, result
	if !result.Finished() {
		return n.stop(ctx, c, report, ReasonSessionFailed, "the session did not finish: "+result.Trouble(), "")
	}

	// The target branch as the remote has it now is what the work is measured
	// against: the other host may have landed on it while this story was worked.
	if err := n.Worktrees.Fetch(ctx, c.rigDir); err != nil {
		return n.stop(ctx, c, report, ReasonGitFailed, fmt.Sprintf("the rig could not be brought up to date with %s: %v", n.remote(), err), "")
	}
	if found := n.refusals(ctx, c, report, false); len(found) > 0 {
		report.Refused = true
		return n.stop(ctx, c, report, found[0].Reason, found[0].Why, found[0].Said)
	}

	// Only one close-out at a time may touch a rig's target branch on this host.
	// The other host's races are settled by the remote itself, below.
	holding, err := n.Slot.Take(ctx, c.rigDir, Holders(n.Seat, n.Host, c.id))
	if err != nil {
		return n.stop(ctx, c, report, ReasonMergeSlot, fmt.Sprintf("the merge slot of %s could not be taken: %v", c.path.Rig, err), "")
	}
	landed, landErr := n.merge(ctx, c, report)
	if err := holding.Release(ctx); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the merge slot of %s could not be given back: %v", c.path.Rig, err))
	}
	if landErr != nil {
		kept := n.keepLandingError(ctx, c, report, landErr)
		if Conflicted(landErr) {
			whyNot := n.sendBack(ctx, c, report, landErr, kept)
			if whyNot == "" {
				return *report, nil
			}
			kept = "It was not sent back to rebase: " + whyNot + "\n\n" + kept
		}
		return n.stop(ctx, c, report, reasonOf(landErr), firstLine(landErr.Error()), kept)
	}
	report.Landed, report.How = true, landed
	n.advanceRig(ctx, c, report)

	// The work is on the target branch at the remote from here: nothing below
	// is undone, and nothing below stops the story being closed.
	outcome := fmt.Sprintf("%s, %d commits, mw next re-ran the rig's tests: pass", landed.LandedAs(c.target), report.Commits)

	// Written before anything else, because it is what tells a later run that
	// this story is landed. Everything after the push can fail — the close did,
	// on the run this was written for — and a run that cannot tell a landed
	// story from an unlanded one would merge it all over again.
	if err := n.Tracker.SetStoryState(ctx, c.id, RunState, RunLanded, outcome); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s could not be recorded as %s=%s: %v", c.id, RunState, RunLanded, err))
	}
	// After the story is recorded as landed, so that a command that is killed
	// with the session it runs in leaves a story a later run can tell is landed.
	n.afterLanding(ctx, c, report)
	return n.finish(ctx, c, report, outcome, false)
}

// PaneTailLines is how many of a session's last printed lines are shown
// beside an empty result: enough to see what it was doing when whatever wrote
// there stopped, not the whole pane.
const PaneTailLines = 40

// emptyResult is the reason and detail for a session whose result file is
// there but holds nothing — not ReasonNoResult's other case, a path that was
// never written at all, but one that exists and is blank. That is what a
// session's own finished result looks like once something else has replaced
// the path underneath the redirect that was still writing to it (mw-gq6.89:
// a completed session's real result never reached result.json because a git
// operation elsewhere in the vault touched the same path while the session's
// shell still held it open). So the bare word "empty" is not enough here: what
// mw next looked at is — the path itself, its size and when it was last
// touched, and the tail of what the session's pane last printed, when the
// session is still there to ask.
func (n Next) emptyResult(ctx context.Context, c *closeOut, resultName, resultPath string) (why, said string) {
	why = fmt.Sprintf("the session's result at %s is empty", resultPath)

	info, err := n.Vault.StatRunFile(ctx, c.id, resultName)
	if err != nil {
		said = fmt.Sprintf("%s could not be examined: %v", resultPath, err)
	} else {
		said = fmt.Sprintf("%s is %d byte(s), last modified %s", resultPath, info.Size, info.ModTime.UTC().Format(time.RFC3339))
	}
	if tail := n.paneTail(ctx, c); tail != "" {
		said += "\n\nThe last lines of the session's pane:\n\n```\n" + tail + "\n```"
	}
	return why, said
}

// paneTail is the tail of what a story's session last printed, empty when
// there is no runner to ask or no session left for it to answer about.
func (n Next) paneTail(ctx context.Context, c *closeOut) string {
	if n.Runner == nil {
		return ""
	}
	out, err := n.Runner.Output(ctx, SessionName(c.id), PaneTailLines)
	if err != nil {
		return ""
	}
	return out
}

// asideSuffix is added to the name of the session a close-out ran in when a
// story is sent back, so that the fresh session can take the story's own name.
const asideSuffix = "-sent-back"

// sendBack answers a branch that would not merge into its target branch without
// conflicts the one time it is allowed to: a fresh session of the seat, in the
// same worktree, told to rebase onto the target branch as the remote has it,
// resolve, run the suite and commit, and the mw next it ends with lands it as
// usual. The branch was never pushed, so the rebase forces nothing.
//
// Once only, and recorded on the story before anything is started, so that it
// cannot loop: a story already sent back, or one that cannot be recorded as
// sent back, is not sent. What it returns is why it was not sent, empty when it
// was; the close-out then stops as for any conflict.
func (n Next) sendBack(ctx context.Context, c *closeOut, report *NextReport, landErr error, kept string) string {
	if n.Runner == nil || n.Boot.Harness == nil {
		return "this mw next has no session to send it back to"
	}
	was, err := n.Tracker.StoryState(ctx, c.id, RebaseState)
	if err != nil {
		return fmt.Sprintf("whether it was sent back before could not be read: %v", err)
	}
	if was == RebaseSentBack {
		return "it was sent back once to rebase already, and a story is sent back only once. The conflict is a person's to resolve now"
	}

	onto := StartPoint(n.remote(), c.target)
	spec, err := n.Boot.Rebase(ctx, c.detail, c.worktree, onto)
	if err != nil {
		return fmt.Sprintf("the session to rebase it could not be assembled: %v", err)
	}
	if err := n.Tracker.SetStoryState(ctx, c.id, RebaseState, RebaseSentBack,
		fmt.Sprintf("sent back to rebase onto %s: %s", onto, firstLine(landErr.Error()))); err != nil {
		return fmt.Sprintf("it could not be recorded as %s=%s, which is what keeps it to once: %v", RebaseState, RebaseSentBack, err)
	}
	aside, err := n.makeWay(ctx, spec.Name)
	if err != nil {
		return fmt.Sprintf("the session %s could not be moved out of the way: %v", spec.Name, err)
	}
	if err := n.Runner.Start(ctx, spec); err != nil {
		// The session moved aside gets its name back, so that the story is
		// left with the session it was worked in, as any stopped story is.
		if aside != "" {
			if back := n.Runner.Rename(ctx, aside, spec.Name); back != nil {
				report.Notes = append(report.Notes, fmt.Sprintf("the session %s could not be given its name %s back: %v", aside, spec.Name, back))
			}
		}
		return fmt.Sprintf("the session %s to rebase it could not be started: %v", spec.Name, err)
	}
	report.Aside = aside

	// From here the fresh session is running and spending fuel: nothing below
	// undoes it, and what cannot be recorded is a note.
	if n.Memory != nil {
		if err := n.Memory.ClearNote(ctx, SweepKey(c.id)); err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("what mw sweep remembered of the ended session could not be cleared: %v", err))
		}
	}
	if err := recordAttempt(ctx, n.Tracker, c.detail); err != nil {
		report.Notes = append(report.Notes, err.Error())
	}
	report.SentBack, report.Reason, report.Why = spec.Name, ReasonMergeConflict, firstLine(landErr.Error())
	where := fmt.Sprintf("sent back to rebase by mw on %s: session %s in %s on %s, onto %s", n.Host, spec.Name, c.worktree, c.branch, onto)
	note := fmt.Sprintf("mw next on %s did not land this story (%s): %s does not merge into %s without conflicts, "+
		"because %s moved on while the story was worked.\n\n"+
		"It was sent back once to a fresh Builder session, %s, in the worktree %s, to rebase %s onto %s, resolve, "+
		"run the suite and commit. The mw next that session ends with lands it as usual. It is not sent back again: "+
		"a second conflict stops the close-out and is a person's to resolve. Nothing was merged, nothing was pushed "+
		"and nothing was forced.\n\n%s",
		n.Host, ReasonMergeConflict, c.branch, c.target, c.target, spec.Name, c.worktree, c.branch, onto, kept)
	if err := n.Tracker.CommentOnStory(ctx, c.id, note); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the send-back could not be written on the story: %v", err))
	}
	if err := n.Tracker.SetStoryState(ctx, c.id, RunState, RunRunning, where); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s could not be recorded as %s=%s: %v", c.id, RunState, RunRunning, err))
	}
	// The line charges the session that worked the story, whose result the
	// fresh one will write over; the fresh one is charged by its own line.
	if err := n.ledger(ctx, c, report, NotLanded(ReasonMergeConflict, "sent back to rebase onto "+onto+" in session "+spec.Name)); err != nil {
		report.Notes = append(report.Notes, err.Error())
	}
	n.commit(ctx, c, report)
	n.mailTheMayor(ctx, c, report, MailSentBack, note)
	return ""
}

// makeWay frees the story's session name for the session it is sent back to.
// A session still running under it is the one this close-out is running in,
// chained on after the harness, and is renamed rather than closed, because
// closing it would end this very process; one that has ended is closed. It
// reports the name a renamed session now has.
func (n Next) makeWay(ctx context.Context, name string) (string, error) {
	status, err := n.Runner.Status(ctx, name)
	if err != nil {
		return "", err
	}
	switch status.State {
	case StateRunning:
		aside := SessionName(name + asideSuffix)
		return aside, n.Runner.Rename(ctx, name, aside)
	case StateExited, StateExitUnknown:
		return "", n.Runner.Close(ctx, name)
	}
	return "", nil
}

// advanceRig brings this host's own checkout of the rig up to the commit just
// pushed, which the landing — made in a worktree of its own — never touched: a
// rig left behind is a rig whose rebuilt binary is the old one. A checkout that
// is not on the target branch or not clean is left alone and the report says so.
// It changes nothing about the landing, so a git that fails is only a note.
func (n Next) advanceRig(ctx context.Context, c *closeOut, report *NextReport) {
	advanced, err := n.Landing.Advance(ctx, c.rigDir, c.target, report.How.Commit)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the rig checkout %s could not be brought up to %s: %v", c.rigDir, c.target, err))
		return
	}
	report.Rig, report.RigDir = advanced, c.rigDir
}

// keepLandingError writes the whole error of a failed landing beside the run
// and returns what the story's comment quotes: the same text, in full. The
// ledger and the run state take only its first line, so this file is what says
// afterwards whether the remote refused the push or hung up. A file that cannot
// be written is a note, not a reason to say less.
func (n Next) keepLandingError(ctx context.Context, c *closeOut, report *NextReport, landErr error) string {
	text := landErr.Error()
	kept := "The whole error, as git said it:\n\n" + fenced(text)
	if _, err := n.Vault.PutRunFile(ctx, c.id, LandingErrorFileName, text+"\n"); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the whole error of the landing could not be kept in the run of %s: %v", c.id, err))
		return kept
	}
	c.landingError = true
	return kept + "\n\nIt is also kept at " + n.Vault.RunFile(c.id, LandingErrorFileName) + "."
}

// fenced is text in a code block whose fence is longer than any run of
// backticks inside it, so that an error which quotes a block of its own does not
// end this one early.
func fenced(text string) string {
	longest, run := 0, 0
	for _, r := range text {
		if r == '`' {
			run++
			longest = max(longest, run)
			continue
		}
		run = 0
	}
	fence := strings.Repeat("`", max(3, longest+1))
	return fence + "\n" + text + "\n" + fence
}

// Refusal is one reason a branch is not landed: the line that says it, and the
// detail a person needs to act on it — which steps are open, which commit is
// signed, what the tests said. It is what a close-out writes on the story and
// what mw check prints, worded once. Reason is the code it is filed under.
type Refusal struct {
	Reason    Reason
	Why, Said string
}

// refusals reads the branch a session left against everything mw next asks of
// it before landing, in the order it asks: commits, whom they are signed by,
// the formula's steps and the rig's own tests in the worktree. A branch with no
// commits is refused there and then, because there is nothing to sign, close or
// test. With all false it stops at the first refusal, as a close-out does,
// because a commit message is permanent and the tests are the slow part; with
// all true it goes on to the rest, which is what a session checking its own
// branch wants to be told at once. It reads and never writes: all it changes
// is the report it fills in.
func (n Next) refusals(ctx context.Context, c *closeOut, report *NextReport, all bool) []Refusal {
	var found []Refusal
	// refuse notes one refusal and says whether to go no further.
	refuse := func(reason Reason, why, said string) bool {
		found = append(found, Refusal{reason, why, said})
		return !all
	}

	// What the session left on the branch, measured against the target branch
	// as this rig last saw the remote's.
	base := StartPoint(n.remote(), c.target)
	commits, err := n.Landing.Ahead(ctx, c.rigDir, c.branch, base)
	if err != nil {
		refuse(ReasonGitFailed, fmt.Sprintf("the commits on %s could not be counted: %v", c.branch, err), "")
		return found
	}
	report.Commits = commits
	if commits == 0 {
		reason, why, said := n.nothingCommitted(ctx, c, report)
		refuse(reason, why, said)
		return found
	}

	// What the commits say about who wrote them. This is asked before the
	// formula, before the tests and before the merge slot, because a commit
	// message is the one thing a landing makes permanent and cannot take back:
	// once it is on the target branch at the remote, only a force-push would
	// undo it, and mw forces nothing. Refused here, the branch is still the
	// session's to amend.
	if stopped, reason, why, said := n.signedByAMachine(ctx, c); stopped && refuse(reason, why, said) {
		return found
	}

	// What the formula says the session was to do. A step still open is a step
	// the session did not do, whatever the code looks like.
	if root := c.detail.Molecule.RootID; root != "" {
		open, err := n.Tracker.OpenSteps(ctx, root)
		switch {
		case err != nil:
			if refuse(ReasonTrackerFailed, fmt.Sprintf("the steps of the formula poured as %s could not be read: %v", root, err), "") {
				return found
			}
		case len(open) > 0:
			if refuse(ReasonOpenSteps, fmt.Sprintf("%d formula step(s) of %s are still open, so the formula was not finished", len(open), root),
				"Still open:\n"+stepList(open)) {
				return found
			}
		}
	}

	// What the rig itself says about the work, in the worktree the session left.
	checked, err := n.Checks.Run(ctx, c.path.Rig, c.worktree)
	switch {
	case err != nil:
		refuse(ReasonTestsNotRun, fmt.Sprintf("the rig's tests could not be run in %s: %v", c.worktree, err), "")
	case checked.NotRun:
		refuse(ReasonTestsNotRun, fmt.Sprintf("the rig's tests could not be run in the worktree: `%s` did not start, so this host is missing something the command needs (a toolchain not on its PATH?)", checked.Command),
			"The last lines of `"+checked.Command+"` in "+c.worktree+":\n\n```\n"+checked.Tail(CheckLines)+"\n```")
	case !checked.Passed:
		refuse(ReasonTestsFail, fmt.Sprintf("the rig's tests fail in the worktree: `%s` did not pass", checked.Command),
			"The last lines of `"+checked.Command+"` in "+c.worktree+":\n\n```\n"+checked.Tail(CheckLines)+"\n```")
	}
	return found
}

// nothingCommitted is the refusal of a branch with no commits on it. A
// session that did nothing and a session that did the work and never committed
// it look alike on the branch and nothing like each other in the worktree — the
// headless session ends when its turn does, whatever it left running — so the
// worktree is read too, and what is in it is named: the person told "committed
// nothing" would otherwise throw the worktree away.
func (n Next) nothingCommitted(ctx context.Context, c *closeOut, report *NextReport) (reason Reason, why, said string) {
	plain := fmt.Sprintf("the session committed nothing to %s, so there is nothing to land on %s", c.branch, c.target)

	left, err := n.Landing.Uncommitted(ctx, c.worktree)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the worktree %s could not be read for uncommitted work: %v", c.worktree, err))
		return ReasonNoCommits, plain, ""
	}
	if len(left) == 0 {
		return ReasonNoCommits, plain, ""
	}
	report.Uncommitted = left
	return ReasonUncommittedWork, fmt.Sprintf("the session committed nothing to %s but left uncommitted work in its worktree (%s), so there is nothing to land on %s",
			c.branch, pathList(left, UncommittedShort), c.target),
		fmt.Sprintf("Uncommitted work left in %s (%d path(s)):\n%s", c.worktree, len(left), bullets(left, UncommittedListed))
}

// pathList is up to limit paths on one line, and how many more there were.
func pathList(paths []string, limit int) string {
	if len(paths) <= limit {
		return strings.Join(paths, ", ")
	}
	return fmt.Sprintf("%s, and %d more", strings.Join(paths[:limit], ", "), len(paths)-limit)
}

// bullets is up to limit paths one to a line, and how many more there were.
func bullets(paths []string, limit int) string {
	shown := paths
	if len(shown) > limit {
		shown = shown[:limit]
	}
	lines := make([]string, 0, len(shown)+1)
	for _, path := range shown {
		lines = append(lines, "- "+path)
	}
	if len(paths) > limit {
		lines = append(lines, fmt.Sprintf("- and %d more", len(paths)-limit))
	}
	return strings.Join(lines, "\n")
}

// signedByAMachine reads the commits a landing would put on the target branch
// and reports whether any of them is signed as a machine's work — the reason to
// stop, and the detail for the story's comment. A branch whose commits could
// not be read is refused too: a close-out that cannot tell lands nothing.
//
// The rule itself is AIAttribution, and it is applied to every commit the
// landing would add, not only the newest: the target branch takes all of them.
func (n Next) signedByAMachine(ctx context.Context, c *closeOut) (bool, Reason, string, string) {
	commits, err := n.Landing.Commits(ctx, c.rigDir, c.branch, StartPoint(n.remote(), c.target))
	if err != nil {
		return true, ReasonGitFailed, fmt.Sprintf("the commit messages on %s could not be read: %v", c.branch, err), ""
	}

	var carrying []string
	first, signature := Commit{}, ""
	for _, commit := range commits {
		line := AIAttribution(commit.Message)
		if line == "" {
			continue
		}
		if signature == "" {
			first, signature = commit, line
		}
		carrying = append(carrying, "- "+commit.Hash+" · "+firstLine(commit.Message)+"\n  "+line)
	}
	if signature == "" {
		return false, "", "", ""
	}

	why := fmt.Sprintf("commit %s on %s is signed as a machine's work, which this factory's commits never are: %s",
		first.Hash, c.branch, signature)
	said := fmt.Sprintf("The commit(s) carrying it:\n\n%s\n\nA seat outlives every session that occupies it, "+
		"so the seat signs the work and the model never does. Nothing was merged: the branch is still the "+
		"session's to amend. Reword the message(s) — `git rebase -i %s` or `git commit --amend` for the "+
		"newest — and run `mw next %s` again.",
		strings.Join(carrying, "\n"), StartPoint(n.remote(), c.target), c.id)
	return true, ReasonSignedCommit, why, said
}

// closeALanding is a close-out run again on a story an earlier one landed and
// could not close. Nothing is read from the session, nothing is merged, tested
// or pushed: the work is on the target branch already, and the only things left
// are the ones that come after a landing.
func (n Next) closeALanding(ctx context.Context, c *closeOut, report *NextReport) (NextReport, error) {
	report.Landed, report.LandedEarlier = true, true
	outcome := fmt.Sprintf("landed on %s by an earlier mw next on %s; closed by a later run", c.target, n.Host)
	return n.finish(ctx, c, report, outcome, true)
}

// finish is everything that follows a landing: the worktree taken away, one
// line in the seat's ledger, the story closed, and the baton carried on. It is
// run by the close-out that landed the story and, when that one could not close
// it, again by the next — so every step of it can be run twice. The worktree
// port takes removing what is not there; the ledger is append-only, so a line
// already written is the one that stands, and `again` is what asks.
func (n Next) finish(ctx context.Context, c *closeOut, report *NextReport, outcome string, again bool) (NextReport, error) {
	if err := n.Worktrees.Remove(ctx, c.rigDir, c.worktree, c.branch); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the worktree %s could not be taken away: %v", c.worktree, err))
	}

	written := false
	if again {
		held, err := n.ledgered(ctx, c.id)
		if err != nil {
			report.Notes = append(report.Notes, err.Error())
		}
		written = held
	}
	if !written {
		if err := n.ledger(ctx, c, report, outcome); err != nil {
			report.Notes = append(report.Notes, err.Error())
		}
	}
	// Committed whether this run wrote the line or an earlier one did: a line
	// written by a run that could not commit it is still standing in the way of
	// every sync until somebody commits it, and this is the run that can.
	n.commit(ctx, c, report)

	closeErr := n.Tracker.CloseStory(ctx, c.id, outcome)
	if closeErr != nil {
		report.NotClosed = closeErr.Error()
	} else {
		report.Closed = true
	}
	// Told once: the run that landed the story says so, and a later run that
	// only closes it has nothing new for the Mayor.
	if !again {
		n.mailTheMayor(ctx, c, report, MailLanded, "")
	}
	if closeErr != nil {
		return *report, fmt.Errorf("closing out %s: it %s, but the story could not be closed, so it is landed and still open: %w",
			c.id, outcome, closeErr)
	}

	report.Abandoned = n.abandoned(ctx, c.id, report)
	return n.carryOn(ctx, c, report)
}

// ledgered reports whether the seat's ledger already holds this story's line.
// The ledger is append-only and read for nothing but a report, so this is the
// one thing a re-run asks it: a line the run that landed the story wrote is the
// line that stands, and the run that closes it adds none.
func (n Next) ledgered(ctx context.Context, id string) (bool, error) {
	lines, err := n.Vault.ReadLedger(ctx, n.Seat)
	if err != nil {
		return false, fmt.Errorf("the %s seat's ledger could not be read to see whether %s is in it already: %v", n.Seat, id, err)
	}
	for _, line := range lines {
		if LedgerNamesStory(line, id) {
			return true, nil
		}
	}
	return false, nil
}

// merge is the landing itself, under the merge slot: fetch, merge into the
// target branch as the remote has it, test whatever merging made, and push. A
// push the remote refuses because the branch moved is a race with the other
// host, and the whole thing is done again from its newest commit — a bounded
// number of times, and never forced. A push that fails on a fault at the
// remote itself, worth trying again, is retried in place by push, below,
// without a fresh fetch or merge, since nothing upstream has changed; a push
// the remote refuses for a stated reason is landing-failed at once, exactly as
// before either kind of retry existed.
func (n Next) merge(ctx context.Context, c *closeOut, report *NextReport) (Landed, error) {
	base := StartPoint(n.remote(), c.target)
	var lost error
	for try := 1; try <= n.tries(); try++ {
		if try > 1 {
			if err := n.Worktrees.Fetch(ctx, c.rigDir); err != nil {
				return Landed{}, failedFor(ReasonGitFailed, fmt.Errorf("the rig could not be brought up to date with %s: %w", n.remote(), err))
			}
		}
		dir, err := n.Landing.OpenLanding(ctx, c.rigDir, base)
		if err != nil {
			return Landed{}, failedFor(ReasonLandingFailed, fmt.Errorf("a landing of %s at %s could not be opened: %w", c.path.Rig, base, err))
		}

		landed, err := n.push(ctx, c, report, dir)
		if closeErr := n.Landing.CloseLanding(ctx, c.rigDir, dir); closeErr != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("the landing worktree %s could not be taken away: %v", dir, closeErr))
		}
		switch {
		case err == nil:
			return landed, nil
		case Rejected(err):
			lost = err
			continue
		default:
			return Landed{}, err
		}
	}
	return Landed{}, failedFor(ReasonPushLost, fmt.Errorf("%s could not be landed on %s: the other host got there first %d times running; nothing was forced: %w",
		c.branch, c.target, n.tries(), lost))
}

// push is one attempt at a landing, in a landing worktree that is already open:
// merge, test what merging made if it made anything new, and push.
func (n Next) push(ctx context.Context, c *closeOut, report *NextReport, dir string) (Landed, error) {
	landed, err := n.Landing.Merge(ctx, dir, c.branch)
	if err != nil {
		if Conflicted(err) {
			return Landed{}, failedFor(ReasonMergeConflict, fmt.Errorf("%s does not merge into %s without conflicts, which mw will not resolve for anybody: %w",
				c.branch, c.target, err))
		}
		return Landed{}, failedFor(ReasonLandingFailed, fmt.Errorf("%s could not be merged into %s: %w", c.branch, c.target, err))
	}

	// A merge commit is a combination of two branches that nothing has ever been
	// tested on: the story's tests passed on the story's branch, and the other
	// host's passed on its own. Only this result matters now.
	if !landed.FastForward {
		checked, err := n.Checks.Run(ctx, c.path.Rig, dir)
		if err != nil {
			return Landed{}, failedFor(ReasonTestsNotRun, fmt.Errorf("the rig's tests could not be run on the merged result in %s: %w", dir, err))
		}
		if checked.NotRun {
			return Landed{}, failedFor(ReasonTestsNotRun, fmt.Errorf("the rig's tests could not be run on the merged result in %s: `%s` did not start, so nothing was pushed\n\n```\n%s\n```",
				dir, checked.Command, checked.Tail(CheckLines)))
		}
		if !checked.Passed {
			return Landed{}, failedFor(ReasonMergedTestsFail, fmt.Errorf("%s and %s do not pass the rig's tests together: `%s` failed on the merged result, so nothing was pushed\n\n```\n%s\n```",
				c.branch, c.target, checked.Command, checked.Tail(CheckLines)))
		}
	}

	if err := n.pushRetrying(ctx, c, report, dir); err != nil {
		return Landed{}, err
	}
	return landed, nil
}

// pushRetrying pushes what the landing worktree has checked out, and tries
// again after a wait when the push itself failed on a fault at the remote
// worth trying again, up to PushTries times in all. It never retries a push
// the remote refused for a stated reason, or the race with the other host —
// Rejected — which merge's own loop answers by fetching and merging afresh;
// either comes back as it was, for merge to tell apart. Every attempt is
// counted on the report, whichever kind of failure eventually stops it.
func (n Next) pushRetrying(ctx context.Context, c *closeOut, report *NextReport, dir string) error {
	var last error
	for try := 1; try <= n.pushTries(); try++ {
		if try > 1 {
			if err := waitFor(ctx, n.PushWait); err != nil {
				return failedFor(ReasonLandingFailed, fmt.Errorf("waiting %s to try the push to %s again: %w", n.PushWait, c.target, err))
			}
		}
		report.Pushes++
		err := n.Landing.Push(ctx, dir, n.remote(), c.target)
		if err == nil {
			return nil
		}
		if !Transient(err) {
			return err
		}
		last = err
	}
	return failedFor(ReasonLandingFailed, fmt.Errorf("the push to %s kept failing on a fault at the remote, %d time(s) running: %w",
		c.target, n.pushTries(), last))
}

// carryOn brings the hosts level and dispatches whatever is ready now. It runs
// only once the story is closed, so that the other host sees a closed story
// rather than a claimed one, and so that what is dispatched next accounts for
// the story that has just finished.
func (n Next) carryOn(ctx context.Context, c *closeOut, report *NextReport) (NextReport, error) {
	if n.Sync != nil {
		synced, err := n.Sync.Run(ctx)
		if err != nil {
			return *report, fmt.Errorf("closing out %s: it landed and was closed, but the hosts could not be brought level, so nothing was dispatched: %w", c.id, err)
		}
		report.Sync, report.Synced = synced, true
	}
	if n.Dispatch == nil {
		return *report, nil
	}
	dispatched, err := n.Dispatch.Run(ctx)
	report.Dispatch, report.Dispatched = dispatched, true
	if err != nil {
		return *report, fmt.Errorf("closing out %s: it landed and was closed, but the next dispatch failed: %w", c.id, err)
	}
	return *report, nil
}

// stop records a close-out that landed nothing: the reason, and the code it is
// filed under, on the story; the story marked blocked so that nobody takes it
// for work in flight; and one line in the ledger saying it did not land, its
// outcome starting with the code. Nothing is merged, nothing is pushed, nothing
// is closed and nothing is given back — the worktree and the branch are left
// exactly as the session left them, because they are the evidence.
func (n Next) stop(ctx context.Context, c *closeOut, report *NextReport, reason Reason, why, said string) (NextReport, error) {
	report.Why, report.Reason = why, reason

	note := fmt.Sprintf("mw next on %s "+RefusedPhrase+"%s): %s\n\n"+
		"Nothing was merged and nothing was pushed. The story is not closed and the claim was not given back. "+
		"The worktree %s and the branch %s are left as the session left them.",
		n.Host, reason, why, c.worktree, c.branch)
	if said != "" {
		note += "\n\n" + said
	}

	var trouble []string
	if err := n.Tracker.CommentOnStory(ctx, c.id, note); err != nil {
		trouble = append(trouble, fmt.Sprintf("the reason could not be written on the story: %v", err))
	}
	if err := n.Tracker.SetStoryState(ctx, c.id, RunState, RunBlocked, "("+string(reason)+") "+why); err != nil {
		trouble = append(trouble, fmt.Sprintf("%s could not be recorded as %s=%s: %v", c.id, RunState, RunBlocked, err))
	}
	if err := n.ledger(ctx, c, report, NotLanded(reason, firstLine(why))); err != nil {
		trouble = append(trouble, err.Error())
	}
	// A close-out that lands nothing syncs nothing either, but the line it has
	// just written is in the vault all the same: left uncommitted it would stop
	// the next sync, whoever runs it, for a story that never landed.
	n.commit(ctx, c, report)

	verdict := MailBlocked
	if report.Refused {
		verdict = MailRefused
	}
	n.mailTheMayor(ctx, c, report, verdict, said)

	err := fmt.Errorf("closing out %s: %s", c.id, why)
	if len(trouble) > 0 {
		report.Notes = append(report.Notes, trouble...)
		err = fmt.Errorf("%w (and %s)", err, strings.Join(trouble, "; "))
	}
	return *report, err
}

// mailTheMayor sends the one mail that says what became of the story: from mw
// on this host, to the Mayor, titled by the verdict and the story, and holding
// the report mw next prints — the commit and the fuel line of a landing, the
// reason and its detail of a story that landed nothing. It runs once the
// outcome is settled and before the hosts are synced, so that the mail travels
// with the sync that follows.
//
// A mail that cannot be sent is said on stderr and changes nothing: the story
// is landed, or stopped, exactly as it was, and the Mayor still has the rig log
// and the ledger to read.
func (n Next) mailTheMayor(ctx context.Context, c *closeOut, report *NextReport, verdict, said string) {
	if n.Mailbox == nil {
		return
	}
	title := strings.Join(strings.Fields(c.detail.Story.Title), " ")
	if title == "" {
		title = c.id
	}
	body := report.String()
	if said != "" {
		body += "\n" + said + "\n"
	}
	if _, err := n.Mailbox.Send(ctx, NewMessage{
		From:    SeatIdentity(MwSeat, n.Host),
		To:      MayorMailbox,
		Subject: verdict + ": " + title,
		Body:    body,
	}); err != nil && n.Err != nil {
		fmt.Fprintf(n.Err, "mw next: the mail to %s about %s (%s) could not be sent: %v\n",
			MayorMailbox, c.id, strings.ToLower(verdict), err)
	}
}

// ledger appends this story's one line to the seat's ledger. A session's fuel is
// charged by one line only: when the ledger already holds the line that charged
// this session — a close-out run again after a refusal — this line records its
// own outcome and says the fuel was charged above.
func (n Next) ledger(ctx context.Context, c *closeOut, report *NextReport, outcome string) error {
	charged, err := n.charged(ctx, c.result.SessionID)
	if err != nil {
		report.Notes = append(report.Notes, err.Error())
	}
	line := LedgerLine{
		When:        n.now(),
		StoryID:     c.id,
		Title:       c.detail.Story.Title,
		Outcome:     outcome,
		Path:        c.path,
		Result:      c.result,
		Notes:       n.ledgerNotes(c, report),
		FuelCharged: charged,
	}.String()

	if err := n.Vault.AppendToLedger(ctx, n.Seat, line); err != nil {
		return fmt.Errorf("the line for %s could not be appended to the %s seat's ledger: %v", c.id, n.Seat, err)
	}
	report.Ledger = line
	return nil
}

// charged reports whether the seat's ledger already holds the line that
// counted this session's fuel. A ledger that cannot be read says no, with the
// reason: a line that counts fuel twice can be seen and put right, and a line
// that is not written cannot.
func (n Next) charged(ctx context.Context, sessionID string) (bool, error) {
	if sessionID == "" {
		return false, nil
	}
	lines, err := n.Vault.ReadLedger(ctx, n.Seat)
	if err != nil {
		return false, fmt.Errorf("the %s seat's ledger could not be read to see whether session %s was charged already, so its fuel is counted again: %v",
			n.Seat, sessionID, err)
	}
	for _, line := range lines {
		if LedgerChargesSession(line, sessionID) {
			return true, nil
		}
	}
	return false, nil
}

// commit records in the vault exactly what this story was allowed to write
// there: the line mw next has just appended to the seat's ledger, the session's
// memory of the rig it worked, and the session's result — the run record, which
// is the evidence behind the ledger line's fuel. Nothing else, and never the
// boot file beside the result — by explicit path, never everything that happens
// to be lying about — because a vault holds the work of two hosts and several
// seats, and a close-out is only entitled to its own.
//
// The whole error of a landing that failed, when this run kept one, is
// committed beside it by explicit path, so that what git said survives the
// worktree and reaches the other host.
//
// A run record that is not there is a note and no more: the rest is committed
// all the same, and the report says which file was missing.
//
// A commit that cannot be made is a note, not a failure: the story landed, and
// the sync that follows will name the files and refuse, which is exactly what
// it does for anybody else's uncommitted work.
func (n Next) commit(ctx context.Context, c *closeOut, report *NextReport) {
	if n.Files == nil {
		return
	}
	paths := SeatWork(n.Seat, c.path.Rig)
	if len(paths) == 0 {
		return
	}
	if record := RunRecordForAttempt(c.id, c.attempt()); record != "" {
		if _, err := n.Vault.ReadRunFile(ctx, c.id, ResultFileNameForAttempt(c.attempt())); errors.Is(err, fs.ErrNotExist) {
			report.Notes = append(report.Notes, fmt.Sprintf("the run record %s is missing from the vault, so it was not committed", record))
		} else {
			paths = append(paths, record)
		}
	}

	if c.landingError {
		if record := LandingErrorRecord(c.id); record != "" {
			paths = append(paths, record)
		}
	}

	committed, err := n.Files.Commit(ctx, VaultCommitMessage(c.id, c.detail.Story.Title), paths)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s could not be committed in the vault, so the next sync will refuse until somebody does: %v",
			strings.Join(paths, " and "), err))
		return
	}
	report.Committed = append(report.Committed, committed...)
}

// VaultCommitMessage is the message a close-out commits the vault under: which
// story the work belongs to, in one plain line, signed by nobody. A seat
// outlives every session that occupies it, so nothing here is signed by a model
// — and mw's own commits hold to the same rule mw next holds a branch to.
func VaultCommitMessage(storyID, title string) string {
	said := strings.Join(strings.Fields(title), " ")
	if said == "" {
		return "Close out " + storyID
	}
	return fmt.Sprintf("Close out %s: %s", storyID, said)
}

// ledgerNotes is the last column of the ledger line: who ran the story and
// anything a person reading the line later would want to know.
func (n Next) ledgerNotes(c *closeOut, report *NextReport) []string {
	notes := []string{"dispatched by mw on " + n.Host}
	if id := c.result.SessionID; id != "" {
		notes = append(notes, "session "+id)
	}
	if report.Pushes > 1 {
		notes = append(notes, fmt.Sprintf("pushed %d times: the other host landed first", report.Pushes))
	}
	if c.result.Denials > 0 {
		notes = append(notes, fmt.Sprintf("%d permission denial(s) during the session", c.result.Denials))
	}
	return append(notes, report.Notes...)
}

// recordedGone reports whether id's run state already says something about its
// session that a close-out or sweep has written: set by mw next (RunStopped)
// once a close-out found it gone, by mw sweep (RunStuck) once a sweep did, or by
// mw next (RunBlocked) once a landing was refused. A blocked story's session did
// finish; it is not abandoned, and its reason is in the state the refusal wrote,
// which a sweep must not write over. It is the guard both share, so that whichever
// of them notices a claimed story's session is gone first is the one that
// comments, and the other says nothing again.
func recordedGone(ctx context.Context, tracker WorkTracker, id string) bool {
	was, err := tracker.StoryState(ctx, id, RunState)
	return err == nil && (was == RunStopped || was == RunStuck || was == RunBlocked)
}

// abandoned finds the stories this host has claimed whose session is not there
// any anymore, and says so on each of them. The claim is left alone on purpose:
// the story's worktree holds work nobody has looked at, and a claim given back
// is a worktree the next dispatch would take away. Somebody has to decide, and
// this is how they are told there is something to decide.
func (n Next) abandoned(ctx context.Context, skip string, report *NextReport) []string {
	if n.Runner == nil {
		return nil
	}
	running, err := n.Tracker.RunningStories(ctx, n.Host)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("what else is running here could not be read: %v", err))
		return nil
	}

	var gone []string
	for _, detail := range running {
		id := detail.Story.ID
		if id == skip {
			continue
		}
		status, err := n.Runner.Status(ctx, SessionName(id))
		if err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("the session of %s could not be asked about: %v", id, err))
			continue
		}
		if status.Running() {
			continue
		}
		gone = append(gone, id)

		// Said once. A story a sweep already found stuck, or that an earlier
		// close-out already found stopped, has been reported before, and a
		// comment on every close-out would bury the first one.
		if recordedGone(ctx, n.Tracker, id) {
			continue
		}
		why := fmt.Sprintf("mw next on %s found this story claimed here with no session behind it: %s is %s. "+
			"The session ended without closing the story out. The claim was left alone, because the worktree may hold work "+
			"nobody has looked at yet, and giving the claim back is how that work gets taken away.",
			n.Host, SessionName(id), status.State)
		if err := n.Tracker.CommentOnStory(ctx, id, why); err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("the abandoned session of %s could not be written on it: %v", id, err))
		}
		if err := n.Tracker.SetStoryState(ctx, id, RunState, RunStopped, why); err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("%s could not be recorded as %s=%s: %v", id, RunState, RunStopped, err))
		}
	}
	return gone
}

// remote is the remote the target branch is read from and pushed to.
func (n Next) remote() string {
	if n.Remote == "" {
		return DefaultRemote
	}
	return n.Remote
}

// tries is how many times a landing will try again after losing the race.
func (n Next) tries() int {
	if n.Tries < 1 {
		return MergeTries
	}
	return n.Tries
}

// DefaultPushTries is how many times a push is tried when nothing says
// otherwise: infrastructure/config.DefaultPushTries is the same number.
const DefaultPushTries = 3

// pushTries is how many times a push will try again after failing on a fault
// at the remote worth trying again.
func (n Next) pushTries() int {
	if n.PushTries < 1 {
		return DefaultPushTries
	}
	return n.PushTries
}

// now is the clock the ledger line is dated by.
func (n Next) now() time.Time {
	if n.Now == nil {
		return time.Now()
	}
	return n.Now()
}

// print writes the report, when there is somewhere to write it.
func (n Next) print(text string) {
	if n.Out == nil {
		return
	}
	fmt.Fprint(n.Out, text)
}

// String is the close-out as a person reads it.
func (r NextReport) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "next on %s: %s\n", r.Host, r.StoryID)
	switch {
	case r.LandedEarlier:
		fmt.Fprintf(&b, "  landed  on %s by an earlier run of mw next: nothing was merged, tested or pushed again\n", r.Target)
	case r.Landed:
		fmt.Fprintf(&b, "  landed  %s on %s (%d commits, %d push(es))\n", r.How.LandedAs(r.Target), r.Target, r.Commits, r.Pushes)
	case r.SentBack != "":
		fmt.Fprintf(&b, "  SENT BACK (%s) %s\n", r.Reason, r.Why)
		fmt.Fprintf(&b, "          to %s, a fresh session rebasing onto %s; sent back once, never again\n", r.SentBack, r.Target)
	default:
		if r.Reason == "" {
			fmt.Fprintf(&b, "  STOPPED %s\n", r.Why)
		} else {
			fmt.Fprintf(&b, "  STOPPED (%s) %s\n", r.Reason, r.Why)
		}
	}
	switch {
	case r.Rig.Moved:
		fmt.Fprintf(&b, "  rig     %s fast-forwarded to %s on %s\n", r.RigDir, shortCommit(r.How.Commit), r.Target)
	case r.Rig.Left != "":
		fmt.Fprintf(&b, "  rig     %s left as it was, not brought up to %s: %s\n", r.RigDir, r.Target, r.Rig.Left)
	}
	if len(r.Uncommitted) > 0 {
		fmt.Fprintf(&b, "  left    uncommitted work, %d path(s), in the worktree:\n", len(r.Uncommitted))
		for _, line := range strings.Split(bullets(r.Uncommitted, UncommittedListed), "\n") {
			fmt.Fprintf(&b, "            %s\n", line)
		}
	}
	if r.Ledger != "" {
		fmt.Fprintf(&b, "  ledger  %s\n", r.Ledger)
	}
	if len(r.Committed) > 0 {
		fmt.Fprintf(&b, "  commit  %s committed in the vault\n", strings.Join(r.Committed, ", "))
	}
	if r.Closed {
		b.WriteString("  closed  the story is closed\n")
	}
	if r.Landed && !r.Closed {
		fmt.Fprintf(&b, "  OPEN    the story is landed but still open: %s\n", firstLine(r.NotClosed))
		fmt.Fprintf(&b, "          nothing above is undone; run `mw next %s` again to close it, and nothing is merged or ledgered twice\n", r.StoryID)
		if r.Assignee != "" {
			b.WriteString(r.claimHint())
		}
	}
	for _, id := range r.Abandoned {
		fmt.Fprintf(&b, "  gone    %s is claimed here with no session behind it\n", id)
	}
	for _, note := range r.Notes {
		fmt.Fprintf(&b, "  note    %s\n", note)
	}
	if r.Synced {
		fmt.Fprintf(&b, "  synced  %s\n", r.Sync)
	}
	if r.Dispatched {
		b.WriteString(r.Dispatch.String())
	}
	return b.String()
}

// claimHint is what a person is told when a landed story would not close
// because somebody's name holds the claim: whose name it is, why mw cannot act
// under it, and the bd command that closes the story as its holder. The command
// is written for a shell to read as it stands, so a name with a space in it —
// as the claims made before mw acted under a name of its own carry — is quoted.
func (r NextReport) claimHint() string {
	mw := SeatIdentity(MwSeat, r.Host)
	closeIt := "`" + strings.Join([]string{"bd", "--actor", shellWord(r.Assignee), "close", shellWord(r.StoryID),
		"--reason", shellWord("landed")}, " ") + "`"
	if r.Assignee == mw {
		return fmt.Sprintf("          %s holds the claim, which is mw's own name: close it under that name: %s\n", r.Assignee, closeIt)
	}
	return fmt.Sprintf("          %s holds the claim, which predates mw acting as %s: a story claimed under another name is closed under it: %s\n",
		r.Assignee, mw, closeIt)
}

// stepList is the formula steps a session left open, one per line.
func stepList(steps []FormulaStep) string {
	said := make([]string, 0, len(steps))
	for _, step := range steps {
		said = append(said, "- "+step.ID+" · "+strings.TrimSpace(step.Title))
	}
	return strings.Join(said, "\n")
}
