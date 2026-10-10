package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// MoleculeField is the metadata a dispatched story carries so that whoever
// closes it out can find the formula steps that were poured for it.
const MoleculeField = "molecule"

// RunState is the state dimension a dispatched story carries, and RunRunning is
// its value while a session is working it.
const (
	RunState   = "run"
	RunRunning = "running"
)

// RunStates is every value a story's run state takes, those named here and in
// next.go and eventcontrol.go: what a claim clears from a story an earlier
// attempt left one on, since a fresh claim is not the refusal, landing or
// cancel the label would still say.
var RunStates = []string{RunRunning, RunLanded, RunBlocked, RunStopped, RunStuck, RunCancelled}

// HostSync is this host being brought level with the other one. Sync is the
// implementation; the port is here so that a dispatch can be tested without a
// remote, and so that dispatch depends on the act rather than on the adapter.
type HostSync interface {
	Run(ctx context.Context) (SyncReport, error)
}

// Sync satisfies the port.
var _ HostSync = Sync{}

// Dispatch starts a fresh session for each story ready on this host, up to a
// concurrency cap. It is the use case that strings the others together: sync,
// then the tracker, then a worktree, then the formula, then the seat's boot,
// then the runner.
//
// Claiming is what makes a story this host's, and everything after the claim
// can fail. So everything after the claim is undone when it does: the worktree
// is removed, the claim is given back, and the failure is written on the story,
// where the next dispatch and the Mayor will both see it. The one thing never
// undone is a session that has actually started — a story whose session is
// running stays claimed, however badly the recording of it went, because a
// claim given back while a session works the story would let a second dispatch
// start it twice. A session that has ended and still holds the story's name is
// closed before the story starts again; one that is still running is a story
// being worked, and is refused before anything is claimed, cut or removed.
//
// A story is started a bounded number of times. Each session that gets as far as
// running is counted on the story (AttemptsField), after the session starts and
// never before, so that a dispatch that fails on the way to one — the fetch, the
// worktree, the runner — leaves the count as it was. A story that has had
// MaxAttempts is not claimed at all: it is marked blocked and the Mayor is told
// once (see exhausted), and only a person resetting the count starts it again.
type Dispatch struct {
	Tracker   WorkTracker
	Worktrees Worktrees
	Runner    Runner
	Boot      SeatBoot

	// Landing is how a leftover worktree from an earlier attempt — one a
	// dead-pane reclaim or a mw retry gave the claim back on — is read for
	// uncommitted work before a fresh cut takes it away (mw-gq6.326). Left nil,
	// a leftover is never looked for: Add's own failure decides what happens
	// next.
	Landing Landing

	// Memory is where mw sweep remembers what it saw of a story's last session,
	// so that a fresh attempt can clear it (mw-gq6.86): a session's pane starts
	// blank whichever attempt it is, and a note an earlier attempt left behind
	// would otherwise make a fresh session look silent since the old attempt's
	// clock. A nil Memory clears nothing, which is what a dry run does too.
	Memory SweepNotes

	// Sync is run before anything is asked of the tracker, so that this host
	// sees the other host's claims before it makes its own. A nil Sync skips
	// it, which is what a dry run does.
	Sync HostSync

	// SyncHalts is this host's own mark of a halted sync: written once the
	// sync halts on a merge conflict or a stuck working set, left alone on a
	// halt that repeats, and cleared once the sync is level again. A nil
	// SyncHalts writes and clears nothing, and mw status here has no local
	// mark to read.
	SyncHalts SyncHaltMarker

	// SyncTries is how many times the sync is tried when it fails because a name
	// could not be resolved, which is what a host just woken from standby says
	// until its network is back, and SyncWait is how long to wait between the
	// tries. Only that failure is tried again: a retry must never hide a real
	// fault. Fewer than one is one try, which is no retry at all.
	SyncTries int
	SyncWait  time.Duration

	// Wait is how the wait between tries is made, so that a test never sleeps.
	// The zero value waits for real, and stops when the context does.
	Wait func(ctx context.Context, d time.Duration) error

	// Load is how busy this host is, read once a pass: no story, whichever host it
	// names, is taken while the host has no room (mw-t0z3fu.1), which is its
	// 1-minute load at or above Room's share of a core each or its available
	// memory under Room's floor. The cap is still the ceiling. A nil Load, or a
	// load or memory that cannot be read, does not hold a story back: the cap
	// rules.
	Load HostLoad
	Room RoomLimits

	// Grist is what the mill is doing on this host, read once a pass (mw-t0z3fu.4):
	// while a tutor is in use (a grind running or one answered lately) the host
	// starts no more stories than the grist cap, and while the tutor's answers
	// run past their par it starts none, as it does for want of room. Stories
	// already running are never stopped. A nil Grist, or one that cannot be read,
	// holds nothing back.
	Grist GristPulses

	// Host is which of the factory's hosts this is, Cap is how many sessions
	// may be running here at once, and Rigs is where each rig is checked out.
	Host string
	Cap  int
	Rigs map[string]string

	// Exclusive is this host's dispatch lock, taken without waiting and held for
	// the whole of a real run: a second dispatch that cannot take it says so and
	// does nothing, since two would claim the same story and race to cut its
	// worktree (mw-gq6.140). A nil Exclusive takes none, and a dry run needs none.
	Exclusive GristLock

	// SelfUpdate is run first by a real tick, once the dispatch lock is held: it
	// keeps this host's mw level with the factory rig's main, as the Millhand's
	// tick does, so a host that dispatches and runs no Millhand tick is kept
	// level too (mw-gq6.184). What it did is printed. A zero SelfUpdate does
	// nothing, and a dry run runs none.
	SelfUpdate SelfUpdate

	// Backend is the other thing the home's tick does first: stage the backend of
	// a landing made on the other host (mw-gq6.185), which left a note for it.
	// A zero Backend, a host that is not home and a dry run do nothing.
	Backend BackendStage

	// Mill and Home make the tick answer what the mill left waiting: after
	// its own claims, on the host that is home, one pass of the mill runs
	// (mw grist grind's own use case, with its own locks and its own limit: a
	// grind takes none of the sessions Cap counts, and Cap never holds a grind
	// back). A nil Mill, or a Home that says another host is home or cannot
	// be read, runs none. A dry run runs none either.
	Mill GristMill
	Home HomeFile

	// MaxAttempts is how many times a story may be started in all; a story tried
	// that many times is not started again, and the Mayor is told. Fewer than one
	// is DefaultMaxAttempts.
	MaxAttempts int

	// Mailbox is how the Mayor is told a story used up its attempts. A nil
	// Mailbox tells nobody: the story is still marked and commented on.
	Mailbox Mailbox

	// Remote is the remote a story's branch is cut from. Empty is DefaultRemote,
	// and whatever it says must be the remote the Worktrees adapter fetches.
	Remote string

	// Events is the home's event log, read at the start of a pass for a
	// pause-host event that no resume-host has undone (mw-jrx0s.16): a paused
	// host's pass claims and starts nothing. A nil Events never pauses; a log
	// that cannot be read is a note and the pass goes on, since a fault of the
	// log is no reason to stop the factory.
	Events EventLog

	// Network says whether the network is metered, and HeavyNet names the rigs
	// whose gate or close-out installs dependencies (npm ci, go mod download).
	// While the network is metered a story on such a rig is passed over and
	// stays open, taken by the first tick after it is not. The network is asked
	// once at the start of a real tick, so that a change of it is noticed whether
	// or not a story waits on it. A nil Network is never metered.
	Network  NetworkReader
	HeavyNet map[string]bool

	// SmokeHolds is asked, once for each rig, whether a failed grist smoke holds
	// the rig's open stories (mw-gq6.319): such a story is passed over and stays
	// open, taken by the first tick after the smoke passes or the hold is lifted.
	// A nil SmokeHolds, or one that cannot be read, holds nothing back.
	SmokeHolds GristSmokeHolds

	// DryRun prints what would be started and writes nothing at all: nothing is
	// synced, nothing claimed, no worktree made, no formula poured, no session
	// started.
	DryRun bool

	// Log is the log this host keeps of its dispatch runs: one dated line for
	// every run that was made, the failed ones too, so that a timer whose runs
	// fail can be told from one whose runs work. A nil Log keeps none, and a dry
	// run is not a run and adds nothing to it.
	Log TickLog

	// Now is the clock a line of the log is dated by; nil is time.Now.
	Now func() time.Time

	// Out is where the report is printed. A nil Out prints nothing.
	Out io.Writer
}

// Started is one story this dispatch started, or would have started.
type Started struct {
	StoryID  string
	Title    string
	Path     domain.Path
	Worktree string
	Branch   string
	// Auto is true when the story's path said host=auto, so that this host was
	// free to take it; Path.Host is this host once the claim has written it.
	Auto bool
	// Start is the commit the branch was cut from.
	Start string
	// Session is the runner's name for the session, not the story's id.
	Session  string
	Molecule Molecule
	// Attempt is which attempt this session is for the story: the first is 1.
	// Zero on a dry run, which starts nothing.
	Attempt int
	// Reused is true when Molecule is one an earlier dispatch poured for the story
	// and that was still open, so that nothing was poured this time. Its Steps are
	// the ones still open.
	Reused bool
	// KeptBranch is the name an earlier attempt's branch was kept under before
	// this attempt's fresh cut (mw-gq6.326). Empty when no branch was left.
	KeptBranch string
}

