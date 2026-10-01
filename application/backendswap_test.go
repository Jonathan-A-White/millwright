package application_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// swapRig is the home a swap step runs on, in a temp directory: a live binary
// and a staged one, and stand-ins first on PATH for curl (which answers as
// health says), systemctl (which only logs) and sleep (which does not).
type swapRig struct {
	dir, live, staged, health, systemctl string
	cfg                                  application.BackendRig
}

func aSwapRig(t *testing.T, check string) *swapRig {
	t.Helper()
	dir := t.TempDir()
	r := &swapRig{
		dir: dir, live: filepath.Join(dir, "postern"), staged: filepath.Join(dir, "stage", "postern-4c9db71"),
		health: filepath.Join(dir, "health"), systemctl: filepath.Join(dir, "systemctl.log"),
	}
	mustDo(t, os.MkdirAll(filepath.Dir(r.staged), 0o755))
	mustDo(t, os.MkdirAll(filepath.Join(dir, "bin"), 0o755))
	mustDo(t, os.WriteFile(r.live, []byte("old"), 0o755))
	mustDo(t, os.WriteFile(r.staged, []byte("new"), 0o755))
	for name, body := range map[string]string{
		"curl":      fmt.Sprintf("#!/bin/sh\n[ \"$(cat %q)\" = up ]\n", r.health),
		"systemctl": fmt.Sprintf("#!/bin/sh\necho \"$@\" >> %q\n", r.systemctl),
		"sleep":     "#!/bin/sh\n",
	} {
		mustDo(t, os.WriteFile(filepath.Join(dir, "bin", name), []byte(body), 0o755))
	}
	r.cfg = application.BackendRig{
		Dir: "server", Stage: filepath.Dir(r.staged), Live: r.live, Service: "postern-backend",
		Health: "https://postern.example.org/api/healthz", Check: check,
	}
	return r
}

func (r *swapRig) answers(t *testing.T, state string) {
	t.Helper()
	mustDo(t, os.WriteFile(r.health, []byte(state), 0o644))
}

