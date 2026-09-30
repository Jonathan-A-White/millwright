package application_test

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// The clock of every move here, and the times the machines report.
var (
	moveAt     = time.Date(2026, 9, 29, 14, 0, 0, 0, time.UTC)
	backupAt   = time.Date(2026, 9, 29, 13, 37, 19, 0, time.UTC)
	postern    = time.Date(2026, 9, 29, 13, 52, 0, 0, time.UTC)
	vaultDir   = "/home/jwhite/millwright-vault"
	asideMoved = application.AsideMove{
		From: vaultDir + "/.beads/embeddeddolt",
		To:   "/home/jwhite/beads-embeddeddolt-aside-20260929T140000Z",
	}
	// doltMoved is the directory dolt-beads serves, set aside beside the other one.
	doltMoved = application.AsideMove{
		From: vaultDir + "/.beads/dolt",
		To:   "/home/jwhite/beads-dolt-aside-20260929T140000Z",
	}
)

// moveWorld is every machine a move reaches — ssh, GitHub, bd, git, systemctl,
// the backend, the vault's bin/mayor-up — as one stand-in that writes the command
// it stands for into calls, so a test reads what ran, in order.
type moveWorld struct {
	*apptest.FakeVaultFiles
	*apptest.FakeMailbox
	calls []string

	// What the machines answer.
	oldHomeUp   bool
	hasEmbedded bool
	// hasDolt is a .beads/dolt on this host, the directory dolt-beads serves.
	hasDolt   bool
	installed map[string]bool
	running   map[string]bool
	beads     int
	// oldBeads is what bd counts on the old home, in a planned move.
	oldBeads     int
	mayorStarted bool
	hasIndex     bool
	// The old home, when the move is planned: whether its Mayor hands off, and the
	// units it has.
	mayorStays   bool
	handoffMail  string
	oldInstalled map[string]bool
	// failAt is a call that fails, once it has been made and written down.
	failAt string
	// written is the last home file written.
	written domain.HomeRecord
}

func newMoveWorld() *moveWorld {
	return &moveWorld{
		FakeVaultFiles: &apptest.FakeVaultFiles{HeadSHA: "abc1234"},
		FakeMailbox:    apptest.NewFakeMailbox(),
		hasEmbedded:    true,
		installed:      map[string]bool{application.DoltBeadsUnit: true, application.PosternBackendUnit: true},
		running:        map[string]bool{},
		beads:          4422,
		oldBeads:       4422,
		mayorStarted:   true,
		hasIndex:       true,
		oldInstalled:   map[string]bool{application.DoltBeadsUnit: true, application.PosternBackendUnit: true},
	}
}

// did writes a call down and says whether it is the one told to fail.
func (w *moveWorld) did(call string) error {
	w.calls = append(w.calls, call)
	if call == w.failAt {
		return errors.New("stand-in: " + call + " failed")
	}
	return nil
}

func (w *moveWorld) OldHomeAnswers(_ context.Context, ssh []string, wait time.Duration) (bool, error) {
	if wait != 10*time.Second {
		return false, errors.New("the wait is 10 s")
	}
	return w.oldHomeUp, w.did(strings.Join(ssh, " "))
}

func (w *moveWorld) BackupTime(context.Context) (time.Time, error) {
	return backupAt, w.did("git: read refs/dolt/data time")
}

func (w *moveWorld) SetBeadsAside(_ context.Context, stamp string) (application.AsideMove, error) {
	if stamp != "20260929T140000Z" {
		return application.AsideMove{}, errors.New("the stamp is the move's time: " + stamp)
	}
	if err := w.did("aside .beads/embeddeddolt"); err != nil || !w.hasEmbedded {
		return application.AsideMove{}, err
	}
	return asideMoved, nil
}

// SetDoltAside writes a call down only when there is a .beads/dolt to set aside.
func (w *moveWorld) SetDoltAside(_ context.Context, stamp string) (application.AsideMove, error) {
	if stamp != "20260929T140000Z" {
		return application.AsideMove{}, errors.New("the stamp is the move's time: " + stamp)
	}
	if !w.hasDolt {
		return application.AsideMove{}, nil
	}
	return doltMoved, w.did("aside .beads/dolt")
}

