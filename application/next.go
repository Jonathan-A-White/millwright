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

// CheckLines is how much of a failed test run is written onto the story. Enough
// to see what broke, not so much that a bead becomes a log file.
const CheckLines = 40

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
// did not land. Everything it refuses to do is written on the story and in the
// seat's ledger, so that a story that did not land says so in both places.
type Next struct {
	Tracker   WorkTracker
	Worktrees Worktrees
	Landing   Landing
	Checks    Checks
	Slot      MergeSlot
	Vault     Vault

	// Runner is how a story claimed here is told from a story really being
	// worked here: a claim with no session behind it is a session that went
	// away. A nil Runner skips that check.
	Runner Runner

	// Sync brings the hosts level after the story is closed and before anything
	// new is dispatched, so that the other host sees the closed story before it
	// picks its own next work. A nil Sync skips it.
	Sync HostSync

	// Dispatch is the baton: what is ready on this host becomes a running
	// session. A nil Dispatch closes the story out and stops there.
	Dispatch Dispatcher

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

	// Now is the clock the ledger line is dated by. The zero value reads the
	// real one.
	Now func() time.Time

	// Out is where the report is printed. A nil Out prints nothing.
	Out io.Writer
}

// NextReport is what one close-out did.
type NextReport struct {
	StoryID string
	Title   string
	Host    string
	// Landed says the story's work is on its target branch at the remote, and
	// How is the way it got there.
	Landed bool
	How    Landed
	Target string
	// Commits is how many commits the session left on the story's branch.
	Commits int
	// Pushes is how many times the push was attempted: more than one means the
	// other host landed something while this story was being landed.
	Pushes int
	// Why is the reason nothing landed, empty when something did.
	Why string
	// Result is what the session reported, as far as it could be read.
	Result SessionResult
	// Ledger is the line that was appended, empty when none was.
	Ledger string
	Closed bool
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
}

// Run closes out one story and reports what it did. The error it returns is the
// reason the story did not land; the report says what was recorded anyway,
// because a close-out that lands nothing still writes down what happened.
func (n Next) Run(ctx context.Context, storyID string) (NextReport, error) {
	report, err := n.closeOut(ctx, storyID)
	n.print(report.String())
	return report, err
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
	report.Title = detail.Story.Title
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
	return n.land(ctx, c, &report)
}