// Passed is one ready story this dispatch did not take, and why. It is not a
// failure: most of them are somebody else's work.
type Passed struct {
	StoryID string
	Why     string
}

// Failed is one story this dispatch claimed and could not start. Released says
// whether the claim was given back — it is not when the session had already
// started, because then the story really is being worked.
type Failed struct {
	StoryID  string
	Err      error
	Released bool
}

// Reclaimed is one story this dispatch found already claimed here whose tmux
// window still stood but whose pane had died, with the tracker's own lease on
// the claim expired too (mw-gq6.106): the session ended without mw next ever
// hearing about it. Its window was closed and its claim given back, so it is
// read fresh among what is ready and, if nothing else stops it, dispatched
// again below as the next attempt.
type Reclaimed struct {
	StoryID string
	Session string
	// LeaseExpired is when the tracker's lease on the claim ran out, the second
	// sign alongside the dead pane that made this dispatch act rather than
	// leave the claim counted as running.
	LeaseExpired time.Time
}

// LandedAlready is one story this dispatch found claimed here with a dead pane
// and an expired lease that had already landed: its branch merged into its
// target branch (mw-gq6.161), its last run recorded run=landed, or the newest
// line its seat's ledger holds for it saying landed (mw-gq6.191). The close-out
// got as far as the landing and only the close of the story was lost. It was
// closed, not given back, so it is never worked a second time on top of its
// own landed commits.
type LandedAlready struct {
	StoryID string
	Session string
	// Tip is the commit at the branch's tip, the one the target branch holds,
	// when the branch is how it was found landed; Target is the story's target
	// branch.
	Tip    string
	Target string
	// By is how it was known to have landed, as a person reads it.
	By string
}

// smokeHold is the reason a failed grist smoke holds the stories of rig, empty
// when it does not. Each rig is asked once a pass; a record that cannot be read
// holds nothing back.
func (d Dispatch) smokeHold(ctx context.Context, rig string, asked map[string]string) string {
	if d.SmokeHolds == nil {
		return ""
	}
	if why, done := asked[rig]; done {
		return why
	}
	why, err := d.SmokeHolds.HeldBy(ctx, rig)
	if err != nil {
		why = ""
	}
	asked[rig] = why
	return why
}

// HeldRefused is one story this dispatch found claimed here with a dead pane
// and a lapsed lease whose close-out had refused it (mw-gq6.182). Its claim,
// worktree and branch are the evidence of the refusal, so nothing was given
// back: it waits for a person (mw retry, a hold, a give-back by hand).
type HeldRefused struct {
	StoryID string
	Session string
}

// DispatchReport is what one dispatch did.
type DispatchReport struct {
	Host string
	Cap  int
	// Running is how many sessions this host already had in flight.
	Running int
	// Grist is what the mill's pass after the claims did, when one ran.
	Grist *GristReport
	// Reclaimed is every claim this dispatch took back from a dead pane and an
	// expired lease before it read what is ready.
	Reclaimed []Reclaimed
	// LandedAlready is every dead-pane story found already landed and closed
	// rather than given back.
	LandedAlready []LandedAlready
	// HeldRefused is every dead-pane story left claimed because its close-out
	// refused it.
	HeldRefused []HeldRefused
	// Paused is the pause-host event that stopped this pass, nil when none did.
	Paused  *HostPause
	Started []Started
	Passed  []Passed
	Failed  []Failed
	// Notes are what could not be written when a story was found to have used up
	// its attempts: the story is left as it was, and a later tick tries again.
	Notes  []string
	DryRun bool
	// Sync is what the sync before the dispatch moved, when there was one.
	Sync   SyncReport
	Synced bool
	// SyncRetries is how many times the sync had to be tried again, because a
	// name could not be resolved, before it got through.
	SyncRetries int
}

// inStartOrder is the stories in the order a dispatch starts them: the most
// urgent first, and of equal urgency the one filed longest ago. A story with no
// creation time comes after every story that has one. Stories the tracker lists
// alike keep the order it listed them in, because the tracker's timestamps are
// whole seconds and a plan filed in one go is all one second.
//
// mw does the sorting itself rather than trusting the order the tracker lists
// them in, which is the tracker's idea of what is ready and not the Mayor's of
// what is next. The list it is given is left as it was.
func inStartOrder(ready []StoryDetail) []StoryDetail {
	ordered := append([]StoryDetail(nil), ready...)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}
		if a.Created.IsZero() != b.Created.IsZero() {
			return !a.Created.IsZero()
		}
		return a.Created.Before(b.Created)
	})
	return ordered
}

// Run dispatches what this host can take and reports what it did.
//
// The order is the point. This host is brought level with the other one before
// anything is asked of the tracker, because the other host's claims and the
// Mayor's newest stories are both in what a sync brings in, and a dispatcher
// working from a stale view claims work that is not its own. Then what is
// already in flight here, because that is what the cap counts. Then what is
// ready, most urgent and oldest first, so that when the cap is smaller than what
// is ready it is the right stories that wait. Only then is anything claimed.
func (d Dispatch) Run(ctx context.Context) (DispatchReport, error) {
	// One real dispatch at a time on a host. A second one, started while the first
	// runs, would claim the same stories under the same actor, pour their formulas
	// twice and race to cut their worktrees: it says so and does nothing, and is
	// no failure. Nor is it logged, for the run that holds the lock logs itself.
	if d.Exclusive != nil && !d.DryRun {
		release, taken, err := d.Exclusive.TryTake(ctx)
		if err != nil {
			return DispatchReport{}, fmt.Errorf("taking this host's dispatch lock: %w", err)
		}
		if !taken {
			d.print("another mw dispatch is running here; nothing done\n")
			return DispatchReport{Host: d.Host, Cap: d.Cap}, nil
		}
		defer release()
	}
	// First, before the sync or any claim, so that a host with a Millhand that
	// never ticks keeps its mw level: the same look as the Millhand tick's, and
	// what it says is printed. A build that fails costs the tick nothing.
	if !d.DryRun {
		for _, note := range d.SelfUpdate.Run(ctx) {
			d.print(note + "\n")
		}
		for _, note := range d.Backend.Pending(ctx) {
			d.print(note + "\n")
		}
	}
	report, err := d.run(ctx)
	if d.Log != nil && !d.DryRun {
		line := d.now().UTC().Format(time.RFC3339) + " " + dispatchLogWords(report, err)
		if logErr := d.Log.Append(ctx, line); logErr != nil && err == nil {
			err = fmt.Errorf("appending to the dispatch log: %w", logErr)
		}
	}
	return report, err
}

// The words a dispatch's line of its log holds after the time. ok says the run
// did what it is for, and is followed by how many sessions it started or that
// nothing was ready; a local network fault is a run that could not be made, not
// one that failed; failed is followed by why, on one line.
const (
	DispatchLogOK           = "ok: "
	DispatchLogNothingReady = DispatchLogOK + "nothing ready"
	DispatchLogPaused       = DispatchLogOK + "paused"
	DispatchLogFault        = "local network fault"
	DispatchLogFailed       = "failed: "
)

// DispatchLogReasonLimit is how many characters of a failure's reason its line
// of the log keeps: a complaint of many lines of git's is one line, and short.
const DispatchLogReasonLimit = 200

// dispatchLogWords is what one run of Run says of itself in the log.
func dispatchLogWords(report DispatchReport, err error) string {
	switch {
	case err != nil:
		if _, fault := LocalFault(err); fault {
			return DispatchLogFault
		}
		return DispatchLogFailed + clippedTo(oneLine(err.Error()), DispatchLogReasonLimit)
	case report.Paused != nil:
		return DispatchLogPaused + " by " + report.Paused.Actor
	case len(report.Started) == 0 && len(report.Passed) == 0:
		return DispatchLogNothingReady
	}
	return fmt.Sprintf("%s%d started", DispatchLogOK, len(report.Started))
}

// now is the clock the log is dated by.
func (d Dispatch) now() time.Time {
	if d.Now == nil {
		return time.Now()
	}
	return d.Now()
}

