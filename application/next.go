package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/domain/events"
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
	// RunQueued is a story of a rig that lands in batches whose close-out
	// passed: its branch is on the remote as mw/<id>, its worktree and claim
	// are kept, and it waits for its batch to land it (plans/0117, Land Rush).
	// Its session is gone and its lease lapses, and that is no sign of trouble:
	// mw status, the sweep, the dispatch and the quiet alarm call it alive, and
	// none of them counts it as a session, marks it stuck or gives it back.
	RunQueued = "queued"
	// RunCloseOutAgain is a story whose close-out waited the overall most for
	// its rig's merge slot and found it still held (mw-gq6.467): its session
	// finished and its work is committed, so it keeps its claim, worktree and
	// branch, and the next dispatch tick runs its close-out alone again, never
	// a fresh Builder session. Until then it has no session and its lease
	// lapses: the sweep, the quiet alarm and another close-out pass it over.
	RunCloseOutAgain = "close-out-again"
)

// RebaseState is the state dimension that says a story's branch was sent back
// to a fresh Builder session to rebase, after it would not merge into its
// target branch without conflicts. RebaseSentBack is its one value. It is on
// the story, not in a note, so that a story sent back is seen to be.
//
// What bounds the send-backs is two metadata fields: RebaseSendsField counts
// them and RebaseBaseField holds the head of the target branch the last was
// made against. A branch that conflicts again is sent back again only when the
// target branch has moved since that head, and never more than MaxRebaseSends
// times in all: otherwise it is a person's to resolve.
const (
	RebaseState    = "rebase"
	RebaseSentBack = "sent-back"

	RebaseSendsField = "rebase_sends"
	RebaseBaseField  = "rebase_base"
	MaxRebaseSends   = 3
)

// MergedTestsState is the state dimension that says a story's branch was sent
// back to a fresh Builder session to fix the rig's tests, after its branch
// merged into its target branch without conflicts but the merged result
// failed them. MergedTestsSentBack is its one value, kept separately from
// RebaseState because the two send-backs answer different failures and a
// story may need either on its own. It is on the story, not in a note, for the
// same reason RebaseState is: a story carrying it whose branch fails the
// merged tests again is stopped, never sent back again.
const (
	MergedTestsState    = "mergedtests"
	MergedTestsSentBack = "sent-back"
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
	ReasonTrackedBinary   Reason = "tracked-binary"
	// ReasonLockfileSSH is a branch that adds a git+ssh:// line to a
	// package-lock.json (mw-gq6.740): a machine with no GitHub key cannot
	// npm ci it. The session mends it in its own worktree, so the branch is
	// continued.
	ReasonLockfileSSH Reason = "lockfile-ssh"
	ReasonOpenSteps   Reason = "open-steps"
	// ReasonGristExampleUnproved is a branch that adds or changes a grind
	// example (grinds/examples/<kind>/<name>.json) whose closing comment has no
	// 'mw grist eval <app> <kind> --example <name>' line with PASS (mw-gq6.772):
	// an example that passed only the fake mill can fail live. Like open steps,
	// it is the Builder's to mend, by running the eval and saying so.
	ReasonGristExampleUnproved Reason = "grist-example-unproved"
	// ReasonNoClosingComment is a story whose formula is finished but whose
	// branch is not: no closing comment was written after its newest commit
	// (mw-gq6.630). Like open steps, it is the Builder's to mend.
	ReasonNoClosingComment Reason = "no-closing-comment"
	// ReasonUnmetCriterion is a story whose closing comment says an acceptance
	// criterion holds only in part, is not met or was not run (mw-gq6.711): not
	// a landing, whatever the gate says. Its claim is kept, as for the tests
	// failing: a new attempt would meet the same criterion, so the Mayor
	// re-scopes or closes it.
	ReasonUnmetCriterion  Reason = "unmet-criterion"
	ReasonTestsFail       Reason = "tests-fail"
	ReasonMergeConflict   Reason = "merge-conflict"
	ReasonMergedTestsFail Reason = "merged-tests-fail"
	// A Tester story's branch (mw-it6qk5.5): it committed something, or wrote
	// no FINDINGS.
	ReasonTesterCommitted Reason = "tester-committed"
	ReasonNoFindings      Reason = "no-findings"

	// The factory.
	ReasonNoResult      Reason = "no-result"
	ReasonSessionFailed Reason = "session-failed"
	// ReasonTurnCap is a session its harness stopped at its turn cap
	// (mw-1ljpo9.6): the claim is given back, as for open steps.
	ReasonTurnCap       Reason = "turn-cap"
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

// HeartbeatInterval is how often Heartbeat renews a claimed story's lease for
// as long as its session is running: comfortably inside bd's fixed five-
// minute lease (LeaseTTL, infrastructure/beads), so the lease a claim starts
// with never lapses mid-session.
const HeartbeatInterval = 2 * time.Minute

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
// did not land. A branch that will not merge is sent back to a fresh
// session of the seat to rebase, again each time the target branch has moved
// since, three times at most; any other conflict stops there. Everything it
// refuses to do is written on the story and in the seat's ledger, so that a
// story that did not land says so in both places.
type Next struct {
	Tracker   WorkTracker
	Worktrees Worktrees
	Landing   Landing
	Checks    Checks
	Slot      MergeSlot

	// SlotYield, when set, is called when the close-out is about to wait on its
	// own, for a gate lock another gate holds or for a busy host to calm, and
	// it gives up the rig's merge slot to the waiters that can share it until
	// the function it returns is called, which takes it back alone. A batch
	// landing sets it, so that a Builder's check of the rig runs beside the
	// wait (mw-4hqh0en); mw next leaves it nil and holds the slot throughout.
	SlotYield func(ctx context.Context) (resume func())

	// SpecChecks runs a rig's changed-specs command after the rig's gate, on the
	// specs the story added or changed (mw-gq6.366). Nil runs none.
	SpecChecks SpecChecks

	// GateParts and GateRecords are how a rig's gate is split into parts on this
	// host and what each part said: a story stopped with steps open is told which
	// parts were not run at its tip and the command to run each (mw-6zgnz5y).
	// Nil of either says nothing of the parts.
	GateParts   GateParts
	GateRecords CheckRecords

	// RelatedChecks runs a rig's related-tests line at the landing gate, in
	// place of the rig's whole tests, on the files the branch changed
	// (mw-gq6.601). Nil runs the whole tests: mw check never sets it.
	RelatedChecks RelatedChecks
	Vault         Vault

	// NamedChecks runs just the tests that failed on a merged result, by name,
	// on the target branch alone, to tell a story's failure from main's own red
	// (mw-gq6.645). Nil, or a rig that names no line, blames the story as
	// before.
	NamedChecks NamedChecks

	// CrossSlot is the rig's merge slot across hosts, on the beads server both
	// hosts share, taken inside Slot so that the other host's close-outs wait
	// their turn rather than race this one to the remote (mw-gq6.359). The zero
	// CrossSlot takes none, and the remote alone settles the race.
	CrossSlot CrossSlot

	// Rules is where a rig's version_files are read from, so that a landing
	// raises the rig's patch version. A nil Rules, or a rig that names none,
	// raises nothing.
	Rules EpicRules

	// AfterLanding runs the command a host names for a rig once a landing has
	// moved this host's checkout of it, so that a host rebuilds what it runs
	// from the rig by itself. A nil AfterLanding runs nothing.
	AfterLanding AfterLanding

	// Home is the vault's home file, read to say, when this host names no
	// command for a rig, which host deploys it instead (mw-gq6.562). A nil Home
	// says nothing of the home.
	Home HomeFile

	// DeploySlot is the rig's after-landing lock, taken around its command so that
	// a landing's deploy and `mw after-landing` never run side by side
	// (mw-gq6.309). A nil DeploySlot takes none. AfterRetryWait is how long the
	// command is left before it is run once more after a passing network fault;
	// zero is AfterLandingRetryWait.
	DeploySlot     MergeSlot
	AfterRetryWait time.Duration

	// Deployed is where this host remembers the commit a passing after-landing
	// run last deployed, per rig, so that the home's tick does not deploy it
	// again (mw-gq6.564). A nil Deployed remembers nothing.
	Deployed BuiltMarks

	// Units restarts this host's long-running mw user units once a landing's
	// build of the factory rig has succeeded, so that they run the new binary
	// (mw-gq6.232). A nil Units restarts nothing.
	Units UnitRestarter

	// Backend stages the backend of a landing whose story changed it, on the
	// home, and writes its swap as a hands step for the Governor to approve
	// (mw-gq6.185). A zero Backend, or a rig it names no backend for, does
	// nothing, and nothing it does can stop a story being landed or closed.
	Backend BackendStage

	// Smoke runs the grist smoke of the apps a landing changed the grist of
	// (grinds/, an app's grist client paths, or the mill itself), once the
	// landing has moved the rig. A failure is an alarm, a line of mw status,
	// and a hold on the rig's open stories (mw-gq6.319). A zero Smoke does
	// nothing, and nothing it does can stop a story being landed or closed.
	Smoke GristSmokeAfter

	// Files is the vault as a git clone: what a close-out commits its own
	// ledger line and the session's rig memory through, so that the sync it
	// then runs is not stopped by the work it has just done. A nil Files leaves
	// them uncommitted, and the sync will say so.
	Files VaultFiles

	// Runner is how a story claimed here is told from a story really being
	// worked here: a claim with no session behind it is a session that went
	// away. A nil Runner skips that check.
	Runner Runner

	// Transcripts is what the harness recorded in a story's worktree. A claim
	// here with no session behind it is given back only when no transcript was
	// recorded since the claim began; a nil Transcripts cannot say so, and the
	// claim is left alone.
	Transcripts ClaimTranscripts

	// Modes is the harness's record of the permission mode a story's session ran
	// in (mw-gq6.545): a refusal of a session that was launched in auto and ran in
	// another says so. A nil Modes reads none.
	Modes TranscriptModes

	// HeartbeatWait is how Heartbeat waits between ticks: the real clock,
	// unless a test replaces it to move time without truly waiting for it.
	// A nil HeartbeatWait waits for real.
	HeartbeatWait func(ctx context.Context, d time.Duration) error

	// Memory is where mw sweep remembers what it saw of a story's last session,
	// cleared when a close-out sends the story back to a fresh session of its
	// own (mw-gq6.86): that fresh session's pane starts blank too, and a note
	// the ended session left behind would otherwise be read as its silence,
	// since the old session's clock. A nil Memory clears nothing.
	Memory SweepNotes

	// RetryFromBranch is next.retry_from_branch (mw-1ljpo9.7): when it is on, a
	// close-out that stops a story for a reason ContinuesFrom names writes
	// ContinueKey in Memory, so that mw retry keeps its branch and the next
	// attempt is cut from it, rebased onto the target. Off, nothing is written.
	RetryFromBranch bool

	// HasGristTestKey says whether this host holds the factory's grist test key,
	// and so can reach the live mill a grind example is proved on. A host that
	// holds none and stops a branch grist-example-unproved pushes the branch to
	// the remote for the home to prove and finish (mw-1knt69p). Nil is a host
	// that can: nothing is pushed.
	HasGristTestKey func() bool

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

	// Load is how busy this host is, read around the rig's tests: a close-out
	// waits, up to LoadBound, for a busy host to calm before them, and runs them
	// once more when they fail while it was busy (mw-gq6.228). A nil Load, or a
	// load that cannot be read, does neither. LoadPoll is how often the wait looks
	// again and LoadBound how long it waits in all; zero is GateLoadPoll and
	// GateLoadBound. LoadWait is how it waits, the real clock unless a test
	// replaces it.
	Load      HostLoad
	LoadPoll  time.Duration
	LoadBound time.Duration
	LoadWait  func(ctx context.Context, d time.Duration) error

	// CalmHandBack, when it is set, changes what the first wait for a calm host
	// does at LoadBound with the host still busy: it says again every minute that
	// it is waiting, and instead of running the tests anyway it ends the gate with
	// the error CalmHandBack returns, the load being the reading taken last and
	// since when the wait began. mw check sets it (mw-gq6.819); a close-out does
	// not, and runs the tests anyway.
	CalmHandBack func(ctx context.Context, reading LoadReading, since time.Time, waited time.Duration) error

	// GateLock keeps this host to one rig gate at a time, whatever rig: a
	// close-out takes it before the rig's tests, in the worktree and again on the
	// merged result, and waits for the load to calm only once it has it
	// (mw-gq6.388). On the merged result it is kept until the landing ends, and
	// never waited for under the rig's merge slot across hosts (mw-gq6.422). A
	// nil GateLock takes none.
	GateLock GateLock

	// Benchmarks is where the benchmarks of past close-outs are read from, for a
	// landed story's par and the calibration printed with the report, and Bench
	// the thresholds of both (mw-t0z3fu.2). Every close-out that ran the rig's
	// tests records its own in the story's result file whether or not there
	// is a book; a nil Benchmarks gives it no par and the report no summary.
	Benchmarks BenchmarkBook
	Bench      BenchmarkSettings

	// BenchFlags is what this host last printed of each kind's calibration
	// flag, so that the report says a flag once a day and then only when it
	// changes (mw-gq6.518). A nil BenchFlags prints every flag every time.
	BenchFlags BenchFlagMemory

	// CloseOuts is where this close-out says it is running, so that mw status
	// and the quiet alarm do not take the story for quiet while it waits its
	// turn or for the host to calm. A nil CloseOuts leaves no mark.
	CloseOuts CloseOutMarks

	// Tester is the home's Tester trial: a landing in it springs a Tester story
	// (springTester). The zero Tester is no trial, and springs nothing.
	Tester TesterTrial

	// Changes reads the files a story's branch has changed: the heartbeat
	// records them on the story as it works, and a landing says which of them
	// the story's files key did not name (mw-gq6.406). A nil Changes does
	// neither.
	Changes BranchChanges

	// Errors is the common-errors register a landing reads to say, of a bug's
	// 'For common errors:' line, whether its class is already there
	// (mw-gq6.420). A nil Errors reads every class as new.
	Errors ErrorFiles

	// RigFacts is the seat's rig memory a landing reads to tell a facts rig from
	// a flat-file rig, so that its rig-memory block proposes what mw memory
	// takes (mw-gq6.558). A nil RigFacts reads every rig as a facts rig.
	RigFacts RigFactFiles

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
	// tries; the zero value is no wait, so that a test never sleeps. A close of
	// the landed story that fails is tried again the same way (closeStory).
	PushTries int
	PushWait  time.Duration

	// RetryWait waits d, or until ctx ends, between the tries of a fetch or a
	// push that failed on a passing fault; a test replaces it. Nil waits for
	// real. HeartbeatWait is the heartbeat's own clock and is not this one.
	RetryWait func(ctx context.Context, d time.Duration) error

	// Heartbeats is how a by-hand close-out learns that the story's session is
	// still running, which it then refuses to close out (mw-gq6.394). Nil asks
	// nothing, and every close-out goes on.
	Heartbeats LiveHeartbeats

	// HeartbeatGrace is how long a close-out waits for a heartbeat of its story
	// to go before it refuses the story as still running (mw-gq6.500): the pane
	// line of a session launched before mw-gq6.487 kills the heartbeat and starts
	// the close-out at once, and mw ends on TERM by cancelling what it was doing,
	// so the heartbeat can outlive the kill by a moment. Zero waits not at all,
	// so that a test never sleeps.
	HeartbeatGrace time.Duration

	// Leftovers is the host's table of the user's processes, swept at the end
	// of a Tester story's close-out for what its session left running under
	// /tmp/tester-<story>/ (mw-gq6.409). Nil sweeps nothing.
	Leftovers UserProcesses

	// Events is the home's event log, where a close-out says it could not record
	// what it meant to (mw-gq6.375). Nil writes none.
	Events EventLog

	// Stamps is where a landing leaves a chain stamp for the follower to
	// broadcast (docs/chain-stamps.md). A nil Stamps queues none, and a queue
	// that fails is said on the report and changes nothing about the landing.
	Stamps StampQueue

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
	// Queued says the story passed its close-out on a rig that lands in
	// batches, and was queued for its batch rather than landed: its branch is
	// at the remote as QueuedAs, and Trusted is the commit whose mw check run
	// was trusted in place of running the rig's tests again, empty when they
	// were run.
	Queued   bool
	QueuedAs string
	Trusted  string
	// MainRed is what the close-out found when the tests that failed on the
	// story's merged result fail on the target branch alone too: the story was
	// held on the bead that carries main's fix, not sent back or refused
	// (mw-gq6.645). Nil when it did not.
	MainRed *MainRed
	// HandedBack says the merge slot was still held at the overall most a
	// close-out waits, and the story was handed back for its close-out alone
	// (RunCloseOutAgain), its claim, worktree and branch kept (mw-gq6.467).
	HandedBack bool
	// Interrupted says the close-out's own context ended before it settled
	// anything — a signal, the caller's session gone — so that nothing was
	// recorded as the story's outcome (mw-gq6.441).
	Interrupted bool
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
	// Tags is what became of the release tags the landing carries to the
	// remote: pushed, left alone because the remote has them elsewhere, or
	// named by the acceptance criteria and still not there (mw-gq6.673).
	Tags PublishedTags
	// CrossSlot is what became of the rig's cross-host merge slot: how long
	// the close-out waited for it and for whose landing, a holder gone away it
	// was taken over from, or why it was skipped. Empty when there was nothing
	// to say.
	CrossSlot []string
	// Rig is what became of this host's own checkout of the rig once the story
	// was landed: fast-forwarded onto the landed commit, or left as it was with
	// the reason. RigDir is where that checkout is. A landing made by an earlier
	// run says nothing about it.
	Rig    Advanced
	RigDir string
	// AfterLandingSkipped is the line saying the rig's after-landing command was
	// named for this host but not run, and why; empty when it ran or none is
	// named. The mail to the Mayor says it first.
	AfterLandingSkipped string
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
	// NotCommitted is the line saying what a close-out could not commit in the
	// vault, and why (NotCommittedLine); empty when it all was.
	NotCommitted string
	// FormulaClosed is what this close-out closed of the story's formula
	// (mw-gq6.438).
	FormulaClosed FormulaClosed
	Closed        bool
	// DemoHeld is true when the story landed and is a demo, so it was left open
	// and held for the Governor's "Looks good" rather than closed.
	DemoHeld bool
	// Abandoned names the stories claimed here whose session is not there any
	// more — claims a person or a later sweep has to settle.
	Abandoned []string
	// GaveBack names the stories claimed here whose session never started —
	// no transcript, no commit, nothing changed in a worktree — and whose
	// claim this close-out gave back, so that the next dispatch takes them.
	GaveBack []string
	// Notes are the things that went sideways without changing the outcome.
	Notes []string
	// AfterLandingStopped is the limit the rig's after-landing command outlived
	// and was stopped at, zero when it was not: a command cut off in the middle
	// of its work may have left the rig half done, which the mail to the Mayor
	// says first (AfterLandingStoppedLine).
	AfterLandingStopped time.Duration

	// UnitRestartFailed holds a line for each long-running unit that could not be
	// restarted on the new build, which the mail to the Mayor says first.
	UnitRestartFailed []string

	// Tested is the landed story a Tester story tested, when this close-out
	// closed one, and Findings what the Tester found. TesterFiled is the
	// Tester story this landing sprang, empty when it sprang none.
	Tested      string
	Findings    TesterFindings
	TesterFiled string

	// Swept is how many processes a Tester story left running were stopped under
	// SweptDir, /tmp/tester-<story>/, at the end of its close-out (mw-gq6.409);
	// tester is whether the story is a Tester story, read with the story.
	Swept    int
	SweptDir string
	tester   bool

	// Bench is what the close-out recorded of its gate, one line and the
	// calibration of its kind under it; empty when it recorded none or has no
	// book to compare it with.
	Bench []string

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
	// branchPushed is set when the stop pushed the story's branch to the remote
	// for the home to finish (unprovedFromAHostWithNoMill).
	branchPushed bool
	target       string
	result       SessionResult

	// cappedAfterClosing says the harness stopped the session at its turn cap
	// after it wrote its closing comment, so the close-out judges it as a
	// finished one (mw-gq6.595).
	cappedAfterClosing bool

	// own are the stories whose own checks the related-tests line is given the
	// failures of when this is a batch's close-out; nil is the story itself.
	// failedFiles are the test files those checks failed, once failedRead says
	// they were read (mw-gq6.808).
	own         []ownCheck
	failedFiles []string
	failedRead  bool

	// landingReadErr is why the close-out's last read of the rig's file for how
	// it lands failed, nil when it did not; merge's guard names it.
	landingReadErr error

	// codemapBytes is the size of the union-merged docs/codemap.md when the
	// merged result's tests failed only on its size and the landing let it
	// through (codemapBandPercent), 0 otherwise; a landing that pushes files
	// the bug that asks for the map to be trimmed.
	codemapBytes int

	// mainRed is what a refusal for failing tests found of the target branch
	// alone, when it fails them too (mw-gq6.645).
	mainRed *MainRed

	// mergedFailed is the last run of the rig's tests that failed on a merged
	// result, from which the failing tests are named (mw-gq6.645); the zero
	// Checked when none did.
	mergedFailed Checked

	// worktreeFailed says the rig's tests failed in the worktree and the
	// close-out went on to the merged result (mw-gq6.552): a merge that is a
	// fast-forward then still has its tests run, since no run has passed.
	worktreeFailed bool

	// backendChanged says the story's own commits changed what the rig's backend
	// is built from, so that the landing is followed by staging it.
	backendChanged bool

	// smokeApps are the apps whose grist the story's own commits changed.
	smokeApps []string

	// smokeUnhosted says the story's own commits changed grist that this host,
	// naming no [grist-apps], cannot smoke (mw-gq6.728).
	smokeUnhosted bool

	// outsideKey are the files the story's own commits changed that its files
	// key does not name, read before the landing (mw-gq6.406).
	outsideKey []string

	// cross is the rig's merge slot across hosts while the close-out holds it,
	// nil while it holds none. Holding it, the first try at a landing fetches
	// first: the other host may have landed while this one waited for it.
	cross *CrossHolding

	// merged is what the gate on a merged result keeps between tries: this
	// host's gate lock, once taken, and how far the host was let calm.
	merged mergedGate

	// landingError says this run kept the whole error of a failed landing in the
	// vault, so the close-out commits it.
	landingError bool

	// unrecorded says this run kept what it could not record of a stopped story
	// in the vault (UnrecordedFileName), so the close-out commits it.
	unrecorded bool

	// gateLog is the name of the file in the run that holds the whole output of
	// a failed test run, empty when none was kept; the close-out commits it.
	gateLog string

	// harnessStop is the name of the file in the run that keeps a harness stop
	// read from words that are not a result (HarnessStopFileName), empty when
	// none was kept; the close-out commits it.
	harnessStop string

	// loginTold is set when the Mayor has been mailed this host's expired login
	// (holdHostForLogin), so that the refusal of this story does not mail the
	// same news again (mw-gq6.781).
	loginTold bool

	// afterLog is the name of the file in the run that holds the whole output of
	// the after-landing command (AfterLandingLogFileName), empty when none was
	// kept; the close-out commits it.
	afterLog string

	// began is when the close-out began, as its mark says.
	began time.Time

	// gate is what the rig's tests measured of the host, as the benchmark
	// records it.
	gate gateRecord

	// batch says the story's rig lands in batches, so that a close-out that
	// passes queues the story rather than landing it (plans/0117); tip is the
	// commit at its branch's tip as the close-out read it then, and trusted is
	// that same commit when mw check passed at it, so that the close-out does
	// not run the rig's tests again. Both are empty on a rig that lands each.
	batch   bool
	tip     string
	trusted string

	// tested is the landed story a Tester story tested, findings what it
	// found, and findingsFrom the story they were written on, as its checks
	// read them (testerRefusals).
	tested       string
	findings     TesterFindings
	findingsFrom string
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
	// A story whose session is still at work is not closed out by hand: nothing
	// is written, so that the session's own close-out finds the story as it left
	// it (mw-gq6.394).
	if why := n.stillRunning(ctx, storyID); why != "" {
		return NextReport{StoryID: storyID, Host: n.Host}, fmt.Errorf("closing out %s: %s", storyID, why)
	}
	// The session that held the claim's lease alive has ended by now; the merge
	// queue and the rig's tests that follow can outlast bd's five-minute TTL, and
	// a lapsed lease is what mw sweep marks run=stuck (mw-gq6.138).
	release := n.holdLease(ctx, storyID)
	// Say the close-out is running, so that mw status and the quiet alarm do not
	// take the story for quiet while it waits its turn or for the host to calm.
	began := n.now()
	n.markCloseOut(ctx, storyID, began, false)
	report, err := n.closeOut(ctx, storyID, began)
	n.sweepTester(ctx, &report)
	// Cleared before the session is closed below, which may hang this process up.
	n.clearCloseOut(storyID)
	report.Notes = append(report.Notes, release()...)
	n.print(report.String())
	n.closeSession(ctx, report)
	return report, err
}

// closeSession takes away the runner session the story was worked in, once the
// story is landed and closed. It is the last thing Run does, after the report is
// printed, because mw next usually runs inside that very session: closing it
// hangs up this process, and nothing after it is sure to run. A story that was
// not closed keeps its session, exited, on screen — that is what it is kept for —
// unless the tracker holds it closed or open again (finishedStory), or it was
// refused and the close-out's words are kept in its run (refusedWithLogKept).
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
	case !report.Closed && !report.DemoHeld && !report.Queued && !n.finishedStory(ctx, report.StoryID) && !n.refusedWithLogKept(ctx, report.StoryID):
		return
	}
	if err := n.Runner.Close(ctx, name); err != nil {
		n.print(fmt.Sprintf("  note    the session %s could not be closed: %v\n", name, err))
	}
}

