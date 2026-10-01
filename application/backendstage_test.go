package application_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// fakeBuilds is the backend's git and build, held in memory: Changes says what a
// rig's story touches and every Build is kept, with the command it was given. It
// has no way to restart, install or reach a live binary: the port it satisfies
// does not offer one.
type fakeBuilds struct {
	mu       sync.Mutex
	Changes  bool
	ChangeEr error
	BuildErr error
	Built    []fakeBuild
	Asked    []string
}

type fakeBuild struct{ RigDir, Commit, Subdir, Command, Out string }

func (f *fakeBuilds) Changed(_ context.Context, rigDir, base, branch, subdir string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Asked = append(f.Asked, rigDir+" "+base+"..."+branch+" "+subdir)
	return f.Changes, f.ChangeEr
}

func (f *fakeBuilds) Build(_ context.Context, rigDir, commit, subdir, command, out string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Built = append(f.Built, fakeBuild{rigDir, commit, subdir, command, out})
	return f.BuildErr
}

const landedCommit = "4c9db71e0f7a5b3c2d1e8f9a0b1c2d3e4f5a6b7c"

type backendRig struct {
	stage   application.BackendStage
	tracker *apptest.FakeTracker
	builds  *fakeBuilds
	sender  *apptest.FakePosternSender
}

func aBackendRig(t *testing.T, host, home string) backendRig {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-j0f2d", domain.Path{})
	tracker.AddStory("mw-j0f2d", domain.Story{ID: "mw-j0f2d.28", Title: "Show the Mayor's presence"})
	builds := &fakeBuilds{Changes: true}
	sender := &apptest.FakePosternSender{}
	return backendRig{
		tracker: tracker,
		builds:  builds,
		sender:  sender,
		stage: application.BackendStage{
			Rigs: map[string]string{"postern": "/rigs/postern"},
			Settings: map[string]application.BackendRig{"postern": {
				Dir:     "server",
				Build:   "go build -o {out} ./cmd/postern",
				Stage:   "/home/j/.local/share/postern",
				Live:    "/home/j/.local/bin/postern",
				Service: "postern-backend",
				Health:  "https://postern.example.org/api/healthz",
				Check:   "/home/j/.local/bin/mw postern inbox --unread-count",
			}},
			Builds:  builds,
			Home:    &apptest.FakeHomeFile{Text: home + " 2026-10-01T05:00:00Z mayor\n"},
			Host:    host,
			Tracker: tracker,
			Notes:   tracker,
			Hands: application.HandsAdd{
				Tracker: tracker, Notes: tracker, Push: sender,
				Now: func() time.Time { return handsNow },
			},
		},
	}
}

func presenceLanding() application.BackendLanding {
	return application.BackendLanding{Rig: "postern", Story: "mw-j0f2d.28", Title: "Show the Mayor's presence", Epic: "mw-j0f2d", Commit: landedCommit}
}

// The one open hitl bead filed under the epic, by whatever title.
func filedUnder(t *testing.T, r backendRig, epic string) []application.StoryDetail {
	t.Helper()
	detail, err := r.tracker.ShowEpic(context.Background(), epic)
	if err != nil {
		t.Fatalf("reading the epic: %v", err)
	}
	var filed []application.StoryDetail
	for _, s := range detail.Stories {
		if s.Story.ID != "mw-j0f2d.28" {
			filed = append(filed, s)
		}
	}
	return filed
}

