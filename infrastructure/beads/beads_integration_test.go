package beads_test

// This test drives a real bd against a throwaway database in a temp directory.
// It never touches the factory's vault. bd is slow to start (about a second a
// call on the VPS, several for `bd init`), so the whole story — create, show,
// list, claim, write, close — runs against one database in one test.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// throwawayVault makes an empty beads database in a temp directory and returns
// the directory it is in.
func throwawayVault(t *testing.T) string {
	t.Helper()
	if !beads.Available() {
		t.Skipf("%s is not on PATH", beads.Program)
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}

	vault := t.TempDir()
	bdRun(t, vault, "git", "init", "-q")
	bdRun(t, vault, beads.Program, "init", "-p", "t", "--non-interactive",
		"--role", "maintainer", "--skip-agents", "--skip-hooks", "-q")
	return vault
}

// bdRun runs one setup command in the throwaway vault and fails the test if it
// does not succeed. It returns the trimmed standard output.
func bdRun(t *testing.T, vault, program string, args ...string) string {
	t.Helper()
	cmd := exec.Command(program, args...)
	cmd.Dir = vault
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", program, strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// installFormula copies one of the rig's formulas into a throwaway database's
// own formulas directory, which is where bd looks for them first.
func installFormula(t *testing.T, vault, name string) {
	t.Helper()
	dir := filepath.Join(vault, ".beads", "formulas")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making %s: %v", dir, err)
	}
	written, err := os.ReadFile(filepath.Join("..", "..", "formulas", name+".formula.json"))
	if err != nil {
		t.Fatalf("reading the %s formula: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".formula.json"), written, 0o644); err != nil {
		t.Fatalf("installing the %s formula: %v", name, err)
	}
}

func TestGatewayWorksAStoryThroughBeads(t *testing.T) {
	vault := throwawayVault(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	epicID := bdRun(t, vault, beads.Program, "create", "A walking skeleton", "-t", "epic",
		"--metadata", `{"rig":"millwright","branch":"main","harness":"claude","model":"opus","effort":"high","formula":"tdd-feature","host":"vps"}`,
		"--silent")
	storyID := bdRun(t, vault, beads.Program, "create", "Beads gateway",
		"--metadata", `{"model":"haiku"}`,
		"--acceptance", "make test passes", "-e", "90",
		"--parent", epicID, "--silent")

	gateway := beads.New(vault)

	// The story's Path is the epic's defaults with the story's override on top.
	detail, err := gateway.ShowStory(ctx, storyID)
	if err != nil {
		t.Fatalf("showing %s: %v", storyID, err)
	}
	if detail.Story.Title != "Beads gateway" || detail.EpicID != epicID {
		t.Errorf("expected the story's title and epic, got %+v", detail)
	}
	if detail.Acceptance != "make test passes" || detail.EstimateMinutes != 90 {
		t.Errorf("expected the acceptance criteria and estimate, got %q and %d", detail.Acceptance, detail.EstimateMinutes)
	}
	path, err := detail.Path()
	if err != nil {
		t.Fatalf("the story has no path: %v", err)
	}
	want := domain.Path{
		Rig:     "millwright",
		Branch:  "main",
		Harness: domain.HarnessClaude,
		Model:   domain.ModelHaiku,
		Effort:  domain.EffortHigh,
		Formula: "tdd-feature",
		Host:    "vps",
	}
	if path != want {
		t.Fatalf("expected the merged path %+v, got %+v", want, path)
	}

	// It is ready on the host its Path names, which it inherits from the epic.
	ready, err := gateway.ReadyStories(ctx, epicID, "vps")
	if err != nil {
		t.Fatalf("listing the ready stories: %v", err)
	}
	if len(ready) != 1 || ready[0].Story.ID != storyID {
		t.Fatalf("expected %s to be ready on vps, got %+v", storyID, ready)
	}
	if model := ready[0].Merged().Model; model != domain.ModelHaiku {
		t.Errorf("expected a ready story to carry its merged path, got model %q", model)
	}

	// And not on another host.
	elsewhere, err := gateway.ReadyStories(ctx, epicID, "laptop")
	if err != nil {
		t.Fatalf("listing the ready stories for laptop: %v", err)
	}
	if len(elsewhere) != 0 {
		t.Fatalf("expected nothing ready on laptop, got %+v", elsewhere)
	}

	// A dispatcher asks for what is ready anywhere, not under one epic, and for
	// what this host already has in flight.
	anywhere, err := gateway.ReadyForHost(ctx, "vps")
	if err != nil {
		t.Fatalf("listing what is ready on vps: %v", err)
	}
	if len(anywhere) != 1 || anywhere[0].Story.ID != storyID {
		t.Fatalf("expected %s to be ready on vps, got %+v", storyID, anywhere)
	}
	if elsewhere, err := gateway.ReadyForHost(ctx, "laptop"); err != nil || len(elsewhere) != 0 {
		t.Fatalf("expected nothing ready on laptop, got %+v: %v", elsewhere, err)
	}
	if inFlight, err := gateway.RunningStories(ctx, "vps"); err != nil || len(inFlight) != 0 {
		t.Fatalf("expected nothing running on vps yet, got %+v: %v", inFlight, err)
	}

	// Claiming it takes it out of the ready stories.
	if err := gateway.ClaimStory(ctx, storyID); err != nil {
		t.Fatalf("claiming %s: %v", storyID, err)
	}
	ready, err = gateway.ReadyStories(ctx, epicID, "vps")
	if err != nil {
		t.Fatalf("listing the ready stories after the claim: %v", err)
	}
	if len(ready) != 0 {
		t.Fatalf("expected a claimed story not to be ready, got %+v", ready)
	}

	// A claimed story is what the concurrency cap counts, and releasing the
	// claim makes it ready again — which is what a dispatch that failed after
	// claiming does.
	inFlight, err := gateway.RunningStories(ctx, "vps")
	if err != nil {
		t.Fatalf("listing what is running on vps: %v", err)
	}
	if len(inFlight) != 1 || inFlight[0].Story.ID != storyID {
		t.Fatalf("expected %s to be running on vps, got %+v", storyID, inFlight)
	}
	if err := gateway.ReleaseClaim(ctx, storyID); err != nil {
		t.Fatalf("giving back the claim on %s: %v", storyID, err)
	}
	given, err := gateway.ReadyForHost(ctx, "vps")
	if err != nil {
		t.Fatalf("listing what is ready after the claim was given back: %v", err)
	}
	if len(given) != 1 || given[0].Story.ID != storyID {
		t.Fatalf("expected %s to be ready again once the claim was given back, got %+v", storyID, given)
	}
	if err := gateway.ClaimStory(ctx, storyID); err != nil {
		t.Fatalf("claiming %s again: %v", storyID, err)
	}

	// What mw records on a dispatched story: run=running, with where it runs.
	if err := gateway.SetStoryState(ctx, storyID, application.RunState, application.RunRunning,
		"dispatched by the test"); err != nil {
		t.Fatalf("recording the run state of %s: %v", storyID, err)
	}
	if state := bdRun(t, vault, beads.Program, "state", storyID, application.RunState); state != application.RunRunning {
		t.Fatalf("expected %s to be recorded %s=%s, got %q", storyID, application.RunState, application.RunRunning, state)
	}

	// Metadata written is metadata read back, Path fields included.
	if err := gateway.SetStoryMetadata(ctx, storyID, map[string]string{"effort": "max", "run": "running"}); err != nil {
		t.Fatalf("setting metadata on %s: %v", storyID, err)
	}
	detail, err = gateway.ShowStory(ctx, storyID)
	if err != nil {
		t.Fatalf("showing %s after the write: %v", storyID, err)
	}
	if detail.Merged().Effort != domain.EffortMax {
		t.Errorf("expected the written effort to be read back, got %q", detail.Merged().Effort)
	}
	if detail.Status != beads.StatusInProgress || detail.Assignee == "" {
		t.Errorf("expected a claimed story, got status %q assignee %q", detail.Status, detail.Assignee)
	}

	// A claim made a moment ago is not stale.
	stale, err := gateway.StaleClaims(ctx, 1)
	if err != nil {
		t.Fatalf("listing the stale claims: %v", err)
	}
	for _, claim := range stale {
		if claim.Story.ID == storyID {
			t.Errorf("expected a fresh claim not to be stale, got %+v", claim)
		}
	}

	// Commenting and closing.
	if err := gateway.CommentOnStory(ctx, storyID, "Green, with a note: bd prints dependencies two ways."); err != nil {
		t.Fatalf("commenting on %s: %v", storyID, err)
	}
	if err := gateway.CloseStory(ctx, storyID, "the gateway reads and writes stories"); err != nil {
		t.Fatalf("closing %s: %v", storyID, err)
	}
	detail, err = gateway.ShowStory(ctx, storyID)
	if err != nil {
		t.Fatalf("showing %s after the close: %v", storyID, err)
	}
	if detail.Status != "closed" {
		t.Fatalf("expected %s to be closed, got status %q", storyID, detail.Status)
	}

	// The rig's own formula, installed where bd pours from, poured for a story:
	// the steps come back in the order they are worked, whatever order bd
	// listed them in, and none of them is offered to a dispatcher as a story of
	// its own — a step bead carries no path, and so belongs to no host.
	installFormula(t, vault, "tdd-feature")
	installed, err := gateway.Formulas(ctx)
	if err != nil {
		t.Fatalf("listing the installed formulas: %v", err)
	}
	if len(installed) != 1 || installed[0] != "tdd-feature" {
		t.Fatalf("expected tdd-feature to be installed, got %q", installed)
	}

	molecule, err := gateway.PourFormula(ctx, "tdd-feature", storyID, "Beads gateway")
	if err != nil {
		t.Fatalf("pouring tdd-feature for %s: %v", storyID, err)
	}
	if !molecule.Poured() || molecule.RootID == "" {
		t.Fatalf("expected a poured molecule, got %+v", molecule)
	}
	worked := make([]string, 0, len(molecule.Steps))
	for _, step := range molecule.Steps {
		worked = append(worked, step.Title)
	}
	if len(worked) != 7 {
		t.Fatalf("expected the seven steps of tdd-feature, got %q", worked)
	}
	if !strings.HasPrefix(worked[0], "Understand "+storyID) {
		t.Fatalf("expected the first step to be understanding the story, got %q", worked[0])
	}
	for at, want := range map[int]string{1: "failing", 2: "green", 3: "vet", 6: "closing comment"} {
		if !strings.Contains(worked[at], want) {
			t.Fatalf("expected step %d to be about %q, got %q (all: %q)", at+1, want, worked[at], worked)
		}
	}
	if steps, err := gateway.ReadyForHost(ctx, "vps"); err != nil || len(steps) != 0 {
		t.Fatalf("expected poured steps not to be offered as stories, got %+v: %v", steps, err)
	}

	// The note each host leaves saying when it was last level with the other.
	// Reading one nobody has written is not a failure: it is a host the other
	// has never heard from. `bd sync` itself is never run here — it would reach
	// the factory's real remote — so only the note is exercised against bd.
	key := application.LastSyncKey("vps")
	before, err := gateway.Note(ctx, key)
	if err != nil {
		t.Fatalf("reading %s before anything wrote it: %v", key, err)
	}
	if before != "" {
		t.Fatalf("expected no note yet, got %q", before)
	}
	level := time.Now().UTC().Format(application.LastSyncFormat)
	if err := gateway.SetNote(ctx, key, level); err != nil {
		t.Fatalf("writing %s: %v", key, err)
	}
	after, err := gateway.Note(ctx, key)
	if err != nil {
		t.Fatalf("reading %s: %v", key, err)
	}
	if after != level {
		t.Fatalf("expected %s to be %q, got %q", key, level, after)
	}
}

// Filing a plan is the other half of the gateway: it writes beads rather than
// reading them. Everything here goes into one throwaway database too.
func TestGatewayFilesAnEpicWithItsStoriesHeld(t *testing.T) {
	vault := throwawayVault(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	gateway := beads.New(vault)
	defaults := domain.Path{
		Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Formula: "tdd-feature", Host: "vps",
	}

	epicID, err := gateway.CreateEpic(ctx, application.NewEpic{
		Title:           "Walking skeleton",
		Description:     "The smallest mw that closes the loop.",
		SuccessCriteria: "One story goes from the plan file to a commit.",
		Priority:        1,
		Defaults:        defaults,
	})
	if err != nil {
		t.Fatalf("filing the epic: %v", err)
	}

	first, err := gateway.CreateStory(ctx, application.NewStory{
		EpicID:          epicID,
		Title:           "Go module and the Path domain",
		Description:     "Create the module.",
		Acceptance:      "make test passes.",
		Priority:        1,
		EstimateMinutes: 60,
	})
	if err != nil {
		t.Fatalf("filing the first story: %v", err)
	}
	second, err := gateway.CreateStory(ctx, application.NewStory{
		EpicID:     epicID,
		Title:      "First formulas",
		Acceptance: "Both formulas cook.",
		Priority:   2,
		Overrides:  domain.Path{Model: domain.ModelSonnet, Formula: "chore"},
		Needs:      []string{first},
	})
	if err != nil {
		t.Fatalf("filing the second story: %v", err)
	}

	// Held: beads offers a dispatcher neither of them, path or no path.
	ready, err := gateway.ReadyStories(ctx, epicID, "vps")
	if err != nil {
		t.Fatalf("listing the ready stories of a held plan: %v", err)
	}
	if len(ready) != 0 {
		t.Fatalf("expected a held plan to leave nothing ready, got %+v", ready)
	}

	// The story carries its structured fields and its path override.
	detail, err := gateway.ShowStory(ctx, second)
	if err != nil {
		t.Fatalf("showing %s: %v", second, err)
	}
	if detail.Acceptance != "Both formulas cook." || detail.EpicID != epicID {
		t.Errorf("expected the filed story's acceptance criteria and epic, got %+v", detail)
	}
	path, err := detail.Path()
	if err != nil {
		t.Fatalf("the filed story has no path: %v", err)
	}
	want := defaults
	want.Model, want.Formula = domain.ModelSonnet, "chore"
	if path != want {
		t.Errorf("expected the filed story's path %+v, got %+v", want, path)
	}

	// An epic's success criteria are where bd lint looks for them.
	epic, err := gateway.ShowStory(ctx, epicID)
	if err != nil {
		t.Fatalf("showing %s: %v", epicID, err)
	}
	if !strings.Contains(epic.Description, "## Success Criteria") {
		t.Errorf("expected the epic to carry its success criteria, got %q", epic.Description)
	}

	// bd's own lint is the cheapest check that what was filed is specified
	// well enough to be worked: it exits 1 and says what is missing when an
	// epic has no success criteria or a story no acceptance criteria.
	if said := bdRun(t, vault, beads.Program, "lint", epicID, first, second); !strings.Contains(said, "No template warnings") {
		t.Errorf("expected a filed plan to pass bd lint, got %q", said)
	}

	// Released: the one that waits on nothing is ready, the other is not.
	for _, id := range []string{first, second} {
		if err := gateway.ReleaseStory(ctx, id); err != nil {
			t.Fatalf("releasing %s: %v", id, err)
		}
	}
	ready, err = gateway.ReadyStories(ctx, epicID, "vps")
	if err != nil {
		t.Fatalf("listing the ready stories of a released plan: %v", err)
	}
	if len(ready) != 1 || ready[0].Story.ID != first {
		t.Fatalf("expected only %s to be ready, got %+v", first, ready)
	}

	// And beads lets the second one through once the first is closed.
	if err := gateway.CloseStory(ctx, first, "worked"); err != nil {
		t.Fatalf("closing %s: %v", first, err)
	}
	ready, err = gateway.ReadyStories(ctx, epicID, "vps")
	if err != nil {
		t.Fatalf("listing the ready stories after the close: %v", err)
	}
	if len(ready) != 1 || ready[0].Story.ID != second {
		t.Fatalf("expected %s to be ready once %s is closed, got %+v", second, first, ready)
	}
}

func TestGatewayReportsWhatBeadsRefused(t *testing.T) {
	vault := throwawayVault(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	_, err := beads.New(vault).ShowStory(ctx, "t-nope")
	if err == nil {
		t.Fatal("expected showing an unknown story to fail")
	}
	if !strings.Contains(err.Error(), "t-nope") {
		t.Fatalf("expected the error to name the story, got %q", err)
	}
}