// finishedStory says a close-out that did not close its story still leaves the
// story with nothing for its session's pane to be evidence of: the tracker now
// holds it closed, or open (its claim was given back, or never held), so that
// whatever session is next started for it is a fresh one (mw-gq6.687). A story
// still in progress is a refusal whose claim is kept, and its dead pane stays
// on screen for the Mayor (mw-gq6.61, mw-gq6.182). A story that cannot be read
// is taken for one still in progress: the pane is kept.
func (n Next) finishedStory(ctx context.Context, storyID string) bool {
	if n.Tracker == nil {
		return false
	}
	detail, err := n.Tracker.ShowStory(ctx, storyID)
	if err != nil {
		return false
	}
	status := strings.TrimSpace(detail.Status)
	return detail.Closed() || strings.EqualFold(status, StatusOpen) && strings.TrimSpace(detail.Assignee) == ""
}

// refusedWithLogKept says a close-out that refused the story (run=blocked, its
// claim kept for the Mayor) has its words kept in the story's run, so that its
// session's pane is not the evidence any more and is closed with the rest
// (mw-gq6.797): left on screen it sat dead for 7 days with the story in progress
// and nothing to say so. mw status names the story as refused, and mw retry
// recovers it. A story that cannot be read, or whose log is not kept, keeps its
// pane, as before.
func (n Next) refusedWithLogKept(ctx context.Context, storyID string) bool {
	if n.Tracker == nil || n.Vault == nil {
		return false
	}
	if run, err := n.Tracker.StoryState(ctx, storyID, RunState); err != nil || run != RunBlocked {
		return false
	}
	detail, err := n.Tracker.ShowStory(ctx, storyID)
	return err == nil && closeOutLogKept(ctx, n.Vault, detail)
}

// Heartbeat renews storyID's claim's lease every HeartbeatInterval for as
// long as its session is still running, so that the lease bd's fixed five-
// minute TTL grants at claim never lapses while the Builder is still at
// work: it is mw next itself, run alongside the Builder's own session from
// the moment it starts to the moment it ends, at zero tokens and with no
// daemon. It returns once the session is no longer running, or ctx is
// cancelled, whichever comes first — neither is an error, since both are the
// ordinary way this loop ends. A heartbeat that fails is a note on Out, and
// does not stop the loop: the very next tick may renew the lease, and one
// truly lost is StaleClaims's and ReclaimStory's to settle, not this loop's.
func (n Next) Heartbeat(ctx context.Context, storyID string) error {
	if n.Tracker == nil || n.Runner == nil {
		return fmt.Errorf("heartbeating %s: it needs a work tracker and a runner to watch its session", storyID)
	}
	name := SessionName(storyID)
	// A renewal that fails the same way tick after tick is said when it first
	// fails, and its repeats once, when it ends (mw-gq6.820).
	var repeat leaseRepeat
	flush := func() {
		if line := repeat.more(); line != "" {
			n.print(fmt.Sprintf("mw next: the claim on %s could not be heartbeated, %s\n", storyID, line))
		}
		repeat = leaseRepeat{}
	}
	defer flush()
	for {
		if err := n.wait(ctx, HeartbeatInterval); err != nil {
			return nil
		}
		status, err := n.Runner.Status(ctx, name)
		if err != nil {
			return fmt.Errorf("heartbeating %s: its session %s could not be asked about: %w", storyID, name, err)
		}
		if !status.Running() {
			return nil
		}
		if err := n.Tracker.HeartbeatClaim(ctx, storyID); err != nil {
			if repeat.same(err) {
				repeat.again(n.now())
			} else {
				flush()
				repeat = leaseRepeat{err: err.Error(), count: 1, first: n.now(), last: n.now()}
				n.print(fmt.Sprintf("mw next: the claim on %s could not be heartbeated: %v\n", storyID, err))
			}
		} else {
			flush()
		}
		n.recordFilesChanged(ctx, storyID)
	}
}

// holdLease renews storyID's claim's lease every HeartbeatInterval from now
// until the function it returns is called, whatever the story's session is
// doing: Heartbeat stops with the session, and a close-out that waits its turn
// in the merge slot and re-runs the rig's tests would otherwise let the lease
// lapse under a landing that is alive. The function it returns stops the loop,
// waits for it to be gone — so that no renewal happens after it returns — and
// gives back what could not be renewed, for the report. A renewal that fails
// does not stop the loop, as in Heartbeat, and one that fails the same way again
// is told once, with how many times and since when. A story that is not
// claimable, one no longer in progress, cannot be renewed by trying again: that
// is told once and the loop ends, the landing going on without it (mw-gq6.820).
func (n Next) holdLease(ctx context.Context, storyID string) (release func() []string) {
	if n.Tracker == nil || strings.TrimSpace(storyID) == "" {
		return func() []string { return nil }
	}
	ctx, cancel := context.WithCancel(ctx)
	var failed []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		var fails []*leaseRepeat
		defer func() {
			for _, f := range fails {
				failed = append(failed, f.line(fmt.Sprintf("the claim on %s could not be heartbeated while it was landed", storyID)))
			}
		}()
		for n.wait(ctx, HeartbeatInterval) == nil {
			if ctx.Err() != nil {
				return
			}
			err := n.Tracker.HeartbeatClaim(ctx, storyID)
			if err == nil || ctx.Err() != nil {
				continue
			}
			if strings.Contains(err.Error(), notClaimable) {
				failed = append(failed, fmt.Sprintf("the claim on %s is not in progress, so its lease is no longer renewed while it is landed: %v", storyID, err))
				return
			}
			var seen *leaseRepeat
			for _, f := range fails {
				if f.same(err) {
					seen = f
				}
			}
			if seen == nil {
				fails = append(fails, &leaseRepeat{err: err.Error(), count: 1, first: n.now(), last: n.now()})
				continue
			}
			seen.again(n.now())
		}
	}()
	return func() []string {
		cancel()
		<-done
		return failed
	}
}

// notClaimable is what bd says of a heartbeat on a story that is not in
// progress, as after a hand-back.
const notClaimable = "not claimable"

// leaseRepeat is one way a lease renewal failed, how many times in a row it did
// and between when and when, so that a loop tells it once.
type leaseRepeat struct {
	err         string
	count       int
	first, last time.Time
}

func (r leaseRepeat) same(err error) bool { return r.count > 0 && r.err == err.Error() }

func (r *leaseRepeat) again(at time.Time) {
	r.count++
	r.last = at
}

// span is "24 times from 23:22 to 23:58", or "" for a failure that happened once.
func (r leaseRepeat) span() string {
	if r.count < 2 {
		return ""
	}
	return fmt.Sprintf("%d times from %s to %s", r.count, r.first.UTC().Format("15:04"), r.last.UTC().Format("15:04"))
}

// line is the failure as a report line under what: "<what>: <err>", with the
// count and the time between the what and the colon when it was repeated.
func (r leaseRepeat) line(what string) string {
	if span := r.span(); span != "" {
		return fmt.Sprintf("%s (%s): %s", what, span, r.err)
	}
	return fmt.Sprintf("%s: %s", what, r.err)
}

// more is what Heartbeat adds once the failure it said when it first happened
// has stopped happening, counting that first one too, or "" when it happened
// only once.
func (r leaseRepeat) more() string {
	if span := r.span(); span != "" {
		return fmt.Sprintf("the same way %s in all: %s", span, r.err)
	}
	return ""
}

// wait pauses for d, or until ctx ends, whichever is first — the real clock,
// unless HeartbeatWait says otherwise, which is how a test moves time without
// truly waiting for it.
func (n Next) wait(ctx context.Context, d time.Duration) error {
	if n.HeartbeatWait != nil {
		return n.HeartbeatWait(ctx, d)
	}
	return waitFor(ctx, d)
}