func (w *moveWorld) BootstrapBeads(context.Context) error { return w.did("bd bootstrap --yes") }

func (w *moveWorld) RestoreBeadsConfig(context.Context) error {
	return w.did("git checkout -- .beads/config.yaml")
}

func (w *moveWorld) BeadsCount(context.Context) (int, error) {
	return w.beads, w.did("bd count")
}

func (w *moveWorld) UnitInstalled(_ context.Context, unit string) (bool, error) {
	return w.installed[unit], w.did("systemctl installed? " + unit)
}

func (w *moveWorld) StartUnit(_ context.Context, unit string) (bool, error) {
	started := !w.running[unit]
	w.running[unit] = true
	return started, w.did("systemctl --user start " + unit)
}

// StopUnit writes a call down only when it stops a unit that was running.
func (w *moveWorld) StopUnit(_ context.Context, unit string) (bool, error) {
	if !w.running[unit] {
		return false, nil
	}
	w.running[unit] = false
	return true, w.did("systemctl --user stop " + unit)
}

func (w *moveWorld) BackendServing(_ context.Context, url string, wait time.Duration) error {
	if wait != 45*time.Second {
		return errors.New("the wait is 45 s")
	}
	return w.did("healthz " + url)
}

func (w *moveWorld) MayorUp(context.Context) (bool, string, error) {
	return w.mayorStarted, "@12", w.did("bin/mayor-up")
}

func (w *moveWorld) WriteHome(_ context.Context, record domain.HomeRecord) error {
	w.written = record
	return w.did("write home")
}

func (w *moveWorld) Pull(context.Context) (int, error) { return 3, w.did("git pull") }
func (w *moveWorld) Push(context.Context) (int, error) { return 1, w.did("git push") }
func (w *moveWorld) Head(context.Context) (string, error) {
	return "abc1234", w.did("git rev-parse HEAD")
}
func (w *moveWorld) Commit(_ context.Context, message string, paths []string) ([]string, error) {
	if err := w.did("git commit " + strings.Join(paths, " ")); err != nil {
		return nil, err
	}
	if !strings.Contains(message, "laptop") {
		return nil, errors.New("the commit names the new home: " + message)
	}
	return paths, nil
}

func (w *moveWorld) Send(ctx context.Context, message application.NewMessage) (string, error) {
	if err := w.did("mail " + message.To); err != nil {
		return "", err
	}
	return w.FakeMailbox.Send(ctx, message)
}

func (w *moveWorld) LastIndexTime(_ context.Context, ssh []string, dir string) (time.Time, bool, error) {
	if len(ssh) != 0 || dir != "/home/jwhite/.local/state/postern" {
		return time.Time{}, false, errors.New("the index read is this host's own data")
	}
	return postern, w.hasIndex, w.did("postern index")
}

func (w *moveWorld) Take(context.Context) (func(), error) {
	w.calls = append(w.calls, "host lock taken")
	return func() { w.calls = append(w.calls, "host lock released") }, nil
}

// move is a move on the laptop of the desktop's home, the desktop said to be
// dead, with every machine the world.
func move(w *moveWorld, out *bytes.Buffer) application.HomeMove {
	return application.HomeMove{
		Files:       homeIs("desktop"),
		Writer:      w,
		Vault:       w,
		Machine:     w,
		Mailbox:     w,
		Index:       w,
		Lock:        w,
		Out:         out,
		Host:        "laptop",
		Target:      "laptop",
		Reach:       map[string]string{"desktop": "ssh desktop"},
		VaultDir:    vaultDir,
		DataDir:     "/home/jwhite/.local/state/postern",
		BackendURL:  "http://laptop.mw:8787",
		Actor:       "mw@laptop",
		BeadsSync:   application.BeadsSyncAuto,
		OldHomeDead: true,
		Now:         func() time.Time { return moveAt },
	}
}

