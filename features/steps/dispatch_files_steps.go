package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"

	"github.com/cucumber/godog"
)

// registerFileSteps registers the steps of dispatch.feature's files scenarios
// (mw-gq6.349): stories that name the files they expect to change, running
// stories whose branches have changed some, and mw status reading the same.
func (c *dispatchContext) registerFileSteps(ctx *godog.ScenarioContext) {
	ctx.Given(`^a ready story "([^"]*)" of that epic expecting to change "([^"]*)"$`, c.aReadyStoryExpectingToChange)
	ctx.Given(`^a ready story "([^"]*)" of that epic labelled "([^"]*)" expecting to change "([^"]*)"$`, c.aReadyStoryLabelledExpectingToChange)
	ctx.Given(`^a story "([^"]*)" of that epic is already running here, expecting to change "([^"]*)"$`, c.aStoryRunningExpectingToChange)
	ctx.Given(`^a story "([^"]*)" of that epic is running on "([^"]*)", expecting to change "([^"]*)"$`, c.aStoryRunningOnAnotherHostExpectingToChange)
	ctx.Given(`^a story "([^"]*)" of that epic is already running here, its branch changing "([^"]*)"$`, c.aStoryRunningWithItsBranchChanging)
	ctx.Given(`^the branch of "([^"]*)" has a commit changing "([^"]*)"$`, c.theBranchHasACommitChanging)
	ctx.Given(`^a story "([^"]*)" of that epic is already running here under the formula "([^"]*)"$`, c.aStoryRunningUnderTheFormula)
	ctx.Given(`^the rig "([^"]*)" expects a files key$`, c.theRigExpectsAFilesKey)
	ctx.When(`^the story "([^"]*)" is pathed with files "([^"]*)"$`, c.theStoryIsPathedWithFiles)
	ctx.Given(`^the story "([^"]*)" has recorded "([^"]*)" as changed$`, c.theStoryHasRecordedAsChanged)
	ctx.When(`^mw status is read on "([^"]*)"$`, c.mwStatusIsReadOn)
	ctx.Then(`^mw status lists "([^"]*)" as ready, saying "([^"]*)"$`, c.mwStatusListsAsReadySaying)
	ctx.Then(`^mw status lists "([^"]*)" as ready, saying nothing of waiting$`, c.mwStatusListsAsReadyNotWaiting)
}

// theRigExpectsAFilesKey is the rig's file in the vault saying files_expected =
// true (mw-gq6.390).
func (c *dispatchContext) theRigExpectsAFilesKey(rigName string) error {
	if c.rules == nil {
		c.rules = apptest.NewFakeEpicRules()
	}
	c.rules.Require(rigName, domain.EpicRequirements{FilesExpected: true})
	return nil
}

// theStoryIsPathedWithFiles is mw path set <id> files=<csv>: the key is
// written to the story's metadata and nothing else changes (mw-gq6.599).
func (c *dispatchContext) theStoryIsPathedWithFiles(id, files string) error {
	return c.tracker.SetStoryMetadata(context.Background(), id, map[string]string{"files": files})
}

func (c *dispatchContext) aReadyStoryExpectingToChange(id, files string) error {
	return c.aReadyStoryThatOverrides(id, "files", files)
}

// aReadyStoryLabelledExpectingToChange is a ready story carrying a label and
// naming the files it expects to change (mw-gq6.535).
func (c *dispatchContext) aReadyStoryLabelledExpectingToChange(id, label, files string) error {
	if err := c.aReadyStoryExpectingToChange(id, files); err != nil {
		return err
	}
	return c.tracker.SetLabels(id, label)
}

// aStoryRunningUnderTheFormula is a story pathed to a formula and claimed here
// (mw-gq6.413).
func (c *dispatchContext) aStoryRunningUnderTheFormula(id, formula string) error {
	if err := c.aReadyStoryThatOverrides(id, "formula", formula); err != nil {
		return err
	}
	return c.tracker.ClaimStory(context.Background(), id)
}

func (c *dispatchContext) aStoryRunningExpectingToChange(id, files string) error {
	if err := c.aReadyStoryExpectingToChange(id, files); err != nil {
		return err
	}
	return c.tracker.ClaimStory(context.Background(), id)
}