// closeOut is Run without the printing.
func (n Next) closeOut(ctx context.Context, storyID string, began time.Time) (NextReport, error) {
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

	detail, err := readRetrying(ctx, n, &report, "the story", func() (StoryDetail, error) { return n.Tracker.ShowStory(ctx, storyID) })
	if err != nil {
		return report, fmt.Errorf("closing out %s: %w", storyID, err)
	}
	report.Title, report.Assignee = detail.Story.Title, detail.Assignee
	report.tester = IsTesterStory(detail)
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
		began:    began,
	}
	report.Target = c.target

	// A story this host has already landed, and could not close, is finished
	// with everything but the close. Running mw next again must close it —
	// without merging, testing, pushing or ledgering anything a second time.
	if was, err := n.Tracker.StoryState(ctx, storyID, RunState); err == nil && was == RunLanded {
		return n.closeALanding(ctx, c, &report)
	} else if err == nil && was == RunQueued {
		// Queued by an earlier run, on a rig that lands in batches: its batch
		// lands it, and nothing is pushed, ledgered or dispatched twice.
		return report, fmt.Errorf("closing out %s: it is queued for batch landing already (%s=%s), so nothing was pushed, ledgered or dispatched again: its batch lands it",
			storyID, RunState, RunQueued)
	}
	// The run=landed write can fail after the push, and then the story carries
	// no marker. What a landing does leave is its ledger line, written before
	// the close was tried, and the branch merged into the target.
	if n.ledgeredAsLanded(ctx, c, &report) {
		return n.closeALanding(ctx, c, &report)
	}
	// A Tester story an earlier run closed out and could not close has no
	// branch left to read: only the close is left to do.
	if IsTesterStory(detail) {
		if tested, err := n.testedAlready(ctx, storyID); err != nil {
			report.Notes = append(report.Notes, err.Error())
		} else if tested {
			return n.closeATested(ctx, c, &report)
		}
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
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return n.stop(ctx, c, report, ReasonNoResult, fmt.Sprintf("the session's result could not be read: %v", err), "")
	}
	result, readErr := ReadSessionResult(printed)
	if readErr != nil {
		// A usage limit can stop the harness before it writes its JSON: what it
		// printed instead, or its pane, still names the limit (mw-gq6.471).
		if stop, stopped := n.harnessStopWithoutResult(ctx, c, report, printed); stopped {
			return n.harnessStopped(ctx, c, report, stop)
		}
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return n.stop(ctx, c, report, ReasonNoResult, fmt.Sprintf("the session left no result at %s, so it never started or it died before it could write one",
			resultPath), "")
	case strings.TrimSpace(printed) == "":
		why, said := n.emptyResult(ctx, c, resultName, resultPath)
		return n.stop(ctx, c, report, ReasonNoResult, why, said)
	case readErr != nil:
		return n.stop(ctx, c, report, ReasonNoResult, fmt.Sprintf("the session's result could not be read: %v", readErr), "")
	}
	c.result, report.Result = result, result
	fetched := false
	if CappedAtTurns(result) {
		// The session ran to its turn cap and its harness stopped it
		// (mw-1ljpo9.6): it is not waiting for a person, so stop gives the
		// claim back and the next attempt keeps this one's branch. Unless it
		// wrote its closing comment first: the close-out turns count against
		// the cap, so that session is done and the gate below judges it
		// (mw-gq6.595).
		if !n.cappedAfterClosing(ctx, c, report) {
			return n.stop(ctx, c, report, ReasonTurnCap, TurnCapWhy(result.Turns), "")
		}
		c.cappedAfterClosing, fetched = true, true
	} else if !result.Finished() {
		// Stopped by the harness for something that is not the work's: the
		// claim is kept as for any stop, and mw dispatch gives it back itself
		// once the stop's time has come, the attempt not counted (mw-gq6.434).
		if stop, stopped := HarnessStopOf(result, sessionEnded(c.detail, result, n.now())); stopped {
			return n.harnessStopped(ctx, c, report, stop)
		}
		return n.stop(ctx, c, report, ReasonSessionFailed, "the session did not finish: "+result.Trouble(), "")
	}

	// The target branch as the remote has it now is what the work is measured
	// against: the other host may have landed on it while this story was worked.
	if !fetched {
		if err := n.fetchRetrying(ctx, report, c.rigDir); err != nil {
			return n.stop(ctx, c, report, ReasonGitFailed, fmt.Sprintf("the rig could not be brought up to date with %s: %v", n.remote(), err), "")
		}
	}
	n.readLanding(ctx, c, report)
	if found := n.refusals(ctx, c, report, false); len(found) > 0 {
		if c.cappedAfterClosing {
			// Cut at the cap and not done: given back as any capped session is,
			// with what the close-out found said beside it (mw-gq6.595).
			return n.stop(ctx, c, report, ReasonTurnCap, TurnCapWhy(result.Turns),
				"It wrote its closing comment before the cap, but the close-out refused it as "+string(found[0].Reason)+": "+found[0].Why)
		}
		if found[0].Reason == ReasonMainRed && c.mainRed != nil {
			if rep, held := n.holdOnMainRed(ctx, c, report, c.mainRed, ""); held {
				return rep, nil
			}
		}
		report.Refused = true
		return n.stop(ctx, c, report, found[0].Reason, found[0].Why, found[0].Said)
	}
	if IsTesterStory(c.detail) {
		return n.closeTested(ctx, c, report)
	}
	n.rereadLanding(ctx, c, report)
	if c.batch {
		return n.queue(ctx, c, report)
	}

	n.readBackend(ctx, c, report)
	var smokeNotes []string
	c.smokeApps, smokeNotes = n.Smoke.Wants(ctx, c.path.Rig, c.rigDir, StartPoint(n.remote(), c.target), c.branch)
	report.Notes = append(report.Notes, smokeNotes...)
	n.readSmokeElsewhere(ctx, c, StartPoint(n.remote(), c.target), c.branch)
	n.readFilesOutsideTheKey(ctx, c, report)

	// Only one close-out at a time may touch a rig's target branch: this host's
	// own slot keeps this host's out, and inside it the rig's slot across hosts
	// keeps the other host's (mw-gq6.359). The slot across hosts is the wider,
	// so it is taken last and given back first: a close-out waiting in line on
	// this host never keeps the other host waiting too (mw-gq6.421), nor does
	// one whose gate on the merged result waits for this host's gate lock or
	// calm, which gives it back while it waits (mw-gq6.422). Whatever race gets
	// past both is settled by the remote itself, below.
	holding, err := n.Slot.Take(ctx, c.rigDir, Holders(n.Seat, n.Host, c.id))
	if errors.Is(err, ErrSlotCap) {
		// Waited the overall most, on a live holder or a queue of them: the
		// work is not at fault, so only the close-out is run again (mw-gq6.467).
		return n.handBack(ctx, c, report, fmt.Sprintf("the merge slot of %s could not be taken: %v", c.path.Rig, err))
	}
	if err != nil {
		return n.stop(ctx, c, report, ReasonMergeSlot, fmt.Sprintf("the merge slot of %s could not be taken: %v", c.path.Rig, err), "")
	}
	n.takeCrossSlot(ctx, c, report)
	landed, landErr := n.merge(ctx, c, report)
	report.Notes = append(report.Notes, c.cross.Release(ctx)...)
	c.cross = nil
	if c.merged.lock != nil {
		n.giveTheGateLockBack(report, c.merged.lock)
	}
	if err := holding.Release(ctx); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the merge slot of %s could not be given back: %v", c.path.Rig, err))
	}
	if landErr != nil {
		// A landing cut off by its caller failed for the caller, not the
		// story: it is neither sent back nor stopped (mw-gq6.441).
		if ctx.Err() != nil {
			return n.interrupted(ctx, c, report)
		}
		kept := n.keepLandingError(ctx, c, report, landErr)
		switch {
		case Conflicted(landErr):
			// Named in the send-back and in the refusal alike, and kept on the
			// story for mw report (mw-gq6.559).
			if line := n.recordConflictFiles(ctx, c, report, ConflictFiles(landErr)); line != "" {
				kept = line + "\n\n" + kept
			}
			whyNot := n.sendBack(ctx, c, report, landErr, kept)
			if whyNot == "" {
				return *report, nil
			}
			kept = "It was not sent back to rebase: " + whyNot + "\n\n" + kept
		case reasonOf(landErr) == ReasonMergedTestsFail:
			// The tests that failed on the merged result may fail on the target
			// branch alone: that is main's red, not the story's (mw-gq6.645).
			if red := n.mainAlone(ctx, c, report, c.mergedFailed, StartPoint(n.remote(), c.target)); red != nil {
				if rep, held := n.holdOnMainRed(ctx, c, report, red, kept); held {
					return rep, nil
				}
			}
			whyNot := n.sendBackForFailingTests(ctx, c, report, landErr, kept)
			if whyNot == "" {
				return *report, nil
			}
			kept = "It was not sent back to fix the tests: " + whyNot + "\n\n" + kept
		}
		return n.stop(ctx, c, report, reasonOf(landErr), firstLine(landErr.Error()), kept)
	}
	report.Landed, report.How = true, landed
	n.advanceRig(ctx, c, report)

	// The work is on the target branch at the remote from here: nothing below
	// is undone, and nothing below stops the story being closed.
	outcome := fmt.Sprintf("%s, %d commits, mw next re-ran the rig's tests: pass", landed.LandedAs(c.target), report.Commits)
	if c.cappedAfterClosing {
		outcome += "; " + LandedAfterTurnCap
	}

	// Written before anything else, because it is what tells a later run that
	// this story is landed. Everything after the push can fail — the close did,
	// on the run this was written for — and a run that cannot tell a landed
	// story from an unlanded one would merge it all over again.
	n.writeOrSay(ctx, c, report, "the run state", fmt.Sprintf("%s could not be recorded as %s=%s", c.id, RunState, RunLanded),
		func() error { return n.Tracker.SetStoryState(ctx, c.id, RunState, RunLanded, outcome) })
	// Before the after-landing command, which may be killed with the session it
	// runs in: the stamp is queued as soon as the work is known to be landed.
	n.queueStamp(ctx, c, report, landed)
	n.sayFilesOutsideTheKey(ctx, c, report)
	// After the story is recorded as landed, so that a command that is killed
	// with the session it runs in leaves a story a later run can tell is landed.
	n.afterLanding(ctx, c, report)
	n.smoke(ctx, c, report)
	n.stageBackend(ctx, c, report)
	n.springTester(ctx, c, report, landed)
	return n.finish(ctx, c, report, outcome, false)
}

// takeCrossSlot waits for the rig's merge slot across hosts and takes it, and
// the report says what became of it.
func (n Next) takeCrossSlot(ctx context.Context, c *closeOut, report *NextReport) {
	cross, said := n.CrossSlot.Take(ctx, c.path.Rig, Holders(n.Seat, n.Host, c.id))
	report.CrossSlot = append(report.CrossSlot, said...)
	c.cross = cross
}

// waitOffTheSlot gives the rig's merge slot across hosts back while the gate on
// a merged result waits for this host, for waits, and takes it again once it
// is done waiting (mw-gq6.422): the other host lands meanwhile, and the next
// try starts over from a fresh fetch. It is no try lost, since nothing was
// pushed, and it cannot come round for ever: the gate lock, once had, is kept,
// and the host is let calm once before each run.
func (n Next) waitOffTheSlot(ctx context.Context, c *closeOut, report *NextReport, waits string) {
	report.Notes = append(report.Notes, c.cross.Release(ctx)...)
	c.cross = nil
	report.CrossSlot = append(report.CrossSlot, fmt.Sprintf("gave the cross-host merge slot of %s back while it waited for %s before %s",
		c.path.Rig, waits, c.merged.what()))
	n.waitForTheMergedGate(ctx, c, report)
	n.takeCrossSlot(ctx, c, report)
}

// offTheSlot is a try at a landing that stopped before its gate on the merged
// result, which would have waited for this host, for waits, while holding the
// rig's merge slot across hosts.
type offTheSlot struct{ waits string }

func (o offTheSlot) Error() string {
	return "the rig's tests on the merged result wait for " + o.waits + ", which is not waited for under the merge slot across hosts"
}

// queueStamp leaves a chain stamp of the landed commit for the follower's
// chain-stamp job. It never fails the landing: a queue that will not take the
// stamp is a note on the report, and the landing stands.
func (n Next) queueStamp(ctx context.Context, c *closeOut, report *NextReport, landed Landed) {
	if n.Stamps == nil {
		return
	}
	stamp := domain.Stamp{
		Rig: c.path.Rig, Branch: c.target, Commit: landed.Commit,
		Story: c.id, Title: c.detail.Story.Title, Host: n.Host, At: n.now().UTC(),
	}
	if err := n.Stamps.Append(ctx, stamp); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("chain stamp: not queued: %s", firstLine(err.Error())))
	}
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

// harnessStopped stops a close-out for a harness stop. One that is an expired
// login also holds this host's new claims and tells the Mayor, once per host
// (holdHostForLogin), so that the next story is not claimed to fail the same way
// (mw-gq6.781).
func (n Next) harnessStopped(ctx context.Context, c *closeOut, report *NextReport, stop HarnessStop) (NextReport, error) {
	notes, told := holdHostForLogin(ctx, n.Memory, n.Mailbox, n.Host, c.id, stop, n.now())
	report.Notes = append(report.Notes, notes...)
	c.loginTold = told
	return n.stop(ctx, c, report, stop.Reason, n.harnessStopWhy(stop), "")
}

// harnessStopWhy is why a close-out stopped for a harness stop.
func (n Next) harnessStopWhy(stop HarnessStop) string {
	return fmt.Sprintf("the harness stopped the session, not its work: %s. The attempt is not counted: mw dispatch on %s claims nothing new until %s, "+
		"then gives this story back itself", stop.Said, n.Host, stop.Until.UTC().Format(time.RFC3339))
}

// harnessStopWithoutResult reads whether the harness stopped a session that
// left no JSON at the usage limit (mw-gq6.471): from what it printed where its
// JSON belonged, and the tail of its pane. A stop found is kept in the run
// (HarnessStopFileName) for mw dispatch, retry and status to read once the
// pane is gone; one that cannot be kept is a note, and the stop stands.
func (n Next) harnessStopWithoutResult(ctx context.Context, c *closeOut, report *NextReport, printed string) (HarnessStop, bool) {
	ended := n.now()
	stop, stopped := HarnessStopOfWords(printed+"\n"+n.paneTail(ctx, c), ended)
	if !stopped {
		return HarnessStop{}, false
	}
	name := HarnessStopFileNameForAttempt(c.attempt())
	if _, err := n.Vault.PutRunFile(ctx, c.id, name, HarnessStopKept(ended, stop.Said)); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the harness's stop could not be kept in the run of %s, so mw dispatch may not read it: %v", c.id, err))
	} else {
		c.harnessStop = name
	}
	return stop, true
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
// conflicts: a fresh session of the seat, in the same worktree, told to rebase
// onto the target branch as the remote has it, resolve, run the suite and
// commit, and the mw next it ends with lands it as usual. The branch was never
// pushed, so the rebase forces nothing.
//
// A rebase is only worth another session when the target branch has moved since
// the last one: a story is sent back again when its head differs from the one
// recorded at the last send-back, up to MaxRebaseSends times in all. The count
// and the head are recorded on the story before anything is started, so that it
// cannot loop: one that cannot be recorded is not sent. What it returns is why
// it was not sent, empty when it was; the close-out then stops as for any
// conflict.
func (n Next) sendBack(ctx context.Context, c *closeOut, report *NextReport, landErr error, kept string) string {
	if n.Runner == nil || n.Boot.Harness == nil {
		return "this mw next has no session to send it back to"
	}
	was, err := readRetrying(ctx, n, report, "the rebase state", func() (string, error) { return n.Tracker.StoryState(ctx, c.id, RebaseState) })
	if err != nil {
		return fmt.Sprintf("whether it was sent back before could not be read: %v", err)
	}
	sends := c.detail.RebaseSends
	if sends == 0 && was == RebaseSentBack {
		// Sent back before the count was kept: once, against a head not known.
		sends = 1
	}
	if sends >= MaxRebaseSends {
		return fmt.Sprintf("it was sent back to rebase %d times already, which is the most a story is sent back. The conflict is a person's to resolve now", sends)
	}

	onto := StartPoint(n.remote(), c.target)
	head, err := n.Landing.RefHead(ctx, c.rigDir, onto)
	if err != nil {
		return fmt.Sprintf("the head of %s could not be read: %v", onto, err)
	}
	if sends > 0 && head == c.detail.RebaseBase {
		return fmt.Sprintf("it was sent back %s to rebase already, onto %s at %s, and %s has not moved since, so another rebase would change nothing. The conflict is a person's to resolve now",
			times(sends), onto, shortSHA(head), c.target)
	}
	spec, err := n.Boot.Rebase(ctx, c.detail, c.worktree, onto)
	if err != nil {
		return fmt.Sprintf("the session to rebase it could not be assembled: %v", err)
	}
	if err := n.Landing.UnionNotes(ctx, c.worktree); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("CLAUDE.md could not be set to merge by mw's trim-safe driver in %s: %v", c.worktree, err))
	}
	sendFields := map[string]string{
		RebaseSendsField: strconv.Itoa(sends + 1), RebaseBaseField: head,
		SentBackAtField: n.now().UTC().Format(time.RFC3339),
	}
	if err := writeRetrying(ctx, n, report, "the send-back's record", func() error { return n.Tracker.SetStoryMetadata(ctx, c.id, sendFields) }); err != nil {
		return fmt.Sprintf("it could not be recorded as sent back to rebase against %s, which is what bounds the send-backs: %v", shortSHA(head), err)
	}
	rebaseWhy := fmt.Sprintf("sent back to rebase onto %s (%s, send-back %d of %d): %s", onto, shortSHA(head), sends+1, MaxRebaseSends, firstLine(landErr.Error()))
	if err := writeRetrying(ctx, n, report, "the rebase state", func() error {
		return n.Tracker.SetStoryState(ctx, c.id, RebaseState, RebaseSentBack, rebaseWhy)
	}); err != nil {
		return fmt.Sprintf("it could not be recorded as %s=%s: %v", RebaseState, RebaseSentBack, err)
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
		"It was sent back (send-back %d of %d, against %s at %s) to a fresh Builder session, %s, in the worktree %s, to rebase %s onto %s, resolve, "+
		"run the suite and commit. The mw next that session ends with lands it as usual. If it conflicts again it is sent back again only "+
		"if %s has moved by then, and never more than %d times in all; otherwise the conflict stops the close-out and is a person's to resolve. "+
		"Nothing was merged, nothing was pushed and nothing was forced.\n\n%s",
		n.Host, ReasonMergeConflict, c.branch, c.target, c.target, sends+1, MaxRebaseSends, onto, shortSHA(head), spec.Name, c.worktree, c.branch, onto,
		c.target, MaxRebaseSends, kept)
	n.writeOrSay(ctx, c, report, "the send-back on the story", "the send-back could not be written on the story",
		func() error { return n.Tracker.CommentOnStory(ctx, c.id, note) })
	n.writeOrSay(ctx, c, report, "the run state", fmt.Sprintf("%s could not be recorded as %s=%s", c.id, RunState, RunRunning),
		func() error { return n.Tracker.SetStoryState(ctx, c.id, RunState, RunRunning, where) })
	// The line charges the session that worked the story, whose result the
	// fresh one will write over; the fresh one is charged by its own line.
	if err := n.ledger(ctx, c, report, NotLanded(ReasonMergeConflict, "sent back to rebase onto "+onto+" in session "+spec.Name)); err != nil {
		report.Notes = append(report.Notes, err.Error())
	}
	n.commit(ctx, c, report)
	n.mailTheMayor(ctx, c, report, MailSentBack, note)
	return ""
}

