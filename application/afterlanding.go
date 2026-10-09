package application

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// AfterLandingLimit is how long a rig's after-landing command may run before it
// is stopped and reported as a failure. A build is minutes on the smaller host,
// and a close-out is a session nobody is watching: a command that hangs is one
// that would hold the baton for good.
const AfterLandingLimit = 5 * time.Minute

// AfterLandingStoppedLine is the first line of the mail to the Mayor when the
// rig's after-landing command outlived its limit and was stopped. A command that
// ships a site was cut off between its first file and its last: the report's
// quiet note is not enough for what a person has to go and look at.
const AfterLandingStoppedLine = "after landing STOPPED at the limit: the site may be half-deployed"

// AfterLandingRetryWait is how long a rig's after-landing command is left before
// it is run once more, when it failed on a fault of the network that passes
// (DNS that did not answer, an ssh that could not connect). A resolver that
// failed a deploy at 05:54 answered again three minutes later: half a minute is
// long enough for most of such faults and short enough that the landing, which
// holds the baton while it waits, is not held up.
const AfterLandingRetryWait = 30 * time.Second

// UnitRestartFailedLine is the first line of the mail to the Mayor when a
// long-running mw user unit could not be restarted on the new build: the
// follower keeps publishing the Governor's view with the old binary until
// somebody restarts it by hand.
const UnitRestartFailedLine = "a user unit could not be restarted on the new build: the old mw is still running there"

// FactoryUnits are the long-running user units that run the factory's own mw,
// which a build of the factory rig leaves on the old binary until they are
// restarted. mw-postern-mirror.service is a oneshot a timer starts, so it picks
// up the new binary by itself.
var FactoryUnits = []string{"mw-view-follow.service"}

// UnitRestarter is the port a host's user manager is asked to restart a running
// unit through.
type UnitRestarter interface {
	// TryRestart restarts unit if it is running, as `systemctl --user
	// try-restart` does, and says whether it did: a unit that is stopped or not
	// installed here is left as it is, with no error. An error is a unit that was
	// running and could not be restarted.
	TryRestart(ctx context.Context, unit string) (restarted bool, err error)
}

// RestartFactoryUnits restarts the long-running mw units of this host once a
// build of rig has succeeded, so that they run the binary just built, and says
// what it did, as notes: one for each unit restarted, and one for each that could
// not be, which are also returned in failed. Nothing is done for a rig that is
// not the factory rig, whose build changes no binary these units run, nor for a
// nil units.
func RestartFactoryUnits(ctx context.Context, units UnitRestarter, rig string) (notes, failed []string) {
	if units == nil || rig != FactoryRig {
		return nil, nil
	}
	for _, unit := range FactoryUnits {
		restarted, err := units.TryRestart(ctx, unit)
		switch {
		case err != nil:
			note := fmt.Sprintf("%s could not be restarted on the new build and still runs the old mw: %s", unit, firstLine(err.Error()))
			notes, failed = append(notes, note), append(failed, note)
		case restarted:
			notes = append(notes, "restarted "+unit+" on the new build")
		}
	}
	return notes, failed
}

// afterTail is how much of a failed command's output the one line about it
// quotes: the last lines, which are where a build says what broke, and no more
// than afterTailRunes of them, because the line goes into a ledger, a comment and
// a mail.
const (
	afterTailLines = 3
	afterTailRunes = 240
)

// AfterLanding is the port a rig's own command is run through once a landing has
// moved this host's checkout of it. What the command is belongs to the rig and
// to the host: `make build` is what a host that runs its own mw from the rig's
// bin/ says, and a host that runs nothing from a rig says nothing.
type AfterLanding interface {
	// Command is the command line the host names for a rig, empty when it names
	// none — and then nothing is run.
	Command(rig string) string

	// Run runs the rig's command in dir, the rig's checkout, under a time limit.
	// A command that exits non-zero, cannot be found or outlives the limit is a
	// Ran that says so, not an error: an error is a command that could not be
	// started at all.
	Run(ctx context.Context, rig, dir string) (Ran, error)
}

// Ran is one run of a rig's after-landing command: the command line, the status
// it exited with, and what it printed. TimedOut is the limit it outlived and
// was stopped at, zero when it did not; Status is then -1, because it never
// exited of itself.
type Ran struct {
	Command  string
	Status   int
	Output   string
	TimedOut time.Duration
}

// Succeeded says the command ran to the end and exited with status zero.
func (r Ran) Succeeded() bool { return r.Status == 0 && r.TimedOut == 0 }