// run is the swap step, run by sh as a hands step is, and what it printed.
func (r *swapRig) run(t *testing.T) (string, error) {
	t.Helper()
	step := application.BackendSwap(r.cfg, "laptop", "4c9db71", r.staged)
	cmd := exec.Command("sh", "-c", step.Run)
	cmd.Env = append(os.Environ(), "PATH="+filepath.Join(r.dir, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (r *swapRig) liveIs(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(r.live)
	mustDo(t, err)
	return string(raw)
}

func (r *swapRig) restarts(t *testing.T) int {
	t.Helper()
	raw, _ := os.ReadFile(r.systemctl)
	return strings.Count(string(raw), "restart")
}

// The swap step is run by mw postern inbox --apply, which holds the inbox's
// lock for the whole of the step: nothing in the step may read the inbox, so
// a configured check that does is left out of the text (mw-gq6.209).
func TestTheSwapStepNeverCallsTheInboxItIsRunFrom(t *testing.T) {
	for _, check := range []string{
		"/home/j/.local/bin/mw postern inbox --unread-count",
		"mw postern inbox",
		"cd /vault && mw postern inbox --apply",
	} {
		step := application.BackendSwap(application.BackendRig{
			Live: "/home/j/.local/bin/postern", Stage: "/home/j/.local/share/postern", Service: "postern-backend",
			Health: "https://postern.example.org/api/healthz", Check: check,
		}, "laptop", "4c9db71", "/home/j/.local/share/postern/postern-4c9db71")
		if strings.Contains(step.Run, "postern inbox") || strings.Contains(step.WayBack, "postern inbox") {
			t.Errorf("expected no inbox call in the step for check %q, got:\n%s", check, step.Run)
		}
		if !strings.Contains(step.Run, "curl -fsS -m 20 'https://postern.example.org/api/healthz'") {
			t.Errorf("expected the health check to stay, got:\n%s", step.Run)
		}
	}
}

// The step's health check completes with the inbox's lock held, because it
// takes none, and the swap goes through.
func TestTheSwapStepGoesThroughWhileTheInboxLockIsHeld(t *testing.T) {
	r := aSwapRig(t, "mw postern inbox --unread-count")
	r.answers(t, "up")

	out, err := r.run(t)

	if err != nil || r.liveIs(t) != "new" || !strings.Contains(out, "is live and answering") {
		t.Fatalf("expected the new backend live and answering, got %v, live %q:\n%s", err, r.liveIs(t), out)
	}
}

// A backend that answers is not put back over because an optional check of
// the host's own failed or hung: only a backend that never answers is.
func TestAHealthyNewBackendIsNotReplacedBecauseItsCheckFailed(t *testing.T) {
	r := aSwapRig(t, "false")
	r.answers(t, "up")

	out, err := r.run(t)

	if err == nil {
		t.Fatalf("expected the step to fail, saying the check did, got:\n%s", out)
	}
	if r.liveIs(t) != "new" || r.restarts(t) != 1 {
		t.Fatalf("expected the new backend left live with one restart, got live %q, %d restarts:\n%s", r.liveIs(t), r.restarts(t), out)
	}
	if !strings.Contains(out, "check failed") {
		t.Fatalf("expected the output to say the check failed, got:\n%s", out)
	}
}

func TestACheckThatHangsIsCutOff(t *testing.T) {
	step := application.BackendSwap(application.BackendRig{Live: "/l", Stage: "/s", Service: "svc", Health: "http://h", Check: "some-check"}, "laptop", "abc", "/s/l-abc")
	if !strings.Contains(step.Run, "timeout 60 sh -c 'some-check'") {
		t.Fatalf("expected the check bounded by timeout, got:\n%s", step.Run)
	}
}

// A backend that never answers is still put back.
func TestABackendThatNeverAnswersIsPutBack(t *testing.T) {
	r := aSwapRig(t, "")
	r.answers(t, "down")

	out, err := r.run(t)

	if err == nil || r.liveIs(t) != "old" || r.restarts(t) != 2 {
		t.Fatalf("expected the old backend back after 2 restarts, got %v, live %q, %d restarts:\n%s", err, r.liveIs(t), r.restarts(t), out)
	}
}

// ranDuring is a runner that reads, while the step runs, what mw hands list
// would say about it, and so what survives if the pass is killed there.
type ranDuring struct {
	tracker *apptest.FakeTracker
	seen    string
}

func (r *ranDuring) Run(ctx context.Context, job application.HandsJob) (application.HandsOutcome, error) {
	r.seen, _ = r.tracker.Note(ctx, application.HandsRanKey(job.Request.Bead, job.Request.ID))
	return application.HandsOutcome{Exit: 0, Output: "ok\n"}, nil
}

// A step that restarts the backend may take the pass that runs it down before
// it records anything: the run is therefore written down as started before
// the step starts, and finished after, so mw hands list never says "not run"
// of a step that was (mw-gq6.209).
func TestAStepIsRecordedAsStartedBeforeItRuns(t *testing.T) {
	f := newRunFixture(t)
	during := &ranDuring{tracker: f.tracker}
	f.runner = nil
	f.approve(t, "tx-run", "echo", runNow.Add(-2*time.Minute), "")
	inbox := f.inbox()
	inbox.HandsRunner = during

	if _, err := inbox.Apply(context.Background()); err != nil {
		t.Fatalf("applying: %v", err)
	}

	var started application.HandsRan
	if err := json.Unmarshal([]byte(during.seen), &started); err != nil || started.At == "" || started.Exit == 0 || !strings.Contains(started.Why, "started") {
		t.Fatalf("expected a started record, not a clean one, while the step ran, got %q (%v)", during.seen, err)
	}
	after, _ := f.tracker.Note(context.Background(), application.HandsRanKey("mw-e.3", "echo"))
	if after != `{"at":"2026-09-28T12:10:00Z","exit":0,"host":"desktop"}` {
		t.Fatalf("expected the finished record to replace it, got %q", after)
	}
}

// And mw hands list says so.
func TestHandsListSaysAStepThatStartedButNeverReportedBack(t *testing.T) {
	f := newRunFixture(t)
	mustDo(t, f.tracker.SetNote(context.Background(), application.HandsRanKey("mw-e.3", "echo"),
		`{"at":"2026-09-28T12:10:00Z","exit":-1,"host":"desktop","why":"`+application.HandsStartedWhy+`"}`))
	var out strings.Builder

	if _, err := (application.HandsList{Notes: f.tracker, Out: &out}).Run(context.Background(), "mw-e.3"); err != nil {
		t.Fatal(err)
	}

	if line := strings.SplitN(out.String(), "\n", 2); !strings.Contains(line[0], "started 2026-09-28T12:10:00Z on desktop") || strings.Contains(line[0], "not run") {
		t.Fatalf("expected echo not listed as not run, got:\n%s", out.String())
	}
}

// mw postern inbox --unread-count, the read a swap's health check used to end
// with, takes no inbox lock: it returns while a pass holds it.
type heldLock struct{ taken int }

func (l *heldLock) Take(ctx context.Context) (func(), error) {
	l.taken++
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestUnreadCountReturnsWhileAnotherPassHoldsTheInboxLock(t *testing.T) {
	f := newApplyFixture(t)
	lock := &heldLock{}
	inbox := f.inbox()
	inbox.Lock = lock
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := inbox.UnreadCount(ctx); err != nil {
		t.Fatalf("expected the count without waiting on the lock, got %v", err)
	}
	if lock.taken != 0 {
		t.Fatalf("expected the lock not to be taken, it was %d times", lock.taken)
	}
}