// ConflictedFilesLine is the one line a story's comment names the files a
// merge conflicted in by; empty when none are known.
func ConflictedFilesLine(files []string) string {
	if len(files) == 0 {
		return ""
	}
	return "Conflicted files: " + strings.Join(files, ", ")
}

// conflictedSuffix is ". Conflicted files: a, b" for a merge that conflicted
// and said where, to end a sentence that quotes its error; empty otherwise.
func conflictedSuffix(err error) string {
	if line := ConflictedFilesLine(ConflictFiles(err)); line != "" {
		return ". " + line
	}
	return ""
}

// recordConflictFiles writes the files a merge conflicted in onto the story
// (ConflictFilesField) and returns the line that names them for its comment;
// both are empty when the landing did not say which files. A record that
// cannot be written is a note, not a reason to stop.
func (n Next) recordConflictFiles(ctx context.Context, c *closeOut, report *NextReport, files []string) string {
	if len(files) == 0 {
		return ""
	}
	n.writeOrSay(ctx, c, report, "the conflicted files", "the files the merge conflicted in could not be recorded on the story",
		func() error {
			return n.Tracker.SetStoryMetadata(ctx, c.id, map[string]string{ConflictFilesField: strings.Join(files, ",")})
		})
	return ConflictedFilesLine(files)
}

// times says a count of send-backs in words: "once", "2 times".
func times(count int) string {
	if count == 1 {
		return "once"
	}
	return fmt.Sprintf("%d times", count)
}

// shortSHA is a commit as a person names it.
func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
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
		// A story sent back before left its earlier session under this name,
		// ended, since the one running now is its successor; a name can be
		// held by one session only.
		if err := n.Runner.Close(ctx, aside); err != nil {
			return "", err
		}
		return aside, n.Runner.Rename(ctx, name, aside)
	case StateExited, StateExitUnknown:
		return "", n.Runner.Close(ctx, name)
	}
	return "", nil
}

// sendBackForFailingTests answers a branch that merges into its target branch
// without conflicts but whose merged result fails the rig's tests the one time
// it is allowed to: onto — the target branch as the remote has it — is merged
// into the story's own branch, in its own worktree, because that merge is
// already known to have no conflicts and only the tests are left broken; a
// fresh session of the seat is sent back to find why they fail on the merged
// result, fix it, run the suite and commit. The mw next it ends with lands it
// as usual.
//
// Once only, and recorded on the story before anything is started, so that it
// cannot loop: a story already sent back for this reason, or one that cannot be
// recorded as sent back, is not sent. What it returns is why it was not sent,
// empty when it was; the close-out then stops as for any merged-tests-fail.
func (n Next) sendBackForFailingTests(ctx context.Context, c *closeOut, report *NextReport, landErr error, kept string) string {
	if n.Runner == nil || n.Boot.Harness == nil {
		return "this mw next has no session to send it back to"
	}
	was, err := readRetrying(ctx, n, report, "the merged-tests state", func() (string, error) { return n.Tracker.StoryState(ctx, c.id, MergedTestsState) })
	if err != nil {
		return fmt.Sprintf("whether it was sent back before could not be read: %v", err)
	}
	if was == MergedTestsSentBack {
		return "it was sent back once to fix the merged tests already, and a story is sent back only once. The failure is a person's to resolve now"
	}

	onto := StartPoint(n.remote(), c.target)
	if _, err := n.Landing.Merge(ctx, c.worktree, onto); err != nil {
		return fmt.Sprintf("%s could not be merged into the worktree to send it back: %v%s", onto, err, conflictedSuffix(err))
	}
	spec, err := n.Boot.MergeFix(ctx, c.detail, c.worktree, onto)
	if err != nil {
		return fmt.Sprintf("the session to fix the tests could not be assembled: %v", err)
	}
	next := c.detail.Attempts + 1
	sentBackAt := map[string]string{SentBackAtField: n.now().UTC().Format(time.RFC3339)}
	if err := writeRetrying(ctx, n, report, "the time it was sent back", func() error { return n.Tracker.SetStoryMetadata(ctx, c.id, sentBackAt) }); err != nil {
		return fmt.Sprintf("the time it was sent back could not be recorded, which is what the quiet alarm counts from: %v", err)
	}
	sentBackWhy := fmt.Sprintf("sent back as attempt %d to fix the merged tests, %s merged in: %s", next, onto, firstLine(landErr.Error()))
	if err := writeRetrying(ctx, n, report, "the merged-tests state", func() error {
		return n.Tracker.SetStoryState(ctx, c.id, MergedTestsState, MergedTestsSentBack, sentBackWhy)
	}); err != nil {
		return fmt.Sprintf("it could not be recorded as %s=%s, which is what keeps it to once: %v", MergedTestsState, MergedTestsSentBack, err)
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
		return fmt.Sprintf("the session %s to fix the tests could not be started: %v", spec.Name, err)
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
	report.SentBack, report.Reason, report.Why = spec.Name, ReasonMergedTestsFail, firstLine(landErr.Error())
	where := fmt.Sprintf("sent back to fix the merged tests by mw on %s: session %s in %s on %s, %s merged in, attempt %d",
		n.Host, spec.Name, c.worktree, c.branch, onto, next)
	note := fmt.Sprintf("mw next on %s did not land this story (%s): %s and %s do not pass the rig's tests together.\n\n"+
		"%s has been merged into %s in its worktree, since that merge on its own has no conflicts — only the tests fail. "+
		"It was sent back once, as attempt %d, to a fresh Builder session, %s, in the worktree %s, to find why the "+
		"tests fail on the merged result, fix it, run the suite and commit. The mw next that session ends with lands "+
		"it as usual. It is not sent back again: a second failure here stops the close-out and is a person's to "+
		"resolve. Nothing was pushed.\n\n%s",
		n.Host, ReasonMergedTestsFail, c.branch, c.target, onto, c.branch, next, spec.Name, c.worktree, kept)
	n.writeOrSay(ctx, c, report, "the send-back on the story", "the send-back could not be written on the story",
		func() error { return n.Tracker.CommentOnStory(ctx, c.id, note) })
	n.writeOrSay(ctx, c, report, "the run state", fmt.Sprintf("%s could not be recorded as %s=%s", c.id, RunState, RunRunning),
		func() error { return n.Tracker.SetStoryState(ctx, c.id, RunState, RunRunning, where) })
	// The line charges the session that worked the story, whose result the
	// fresh one will write over; the fresh one is charged by its own line.
	if err := n.ledger(ctx, c, report, NotLanded(ReasonMergedTestsFail,
		fmt.Sprintf("sent back as attempt %d to fix the merged tests, %s merged into %s, in session %s", next, onto, c.branch, spec.Name))); err != nil {
		report.Notes = append(report.Notes, err.Error())
	}
	n.commit(ctx, c, report)
	n.mailTheMayor(ctx, c, report, MailSentBack, note)
	return ""
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
		report.Rig, report.RigDir = Advanced{Held: "git said: " + firstLine(err.Error())}, c.rigDir
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

// refusalsRead reads the branch a session left against everything mw next asks of
// it before landing, in the order it asks: commits, whom they are signed by,
// the formula's steps and the rig's own tests in the worktree. A branch with no
// commits is refused there and then, because there is nothing to sign, close or
// test. With all false it stops at the first refusal, as a close-out does,
// because a commit message is permanent and the tests are the slow part; with
// all true it goes on to the rest, which is what a session checking its own
// branch wants to be told at once. It reads and never writes: all it changes
// is the report it fills in.
func (n Next) refusalsRead(ctx context.Context, c *closeOut, report *NextReport, all bool) []Refusal {
	if IsTesterStory(c.detail) {
		return n.testerRefusals(ctx, c, report, all)
	}
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

	// What the commits add: build output or a very large file stays in every
	// clone for good once landed, so it is turned away before the formula, the
	// tests and the merge (mw-gq6.517).
	if refusal, refused := n.trackedBinaryRefusal(ctx, c, report); refused && refuse(refusal.Reason, refusal.Why, refusal.Said) {
		return found
	}

	// What the commits write into a lockfile: a git+ssh:// line is one the
	// next machine without a GitHub key cannot install, and the one sed that
	// mends it is named while the branch is still the session's (mw-gq6.740).
	if refusal, refused := n.lockfileSSHRefusal(ctx, c, report); refused && refuse(refusal.Reason, refusal.Why, refusal.Said) {
		return found
	}

	// What the formula says the session was to do. A step still open is a step
	// the session did not do, whatever the code looks like.
	// A check (all) only reads: it says the closing-comment step would be closed.
	closing := closeClosingStep
	if all {
		closing = previewClosingStep
	}
	if refusal, open := n.openStepsRefusal(ctx, c, report, closing, base); open {
		if refuse(refusal.Reason, refusal.Why, refusal.Said) {
			return found
		}
	} else if refusal, missing := n.closingCommentRefusal(ctx, c, report, base); missing && refuse(refusal.Reason, refusal.Why, refusal.Said) {
		// A finished formula is not a closing comment: the story is read for one.
		return found
	}

	// A batch landing trusts mw check's own run at this very commit, and does
	// not run the rig's tests, or the changed specs after them, a second time:
	// the batch's gate runs them again on the merged result (plans/0117, Q2).
	if c.trusted != "" {
		return found
	}

	// What the rig itself says about the work, in the worktree the session left.
	gate := n.gate(ctx, c, report, c.worktree, "the rig's tests", RelatedSpan{Base: base})
	where, mergedRun := c.worktree, false
	if gate.Err == nil && !gate.Checked.NotRun && !gate.Checked.Passed && (all || c.batch) {
		// A rig that lands in batches has no merge of its own here, and a check
		// only reads, so the target is merged in a throwaway landing worktree
		// and the tests run there once more, before anything is refused
		// (mw-gq6.553).
		if merged, dir, ok := n.batchRunOnTheMerge(ctx, c, report, gate.Checked, base); ok {
			gate, where, mergedRun = merged, dir, true
		}
	}
	checked, err := gate.Checked, gate.Err
	switch {
	case err != nil:
		refuse(ReasonTestsNotRun, fmt.Sprintf("the rig's tests could not be run in %s: %v", c.worktree, err), "")
	case checked.NotRun:
		refuse(ReasonTestsNotRun, fmt.Sprintf("the rig's tests could not be run in the worktree: `%s` did not start, so this host is missing something the command needs (a toolchain not on its PATH?)", checked.Command),
			"The last lines of `"+checked.Command+"` in "+c.worktree+":\n\n```\n"+checked.Tail(CheckLines)+"\n```")
	case !checked.Passed && !all && !c.batch && n.failureGoesOnToTheMerge(ctx, c, report, checked, base):
		// Not a refusal: the merged result is what lands, and it is tested next.
	case !checked.Passed:
		reason, why := ReasonTestsFail, fmt.Sprintf("the rig's tests fail in the worktree: `%s` did not pass", checked.Command)
		if mergedRun {
			why = fmt.Sprintf("the rig's tests fail on %s merged into the branch: `%s` did not pass", c.target, checked.Command)
		}
		// A worktree that already carries the target merged in, because it was
		// sent back once to fix the merged tests, is the merged result: a
		// failure here is the same failure again, not a fresh one.
		if sentBack, _ := n.Tracker.StoryState(ctx, c.id, MergedTestsState); sentBack == MergedTestsSentBack {
			reason = ReasonMergedTestsFail
			why = fmt.Sprintf("second merged-tests-fail: %s and %s still do not pass the rig's tests together: `%s` did not pass",
				c.branch, c.target, checked.Command)
		}
		if gate.Said != "" {
			why += " (" + gate.Said + ")"
		}
		if failing := FailingTestsShort(checked.Output); failing != "" {
			why += " [" + failing + "]"
		}
		if all && !mergedRun {
			if behind := n.commitsBehind(ctx, c, base); behind > 0 {
				why += fmt.Sprintf("; %s has %d commits this branch was cut before, which may fix it: mw next goes on to the merged result with them", c.target, behind)
			}
		}
		detail := n.failedTestsDetail(ctx, c, report, checked, where)
		// A failure the target branch has alone is main's red, not the story's:
		// a close-out holds the story on main's fix instead of refusing it, and
		// a check says so (mw-gq6.645).
		if red := n.mainAlone(ctx, c, report, checked, base); red != nil {
			why = fmt.Sprintf("main is red: %s fail on %s alone too (`%s`), so the failure is not this branch's", red.tests(), c.target, checked.Command)
			if all {
				why += "; mw next holds the story on main's fix and runs its close-out again once that is closed"
			} else {
				c.mainRed = red
			}
			reason = ReasonMainRed
		}
		refuse(reason, why, detail)
	}

	// The specs the story added or changed, which the rig's gate does not run
	// (mw-gq6.366), once the gate itself has passed.
	if err == nil && checked.Passed {
		if refusal, stop := n.specsRefusal(ctx, c, report, c.worktree, base, "in the worktree", n.specsFailReason(ctx, c)); stop {
			refuse(refusal.Reason, refusal.Why, refusal.Said)
		}
	}
	return found
}

// commitsBehind is how many commits the target branch, as this rig last saw the
// remote's, has that the story's branch was cut before; zero when it cannot be
// told.
func (n Next) commitsBehind(ctx context.Context, c *closeOut, base string) int {
	behind, err := n.Landing.Ahead(ctx, c.rigDir, base, c.branch)
	if err != nil {
		return 0
	}
	return behind
}

// failureGoesOnToTheMerge says a failed run of the rig's tests in the worktree
// is not yet a refusal (mw-gq6.552): the target branch has commits the branch
// was cut before, one of which may fix the very test that failed, and it is the
// merged result that lands. The failure is noted, the merge is made and the
// merged result is tested (push), and only a failure there refuses; the
// worktree's run alone guards a branch whose target has not moved, as before.
// A rig that lands in batches is excepted: its queue makes no merge of its own,
// so batchRunOnTheMerge tests the merge in a landing worktree instead, as a
// check does, which only reads.
func (n Next) failureGoesOnToTheMerge(ctx context.Context, c *closeOut, report *NextReport, checked Checked, base string) bool {
	behind := n.commitsBehind(ctx, c, base)
	if behind == 0 {
		return false
	}
	c.worktreeFailed = true
	note := fmt.Sprintf("the rig's tests failed in the worktree (`%s`", checked.Command)
	if failing := FailingTestsShort(checked.Output); failing != "" {
		note += ": " + failing
	}
	note += fmt.Sprintf("), but %s has %d commits this branch was cut before, so they are run on the merged result instead", c.target, behind)
	report.Notes = append(report.Notes, note)
	n.print("  note    " + note + "\n")
	return true
}

// batchRunOnTheMerge is failureGoesOnToTheMerge for a rig that lands in
// batches (mw-gq6.553): its queue merges nothing, so when the rig's tests failed
// in the worktree and the target has commits the branch was cut before, one of
// which may fix the failing test, the target is merged into a throwaway landing
// worktree, which is taken away again, and the tests are run there once. It
// reports that run and where it was made; ok is false, and the worktree's own
// failure stands, when the target has not moved or the merge could not be made.
// A story that really breaks a test fails there too, so it never reaches a
// batch untested on the merged code.
func (n Next) batchRunOnTheMerge(ctx context.Context, c *closeOut, report *NextReport, failed Checked, base string) (merged gated, dir string, ok bool) {
	behind := n.commitsBehind(ctx, c, base)
	if behind == 0 {
		return gated{}, "", false
	}
	dir, err := n.Landing.OpenLanding(ctx, c.rigDir, base)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("a landing of %s at %s could not be opened to test the merged code: %v", c.path.Rig, base, err))
		return gated{}, "", false
	}
	defer func() {
		if closeErr := n.Landing.CloseLanding(ctx, c.rigDir, dir); closeErr != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("the landing worktree %s could not be taken away: %v", dir, closeErr))
		}
	}()
	if _, err := n.Landing.Merge(ctx, dir, c.branch); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s could not be merged into %s to test the merged code: %v%s", c.branch, c.target, err, conflictedSuffix(err)))
		return gated{}, "", false
	}
	note := fmt.Sprintf("the rig's tests failed in the worktree (`%s`", failed.Command)
	if failing := FailingTestsShort(failed.Output); failing != "" {
		note += ": " + failing
	}
	note += fmt.Sprintf("), but %s has %d commits this branch was cut before, so they are run once more with %s merged in", c.target, behind, c.target)
	report.Notes = append(report.Notes, note)
	n.print("  note    " + note + "\n")
	return n.gate(ctx, c, report, dir, "the rig's tests with the target merged in", RelatedSpan{Base: base}), dir, true
}

// ExpectedPermissionMode is the permission mode a story's session is launched
// in, which a transcript showing another has not run in.
const ExpectedPermissionMode = "auto"

// refusals is refusalsRead with the permission mode the session ran in told
// beside each refusal, when it was not the mode it was launched in
// (mw-gq6.545): such a session had every command outside its allow rules
// denied, so its formula steps stayed open or its work was never tested, and a
// refusal that says only that sends the Mayor to look for a fault in the work.
func (n Next) refusals(ctx context.Context, c *closeOut, report *NextReport, all bool) []Refusal {
	found := n.refusalsRead(ctx, c, report, all)
	if len(found) == 0 {
		return found
	}
	mode := ranInAnotherPermissionMode(ctx, n.Modes, c)
	if mode == "" {
		return found
	}
	for i := range found {
		found[i].Why += "; " + PermissionModeWhy(mode)
	}
	return found
}

