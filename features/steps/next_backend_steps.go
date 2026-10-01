package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"

	"github.com/cucumber/godog"
)

// The steps of the backend a landing may leave undeployed (mw-gq6.185). They hang
// off nextContext: c.backend is what this scenario's host says about the rig's
// backend, and the stage it builds is handed to mw next as its Backend.

// backendFixture is the [backend.<rig>] table of a scenario's host and the hands
// step's fixtures: where the binary is staged, who is home, what was pushed.
type backendFixture struct {
	dir    string
	home   string
	host   string
	sender *apptest.FakePosternSender
}

// registerNextBackendSteps registers the backend steps of features/next.feature.
func registerNextBackendSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^this host names a backend for the rig, built in "([^"]*)", and is the home$`, c.thisHostIsHomeWithABackend)
	ctx.Given(`^this host names a backend for the rig, built in "([^"]*)", and "([^"]*)" is the home$`, c.thisHostNamesABackendHomeElsewhere)
	ctx.Given(`^the work of "([^"]*)" also changes "([^"]*)"$`, c.theWorkAlsoChanges)

	ctx.Then(`^the backend was staged for the commit that landed on "([^"]*)", built from that commit$`, c.theBackendWasStaged)
	ctx.Then(`^a new open hitl bead under "([^"]*)" holds one hands step that swaps it, with the check, backup, install, restart, four tries and way back$`, c.aHandsStepSwapsIt)
	ctx.Then(`^one message was sent on that bead's channel$`, c.oneMessageWasSent)
	ctx.Then(`^no backend was built, no bead filed and no message sent$`, c.noBackendWasStaged)
	ctx.Then(`^the landing is left as a note for the home "([^"]*)" and nothing was built$`, c.theLandingWaitsForTheHome)
}

func (c *nextContext) thisHostIsHomeWithABackend(dir string) error {
	return c.backendSettings(dir, "laptop", "laptop")
}

func (c *nextContext) thisHostNamesABackendHomeElsewhere(dir, home string) error {
	return c.backendSettings(dir, home, "vps")
}

func (c *nextContext) backendSettings(dir, home, host string) error {
	c.backend = &backendFixture{dir: dir, home: home, host: host, sender: &apptest.FakePosternSender{}}
	return nil
}

// stagedAt is where this scenario's backend is staged: in the scenario's own
// directory, never the host's.
func (c *nextContext) stagedAt() string { return filepath.Join(c.root, "stage") }

// backendStage is the BackendStage mw next is given: nothing for a scenario
// whose host names no backend.
func (c *nextContext) backendStage() application.BackendStage {
	if c.backend == nil {
		return application.BackendStage{}
	}
	return application.BackendStage{
		Rigs: map[string]string{"millwright": c.rig},
		Settings: map[string]application.BackendRig{"millwright": {
			Dir:     c.backend.dir,
			Build:   "cat route.txt > {out}",
			Stage:   c.stagedAt(),
			Live:    filepath.Join(c.root, "live", "backend"),
			Service: "backend-unit",
			Health:  "http://127.0.0.1:1/api/healthz",
			Check:   "mw postern inbox --unread-count",
		}},
		Builds:  c.worktrees,
		Home:    &apptest.FakeHomeFile{Text: c.backend.home + " 2026-10-01T05:00:00Z mayor\n"},
		Host:    c.backend.host,
		Tracker: c.tracker,
		Notes:   c.tracker,
		Hands: application.HandsAdd{
			Tracker: c.tracker, Notes: c.tracker, Push: c.backend.sender,
			Now: func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) },
		},
	}
}

// theWorkAlsoChanges commits one more file in the story's worktree.
func (c *nextContext) theWorkAlsoChanges(id, file string) error {
	dir := application.WorktreeDir(c.rig, id)
	full := filepath.Join(dir, filepath.FromSlash(file))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(full, []byte("the backend of "+id+"\n"), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-qm", "The backend of " + id}} {
		if err := gitRun(dir, "git", args...); err != nil {
			return err
		}
	}
	return nil
}

