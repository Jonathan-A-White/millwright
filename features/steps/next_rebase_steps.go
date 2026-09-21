package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"

	"github.com/cucumber/godog"
)

// The steps of a landing that conflicts and is sent back to a fresh Builder to
// rebase. They hang off nextContext: the other host's change is made in c.seed,
// the rebase a sent-back session would make is made for real in the story's
// worktree, and the sessions are the fake runner's.

// registerNextRebaseSteps registers the send-back steps of features/next.feature.
func registerNextRebaseSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the other host landed a change to the same file as "([^"]*)" on "([^"]*)"$`, c.theOtherHostChangedTheSameFile)
	ctx.Given(`^mw next is running in the session of "([^"]*)"$`, c.mwNextIsRunningInTheSession)
	ctx.Given(`^the session of "([^"]*)" was the first that dispatch started for it$`, c.theSessionWasTheFirstAttempt)
	ctx.Given(`^the session sent back rebases the branch of "([^"]*)" onto "([^"]*)" and commits the resolution$`, c.theSentBackSessionRebases)
	ctx.Given(`^the session sent back for "([^"]*)" ends without rebasing$`, c.theSentBackSessionEndsWithoutRebasing)

	ctx.Then(`^a fresh session is running for "([^"]*)" in its worktree, told to rebase onto "([^"]*)"$`, c.aSessionIsRunningToRebase)
	ctx.Then(`^the story "([^"]*)" is recorded as sent back to rebase$`, c.theStoryIsRecordedAsSentBack)
	ctx.Then(`^the report says "([^"]*)" was sent back to rebase$`, c.theReportSaysItWasSentBack)
	ctx.Then(`^(\d+) sessions were ever started for "([^"]*)"$`, c.sessionsWereEverStarted)
	ctx.Then(`^the attempts counted on "([^"]*)" come to (\d+)$`, c.theAttemptsCountedComeTo)
	ctx.Then(`^the file of "([^"]*)" on "([^"]*)" at the rig's origin keeps the other host's line too$`, c.theOtherHostsLineIsKept)
}

// otherHostsLine is what the other host wrote into the story's own file, so that
// the story's branch and the target branch both add it and cannot be merged.
const otherHostsLine = "the other host's word on this\n"

func (c *nextContext) theOtherHostChangedTheSameFile(id, branch string) error {
	if err := os.WriteFile(filepath.Join(c.seed, id+".md"), []byte(otherHostsLine), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"add", "-A"}, {"commit", "-qm", "The other host writes the same file"}, {"push", "-q", "origin", branch},
	} {
		if err := gitRun(c.seed, "git", args...); err != nil {
			return err
		}
	}
	return nil
}

// mwNextIsRunningInTheSession is the session the story was worked in, as it is
// while mw next runs: mw next is its command, chained on after the harness, so
// the session is still running.
func (c *nextContext) mwNextIsRunningInTheSession(id string) error {
	return c.runner.Start(context.Background(), application.SessionSpec{
		Name: application.SessionName(id), Command: []string{"sh", "-c", "claude; mw next " + id},
	})
}

// theSentBackSessionRebases does what the session sent back is told to: rebase
// the branch onto the target as the rig last fetched it, keep both sides of the
// file, commit, and report a success of its own before mw next runs again.
func (c *nextContext) theSentBackSessionRebases(id, onto string) error {
	dir := application.WorktreeDir(c.rig, id)
	// The rebase stops on the conflict, which is the point: it is resolved by hand.
	_ = gitRun(dir, "git", "rebase", onto)
	both := otherHostsLine + "the work of " + id + "\n"
	if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(both), 0o644); err != nil {
		return err
	}
	if err := gitRun(dir, "git", "add", id+".md"); err != nil {
		return err
	}
	if err := gitRun(dir, "git", "-c", "core.editor=true", "rebase", "--continue"); err != nil {
		return err
	}
	return c.theSentBackSessionSucceeds(id)
}

