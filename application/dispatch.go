package application

import (
	"context"
	"fmt"
	"io"
	"strings"

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
// start it twice.
type Dispatch struct {
	Tracker   WorkTracker
	Worktrees Worktrees
	Runner    Runner
	Boot      SeatBoot

	// Sync is run before anything is asked of the tracker, so that this host
	// sees the other host's claims before it makes its own. A nil Sync skips
	// it, which is what a dry run does.
	Sync HostSync

	// Host is which of the factory's hosts this is, Cap is how many sessions
	// may be running here at once, and Rigs is where each rig is checked out.
	Host string
	Cap  int
	Rigs map[string]string

	// Remote is the remote a story's branch is cut from. Empty is DefaultRemote,
	// and whatever it says must be the remote the Worktrees adapter fetches.
	Remote string

	// DryRun prints what would be started and writes nothing at all: nothing is
	// synced, nothing claimed, no worktree made, no formula poured, no session
	// started.
	DryRun bool

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

// DispatchReport is what one dispatch did.
type DispatchReport struct {
	Host string
	Cap  int
	// Running is how many sessions this host already had in flight.
	Running int
	Started []Started
	Passed  []Passed
	Failed  []Failed
	DryRun  bool
	// Sync is what the sync before the dispatch moved, when there was one.
	Sync   SyncReport
	Synced bool
}

// Run dispatches what this host can take and reports what it did.
//
// The order is the point. This host is brought level with the other one before
// anything is asked of the tracker, because the other host's claims and the
// Mayor's newest stories are both in what a sync brings in, and a dispatcher
// working from a stale view claims work that is not its own. Then what is
// already in flight here, because that is what the cap counts. Then what is
// ready. Only then is anything claimed.
func (d Dispatch) Run(ctx context.Context) (DispatchReport, error) {
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
		synced, err := d.Sync.Run(ctx)
		if err != nil {
			return report, fmt.Errorf("dispatching on %s: the hosts could not be brought level, so nothing was claimed: %w", d.Host, err)
		}
		report.Sync, report.Synced = synced, true
	}

	running, err := d.Tracker.RunningStories(ctx, d.Host)
	if err != nil {
		return report, fmt.Errorf("dispatching on %s: reading what is already running here: %w", d.Host, err)
	}
	report.Running = len(running)
	free := d.Cap - len(running)

	ready, err := d.Tracker.ReadyForHost(ctx, d.Host)
	if err != nil {
		return report, fmt.Errorf("dispatching on %s: reading what is ready here: %w", d.Host, err)
	}

	// The formulas installed are read once, and only if a story names one.
	var formulas map[string]bool
	for _, detail := range ready {
		id := detail.Story.ID
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
		rigDir, checkedOut := d.Rigs[path.Rig]
		if !checkedOut {
			report.Passed = append(report.Passed, Passed{StoryID: id, Why: fmt.Sprintf(
				"the rig %s is not checked out on %s: add it under [rigs] in the config file", path.Rig, d.Host)})
			continue
		}

		if !d.DryRun && path.Formula != "" && formulas == nil {
			if formulas, err = d.installedFormulas(ctx); err != nil {
				return report, fmt.Errorf("dispatching on %s: %w", d.Host, err)
			}
		}

		started, released, err := d.start(ctx, detail, path, rigDir, formulas)
		if started.StoryID != "" {
			report.Started = append(report.Started, started)
		}
		if err != nil {
			report.Failed = append(report.Failed, Failed{StoryID: id, Err: err, Released: released})
		}
	}

	d.print(report.String())
	if len(report.Failed) > 0 {
		return report, fmt.Errorf("dispatching on %s: %s", d.Host, report.failures())
	}
	return report, nil
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

	if err := d.Tracker.ClaimStory(ctx, id); err != nil {
		return Started{}, false, fmt.Errorf("claiming %s: %w", id, err)
	}

	// undo gives back everything this dispatch took, and says what it could not.
	undo := func(what string, why error, worktree bool) (Started, bool, error) {
		failed := fmt.Errorf("%s of %s: %w", what, id, why)
		if worktree {
			if err := d.Worktrees.Remove(ctx, rigDir, started.Worktree, started.Branch); err != nil {
				failed = fmt.Errorf("%w (and the worktree %s is still there: %v)", failed, started.Worktree, err)
			}
		}
		// Beads are never deleted here, so a formula poured before the failure
		// stays where it is, and is said so rather than quietly left.
		if started.Molecule.Poured() {
			failed = fmt.Errorf("%w (the formula was already poured as %s, which is left behind; the next dispatch pours another)",
				failed, started.Molecule.RootID)
		}
		released, err := d.release(ctx, id, failed)
		return Started{}, released, err
	}

	// The target branch as the rig's origin has it now, not as this host saw it
	// last: the other host may have pushed to it since.
	if err := d.Worktrees.Fetch(ctx, rigDir); err != nil {
		return undo("fetching the rig", err, false)
	}
	if err := d.Worktrees.Add(ctx, rigDir, started.Worktree, started.Branch, started.Start); err != nil {
		return undo("cutting the worktree", err, false)
	}

	if path.Formula != "" && formulas[path.Formula] {
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
	}

	spec, err := d.Boot.Boot(ctx, detail, started.Worktree)
	if err != nil {
		return undo("assembling the session", err, true)
	}
	if err := d.Runner.Start(ctx, spec); err != nil {
		return undo("starting the session", err, true)
	}
	started.Session = spec.Name

	// From here the session is alive and spending fuel. Nothing below is worth
	// undoing it for.
	where := fmt.Sprintf("dispatched by mw on %s: session %s in %s on %s", d.Host, spec.Name, started.Worktree, started.Branch)
	if molecule := started.Molecule; molecule.Poured() {
		where += fmt.Sprintf(", working %s (%d steps)", molecule.RootID, len(molecule.Steps))
	}
	if err := d.Tracker.SetStoryState(ctx, id, RunState, RunRunning, where); err != nil {
		return started, false, fmt.Errorf("the session %s is running in %s, but %s could not be recorded as %s=%s: %w",
			spec.Name, started.Worktree, id, RunState, RunRunning, err)
	}
	return started, false, nil
}

// release gives a claim back and writes on the story why it was given back. It
// reports whether the claim really did go back, and the failure a person should
// read — the original one, with whatever went wrong releasing it after it.
func (d Dispatch) release(ctx context.Context, id string, why error) (bool, error) {
	if err := d.Tracker.ReleaseClaim(ctx, id); err != nil {
		return false, fmt.Errorf("%w (and the claim could not be given back either: %v — %s is claimed by a session that is not running, and needs releasing by hand)", why, err, id)
	}
	said := fmt.Sprintf("mw dispatch on %s could not start this story, so the claim was given back and nothing is running: %v", d.Host, why)
	if err := d.Tracker.CommentOnStory(ctx, id, said); err != nil {
		return true, fmt.Errorf("%w (the claim was given back, but the failure could not be written on the story: %v)", why, err)
	}
	return true, why
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

	verb := "started"
	if r.DryRun {
		verb = "would start"
	}
	for _, started := range r.Started {
		fmt.Fprintf(&b, "  %s %s · %s · %s on %s cut from %s", verb, started.StoryID, started.Session,
			started.Worktree, started.Branch, started.Start)
		if molecule := started.Molecule; molecule.Poured() {
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