// run is one dispatch, without the line it leaves in the log.
func (d Dispatch) run(ctx context.Context) (DispatchReport, error) {
	switch {
	case d.Tracker == nil || d.Worktrees == nil || d.Runner == nil:
		return DispatchReport{}, fmt.Errorf("dispatching: a dispatch needs a work tracker, worktrees and a runner")
	case d.Host == "":
		return DispatchReport{}, fmt.Errorf("dispatching: which host is this? set MW_HOST, or host in the config file")
	case d.Cap < 1:
		return DispatchReport{}, fmt.Errorf("dispatching on %s: the cap on sessions running at once is %d, so nothing could be started; set cap in the config file", d.Host, d.Cap)
	}
	report := DispatchReport{Host: d.Host, Cap: d.Cap, DryRun: d.DryRun}

	// A paused host starts nothing, and is not even synced: a pause is a word
	// that the host is to be left alone until a resume-host says otherwise.
	if d.Events != nil {
		pause, paused, err := PausedHost(ctx, d.Events, d.Host)
		if err != nil {
			report.Notes = append(report.Notes, err.Error())
		} else if paused {
			report.Paused = &pause
			d.print(fmt.Sprintf("dispatch on %s: paused by %s at %s (control event %d); nothing done until a resume-host event\n",
				d.Host, pause.Actor, pause.At.UTC().Format(time.RFC3339), pause.Seq))
			return report, nil
		}
	}

	// Asked before the sync, so that the backup it may skip reads the answer
	// this tick keeps rather than asking again.
	var network *NetworkReading
	if d.Network != nil {
		reading := d.Network.Read(ctx)
		network = &reading
	}

	// A dry run writes nothing at all, and a sync writes: it pushes this host's
	// commits and pulls the other's. So a dry run reads the view this host
	// already has, and says so.
	if !d.DryRun && d.Sync != nil {
		synced, retries, err := d.syncWaitingForTheNetwork(ctx)
		report.SyncRetries = retries
		if fault, gaveUp := LocalFault(err); gaveUp {
			// Not a failed run: the network is not back yet, and the next tick tries
			// again. The one line is printed here, so that nothing else is.
			d.print(fault.Line() + "\n")
			return report, err
		}
		if err != nil {
			RecordSyncHalt(ctx, d.SyncHalts, err, d.now())
			d.tellVaultBlocked(ctx, err, &report)
			return report, fmt.Errorf("dispatching on %s: the hosts could not be brought level, so nothing was claimed: %w", d.Host, err)
		}
		KeepSyncHalt(ctx, d.SyncHalts, synced, nil, d.now())
		d.clearVaultBlocked(ctx)
		report.Sync, report.Synced = synced, true
		d.print(fmt.Sprintf("  synced  %s\n", synced))
	}

	running, err := d.Tracker.RunningStories(ctx, d.Host)
	if err != nil {
		return report, fmt.Errorf("dispatching on %s: reading what is already running here: %w", d.Host, err)
	}
	// A story the Governor must be present for is claimed by the Mayor, not run
	// by a session of this host, so it is not one of the sessions the cap counts.
	for _, detail := range running {
		if detail.Hitl() {
			continue
		}
		if !d.DryRun {
			reclaimed, held, err := d.reclaimDeadPane(ctx, detail, &report)
			if err != nil {
				return report, fmt.Errorf("dispatching on %s: %w", d.Host, err)
			}
			if reclaimed {
				// Given back below, so it is read fresh among what is ready rather
				// than counted here: a claim this dispatch just returned is not one
				// still holding a session.
				continue
			}
			if held {
				// Refused and waiting on the Mayor: it holds no session, so it
				// does not take one of the cap's slots (mw-gq6.211).
				continue
			}
		} else if d.refusedWithoutSession(ctx, detail) {
			report.HeldRefused = append(report.HeldRefused, HeldRefused{StoryID: detail.Story.ID, Session: SessionName(detail.Story.ID)})
			continue
		}
		report.Running++
	}

	// The room is read once a pass. A pass that is only looking leaves no word of
	// it in the log.
	noRoom, roomRead := ReadRoom(ctx, d.Load, d.Room)
	// Grist comes first (mw-t0z3fu.4): slow tutor answers are a want of room,
	// and a tutor in use lowers the cap.
	limit := d.Cap
	var pulse GristPulse
	if d.Grist != nil {
		var err error
		if pulse, err = d.Grist.Pulse(ctx, d.Cap); err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("grist could not be read, so it holds nothing back: %v", err))
		} else {
			roomRead = roomRead || pulse.Slow != ""
			noRoom = strings.Join(nonEmpty(noRoom, pulse.Slow), "; ")
			limit = pulse.Cap
		}
	}
	if roomRead && !d.DryRun {
		d.tellRoom(ctx, noRoom, &report)
	}
	free := limit - report.Running

	ready, err := d.Tracker.ReadyForHost(ctx, d.Host)
	if err != nil {
		return report, fmt.Errorf("dispatching on %s: reading what is ready here: %w", d.Host, err)
	}
	ready = inStartOrder(ready)

	// The formulas installed are read once, and only if a story names one. A
	// dry run reads them too, so that it reports the same refusal a real run
	// would, without writing anything.
	var formulas map[string]bool
	smokeHolds := map[string]string{}
	for _, detail := range ready {
		id := detail.Story.ID
		// First, so that a story that is not a session's to take is never passed
		// over for the cap instead: the cap is not what stops it.
		if detail.Hitl() {
			report.Passed = append(report.Passed, Passed{StoryID: id, Why: fmt.Sprintf(
				"the Governor must be present for it (labelled %s): it is worked with the Mayor, never by a dispatched session", LabelHitl)})
			continue
		}
		path, err := detail.Path()
		if err != nil {
			report.Passed = append(report.Passed, Passed{StoryID: id, Why: "it has no path: " + err.Error()})
			continue
		}
		// The tracker filters by host too; this is the second lock on the same
		// door, because starting another host's story is the one mistake a
		// two-host factory cannot undo by itself. A story that names no host is
		// nobody's: the Mayor has not said where it is worked yet.
		if path.Host == "" {
			report.Passed = append(report.Passed, Passed{StoryID: id, Why: "its path names no host, so no host may take it"})
			continue
		}
		// A story that may run on any host passes the host check on every host:
		// the cap is checked below as it is for any story.
		auto := path.Host == domain.HostAuto
		if path.Host != d.Host && !auto {
			report.Passed = append(report.Passed, Passed{StoryID: id, Why: "it is worked on " + path.Host})
			continue
		}
		// A story this host would take waits for the room to take it, whichever
		// host it names (mw-t0z3fu.1). It stays ready, claimed by nobody.
		if noRoom != "" {
			report.Passed = append(report.Passed, Passed{StoryID: id, Why: fmt.Sprintf("%s has no room: %s", d.Host, noRoom)})
			continue
		}

		// A story whose last run landed it is finished but for its close, which
		// was lost: claiming it would work it again on top of its own landed
		// commits (mw-gq6.191). The run state is read off the listing.
		if hasLabel(detail.Labels, RunState+":"+RunLanded) {
			report.Passed = append(report.Passed, Passed{StoryID: id, Why: fmt.Sprintf(
				"its last run landed it (%s=%s) and only its close was lost, so it is not claimed again: `mw next %s` closes it, and more work on it is a new story",
				RunState, RunLanded, id)})
			continue
		}

		// bd's own ready set is trusted for everything except this: a dependency
		// filed on a story moments after bd decided it was ready is not always
		// caught by it (mw-gq6.93), so what the candidate still waits on is read
		// back fresh here rather than taken on the ready listing's word.
		if blocker, status, blocked, err := d.blockedBy(ctx, detail); err != nil {
			return report, fmt.Errorf("dispatching on %s: %w", d.Host, err)
		} else if blocked {
			report.Passed = append(report.Passed, Passed{StoryID: id, Why: fmt.Sprintf(
				"waits on %s (%s)", blocker, status)})
			continue
		}

		rigDir, checkedOut := d.Rigs[path.Rig]
		if !checkedOut {
			report.Passed = append(report.Passed, Passed{StoryID: id, Why: fmt.Sprintf(
				"the rig %s is not checked out on %s: add it under [rigs] in the config file", path.Rig, d.Host)})
			continue
		}

		// A rig whose last grist smoke failed holds its open stories, so that no
		// more work lands on a mill that does not answer as its examples say.
		if held := d.smokeHold(ctx, path.Rig, smokeHolds); held != "" {
			report.Passed = append(report.Passed, Passed{StoryID: id, Why: held})
			continue
		}

		// A story on a rig that installs dependencies waits for a network that is
		// not metered. It stays open and unclaimed, so no attempt is spent on it.
		if network != nil && network.Metered && d.HeavyNet[path.Rig] {
			report.Passed = append(report.Passed, Passed{StoryID: id, Why: fmt.Sprintf(
				"the rig %s installs dependencies and the network is metered %s: it waits for one that is not", path.Rig, network.Why())})
			continue
		}

		// A story whose formula this vault has not installed is refused before
		// the cap, the same as one whose attempts are exhausted: claiming it
		// would start a session with nothing poured to work from, over and over,
		// for a mistake only a person filing the story again can fix (mw-gq6.95).
		if path.Formula != "" {
			if formulas == nil {
				if formulas, err = d.installedFormulas(ctx); err != nil {
					return report, fmt.Errorf("dispatching on %s: %w", d.Host, err)
				}
			}
			if !formulas[path.Formula] {
				report.Passed = append(report.Passed, Passed{StoryID: id, Why: fmt.Sprintf(
					"its formula %s is not installed here, so it is not claimed", path.Formula)})
				if !d.DryRun {
					d.refuseUninstalledFormula(ctx, id, path.Formula, &report)
				}
				continue
			}
		}

		// Before the cap, because a story that is not started takes none of the
		// sessions the cap counts, and the Mayor is to be told of it now rather
		// than once whatever is running has finished.
		if tried := detail.Attempts; tried >= d.maxAttempts() {
			report.Passed = append(report.Passed, Passed{StoryID: id, Why: fmt.Sprintf(
				"it has been started %d times, the most a story may be (max_attempts): it is not started again until its counter is reset by hand", tried)})
			if !d.DryRun {
				d.exhausted(ctx, detail, &report)
			}
			continue
		}

		// A story that was claimed and could not be started counts against the
		// cap too: whatever stopped it will probably stop the next one, and a
		// dispatcher that claims and releases every ready story in turn is
		// worse than one that stops and says so.
		if len(report.Started)+len(report.Failed) >= free {
			why := fmt.Sprintf("%s has taken %d of the %d sessions it may run at once", d.Host,
				report.Running+len(report.Started)+len(report.Failed), limit)
			if pulse.Lowered() {
				why += " while a tutor is in use (grist first)"
			}
			report.Passed = append(report.Passed, Passed{StoryID: id, Why: why})
			continue
		}

		started, released, err := d.start(ctx, detail, path, rigDir, formulas)
		if started.StoryID != "" {
			report.Started = append(report.Started, started)
		}
		if err != nil {
			var pour *pourRefusal
			if errors.As(err, &pour) {
				// Not a failed run either: the story is blocked with the reason on
				// it, the claim kept, and the next dispatch holds it as refused.
				report.Passed = append(report.Passed, Passed{StoryID: id, Why: pour.Error()})
			} else {
				report.Failed = append(report.Failed, Failed{StoryID: id, Err: err, Released: released})
				d.tellStuck(ctx, id, err, &report)
			}
		} else if started.StoryID != "" {
			d.clearStuck(ctx, id)
		}
	}

	d.print(report.String())
	d.answerWaitingGrist(ctx, &report)
	if len(report.Failed) > 0 {
		return report, fmt.Errorf("dispatching on %s: %s", d.Host, report.failures())
	}
	return report, nil
}

