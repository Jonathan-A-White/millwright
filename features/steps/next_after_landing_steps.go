package steps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"

	"github.com/cucumber/godog"
)

// The steps of the command a rig names to run once a landing has moved the
// host's own checkout of it. They hang off nextContext: c.afterCommands is the
// [after_landing] table mwClosesOut hands to the adapter, and the script they
// write logs where it ran and at what commit into a file beside the origin.

// registerNextAfterLandingSteps registers the after-landing steps of
// features/next.feature.
func registerNextAfterLandingSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the rig names a command to run after a landing$`, c.theRigNamesACommand)
	ctx.Given(`^the rig names a command to run after a landing, which prints "([^"]*)" and exits (\d+)$`, c.theRigNamesAFailingCommand)
	ctx.Given(`^the rig names a command to run after a landing, which runs far too long$`, c.theRigNamesASlowCommand)
	ctx.Given(`^another rig, and not this one, names a command to run after a landing$`, c.anotherRigNamesACommand)
	ctx.Given(`^this host runs the follower, mw-view-follow.service$`, c.thisHostRunsTheFollower)
	ctx.Given(`^this host runs the follower, mw-view-follow.service, which fails to restart, saying: (.+)$`, c.theFollowerFailsToRestart)
	ctx.Given(`^after-landing commands are stopped after (\d+) milliseconds$`, c.afterLandingCommandsAreStoppedAfter)
	ctx.Given(`^the rig names a command to run after a landing, which fails the first time on a name-resolution fault, exit 255, and then succeeds$`, c.theRigNamesAFlakyCommand)
	ctx.Given(`^after-landing commands are run again after (\d+) milliseconds$`, c.afterLandingCommandsAreRetriedAfter)
	ctx.Given(`^another deploy of the rig holds its after-landing lock for (\d+) milliseconds$`, c.anotherDeployHoldsTheLock)

	ctx.Then(`^the rig's after-landing command ran once, in the rig checkout, at the commit that landed on "([^"]*)"$`, c.theCommandRanOnce)
	ctx.Then(`^the rig's after-landing command ran twice, in the rig checkout, at the commit that landed on "([^"]*)"$`, c.theCommandRanTwice)
	ctx.Then(`^the rig's after-landing command did not start until the other deploy let go$`, c.theCommandWaitedForTheOtherDeploy)
	ctx.Then(`^mw-view-follow.service was try-restarted once$`, c.theFollowerWasRestartedOnce)
	ctx.Then(`^no user unit was try-restarted$`, c.noUnitWasRestarted)
	ctx.Then(`^the rig's after-landing command did not run$`, c.theCommandDidNotRun)
	ctx.Then(`^the report says: (.+)$`, c.theReportSays)
	ctx.Then(`^the comment on "([^"]*)" says: (.+)$`, c.theCommentSays)
	ctx.Then(`^that mail's body says: (.+)$`, c.thatMailsBodySays)
	ctx.Then(`^that mail's first line is: (.+)$`, c.thatMailsFirstLineIs)
	ctx.Then(`^the story "([^"]*)" is closed exactly as it is without an after-landing command$`, c.theStoryIsClosedAsWithout)
}

// afterCommandToken stands in a step's text for the command line, which holds
// this scenario's temporary directory and so cannot be written in the feature.
const afterCommandToken = "<the command>"

// afterScript writes the script a scenario's command runs: it says where it ran
// and at what commit into after.log, then does what the body says. The command
// is the shell line that runs it, and is what a rig's table would hold.
func (c *nextContext) afterScript(body string) error {
	script := filepath.Join(c.root, "after.sh")
	text := "echo \"$PWD $(git rev-parse HEAD)\" >> \"$(dirname \"$0\")/after.log\"\n" + body + "\n"
	if err := os.WriteFile(script, []byte(text), 0o755); err != nil {
		return err
	}
	c.afterCommands = map[string]string{"millwright": "sh " + script}
	return nil
}

func (c *nextContext) theRigNamesACommand() error { return c.afterScript("exit 0") }