// ranInAnotherPermissionMode is the mode a story's session ran in when its
// transcript shows one that is not ExpectedPermissionMode; empty for a session
// that ran in auto, or whose mode cannot be told (no transcript, no harness
// record, another harness).
func ranInAnotherPermissionMode(ctx context.Context, modes TranscriptModes, c *closeOut) string {
	if modes == nil || c.path.Harness != domain.HarnessClaude {
		return ""
	}
	mode, err := modes.PermissionMode(ctx, c.worktree)
	if err != nil || mode == "" || mode == ExpectedPermissionMode {
		return ""
	}
	return mode
}

// PermissionModeWhy is the words of a refusal for a session that was launched in
// auto and ran in mode.
func PermissionModeWhy(mode string) string {
	return fmt.Sprintf("the session ran in permission mode %s, not %s, so what it needed outside its allow rules was denied (a model its host's Claude Code cannot run in auto does this)",
		mode, ExpectedPermissionMode)
}

// closingStepMode is what openStepsRefusal does about a lone open
// closing-comment step whose comment is on the story (mw-gq6.398).
type closingStepMode int

const (
	// leaveClosingStep treats it as any open step.
	leaveClosingStep closingStepMode = iota
	// closeClosingStep closes the step, and notes that it did.
	closeClosingStep
	// previewClosingStep writes nothing: it does not refuse, and notes that
	// the close-out would close the step.
	previewClosingStep
)

// ClosingCommentStepTitle begins the title of the formula step that asks the
// Builder for the closing comment.
const ClosingCommentStepTitle = "Write the closing comment"

// openStepsRefusal is the refusal of a story whose poured formula still has a
// step open, or whose steps could not be read; false when there is none. The
// one exception, by closing: a lone open step that asks for the closing comment
// when the story already carries one written after its newest commit — the
// Builder did what the step asks and never closed it (mw-gq6.398).
func (n Next) openStepsRefusal(ctx context.Context, c *closeOut, report *NextReport, closing closingStepMode, base string) (Refusal, bool) {
	root := c.detail.Molecule.RootID
	if root == "" {
		return Refusal{}, false
	}
	open, err := readRetrying(ctx, n, report, "the formula's steps", func() ([]FormulaStep, error) { return n.Tracker.OpenSteps(ctx, root) })
	switch {
	case err != nil:
		return Refusal{ReasonTrackerFailed, fmt.Sprintf("the steps of the formula poured as %s could not be read: %v", root, err), ""}, true
	case len(open) > 0:
		if closing != leaveClosingStep && len(open) == 1 && n.closingCommentWritten(ctx, c, open[0], base) {
			if closing == previewClosingStep {
				report.Notes = append(report.Notes, ClosingStepNote(true))
				return Refusal{}, false
			}
			if err := n.Tracker.CloseStory(ctx, open[0].ID, "the closing comment is on the story (closed by mw next)"); err == nil {
				report.Notes = append(report.Notes, ClosingStepNote(false))
				return Refusal{}, false
			}
		}
		return Refusal{ReasonOpenSteps, fmt.Sprintf("%d formula step(s) of %s are still open, so the formula was not finished", len(open), root),
			"Still open:\n" + stepList(open) + n.unrunPartsSaid(ctx, c)}, true
	}
	return Refusal{}, false
}

// unrunPartsSaid is what an open-steps refusal adds when the rig's gate is split
// into parts and some part has not passed at the branch's tip: each such part
// with the command that runs it, so that whoever takes the story over runs
// them rather than the whole gate. Empty when the gate is not split, every part
// passed, or the parts cannot be read.
func (n Next) unrunPartsSaid(ctx context.Context, c *closeOut) string {
	if n.GateParts == nil || n.GateRecords == nil || n.Landing == nil {
		return ""
	}
	parts := n.GateParts.Parts(c.path.Rig)
	if len(parts) == 0 {
		return ""
	}
	gate, _ := recordedGate{records: n.GateRecords, storyID: c.id, attempt: c.attempt(), parts: parts, tip: cleanTipOf(ctx, n.Landing, c)}.Run(ctx, "", "")
	if gate.Passed {
		return ""
	}
	return "\n\nThe rig's gate is split into " + fmt.Sprint(len(parts)) + " parts and these have not passed at the branch's tip:\n" + bulletList(strings.Split(gate.Output, "\n"))
}

// closingCommentRefusal is the refusal of a story whose formula is finished but
// which carries no closing comment written after its newest commit; false when
// it has one, or has no formula (mw-gq6.630). A Builder can close the step that
// asks for the comment and leave '•' on the story, so the step is not trusted:
// the comment is read. It is a hand-back, as open steps are, for the Builder to
// mend by writing the comment. The comment it finds is read for criteria it
// says are unmet (UnmetCriteria, mw-gq6.711): that is the refusal
// ReasonUnmetCriterion, which keeps the claim for the Mayor. A comment that
// stands is then read for a live eval line of every grind example the branch
// changes (gristExampleRefusal, mw-gq6.772).
func (n Next) closingCommentRefusal(ctx context.Context, c *closeOut, report *NextReport, base string) (Refusal, bool) {
	if c.detail.Molecule.RootID == "" {
		return Refusal{}, false
	}
	text, written := n.closingCommentTextOnStory(ctx, c, base)
	if unmet := UnmetCriteria(text); written && len(unmet) > 0 {
		return Refusal{ReasonUnmetCriterion, fmt.Sprintf("the closing comment of %s says an acceptance criterion is not met: %s", c.id, unmet[0]),
			"The closing comment says, of acceptance criteria:\n" + bulletList(unmet) + "\n" +
				"A story is landed when every acceptance criterion holds when run. One that holds only in part, is not met or was not run is not a landing: " +
				"the story is handed back, and the Mayor re-scopes it or closes what is done."}, true
	}
	if written {
		return n.gristExampleRefusal(ctx, c, report, text)
	}
	return Refusal{ReasonNoClosingComment, fmt.Sprintf("the formula of %s is finished, but the story has no closing comment written after its newest commit", c.id),
		"A closing comment has HOW TO CHECK IT, for the Governor (or the line 'Internal: nothing for the Governor to look at') and 'For the rig memory:'. " +
			"A closed 'Write the closing comment' step is not one: write the comment on the story, after the last commit, and run mw next again."}, true
}

// bulletList is lines, one to a line, each after a '- '.
func bulletList(lines []string) string {
	return "- " + strings.Join(lines, "\n- ")
}

// ClosingStepNote is the note of a close-out that closed the closing-comment
// step itself, or with preview that a close-out would.
func ClosingStepNote(preview bool) string {
	if preview {
		return "would close the closing-comment step: the comment is on the story"
	}
	return "closed the closing-comment step: the comment is on the story"
}

// closingCommentWritten reports whether step asks for the closing comment and
// the story carries one — HOW TO CHECK IT (or the Internal line that stands in
// for it) and 'For the rig memory' — written after its newest commit. Anything
// it cannot read counts as no.
func (n Next) closingCommentWritten(ctx context.Context, c *closeOut, step FormulaStep, base string) bool {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(step.Title)), strings.ToLower(ClosingCommentStepTitle)) {
		return false
	}
	return n.closingCommentOnStory(ctx, c, base)
}

// closingCommentOnStory reports whether the story carries a closing comment
// written after the newest commit of its branch, whether or not the step that
// asks for it is closed (mw-gq6.630). Anything it cannot read counts as no.
func (n Next) closingCommentOnStory(ctx context.Context, c *closeOut, base string) bool {
	_, written := n.closingCommentTextOnStory(ctx, c, base)
	return written
}

// closingCommentTextOnStory is the newest closing comment written after the
// newest commit of the story's branch, and whether there is one.
func (n Next) closingCommentTextOnStory(ctx context.Context, c *closeOut, base string) (string, bool) {
	commits, err := n.Landing.Commits(ctx, c.rigDir, c.branch, base)
	if err != nil {
		return "", false
	}
	var newest time.Time
	for _, commit := range commits {
		if commit.Committed.After(newest) {
			newest = commit.Committed
		}
	}
	comments, err := n.Tracker.StoryComments(ctx, c.id)
	if err != nil {
		return "", false
	}
	var text string
	written := false
	for _, comment := range comments {
		if comment.Created.After(newest) && isClosingComment(comment.Text) {
			newest, text, written = comment.Created, comment.Text, true
		}
	}
	return text, written
}

// isClosingComment is a comment with the two sections of a Builder's closing
// comment: how the Governor checks it (or the line that says there is nothing
// to look at) and the rig memory proposals.
func isClosingComment(text string) bool {
	if !strings.Contains(text, "For the rig memory") {
		return false
	}
	return strings.Contains(text, "HOW TO CHECK IT") || strings.Contains(text, "Internal: nothing for the Governor to look at")
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
	// A story found by its ledger line carries no run=landed, because writing it
	// is what failed. The Verify card is built from it, so it is written now,
	// before the close; failing to write it never stops the close.
	if was, err := n.Tracker.StoryState(ctx, c.id, RunState); err != nil || was != RunLanded {
		n.writeOrSay(ctx, c, report, "the run state", fmt.Sprintf("%s could not be recorded as %s=%s", c.id, RunState, RunLanded),
			func() error { return n.Tracker.SetStoryState(ctx, c.id, RunState, RunLanded, outcome) })
	}
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
		n.recordBenchmark(ctx, c, report, true)
		if err := n.ledger(ctx, c, report, outcome); err != nil {
			report.Notes = append(report.Notes, err.Error())
		}
	}
	// Committed whether this run wrote the line or an earlier one did: a line
	// written by a run that could not commit it is still standing in the way of
	// every sync until somebody commits it, and this is the run that can.
	n.commit(ctx, c, report)

	var closeErr error
	if isDemo(c.detail) {
		closeErr = n.holdDemo(ctx, c, report, outcome)
	} else {
		var notes []string
		notes, closeErr = n.closeStory(ctx, c.id, outcome)
		report.Notes = append(report.Notes, notes...)
	}
	if closeErr == nil {
		what := "landed"
		if c.tested != "" {
			what = "was tested"
		}
		n.closeFormula(ctx, c, report, what)
	}
	switch {
	case closeErr != nil:
		report.NotClosed = closeErr.Error()
	case isDemo(c.detail):
		report.DemoHeld = true
	default:
		report.Closed = true
	}
	// Told once: the run that landed the story says so, and a later run that
	// only closes it has nothing new for the Mayor (it files only the bug an
	// earlier one could not).
	verdict, said := n.closingMail(ctx, c, report, again)
	if !again {
		n.mailTheMayor(ctx, c, report, verdict, said)
	}
	if closeErr != nil {
		return *report, fmt.Errorf("closing out %s: it %s, but the story could not be closed, so it is landed and still open: %w",
			c.id, outcome, closeErr)
	}

	report.Abandoned = n.abandoned(ctx, c.id, report)
	return n.carryOn(ctx, c, report)
}

// holdDemo is what landing does in place of closeStory for a demo, which only
// the Governor's "Looks good" closes: it is left open, labelled hitl and
// unclaimed, tried again the way a close is, and counts as waiting when a read
// back finds it so.
func (n Next) holdDemo(ctx context.Context, c *closeOut, report *NextReport, outcome string) error {
	tries := n.pushTries()
	var last error
	for try := 1; try <= tries; try++ {
		if try > 1 {
			if err := waitFor(ctx, n.PushWait); err != nil {
				return fmt.Errorf("%w (and waiting %s to try holding the demo again: %v)", last, n.PushWait, err)
			}
		}
		var notes []string
		notes, last = holdLandedDemo(ctx, n.Tracker, c.id, outcome, "")
		report.Notes = append(report.Notes, notes...)
		if last == nil {
			return nil
		}
		if detail, err := n.Tracker.ShowStory(ctx, c.id); err == nil && demoWaits(detail) {
			return nil
		}
	}
	if tries == 1 {
		return last
	}
	return fmt.Errorf("holding the demo failed on each of its %d tries, the last saying: %w", tries, last)
}

// closeStory closes a landed story, and when the close fails tries it again the
// way a push is tried: PushTries times in all, PushWait apart (mw-gq6.191). The
// tracker's server can be out of reach just as a landing ends, and a landed
// story left in_progress is one a later dispatch could take for work in
// flight. A close whose answer was lost but whose write went through is read
// back, and counts as closed.
//
// A story another host claimed — dispatched there and landed here, as a batch
// lands them — is refused to this host's name, and mw acts under no other:
// this host takes the claim over and closes it under its own, and says so on
// the story (mw-gq6.475). What it says is in the notes returned.
func (n Next) closeStory(ctx context.Context, id, outcome string) ([]string, error) {
	tries := n.pushTries()
	var last error
	var notes []string
	for try := 1; try <= tries; try++ {
		if try > 1 {
			if err := waitFor(ctx, n.PushWait); err != nil {
				return notes, fmt.Errorf("%w (and waiting %s to try the close again: %v)", last, n.PushWait, err)
			}
		}
		last = n.Tracker.CloseStory(ctx, id, outcome)
		var held *CloseHeldError
		if errors.As(last, &held) {
			var said []string
			said, last = n.takeOverAndClose(ctx, id, outcome, held)
			notes = append(notes, said...)
		}
		if last == nil {
			return notes, nil
		}
		if detail, err := n.Tracker.ShowStory(ctx, id); err == nil && detail.Closed() {
			return notes, nil
		}
	}
	if tries == 1 {
		return notes, last
	}
	return notes, fmt.Errorf("the close failed on each of its %d tries, the last saying: %w", tries, last)
}

// takeOverAndClose is the close of a landed story whose claim another actor
// holds: the claim is taken over by force, a comment on the story says who
// held it and who took it to close it, and the story is closed under this
// host's own name. The comment is written before the close, so a close that
// fails or is tried again later leaves the takeover recorded all the same; a
// comment that cannot be written stops nothing, and the note says so.
func (n Next) takeOverAndClose(ctx context.Context, id, outcome string, held *CloseHeldError) ([]string, error) {
	actor := held.Actor
	if actor == "" {
		actor = SeatIdentity(MwSeat, n.Host)
	}
	if err := n.Tracker.TakeOverClaim(ctx, id, held.Holder); err != nil {
		return nil, fmt.Errorf("%w; taking the claim over from %s failed: %v", held, held.Holder, err)
	}
	notes := []string{fmt.Sprintf("%s was claimed by %s: %s took the claim over to close it", id, held.Holder, actor)}
	said := fmt.Sprintf("%s held the claim on %s when it landed, and beads lets only a claim's holder close a story: "+
		"%s took the claim over from %s to close it.", held.Holder, id, actor, held.Holder)
	if err := n.Tracker.CommentOnStory(ctx, id, said); err != nil {
		notes = append(notes, fmt.Sprintf("%s: the comment saying who held its claim could not be written: %v", id, err))
	}
	return notes, n.Tracker.CloseStory(ctx, id, outcome)
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
// before either kind of retry existed. A close-out holding its rig's slot
// across hosts fetches on the first try too: the other host may have landed
// while it waited for the slot. A try whose gate on the merged result would
// wait for this host gives that slot back while it waits, and is done again
// from a fresh fetch without being counted (waitOffTheSlot).
func (n Next) merge(ctx context.Context, c *closeOut, report *NextReport) (Landed, error) {
	base := StartPoint(n.remote(), c.target)
	var version VersionBump
	var union []string
	if n.Rules != nil {
		rules, unknown, err := n.landingRequirements(ctx, c.path.Rig)
		if err != nil {
			return Landed{}, failedFor(ReasonLandingFailed, fmt.Errorf("reading the version files of %s: %w", c.path.Rig, err))
		}
		for _, key := range unknown {
			report.Notes = append(report.Notes, fmt.Sprintf("%s/%s%s has a key this mw does not know: %s (rebuild mw on %s)",
				RigsDir, c.path.Rig, ".toml", key, n.Host))
		}
		// Only mw land pushes the target branch of a rig that lands in batches
		// (plans/0117): a close-out that got this far on one, its reads of the
		// rig's file before and after the tests having failed or said otherwise,
		// is stopped before anything is merged.
		if rules.LandingSettings().Mode == domain.LandingBatch {
			why := "its file said so only when the merge read it, after the reads before and after the rig's tests"
			if c.landingReadErr != nil {
				why = c.landingReadErr.Error()
			}
			return Landed{}, failedFor(ReasonLandingFailed, fmt.Errorf("%s lands in batches, so only mw land pushes %s: nothing was merged (%s); run mw next %s again to queue it",
				c.path.Rig, c.target, why, c.id))
		}
		version.Files = rules.VersionFiles
		version.Minor = c.path.Bump == domain.BumpMinor
		union = rules.UnionMergeFiles
		if len(rules.VersionFiles) > 0 && len(rules.ChangelogFiles) > 0 {
			if version.Entry, err = n.changelogEntry(ctx, c, report); err != nil {
				return Landed{}, failedFor(ReasonLandingFailed, err)
			}
			if version.Entry.Text != "" {
				version.Changelog = rules.ChangelogFiles
			}
		}
	}
	var firstLost, lost error
	fetch := c.cross != nil
	for try := 1; try <= n.tries(); {
		if fetch {
			if err := n.fetchRetrying(ctx, report, c.rigDir); err != nil {
				return Landed{}, failedFor(ReasonGitFailed, fmt.Errorf("the rig could not be brought up to date with %s: %w", n.remote(), err))
			}
		}
		fetch = true
		dir, err := n.Landing.OpenLanding(ctx, c.rigDir, base)
		if err != nil {
			return Landed{}, failedFor(ReasonLandingFailed, fmt.Errorf("a landing of %s at %s could not be opened: %w", c.path.Rig, base, err))
		}

		landed, err := n.push(ctx, c, report, dir, version, union)
		if closeErr := n.Landing.CloseLanding(ctx, c.rigDir, dir); closeErr != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("the landing worktree %s could not be taken away: %v", dir, closeErr))
		}
		var off offTheSlot
		switch {
		case err == nil:
			return landed, nil
		case errors.As(err, &off):
			n.waitOffTheSlot(ctx, c, report, off.waits)
		case Rejected(err):
			if firstLost == nil {
				firstLost = err
			}
			lost = err
			try++
		default:
			return Landed{}, err
		}
	}
	first := ""
	if n.tries() > 1 {
		first = fmt.Sprintf(" (the first push was refused too: %s)", firstLine(firstLost.Error()))
	}
	return Landed{}, failedFor(ReasonPushLost, fmt.Errorf("%s could not be landed on %s: the other host got there first %d times running; nothing was forced%s: %w",
		c.branch, c.target, n.tries(), first, lost))
}