// answerWaitingGrist runs one pass of the mill after the claims, on the host
// that is home: a grist left waiting for a slot, or for a busy pass, is
// answered by the next tick and not only when another grist arrives. The pass
// is the mill's own, with its own locks and its own limit. What goes wrong
// in it is a note: the tick's claims have been made and stand.
func (d Dispatch) answerWaitingGrist(ctx context.Context, report *DispatchReport) {
	if d.Mill == nil || d.Home == nil || d.DryRun {
		return
	}
	home, err := IsHome(ctx, d.Home, d.Host)
	if unknown, ok := HomeUnknownIn(err); ok {
		note := "no grist pass was run: " + unknown.Error()
		report.Notes = append(report.Notes, note)
		d.print("  note    " + note + "\n")
		return
	}
	if err != nil {
		note := fmt.Sprintf("no grist pass was run: %v", err)
		report.Notes = append(report.Notes, note)
		d.print("  note    " + note + "\n")
		return
	}
	if !home {
		return
	}
	grist, err := d.Mill.Run(ctx)
	report.Grist = &grist
	if err != nil {
		note := fmt.Sprintf("the grist pass failed: %v", err)
		report.Notes = append(report.Notes, note)
		d.print("  note    " + note + "\n")
	}
}

// syncWaitingForTheNetwork runs the sync, and runs it again after SyncWait while
// what stops it is a name that could not be resolved, up to SyncTries tries. It
// reports how many times it had to try again. When the name still cannot be
// resolved it is a *LocalNetworkFault; every other failure comes back at once,
// as it was.
func (d Dispatch) syncWaitingForTheNetwork(ctx context.Context) (SyncReport, int, error) {
	tries := max(d.SyncTries, 1)
	for try := 1; ; try++ {
		report, err := d.Sync.Run(ctx)
		unresolved, down := Unresolved(err)
		if !down {
			return report, try - 1, err
		}
		if try >= tries {
			return report, try - 1, &LocalNetworkFault{Said: unresolved.Said, Tries: try}
		}
		wait := d.Wait
		if wait == nil {
			wait = waitFor
		}
		if err := wait(ctx, d.SyncWait); err != nil {
			return report, try - 1, fmt.Errorf("waiting %s for the network to come back: %w", d.SyncWait, err)
		}
	}
}

// tellRoom writes the job event for a change of this host's room from what the
// log last said of it: one when it has none, one when it has it back, and none
// for a host whose room stays as it was. A host the log has said nothing of
// has room. What cannot be read or written is a note, and the next pass tries
// again.
func (d Dispatch) tellRoom(ctx context.Context, noRoom string, report *DispatchReport) {
	if d.Events == nil {
		return
	}
	held, err := roomHeld(ctx, d.Events, d.Host)
	if err != nil {
		report.Notes = append(report.Notes, err.Error())
		return
	}
	if held == (noRoom != "") {
		return
	}
	event := events.Event{Kind: events.KindJob, Actor: JobRoom + "@" + d.Host, From: events.JobRunning}
	if noRoom != "" {
		event.To = events.JobFailed
		event.Detail = events.CutDetail(fmt.Sprintf("%s has no room: %s; it starts no story until it has", d.Host, noRoom))
	} else {
		event.To = events.JobDone
		event.Detail = fmt.Sprintf("%s has room again: it starts stories up to its cap of %d", d.Host, d.Cap)
	}
	if _, err := (EventEmit{Log: d.Events, Now: d.now, Event: event}).Run(ctx); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the room event could not be written: %v", err))
	}
}

// nonEmpty is the words that say something.
func nonEmpty(words ...string) []string {
	var out []string
	for _, w := range words {
		if w != "" {
			out = append(out, w)
		}
	}
	return out
}

// roomHeld reports whether the log's latest word of host's room is that it has
// none. The log is read whole, as a pause is: a pass reads it once.
func roomHeld(ctx context.Context, log EventLog, host string) (bool, error) {
	evs, err := log.Since(ctx, 0)
	if err != nil {
		return false, fmt.Errorf("reading the event log for the room of %s: %w", host, err)
	}
	held := false
	for _, ev := range evs {
		if ev.Kind == events.KindJob && ev.Actor == JobRoom+"@"+host {
			held = ev.To == events.JobFailed
		}
	}
	return held, nil
}