func (c *nextContext) theRigNamesAFailingCommand(prints string, status int) error {
	return c.afterScript(fmt.Sprintf("echo %q\nexit %d", prints, status))
}

func (c *nextContext) theRigNamesASlowCommand() error { return c.afterScript("sleep 30") }

// theRigNamesAFlakyCommand is a command that fails the first time it is run the
// way ssh fails on a passing DNS fault, and succeeds every time after.
func (c *nextContext) theRigNamesAFlakyCommand() error {
	return c.afterScript(`seen="$(dirname "$0")/seen"
if [ ! -e "$seen" ]; then
  touch "$seen"
  echo "ssh: Could not resolve hostname allmymind.org: Temporary failure in name resolution"
  exit 255
fi
exit 0`)
}

func (c *nextContext) afterLandingCommandsAreRetriedAfter(milliseconds int) error {
	c.afterRetry = time.Duration(milliseconds) * time.Millisecond
	return nil
}

// anotherDeployHoldsTheLock takes the rig's after-landing lock now, as another
// deploy would, and gives it back after the time given. Whether the command had
// started by then is read off its log at the moment of letting go.
func (c *nextContext) anotherDeployHoldsTheLock(milliseconds int) error {
	held, err := rig.NewSlots(rig.WithSlotSuffix(rig.AfterLandingSlotSuffix)).Take(context.Background(), c.rig, "another deploy")
	if err != nil {
		return err
	}
	c.deployHolding = held
	log := filepath.Join(c.root, "after.log")
	released := make(chan struct{})
	c.deployReleased = released
	time.AfterFunc(time.Duration(milliseconds)*time.Millisecond, func() {
		_, err := os.Stat(log)
		c.deployRanBeside.Store(err == nil)
		_ = held.Release(context.Background())
		close(released)
	})
	return nil
}

func (c *nextContext) theCommandWaitedForTheOtherDeploy() error {
	select {
	case <-c.deployReleased:
	case <-time.After(5 * time.Second):
		return fmt.Errorf("the other deploy never let go of the lock")
	}
	if c.deployRanBeside.Load() {
		return fmt.Errorf("the after-landing command had already started while another deploy held the lock")
	}
	return nil
}

func (c *nextContext) anotherRigNamesACommand() error {
	if err := c.afterScript("exit 0"); err != nil {
		return err
	}
	c.afterCommands = map[string]string{"elsewhere": c.afterCommands["millwright"]}
	return nil
}

func (c *nextContext) afterLandingCommandsAreStoppedAfter(milliseconds int) error {
	c.afterLimit = time.Duration(milliseconds) * time.Millisecond
	return nil
}

// theCommandRanOnce reads the log the command wrote: one line, the rig checkout
// and the commit the origin's branch holds.
func (c *nextContext) theCommandRanOnce(branch string) error { return c.theCommandRan(1, branch) }

func (c *nextContext) theCommandRanTwice(branch string) error { return c.theCommandRan(2, branch) }

func (c *nextContext) theCommandRan(times int, branch string) error {
	held, err := os.ReadFile(filepath.Join(c.root, "after.log"))
	if err != nil {
		return fmt.Errorf("expected the after-landing command to have run: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(held)), "\n")
	if len(lines) != times {
		return fmt.Errorf("expected the command to run %d times, got %d runs: %q", times, len(lines), lines)
	}
	landed, err := gitSay(c.origin(), "rev-parse", branch)
	if err != nil {
		return err
	}
	// The rig checkout may be reached through a symlinked directory, so what the
	// shell calls its working directory is compared by what it points at.
	wantDir, err := filepath.EvalSymlinks(c.rig)
	if err != nil {
		return err
	}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return fmt.Errorf("expected the command's log line to hold a directory and a commit, got %q", line)
		}
		gotDir, err := filepath.EvalSymlinks(fields[0])
		if err != nil {
			return err
		}
		if gotDir != wantDir || fields[1] != landed {
			return fmt.Errorf("expected the command to run in %s at %s, got %s at %s", wantDir, landed, gotDir, fields[1])
		}
	}
	return nil
}

