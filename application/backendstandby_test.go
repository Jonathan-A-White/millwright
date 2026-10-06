package application_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// fakeShip is the way a staged binary is copied to another host, held in memory.
type fakeShip struct {
	mu      sync.Mutex
	Err     error
	Shipped []fakeShipped
}

type fakeShipped struct{ Host, Src, Dest string }

func (f *fakeShip) Ship(_ context.Context, host, src, dest string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Shipped = append(f.Shipped, fakeShipped{host, src, dest})
	return f.Err
}

// aBackendRigWithStandby is aBackendRig whose rig also names the VPS standby.
func aBackendRigWithStandby(t *testing.T, host, home string) (backendRig, *fakeShip) {
	t.Helper()
	r := aBackendRig(t, host, home)
	ship := &fakeShip{}
	cfg := r.stage.Settings["postern"]
	cfg.VPSHost = "vps"
	cfg.VPSStage = "/var/lib/postern/stage"
	cfg.VPSLive = "/usr/local/bin/postern"
	cfg.VPSService = "postern"
	cfg.VPSHealth = "http://vps.mw:8787/healthz"
	r.stage.Settings["postern"] = cfg
	r.stage.Ship = ship
	return r, ship
}

// A landing that changed the backend, on a rig that names the VPS standby, also
// copies the built binary to the standby's stage and writes the standby's swap as
// a root hands step on a bead of its own, which is carded like the home's.
func TestALandingStagesTheStandbysSwapAndCardsItWhenAVPSBackendIsConfigured(t *testing.T) {
	r, ship := aBackendRigWithStandby(t, "laptop", "laptop")

	notes := r.stage.Landed(context.Background(), presenceLanding())

	if len(r.builds.Built) != 1 {
		t.Fatalf("expected the backend built once, got %+v", r.builds.Built)
	}
	wantDest := "/var/lib/postern/stage/postern-4c9db71"
	if len(ship.Shipped) != 1 || ship.Shipped[0] != (fakeShipped{"vps", "/home/j/.local/share/postern/postern-4c9db71", wantDest}) {
		t.Fatalf("expected the built binary shipped to the standby's stage, got %+v (notes %q)", ship.Shipped, notes)
	}

	filed := filedUnder(t, r, "mw-j0f2d")
	if len(filed) != 2 {
		t.Fatalf("expected the home's bead and the standby's under the epic, got %d: %+v", len(filed), filed)
	}
	var standby application.StoryDetail
	for _, s := range filed {
		if strings.Contains(s.Story.Title, "VPS") {
			standby = s
		}
	}
	if standby.Story.ID == "" || !standby.Hitl() || standby.Status != application.StatusOpen {
		t.Fatalf("expected an open hitl bead for the VPS standby, got %+v", filed)
	}
	if !strings.Contains(standby.Story.Title, "4c9db71") {
		t.Errorf("expected the standby's bead to name the commit, got %q", standby.Story.Title)
	}

	steps := handsSteps(t, r.tracker, standby.Story.ID)
	if len(steps) != 1 {
		t.Fatalf("expected one hands step on %s, got %+v", standby.Story.ID, steps)
	}
	step := steps[0]
	if step.ID != "backend-4c9db71" || step.Host != "vps" || step.As != domain.HandsAsRoot {
		t.Fatalf("expected backend-4c9db71 on vps as root, got %+v", step.HandsStep)
	}
	for what, want := range map[string]string{
		"the standby's live path":    "l='/usr/local/bin/postern'",
		"the staged copy":            "n='/var/lib/postern/stage/postern-4c9db71'",
		"the restart (system unit)":  "systemctl restart 'postern'",
		"the standby's health check": "curl -fsS -m 20 'http://vps.mw:8787/healthz'; then ok=1",
		"the roll back":              "putting the old backend back",
	} {
		if !strings.Contains(step.Run, want) {
			t.Errorf("expected the step to hold %s (%q):\n%s", what, want, step.Run)
		}
	}
	if strings.Contains(step.Run, "--user") || strings.Contains(step.WayBack, "--user") {
		t.Errorf("the standby's unit is a system unit: no --user in\n%s\n%s", step.Run, step.WayBack)
	}

	// Both beads are carded: one message on each channel.
	sent := r.sender.Sent()
	threads := map[string]bool{}
	for _, m := range sent {
		threads[m.Thread] = true
	}
	if len(sent) != 2 || len(threads) != 2 || !threads[standby.Story.ID] {
		t.Fatalf("expected one message on each of the two beads' channels, got %+v", sent)
	}
	if got, _ := r.tracker.NotesWithPrefix(context.Background(), application.BackendPendingPrefix); len(got) != 0 {
		t.Fatalf("expected nothing left pending, got %v", got)
	}
}

// The standby's swap is a tap, never the home's tick: a root step needs the
// Governor's approval, so no staged-swap note is kept for the standby's bead.
func TestTheStandbysSwapIsNotLeftToTheHomesTick(t *testing.T) {
	r, _ := aBackendRigWithStandby(t, "laptop", "laptop")
	r.stage.Landed(context.Background(), presenceLanding())

	notes, err := r.tracker.NotesWithPrefix(context.Background(), application.BackendSwapPrefix)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) > 1 {
		t.Fatalf("expected at most the home's own staged swap, got %v", notes)
	}
	for _, text := range notes {
		if strings.Contains(text, "vps") {
			t.Errorf("the standby's swap must stay a hands step: %s", text)
		}
	}
}