// A landing that changed server/ on the home stages the binary where the story
// says and writes the swap as a hands step on a new hitl bead under the epic.
func TestALandingThatChangedTheServerStagesTheBinaryAndWritesTheSwapAsAHandsStep(t *testing.T) {
	r := aBackendRig(t, "laptop", "laptop")

	notes := r.stage.Landed(context.Background(), presenceLanding())

	if len(r.builds.Built) != 1 {
		t.Fatalf("expected one build, got %+v (notes %q)", r.builds.Built, notes)
	}
	built := r.builds.Built[0]
	wantOut := "/home/j/.local/share/postern/postern-4c9db71"
	if built.RigDir != "/rigs/postern" || built.Commit != landedCommit || built.Subdir != "server" || built.Out != wantOut {
		t.Fatalf("expected the landed commit built in server/ into %s, got %+v", wantOut, built)
	}
	if built.Command != "go build -o {out} ./cmd/postern" {
		t.Fatalf("expected the rig's build command, placeholder and all, got %q", built.Command)
	}

	filed := filedUnder(t, r, "mw-j0f2d")
	if len(filed) != 1 {
		t.Fatalf("expected one new bead under the epic, got %+v", filed)
	}
	bead := filed[0]
	if !bead.Hitl() || bead.Status != application.StatusOpen {
		t.Fatalf("expected an open hitl bead, got status %q labels %v", bead.Status, bead.Labels)
	}
	for _, want := range []string{"4c9db71", "mw-j0f2d.28"} {
		if !strings.Contains(bead.Story.Title+bead.Description, want) {
			t.Errorf("expected the bead to name %s, got %q / %q", want, bead.Story.Title, bead.Description)
		}
	}

	steps := handsSteps(t, r.tracker, bead.Story.ID)
	if len(steps) != 1 {
		t.Fatalf("expected one hands step on %s, got %+v", bead.Story.ID, steps)
	}
	step := steps[0]
	if step.ID != "backend-4c9db71" || step.Host != "laptop" || step.As != domain.HandsAsUser {
		t.Fatalf("expected backend-4c9db71 on laptop as user, got %+v", step.HandsStep)
	}
	for what, want := range map[string]string{
		"the live path":          "l='/home/j/.local/bin/postern'",
		"the staged path":        "n='/home/j/.local/share/postern/postern-4c9db71'",
		"the already-live check": `if cmp -s "$l" "$n"`,
		"the one backup":         `[ -e "$b" ] || cp -p`,
		"the install":            "install -m 755",
		"the restart":            "systemctl --user restart 'postern-backend'",
		"the health check":       "curl -fsS -m 20 'https://postern.example.org/api/healthz'; then ok=1",
		"the four tries":         "for i in 1 2 3 4",
		"the roll back":          "putting the old backend back",
	} {
		if !strings.Contains(step.Run, want) {
			t.Errorf("expected the step to hold %s (%q):\n%s", what, want, step.Run)
		}
	}
	if !strings.Contains(step.WayBack, "install -m 755") || !strings.Contains(step.WayBack, "systemctl --user restart 'postern-backend'") {
		t.Errorf("expected a way back that puts the old backend back, got %q", step.WayBack)
	}

	sent := r.sender.Sent()
	if len(sent) != 1 || sent[0].Thread != bead.Story.ID || sent[0].Class != application.HandsPushClass {
		t.Fatalf("expected one message on %s's channel, got %+v", bead.Story.ID, sent)
	}
	if got, _ := r.tracker.NotesWithPrefix(context.Background(), application.BackendPendingPrefix); len(got) != 0 {
		t.Fatalf("expected nothing left pending, got %v", got)
	}
}

// A landing whose diff holds nothing under server/ asks for nothing: no build,
// no bead, no step, no message.
func TestALandingThatDidNotChangeTheServerDoesNothingNew(t *testing.T) {
	r := aBackendRig(t, "laptop", "laptop")
	r.builds.Changes = false

	touched, err := r.stage.Touches(context.Background(), "postern", "origin/main", "mw/mw-j0f2d.28")
	if err != nil || touched {
		t.Fatalf("expected no server change, got %v, %v", touched, err)
	}
	if len(r.builds.Asked) != 1 || !strings.Contains(r.builds.Asked[0], "/rigs/postern origin/main...mw/mw-j0f2d.28 server") {
		t.Fatalf("expected the story's own changes under server asked, got %q", r.builds.Asked)
	}
	if len(r.builds.Built) != 0 || len(filedUnder(t, r, "mw-j0f2d")) != 0 || len(r.sender.Sent()) != 0 {
		t.Fatal("expected nothing built, filed or sent")
	}
}

