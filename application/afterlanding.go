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

	ran, err := n.AfterLanding.Run(ctx, c.path.Rig, c.rigDir)
	line := ran.Line()
	if err != nil {
		line = afterLandingLine(command, "could not be run: "+firstLine(err.Error()))
	}
	report.Notes = append(report.Notes, line)
	if err == nil && ran.Succeeded() {
		return
	}

	comment := fmt.Sprintf("mw next on %s landed this story, and the command this host runs in the rig's checkout %s after a landing did not go well, "+
		"so the binary built there may be old. The landing is not undone.\n\n%s", n.Host, c.rigDir, line)
	if err := n.Tracker.CommentOnStory(ctx, c.id, comment); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the after-landing command's failure could not be written on the story: %v", err))
	}
}