// landingRequirements reads the rig's file for a landing: leniently when the
// rules can, so that a key set in the vault after this mw was built does not
// stop it, with the keys it did not know beside what the known ones say.
func (n Next) landingRequirements(ctx context.Context, rig string) (domain.EpicRequirements, []string, error) {
	if lenient, ok := n.Rules.(LandingRules); ok {
		return lenient.LandingRequirements(ctx, rig)
	}
	rules, err := n.Rules.EpicRequirements(ctx, rig)
	return rules, nil, err
}

// push is one attempt at a landing, in a landing worktree that is already open:
// merge (by union, in the files the rig lists for it), raise the rig's version
// when it names files for one, test what merging
// made if it made anything new, and push. Each attempt starts from a fresh
// landing worktree at the newest base, so a push that was refused and tried
// again raises the version once, from the new base, never on top of its own.
func (n Next) push(ctx context.Context, c *closeOut, report *NextReport, dir string, version VersionBump, union []string) (Landed, error) {
	var landed Landed
	var err error
	if len(union) > 0 {
		landed, err = n.Landing.MergeUnion(ctx, dir, c.branch, union)
	} else {
		landed, err = n.Landing.Merge(ctx, dir, c.branch)
	}
	if err != nil {
		if Conflicted(err) {
			return Landed{}, failedFor(ReasonMergeConflict, fmt.Errorf("%s does not merge into %s without conflicts, which mw will not resolve for anybody: %w",
				c.branch, c.target, err))
		}
		return Landed{}, failedFor(ReasonLandingFailed, fmt.Errorf("%s could not be merged into %s: %w", c.branch, c.target, err))
	}

	// versionCommit is the commit that raised the version, which is no work of
	// the branch's and is left out of the files its related-tests line is given.
	var versionCommit string
	if len(version.Files) > 0 {
		version.Before, version.Branch, version.StoryID = landed.Before, c.branch, c.id
		bumped, err := n.Landing.BumpVersion(ctx, dir, version)
		if err != nil {
			return Landed{}, failedFor(ReasonLandingFailed, fmt.Errorf("the version of %s could not be raised: %w", c.path.Rig, err))
		}
		report.Notes = appendNew(report.Notes, bumped.Tidied...)
		if bumped.Commit != "" {
			// The version commit is what is pushed, and so what has landed. It
			// changes only the version fields and the changelog's notes, so a
			// merge that was a fast-forward stays one for the gate below: the
			// story's tests already passed on its branch, and a commit of
			// that kind on top needs no run of its own. A story that raised
			// the version itself has only its note committed, and no Version.
			landed.Commit, landed.Version = bumped.Commit, bumped.Version
			versionCommit = bumped.Commit
		}
	}

	// A merge commit is a combination of two branches that nothing has ever been
	// tested on: the story's tests passed on the story's branch, and the other
	// host's passed on its own. Only this result matters now.
	if !landed.FastForward || c.worktreeFailed {
		gate, waits := n.mergedResultGate(ctx, c, report, dir, RelatedSpan{Base: landed.Before, Skip: versionCommit})
		if waits != "" {
			return Landed{}, offTheSlot{waits}
		}
		checked, err := gate.Checked, gate.Err
		if err != nil {
			return Landed{}, failedFor(ReasonTestsNotRun, fmt.Errorf("the rig's tests could not be run on the merged result in %s: %w", dir, err))
		}
		if checked.NotRun {
			return Landed{}, failedFor(ReasonTestsNotRun, fmt.Errorf("the rig's tests could not be run on the merged result in %s: `%s` did not start, so nothing was pushed\n\n```\n%s\n```",
				dir, checked.Command, checked.Tail(CheckLines)))
		}
		if !checked.Passed && !n.codemapBandLetsIn(c, report, checked) {
			c.mergedFailed = checked
			ran := ""
			if gate.Said != "" {
				ran = " (" + gate.Said + ")"
			}
			return Landed{}, failedFor(ReasonMergedTestsFail, fmt.Errorf("%s and %s do not pass the rig's tests together: `%s` failed on the merged result, so nothing was pushed%s\n\n%s",
				c.branch, c.target, checked.Command, ran, n.failedTestsDetail(ctx, c, report, checked, dir)))
		}
	}

	// The story's changed specs again on a merged result, from the commit the
	// merge started from.
	if !landed.FastForward {
		if refusal, stop := n.specsRefusal(ctx, c, report, dir, landed.Before, "on the merged result", ReasonMergedTestsFail); stop {
			return Landed{}, failedFor(refusal.Reason, fmt.Errorf("%s and %s do not pass together: %s, so nothing was pushed\n\n%s",
				c.branch, c.target, refusal.Why, refusal.Said))
		}
	}

	if err := n.pushRetrying(ctx, report, dir, c.target); err != nil {
		return Landed{}, err
	}
	report.Tags = n.publishTags(ctx, report, dir, landed.Before, ACVersionTags(c.detail.Acceptance))
	n.fileCodemapTrim(ctx, c, report)
	return landed, nil
}

// codemapCap is the byte cap scripts/check-codemap.sh puts on docs/codemap.md,
// and codemapBandPercent how far over it a union-merged map may be for a
// landing to go through anyway (mw-gq6.598): a story whose own branch passed
// the script is not refused for a squeeze that sibling landings made, once its
// tests are otherwise green. The band is bounded so that the map cannot grow
// without end; the bug a landing in it files asks for the map to be trimmed.
const (
	codemapCap         = 10240
	codemapBandPercent = 10
)

// codemapSizeLine is the line scripts/check-codemap.sh stops at when the map is
// over its cap; its size check is the first of five, so it is the only line the
// script prints then.
var codemapSizeLine = regexp.MustCompile(`^check-codemap: docs/codemap\.md is (\d+) bytes, over the (\d+)-byte limit`)

// codemapOverTheCap is the size of the merged map when the failed run of the
// rig's tests says nothing but that it is over its cap: one check-codemap line,
// the size one, and no failing test or other check beside it.
func codemapOverTheCap(output string) (int, bool) {
	bytes := 0
	for _, line := range strings.Split(domain.StripANSI(output), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "check-codemap:"):
			found := codemapSizeLine.FindStringSubmatch(line)
			if found == nil || bytes != 0 {
				return 0, false
			}
			size, _ := strconv.Atoi(found[1])
			if limit, _ := strconv.Atoi(found[2]); limit != codemapCap {
				return 0, false
			}
			bytes = size
		case strings.HasPrefix(line, "check-"), strings.HasPrefix(line, "FAIL"), strings.HasPrefix(line, "--- FAIL"),
			strings.HasPrefix(line, "panic:"), strings.Contains(line, "*** ["):
			return 0, false
		}
	}
	return bytes, bytes > codemapCap
}

// codemapBandLetsIn says whether a failed run of the rig's tests on the merged
// result is let through: its only failure is the size of docs/codemap.md, which
// is over its cap by at most codemapBandPercent, and the story's own branch
// passed the same tests in its worktree, map included, so that a map past the
// cap is the other landings' doing. It keeps the size for the bug filed once
// the landing is pushed.
func (n Next) codemapBandLetsIn(c *closeOut, report *NextReport, checked Checked) bool {
	bytes, over := codemapOverTheCap(checked.Output)
	if !over || c.worktreeFailed || bytes > codemapCap+codemapCap*codemapBandPercent/100 {
		return false
	}
	c.codemapBytes = bytes
	note := fmt.Sprintf("docs/codemap.md is %d bytes on the merged result, %d over its %d-byte cap and inside the %d%% band, and the rest of the tests pass: landing %s, and a bug asks for the map to be trimmed",
		bytes, bytes-codemapCap, codemapCap, codemapBandPercent, c.id)
	report.Notes = append(report.Notes, note)
	n.print("  note    " + note + "\n")
	return true
}

// codemapTrimMark is what every title of the bug that asks for the map to be
// trimmed holds, and so how an open one is found.
const codemapTrimMark = "-byte cap after "

func codemapTrimTitle(over int, id string) string {
	return fmt.Sprintf("docs/codemap.md is %d bytes over its %d%s%s: merge or shorten rows", over, codemapCap, codemapTrimMark, id)
}

// fileCodemapTrim files the one open bug that asks for the map to be trimmed
// after a landing let it through over its cap, under the landed story's epic
// with its path; while such a bug is open, a landing adds its story and the
// bytes to that bug's description instead. A write that fails is a note and a
// mail to the Mayor: the landing is pushed and is never undone for it.
func (n Next) fileCodemapTrim(ctx context.Context, c *closeOut, report *NextReport) {
	if c.codemapBytes == 0 {
		return
	}
	over := c.codemapBytes - codemapCap
	lost := func(what string, err error) {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: %v", what, err))
		n.mailLostWrite(ctx, c, report, what, fmt.Sprintf("docs/codemap.md is %d bytes after %s, over its %d-byte cap, and nothing asks for it to be trimmed; file a bug for it: %v", c.codemapBytes, c.id, codemapCap, err))
	}
	var open *StoryDetail
	if lister, ok := n.Tracker.(TesterStories); ok {
		stories, err := readRetrying(ctx, n, report, "the open stories", func() ([]StoryDetail, error) { return lister.StoriesSince(ctx, time.Time{}) })
		if err != nil {
			lost("the bug that asks for docs/codemap.md to be trimmed could not be filed, the open stories being unreadable", err)
			return
		}
		for i, d := range stories {
			if !d.IsEpic && !d.Closed() && d.Merged().Rig == c.path.Rig &&
				strings.HasPrefix(d.Story.Title, "docs/codemap.md is ") && strings.Contains(d.Story.Title, codemapTrimMark) {
				open = &stories[i]
				break
			}
		}
	}
	if open != nil {
		if strings.Contains(open.Description, "after "+c.id+":") {
			return
		}
		text := strings.TrimRight(open.Description, "\n") + fmt.Sprintf("\nAlso after %s: %d bytes, %d over the cap.\n", c.id, c.codemapBytes, over)
		err := writeRetrying(ctx, n, report, "the trim bug's description", func() error {
			return n.Tracker.EditStory(ctx, open.Story.ID, StoryEdit{Description: &text})
		})
		if err != nil {
			lost("the open trim bug "+open.Story.ID+" could not be told of the landing", err)
			return
		}
		report.Notes = append(report.Notes, fmt.Sprintf("%s, the open bug that asks for docs/codemap.md to be trimmed, now names %s", open.Story.ID, c.id))
		return
	}

	path := c.path
	path.Files, path.Size, path.MaxTurns, path.Bump = "docs/codemap.md", domain.SizeXS, "", ""
	story := NewStory{
		EpicID:     c.detail.EpicID,
		Standalone: c.detail.EpicID == "",
		Title:      codemapTrimTitle(over, c.id),
		Description: fmt.Sprintf("mw next landed %s although the union-merged docs/codemap.md is %d bytes, %d over its %d-byte cap: the story's own branch passed scripts/check-codemap.sh, "+
			"and sibling landings grew the map. A landing is let in up to %d%% over the cap and no further, so trim the map before more landings fill the band.\n\n"+
			"Landed: %s, %d bytes, %d over.\n\nSource: mw next on %s (mw-gq6.598).\n",
			c.id, c.codemapBytes, over, codemapCap, codemapBandPercent, c.id, c.codemapBytes, over, n.Host),
		Acceptance: "1. scripts/check-codemap.sh exits 0, and `wc -c docs/codemap.md` is at most 10240.\n" +
			"2. make test passes.\n" +
			"Sweep: none, because the cap is one page and the next landing past the band is refused by mw next, naming check-codemap.",
		Priority:  2,
		Overrides: path,
		Labels:    []string{LabelBug},
	}
	var id string
	err := writeRetrying(ctx, n, report, "the trim bug", func() error {
		var err error
		id, err = n.Tracker.CreateStory(ctx, story)
		return err
	})
	if err == nil {
		err = writeRetrying(ctx, n, report, "the release of the trim bug", func() error { return n.Tracker.ReleaseStory(ctx, id) })
	}
	if err != nil {
		lost("the bug that asks for docs/codemap.md to be trimmed could not be filed", err)
		return
	}
	report.Notes = append(report.Notes, fmt.Sprintf("filed %s: docs/codemap.md is %d bytes over its %d-byte cap", id, over, codemapCap))
}

// changelogEntry is the note a landing writes into the rig's changelog files:
// the story's newest What's new: line, or its title when it has none, under
// today's date (UTC). Its Text is empty when the story says "What's new: none".
// The version is the landing's to fill in.
func (n Next) changelogEntry(ctx context.Context, c *closeOut, report *NextReport) (domain.ChangelogEntry, error) {
	comments, err := readRetrying(ctx, n, report, "the comments", func() ([]Comment, error) { return n.Tracker.StoryComments(ctx, c.id) })
	if err != nil {
		return domain.ChangelogEntry{}, fmt.Errorf("the comments on %s could not be read for its What's new line: %w", c.id, err)
	}
	texts := make([]string, len(comments))
	for i, comment := range comments {
		texts[i] = comment.Text
	}
	bug := domain.IsBugStory(c.detail.Type, c.detail.Story.Title) || slices.Contains(c.detail.Labels, "bug")
	kind := domain.ChangelogNew
	if bug {
		kind = domain.ChangelogFixed
	}
	note, found := domain.NewestWhatsNew(texts)
	switch {
	case found && note.None:
		return domain.ChangelogEntry{}, nil
	case found:
		if note.Kind != "" {
			kind = note.Kind
		}
	default:
		note.Text = domain.NoteFromTitle(c.detail.Story.Title, c.path.Rig)
	}
	return domain.ChangelogEntry{Date: n.now().UTC().Format("2006-01-02"), Story: c.id, Kind: kind, Text: note.Text}, nil
}

// closingMail is the verdict a landing's mail goes under and the block it
// carries, from the story's closing comments, read once: for a landing whose
// comment says an acceptance criterion is not met, the criteria marked in the
// verdict and the bug filed for them, then its common errors (mw-gq6.420) and
// its rig-memory proposals, a blank line apart. When the comments cannot be
// read the block is empty, and a landing is never refused for it. A close-out run again (again) files the bug an earlier one could not,
// and sends nothing.
func (n Next) closingMail(ctx context.Context, c *closeOut, report *NextReport, again bool) (verdict, said string) {
	verdict = MailLanded
	if c.tested != "" {
		verdict = MailTested
	}
	comments, err := n.Tracker.StoryComments(ctx, c.id)
	if err != nil {
		return verdict, ""
	}
	var unmet string
	if c.tested == "" {
		verdict, unmet = n.landedWithoutACs(ctx, c, report, again, comments)
	}
	return verdict, joinBlocks(unmet, n.commonErrors(ctx, c, comments), n.rigMemoryProposals(ctx, c, comments))
}

// rigMemoryProposals is the block from the story's newest closing comment that
// has a 'For the rig memory:' section: the proposals as mw memory lines to
// paste, or for a flat-file rig the lines to place by hand. It is empty when no
// comment has the section; a line that does not parse is flagged in the block.
func (n Next) rigMemoryProposals(ctx context.Context, c *closeOut, comments []Comment) string {
	for i := len(comments) - 1; i >= 0; i-- {
		if proposals := ParseFactProposals(comments[i].Text); proposals.Found {
			return proposals.RigMemoryBlock(c.path.Rig, c.id, n.flatRigFile(ctx, c.path.Rig))
		}
	}
	return ""
}

// flatRigFile is the rig's flat memory file when the seat keeps no facts folder
// for it, else empty; a rig whose folder cannot be read is read as a facts rig.
func (n Next) flatRigFile(ctx context.Context, rig string) string {
	if n.RigFacts == nil {
		return ""
	}
	if has, err := HasFactsFolder(ctx, n.RigFacts, n.Seat, rig); err == nil && !has {
		return FlatRigFile(n.Seat, rig)
	}
	return ""
}

// commonErrors is the block from the story's newest closing comment that has a
// 'For common errors:' section, as mw errors lines to paste, or, for a bug
// whose comments have none, the line that says so (mw-gq6.420). A register
// that cannot be read reads every class as new.
func (n Next) commonErrors(ctx context.Context, c *closeOut, comments []Comment) string {
	var proposals CommonErrorProposals
	for i := len(comments) - 1; i >= 0 && !proposals.Found; i-- {
		proposals = ParseCommonErrors(comments[i].Text)
	}
	known, kinds := map[string]bool{}, RigKinds{}
	if n.Errors != nil && proposals.Found {
		if files, kindsText, err := n.Errors.ReadErrorRegister(ctx, n.Seat); err == nil {
			for slug := range files {
				known[slug] = true
			}
			kinds, _ = ParseRigKinds(kindsText)
		}
	}
	return proposals.Block(c.path.Rig, c.id, isBugStory(c.detail), known, kinds[c.path.Rig])
}

// joinBlocks is the blocks that say something, a blank line apart.
func joinBlocks(blocks ...string) string {
	var said []string
	for _, block := range blocks {
		if block != "" {
			said = append(said, block)
		}
	}
	return strings.Join(said, "\n\n")
}

