//go:build beads_integration

package beads_test

// This test drives a real bd against throwaway databases in temp directories.
// It never touches the factory's vault. bd is slow to start (about a second a
// call on the VPS, several for `bd init`), so each case works a whole story —
// create, show, list, claim, write, close — in one database of its own.
//
// It is behind a build tag because it is most of this package's clock: a plain
// `go test ./infrastructure/beads` skips it (see beads_integration_skipped_test.go)
// and `make test` runs it with -tags beads_integration.
//
// `bd init` is paid once, in TestMain; each case starts from a copy of that
// database, so the cases stay as separate from one another as they were when
// each ran its own `bd init`.

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// templateVault is an empty beads database, made once for the whole binary and
// copied for each case; templateSkip says why there is none when bd or git is
// missing, and the cases skip with it.
var (
	templateVault string
	templateSkip  string
)

func TestMain(m *testing.M) {
	// bd finds its database from these before it looks at the directory it runs
	// in: a session that carries them must not point a test at its own vault.
	os.Unsetenv("BEADS_DIR")
	os.Unsetenv("BEADS_DB")

	root, err := os.MkdirTemp("", "mw-beads-template-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "making the template database directory:", err)
		os.Exit(1)
	}
	code := func() int {
		defer os.RemoveAll(root)
		switch {
		case !beads.Available():
			templateSkip = beads.Program + " is not on PATH"
		default:
			if _, err := exec.LookPath("git"); err != nil {
				templateSkip = "git is not on PATH"
				break
			}
			templateVault = root
			for _, args := range [][]string{
				{"git", "init", "-q"},
				{beads.Program, "init", "-p", "t", "--non-interactive",
					"--role", "maintainer", "--skip-agents", "--skip-hooks", "-q"},
			} {
				cmd := exec.Command(args[0], args[1:]...)
				cmd.Dir = root
				if out, err := cmd.CombinedOutput(); err != nil {
					fmt.Fprintf(os.Stderr, "%s: %v\n%s", strings.Join(args, " "), err, out)
					return 1
				}
			}
		}
		return m.Run()
	}()
	os.Exit(code)
}

// throwawayVault makes an empty beads database in a temp directory, a copy of
// the template, and returns the directory it is in.
func throwawayVault(t *testing.T) string {
	t.Helper()
	if templateSkip != "" {
		t.Skip(templateSkip)
	}

	vault := t.TempDir()
	if err := copyTree(templateVault, vault); err != nil {
		t.Fatalf("copying the template database into %s: %v", vault, err)
	}
	return vault
}