// aStoryRunningOnAnotherHostExpectingToChange is a story pathed to host and
// claimed there, as a session of that host holds it (mw-gq6.389).
func (c *dispatchContext) aStoryRunningOnAnotherHostExpectingToChange(id, host, files string) error {
	story := domain.Story{ID: id, Title: "The story " + id}
	if err := story.Overrides.Set("files", files); err != nil {
		return err
	}
	if err := story.Overrides.Set("host", host); err != nil {
		return err
	}
	c.tracker.AddStory(c.lastEpic, story)
	return c.tracker.ClaimAs(id, "mw@"+host, dispatchNow.Add(5*time.Minute))
}

func (c *dispatchContext) aStoryRunningWithItsBranchChanging(id, file string) error {
	if err := c.aStoryAlreadyRunningHere(id); err != nil {
		return err
	}
	return c.theBranchHasACommitChanging(id, file)
}

// theBranchHasACommitChanging cuts the story's worktree from the rig's target
// branch, as a dispatch does, when it has none yet, and commits a change to
// file on its branch: what a session part way through its story leaves.
func (c *dispatchContext) theBranchHasACommitChanging(id, file string) error {
	ctx := context.Background()
	dir, branch := c.worktreeOf(id), application.StoryBranch(id)
	if _, err := os.Stat(dir); err != nil {
		if err := rig.New().Add(ctx, c.rig, dir, branch, application.StartPoint(application.DefaultRemote, "main")); err != nil {
			return fmt.Errorf("cutting the worktree of %s: %w", id, err)
		}
	}
	target := filepath.Join(dir, filepath.FromSlash(file))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(target, []byte("changed by "+id+"\n"), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-qm", "change " + file}} {
		if err := gitRun(dir, "git", args...); err != nil {
			return err
		}
	}
	return nil
}

// mwStatusIsReadOn reads mw status from the scenario's tracker and rig, with
// the real worktrees adapter to read a running story's branch through.
func (c *dispatchContext) mwStatusIsReadOn(host string) error {
	c.out.Reset()
	var rules application.EpicRules
	if c.rules != nil {
		rules = c.rules
	}
	c.status, c.statusErr = application.Status{
		Tracker: c.tracker,
		Rules:   rules,
		Host:    host,
		Seat:    "builder",
		Rigs:    map[string]string{"millwright": c.rig},
		Changes: rig.New(),
		Out:     &c.out,
	}.Run(context.Background())
	return nil
}

// statusReady is the READY block mw status printed for id: its lines from the
// story's own up to the next story or the end of the section.
func (c *dispatchContext) statusReady(id string) (string, error) {
	if c.statusErr != nil {
		return "", fmt.Errorf("mw status failed: %w", c.statusErr)
	}
	listed := false
	for _, d := range c.status.Ready {
		listed = listed || d.Story.ID == id
	}
	if !listed {
		return "", fmt.Errorf("expected %s among the ready stories, got %+v", id, c.status.Ready)
	}
	printed := c.status.String()
	_, ready, found := strings.Cut(printed, "READY (")
	if !found {
		return "", fmt.Errorf("mw status printed no READY section:\n%s", printed)
	}
	ready, _, _ = strings.Cut(ready, "\n\n")
	_, block, found := strings.Cut(ready, "  "+id+" · ")
	if !found {
		return "", fmt.Errorf("mw status did not list %s under READY:\n%s", id, printed)
	}
	var lines []string
	for i, line := range strings.Split(block, "\n") {
		if i > 0 && strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") {
			break
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"), nil
}

func (c *dispatchContext) mwStatusListsAsReadySaying(id, saying string) error {
	block, err := c.statusReady(id)
	if err != nil {
		return err
	}
	if !strings.Contains(block, "\n    "+saying) {
		return fmt.Errorf("expected mw status to say %q of %s, it printed:\n%s", saying, id, block)
	}
	return nil
}

func (c *dispatchContext) mwStatusListsAsReadyNotWaiting(id string) error {
	block, err := c.statusReady(id)
	if err != nil {
		return err
	}
	if strings.Contains(block, "waiting") {
		return fmt.Errorf("expected mw status to say nothing of %s waiting, it printed:\n%s", id, block)
	}
	return nil
}

// theStoryHasRecordedAsChanged is what mw next leaves on a running story as its
// branch changes (mw-gq6.406): the files changed so far, in its metadata.
func (c *dispatchContext) theStoryHasRecordedAsChanged(id, files string) error {
	return c.tracker.SetStoryMetadata(context.Background(), id, map[string]string{application.FilesChangedField: files})
}