// A rig the host names no backend for is not looked at, and a landing on it
// changes nothing.
func TestARigWithoutABackendIsNotLookedAt(t *testing.T) {
	r := aBackendRig(t, "laptop", "laptop")
	if r.stage.Wants("millwright") || !r.stage.Wants("postern") {
		t.Fatal("expected only postern to be wanted")
	}
	touched, err := r.stage.Touches(context.Background(), "millwright", "origin/main", "mw/x")
	if err != nil || touched || len(r.builds.Asked) != 0 {
		t.Fatalf("expected an unconfigured rig not asked about, got %v %v %q", touched, err, r.builds.Asked)
	}
	if notes := r.stage.Landed(context.Background(), application.BackendLanding{Rig: "millwright", Epic: "mw-j0f2d", Commit: landedCommit}); len(notes) != 0 || len(r.builds.Built) != 0 {
		t.Fatalf("expected nothing for an unconfigured rig, got %q", notes)
	}
}

// On a host that is not home the landing builds nothing: it leaves a note for
// the home's next tick, which then does exactly what a landing on the home
// does, and clears the note.
func TestALandingOnTheBoostAsksTheHomeToStageItAndTheHomesTickDoesIt(t *testing.T) {
	boost := aBackendRig(t, "desktop", "laptop")
	notes := boost.stage.Landed(context.Background(), presenceLanding())
	if len(boost.builds.Built) != 0 || len(filedUnder(t, boost, "mw-j0f2d")) != 0 {
		t.Fatal("expected the boost to build and file nothing")
	}
	pending, _ := boost.tracker.NotesWithPrefix(context.Background(), application.BackendPendingPrefix)
	if len(pending) != 1 || len(notes) != 1 || !strings.Contains(notes[0], "laptop") {
		t.Fatalf("expected one pending note and a line naming the home, got %v / %q", pending, notes)
	}

	// The home reads the same notes and the same epic.
	home := boost
	home.stage.Host = "laptop"
	if said := home.stage.Pending(context.Background()); len(said) == 0 {
		t.Fatal("expected the home's tick to say it staged the backend")
	}
	if len(home.builds.Built) != 1 || home.builds.Built[0].Commit != landedCommit {
		t.Fatalf("expected the landed commit built, got %+v", home.builds.Built)
	}
	if filed := filedUnder(t, home, "mw-j0f2d"); len(filed) != 1 || len(handsSteps(t, home.tracker, filed[0].Story.ID)) != 1 {
		t.Fatalf("expected one bead with one step, got %+v", filed)
	}
	if left, _ := home.tracker.NotesWithPrefix(context.Background(), application.BackendPendingPrefix); len(left) != 0 {
		t.Fatalf("expected the pending note cleared, got %v", left)
	}

	// And a second tick has nothing to do.
	if said := home.stage.Pending(context.Background()); len(said) != 0 || len(home.builds.Built) != 1 {
		t.Fatalf("expected nothing more, got %q and %d builds", said, len(home.builds.Built))
	}
}

// A tick on a host that is not home leaves the notes where they are.
func TestAPendingNoteIsLeftAloneOnAHostThatIsNotHome(t *testing.T) {
	boost := aBackendRig(t, "desktop", "laptop")
	boost.stage.Landed(context.Background(), presenceLanding())
	if said := boost.stage.Pending(context.Background()); len(said) != 0 || len(boost.builds.Built) != 0 {
		t.Fatalf("expected the boost to leave the note, got %q", said)
	}
	if left, _ := boost.tracker.NotesWithPrefix(context.Background(), application.BackendPendingPrefix); len(left) != 1 {
		t.Fatalf("expected the note kept, got %v", left)
	}
}