// land takes one story from "its session has ended" to "it is on the target
// branch and closed", stopping at the first thing that says it should not be.
func (n Next) land(ctx context.Context, c *closeOut, report *NextReport) (NextReport, error) {
	// What the session itself reported. A session that wrote nothing is not a
	// session that succeeded quietly: it is one that died.
	printed, err := n.Vault.ReadRunFile(ctx, c.id, ResultFileName)
	if errors.Is(err, fs.ErrNotExist) {
		return n.stop(ctx, c, report, fmt.Sprintf("the session left no result at %s, so it never started or it died before it could write one",
			n.Vault.RunFile(c.id, ResultFileName)), "")
	}
	if err != nil {
		return n.stop(ctx, c, report, fmt.Sprintf("the session's result could not be read: %v", err), "")
	}
	result, err := ReadSessionResult(printed)
	if err != nil {
		return n.stop(ctx, c, report, fmt.Sprintf("the session's result could not be read: %v", err), "")
	}
	c.result, report.Result = result, result
	if !result.Finished() {
		return n.stop(ctx, c, report, "the session did not finish: "+result.Trouble(), "")
	}

	// What the session left on the branch. The target branch as the remote has
	// it now is what the work is measured against: the other host may have
	// landed on it while this story was worked.
	if err := n.Worktrees.Fetch(ctx, c.rigDir); err != nil {
		return n.stop(ctx, c, report, fmt.Sprintf("the rig could not be brought up to date with %s: %v", n.remote(), err), "")
	}
	base := StartPoint(n.remote(), c.target)
	commits, err := n.Landing.Ahead(ctx, c.rigDir, c.branch, base)
	if err != nil {
		return n.stop(ctx, c, report, fmt.Sprintf("the commits on %s could not be counted: %v", c.branch, err), "")
	}
	report.Commits = commits
	if commits == 0 {
		return n.stop(ctx, c, report, fmt.Sprintf("the session committed nothing to %s, so there is nothing to land on %s",
			c.branch, c.target), "")
	}

	// What the formula says the session was to do. A step still open is a step
	// the session did not do, whatever the code looks like.
	if root := c.detail.Molecule.RootID; root != "" {
		open, err := n.Tracker.OpenSteps(ctx, root)
		if err != nil {
			return n.stop(ctx, c, report, fmt.Sprintf("the steps of the formula poured as %s could not be read: %v", root, err), "")
		}
		if len(open) > 0 {
			return n.stop(ctx, c, report,
				fmt.Sprintf("%d formula step(s) of %s are still open, so the formula was not finished", len(open), root),
				"Still open:\n"+stepList(open))
		}
	}

	// What the rig itself says about the work, in the worktree the session left.
	checked, err := n.Checks.Run(ctx, c.path.Rig, c.worktree)
	if err != nil {
		return n.stop(ctx, c, report, fmt.Sprintf("the rig's tests could not be run in %s: %v", c.worktree, err), "")
	}
	if !checked.Passed {
		return n.stop(ctx, c, report,
			fmt.Sprintf("the rig's tests fail in the worktree: `%s` did not pass", checked.Command),
			"The last lines of `"+checked.Command+"` in "+c.worktree+":\n\n```\n"+checked.Tail(CheckLines)+"\n```")
	}

	// Only one close-out at a time may touch a rig's target branch on this host.
	// The other host's races are settled by the remote itself, below.
	holding, err := n.Slot.Take(ctx, c.rigDir, Holders(n.Seat, n.Host, c.id))
	if err != nil {
		return n.stop(ctx, c, report, fmt.Sprintf("the merge slot of %s could not be taken: %v", c.path.Rig, err), "")
	}
	landed, landErr := n.merge(ctx, c, report)
	if err := holding.Release(ctx); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the merge slot of %s could not be given back: %v", c.path.Rig, err))
	}
	if landErr != nil {
		return n.stop(ctx, c, report, firstLine(landErr.Error()), said(landErr))
	}
	report.Landed, report.How = true, landed

	// The work is on the target branch at the remote from here: nothing below
	// is undone, and nothing below stops the story being closed.
	if err := n.Worktrees.Remove(ctx, c.rigDir, c.worktree, c.branch); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the worktree %s could not be taken away: %v", c.worktree, err))
	}

	outcome := fmt.Sprintf("%s, %d commits, mw next re-ran the rig's tests: pass", landed.LandedAs(c.target), report.Commits)
	if err := n.ledger(ctx, c, report, outcome); err != nil {
		report.Notes = append(report.Notes, err.Error())
	}

	if err := n.Tracker.SetStoryState(ctx, c.id, RunState, RunLanded, outcome); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s could not be recorded as %s=%s: %v", c.id, RunState, RunLanded, err))
	}
	if err := n.Tracker.CloseStory(ctx, c.id, outcome); err != nil {
		return *report, fmt.Errorf("closing out %s: it %s, but the story could not be closed: %w", c.id, outcome, err)
	}
	report.Closed = true

	report.Abandoned = n.abandoned(ctx, c.id, report)
	return n.carryOn(ctx, c, report)
}