// Line is what the report, the story's comment and the mail to the Mayor say
// about the run, in one line: the command, and either that it went well or how it
// did not, with the tail of its output.
func (r Ran) Line() string {
	if r.Succeeded() {
		return afterLandingLine(r.Command, "ok")
	}
	var how string
	switch {
	case r.TimedOut > 0:
		how = fmt.Sprintf("stopped after %s, still running", r.TimedOut)
	case r.Status == 127:
		how = "exit status 127, the shell found no such program"
	case r.Status == 126:
		how = "exit status 126, the shell could not run it"
	default:
		how = fmt.Sprintf("exit status %d", r.Status)
	}
	if tail := tailLine(r.Output); tail != "" {
		how += ": " + tail
	}
	return afterLandingLine(r.Command, how)
}

// networkFaultMarks are what a command's output says when the network, not the
// command, failed — in the words of ssh, git, curl and the resolver, lower-cased.
var networkFaultMarks = []string{
	"could not resolve host",
	"temporary failure in name resolution",
	"name or service not known",
	"connection refused",
	"connection timed out",
	"connection reset",
	"network is unreachable",
	"no route to host",
}

// networkFaultLines is how many of the last lines of a failed command's output
// are read for those words: where a deploy says what broke, and not the whole of
// a build's chatter.
const networkFaultLines = 20

// NetworkFault says the run failed on a fault of the network that may pass, and
// so is worth one more try: ssh's own exit status 255 (it could not connect, or
// was cut off), or a resolver or connection error in the tail of what it printed.
// A command that succeeded or was stopped at its limit is not one: the second
// would be run with nothing to say it would go any differently.
func (r Ran) NetworkFault() bool {
	if r.Succeeded() || r.TimedOut > 0 {
		return false
	}
	if r.Status == 255 {
		return true
	}
	tail := strings.ToLower(RecentLines(r.Output, networkFaultLines))
	for _, mark := range networkFaultMarks {
		if strings.Contains(tail, mark) {
			return true
		}
	}
	return false
}

// runAfterLanding runs the rig's after-landing command in dir and, when it
// failed on a fault of the network that may pass, once more after pause (zero is
// AfterLandingRetryWait), using wait to pause. It says in retried, as a line,
// that it ran the command again, and why; retried is empty when it did not. The
// Ran is the last run's. An error is a command that could not be started, or a
// pause cut short by ctx.
func runAfterLanding(ctx context.Context, port AfterLanding, rig, dir string, pause time.Duration, wait func(context.Context, time.Duration) error) (ran Ran, retried string, err error) {
	ran, err = port.Run(ctx, rig, dir)
	if err != nil || !ran.NetworkFault() {
		return ran, "", err
	}
	if pause <= 0 {
		pause = AfterLandingRetryWait
	}
	first := fmt.Sprintf("exit status %d", ran.Status)
	retried = afterLandingLine(ran.Command, fmt.Sprintf("retried once after %s, because the first run failed on a network fault that may pass (%s)", pause, first))
	if err := wait(ctx, pause); err != nil {
		return ran, retried, err
	}
	ran, err = port.Run(ctx, rig, dir)
	return ran, retried, err
}

// takeAfterLandingSlot takes the rig's after-landing lock, which is what keeps
// two runs of the rig's command — a landing's own and one a person asks for —
// from deploying at once. A nil slot takes nothing, and the release it returns
// does nothing. The release is safe to call twice.
func takeAfterLandingSlot(ctx context.Context, slot MergeSlot, rigDir, holder string) (release func(context.Context) error, err error) {
	if slot == nil {
		return func(context.Context) error { return nil }, nil
	}
	holding, err := slot.Take(ctx, rigDir, holder)
	if err != nil {
		return nil, fmt.Errorf("the after-landing lock of %s could not be taken: %w", rigDir, err)
	}
	return holding.Release, nil
}

// afterLandingLine is the shape every line about the command has, so that the
// three places it is said read alike and a person can search for it.
func afterLandingLine(command, said string) string {
	return "after landing: " + command + ": " + said
}

// tailLine is the last lines of a command's output as a single line, the earlier
// of them cut off when there is more than fits.
func tailLine(output string) string {
	said := strings.Join(strings.Fields(strings.Join(strings.Split(RecentLines(output, afterTailLines), "\n"), " / ")), " ")
	if runes := []rune(said); len(runes) > afterTailRunes {
		said = "…" + string(runes[len(runes)-afterTailRunes:])
	}
	return said
}