// A rig that names no VPS backend stages the home's alone, as it always did.
func TestARigWithNoVPSBackendStagesOnlyTheHomes(t *testing.T) {
	r := aBackendRig(t, "laptop", "laptop")
	ship := &fakeShip{}
	r.stage.Ship = ship

	r.stage.Landed(context.Background(), presenceLanding())

	if len(ship.Shipped) != 0 || len(filedUnder(t, r, "mw-j0f2d")) != 1 {
		t.Fatalf("expected nothing shipped and one bead, got %+v and %d beads", ship.Shipped, len(filedUnder(t, r, "mw-j0f2d")))
	}
}

// A copy that fails is a failed try like a failed build: the landing is kept
// pending, the next tick tries again, and the beads already filed are reused
// rather than filed twice.
func TestAFailedShipIsTriedAgainWithoutFilingASecondBead(t *testing.T) {
	r, ship := aBackendRigWithStandby(t, "laptop", "laptop")
	ship.Err = errors.New("ssh: connection refused")

	notes := r.stage.Landed(context.Background(), presenceLanding())
	if !strings.Contains(strings.Join(notes, "\n"), "not staged (try 1 of 3)") {
		t.Fatalf("expected the failed try said, got %q", notes)
	}
	if got, _ := r.tracker.NotesWithPrefix(context.Background(), application.BackendPendingPrefix); len(got) != 1 {
		t.Fatalf("expected the landing kept pending, got %v", got)
	}

	ship.Err = nil
	r.stage.Pending(context.Background())

	if got := len(filedUnder(t, r, "mw-j0f2d")); got != 2 {
		t.Fatalf("expected one bead for the home and one for the standby after the retry, got %d", got)
	}
	if got, _ := r.tracker.NotesWithPrefix(context.Background(), application.BackendPendingPrefix); len(got) != 0 {
		t.Fatalf("expected nothing left pending, got %v", got)
	}
}

// A VPS backend with no way to copy to it is not staged half-way: the home's
// swap still is, and the standby's is said to be left alone.
func TestAVPSBackendWithNoShipperIsLeftOutAndSaid(t *testing.T) {
	r, _ := aBackendRigWithStandby(t, "laptop", "laptop")
	r.stage.Ship = nil

	notes := r.stage.Landed(context.Background(), presenceLanding())

	if got := len(filedUnder(t, r, "mw-j0f2d")); got != 1 {
		t.Fatalf("expected only the home's bead, got %d", got)
	}
	if !strings.Contains(strings.Join(notes, "\n"), "standby") {
		t.Errorf("expected the notes to say the standby was left, got %q", notes)
	}
}

// fakeCommits answers each healthz URL with a commit, or an error.
type fakeCommits map[string]string

func (f fakeCommits) Commit(_ context.Context, url string) (string, error) {
	if c, ok := f[url]; ok {
		return c, nil
	}
	return "", errors.New("connection refused")
}

func readStandby(t *testing.T, commits fakeCommits) application.StandbyReading {
	t.Helper()
	return application.Standby{
		Home:       &apptest.FakeHomeFile{Text: laptopHome},
		Commits:    commits,
		HomeURL:    func(host string) string { return "http://" + host + ".mw:8787/healthz" },
		StandbyURL: "http://vps.mw:8787/healthz",
	}.Read(context.Background())
}

func TestAStandbyThatReportsNoCommitIsBehindTheHome(t *testing.T) {
	got := readStandby(t, fakeCommits{"http://laptop.mw:8787/healthz": "141f1d3", "http://vps.mw:8787/healthz": ""})
	if want := "standby behind: none vs 141f1d3"; got.Line() != want {
		t.Errorf("line = %q, want %q", got.Line(), want)
	}
}

func TestAStandbyOnAnOlderCommitIsBehind(t *testing.T) {
	got := readStandby(t, fakeCommits{"http://laptop.mw:8787/healthz": "141f1d3", "http://vps.mw:8787/healthz": "8248967"})
	if want := "standby behind: 8248967 vs 141f1d3"; got.Line() != want {
		t.Errorf("line = %q, want %q", got.Line(), want)
	}
}

func TestAStandbyOnTheHomesCommitIsLevel(t *testing.T) {
	got := readStandby(t, fakeCommits{"http://laptop.mw:8787/healthz": "141f1d3", "http://vps.mw:8787/healthz": "141f1d3"})
	if want := "standby level at 141f1d3"; got.Line() != want {
		t.Errorf("line = %q, want %q", got.Line(), want)
	}
}

func TestAStandbyThatCannotBeReadIsNotChecked(t *testing.T) {
	got := readStandby(t, fakeCommits{"http://laptop.mw:8787/healthz": "141f1d3"})
	if !strings.HasPrefix(got.Line(), "standby not checked (") {
		t.Errorf("line = %q, want a not-checked line", got.Line())
	}
}

func TestMwStatusPrintsTheStandbyBehindLine(t *testing.T) {
	var out strings.Builder
	tracker := &apptest.FakeTracker{}
	status := application.Status{
		Tracker: tracker, Notes: tracker, Host: "laptop", Seat: "builder", Out: &out,
		Standby: application.Standby{
			Home:       &apptest.FakeHomeFile{Text: laptopHome},
			Commits:    fakeCommits{"http://laptop.mw:8787/healthz": "141f1d3", "http://vps.mw:8787/healthz": "8248967"},
			HomeURL:    func(host string) string { return "http://" + host + ".mw:8787/healthz" },
			StandbyURL: "http://vps.mw:8787/healthz",
		},
	}
	if _, err := status.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "standby behind: 8248967 vs 141f1d3\n") {
		t.Errorf("status did not print the line:\n%s", out.String())
	}
}