// pushRetrying pushes what the landing worktree has checked out, and tries
// again after a wait when the push itself failed on a fault at the remote
// worth trying again, up to PushTries times in all, the waits growing as a
// fetch's do (retryPause). It never retries a push the remote refused for a
// stated reason, or the race with the other host — Rejected — which merge's
// own loop answers by fetching and merging afresh; either comes back as it
// was, for merge to tell apart. Every attempt is counted on the report,
// whichever kind of failure eventually stops it, and every retry is one note.
func (n Next) pushRetrying(ctx context.Context, report *NextReport, dir, branch string) error {
	var last error
	for try := 1; try <= n.pushTries(); try++ {
		if try > 1 {
			wait := n.retryPause(try - 1)
			report.Notes = append(report.Notes, retryNote("push", last, wait))
			if err := n.retryWait(ctx, wait); err != nil {
				return failedFor(ReasonLandingFailed, fmt.Errorf("waiting %s to try the push to %s again: %w", wait, branch, err))
			}
		}
		report.Pushes++
		err := n.Landing.Push(ctx, dir, n.remote(), branch)
		if err == nil {
			return nil
		}
		if !Transient(err) {
			return err
		}
		last = err
	}
	return failedFor(ReasonLandingFailed, fmt.Errorf("the push to %s kept failing on a fault at the remote, %d time(s) running: %w",
		branch, n.pushTries(), last))
}

// FetchRetries is how many times a fetch that failed on a passing fault of the
// network is tried again before the close-out stops (mw-gq6.373).
const FetchRetries = 3

// fetchRetrying brings the rig's view of its origin up to date, and when the
// fetch failed on a passing fault — a name that would not resolve, a
// connection that timed out — tries it again FetchRetries times, a growing
// wait apart and about a minute in all with the default PushWait, one note per
// retry. A fetch that failed for any other reason comes back at once as it was.
func (n Next) fetchRetrying(ctx context.Context, report *NextReport, rigDir string) error {
	_, err := fetchWhilePassing(ctx, FetchRetries+1,
		func() error { return n.Worktrees.Fetch(ctx, rigDir) },
		n.retryPause, n.retryWait,
		func(err error, wait time.Duration) {
			report.Notes = append(report.Notes, retryNote("fetch", err, wait))
		})
	return err
}

// readRetrying runs one read of the work tracker, and when it failed on a
// passing fault of the network — a Dolt server that did not answer in time —
// tries it again ReadRetries times, a growing wait apart and under a minute in
// all with the default PushWait, one note per retry. A read that failed for any
// other reason, "no issue found" among them, comes back at once as it was; so
// does one that fails every time, in the tracker's own words (mw-gq6.374).
func readRetrying[T any](ctx context.Context, n Next, report *NextReport, what string, read func() (T, error)) (T, error) {
	return retrying(ctx, n, report, "read", what, read)
}

// writeRetrying is readRetrying for a write to the tracker or the mailbox
// (mw-gq6.375): a write that met a passing fault is tried again, ReadRetries
// times, and one that fails every time comes back in the tracker's own words.
func writeRetrying(ctx context.Context, n Next, report *NextReport, what string, write func() error) error {
	_, err := retrying(ctx, n, report, "write", what, func() (struct{}, error) { return struct{}{}, write() })
	return err
}

func retrying[T any](ctx context.Context, n Next, report *NextReport, verb, what string, try func() (T, error)) (T, error) {
	for retry := 0; ; retry++ {
		got, err := try()
		if err == nil {
			return got, nil
		}
		fault, passing := Faulted(err)
		if !passing || retry == ReadRetries {
			return got, err
		}
		wait := n.PushWait / 4 * time.Duration(retry+1)
		if report != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("%s of %s failed (%s), retried in %s", verb, what, fault.Why, wait))
		}
		if waitErr := n.retryWait(ctx, wait); waitErr != nil {
			return got, fmt.Errorf("%w (and waiting %s to %s %s again: %v)", err, wait, verb, what, waitErr)
		}
	}
}

// retryPause is how long to wait before the retry'th try again of a git step
// that met a passing fault: half of PushWait, then PushWait, then half as much
// again, so that three retries wait 60s in all by default.
func (n Next) retryPause(retry int) time.Duration {
	return n.PushWait / 2 * time.Duration(retry)
}

// retryNote is the one line a retry leaves in the close-out.
func retryNote(step string, err error, wait time.Duration) string {
	why := "passing fault"
	if fault, ok := Faulted(err); ok {
		why = fault.Why
	}
	return fmt.Sprintf("%s failed (%s), retried in %s", step, why, wait)
}

// carryOn brings the hosts level and dispatches whatever is ready now. It runs
// only once the story is closed, so that the other host sees a closed story
// rather than a claimed one, and so that what is dispatched next accounts for
// the story that has just finished.
func (n Next) carryOn(ctx context.Context, c *closeOut, report *NextReport) (NextReport, error) {
	done := "it landed and was closed"
	if report.Queued {
		done = "it was queued for batch landing"
	}
	if n.Sync != nil {
		synced, err := n.Sync.Run(ctx)
		if err != nil {
			return *report, fmt.Errorf("closing out %s: %s, but the hosts could not be brought level, so nothing was dispatched: %w", c.id, done, err)
		}
		report.Sync, report.Synced = synced, true
	}
	if n.Dispatch == nil {
		return *report, nil
	}
	dispatched, err := n.Dispatch.Run(ctx)
	report.Dispatch, report.Dispatched = dispatched, true
	if err != nil {
		return *report, fmt.Errorf("closing out %s: %s, but the next dispatch failed: %w", c.id, done, err)
	}
	return *report, nil
}

// stop records a close-out that landed nothing: the reason, and the code it is
// filed under, on the story; the story marked blocked so that nobody takes it
// for work in flight; and one line in the ledger saying it did not land, its
// outcome starting with the code. Nothing is merged, nothing is pushed, nothing
// is closed but what is left open of the story's formula (mw-gq6.438) and, but
// for open steps (mw-gq6.341), nothing is given back — the worktree and the
// branch are left exactly as the session left them, because they are the
// evidence.
func (n Next) stop(ctx context.Context, c *closeOut, report *NextReport, reason Reason, why, said string) (NextReport, error) {
	if ctx.Err() != nil {
		return n.interrupted(ctx, c, report)
	}
	// Tests that timed out on a host at or above its core count did not fail on
	// the story: the refusal says so, in the mail and on the story.
	if (reason == ReasonTestsFail || reason == ReasonMergedTestsFail) && c.gate.timeout {
		why += " (" + TimeoutUnderLoadNote(n.Host, c.gate.at) + ")"
	}
	report.Why, report.Reason = why, reason
	n.recordBenchmark(ctx, c, report, false)

	// The formula is closed before the claim can be given back, so that a
	// dispatch that takes the story again pours it a fresh one rather than
	// working what this attempt left of its own.
	n.closeFormula(ctx, c, report, fmt.Sprintf("was not landed (%s), and its next attempt pours its formula afresh", reason))

	// Written before the claim can be given back, so that the dispatch that
	// takes the story again finds it (mw-1ljpo9.7).
	continues := n.handOn(ctx, c, report, reason)

	var trouble []string
	// A story whose work is committed and whose formula was left unfinished is
	// not waiting for a person (mw-gq6.341), nor is one whose session ran to its
	// turn cap (mw-1ljpo9.6): the claim is given back so that the
	// next dispatch tick takes it as another attempt, keeping this branch under
	// another name as it keeps any leftover of an earlier attempt. So is a Tester
	// story's that left no FINDINGS (mw-gq6.594): nothing is left to land, and the
	// Mayor can bd close it. Every other stop keeps the claim, because the claim
	// is the evidence.
	given := false
	if reason == ReasonOpenSteps || reason == ReasonNoClosingComment || reason == ReasonGristExampleUnproved || reason == ReasonTurnCap || (reason == ReasonNoFindings && IsTesterStory(c.detail)) {
		if err := n.releaseOnStop(ctx, c.id, report); err != nil {
			trouble = append(trouble, fmt.Sprintf("the claim on %s could not be given back: %v", c.id, err))
		} else {
			given = true
		}
	}

	claim := "the claim was not given back"
	if given && reason == ReasonNoFindings {
		// A Tester story that left nothing to land (mw-gq6.594): its claim is not
		// the evidence, so it is given back and the Mayor can bd close it.
		claim = "the claim was given back and it is open again, so it can be closed with a plain bd close"
	} else if given && continues {
		claim = fmt.Sprintf("the claim was given back and it is open again: the next dispatch tick takes it as attempt %d "+
			"and keeps the branch under another name to cut its own from", c.detail.Attempts+1)
	} else if given {
		claim = fmt.Sprintf("the claim was given back and it is open again: the next dispatch tick takes it as attempt %d "+
			"and keeps the branch under another name before it cuts a fresh one", c.detail.Attempts+1)
	}
	nothingPushed := "nothing was pushed"
	if c.branchPushed {
		nothingPushed = "only the story's branch " + c.branch + " was pushed"
	}
	note := fmt.Sprintf("mw next on %s "+RefusedPhrase+"%s): %s\n\n"+
		"Nothing was merged and "+nothingPushed+". The story is not closed and %s. "+
		"The worktree %s and the branch %s are left as the session left them.",
		n.Host, reason, why, claim, c.worktree, c.branch)
	if continues {
		note += fmt.Sprintf(" Its next attempt continues this branch, rebased onto %s: mw retry keeps it, "+
			"and mw retry --from-main starts the next attempt from %s instead.", StartPoint(n.remote(), c.target), c.target)
	}
	if said != "" {
		note += "\n\n" + said
	}

	// Each write that meets a passing fault of the network is tried again; one
	// that fails all the same is kept, with the mail, in the run for mw dispatch
	// to replay (mw-gq6.375), rather than left to this pane's stderr.
	var unwritten []string
	stateDetail := "(" + string(reason) + ") " + why
	if err := writeRetrying(ctx, n, report, "the reason on the story", func() error { return n.Tracker.CommentOnStory(ctx, c.id, note) }); err != nil {
		trouble = append(trouble, fmt.Sprintf("the reason could not be written on the story: %v", err))
		unwritten = append(unwritten, UnrecordedComment)
	}
	if err := writeRetrying(ctx, n, report, "the run state", func() error { return n.Tracker.SetStoryState(ctx, c.id, RunState, RunBlocked, stateDetail) }); err != nil {
		trouble = append(trouble, fmt.Sprintf("%s could not be recorded as %s=%s: %v", c.id, RunState, RunBlocked, err))
		unwritten = append(unwritten, UnrecordedRunState)
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
	mail := n.mayorMail(c, report, verdict, said)
	if reason == ReasonLoginExpired && c.loginTold {
		// Once per host per expiry, not per story (mw-gq6.781): the Mayor has the
		// news in the mail holdHostForLogin sent.
		mail = NewMessage{}
	} else if err := n.sendMayorMail(ctx, c, report, mail, verdict); err != nil {
		unwritten = append(unwritten, UnrecordedMail)
	}
	if len(unwritten) > 0 {
		trouble = append(trouble, n.keepUnrecorded(ctx, c, report, UnrecordedOutcome{
			Story: c.id, Host: n.Host, At: n.now().UTC(), Outcome: strings.ToLower(verdict), Reason: reason, Why: why,
			Attempt: c.attempt(), Comment: note, StateDetail: stateDetail,
			MailFrom: mail.From, MailTo: mail.To, MailSubject: mail.Subject, MailBody: mail.Body, Pending: unwritten,
		})...)
		n.commit(ctx, c, report)
	}

	err := fmt.Errorf("closing out %s: %s", c.id, why)
	if len(trouble) > 0 {
		report.Notes = append(report.Notes, trouble...)
		err = fmt.Errorf("%w (and %s)", err, strings.Join(trouble, "; "))
	}
	return *report, err
}

// handOn writes the note that the story's next attempt continues the branch
// this one left (mw-1ljpo9.7), when RetryFromBranch is on and reason is one
// ContinuesFrom names, and reports whether it was written. One that cannot be
// written is a note: the next attempt starts from the target, as before.
func (n Next) handOn(ctx context.Context, c *closeOut, report *NextReport, reason Reason) bool {
	if !n.RetryFromBranch || n.Memory == nil || !ContinuesFrom(reason) {
		return false
	}
	if err := writeRetrying(ctx, n, report, "the note that its next attempt continues its branch", func() error {
		return n.Memory.SetNote(ctx, ContinueKey(c.id), strconv.Itoa(c.attempt()))
	}); err != nil {
		lost := fmt.Sprintf("the note that the next attempt of %s continues its branch could not be written, "+
			"so it starts from %s: %v", c.id, c.target, err)
		report.Notes = append(report.Notes, lost)
		n.mailLostWrite(ctx, c, report, "the continue note", lost)
		return false
	}
	return true
}

// InterruptedRecordLimit is how long a close-out whose context has ended
// still gives itself to write its one ledger line and commit it.
const InterruptedRecordLimit = 30 * time.Second

// CloseOutInterrupted is the one line an interrupted close-out says, on stderr
// and in the ledger: what to run to judge the story after all.
func CloseOutInterrupted(id string) string {
	return fmt.Sprintf("close-out interrupted, not judged: re-run mw next %s --no-dispatch", id)
}

// interrupted is a close-out whose own context ended — SIGINT or SIGTERM, the
// session that ran it gone — before it settled what became of the story
// (mw-gq6.441). Whatever failed after that failed because the caller went away,
// not because of the story's work: a test run killed, bd and git cancelled. So
// nothing is recorded as the story's outcome — no refusal, no run=blocked, no
// mail, no record for mw dispatch to replay, no formula closed and no claim
// given back — and a re-run of the close-out finds the story as it was. Only
// the ledger line is written, if it can be, saying so; it does not read as
// landed or as not landed.
func (n Next) interrupted(ctx context.Context, c *closeOut, report *NextReport) (NextReport, error) {
	line := CloseOutInterrupted(c.id)
	report.Why, report.Reason, report.Refused, report.Interrupted = line, "", false, true
	record, cancel := context.WithTimeout(context.WithoutCancel(ctx), InterruptedRecordLimit)
	defer cancel()
	if err := n.ledger(record, c, report, line); err != nil {
		report.Notes = append(report.Notes, err.Error())
	} else {
		n.commit(record, c, report)
	}
	return *report, fmt.Errorf("closing out %s: %s (%v)", c.id, line, context.Cause(ctx))
}

// MaxTurnsSubtype is how Claude Code's result names a session it stopped at its
// --max-turns.
const MaxTurnsSubtype = "error_max_turns"

// CappedAtTurns reports whether the harness stopped the session at its turn cap
// (mw-1ljpo9.6).
func CappedAtTurns(result SessionResult) bool { return result.Subtype == MaxTurnsSubtype }

// LandedAfterTurnCap ends the outcome of a story whose session was cut at the
// turn cap after its closing comment and which landed all the same (mw-gq6.595).
const LandedAfterTurnCap = "landed after the turn cap"

// cappedAfterClosing reports whether a session its harness stopped at the turn
// cap had written its closing comment ('For the rig memory' after its newest
// commit) before it was stopped: the close-out turns of a Builder count against
// the cap, so such a session is done, and only the rig's gate is left to judge
// it (mw-gq6.595). It fetches the rig, which the close-out needs anyway; anything
// it cannot read counts as no.
func (n Next) cappedAfterClosing(ctx context.Context, c *closeOut, report *NextReport) bool {
	if IsTesterStory(c.detail) {
		return false
	}
	if err := n.fetchRetrying(ctx, report, c.rigDir); err != nil {
		return false
	}
	commits, err := n.Landing.Commits(ctx, c.rigDir, c.branch, StartPoint(n.remote(), c.target))
	if err != nil || len(commits) == 0 {
		return false
	}
	var newest time.Time
	for _, commit := range commits {
		if commit.Committed.After(newest) {
			newest = commit.Committed
		}
	}
	comments, err := n.Tracker.StoryComments(ctx, c.id)
	if err != nil {
		return false
	}
	for _, comment := range comments {
		if comment.Created.After(newest) && strings.Contains(comment.Text, "For the rig memory") {
			return true
		}
	}
	return false
}

// TurnCapWhy is the reason a story whose session was stopped at its turn cap is
// given back, with the turns the harness counted when it says.
func TurnCapWhy(turns int) string {
	capped := "capped at its turn cap"
	if turns > 0 {
		capped = fmt.Sprintf("capped at %d turns", turns)
	}
	return "the session was " + capped + " and its harness stopped it before it finished; " +
		"what it committed is kept on its branch for the next attempt"
}

// closeFormula closes what is left open of the story's formula once the
// close-out has settled what became of the story (mw-gq6.438). One that cannot
// be closed is a note: it changes nothing about the story, and mw tidy closes
// it once the story is closed.
func (n Next) closeFormula(ctx context.Context, c *closeOut, report *NextReport, what string) {
	root := c.detail.Molecule.RootID
	closed, err := closeFormula(ctx, n.Tracker, root, FormulaClosedWhy("mw next", n.Host, c.id, what))
	report.FormulaClosed = closed
	if err != nil {
		lost := fmt.Sprintf("the formula %s of %s could not be closed: %v", root, c.id, err)
		report.Notes = append(report.Notes, lost)
		n.mailLostWrite(ctx, c, report, "the formula close", lost)
	}
}

// mailLostWrite tells the Mayor of a write to the tracker that was lost for good
// — tried again by the gateway and by writeRetrying, and still refused — so
// that it is not only a note in the close-out that nobody reads (mw-gq6.538).
// The mail names the story, what was lost and the command that failed, in the
// tracker's own words, and what the loss costs. A mail that cannot be sent is
// said on stderr and changes nothing else.
func (n Next) mailLostWrite(ctx context.Context, c *closeOut, report *NextReport, what, said string) {
	if n.Mailbox == nil || ctx.Err() != nil {
		return
	}
	mail := NewMessage{
		From:    SeatIdentity(MwSeat, n.Host),
		To:      MayorMailbox,
		Subject: fmt.Sprintf("lost write: %s on %s (%s)", what, c.id, n.Host),
		Body: fmt.Sprintf("mw next on %s lost a write while closing out %s: %s.\n\n%s\n\nIt was tried again and still failed; "+
			"nothing else was changed for it. Make the write by hand, with the command named above.\n", n.Host, c.id, what, said),
	}
	err := writeRetrying(ctx, n, report, "the mail about a lost write", func() error {
		_, err := n.Mailbox.Send(ctx, mail)
		return err
	})
	if err != nil && n.Err != nil {
		fmt.Fprintf(n.Err, "mw next: the mail to %s about a lost write of %s could not be sent: %v\n", MayorMailbox, c.id, err)
	}
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
	if err := n.sendMayorMail(ctx, c, report, n.mayorMail(c, report, verdict, said), verdict); err != nil {
		n.saidInEvents(ctx, report, c, fmt.Sprintf("the mail to %s about %s (%s) could not be sent: %v", MayorMailbox, c.id, strings.ToLower(verdict), err))
	}
}

// mayorMail is the mail mailTheMayor sends: what it says is settled by the
// report as it stands.
func (n Next) mayorMail(c *closeOut, report *NextReport, verdict, said string) NewMessage {
	title := strings.Join(strings.Fields(c.detail.Story.Title), " ")
	if title == "" {
		title = c.id
	}
	if verdict == MailTested {
		title = c.tested + ": " + c.findings.Counted()
	}
	body := report.String()
	if report.AfterLandingStopped > 0 {
		body = AfterLandingStoppedLine + "\n" + body
	}
	if report.Landed && report.AfterLandingSkipped != "" {
		body = AfterLandingNotRunLine + ": " + strings.TrimPrefix(report.AfterLandingSkipped, "after landing: ") + "\n" + body
	}
	if len(report.UnitRestartFailed) > 0 {
		body = UnitRestartFailedLine + "\n" + body
	}
	if said != "" {
		body += "\n" + said + "\n"
	}
	if verdict == MailRefused || verdict == MailBlocked {
		body = strings.TrimRight(body, "\n") + "\n\n" + CauseLine(c.id) + "\n"
	}
	return NewMessage{
		From:    SeatIdentity(MwSeat, n.Host),
		To:      MayorMailbox,
		Subject: verdict + ": " + title,
		Body:    body,
	}
}

// sendMayorMail sends the mail, trying it again on a passing fault of the
// network. A mail that cannot be sent is said on stderr and changes nothing
// else: its error is the caller's to keep what it needs (mw-gq6.375).
func (n Next) sendMayorMail(ctx context.Context, c *closeOut, report *NextReport, mail NewMessage, verdict string) error {
	if n.Mailbox == nil {
		return nil
	}
	err := writeRetrying(ctx, n, report, "the mail to the Mayor", func() error {
		_, err := n.Mailbox.Send(ctx, mail)
		return err
	})
	if err == nil {
		return nil
	}
	if n.Err != nil {
		fmt.Fprintf(n.Err, "mw next: the mail to %s about %s (%s) could not be sent: %v\n",
			MayorMailbox, c.id, strings.ToLower(verdict), err)
	}
	return err
}

// writeOrSay writes one record of a close-out that is already past the point of
// stopping — a send-back's, a landing's — trying again on a passing fault of
// the network; a write that fails all the same is a note on the report and a
// line in the events log, never only the pane's stderr.
func (n Next) writeOrSay(ctx context.Context, c *closeOut, report *NextReport, what, failure string, write func() error) {
	if err := writeRetrying(ctx, n, report, what, write); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: %v", failure, err))
		n.saidInEvents(ctx, report, c, fmt.Sprintf("%s: %v", failure, err))
	}
}