// merge is the landing itself, under the merge slot: fetch, merge into the
// target branch as the remote has it, test whatever merging made, and push. A
// push the remote refuses is a race with the other host, and the whole thing is
// done again from its newest commit — a bounded number of times, and never
// forced.
func (n Next) merge(ctx context.Context, c *closeOut, report *NextReport) (Landed, error) {
	base := StartPoint(n.remote(), c.target)
	var lost error
	for try := 1; try <= n.tries(); try++ {
		if try > 1 {
			if err := n.Worktrees.Fetch(ctx, c.rigDir); err != nil {
				return Landed{}, fmt.Errorf("the rig could not be brought up to date with %s: %w", n.remote(), err)
			}
		}
		dir, err := n.Landing.OpenLanding(ctx, c.rigDir, base)
		if err != nil {
			return Landed{}, fmt.Errorf("a landing of %s at %s could not be opened: %w", c.path.Rig, base, err)
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
	return Landed{}, fmt.Errorf("%s could not be landed on %s: the other host got there first %d times running; nothing was forced: %w",
		c.branch, c.target, n.tries(), lost)
}

// push is one attempt at a landing, in a landing worktree that is already open:
// merge, test what merging made if it made anything new, and push.
func (n Next) push(ctx context.Context, c *closeOut, report *NextReport, dir string) (Landed, error) {
	landed, err := n.Landing.Merge(ctx, dir, c.branch)
	if err != nil {
		if Conflicted(err) {
			return Landed{}, fmt.Errorf("%s does not merge into %s without conflicts, which mw will not resolve for anybody: %w",
				c.branch, c.target, err)
		}
		return Landed{}, fmt.Errorf("%s could not be merged into %s: %w", c.branch, c.target, err)
	}

	// A merge commit is a combination of two branches that nothing has ever been
	// tested on: the story's tests passed on the story's branch, and the other
	// host's passed on its own. Only this result matters now.
	if !landed.FastForward {
		checked, err := n.Checks.Run(ctx, c.path.Rig, dir)
		if err != nil {
			return Landed{}, fmt.Errorf("the rig's tests could not be run on the merged result in %s: %w", dir, err)
		}
		if !checked.Passed {
			return Landed{}, fmt.Errorf("%s and %s do not pass the rig's tests together: `%s` failed on the merged result, so nothing was pushed\n\n```\n%s\n```",
				c.branch, c.target, checked.Command, checked.Tail(CheckLines))
		}
	}

	report.Pushes++
	if err := n.Landing.Push(ctx, dir, n.remote(), c.target); err != nil {
		return Landed{}, err
	}
	return landed, nil
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

// stop records a close-out that landed nothing: the reason on the story, the
// story marked blocked so that nobody takes it for work in flight, and one line
// in the ledger saying it did not land. Nothing is merged, nothing is pushed,
// nothing is closed and nothing is given back — the worktree and the branch are
// left exactly as the session left them, because they are the evidence.
func (n Next) stop(ctx context.Context, c *closeOut, report *NextReport, why, said string) (NextReport, error) {
	report.Why = why

	note := fmt.Sprintf("mw next on %s did not close this story out: %s\n\n"+
		"Nothing was merged and nothing was pushed. The story is not closed and the claim was not given back. "+
		"The worktree %s and the branch %s are left as the session left them.",
		n.Host, why, c.worktree, c.branch)
	if said != "" {
		note += "\n\n" + said
	}

	var trouble []string
	if err := n.Tracker.CommentOnStory(ctx, c.id, note); err != nil {
		trouble = append(trouble, fmt.Sprintf("the reason could not be written on the story: %v", err))
	}
	if err := n.Tracker.SetStoryState(ctx, c.id, RunState, RunBlocked, why); err != nil {
		trouble = append(trouble, fmt.Sprintf("%s could not be recorded as %s=%s: %v", c.id, RunState, RunBlocked, err))
	}
	if err := n.ledger(ctx, c, report, "not landed: "+firstLine(why)); err != nil {
		trouble = append(trouble, err.Error())
	}

	err := fmt.Errorf("closing out %s: %s", c.id, why)
	if len(trouble) > 0 {
		report.Notes = append(report.Notes, trouble...)
		err = fmt.Errorf("%w (and %s)", err, strings.Join(trouble, "; "))
	}
	return *report, err
}

// ledger appends this story's one line to the seat's ledger.
func (n Next) ledger(ctx context.Context, c *closeOut, report *NextReport, outcome string) error {
	line := LedgerLine{
		When:    n.now(),
		StoryID: c.id,
		Title:   c.detail.Story.Title,
		Outcome: outcome,
		Path:    c.path,
		Result:  c.result,
		Notes:   n.ledgerNotes(c, report),
	}.String()

	if err := n.Vault.AppendToLedger(ctx, n.Seat, line); err != nil {
		return fmt.Errorf("the line for %s could not be appended to the %s seat's ledger: %v", c.id, n.Seat, err)
	}
	report.Ledger = line
	return nil
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

// recordedGone reports whether id's run state already says its session is not
// to be trusted as running — set by mw next (RunStopped) once a close-out
// found it gone, or by mw sweep (RunStuck) once a sweep did. It is the guard
// both share, so that whichever of them notices a claimed story's session is
// gone first is the one that comments, and the other says nothing again.
func recordedGone(ctx context.Context, tracker WorkTracker, id string) bool {
	was, err := tracker.StoryState(ctx, id, RunState)
	return err == nil && (was == RunStopped || was == RunStuck)
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
	if r.Landed {
		fmt.Fprintf(&b, "  landed  %s on %s (%d commits, %d push(es))\n", r.How.LandedAs(r.Target), r.Target, r.Commits, r.Pushes)
	} else {
		fmt.Fprintf(&b, "  STOPPED %s\n", r.Why)
	}
	if r.Ledger != "" {
		fmt.Fprintf(&b, "  ledger  %s\n", r.Ledger)
	}
	if r.Closed {
		b.WriteString("  closed  the story is closed\n")
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

// stepList is the formula steps a session left open, one per line.
func stepList(steps []FormulaStep) string {
	said := make([]string, 0, len(steps))
	for _, step := range steps {
		said = append(said, "- "+step.ID+" · "+strings.TrimSpace(step.Title))
	}
	return strings.Join(said, "\n")
}

// said is everything after the first line of a failure, which is where a
// command's own output ends up.
func said(err error) string {
	text := err.Error()
	if cut := strings.IndexByte(text, '\n'); cut >= 0 {
		return strings.TrimSpace(text[cut:])
	}
	return ""
}