// start dispatches one story: claim, worktree, formula, boot, session, and the
// record of it on the story. It reports what was started, whether the claim was
// given back, and what went wrong.
//
// Everything between the claim and the session starting is undone if anything
// fails — the worktree is removed and the claim given back — so that a story
// nobody is working is left exactly as ready as it was found. Once the session
// is running, nothing is undone: a claim given back under a live session is how
// one story gets worked twice.
func (d Dispatch) start(ctx context.Context, detail StoryDetail, path domain.Path, rigDir string, formulas map[string]bool) (Started, bool, error) {
	id := detail.Story.ID
	started := Started{
		StoryID:  id,
		Title:    detail.Story.Title,
		Path:     path,
		Worktree: WorktreeDir(rigDir, id),
		Branch:   StoryBranch(id),
		Start:    StartPoint(d.remote(), path.Branch),
		Session:  SessionName(id),
		Auto:     path.Host == domain.HostAuto,
	}
	if d.DryRun {
		return started, false, nil
	}

	// A session already holding the story's name is looked at before anything
	// is taken, so that a refusal leaves everything as it was: the worktree the
	// story may already have is a live session's, and nothing here may undo it.
	lying, err := d.namesake(ctx, started.Session, id)
	if err != nil {
		return Started{}, false, err
	}

	if err := d.Tracker.ClaimStory(ctx, id); err != nil {
		return Started{}, false, fmt.Errorf("claiming %s: %w", id, err)
	}
	d.print(fmt.Sprintf("  claimed %s\n", id))

	// The ready list was read before the claim, and another dispatcher may have
	// poured this story's formula and recorded the molecule since: its session
	// may even be started. The molecule is read again, now that the claim is held,
	// so that a second start never pours over one already handed out
	// (mw-gq6.193); a session of the story's name that has appeared since is
	// release's to find, and release keeps the claim for it.
	fresh, err := d.Tracker.ShowStory(ctx, id)
	if err != nil {
		released, relErr := d.release(ctx, id, fmt.Errorf("reading %s again after claiming it: %w", id, err))
		return Started{}, released, relErr
	}
	detail.Molecule = fresh.Molecule

	// The claim is what makes the story this host's, so it is also what writes
	// this host's name over "auto": mw status, the landing and the ledger all see
	// a concrete host from here on. The one writer of that field is this line.
	if started.Auto {
		if err := d.Tracker.SetStoryMetadata(ctx, id, map[string]string{"host": d.Host}); err != nil {
			released, relErr := d.release(ctx, id, fmt.Errorf("recording the host of %s as %s: %w", id, d.Host, err))
			return Started{}, released, relErr
		}
		path.Host, started.Path.Host = d.Host, d.Host
		detail.Story.Overrides.Host = d.Host
	}
	// A claim given back leaves the story as ready as it was found, and a story
	// that was free to run on any host is still free to.
	handedBack := func(released bool) {
		if released && started.Auto {
			_ = d.Tracker.SetStoryMetadata(context.WithoutCancel(ctx), id, map[string]string{"host": domain.HostAuto})
		}
	}

	// recorded is whether the story names the molecule this dispatch poured, and
	// so whether the next dispatch will find it.
	var recorded bool

	// undo gives back everything this dispatch took, and says what it could not.
	undo := func(what string, why error, worktree bool) (Started, bool, error) {
		failed := fmt.Errorf("%s of %s: %w", what, id, why)
		if worktree {
			// A session of the story's name running now is another dispatcher's, who
			// won the race for the story: the worktree and branch are its, and
			// removing them would pull the ground from under a live session
			// (mw-gq6.140). release, below, keeps the claim for the same reason.
			if status, err := d.Runner.Status(context.WithoutCancel(ctx), SessionName(id)); err == nil && status.Running() {
				failed = fmt.Errorf("%w (the session %s of %s is running here, so no worktree or branch was removed)", failed, status.Name, id)
			} else if err := d.Worktrees.Remove(ctx, rigDir, started.Worktree, started.Branch); err != nil {
				failed = fmt.Errorf("%w (and the worktree %s is still there: %v)", failed, started.Worktree, err)
			}
		}
		// Beads are never deleted here, so a formula poured before the failure
		// stays where it is, and is said so rather than quietly left. One the story
		// records is worked by the next dispatch; one it does not is not found.
		if started.Molecule.Poured() && !started.Reused {
			if recorded {
				failed = fmt.Errorf("%w (the formula was already poured as %s, which is left behind; the next dispatch works it while it is open)",
					failed, started.Molecule.RootID)
			} else {
				failed = fmt.Errorf("%w (the formula was already poured as %s, which is left behind and not recorded on the story; the next dispatch pours another)",
					failed, started.Molecule.RootID)
			}
		}
		released, err := d.release(ctx, id, failed)
		handedBack(released)
		return Started{}, released, err
	}

	// The target branch as the rig's origin has it now, not as this host saw it
	// last: the other host may have pushed to it since.
	if err := d.Worktrees.Fetch(ctx, rigDir); err != nil {
		return undo("fetching the rig", err, false)
	}

	leftover, err := d.leftoverAt(ctx, rigDir, started)
	if err != nil {
		return undo("checking whether an earlier attempt left a worktree or branch of "+id, err, false)
	}
	if leftover {
		leftoverAttempt := detail.Attempts
		if leftoverAttempt < 1 {
			leftoverAttempt = 1
		}
		kept, err := d.keepLeftover(ctx, rigDir, id, started, leftoverAttempt)
		if err != nil {
			return undo("clearing the way for the new worktree", err, false)
		}
		started.KeptBranch = kept
	}

	if err := d.Worktrees.Add(ctx, rigDir, started.Worktree, started.Branch, started.Start); err != nil {
		return undo("cutting the worktree", err, false)
	}

	// A molecule an earlier dispatch poured for this story, and that is still open,
	// is worked on rather than poured again: beads are never deleted, so a second
	// pour would leave the first behind for good, with the steps it had closed.
	if path.Formula != "" && detail.Molecule.RootID != "" {
		open, err := d.Tracker.OpenMolecule(ctx, detail.Molecule.RootID)
		if err != nil {
			return undo("reading the molecule "+detail.Molecule.RootID, err, true)
		}
		if open.Poured() {
			open.Formula = path.Formula
			detail.Molecule, started.Molecule, started.Reused = open, open, true
		}
	}

	if !started.Reused && path.Formula != "" && formulas[path.Formula] {
		title, err := d.titleToPour(ctx, path.Formula, id, detail.Story.Title)
		if err != nil {
			return undo("reading the step titles of the formula "+path.Formula, err, true)
		}
		molecule, err := d.Tracker.PourFormula(ctx, path.Formula, id, title, domain.IsBugStory(detail.Type, detail.Story.Title))
		var refused *PourRefused
		if errors.As(err, &refused) {
			return d.refusePour(ctx, id, started, rigDir, refused)
		}
		if err != nil {
			return undo("pouring the formula "+path.Formula, err, true)
		}
		detail.Molecule, started.Molecule = molecule, molecule

		// The steps are the session's to close, and whoever closes the story
		// out has to find them without reading the boot file back.
		if err := d.Tracker.SetStoryMetadata(ctx, id, map[string]string{MoleculeField: molecule.RootID}); err != nil {
			return undo("recording the molecule "+molecule.RootID, err, true)
		}
		recorded = true
	}

	spec, err := d.Boot.Boot(ctx, detail, started.Worktree)
	if err != nil {
		return undo("assembling the session", err, true)
	}
	// Only now, with everything else in place, is the dead session cleared away:
	// its output is all that is left of the run before, and a dispatch that fails
	// on the way here has no use for the name.
	if lying {
		if err := d.Runner.Close(ctx, spec.Name); err != nil {
			return undo("clearing the dead session "+spec.Name, err, true)
		}
	}
	if err := d.Runner.Start(ctx, spec); err != nil {
		return undo("starting the session", err, true)
	}
	started.Session = spec.Name
	d.print(fmt.Sprintf("  launched %s · session %s\n", id, spec.Name))

	// From here the session is alive and spending fuel. Nothing below is worth
	// undoing it for.
	started.Attempt = detail.Attempts + 1
	var unrecorded []error
	// What mw sweep remembered of the session this replaces is no use to this
	// one: its pane starts blank too, and would otherwise be read as this
	// attempt's own silence, since the old attempt's clock (mw-gq6.86).
	if d.Memory != nil {
		if err := d.Memory.ClearNote(ctx, SweepKey(id)); err != nil {
			unrecorded = append(unrecorded, fmt.Errorf("what mw sweep remembered of the last attempt could not be cleared: %w", err))
		}
	}
	if err := d.Tracker.SetStoryMetadata(ctx, id, attemptFields(detail, started.Attempt, d.now())); err != nil {
		unrecorded = append(unrecorded, fmt.Errorf("the attempt could not be recorded as %s=%d: %w", AttemptsField, started.Attempt, err))
	}
	where := fmt.Sprintf("dispatched by mw on %s: session %s in %s on %s", d.Host, spec.Name, started.Worktree, started.Branch)
	if started.Attempt > 1 {
		where += fmt.Sprintf(", attempt %d", started.Attempt)
	}
	if started.KeptBranch != "" {
		where += fmt.Sprintf(", after keeping what an earlier attempt left as %s", started.KeptBranch)
	}
	if molecule := started.Molecule; molecule.Poured() {
		if started.Reused {
			where += fmt.Sprintf(", working %s again (%d steps still open)", molecule.RootID, len(molecule.Steps))
		} else {
			where += fmt.Sprintf(", working %s (%d steps)", molecule.RootID, len(molecule.Steps))
		}
	}
	if err := d.Tracker.SetStoryState(ctx, id, RunState, RunRunning, where); err != nil {
		unrecorded = append(unrecorded, fmt.Errorf("%s could not be recorded as %s=%s: %w", id, RunState, RunRunning, err))
	}
	if len(unrecorded) > 0 {
		return started, false, fmt.Errorf("the session %s is running in %s, but %w",
			spec.Name, started.Worktree, errors.Join(unrecorded...))
	}
	return started, false, nil
}

// leftoverAt reports whether an earlier attempt left the worktree or the
// branch a fresh cut is about to take — the shape a dead-pane reclaim or a mw
// retry leaves behind, its session gone but the git it made never cleared away
// (mw-gq6.107, mw-gq6.326). Without Landing wired in, nothing here is ever
// called a leftover, and Add's own failure decides what happens next.
//
// A live session already running under the story's name is never a leftover,
// whatever Exists says: that is the race two dispatchers can lose to each
// other in the same instant (mw-gq6.96), and moving a branch a session is
// working on right now would pull the ground from under it. Add's own failure
// and the namesake check release makes right before giving a claim back both
// still cover that race exactly as they did before this existed.
func (d Dispatch) leftoverAt(ctx context.Context, rigDir string, started Started) (bool, error) {
	if d.Landing == nil {
		return false, nil
	}
	exists, err := d.Worktrees.Exists(ctx, rigDir, started.Worktree, started.Branch)
	if err != nil || !exists {
		return false, err
	}
	if status, err := d.Runner.Status(ctx, started.Session); err == nil && status.Running() {
		return false, nil
	}
	return true, nil
}