// A build that fails files nothing and is tried again by the next tick; after
// three tries it is given up on, said on the landed story, and not tried again.
func TestAFailedBuildIsTriedAgainAndGivenUpAfterThreeTries(t *testing.T) {
	r := aBackendRig(t, "laptop", "laptop")
	r.builds.BuildErr = errors.New("go: no such command")

	notes := r.stage.Landed(context.Background(), presenceLanding())
	if len(notes) == 0 || !strings.Contains(strings.Join(notes, "\n"), "no such command") {
		t.Fatalf("expected the failure said, got %q", notes)
	}
	if len(filedUnder(t, r, "mw-j0f2d")) != 0 {
		t.Fatal("expected no bead for a binary that was never built")
	}
	r.stage.Pending(context.Background())
	r.stage.Pending(context.Background())
	if len(r.builds.Built) != 3 {
		t.Fatalf("expected three tries, got %d", len(r.builds.Built))
	}
	last := strings.Join(r.stage.Pending(context.Background()), "\n")
	if len(r.builds.Built) != 3 {
		t.Fatalf("expected no fourth try, got %d", len(r.builds.Built))
	}
	_ = last
	if comments := r.tracker.Comments("mw-j0f2d.28"); len(comments) == 0 || !strings.Contains(comments[len(comments)-1], "no such command") {
		t.Fatalf("expected the landed story told why, got %q", comments)
	}
}

// A step that could not be written after the bead was filed is written on that
// bead by the next try, not on a second one.
func TestARetryAfterTheBeadWasFiledDoesNotFileASecond(t *testing.T) {
	r := aBackendRig(t, "laptop", "laptop")
	r.stage.Hands = failingHands{}
	r.stage.Landed(context.Background(), presenceLanding())
	if len(filedUnder(t, r, "mw-j0f2d")) != 1 {
		t.Fatal("expected the bead filed")
	}
	r.stage.Hands = application.HandsAdd{Tracker: r.tracker, Notes: r.tracker, Now: func() time.Time { return handsNow }}
	r.stage.Pending(context.Background())
	filed := filedUnder(t, r, "mw-j0f2d")
	if len(filed) != 1 || len(handsSteps(t, r.tracker, filed[0].Story.ID)) != 1 {
		t.Fatalf("expected the one bead to hold the step, got %+v", filed)
	}
}

type failingHands struct{}

func (failingHands) Run(context.Context, application.HandsAddRequest) (application.HandsStepRecord, error) {
	return application.HandsStepRecord{}, errors.New("the tracker is busy")
}

// swapSandbox is a live binary and a stage in a temp directory, with a systemctl,
// curl and sleep on PATH that only record or succeed: the swap step runs for real
// in sh, and nothing of the host's is reached.
type swapSandbox struct {
	cfg     application.BackendRig
	bin     string
	logFile string
}