func (c *nextContext) theBackendWasStaged(branch string) error {
	landed, err := gitSay(c.origin(), "rev-parse", branch)
	if err != nil {
		return err
	}
	staged := filepath.Join(c.stagedAt(), "backend-"+landed[:7])
	got, err := os.ReadFile(staged)
	if err != nil {
		return fmt.Errorf("expected the backend staged at %s: %w (report %q)", staged, err, c.printed.String())
	}
	if !strings.Contains(string(got), "the backend of mw-gq6.1") {
		return fmt.Errorf("expected the binary built from the landed commit's server/, got %q", got)
	}
	return nil
}

// newBead is the one bead filed under the epic by the close-out, the story
// itself aside.
func (c *nextContext) newBead(epic string) (application.StoryDetail, error) {
	detail, err := c.tracker.ShowEpic(context.Background(), epic)
	if err != nil {
		return application.StoryDetail{}, err
	}
	var filed []application.StoryDetail
	for _, s := range detail.Stories {
		if s.Story.ID != "mw-gq6.1" {
			filed = append(filed, s)
		}
	}
	if len(filed) != 1 {
		return application.StoryDetail{}, fmt.Errorf("expected one new bead under %s, got %d", epic, len(filed))
	}
	return filed[0], nil
}

func (c *nextContext) aHandsStepSwapsIt(epic string) error {
	bead, err := c.newBead(epic)
	if err != nil {
		return err
	}
	if !bead.Hitl() || bead.Status != application.StatusOpen {
		return fmt.Errorf("expected an open hitl bead, got %q %v", bead.Status, bead.Labels)
	}
	raw, err := c.tracker.Note(context.Background(), application.HandsStepsKey(bead.Story.ID))
	if err != nil {
		return err
	}
	var steps []application.HandsStepRecord
	if err := json.Unmarshal([]byte(raw), &steps); err != nil || len(steps) != 1 {
		return fmt.Errorf("expected one hands step on %s, got %q (%v)", bead.Story.ID, raw, err)
	}
	for _, want := range []string{"cmp -s", "cp -p", "install -m 755", "systemctl --user restart", "for i in 1 2 3 4", "putting the old backend back"} {
		if !strings.Contains(steps[0].Run, want) {
			return fmt.Errorf("expected the step to hold %q, got %s", want, steps[0].Run)
		}
	}
	if !strings.Contains(steps[0].WayBack, "install -m 755") {
		return fmt.Errorf("expected a way back, got %q", steps[0].WayBack)
	}
	return nil
}

func (c *nextContext) oneMessageWasSent() error {
	bead, err := c.newBead("mw-gq6")
	if err != nil {
		return err
	}
	sent := c.backend.sender.Sent()
	if len(sent) != 1 || sent[0].Thread != bead.Story.ID {
		return fmt.Errorf("expected one message on %s's channel, got %+v", bead.Story.ID, sent)
	}
	return nil
}

func (c *nextContext) noBackendWasStaged() error {
	if staged, _ := filepath.Glob(filepath.Join(c.stagedAt(), "*")); len(staged) != 0 {
		return fmt.Errorf("expected nothing staged, got %v", staged)
	}
	if _, err := c.newBead("mw-gq6"); err == nil {
		return fmt.Errorf("expected no new bead")
	}
	if sent := c.backend.sender.Sent(); len(sent) != 0 {
		return fmt.Errorf("expected no message, got %+v", sent)
	}
	return nil
}

func (c *nextContext) theLandingWaitsForTheHome(home string) error {
	if staged, _ := filepath.Glob(filepath.Join(c.stagedAt(), "*")); len(staged) != 0 {
		return fmt.Errorf("expected nothing built on a host that is not home, got %v", staged)
	}
	pending, err := c.tracker.NotesWithPrefix(context.Background(), application.BackendPendingPrefix)
	if err != nil || len(pending) != 1 {
		return fmt.Errorf("expected one pending note, got %v (%v)", pending, err)
	}
	if !strings.Contains(c.printed.String(), home) {
		return fmt.Errorf("expected the report to name the home %s, got %q", home, c.printed.String())
	}
	return nil
}