// keepLeftover clears an earlier attempt's worktree and branch out of the way of
// a fresh cut without losing anything (mw-gq6.326). A worktree holding
// uncommitted work is refused, naming it and what is in it: it is somebody's to
// settle by hand, and nothing is changed. Otherwise the clean worktree is taken
// away (never forced) and the branch is renamed to mw/<id>-attempt<N>, N the
// attempt that made it, with a number added if that name is taken by an attempt
// kept before; no branch is ever deleted. It returns the name the branch was
// kept under, empty when there was no branch.
func (d Dispatch) keepLeftover(ctx context.Context, rigDir, id string, started Started, attempt int) (string, error) {
	hasDir, err := d.Worktrees.Exists(ctx, rigDir, started.Worktree, "")
	if err != nil {
		return "", err
	}
	hasBranch, err := d.Worktrees.Exists(ctx, rigDir, "", started.Branch)
	if err != nil {
		return "", err
	}
	if hasDir {
		left, err := d.Landing.Uncommitted(ctx, started.Worktree)
		if err != nil {
			return "", fmt.Errorf("reading what the worktree %s left uncommitted: %w", started.Worktree, err)
		}
		if len(left) > 0 {
			return "", fmt.Errorf("the worktree %s of an earlier attempt holds uncommitted work (%s), so it and its branch %s were left as they were; commit or discard it by hand, or mw retry %s",
				started.Worktree, strings.Join(left, ", "), started.Branch, id)
		}
		if err := d.Worktrees.RemoveWithoutForce(ctx, rigDir, started.Worktree); err != nil {
			return "", fmt.Errorf("the worktree %s of an earlier attempt could not be removed: %w", started.Worktree, err)
		}
	}
	if !hasBranch {
		return "", nil
	}
	kept := fmt.Sprintf("%s-attempt%d", started.Branch, attempt)
	for n := 2; ; n++ {
		taken, err := d.Worktrees.Exists(ctx, rigDir, "", kept)
		if err != nil {
			return "", err
		}
		if !taken {
			break
		}
		kept = fmt.Sprintf("%s-attempt%d-%d", started.Branch, attempt, n)
	}
	if err := d.Worktrees.RenameBranch(ctx, rigDir, started.Branch, kept); err != nil {
		return "", fmt.Errorf("the branch %s of an earlier attempt could not be kept as %s: %w", started.Branch, kept, err)
	}
	return kept, nil
}

// ReasonPourRefused is the code a story is marked blocked under when the
// tracker refused to pour its formula.
const ReasonPourRefused Reason = "pour-refused"

// PourRefused is the tracker refusing to pour a formula for a story for a
// reason of the story's own, one that pouring again will not cure: a title that
// with a step's wording before it is over the limit (mw-gq6.244). A tracker
// that merely could not be reached is no such refusal, and returns its own
// error.
type PourRefused struct {
	Formula string
	Story   string
	Reason  string
}

func (e *PourRefused) Error() string {
	return fmt.Sprintf("the tracker refused to pour the formula %s for %s: %s", e.Formula, e.Story, e.Reason)
}

// pourRefusal is what start returns for a PourRefused, so that run passes the
// story over instead of failing: only this story is stuck, and the stories
// after it are still worked.
type pourRefusal struct{ err error }

func (e *pourRefusal) Error() string { return e.err.Error() }
func (e *pourRefusal) Unwrap() error { return e.err }

// JobPourRefused is the actor of the job event a refused pour writes, kept
// apart from "dispatch", which is the follower's job of springing dispatches
// and keeps its own state in the log.
const JobPourRefused = "dispatch-pour"

// titleToPour is the story's title as it is poured into its formula's step
// titles: cut short with an ellipsis when the longest of them, with it in, would
// pass the tracker's limit, so that no title can make a pour fail (mw-gq6.295).
// A tracker that cannot say what the step titles are is given the title whole.
func (d Dispatch) titleToPour(ctx context.Context, formula, id, title string) (string, error) {
	reader, ok := d.Tracker.(FormulaTitles)
	if !ok {
		return title, nil
	}
	steps, err := reader.FormulaStepTitles(ctx, formula)
	if err != nil {
		return "", err
	}
	return domain.FitTitleToSteps(title, id, steps), nil
}

// refusePour leaves a story whose pour the tracker refused blocked, the claim
// kept: giving it back would have the next dispatch claim, cut and pour it
// again for the same refusal, as it did five times in a row on 2026-10-03. The
// story is marked run=blocked with the reason, which is what the next dispatch
// reads to pass it over (reclaimDeadPane), a comment says why, and a failed job
// event tells the home. The worktree cut for it is removed, since nothing ran
// in it. What could not be written is said in the error.
func (d Dispatch) refusePour(ctx context.Context, id string, started Started, rigDir string, refused *PourRefused) (Started, bool, error) {
	// Nothing here may be cut short by the tick that ended: the story is owed
	// its record whatever became of the tick.
	record := context.WithoutCancel(ctx)
	why := fmt.Sprintf("%v", refused)
	var unsaid []string

	if err := d.Worktrees.Remove(record, rigDir, started.Worktree, started.Branch); err != nil {
		unsaid = append(unsaid, fmt.Sprintf("the worktree %s is still there: %v", started.Worktree, err))
	}
	if err := d.Tracker.SetStoryState(record, id, RunState, RunBlocked, "("+string(ReasonPourRefused)+") "+why); err != nil {
		unsaid = append(unsaid, fmt.Sprintf("it could not be recorded as %s=%s: %v", RunState, RunBlocked, err))
	}
	comment := fmt.Sprintf("mw dispatch on %s did not start this story (%s): %s. It is left claimed and blocked, and is not tried again by dispatch "+
		"until whoever cures the cause (its title, or its formula) gives the claim back.", d.Host, ReasonPourRefused, why)
	if err := d.Tracker.CommentOnStory(record, id, comment); err != nil {
		unsaid = append(unsaid, fmt.Sprintf("the comment could not be written: %v", err))
	}
	if d.Events != nil {
		_, err := EventEmit{
			Log: d.Events, Now: d.now,
			Event: events.Event{
				Kind: events.KindJob, Actor: JobPourRefused + "@" + d.Host,
				From: events.JobRunning, To: events.JobFailed,
				Detail: events.CutDetail(fmt.Sprintf("%s blocked: %s", id, why)),
			},
		}.Run(record)
		if err != nil {
			unsaid = append(unsaid, fmt.Sprintf("the job event could not be written: %v", err))
		}
	}
	d.print(fmt.Sprintf("  blocked %s: %s\n", id, why))

	result := fmt.Errorf("%s: blocked, claim kept: %s", id, why)
	if len(unsaid) > 0 {
		result = fmt.Errorf("%w (%s)", result, strings.Join(unsaid, "; "))
	}
	return Started{}, false, &pourRefusal{err: result}
}

// reclaimDeadPane looks at one story this host already has claimed, and gives
// the claim back when its tmux window is still there but its pane has died
// and ReclaimStory says the tracker's own lease on the claim has run out:
// together the strongest sign that the session ended without mw next ever
// hearing about it (the Laptop's DNS outage of 2026-09-24, mw-gq6.106). A
// live session, a window gone outright, or a lease ReclaimStory says still
// holds are all left exactly as they were — counted running, the same as
// before this existed — because either sign alone is not enough to act on
// without a person's word.
//
// A story its close-out refused (mw next recorded run=blocked) is left exactly
// as it is however dead its pane: its claim, worktree and branch are the
// evidence of the refusal, and giving the claim back would run it again
// without the Mayor or the Governor having said so (mw-gq6.182). Its session
// is not alive, so it is reported held, and the caller does not count it toward
// the cap (mw-gq6.211); the same holds when its window is gone outright.
//
// A story that landed is never given back either (mw-gq6.191): one whose last
// run recorded run=landed, or, where that record was lost or written over, the
// newest line its seat's ledger holds for it says landed, is closed once its
// pane is dead and its lease lapsed, exactly as a merged branch is. A close
// that fails keeps the claim, and the next tick tries it again.
//
// Nothing is cut or removed here: only the window, whose pane is already
// dead, is closed, and the claim given back. What the story's worktree and
// branch still hold from the attempt that died is left for the next attempt
// to run into, or for a person to settle by hand with mw retry.
func (d Dispatch) reclaimDeadPane(ctx context.Context, detail StoryDetail, report *DispatchReport) (reclaimed, held bool, err error) {
	id := detail.Story.ID
	name := SessionName(id)
	status, err := d.Runner.Status(ctx, name)
	if err != nil {
		// A runner that cannot say cannot be trusted to say the pane is dead
		// either: leave the claim counted as running, as if this check were
		// never made.
		return false, false, nil
	}
	if status.Running() {
		return false, false, nil
	}
	dead := status.State == StateExited || status.State == StateExitUnknown

	// A claim whose lease has run out and whose branch is already on the target
	// branch is a close-out that got past the merge and lost only the close
	// (mw-gq6.161). It is closed, never given back: a fresh attempt would work
	// the story a second time on top of its own landed commits. Whether the
	// lease ran out is read here from the story, not from ReclaimStory, because
	// that one gives the claim back as it answers.
	lapsed := dead && !detail.LeaseExpires.IsZero() && d.now().After(detail.LeaseExpires)
	if lapsed {
		if tip, target, landed := d.landedBranch(ctx, detail); landed {
			by := fmt.Sprintf("its branch %s (tip %s) is already contained in %s", StoryBranch(id), shortCommit(tip), target)
			return d.closeLanded(ctx, detail, name, LandedAlready{Tip: tip, Target: target, By: by}, report), false, nil
		}
	}

	// Read before ReclaimStory, which gives the claim back as it answers. A
	// state that cannot be read is no licence to give the claim back.
	run, err := d.Tracker.StoryState(ctx, id, RunState)
	if err != nil {
		if dead {
			report.Notes = append(report.Notes, fmt.Sprintf(
				"%s: its session %s has a dead pane, but whether its close-out refused it could not be read, so its claim was kept: %v", id, name, err))
		}
		return false, false, nil
	}
	if run == RunBlocked {
		report.HeldRefused = append(report.HeldRefused, HeldRefused{StoryID: id, Session: name})
		return false, true, nil
	}
	if !dead {
		return false, false, nil
	}
	if run == RunLanded {
		// Never given back: closed once the lease has lapsed, and until then
		// left for one more tick, exactly as a live session is.
		if !lapsed {
			return false, false, nil
		}
		by := fmt.Sprintf("its last run was recorded %s=%s", RunState, RunLanded)
		return d.closeLanded(ctx, detail, name, LandedAlready{Target: d.target(detail), By: by}, report), false, nil
	}
	if lapsed {
		if by, landed := d.ledgeredAsLanded(ctx, detail); landed {
			return d.closeLanded(ctx, detail, name, LandedAlready{Target: d.target(detail), By: by}, report), false, nil
		}
	}

	reclaimed, err = d.Tracker.ReclaimStory(ctx, id)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf(
			"%s: its session %s has a dead pane, but whether its lease had run out could not be read: %v", id, name, err))
		return false, false, nil
	}
	if !reclaimed {
		// The lease still holds: left alone for one more tick, exactly as a
		// live session is.
		return false, false, nil
	}

	// The session ended and no close-out ever ran, so the attempt it used is
	// given back: a story is not spent by sessions that never got to say
	// whether it worked (mw-y0dkzp). A refusal (run=blocked) returned above and
	// is never refunded. The tick re-reads the story, so the next start counts
	// from the lowered number.
	refund := fmt.Sprintf("its attempt count of %d is unchanged, as the refund could not be recorded", detail.Attempts)
	if detail.Attempts > 0 {
		lowered := detail.Attempts - 1
		if err := d.Tracker.SetStoryMetadata(ctx, id, map[string]string{AttemptsField: strconv.Itoa(lowered)}); err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf(
				"%s: its dead-pane attempt could not be refunded (%s=%d): %v", id, AttemptsField, lowered, err))
		} else {
			refund = fmt.Sprintf("that attempt was refunded (%s %d -> %d), as its session ended without a close-out", AttemptsField, detail.Attempts, lowered)
		}
	} else {
		refund = "no attempt was recorded, so none was refunded"
	}

	why := fmt.Sprintf(
		"mw dispatch on %s found %s claimed here with a dead pane (%s is %s) and its lease had run out with no heartbeat since: "+
			"the window was closed and the claim given back so it is dispatched again as a fresh attempt; %s.",
		d.Host, id, name, status.State, refund)

	if err := d.Runner.Close(ctx, name); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf(
			"%s: its session %s had a dead pane and its lease had run out, and the claim was given back, but the window could not be closed: %v",
			id, name, err))
	}
	if err := d.Tracker.CommentOnStory(ctx, id, why); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: the dead-pane reclaim could not be commented on: %v", id, err))
	}
	report.Reclaimed = append(report.Reclaimed, Reclaimed{StoryID: id, Session: name, LeaseExpired: detail.LeaseExpires})
	return true, false, nil
}