func (c *nextContext) theCommandDidNotRun() error {
	if held, err := os.ReadFile(filepath.Join(c.root, "after.log")); err == nil {
		return fmt.Errorf("expected the after-landing command not to run, but it did: %q", held)
	}
	return nil
}

// spoken is a step's text with the command line put in where the feature named it.
func (c *nextContext) spoken(text string) string {
	return strings.ReplaceAll(text, afterCommandToken, c.afterCommands["millwright"])
}

func (c *nextContext) theReportSays(line string) error {
	want := c.spoken(line)
	if said := c.printed.String(); !strings.Contains(said, want) {
		return fmt.Errorf("expected the report to say %q, got:\n%s", want, said)
	}
	return nil
}

func (c *nextContext) theCommentSays(id, line string) error {
	want := c.spoken(line)
	for _, comment := range c.tracker.Comments(id) {
		if strings.Contains(comment, want) {
			return nil
		}
	}
	return fmt.Errorf("expected a comment on %s saying %q, got %q", id, want, c.tracker.Comments(id))
}

func (c *nextContext) thatMailsBodySays(line string) error {
	mail, err := c.theOneMail()
	if err != nil {
		return err
	}
	if want := c.spoken(line); !strings.Contains(mail.Body, want) {
		return fmt.Errorf("expected the mail's body to say %q, got:\n%s", want, mail.Body)
	}
	return nil
}

func (c *nextContext) thatMailsFirstLineIs(line string) error {
	mail, err := c.theOneMail()
	if err != nil {
		return err
	}
	first, _, _ := strings.Cut(mail.Body, "\n")
	if want := c.spoken(line); first != want {
		return fmt.Errorf("expected the mail's first line to be %q, got %q", want, first)
	}
	return nil
}

// theStoryIsClosedAsWithout is a landed story closed the way every landed story
// is: closed, with the reason of a landing, recorded landed, and nothing of the
// command in the reason.
func (c *nextContext) theStoryIsClosedAsWithout(id string) error {
	if err := c.theStoryIsClosed(id); err != nil {
		return err
	}
	if got := c.tracker.State(id, application.RunState); got != application.RunLanded {
		return fmt.Errorf("expected %s recorded %s=%s, got %q", id, application.RunState, application.RunLanded, got)
	}
	reason := c.tracker.CloseReason(id)
	if !strings.HasPrefix(reason, "landed") || !strings.Contains(reason, "mw next re-ran the rig's tests: pass") || strings.Contains(reason, "after landing") {
		return fmt.Errorf("expected %s closed with the reason of a plain landing, got %q", id, reason)
	}
	return nil
}

// thisHostRunsTheFollower gives the scenario's close-out a user manager that
// has mw-view-follow.service running and restarts it without error.
func (c *nextContext) thisHostRunsTheFollower() error {
	c.units = &apptest.FakeUnitRestarter{}
	return nil
}

func (c *nextContext) theFollowerFailsToRestart(saying string) error {
	c.units = &apptest.FakeUnitRestarter{Fails: map[string]error{"mw-view-follow.service": errors.New(saying)}}
	return nil
}

func (c *nextContext) theFollowerWasRestartedOnce() error {
	if got := c.units.Asked; len(got) != 1 || got[0] != "mw-view-follow.service" {
		return fmt.Errorf("expected mw-view-follow.service try-restarted once, got %v", got)
	}
	return nil
}

func (c *nextContext) noUnitWasRestarted() error {
	if got := c.units.Asked; len(got) != 0 {
		return fmt.Errorf("expected no user unit try-restarted, got %v", got)
	}
	return nil
}

// unitRestarter is the user manager mw next is given: the scenario's, and for
// one that names no follower an empty one, so that a step can say nothing was
// asked of it.
func (c *nextContext) unitRestarter() application.UnitRestarter {
	if c.units == nil {
		c.units = &apptest.FakeUnitRestarter{}
	}
	return c.units
}