func aSwapSandbox(t *testing.T) swapSandbox {
	t.Helper()
	root := t.TempDir()
	box := swapSandbox{bin: filepath.Join(root, "fakebin"), logFile: filepath.Join(root, "systemctl.log")}
	for _, dir := range []string{box.bin, filepath.Join(root, "stage"), filepath.Join(root, "live")} {
		mustDo(t, os.MkdirAll(dir, 0o755))
	}
	for name, body := range map[string]string{
		"systemctl": "echo \"$@\" >> " + box.logFile,
		"curl":      "echo answered",
		"sleep":     "true",
	} {
		mustDo(t, os.WriteFile(filepath.Join(box.bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755))
	}
	box.cfg = application.BackendRig{
		Stage: filepath.Join(root, "stage"), Live: filepath.Join(root, "live", "postern"),
		Service: "postern-backend", Health: "http://127.0.0.1:1/api/healthz",
	}
	return box
}

// stageBinary leaves a staged binary named for short, built at when.
func (b swapSandbox) stageBinary(t *testing.T, short string, when time.Time) string {
	t.Helper()
	path := filepath.Join(b.cfg.Stage, "postern-"+short)
	mustDo(t, os.WriteFile(path, []byte("binary "+short+"\n"), 0o755))
	mustDo(t, os.Chtimes(path, when, when))
	return path
}

func (b swapSandbox) goLive(t *testing.T, short string) {
	t.Helper()
	mustDo(t, os.WriteFile(b.cfg.Live, []byte("binary "+short+"\n"), 0o755))
}

// tap runs the swap step of short as the runner does, in sh.
func (b swapSandbox) tap(t *testing.T, short string) (string, error) {
	t.Helper()
	step := application.BackendSwap(b.cfg, "laptop", short, filepath.Join(b.cfg.Stage, "postern-"+short))
	cmd := exec.Command("sh", "-c", step.Run)
	cmd.Env = append(os.Environ(), "PATH="+b.bin+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (b swapSandbox) restarts(t *testing.T) int {
	t.Helper()
	raw, err := os.ReadFile(b.logFile)
	if os.IsNotExist(err) {
		return 0
	}
	mustDo(t, err)
	return strings.Count(string(raw), "\n")
}

func (b swapSandbox) liveIs(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(b.cfg.Live)
	mustDo(t, err)
	return strings.TrimSpace(string(raw))
}

// A swap step whose binary was built before the one that is live installs nothing:
// it says which is newer and leaves the live binary and the service alone
// (mw-gq6.190: the two-day-old backend that ran two minutes after the new one).
func TestASwapStepWithAnOlderBinaryThanTheLiveOneChangesNothing(t *testing.T) {
	box := aSwapSandbox(t)
	box.stageBinary(t, "419ee98", time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC))
	box.stageBinary(t, "b563092", time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC))
	box.goLive(t, "b563092")

	out, err := box.tap(t, "419ee98")

	if err != nil {
		t.Fatalf("expected the stale tap to end cleanly, got %v: %s", err, out)
	}
	if !strings.Contains(out, "live backend b563092 is newer than 419ee98: nothing was changed") {
		t.Fatalf("expected the step to say the live backend is newer, got %q", out)
	}
	if got := box.liveIs(t); got != "binary b563092" {
		t.Fatalf("expected the live binary left alone, got %q", got)
	}
	if n := box.restarts(t); n != 0 {
		t.Fatalf("expected no restart, got %d", n)
	}
	if _, err := os.Stat(box.cfg.Live + ".prev-before-419ee98"); err == nil {
		t.Fatal("expected no backup taken by a step that changed nothing")
	}
}

// The same step with nothing newer live is still the swap: the guard only holds
// back a step that would go backwards.
func TestASwapStepWithANewerBinaryThanTheLiveOneStillSwaps(t *testing.T) {
	box := aSwapSandbox(t)
	box.stageBinary(t, "419ee98", time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC))
	box.stageBinary(t, "b563092", time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC))
	box.goLive(t, "419ee98")

	out, err := box.tap(t, "b563092")

	if err != nil || !strings.Contains(out, "backend b563092 is live and answering") {
		t.Fatalf("expected the swap to run, got %v: %s", err, out)
	}
	if got := box.liveIs(t); got != "binary b563092" {
		t.Fatalf("expected the staged binary installed, got %q", got)
	}
	if n := box.restarts(t); n != 1 {
		t.Fatalf("expected one restart, got %d", n)
	}
}

// A live binary nobody staged (built by hand) or a stage with nothing newer is not
// a reason to refuse.
func TestASwapStepIsNotHeldBackByAStagedBinaryThatIsNotLive(t *testing.T) {
	box := aSwapSandbox(t)
	box.stageBinary(t, "aaaaaaa", time.Date(2026, 10, 1, 14, 0, 0, 0, time.UTC))
	box.stageBinary(t, "419ee98", time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC))
	box.goLive(t, "something else")

	out, err := box.tap(t, "419ee98")

	if err != nil || !strings.Contains(out, "backend 419ee98 is live and answering") {
		t.Fatalf("expected the swap to run, got %v: %s", err, out)
	}
}

func swapLanding(story, commit string) application.BackendLanding {
	return application.BackendLanding{Rig: "postern", Story: story, Title: "Story " + story, Epic: "mw-j0f2d", Commit: commit}
}

// beadOfSwap is the bead whose hands step is backend-<short>.
func beadOfSwap(t *testing.T, tracker *apptest.FakeTracker, short string) string {
	t.Helper()
	notes, err := tracker.NotesWithPrefix(context.Background(), "hands.")
	mustDo(t, err)
	for key, raw := range notes {
		if strings.HasPrefix(key, "hands.ran.") || strings.HasPrefix(key, "hands.superseded.") {
			continue
		}
		if strings.Contains(raw, `"id":"backend-`+short+`"`) {
			return strings.TrimPrefix(key, "hands.")
		}
	}
	t.Fatalf("no bead holds the step backend-%s", short)
	return ""
}