// refusedWithoutSession says whether a claimed story's close-out refused it
// (run=blocked) and its session is not alive, the read-only look a dry run
// takes where a real one calls reclaimDeadPane. Anything it cannot read is not
// refused: the claim is counted as running, as before.
func (d Dispatch) refusedWithoutSession(ctx context.Context, detail StoryDetail) bool {
	status, err := d.Runner.Status(ctx, SessionName(detail.Story.ID))
	if err != nil || status.Running() {
		return false
	}
	run, err := d.Tracker.StoryState(ctx, detail.Story.ID, RunState)
	return err == nil && run == RunBlocked
}

// landedBranch reports the tip of the story's branch when that branch has work
// of its own and every commit of it is already on the story's target branch,
// as the rig's origin has it or as this checkout has it. Anything it cannot
// tell — no Landing wired in, a path or rig it cannot resolve, git failing — is
// not landed: the claim is judged as it was before this was asked.
func (d Dispatch) landedBranch(ctx context.Context, detail StoryDetail) (tip, target string, landed bool) {
	if d.Landing == nil {
		return "", "", false
	}
	path, err := detail.Path()
	if err != nil {
		return "", "", false
	}
	rigDir, checkedOut := d.Rigs[path.Rig]
	if !checkedOut {
		return "", "", false
	}
	branch := StoryBranch(detail.Story.ID)
	for _, base := range []string{StartPoint(d.remote(), path.Branch), path.Branch} {
		if tip, merged, err := d.Landing.MergedInto(ctx, rigDir, branch, base); err == nil && merged {
			return tip, path.Branch, true
		}
	}
	return "", "", false
}

// ledgeredAsLanded reports whether the newest line the seat's ledger holds for
// a claimed story says it landed while its branch has nothing left to land:
// the landing's own record, for when its run=landed was never written or a
// sweep wrote over it. A branch with commits beyond the target is new work on
// a story landed once before, and is not landed. A ledger that cannot be read,
// or no vault to read it from, says no: the claim is judged as before.
func (d Dispatch) ledgeredAsLanded(ctx context.Context, detail StoryDetail) (by string, landed bool) {
	if d.Boot.Vault == nil || d.Boot.Seat == "" {
		return "", false
	}
	lines, err := d.Boot.Vault.ReadLedger(ctx, d.Boot.Seat)
	if err != nil {
		return "", false
	}
	id := detail.Story.ID
	newest := ""
	for _, line := range lines {
		if LedgerNamesStory(line, id) {
			newest = line
		}
	}
	if !LedgerLandsStory(newest, id) {
		return "", false
	}
	if d.Landing != nil {
		if path, err := detail.Path(); err == nil {
			if rigDir, checkedOut := d.Rigs[path.Rig]; checkedOut {
				ahead, err := d.Landing.Ahead(ctx, rigDir, StoryBranch(id), StartPoint(d.remote(), path.Branch))
				if err == nil && ahead > 0 {
					return "", false
				}
			}
		}
	}
	return fmt.Sprintf("the newest line the %s seat's ledger holds for it says it landed", d.Boot.Seat), true
}

// target is the branch a story lands on, as a sentence names it.
func (d Dispatch) target(detail StoryDetail) string {
	if path, err := detail.Path(); err == nil && path.Branch != "" {
		return path.Branch
	}
	return "its target branch"
}

// closeLanded closes a story found already landed, the way a finished close-out
// would have, and reports it; found says how it was known. The claim is never
// given back. A close that fails leaves the claim exactly as it was, counted
// running, for the next tick to try again; nothing else is written until it
// has gone through, so a server that stays unreachable is not written to over
// and over.
func (d Dispatch) closeLanded(ctx context.Context, detail StoryDetail, session string, found LandedAlready, report *DispatchReport) bool {
	id := detail.Story.ID
	found.StoryID, found.Session = id, session
	outcome := fmt.Sprintf("landed on %s already: %s; closed by mw dispatch on %s, "+
		"which found the claim with a dead pane (%s) and its lease run out",
		found.Target, found.By, d.Host, session)
	if err := d.Tracker.CloseStory(ctx, id, outcome); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf(
			"%s: it landed already (%s), but the story could not be closed, so its claim was kept: %v",
			id, found.By, err))
		return false
	}
	if err := d.Runner.Close(ctx, session); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: its session %s had a dead pane, but the window could not be closed: %v", id, session, err))
	}
	if err := d.Tracker.SetStoryState(ctx, id, RunState, RunLanded, outcome); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s could not be recorded as %s=%s: %v", id, RunState, RunLanded, err))
	}
	said := "mw dispatch on " + d.Host + " found " + id + " claimed here with a dead pane (" + session + ") and its lease run out, " +
		"and " + found.By + ": the close-out landed it on " + found.Target + " and only the close of the story was lost, " +
		"so the story was closed rather than dispatched again."
	if err := d.Tracker.CommentOnStory(ctx, id, said); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: the landed close could not be commented on: %v", id, err))
	}
	report.LandedAlready = append(report.LandedAlready, found)
	return true
}

// namesake looks for a session already called name, the one a story is worked
// in. A running one is a real second session, and the story is refused rather
// than started twice. One that has ended is a corpse mw leaves on purpose
// (remain-on-exit) that would make the runner refuse the name for ever; lying
// says there is one to close before the new session starts.
func (d Dispatch) namesake(ctx context.Context, name, id string) (lying bool, err error) {
	status, err := d.Runner.Status(ctx, name)
	if err != nil {
		// A runner that cannot say cannot start the session either, and Start
		// says so on the path that gives the claim back; refusing here would
		// leave the failure unrecorded on the story.
		return false, nil
	}
	switch status.State {
	case StateRunning:
		return false, fmt.Errorf("the session %s of %s is still running here, so it was not started again; nothing was claimed, closed or removed", name, id)
	case StateExited, StateExitUnknown:
		return true, nil
	}
	return false, nil
}

