package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
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

	// Landing and Files are how a leftover worktree or branch from an earlier
	// attempt — one a dead-pane reclaim gave the claim back on, or any other
	// attempt that never had it cleared away — is saved before a fresh cut
	// clears it, the same steps mw retry takes (mw-gq6.107). The vault a
	// leftover's bundle is written under is Boot.Vault, already wired for the
	// boot file. Either left nil means a leftover is never salvaged: Add's own
	// failure decides what happens next, exactly as before mw-gq6.107.
	Landing Landing
	Files   VaultFiles

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

	// Host is which of the factory's hosts this is, Cap is how many sessions
	// may be running here at once, and Rigs is where each rig is checked out.
	Host string
	Cap  int
	Rigs map[string]string

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
	// SalvagedBundle is where a worktree or branch left over from an earlier
	// attempt was bundled before this attempt's fresh cut cleared it away
	// (mw-gq6.107), by path from the vault's root. Empty when nothing was left
	// over, or when the leftover had no commits ahead of its target to bundle.
	SalvagedBundle string
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

// DispatchReport is what one dispatch did.
type DispatchReport struct {
	Host string
	Cap  int
	// Running is how many sessions this host already had in flight.
	Running int
	// Reclaimed is every claim this dispatch took back from a dead pane and an
	// expired lease before it read what is ready.
	Reclaimed []Reclaimed
	Started   []Started
	Passed    []Passed
	Failed    []Failed
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
			return report, fmt.Errorf("dispatching on %s: the hosts could not be brought level, so nothing was claimed: %w", d.Host, err)
		}
		ClearSyncHalt(ctx, d.SyncHalts)
		report.Sync, report.Synced = synced, true
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
			reclaimed, err := d.reclaimDeadPane(ctx, detail, &report)
			if err != nil {
				return report, fmt.Errorf("dispatching on %s: %w", d.Host, err)
			}
			if reclaimed {
				// Given back below, so it is read fresh among what is ready rather
				// than counted here: a claim this dispatch just returned is not one
				// still holding a session.
				continue
			}
		}
		report.Running++
	}
	free := d.Cap - report.Running

	ready, err := d.Tracker.ReadyForHost(ctx, d.Host)
	if err != nil {
		return report, fmt.Errorf("dispatching on %s: reading what is ready here: %w", d.Host, err)
	}
	ready = inStartOrder(ready)

	// The formulas installed are read once, and only if a story names one. A
	// dry run reads them too, so that it reports the same refusal a real run
	// would, without writing anything.
	var formulas map[string]bool
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
		if path.Host != d.Host {
			report.Passed = append(report.Passed, Passed{StoryID: id, Why: "it is worked on " + path.Host})
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
			report.Passed = append(report.Passed, Passed{StoryID: id, Why: fmt.Sprintf(
				"%s has taken %d of the %d sessions it may run at once", d.Host,
				report.Running+len(report.Started)+len(report.Failed), d.Cap)})
			continue
		}

		started, released, err := d.start(ctx, detail, path, rigDir, formulas)
		if started.StoryID != "" {
			report.Started = append(report.Started, started)
		}
		if err != nil {
			var refusal *leftoverRefusal
			if errors.As(err, &refusal) {
				// Not a failed run: only this story is refused, its own leftover
				// is somebody's to settle with mw retry, and the stories after it
				// are still worked (mw-gq6.107).
				report.Passed = append(report.Passed, Passed{StoryID: id, Why: refusal.Error()})
			} else {
				report.Failed = append(report.Failed, Failed{StoryID: id, Err: err, Released: released})
			}
		}
	}

	d.print(report.String())
	if len(report.Failed) > 0 {
		return report, fmt.Errorf("dispatching on %s: %s", d.Host, report.failures())
	}
	return report, nil
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

	// recorded is whether the story names the molecule this dispatch poured, and
	// so whether the next dispatch will find it.
	var recorded bool

	// undo gives back everything this dispatch took, and says what it could not.
	undo := func(what string, why error, worktree bool) (Started, bool, error) {
		failed := fmt.Errorf("%s of %s: %w", what, id, why)
		if worktree {
			if err := d.Worktrees.Remove(ctx, rigDir, started.Worktree, started.Branch); err != nil {
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
		salvaged, err := salvageBranch(ctx, d.Worktrees, d.Landing, d.Files, d.Boot.Vault, rigDir, id, started.Worktree, started.Branch, started.Start, leftoverAttempt)
		if err != nil {
			return d.refuseLeftover(ctx, id, err)
		}
		started.SalvagedBundle = salvaged.BundlePath
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
		molecule, err := d.Tracker.PourFormula(ctx, path.Formula, id, detail.Story.Title)
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
	if err := d.Tracker.SetStoryMetadata(ctx, id, attemptFields(detail, started.Attempt)); err != nil {
		unrecorded = append(unrecorded, fmt.Errorf("the attempt could not be recorded as %s=%d: %w", AttemptsField, started.Attempt, err))
	}
	where := fmt.Sprintf("dispatched by mw on %s: session %s in %s on %s", d.Host, spec.Name, started.Worktree, started.Branch)
	if started.Attempt > 1 {
		where += fmt.Sprintf(", attempt %d", started.Attempt)
	}
	if started.SalvagedBundle != "" {
		where += fmt.Sprintf(", after saving what an earlier attempt left as %s", started.SalvagedBundle)
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
// branch a fresh cut is about to take — the shape a dead-pane reclaim leaves
// behind, its session gone but the git it made never cleared away
// (mw-gq6.107). Salvaging one needs somewhere to bundle it and somewhere to
// push that bundle; without Landing and Files wired in, nothing here is ever
// called a leftover, and Add's own failure decides what happens next, exactly
// as before mw-gq6.107.
//
// A live session already running under the story's name is never a leftover,
// whatever Exists says: that is the race two dispatchers can lose to each
// other in the same instant (mw-gq6.96), and clearing away a branch a session
// is working right now would destroy it. Add's own failure and the namesake
// check release makes right before giving a claim back both still cover that
// race exactly as they did before this existed.
func (d Dispatch) leftoverAt(ctx context.Context, rigDir string, started Started) (bool, error) {
	if d.Landing == nil || d.Files == nil || d.Boot.Vault == nil {
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

// leftoverRefusal marks a story refused because a worktree or branch left by
// an earlier attempt could not be saved before a fresh cut (mw-gq6.107): the
// claim was given back and why is on the story, the same as any other
// refusal, but it is not counted as a failed run — the leftover is
// somebody's to settle with mw retry, and the other stories ready this tick
// are still worked.
type leftoverRefusal struct{ err error }

func (e *leftoverRefusal) Error() string { return e.err.Error() }
func (e *leftoverRefusal) Unwrap() error { return e.err }

// refuseLeftover gives the claim back and notes on the story that a leftover
// from an earlier attempt could not be saved, so this dispatch tick moves on
// to whatever else is ready rather than failing the whole run over it. The
// leftover itself is left exactly as it was, the same as a failed mw retry
// leaves it, for a person to settle by hand.
func (d Dispatch) refuseLeftover(ctx context.Context, id string, err error) (Started, bool, error) {
	why := fmt.Errorf("cutting the worktree of %s: a worktree or branch left by an earlier attempt could not be saved before a fresh cut: %w", id, err)
	released, err := d.release(ctx, id, why)
	return Started{}, released, &leftoverRefusal{err: err}
}

// reclaimDeadPane looks at one story this host already has claimed, and gives
// the claim back when its tmux window is still there but its pane has died
// and the tracker's own lease on the claim has expired: together the
// strongest sign that the session ended without mw next ever hearing about it
// (the Laptop's DNS outage of 2026-09-24, mw-gq6.106). A live session, a
// window gone outright, or a lease not yet expired are all left exactly as
// they were — counted running, the same as before this existed — because
// either sign alone is not enough to act on without a person's word.
//
// Nothing is cut or removed here: only the window, whose pane is already
// dead, is closed, and the claim given back. What the story's worktree and
// branch still hold from the attempt that died is left for the next attempt
// to run into, or for a person to settle by hand with mw retry.
func (d Dispatch) reclaimDeadPane(ctx context.Context, detail StoryDetail, report *DispatchReport) (bool, error) {
	id := detail.Story.ID
	name := SessionName(id)
	status, err := d.Runner.Status(ctx, name)
	if err != nil {
		// A runner that cannot say cannot be trusted to say the pane is dead
		// either: leave the claim counted as running, as if this check were
		// never made.
		return false, nil
	}
	if status.State != StateExited && status.State != StateExitUnknown {
		return false, nil
	}
	if detail.LeaseExpires.IsZero() || !d.now().After(detail.LeaseExpires) {
		return false, nil
	}

	why := fmt.Sprintf(
		"mw dispatch on %s found %s claimed here with a dead pane (%s is %s) and its lease expired at %s with no heartbeat since: "+
			"the window was closed and the claim given back so it is dispatched again as a fresh attempt.",
		d.Host, id, name, status.State, detail.LeaseExpires.UTC().Format(time.RFC3339))

	if err := d.Runner.Close(ctx, name); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf(
			"%s: its session %s has a dead pane and an expired lease, but the window could not be closed, so the claim was left alone: %v",
			id, name, err))
		return false, nil
	}
	if err := d.Tracker.ReleaseClaim(ctx, id); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf(
			"%s: its session %s had a dead pane and an expired lease and its window was closed, but the claim could not be given back: %v",
			id, name, err))
		return false, nil
	}
	if err := d.Tracker.CommentOnStory(ctx, id, why); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: the dead-pane reclaim could not be commented on: %v", id, err))
	}
	report.Reclaimed = append(report.Reclaimed, Reclaimed{StoryID: id, Session: name, LeaseExpired: detail.LeaseExpires})
	return true, nil
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
	if status, err := d.Runner.Status(ctx, SessionName(id)); err == nil && status.Running() {
		said := fmt.Sprintf("mw dispatch on %s could not start this story, but the session %s of %s is running here now, so the claim stays with it: %v", d.Host, status.Name, id, why)
		if err := d.Tracker.CommentOnStory(ctx, id, said); err != nil {
			return false, fmt.Errorf("%w (the session %s of %s is running here now, so the claim was left alone, but that could not be written on the story: %v)", why, status.Name, id, err)
		}
		return false, fmt.Errorf("%w (the session %s of %s is running here now, so the claim was left alone rather than given back)", why, status.Name, id)
	}
	if err := d.Tracker.ReleaseClaim(ctx, id); err != nil {
		return false, fmt.Errorf("%w (and the claim could not be given back either: %v — %s is claimed by a session that is not running, and needs releasing by hand)", why, err, id)
	}
	said := fmt.Sprintf("mw dispatch on %s could not start this story, so the claim was given back and nothing is running: %v", d.Host, why)
	if err := d.Tracker.CommentOnStory(ctx, id, said); err != nil {
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
	for _, reclaim := range r.Reclaimed {
		fmt.Fprintf(&b, "  reclaimed %s · %s · dead pane, lease expired %s\n", reclaim.StoryID, reclaim.Session,
			reclaim.LeaseExpired.UTC().Format(time.RFC3339))
	}

	verb := "started"
	if r.DryRun {
		verb = "would start"
	}
	for _, started := range r.Started {
		fmt.Fprintf(&b, "  %s %s · %s · %s on %s cut from %s", verb, started.StoryID, started.Session,
			started.Worktree, started.Branch, started.Start)
		if started.Attempt > 1 {
			fmt.Fprintf(&b, " · attempt %d", started.Attempt)
		}
		if started.SalvagedBundle != "" {
			fmt.Fprintf(&b, " · a leftover from an earlier attempt was saved as %s", started.SalvagedBundle)
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