func (c *nextContext) theSentBackSessionEndsWithoutRebasing(id string) error {
	return c.theSentBackSessionSucceeds(id)
}

// theSentBackSessionSucceeds is the result the second session leaves, under a
// session id of its own, over the first session's.
func (c *nextContext) theSentBackSessionSucceeds(id string) error {
	return c.putResult(id, `{"type":"result","subtype":"success","is_error":false,"num_turns":4,`+
		`"duration_ms":300000,"session_id":"s-rebase","total_cost_usd":0.5,`+
		`"usage":{"input_tokens":50,"output_tokens":900,"cache_read_input_tokens":20000,"cache_creation_input_tokens":1000}}`)
}

func (c *nextContext) aSessionIsRunningToRebase(id, onto string) error {
	name := application.SessionName(id)
	status, err := c.runner.Status(context.Background(), name)
	if err != nil {
		return err
	}
	if !status.Running() {
		return fmt.Errorf("expected a session %s running, got %+v (the close-out said: %v)", name, status, c.err)
	}
	spec, _ := c.runner.Spec(name)
	if want := application.WorktreeDir(c.rig, id); spec.Dir != want {
		return fmt.Errorf("expected the session %s to run in %s, got %q", name, want, spec.Dir)
	}
	said := strings.Join(spec.Command, " ")
	for _, want := range []string{"rebase", onto, "mw next"} {
		if !strings.Contains(said, want) {
			return fmt.Errorf("expected the session %s to be told %q, got: %s", name, want, said)
		}
	}
	return nil
}

func (c *nextContext) theStoryIsRecordedAsSentBack(id string) error {
	if got := c.tracker.State(id, application.RebaseState); got != application.RebaseSentBack {
		return fmt.Errorf("expected %s to be recorded %s=%s, got %q", id, application.RebaseState, application.RebaseSentBack, got)
	}
	if got := c.tracker.State(id, application.RunState); got != application.RunRunning {
		return fmt.Errorf("expected %s to be recorded %s=%s again, got %q", id, application.RunState, application.RunRunning, got)
	}
	return nil
}

func (c *nextContext) theReportSaysItWasSentBack(id string) error {
	if c.err != nil {
		return fmt.Errorf("expected a send-back to be no failure, got: %v", c.err)
	}
	if c.report.SentBack == "" {
		return fmt.Errorf("expected the report to name the session %s was sent back to, got %+v", id, c.report)
	}
	if want := "SENT BACK (merge-conflict) "; !strings.Contains(c.printed.String(), want) {
		return fmt.Errorf("expected the printed report to say %q, got:\n%s", want, c.printed.String())
	}
	return nil
}

func (c *nextContext) sessionsWereEverStarted(count int, id string) error {
	started := 0
	for _, name := range c.runner.Started() {
		if name == application.SessionName(id) {
			started++
		}
	}
	if started != count {
		return fmt.Errorf("expected %d session(s) ever started for %s, got %d: %q (the close-out said: %v)",
			count, id, started, c.runner.Started(), c.err)
	}
	return nil
}

func (c *nextContext) theOtherHostsLineIsKept(id, branch string) error {
	said, err := gitSay(c.origin(), "show", branch+":"+id+".md")
	if err != nil {
		return err
	}
	if !strings.Contains(said, strings.TrimSpace(otherHostsLine)) {
		return fmt.Errorf("expected %s at the origin to keep the other host's line, got %q", branch, said)
	}
	return nil
}

// theSessionWasTheFirstAttempt is what dispatch leaves on a story it started.
func (c *nextContext) theSessionWasTheFirstAttempt(id string) error {
	return c.tracker.SetStoryMetadata(context.Background(), id, map[string]string{application.AttemptsField: "1"})
}

func (c *nextContext) theAttemptsCountedComeTo(id string, want int) error {
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return err
	}
	if detail.Attempts != want {
		return fmt.Errorf("expected %s to have %d attempts counted, got %d", id, want, detail.Attempts)
	}
	return nil
}