func wantCalls(t *testing.T, w *moveWorld, want ...string) {
	t.Helper()
	if !reflect.DeepEqual(w.calls, want) {
		t.Errorf("what ran, in order:\n got  %q\n want %q", w.calls, want)
	}
}

func mustContain(t *testing.T, what, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("%s: expected %q in:\n%s", what, want, text)
		}
	}
}

func TestMoveRunsTheSixStepsInOrder(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}

	if err := move(w, out).Run(context.Background()); err != nil {
		t.Fatalf("move: %v\n%s", err, out)
	}

	wantCalls(t, w,
		// 1. the old home
		"ssh desktop",
		// 2. beads: GitHub's time first, then the swap under the lock
		"git: read refs/dolt/data time",
		"host lock taken",
		"aside .beads/embeddeddolt",
		"bd bootstrap --yes",
		"git checkout -- .beads/config.yaml",
		"bd count",
		"systemctl installed? dolt-beads",
		"systemctl --user start dolt-beads",
		"bd count",
		"host lock released",
		// 3. the vault
		"git pull",
		"write home",
		"git commit home",
		"git rev-parse HEAD",
		"git push",
		// 4. postern
		"systemctl installed? postern-backend",
		"systemctl --user start postern-backend",
		"healthz http://laptop.mw:8787",
		// 5. the Mayor
		"mail mayor",
		"bin/mayor-up",
		// 6. what was lost
		"postern index",
	)

	if want := (domain.HomeRecord{Host: "laptop", At: moveAt, By: "mw@laptop"}); w.written != want {
		t.Errorf("home file written: got %+v, want %+v", w.written, want)
	}
	mail, err := w.FakeMailbox.Inbox(context.Background(), "mayor")
	if err != nil || len(mail) != 1 {
		t.Fatalf("expected one message to the Mayor, got %v, %v", mail, err)
	}
	wantBody := "Home moved to laptop at 2026-09-29T14:00:00Z: the old home is dead; beads from GitHub as of 2026-09-29T13:37:19Z"
	if mail[0].Body != wantBody || mail[0].From != "mw@laptop" {
		t.Errorf("mail: got from %q, body %q; want body %q", mail[0].From, mail[0].Body, wantBody)
	}

	mustContain(t, "report", out.String(),
		"Step 1 of 6", "Step 2 of 6", "Step 3 of 6", "Step 4 of 6", "Step 5 of 6", "Step 6 of 6",
		"Way back:",
		asideMoved.To,
		"22 minutes", "8 minutes", "2026-09-29T13:37:19Z", "2026-09-29T13:52:00Z",
		"Home is now laptop")
}

func TestMoveStaysEmbeddedWhenThisHostHasNoDoltUnit(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	w.installed[application.DoltBeadsUnit] = false

	if err := move(w, out).Run(context.Background()); err != nil {
		t.Fatalf("move: %v\n%s", err, out)
	}

	for _, call := range w.calls {
		if call == "systemctl --user start dolt-beads" {
			t.Errorf("started a unit this host does not have")
		}
	}
	mustContain(t, "report", out.String(), "no dolt-beads unit here", "embedded", "backup")
}

func TestMoveLeavesARunningBackendRunning(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	w.running[application.PosternBackendUnit] = true

	if err := move(w, out).Run(context.Background()); err != nil {
		t.Fatalf("move: %v\n%s", err, out)
	}

	mustContain(t, "report", out.String(), "already running", "standby")
	if strings.Contains(out.String(), "systemctl --user stop postern-backend") {
		t.Errorf("gave 'stop the backend' as the way back of a unit the move did not start:\n%s", out)
	}
}

func TestMoveStartsThePosternBackendAndNeverServesIt(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	if err := move(w, out).Run(context.Background()); err != nil {
		t.Fatalf("move: %v\n%s", err, out)
	}
	mustContain(t, "report", out.String(), "systemctl --user stop postern-backend", "never mw postern serve")
}