// afterLanding runs the rig's own after-landing command once the landing has
// moved this host's checkout of the rig, and says what became of it. It changes
// nothing about the landing: the work is on the target branch and the story is
// recorded as landed before this runs, and every way it can go wrong — a command
// that fails, a program that is not there, one that outlives its limit — is a
// note, and on the story a comment, so that the Mayor knows the host's binary may
// be old.
//
// Nothing runs for a rig the host names no command for, or for a checkout the
// landing did not move: what a command built there would be the old commit.
// A rig the host names no command for still gets a line in the report, so
// that a Mayor reading it can tell "this host names no command for this rig"
// from "the command was not configured when mw next started".
func (n Next) afterLanding(ctx context.Context, c *closeOut, report *NextReport) {
	if n.AfterLanding == nil {
		return
	}
	command := n.AfterLanding.Command(c.path.Rig)
	if command == "" {
		report.Notes = append(report.Notes, afterLandingLine("none", "this host names no command for "+c.path.Rig))
		return
	}
	if !report.Rig.Moved {
		why := "the rig checkout was not brought up to " + c.target
		if report.Rig.Left != "" {
			why = "the rig checkout was left as it was: " + report.Rig.Left
		}
		report.Notes = append(report.Notes, afterLandingLine(command, "not run, "+why))
		return
	}

	ran, retried, err := n.deploy(ctx, c, report)
	line := ran.Line()
	if err != nil {
		line = afterLandingLine(command, "could not be run: "+firstLine(err.Error()))
	}
	if retried != "" {
		report.Notes = append(report.Notes, retried)
	}
	report.Notes = append(report.Notes, line)
	if err == nil && ran.Succeeded() {
		n.restartUnits(ctx, c, report)
		return
	}
	if err == nil && ran.TimedOut > 0 {
		report.AfterLandingStopped = ran.TimedOut
		line = AfterLandingStoppedLine + "\n" + line
	}

	comment := fmt.Sprintf("mw next on %s landed this story, and the command this host runs in the rig's checkout %s after a landing did not go well, "+
		"so the binary built there may be old. The landing is not undone.\n\n%s", n.Host, c.rigDir, line)
	if err := n.Tracker.CommentOnStory(ctx, c.id, comment); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the after-landing command's failure could not be written on the story: %v", err))
	}
}

// deploy runs the rig's command under the rig's after-landing lock, so that it
// never runs beside another run of it, and again once if it failed on a passing
// network fault. retried is the line saying so, empty when it did not.
func (n Next) deploy(ctx context.Context, c *closeOut, report *NextReport) (ran Ran, retried string, err error) {
	release, err := takeAfterLandingSlot(ctx, n.DeploySlot, c.rigDir, Holders(n.Seat, n.Host, c.id))
	if err != nil {
		return Ran{}, "", err
	}
	ran, retried, err = runAfterLanding(ctx, n.AfterLanding, c.path.Rig, c.rigDir, n.AfterRetryWait, n.wait)
	if relErr := release(ctx); relErr != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the after-landing lock of %s could not be given back: %v", c.path.Rig, relErr))
	}
	return ran, retried, err
}

// restartUnits restarts this host's long-running mw units on the build that just
// succeeded and says so in the report. A restart that fails is said again on the
// story and, first, in the mail to the Mayor: the landing is not undone by it.
func (n Next) restartUnits(ctx context.Context, c *closeOut, report *NextReport) {
	notes, failed := RestartFactoryUnits(ctx, n.Units, c.path.Rig)
	report.Notes = append(report.Notes, notes...)
	if len(failed) == 0 {
		return
	}
	report.UnitRestartFailed = failed
	comment := fmt.Sprintf("mw next on %s landed this story and built it, but %s The landing is not undone.\n\n%s",
		n.Host, UnitRestartFailedLine+".", strings.Join(failed, "\n"))
	if err := n.Tracker.CommentOnStory(ctx, c.id, comment); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the unit restart's failure could not be written on the story: %v", err))
	}
}

// readBackend looks, before the story is merged, at whether its own commits
// changed what the rig's backend is built from: afterwards the commits are on the
// target branch and no longer the story's alone. A look that fails counts as a
// change, because a backend half wrongly staged costs one card, and one left
// undeployed is what this exists to prevent.
func (n Next) readBackend(ctx context.Context, c *closeOut, report *NextReport) {
	if !n.Backend.Wants(c.path.Rig) {
		return
	}
	changed, err := n.Backend.Touches(ctx, c.path.Rig, StartPoint(n.remote(), c.target), c.branch)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("backend: whether %s changed the backend of %s could not be read, so it is staged to be safe: %s", c.id, c.path.Rig, firstLine(err.Error())))
		changed = true
	}
	c.backendChanged = changed
}

// stageBackend, once the story is landed, builds the backend the landing changed
// on the home and writes its swap as a hands step, or leaves a note for the
// home's next tick when this host is not home. It restarts nothing: the swap
// runs when the Governor approves the step.
func (n Next) stageBackend(ctx context.Context, c *closeOut, report *NextReport) {
	if !c.backendChanged {
		return
	}
	report.Notes = append(report.Notes, n.Backend.Landed(ctx, BackendLanding{
		Rig:    c.path.Rig,
		Story:  c.id,
		Title:  c.detail.Story.Title,
		Epic:   c.detail.EpicID,
		Commit: report.How.Commit,
	})...)
}
