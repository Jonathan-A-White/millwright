package application_test

import (
	"context"
	"errors"
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
		"the health check":       "curl -fsS -m 20 'https://postern.example.org/api/healthz' && /home/j/.local/bin/mw postern inbox --unread-count",
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