func TestDryRunRunsNoneOfThem(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	m := move(w, out)
	m.DryRun = true

	if err := m.Run(context.Background()); err != nil {
		t.Fatalf("dry run: %v\n%s", err, out)
	}

	wantCalls(t, w)
	if inbox, _ := w.FakeMailbox.Inbox(context.Background(), "mayor"); len(inbox) != 0 {
		t.Errorf("a dry run sent mail: %v", inbox)
	}
	mustContain(t, "dry run", out.String(),
		"Dry run: nothing below is run",
		"Step 1 of 6", "Step 2 of 6", "Step 3 of 6", "Step 4 of 6", "Step 5 of 6", "Step 6 of 6",
		"ssh desktop", "--old-home-dead", "--planned", "refs/dolt/data", "bd bootstrap --yes", "dolt-beads",
		"postern-backend", "mw postern serve", "bin/mayor-up",
		"Way back:")
	if n := strings.Count(out.String(), "Way back:"); n != 6 {
		t.Errorf("expected a way back for each of the six steps, got %d:\n%s", n, out)
	}
}

func TestAnUnreachableOldHomeWithoutTheWordRefuses(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	m := move(w, out)
	m.OldHomeDead = false

	err := m.Run(context.Background())

	if err == nil || !strings.Contains(err.Error(), "--old-home-dead") || !strings.Contains(err.Error(), "did not answer") {
		t.Fatalf("expected a refusal naming --old-home-dead, got %v", err)
	}
	wantCalls(t, w, "ssh desktop")
	mustContain(t, "report", err.Error(), "Nothing was changed")
}

func TestAReachableOldHomeRefusesWithThePlannedPathLine(t *testing.T) {
	for _, dead := range []bool{false, true} {
		w, out := newMoveWorld(), &bytes.Buffer{}
		w.oldHomeUp = true
		m := move(w, out)
		m.OldHomeDead = dead

		err := m.Run(context.Background())

		if err == nil || !strings.Contains(err.Error(), "old home is up: use --planned") {
			t.Fatalf("with --old-home-dead=%v: expected the planned-path line, got %v", dead, err)
		}
		wantCalls(t, w, "ssh desktop")
	}
}

func TestAMoveToAnotherHostThanThisRefusesBeforeRunningAnything(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	m := move(w, out)
	m.Target = "desktop"

	err := m.Run(context.Background())

	if err == nil || !strings.Contains(err.Error(), "run it on desktop") {
		t.Fatalf("expected a refusal saying to run it on the host that becomes home, got %v", err)
	}
	wantCalls(t, w)
}

func TestAMoveToTheVPSRefuses(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	m := move(w, out)
	m.Host, m.Target = "vps", "vps"

	err := m.Run(context.Background())

	if err == nil || !strings.Contains(err.Error(), "desktop or laptop") {
		t.Fatalf("expected a refusal naming desktop and laptop, got %v", err)
	}
	wantCalls(t, w)
}

func TestAMoveToTheHostThatIsAlreadyHomeRefuses(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	m := move(w, out)
	m.Files = homeIs("laptop")

	err := m.Run(context.Background())

	if err == nil || !strings.Contains(err.Error(), "already home") {
		t.Fatalf("expected a refusal saying this host is already home, got %v", err)
	}
	wantCalls(t, w)
}

func TestAMoveWithNoHomeFileGoesOnAndTheOldHomeIsTheOtherHost(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	m := move(w, out)
	m.Files = &apptest.FakeHomeFile{Missing: true}

	if err := m.Run(context.Background()); err != nil {
		t.Fatalf("move: %v\n%s", err, out)
	}
	if w.calls[0] != "ssh desktop" || w.written.Host != "laptop" {
		t.Errorf("expected the desktop asked and the laptop written, got %q, %+v", w.calls, w.written)
	}
}

func TestAMoveWithNoWayToReachTheOldHomeRefuses(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	m := move(w, out)
	m.Reach = map[string]string{"vps": "ssh root@allmymind.org"}

	err := m.Run(context.Background())

	if err == nil || !strings.Contains(err.Error(), "[hands_hosts]") {
		t.Fatalf("expected a refusal naming [hands_hosts], got %v", err)
	}
	wantCalls(t, w)
}