// saidInEvents writes a failed close-out write to the events log, as a job
// that failed. A log that cannot be written is a note.
func (n Next) saidInEvents(ctx context.Context, report *NextReport, c *closeOut, detail string) {
	if n.Events == nil {
		return
	}
	if _, err := (EventEmit{
		Log: n.Events, Now: n.now,
		Event: events.Event{
			Kind: events.KindJob, Actor: JobCloseOutUnrecorded + "@" + n.Host,
			From: events.JobRunning, To: events.JobFailed,
			Detail: events.CutDetail(detail),
		},
	}).Run(ctx); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the job event for %s could not be written: %v", c.id, err))
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
// The same goes for the whole output of a test run that failed (gate.log).
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

	if c.unrecorded {
		if record := runFilePath(c.id, UnrecordedFileName); record != "" {
			paths = append(paths, record)
		}
	}

	if c.gateLog != "" {
		if record := runFilePath(c.id, c.gateLog); record != "" {
			paths = append(paths, record)
		}
	}

	if c.harnessStop != "" {
		if record := runFilePath(c.id, c.harnessStop); record != "" {
			paths = append(paths, record)
		}
	}

	if c.afterLog != "" {
		if record := runFilePath(c.id, c.afterLog); record != "" {
			paths = append(paths, record)
		}
	}

	committed, err := commitWritten(ctx, n.Files, VaultCommitMessage(c.id, c.detail.Story.Title), paths)
	if err != nil {
		report.NotCommitted = NotCommittedLine(paths, err)
		return
	}
	report.Committed = append(report.Committed, committed...)
}

const notCommittedPrefix = "NOT committed: "

// NotCommittedLine is the one line a close-out or a batch landing says when
// files it wrote in the vault could not be committed: which, and why, so that a
// person reading the mail knows the vault is left holding them and the next sync
// will refuse until somebody commits them (mw-gq6.554).
func NotCommittedLine(paths []string, err error) string {
	why := strings.Join(strings.Fields(err.Error()), " ")
	return fmt.Sprintf(notCommittedPrefix+"%s: %s (the next sync will refuse until somebody commits them)", strings.Join(paths, ", "), why)
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
	if report.Pushes > 1 && !report.Queued {
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
// is a worktree the next dispatch would take away. The exception is a session
// that never started (giveBackIfNeverStarted): there is nothing to take away. Somebody has to decide, and
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
		// A story queued for batch landing has no session by design, and its
		// claim and worktree wait for its batch; one handed back for its
		// close-out alone waits for the next dispatch tick (mw-gq6.467).
		if id == skip || QueuedForBatch(detail) || CloseOutAgain(detail) {
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

		// A session that never started has left nothing in its worktree to
		// protect, so the claim goes back and the next dispatch takes the story.
		if n.giveBackIfNeverStarted(ctx, detail, report) {
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

// NeverStartedGrace is how old a claim must be before mw next takes its missing
// session for one that never started: a dispatch cuts the worktree and starts
// the session some time after it claims the story, and a claim younger than
// this may be in that gap.
const NeverStartedGrace = 5 * time.Minute

// giveBackIfNeverStarted gives back the claim of a story whose session is gone
// and which has left nothing behind it: no transcript written since the claim began, no commit beyond the
// target, no change in its worktree (or no worktree at all). There is no work
// for a given-back claim to take away. Anything it cannot establish — no
// transcripts to read, a claim of unknown or recent age, a git or tracker
// error — leaves the claim alone, as every gone session's was. It reports
// whether the claim was given back, and confirms that by reading the story.
func (n Next) giveBackIfNeverStarted(ctx context.Context, detail StoryDetail, report *NextReport) bool {
	id := detail.Story.ID
	started := detail.ClaimStarted()
	if n.Transcripts == nil || n.Landing == nil || n.Worktrees == nil || started.IsZero() || n.now().Sub(started) < NeverStartedGrace {
		return false
	}
	path, err := detail.Path()
	if err != nil {
		return false
	}
	rigDir, checkedOut := n.Rigs[path.Rig]
	if !checkedOut {
		return false
	}
	worktree, branch := WorktreeDir(rigDir, id), StoryBranch(id)

	// Every attempt of a story reuses its worktree path, so a transcript under
	// it may be an earlier attempt's. Only one written since this claim began
	// is this claim's work (mw-gq6.529).
	written, err := n.Transcripts.LastWrite(ctx, worktree)
	if err != nil {
		return false
	}
	if written.IsZero() || !written.Before(started) {
		tail, err := n.Transcripts.Tail(ctx, worktree, PeekLines)
		if err != nil || strings.TrimSpace(tail) != "" {
			return false
		}
	}
	dirThere, err := n.Worktrees.Exists(ctx, rigDir, worktree, "")
	if err != nil {
		return false
	}
	branchThere, err := n.Worktrees.Exists(ctx, rigDir, "", branch)
	if err != nil {
		return false
	}
	if branchThere {
		ahead, err := n.Landing.Ahead(ctx, rigDir, branch, StartPoint(n.remote(), path.Branch))
		if err != nil || ahead > 0 {
			return false
		}
	}
	if dirThere {
		changed, err := n.Landing.Uncommitted(ctx, worktree)
		if err != nil || len(changed) > 0 {
			return false
		}
	}

	if dirThere || branchThere {
		if err := n.Worktrees.Remove(ctx, rigDir, worktree, branch); err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("%s: the empty worktree %s could not be removed, so its claim was left alone: %v", id, worktree, err))
			return false
		}
	}
	holder := detail.Assignee
	if err := giveBackClaim(ctx, n.Tracker, id, func(ctx context.Context) error { return n.Tracker.ReleaseClaimHeldBy(ctx, id, holder) },
		waitFor, n.PushWait); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: its session never started, but the claim could not be given back: %v", id, err))
		return false
	}
	said := fmt.Sprintf("mw next on %s gave the claim back: %s is not there, and the story left nothing behind it (no transcript, no commits beyond %s, no changes in a worktree), so the session never started. The story is open again and the next dispatch takes it.",
		n.Host, SessionName(id), path.Branch)
	if err := n.Tracker.CommentOnStory(ctx, id, said); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: its claim was given back, but that could not be written on it: %v", id, err))
	}
	report.GaveBack = append(report.GaveBack, id)
	return true
}

// releaseOnStop gives back the claim of a story stopped for open steps, and
// tries again when the give-back met a passing fault of the beads server
// (mw-gq6.393): on the desktop that fault, a lost connection among them,
// left a finished story holding its claim. A try that failed and left the
// story open and unclaimed did land, as a write on a connection that dropped
// can, and counts as given back rather than as a second failure.
func (n Next) releaseOnStop(ctx context.Context, id string, report *NextReport) error {
	return writeRetrying(ctx, n, report, "the give-back of the claim on "+id, func() error {
		err := n.Tracker.ReleaseClaim(ctx, id)
		if err == nil {
			return nil
		}
		if _, passing := Faulted(err); passing {
			return err
		}
		if detail, readErr := n.Tracker.ShowStory(ctx, id); readErr == nil && detail.Status == StatusOpen && detail.Assignee == "" {
			return nil
		}
		return err
	})
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
const DefaultPushTries = 4

// pushTries is how many times a push will try again after failing on a fault
// at the remote worth trying again.
func (n Next) pushTries() int {
	if n.PushTries < 1 {
		return DefaultPushTries
	}
	return n.PushTries
}

// retryWait is the pause between the tries of a git step that met a passing
// fault.
func (n Next) retryWait(ctx context.Context, d time.Duration) error {
	if n.RetryWait != nil {
		return n.RetryWait(ctx, d)
	}
	return waitFor(ctx, d)
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
	case r.Queued:
		fmt.Fprintf(&b, "  queued  for batch landing as %s (%d commits); %s is not touched until its batch lands\n", r.QueuedAs, r.Commits, r.Target)
		if r.Trusted != "" {
			fmt.Fprintf(&b, "          tested by mw check at %s: the rig's tests were not run again\n", shortCommit(r.Trusted))
		}
	case r.Landed:
		fmt.Fprintf(&b, "  landed  %s on %s (%d commits, %d push(es))\n", r.How.LandedAs(r.Target), r.Target, r.Commits, r.Pushes)
		if len(r.How.Unioned) > 0 {
			fmt.Fprintf(&b, "  trim-merged: %s (conflicts in files the rig lists, settled by mw's trim-safe driver: both sides' added lines kept, removed ones left out, tested together)\n", strings.Join(r.How.Unioned, ", "))
		}
	case r.Tested != "" && r.Findings.Text == "":
		fmt.Fprintf(&b, "  tested  %s by an earlier run of mw next: nothing was read or written again\n", r.Tested)
	case r.Tested != "":
		fmt.Fprintf(&b, "  tested  %s: %s; a Tester story, so nothing was merged, raised or pushed\n", r.Tested, r.Findings.Split())
	case r.Interrupted:
		fmt.Fprintf(&b, "  INTERRUPTED %s\n", r.Why)
		b.WriteString("          nothing was recorded as its outcome: its claim, worktree and branch are as they were\n")
	case r.MainRed != nil:
		fmt.Fprintf(&b, "  HELD (%s) %s\n", r.Reason, r.Why)
		b.WriteString("          not sent back and no attempt counted: the next dispatch tick after the bead that carries main's fix is closed runs its close-out alone again\n")
	case r.HandedBack:
		fmt.Fprintf(&b, "  HANDED BACK (%s) %s\n", r.Reason, r.Why)
		b.WriteString("          its claim, worktree and branch are kept: the next dispatch tick runs its close-out alone again, not a new session\n")
	case r.SentBack != "":
		fmt.Fprintf(&b, "  SENT BACK (%s) %s\n", r.Reason, r.Why)
		if r.Reason == ReasonMergedTestsFail {
			fmt.Fprintf(&b, "          to %s, a fresh session fixing the merged tests; sent back once, never again\n", r.SentBack)
		} else {
			fmt.Fprintf(&b, "          to %s, a fresh session rebasing onto %s; sent back only while the target keeps moving, three times at most\n", r.SentBack, r.Target)
		}
	default:
		if r.Reason == "" {
			fmt.Fprintf(&b, "  STOPPED %s\n", r.Why)
		} else {
			fmt.Fprintf(&b, "  STOPPED (%s) %s\n", r.Reason, r.Why)
		}
	}
	if r.Landed {
		for _, line := range r.Tags.Lines() {
			b.WriteString(line + "\n")
		}
	}
	switch {
	case r.Rig.Moved:
		fmt.Fprintf(&b, "  rig     %s fast-forwarded to %s on %s\n", r.RigDir, shortCommit(r.How.Commit), r.Target)
	case r.Rig.Left != "":
		fmt.Fprintf(&b, "  rig     %s left as it was, not brought up to %s: %s\n", r.RigDir, r.Target, r.Rig.Left)
	case r.Rig.Level && r.Rig.Held == "":
		fmt.Fprintf(&b, "  rig     %s was already at %s on %s\n", r.RigDir, shortCommit(r.How.Commit), r.Target)
	case r.Rig.Held != "":
		fmt.Fprintf(&b, "  rig     %s is not a place to build %s from: %s\n", r.RigDir, r.Target, r.Rig.Held)
	}
	if r.TesterFiled != "" {
		fmt.Fprintf(&b, "  tester  %s filed to drive this landing on a phone-sized screen\n", r.TesterFiled)
	}
	if len(r.Findings.Held) > 0 {
		fmt.Fprintf(&b, "  held    %s filed by the Tester for its findings: release or drop them\n", strings.Join(r.Findings.Held, ", "))
	}
	if len(r.Findings.Duplicates) > 0 {
		fmt.Fprintf(&b, "  twin    %s already open, so the Tester filed nothing for a finding the same\n", strings.Join(r.Findings.Duplicates, ", "))
	}
	if r.Swept > 0 {
		fmt.Fprintf(&b, "  stopped %d leftover processes under %s\n", r.Swept, r.SweptDir)
	}
	if len(r.Uncommitted) > 0 {
		fmt.Fprintf(&b, "  left    uncommitted work, %d path(s), in the worktree:\n", len(r.Uncommitted))
		for _, line := range strings.Split(bullets(r.Uncommitted, UncommittedListed), "\n") {
			fmt.Fprintf(&b, "            %s\n", line)
		}
	}
	for _, line := range r.Bench {
		fmt.Fprintf(&b, "  bench   %s\n", line)
	}
	if r.Ledger != "" {
		fmt.Fprintf(&b, "  ledger  %s\n", r.Ledger)
	}
	if len(r.Committed) > 0 {
		fmt.Fprintf(&b, "  commit  %s committed in the vault\n", strings.Join(r.Committed, ", "))
	}
	if r.NotCommitted != "" {
		fmt.Fprintf(&b, "  %s\n", r.NotCommitted)
	}
	if r.Closed {
		b.WriteString("  closed  the story is closed\n")
	}
	if r.FormulaClosed.Any() {
		fmt.Fprintf(&b, "  formula closed %s\n", r.FormulaClosed)
	}
	if r.Queued {
		b.WriteString("  open    the story stays open and claimed, its worktree kept, until its batch lands it\n")
	}
	if r.DemoHeld {
		b.WriteString("  closed  " + DemoLeftOpen + "\n")
	}
	if r.Landed && !r.Closed && !r.DemoHeld {
		fmt.Fprintf(&b, "  OPEN    the story is landed but still open: %s\n", firstLine(r.NotClosed))
		fmt.Fprintf(&b, "          nothing above is undone; run `mw next %s` again to close it, and nothing is merged or ledgered twice\n", r.StoryID)
		if r.Assignee != "" {
			b.WriteString(r.claimHint())
		}
	}
	for _, id := range r.Abandoned {
		fmt.Fprintf(&b, "  gone    %s is claimed here with no session behind it\n", id)
	}
	for _, id := range r.GaveBack {
		fmt.Fprintf(&b, "  open    %s was claimed here but its session never started; the claim was given back\n", id)
	}
	for _, said := range r.CrossSlot {
		fmt.Fprintf(&b, "  slot    %s\n", said)
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
