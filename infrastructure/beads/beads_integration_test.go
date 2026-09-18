package beads_test

// This test drives a real bd against a throwaway database in a temp directory.
// It never touches the factory's vault. bd is slow to start (about a second a
// call on the VPS, several for `bd init`), so the whole story — create, show,
// list, claim, write, close — runs against one database in one test.

import (
	"context"
	"os/exec"
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