func TestGitHubThatCannotBeReadStopsBeforeTheDatabaseIsTouched(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	w.failAt = "git: read refs/dolt/data time"

	err := move(w, out).Run(context.Background())

	if err == nil || !strings.Contains(err.Error(), "step 2") {
		t.Fatalf("expected the move to stop at step 2, got %v", err)
	}
	wantCalls(t, w, "ssh desktop", "git: read refs/dolt/data time")
	if strings.Contains(out.String(), "Ways back") {
		t.Errorf("nothing had changed, so no ways back should be printed:\n%s", out)
	}
	mustContain(t, "error", err.Error(), "Nothing was changed")
}

func TestABootstrapThatFailsStopsWithTheDatabaseSetAsideAndSaysHowToPutItBack(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	w.failAt = "bd bootstrap --yes"

	err := move(w, out).Run(context.Background())

	if err == nil || !strings.Contains(err.Error(), "step 2") || !strings.Contains(err.Error(), "bd bootstrap --yes failed") {
		t.Fatalf("expected the move to stop at step 2 with bd's failure, got %v", err)
	}
	wantCalls(t, w,
		"ssh desktop", "git: read refs/dolt/data time", "host lock taken",
		"aside .beads/embeddeddolt", "bd bootstrap --yes", "host lock released")
	mustContain(t, "ways back", out.String(), "Ways back", asideMoved.To, asideMoved.From)
}

func TestABootstrappedDatabaseWithNoBeadsInItStops(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	w.beads = 0

	err := move(w, out).Run(context.Background())

	if err == nil || !strings.Contains(err.Error(), "no beads") {
		t.Fatalf("expected a refusal to go on with an empty database, got %v", err)
	}
	for _, call := range w.calls {
		if call == "write home" {
			t.Errorf("wrote the home file over an empty database")
		}
	}
}

func TestAMoveThatFailsPrintsTheWaysBackOfTheStepsAlreadyDoneLastFirst(t *testing.T) {
	cases := []struct {
		failAt string
		step   string
		// ran is the last call made; nothing after it ran.
		ran string
		// backs are what the ways back name, last done first.
		backs []string
	}{
		{"git pull", "step 3", "git pull", []string{asideMoved.To}},
		{"git push", "step 3", "git push",
			[]string{"git -C " + vaultDir + " revert --no-edit abc1234", asideMoved.To}},
		{"systemctl --user start postern-backend", "step 4", "systemctl --user start postern-backend",
			[]string{"git -C " + vaultDir + " revert --no-edit abc1234", asideMoved.To}},
		{"healthz http://laptop.mw:8787", "step 4", "healthz http://laptop.mw:8787",
			[]string{"systemctl --user stop postern-backend", "revert --no-edit abc1234", asideMoved.To}},
		{"mail mayor", "step 5", "mail mayor",
			[]string{"systemctl --user stop postern-backend", "revert --no-edit abc1234", asideMoved.To}},
		{"bin/mayor-up", "step 5", "bin/mayor-up",
			[]string{"already sent", "systemctl --user stop postern-backend", "revert --no-edit abc1234", asideMoved.To}},
	}
	for _, c := range cases {
		t.Run(c.failAt, func(t *testing.T) {
			w, out := newMoveWorld(), &bytes.Buffer{}
			w.failAt = c.failAt

			err := move(w, out).Run(context.Background())

			if err == nil || !strings.Contains(err.Error(), c.step) {
				t.Fatalf("expected the move to stop at %s, got %v", c.step, err)
			}
			if last := w.calls[len(w.calls)-1]; last != c.ran {
				t.Errorf("expected nothing to run after %q, but %q did", c.ran, last)
			}
			text := out.String()
			mustContain(t, "ways back", text, "Ways back")
			block, at := text[strings.Index(text, "Ways back"):], -1
			for _, back := range c.backs {
				found := strings.Index(block, back)
				if found < 0 || found < at {
					t.Errorf("expected %q in the ways back, after the ones before it, in:\n%s", back, block)
				}
				at = found
			}
			if strings.Contains(text, "Home is now laptop") {
				t.Errorf("a move that stopped said it finished:\n%s", text)
			}
		})
	}
}