func supersededBy(t *testing.T, tracker *apptest.FakeTracker, bead, step string) string {
	t.Helper()
	got, err := tracker.Note(context.Background(), application.HandsSupersededKey(bead, step))
	mustDo(t, err)
	return strings.TrimSpace(got)
}

// Writing a newer swap step of a rig marks the swap steps written before it, that
// have not run, as superseded by the bead that holds the new one: they can no
// longer be tapped, and mw hands list says so.
func TestANewerSwapStepSupersedesTheOlderOnesOfTheSameRig(t *testing.T) {
	r := aBackendRig(t, "laptop", "laptop")
	r.tracker.AddStory("mw-j0f2d", domain.Story{ID: "mw-j0f2d.29", Title: "Another story"})
	ctx := context.Background()

	r.stage.Landed(ctx, swapLanding("mw-j0f2d.28", "419ee98aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	older := beadOfSwap(t, r.tracker, "419ee98")
	if got := supersededBy(t, r.tracker, older, "backend-419ee98"); got != "" {
		t.Fatalf("expected the first swap superseded by nothing, got %q", got)
	}

	r.stage.Landed(ctx, swapLanding("mw-j0f2d.29", "b563092bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))
	newer := beadOfSwap(t, r.tracker, "b563092")

	if got := supersededBy(t, r.tracker, older, "backend-419ee98"); got != newer {
		t.Fatalf("expected the older swap superseded by %s, got %q", newer, got)
	}
	if got := supersededBy(t, r.tracker, newer, "backend-b563092"); got != "" {
		t.Fatalf("expected the newer swap not superseded, got %q", got)
	}

	var out bytes.Buffer
	_, err := application.HandsList{Notes: r.tracker, Out: &out}.Run(ctx, older)
	mustDo(t, err)
	if !strings.Contains(out.String(), "superseded by "+newer) {
		t.Fatalf("expected the list to say superseded by %s, got:\n%s", newer, out.String())
	}
	out.Reset()
	_, err = application.HandsList{Notes: r.tracker, Out: &out}.Run(ctx, newer)
	mustDo(t, err)
	if strings.Contains(out.String(), "superseded") {
		t.Fatalf("expected the newer step's list not to say superseded, got:\n%s", out.String())
	}
}

// A swap step that already ran clean is history, and another rig's, or any other
// hands step, is not a swap of this one: none of them is superseded.
func TestANewerSwapStepLeavesRanAndForeignStepsAlone(t *testing.T) {
	r := aBackendRig(t, "laptop", "laptop")
	r.tracker.AddStory("mw-j0f2d", domain.Story{ID: "mw-j0f2d.29", Title: "Another story"})
	ctx := context.Background()

	r.stage.Landed(ctx, swapLanding("mw-j0f2d.28", "419ee98aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	ranBead := beadOfSwap(t, r.tracker, "419ee98")
	mustDo(t, r.tracker.SetNote(ctx, application.HandsRanKey(ranBead, "backend-419ee98"), `{"at":"2026-09-30T12:03:00Z","exit":0,"host":"laptop"}`))

	foreign := application.BackendSwap(application.BackendRig{Live: "/home/j/.local/bin/other", Stage: "/s", Service: "other", Health: "http://x"}, "laptop", "1111111", "/s/other-1111111")
	_, err := application.HandsAdd{Tracker: r.tracker, Notes: r.tracker, Now: func() time.Time { return handsNow }}.Run(ctx, application.HandsAddRequest{Bead: "mw-j0f2d.28", Step: foreign})
	mustDo(t, err)

	r.stage.Landed(ctx, swapLanding("mw-j0f2d.29", "b563092bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))

	if got := supersededBy(t, r.tracker, ranBead, "backend-419ee98"); got != "" {
		t.Errorf("expected a swap that already ran left alone, got superseded by %q", got)
	}
	if got := supersededBy(t, r.tracker, "mw-j0f2d.28", "backend-1111111"); got != "" {
		t.Errorf("expected another rig's swap left alone, got superseded by %q", got)
	}
}