// copyTree copies the regular files and directories under src into dst, which
// exists, keeping their permissions.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil || rel == "." {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case entry.IsDir():
			return os.MkdirAll(target, info.Mode().Perm())
		case info.Mode().IsRegular():
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(target, content, info.Mode().Perm())
		default:
			return fmt.Errorf("%s is neither a file nor a directory", path)
		}
	})
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

	// And mw status, reading from the other host, sees the same story as work
	// pathed away from it — with the epic's defaults overlaid, since that is
	// where its host came from. From the VPS itself it is not work elsewhere.
	inHand, err := gateway.WorkInHand(ctx)
	if err != nil {
		t.Fatalf("listing what is in hand: %v", err)
	}
	away := inHand.Elsewhere("laptop")
	if len(away) != 1 || away[0].Story.ID != storyID || away[0].Merged().Host != "vps" {
		t.Fatalf("expected %s to be work elsewhere as far as laptop is concerned, got %+v", storyID, away)
	}
	if here := inHand.Elsewhere("vps"); len(here) != 0 {
		t.Fatalf("expected nothing pathed away from vps, got %+v", here)
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
	// A claimed story is still work pathed elsewhere: that is exactly the work
	// that strands when its host stops syncing.
	inHand, err = gateway.WorkInHand(ctx)
	if err != nil {
		t.Fatalf("listing what is in hand after the claim: %v", err)
	}
	if away := inHand.Elsewhere("laptop"); len(away) != 1 || away[0].Story.ID != storyID {
		t.Fatalf("expected the claimed %s to still be work elsewhere, got %+v", storyID, away)
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
	if err := gateway.SetStoryMetadata(ctx, storyID, map[string]string{"effort": "max", "run": "running", application.AttemptsField: "2"}); err != nil {
		t.Fatalf("setting metadata on %s: %v", storyID, err)
	}
	detail, err = gateway.ShowStory(ctx, storyID)
	if err != nil {
		t.Fatalf("showing %s after the write: %v", storyID, err)
	}
	if detail.Merged().Effort != domain.EffortMax {
		t.Errorf("expected the written effort to be read back, got %q", detail.Merged().Effort)
	}
	// bd keeps the count as a number; it comes back as the count.
	if detail.Attempts != 2 || detail.Exhausted {
		t.Errorf("expected the written attempts to be read back as 2, got %d (exhausted %v)", detail.Attempts, detail.Exhausted)
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

	// What a close-out asks before it lands anything: is every step of the
	// story's formula closed? A step closed the way a session closes its own
	// comes off the list, and the ones left come back in the order they are
	// worked.
	open, err := gateway.OpenSteps(ctx, molecule.RootID)
	if err != nil {
		t.Fatalf("reading the open steps of %s: %v", molecule.RootID, err)
	}
	if len(open) != len(molecule.Steps) || open[0].ID != molecule.Steps[0].ID {
		t.Fatalf("expected every poured step open and in worked order, got %d of %d: %+v",
			len(open), len(molecule.Steps), open)
	}
	bdRun(t, vault, beads.Program, "close", molecule.Steps[0].ID, "--reason", "understood")
	open, err = gateway.OpenSteps(ctx, molecule.RootID)
	if err != nil {
		t.Fatalf("reading the open steps of %s again: %v", molecule.RootID, err)
	}
	if len(open) != len(molecule.Steps)-1 {
		t.Fatalf("expected the closed step to come off the list, got %d of %d", len(open), len(molecule.Steps))
	}
	for _, step := range open {
		if step.ID == molecule.Steps[0].ID {
			t.Fatalf("expected %s to be closed, but it is still open", step.ID)
		}
	}
	if none, err := gateway.OpenSteps(ctx, ""); err != nil || len(none) != 0 {
		t.Fatalf("expected a story with no formula poured to have no open steps, got %+v: %v", none, err)
	}

	// What a dispatch asks before it pours again: is the molecule the story
	// recorded still open, and which of its steps are left? One bd has no record
	// of is missing, not a failure, and a root that is closed is not worked again.
	reopened, err := gateway.OpenMolecule(ctx, molecule.RootID)
	if err != nil {
		t.Fatalf("reading the molecule %s: %v", molecule.RootID, err)
	}
	if reopened.RootID != molecule.RootID || len(reopened.Steps) != len(molecule.Steps)-1 || reopened.Steps[0].ID != open[0].ID {
		t.Fatalf("expected the open molecule with its %d open steps in worked order, got %+v", len(open), reopened)
	}
	if missing, err := gateway.OpenMolecule(ctx, "mw-nope-zzz"); err != nil || missing.Poured() || missing.RootID != "" {
		t.Fatalf("expected a molecule bd has no record of to be none, got %+v: %v", missing, err)
	}
	if none, err := gateway.OpenMolecule(ctx, ""); err != nil || none.RootID != "" {
		t.Fatalf("expected no molecule recorded to be none, got %+v: %v", none, err)
	}
	bdRun(t, vault, beads.Program, "close", molecule.RootID, "--force", "--reason", "finished")
	if closed, err := gateway.OpenMolecule(ctx, molecule.RootID); err != nil || closed.RootID != "" || closed.Poured() {
		t.Fatalf("expected a closed molecule to be none, got %+v: %v", closed, err)
	}

	// And what it writes when it has landed, read back the way mw reads it.
	if err := gateway.SetStoryState(ctx, storyID, application.RunState, application.RunLanded, "landed by the test"); err != nil {
		t.Fatalf("recording the run state of %s: %v", storyID, err)
	}
	state, err := gateway.StoryState(ctx, storyID, application.RunState)
	if err != nil || state != application.RunLanded {
		t.Fatalf("expected %s=%s to be read back, got %q: %v", application.RunState, application.RunLanded, state, err)
	}
	unset, err := gateway.StoryState(ctx, storyID, "nothing-anybody-set")
	if err != nil || unset != "" {
		t.Fatalf("expected a dimension nobody set to read as nothing, got %q: %v", unset, err)
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

	// Taking a note back is how a sync that halted leaves no claim of having
	// been level; clearing one that is already gone is not a failure either.
	if err := gateway.ClearNote(ctx, key); err != nil {
		t.Fatalf("clearing %s: %v", key, err)
	}
	if cleared, err := gateway.Note(ctx, key); err != nil || cleared != "" {
		t.Fatalf("expected %s to be gone after clearing it, got %q: %v", key, cleared, err)
	}
	if err := gateway.ClearNote(ctx, key); err != nil {
		t.Fatalf("expected clearing a note that is not there to be no failure, got %v", err)
	}
}

// What Sweep remembers of a session between runs is a note in bd's key-value
// store, not state on the story: every `bd set-state` files a closed event bead
// under the story, and a sweep every few minutes would file two per pass. This
// shows the note round-trips against a real database and files nothing, where
// set-state files an event, and that the claim time Sweep counts its first
// silence from is read back off a claimed story.
func TestGatewayKeepsWhatSweepSawWithoutFilingEvents(t *testing.T) {
	vault := throwawayVault(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	epicID := bdRun(t, vault, beads.Program, "create", "A walking skeleton", "-t", "epic",
		"--metadata", `{"rig":"millwright","branch":"main","harness":"claude","model":"opus","effort":"high","formula":"tdd-feature","host":"vps"}`,
		"--silent")
	storyID := bdRun(t, vault, beads.Program, "create", "A story a sweep watches",
		"--parent", epicID, "--silent")

	gateway := beads.New(vault, beads.WithActor("mw@vps"))
	var _ application.SweepNotes = gateway

	before := time.Now().UTC().Add(-time.Minute)
	if err := gateway.ClaimStory(ctx, storyID); err != nil {
		t.Fatalf("claiming %s: %v", storyID, err)
	}
	running, err := gateway.RunningStories(ctx, "vps")
	if err != nil || len(running) != 1 {
		t.Fatalf("expected %s to be running on vps, got %+v: %v", storyID, running, err)
	}
	if started := running[0].Started; started.Before(before) || started.After(time.Now().UTC().Add(time.Minute)) {
		t.Fatalf("expected %s to have been claimed just now, bd says %v", storyID, started)
	}

	events := func() int {
		out := bdRun(t, vault, beads.Program, "list", "--parent", storyID, "--all", "--limit", "0", "--json")
		return strings.Count(out, `"issue_type": "event"`)
	}
	if got := events(); got != 0 {
		t.Fatalf("expected %s to have no event beads yet, got %d", storyID, got)
	}

	key := application.SweepKey(storyID)
	if seen, err := gateway.Note(ctx, key); err != nil || seen != "" {
		t.Fatalf("expected sweep to remember nothing of %s yet, got %q: %v", storyID, seen, err)
	}
	for _, note := range []string{"0123456789abcdef 1789000000", "fedcba9876543210 1789003600"} {
		if err := gateway.SetNote(ctx, key, note); err != nil {
			t.Fatalf("remembering %q of %s: %v", note, storyID, err)
		}
		if got, err := gateway.Note(ctx, key); err != nil || got != note {
			t.Fatalf("expected %s to be remembered as %q, got %q: %v", storyID, note, got, err)
		}
	}
	if got := events(); got != 0 {
		t.Fatalf("expected remembering what a sweep saw to file no event beads, got %d", got)
	}

	if err := gateway.ClearNote(ctx, key); err != nil {
		t.Fatalf("forgetting %s: %v", storyID, err)
	}
	if got, err := gateway.Note(ctx, key); err != nil || got != "" {
		t.Fatalf("expected sweep to remember nothing of %s after clearing it, got %q: %v", storyID, got, err)
	}

	// The one state sweep does write is an event worth keeping: that is what
	// set-state files, and what the note above spared the story twice over.
	if err := gateway.SetStoryState(ctx, storyID, application.RunState, application.RunStuck, "stuck, found by the test"); err != nil {
		t.Fatalf("recording %s stuck: %v", storyID, err)
	}
	if got := events(); got != 1 {
		t.Fatalf("expected recording run=stuck to file one event bead, got %d", got)
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

	// The whole epic read back is what `mw release` prints and releases from:
	// the epic's title and defaults, and its stories in the order they were
	// filed, each with what it waits on and what it is.
	whole, err := gateway.ShowEpic(ctx, epicID)
	if err != nil {
		t.Fatalf("reading the epic %s: %v", epicID, err)
	}
	if whole.ID != epicID || whole.Title != "Walking skeleton" || whole.Defaults != defaults {
		t.Errorf("expected the epic's id, title and defaults, got %+v", whole)
	}
	if len(whole.Stories) != 2 || whole.Stories[0].Story.ID != first || whole.Stories[1].Story.ID != second {
		t.Fatalf("expected %s then %s, got %+v", first, second, application.EpicDetail{Stories: whole.Stories}.Stories)
	}
	if !whole.Stories[0].Held() || !whole.Stories[1].Held() {
		t.Errorf("expected both stories to be read back held, got %q and %q",
			whole.Stories[0].Status, whole.Stories[1].Status)
	}
	if got := whole.Stories[0].Needs; len(got) != 0 {
		t.Errorf("expected %s to wait on nothing, got %v", first, got)
	}
	if got := whole.Stories[1].Needs; len(got) != 1 || got[0] != first {
		t.Errorf("expected %s to wait on %s, got %v", second, first, got)
	}
	if got := whole.Stories[1].Merged().Formula; got != "chore" {
		t.Errorf("expected the story's own formula over the epic's, got %q", got)
	}
	if _, err := gateway.ShowEpic(ctx, "t-nope"); err == nil {
		t.Error("expected reading an epic nobody filed to fail")
	}
	if _, err := gateway.ShowEpic(ctx, first); err == nil {
		t.Errorf("expected reading the story %s as an epic to fail", first)
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

	// The story waiting on it is released too, but not ready: it is blocked,
	// with the epic's defaults overlaid the same way ShowStory reads them.
	blocked, err := gateway.BlockedForHost(ctx, "vps")
	if err != nil {
		t.Fatalf("listing what is blocked on vps: %v", err)
	}
	if len(blocked) != 1 || blocked[0].Story.ID != second {
		t.Fatalf("expected only %s to be blocked, got %+v", second, blocked)
	}
	if got := blocked[0].Merged().Formula; got != "chore" {
		t.Errorf("expected the blocked story's own formula over the epic's, got %q", got)
	}
	if elsewhere, err := gateway.BlockedForHost(ctx, "laptop"); err != nil || len(elsewhere) != 0 {
		t.Fatalf("expected nothing blocked on laptop, got %+v: %v", elsewhere, err)
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
	if blocked, err := gateway.BlockedForHost(ctx, "vps"); err != nil || len(blocked) != 0 {
		t.Fatalf("expected nothing blocked once %s is closed, got %+v: %v", first, blocked, err)
	}
	// And it no longer says it waits on it: a wait on work that is finished is
	// not a wait, and bd hands the whole blocking bead over with its status, so
	// a story read on its own can say which of its waits are still waits.
	stillWaiting, err := gateway.ShowStory(ctx, second)
	if err != nil {
		t.Fatalf("reading %s back: %v", second, err)
	}
	if len(stillWaiting.Needs) != 0 {
		t.Errorf("expected %s to wait on nothing once %s is closed, got %v", second, first, stillWaiting.Needs)
	}

	// A story that is finished is still part of its epic: the tree a release
	// prints shows the work already done, so the reading asks bd for the closed
	// ones too, which its listing leaves out by default.
	whole, err = gateway.ShowEpic(ctx, epicID)
	if err != nil {
		t.Fatalf("reading the epic %s after the close: %v", epicID, err)
	}
	if len(whole.Stories) != 2 || whole.Stories[0].Story.ID != first {
		t.Fatalf("expected the closed story to still be part of the epic, got %+v", whole.Stories)
	}
	if !whole.Stories[0].Closed() {
		t.Errorf("expected %s to be read back closed, got %q", first, whole.Stories[0].Status)
	}

	// The listing reads only direct children, so an epic filed under the epic
	// comes back among them: marked as an epic, which is how a tree tells it from
	// a story, and readable in its own right.
	child := bdRun(t, vault, beads.Program, "create", "Later work", "-t", "epic", "--parent", epicID, "--silent")
	whole, err = gateway.ShowEpic(ctx, epicID)
	if err != nil {
		t.Fatalf("reading the epic %s with an epic under it: %v", epicID, err)
	}
	if len(whole.Stories) != 3 {
		t.Fatalf("expected the two stories and the child epic, got %+v", whole.Stories)
	}
	for _, detail := range whole.Stories {
		if got := detail.Story.ID == child; detail.IsEpic != got {
			t.Errorf("expected %s to be an epic: %v, got %v", detail.Story.ID, got, detail.IsEpic)
		}
	}
	if _, err := gateway.ShowEpic(ctx, child); err != nil {
		t.Errorf("expected the child epic %s to be readable as an epic: %v", child, err)
	}
}

// The dogfood failure of 2026-09-19, against a real bd: `mw dispatch` claimed a
// story under whatever name the shell that ran it carried, and the `mw next`
// chained onto the session ran with the Builder's BEADS_ACTOR, so bd refused the
// close — "assignee is root, actor is builder@vps". A gateway that names itself
// on every call claims and closes under one name whatever the environment says,
// and this test runs both halves the way Dispatch and Next run them.
func TestAStoryClaimedTheWayDispatchDoesIsClosedTheWayNextDoes(t *testing.T) {
	vault := throwawayVault(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// The environment of a Builder session: its own name, which is not the name
	// the dispatcher claimed under.
	t.Setenv("BEADS_ACTOR", "builder@vps")

	epicID := bdRun(t, vault, beads.Program, "create", "A walking skeleton", "-t", "epic",
		"--metadata", `{"rig":"millwright","branch":"main","harness":"claude","model":"opus","effort":"high","host":"vps"}`,
		"--silent")
	claimed := bdRun(t, vault, beads.Program, "create", "A story mw claims and closes",
		"--parent", epicID, "--silent")
	byHand := bdRun(t, vault, beads.Program, "create", "A story mw claims and somebody else tries to close",
		"--parent", epicID, "--silent")

	mw := beads.New(vault, beads.WithActor("mw@vps"))

	// What `mw dispatch` does.
	if err := mw.ClaimStory(ctx, claimed); err != nil {
		t.Fatalf("claiming %s the way a dispatch does: %v", claimed, err)
	}
	// What `mw next` does at the end of the session, in the session's own
	// environment.
	if err := mw.CloseStory(ctx, claimed, "landed on main, 3 commits"); err != nil {
		t.Fatalf("closing %s the way a close-out does: %v", claimed, err)
	}
	detail, err := mw.ShowStory(ctx, claimed)
	if err != nil {
		t.Fatalf("reading %s back: %v", claimed, err)
	}
	if !detail.Closed() {
		t.Fatalf("expected %s to be closed, got %q", claimed, detail.Status)
	}

	// And the refusal this story exists because of, still there: a gateway that
	// names nobody acts as whatever BEADS_ACTOR says, and bd will not let it
	// close what mw claimed. That is why the name is passed rather than left to
	// the environment — and it is the escape hatch too, the other way round: a
	// story claimed by hand as root is closed by hand with `bd --actor root`.
	if err := mw.ClaimStory(ctx, byHand); err != nil {
		t.Fatalf("claiming %s the way a dispatch does: %v", byHand, err)
	}
	err = beads.New(vault).CloseStory(ctx, byHand, "closed by somebody else")
	if err == nil {
		t.Fatalf("expected bd to refuse a close of %s by an actor that is not the assignee", byHand)
	}
	if !strings.Contains(err.Error(), "mw@vps") {
		t.Fatalf("expected the refusal to name the assignee mw@vps, got %q", err)
	}
	if err := beads.New(vault, beads.WithActor("mw@vps")).CloseStory(ctx, byHand, "closed under the same name"); err != nil {
		t.Fatalf("closing %s under the name it was claimed under: %v", byHand, err)
	}
}

// A story labelled hitl is one the Governor must be present for. The gateway
// has to tell that label on every way it reads a story that a dispatch reads:
// one story shown, an epic's listing, what is ready, and what is running.
func TestGatewayTellsAStorysLabels(t *testing.T) {
	vault := throwawayVault(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	epicID := bdRun(t, vault, beads.Program, "create", "An epic", "-t", "epic",
		"--metadata", `{"rig":"millwright","branch":"main","harness":"claude","model":"opus","effort":"high","host":"vps"}`,
		"--silent")
	// --no-inherit-labels: bd hands a parent's labels down to its children, and
	// what is wanted here is the labels a story was given itself.
	withLabel := bdRun(t, vault, beads.Program, "create", "With the Governor", "--parent", epicID,
		"--labels", "hitl,other", "--no-inherit-labels", "--silent")
	plain := bdRun(t, vault, beads.Program, "create", "On its own", "--parent", epicID,
		"--no-inherit-labels", "--silent")

	gateway := beads.New(vault)

	shown, err := gateway.ShowStory(ctx, withLabel)
	if err != nil {
		t.Fatalf("showing %s: %v", withLabel, err)
	}
	if got := append([]string(nil), shown.Labels...); !slicesEqual(sorted(got), []string{"hitl", "other"}) {
		t.Errorf("expected %s to carry the labels hitl and other, got %q", withLabel, got)
	}
	if !shown.Hitl() {
		t.Errorf("expected %s to be a story the Governor must be present for", withLabel)
	}
	if shown, err := gateway.ShowStory(ctx, plain); err != nil || len(shown.Labels) != 0 || shown.Hitl() {
		t.Errorf("expected %s to carry no labels, got %+v: %v", plain, shown, err)
	}

	epic, err := gateway.ShowEpic(ctx, epicID)
	if err != nil {
		t.Fatalf("showing the epic %s: %v", epicID, err)
	}
	if hitl := hitlIDs(epic.Stories); len(hitl) != 1 || hitl[0] != withLabel {
		t.Errorf("expected only %s in the epic to be hitl, got %q", withLabel, hitl)
	}

	ready, err := gateway.ReadyForHost(ctx, "vps")
	if err != nil {
		t.Fatalf("listing what is ready on vps: %v", err)
	}
	if len(ready) != 2 {
		t.Fatalf("expected both stories to be ready on vps, got %+v", ready)
	}
	if hitl := hitlIDs(ready); len(hitl) != 1 || hitl[0] != withLabel {
		t.Errorf("expected only %s among what is ready to be hitl, got %q", withLabel, hitl)
	}

	if err := gateway.ClaimStory(ctx, withLabel); err != nil {
		t.Fatalf("claiming %s: %v", withLabel, err)
	}
	running, err := gateway.RunningStories(ctx, "vps")
	if err != nil {
		t.Fatalf("listing what is running on vps: %v", err)
	}
	if len(running) != 1 || !running[0].Hitl() {
		t.Errorf("expected %s to be running on vps and hitl, got %+v", withLabel, running)
	}
}

// A bead labelled hitl that no epic gave a Path — the ticket a Mayor files for
// the Governor — is found by its label alone, if it is open and not blocked;
// one that is closed, blocked, claimed or unlabelled is not.
func TestGatewayListsWhatIsReadyUnderALabelWhateverItsPath(t *testing.T) {
	vault := throwawayVault(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	epicID := bdRun(t, vault, beads.Program, "create", "An epic", "-t", "epic",
		"--metadata", `{"rig":"millwright","branch":"main","harness":"claude","model":"opus","effort":"high","host":"laptop"}`,
		"--silent")
	pathless := bdRun(t, vault, beads.Program, "create", "Governor: turn on the mail notifier",
		"--labels", "hitl,wayfinder:task", "--priority", "1", "--silent")
	pathed := bdRun(t, vault, beads.Program, "create", "A story for the Governor", "--parent", epicID,
		"--labels", "hitl", "--no-inherit-labels", "--silent")
	closed := bdRun(t, vault, beads.Program, "create", "Already done", "--labels", "hitl", "--silent")
	bdRun(t, vault, beads.Program, "close", closed, "--reason", "done")
	blocker := bdRun(t, vault, beads.Program, "create", "The errand first", "--silent")
	blocked := bdRun(t, vault, beads.Program, "create", "The errand after", "--labels", "hitl", "--silent")
	bdRun(t, vault, beads.Program, "dep", "add", blocked, blocker)
	claimed := bdRun(t, vault, beads.Program, "create", "Being done now", "--labels", "hitl", "--silent")
	bdRun(t, vault, beads.Program, "update", claimed, "--claim")
	bdRun(t, vault, beads.Program, "create", "Not for the Governor", "--silent")

	found, err := beads.New(vault).ReadyWithLabel(ctx, application.LabelHitl)
	if err != nil {
		t.Fatalf("listing what is ready under hitl: %v", err)
	}
	var ids []string
	for _, story := range found {
		ids = append(ids, story.Story.ID)
	}
	if want := sorted([]string{pathless, pathed}); !slicesEqual(sorted(ids), want) {
		t.Fatalf("expected %q ready under hitl, got %q", want, ids)
	}
	for _, story := range found {
		switch story.Story.ID {
		case pathless:
			if got := story.Merged().Rig; got != "" || story.Priority != 1 {
				t.Errorf("expected %s to have no rig and priority 1, got rig %q priority %d", pathless, got, story.Priority)
			}
		case pathed:
			if got := story.Merged().Host; got != "laptop" {
				t.Errorf("expected %s to inherit the host laptop, got %q", pathed, got)
			}
		}
	}
}

func sorted(in []string) []string {
	sort.Strings(in)
	return in
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// hitlIDs is the ids among the stories that are hitl.
func hitlIDs(stories []application.StoryDetail) []string {
	var ids []string
	for _, story := range stories {
		if story.Hitl() {
			ids = append(ids, story.Story.ID)
		}
	}
	return ids
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

// A brief reads an epic's status and priority beside its stories, and the
// comments of any bead, whole and oldest first. Reading writes nothing.
func TestGatewayReadsAnEpicsStatusPriorityAndComments(t *testing.T) {
	vault := throwawayVault(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	epicID := bdRun(t, vault, beads.Program, "create", "A map", "-t", "epic", "-p", "1", "--silent")
	bdRun(t, vault, beads.Program, "create", "A child", "--parent", epicID, "--silent")
	bdRun(t, vault, beads.Program, "comments", "add", epicID, "The first.")
	bdRun(t, vault, beads.Program, "comments", "add", epicID, "The second,\n\nwhole.")

	gateway := beads.New(vault)
	epic, err := gateway.ShowEpic(ctx, epicID)
	if err != nil {
		t.Fatalf("reading the epic %s: %v", epicID, err)
	}
	if epic.Status != application.StatusOpen || epic.Priority != 1 || len(epic.Stories) != 1 {
		t.Errorf("expected an open epic at priority 1 with one story, got %+v", epic)
	}

	comments, err := gateway.StoryComments(ctx, epicID)
	if err != nil {
		t.Fatalf("reading the comments of %s: %v", epicID, err)
	}
	if len(comments) != 2 || comments[0].Text != "The first." || comments[1].Text != "The second,\n\nwhole." {
		t.Errorf("expected the two comments oldest first and whole, got %+v", comments)
	}

	if _, err := gateway.StoryComments(ctx, "no-such-bead"); err == nil {
		t.Error("expected the comments of a bead that does not exist to be an error")
	}
}