func TestMoveWarnsWhenBeadsSyncIsNotAuto(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	m := move(w, out)
	m.BeadsSync = application.BeadsSyncRemote

	if err := m.Run(context.Background()); err != nil {
		t.Fatalf("move: %v\n%s", err, out)
	}
	mustContain(t, "report", out.String(), `beads_sync is "remote"`, "auto")
}

func TestMoveSaysWhenThereIsNoPosternDataToAge(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	w.hasIndex = false

	if err := move(w, out).Run(context.Background()); err != nil {
		t.Fatalf("a move does not fail over what it cannot age: %v", err)
	}
	mustContain(t, "report", out.String(), "no postern index")
}

func TestACommitThatFailsPutsTheHomeFileBackAsItWas(t *testing.T) {
	for name, c := range map[string]struct {
		files application.HomeFile
		back  string
	}{
		"a home file":                   {homeIs("desktop"), "git -C " + vaultDir + " checkout -- home"},
		"a home file not understood":    {&apptest.FakeHomeFile{Text: "vps 2026-09-29T12:00:00Z x\n"}, "git -C " + vaultDir + " checkout -- home"},
		"no home file, so none to keep": {&apptest.FakeHomeFile{Missing: true}, "rm " + vaultDir + "/home"},
	} {
		t.Run(name, func(t *testing.T) {
			w, out := newMoveWorld(), &bytes.Buffer{}
			w.failAt = "git commit home"
			m := move(w, out)
			m.Files = c.files

			err := m.Run(context.Background())

			if err == nil || !strings.Contains(err.Error(), "step 3") {
				t.Fatalf("expected the move to stop at step 3, got %v", err)
			}
			mustContain(t, "ways back", out.String(), "Ways back", c.back)
		})
	}
}

func TestADoltDirectoryIsSetAsideBesideTheEmbeddedOneAndItsWayBackIsPrinted(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	w.hasDolt = true
	w.failAt = "bd bootstrap --yes"

	err := move(w, out).Run(context.Background())

	if err == nil {
		t.Fatal("expected the move to stop")
	}
	wantCalls(t, w,
		"ssh desktop", "git: read refs/dolt/data time", "host lock taken",
		"aside .beads/embeddeddolt", "aside .beads/dolt", "bd bootstrap --yes", "host lock released")
	mustContain(t, "ways back", out.String(), "Ways back",
		"mv "+doltMoved.To+" "+doltMoved.From, "mv "+asideMoved.To+" "+asideMoved.From)
}

func TestARunningDoltBeadsIsStoppedBeforeTheRenameAndItsRestartIsAWayBack(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	w.hasDolt = true
	w.running[application.DoltBeadsUnit] = true
	w.failAt = "bd bootstrap --yes"

	err := move(w, out).Run(context.Background())

	if err == nil {
		t.Fatal("expected the move to stop")
	}
	wantCalls(t, w,
		"ssh desktop", "git: read refs/dolt/data time", "host lock taken",
		"systemctl --user stop dolt-beads",
		"aside .beads/embeddeddolt", "aside .beads/dolt", "bd bootstrap --yes", "host lock released")
	text := out.String()
	block := text[strings.Index(text, "The move stopped"):]
	mustContain(t, "error", err.Error(), "stopped here", "cannot bootstrap into a stopped server")
	restart := strings.Index(block, "systemctl --user start dolt-beads")
	back := strings.Index(block, "mv "+doltMoved.To+" "+doltMoved.From)
	if restart < 0 || back < 0 || restart < back {
		t.Errorf("expected the asides moved back, and only then dolt-beads started, in:\n%s", block)
	}
}

func TestTheReportNamesWhereTheDoltDirectoryWasSetAside(t *testing.T) {
	w, out := newMoveWorld(), &bytes.Buffer{}
	w.hasDolt = true

	if err := move(w, out).Run(context.Background()); err != nil {
		t.Fatalf("move: %v\n%s", err, out)
	}
	mustContain(t, "report", out.String(), doltMoved.To, "never deleted")
}