// release gives a claim back and writes on the story why it was given back. It
// reports whether the claim really did go back, and the failure a person should
// read — the original one, with whatever went wrong releasing it after it.
//
// Two dispatchers can claim the same story in the same instant — the check at
// the top of start is not atomic with the claim itself — and then race to cut
// its worktree. The one that loses that race must not clear the claim the
// winner is working under: a session of the story's name is looked for again,
// right here, right before the claim would be given back, and if one is now
// running the claim stays with it untouched. Anywhere else this were checked
// — earlier in start — the winner might not have started its session yet, so
// the race would still be open; here, immediately before release, is as late
// as it can be checked.
func (d Dispatch) release(ctx context.Context, id string, why error) (bool, error) {
	// The give-back runs on a context of its own, detached from the tick's:
	// ctx is exactly what a cancelled tick has already ended, and mw-gq6.108
	// made a real bd or git command refuse outright the instant its context is
	// done. A give-back attempted on that same, already-cancelled context
	// would be refused for the same reason, leaving the claim stuck exactly as
	// mw-gq6.110 found it (mw-1589l.19, TimeoutStartSec, cured by hand). Giving
	// a claim back is owed to the story whatever became of the tick that took
	// it.
	giveBack := context.WithoutCancel(ctx)
	if status, err := d.Runner.Status(giveBack, SessionName(id)); err == nil && status.Running() {
		said := fmt.Sprintf("mw dispatch on %s could not start this story, but the session %s of %s is running here now, so the claim stays with it: %v", d.Host, status.Name, id, why)
		if err := d.Tracker.CommentOnStory(giveBack, id, said); err != nil {
			return false, fmt.Errorf("%w (the session %s of %s is running here now, so the claim was left alone, but that could not be written on the story: %v)", why, status.Name, id, err)
		}
		return false, fmt.Errorf("%w (the session %s of %s is running here now, so the claim was left alone rather than given back)", why, status.Name, id)
	}
	if err := d.Tracker.ReleaseClaim(giveBack, id); err != nil {
		return false, fmt.Errorf("%w (and the claim could not be given back either: %v — %s is claimed by a session that is not running, and needs releasing by hand)", why, err, id)
	}
	d.print(fmt.Sprintf("  gave back %s: %v\n", id, why))
	said := fmt.Sprintf("mw dispatch on %s could not start this story, so the claim was given back and nothing is running: %v", d.Host, why)
	if err := d.Tracker.CommentOnStory(giveBack, id, said); err != nil {
		return true, fmt.Errorf("%w (the claim was given back, but the failure could not be written on the story: %v)", why, err)
	}
	return true, why
}

// blockedBy is the first of a candidate's needs that is not yet finished, and
// its status as the tracker says it now — not as the ready listing said it a
// moment ago. A need already closed is not a wait, the same as everywhere else
// a story's Needs are read (see bead.needs and Brief.waitsOn).
func (d Dispatch) blockedBy(ctx context.Context, detail StoryDetail) (blocker, status string, blocked bool, err error) {
	for _, need := range detail.Needs {
		read, err := d.Tracker.ShowStory(ctx, need)
		if err != nil {
			return "", "", false, fmt.Errorf("reading %s, which %s waits on: %w", need, detail.Story.ID, err)
		}
		if read.Closed() {
			continue
		}
		return need, read.Status, true, nil
	}
	return "", "", false, nil
}

// installedFormulas is the set of formulas the tracker can pour.
func (d Dispatch) installedFormulas(ctx context.Context) (map[string]bool, error) {
	names, err := d.Tracker.Formulas(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading which formulas are installed: %w", err)
	}
	installed := make(map[string]bool, len(names))
	for _, name := range names {
		installed[name] = true
	}
	return installed, nil
}

// ReasonFormulaNotInstalled is the code a comment carries when mw dispatch
// would not claim a story because its path names a formula this vault has
// not installed (mw-gq6.95): the reason refuseUninstalledFormula's own
// comment looks for, so that a second dispatch finding one already there
// says nothing again.
const ReasonFormulaNotInstalled = "formula-not-installed"

// refuseUninstalledFormula tells whoever reads the story that its formula is
// not installed here, once: a story left ready with a formula nobody has
// installed would otherwise be found again on every dispatch and commented on
// every time, drowning the one useful line in noise.
func (d Dispatch) refuseUninstalledFormula(ctx context.Context, id, formula string, report *DispatchReport) {
	comments, err := d.Tracker.StoryComments(ctx, id)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf(
			"%s: whether it was already told its formula is not installed could not be read, so it may be told again: %v", id, err))
	} else {
		for _, comment := range comments {
			if strings.Contains(comment.Text, ReasonFormulaNotInstalled) {
				return
			}
		}
	}

	said := fmt.Sprintf("mw dispatch on %s did not claim this story (%s): its path names the formula %s, "+
		"which is not installed in this vault. It is dispatched once the formula is installed here or the story's path names one that is.",
		d.Host, ReasonFormulaNotInstalled, formula)
	if err := d.Tracker.CommentOnStory(ctx, id, said); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf(
			"%s: the comment about its uninstalled formula could not be written: %v", id, err))
	}
}

// remote is the remote a story's branch is cut from.
func (d Dispatch) remote() string {
	if d.Remote == "" {
		return DefaultRemote
	}
	return d.Remote
}

// print writes the report, when there is somewhere to write it.
func (d Dispatch) print(text string) {
	if d.Out == nil {
		return
	}
	fmt.Fprint(d.Out, text)
}

// String is the report as a person reads it: what was already in flight, what
// was started, what was passed over and what failed.
func (r DispatchReport) String() string {
	var b strings.Builder

	what := "dispatch"
	if r.DryRun {
		what = "dispatch (dry run: nothing was synced, claimed or started)"
	}
	fmt.Fprintf(&b, "%s on %s: %d of %d sessions were already running\n", what, r.Host, r.Running, r.Cap)
	if r.Synced {
		fmt.Fprintf(&b, "  synced  %s\n", r.Sync)
	}
	if r.SyncRetries > 0 {
		times := "times"
		if r.SyncRetries == 1 {
			times = "time"
		}
		fmt.Fprintf(&b, "  retried the sync %d %s: a name could not be resolved until the network came back\n", r.SyncRetries, times)
	}
	for _, landed := range r.LandedAlready {
		fmt.Fprintf(&b, "  closed %s · %s · already landed: %s\n", landed.StoryID, landed.Session, landed.By)
	}
	for _, held := range r.HeldRefused {
		fmt.Fprintf(&b, "  held    %s · %s · refused, waiting on the Mayor: not counted; its claim, worktree and branch are kept (mw retry)\n",
			held.StoryID, held.Session)
	}
	for _, reclaim := range r.Reclaimed {
		fmt.Fprintf(&b, "  reclaimed %s · %s · dead pane, lease expired %s\n", reclaim.StoryID, reclaim.Session,
			reclaim.LeaseExpired.UTC().Format(time.RFC3339))
	}

	verb := "started"
	if r.DryRun {
		verb = "would start"
	}
	for _, started := range r.Started {
		if started.Auto && r.DryRun {
			fmt.Fprintf(&b, "  would take %s (host=%s) here · %s · %s on %s cut from %s", started.StoryID, domain.HostAuto,
				started.Session, started.Worktree, started.Branch, started.Start)
		} else {
			fmt.Fprintf(&b, "  %s %s · %s · %s on %s cut from %s", verb, started.StoryID, started.Session,
				started.Worktree, started.Branch, started.Start)
		}
		if started.Attempt > 1 {
			fmt.Fprintf(&b, " · attempt %d", started.Attempt)
		}
		if started.KeptBranch != "" {
			fmt.Fprintf(&b, " · a leftover from an earlier attempt was kept as %s", started.KeptBranch)
		}
		if molecule := started.Molecule; molecule.Poured() && started.Reused {
			fmt.Fprintf(&b, " · %s reused as %s (%d steps still open)", molecule.Formula, molecule.RootID, len(molecule.Steps))
		} else if molecule.Poured() {
			fmt.Fprintf(&b, " · %s poured as %s (%d steps)", molecule.Formula, molecule.RootID, len(molecule.Steps))
		} else if started.Path.Formula != "" && !r.DryRun {
			fmt.Fprintf(&b, " · the formula %s is not installed here, so no step beads were poured", started.Path.Formula)
		}
		b.WriteString("\n")
	}
	for _, passed := range r.Passed {
		fmt.Fprintf(&b, "  passed  %s · %s\n", passed.StoryID, passed.Why)
	}
	for _, failed := range r.Failed {
		fmt.Fprintf(&b, "  FAILED  %s · %s\n", failed.StoryID, failed.line())
	}
	for _, note := range r.Notes {
		fmt.Fprintf(&b, "  note    %s\n", note)
	}
	if len(r.Started) == 0 && len(r.Failed) == 0 && len(r.Passed) == 0 {
		b.WriteString("  nothing is ready here\n")
	}
	return b.String()
}

// failures is the one sentence a dispatch with failures in it ends with.
func (r DispatchReport) failures() string {
	said := make([]string, 0, len(r.Failed))
	for _, failed := range r.Failed {
		said = append(said, failed.StoryID+": "+failed.line())
	}
	return strings.Join(said, "; ")
}

// line is one failure as a person reads it, saying plainly whether the story is
// ready for somebody else again.
func (f Failed) line() string {
	if f.Released {
		return fmt.Sprintf("%v (the claim was given back)", f.Err)
	}
	return fmt.Sprintf("%v", f.Err)
}
