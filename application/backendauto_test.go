package application_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// autoSwapNow is the clock of the automatic swap's tests.
var autoSwapNow = time.Date(2026, 10, 5, 21, 0, 0, 0, time.UTC)

// swapLock is the inbox lock a swap tick must not take from a pass that holds it.
type swapLock struct {
	held  bool
	taken int
}

func (l *swapLock) TryTake(context.Context) (func(), bool, error) {
	if l.held {
		return nil, false, nil
	}
	l.taken++
	l.held = true
	return func() { l.held = false }, true, nil
}

// autoRig is a home that stages a landing and then swaps it itself: the runner
// answers every step with outcome, the result is said on said.
type autoRig struct {
	backendRig
	runner *apptest.FakeHandsRunner
	said   *apptest.FakePosternSender
	lock   *swapLock
}

func anAutoRig(t *testing.T, outcome application.HandsOutcome) autoRig {
	t.Helper()
	r := aBackendRig(t, "laptop", "laptop")
	a := autoRig{
		backendRig: r,
		runner:     &apptest.FakeHandsRunner{Outcome: outcome},
		said:       &apptest.FakePosternSender{},
		lock:       &swapLock{},
	}
	a.stage.Runner, a.stage.Say, a.stage.Lock = a.runner, a.said, a.lock
	a.stage.Now = func() time.Time { return autoSwapNow }
	return a
}

func (a autoRig) stagedBead(t *testing.T) string {
	t.Helper()
	filed := filedUnder(t, a.backendRig, "mw-j0f2d")
	if len(filed) != 1 {
		t.Fatalf("expected one swap bead filed, got %+v", filed)
	}
	return filed[0].Story.ID
}

func (a autoRig) bead(t *testing.T, id string) application.StoryDetail {
	t.Helper()
	d, err := a.tracker.ShowStory(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// talkAt keeps what TalkWait keeps of the Governor's last talk record, heard ago before the tick.
func (a autoRig) talkAt(t *testing.T, role string, ago time.Duration) {
	t.Helper()
	raw, err := json.Marshal(application.TalkLast{ID: "talk-1", Role: role, At: autoSwapNow.Add(-ago).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.tracker.SetNote(context.Background(), application.TalkLastKey, string(raw)); err != nil {
		t.Fatal(err)
	}
}

const swapAnswered = "curl said ok\nbackend 4c9db71 is live and answering\n"

// AC1: a staged swap with no Talk open is run once by the home's tick, its
// result said once on the swap bead's channel, the bead closed.
func TestAStagedSwapIsRunByTheTickOnceWithNoTalkOpen(t *testing.T) {
	a := anAutoRig(t, application.HandsOutcome{Exit: 0, Output: swapAnswered})
	a.stage.Landed(context.Background(), presenceLanding())
	bead := a.stagedBead(t)
	if len(a.runner.Jobs()) != 0 {
		t.Fatal("expected the landing itself to run nothing: the tick swaps")
	}

	a.stage.Pending(context.Background())

	jobs := a.runner.Jobs()
	if len(jobs) != 1 || jobs[0].Request.Bead != bead || jobs[0].Request.ID != "backend-4c9db71" || len(jobs[0].Remote) != 0 {
		t.Fatalf("expected backend-4c9db71 of %s run here once, got %+v", bead, jobs)
	}
	if want := handsSteps(t, a.tracker, bead)[0].Run; jobs[0].Request.Run != want {
		t.Errorf("expected the step run exactly as it is written on the bead")
	}
	said := a.said.Sent()
	if len(said) != 1 || said[0].Thread != bead || !said[0].Recorded || said[0].Text != "backend 4c9db71 is live and answering" {
		t.Fatalf("expected one message on %s saying the backend is live, got %+v", bead, said)
	}
	if got := a.bead(t, bead); got.Status != application.StatusClosed {
		t.Fatalf("expected %s closed after a good swap, got %q", bead, got.Status)
	}
	raw, _ := a.tracker.Note(context.Background(), application.HandsRanKey(bead, "backend-4c9db71"))
	if !strings.Contains(raw, `"exit":0`) {
		t.Errorf("expected the run recorded as exit 0, got %q", raw)
	}
	var wrote bool
	for _, c := range a.tracker.Comments(bead) {
		wrote = wrote || strings.Contains(c, "live and answering")
	}
	if !wrote {
		t.Errorf("expected the output commented on %s, got %q", bead, a.tracker.Comments(bead))
	}
}

// AC2: while a Talk is open nothing runs; the first tick after it ends does.
func TestAStagedSwapWaitsForTheTalkAndRunsOnTheFirstTickAfterIt(t *testing.T) {
	a := anAutoRig(t, application.HandsOutcome{Exit: 0, Output: swapAnswered})
	a.stage.Landed(context.Background(), presenceLanding())
	bead := a.stagedBead(t)

	a.talkAt(t, "turn", 2*time.Minute)
	a.stage.Pending(context.Background())
	a.stage.Pending(context.Background())
	if len(a.runner.Jobs()) != 0 || len(a.said.Sent()) != 0 {
		t.Fatalf("expected nothing run while the Talk is open, got %+v", a.runner.Jobs())
	}
	if got := a.bead(t, bead); got.Status == application.StatusClosed {
		t.Fatal("expected the bead left open while the Talk is open")
	}

	a.talkAt(t, "end", time.Minute)
	a.stage.Pending(context.Background())
	if len(a.runner.Jobs()) != 1 || len(a.said.Sent()) != 1 {
		t.Fatalf("expected the swap run on the first tick after the Talk, got %d jobs, %d messages", len(a.runner.Jobs()), len(a.said.Sent()))
	}
}

// A Talk nobody has spoken in for the quiet spell is over, as the Talk screen reads it.
func TestATalkLeftQuietIsOverAndDoesNotHoldTheSwap(t *testing.T) {
	a := anAutoRig(t, application.HandsOutcome{Exit: 0, Output: swapAnswered})
	a.stage.Landed(context.Background(), presenceLanding())
	a.talkAt(t, "turn", application.TalkQuietSpell+time.Minute)

	a.stage.Pending(context.Background())

	if len(a.runner.Jobs()) != 1 {
		t.Fatalf("expected the swap run, the Talk being quiet, got %d jobs", len(a.runner.Jobs()))
	}
}

// AC3: a failing health check puts the old backend back (the step does that),
// the failure is said, the bead stays open and hitl with the output, and no
// later tick runs it again.
func TestAFailedSwapIsSaidLeftOpenWithTheOutputAndNeverRetried(t *testing.T) {
	out := "curl: (22) the health url said 502\nFAILED after 4 tries: putting the old backend back\n"
	a := anAutoRig(t, application.HandsOutcome{Exit: 1, Output: out})
	a.stage.Landed(context.Background(), presenceLanding())
	bead := a.stagedBead(t)

	a.stage.Pending(context.Background())
	a.stage.Pending(context.Background())
	a.stage.Pending(context.Background())

	if len(a.runner.Jobs()) != 1 {
		t.Fatalf("expected one run and no retry, got %d", len(a.runner.Jobs()))
	}
	said := a.said.Sent()
	if len(said) != 1 || said[0].Thread != bead || !strings.Contains(said[0].Text, "old backend was put back") || !strings.Contains(said[0].Text, "4c9db71") {
		t.Fatalf("expected one message that the swap failed and the old backend was put back, got %+v", said)
	}
	got := a.bead(t, bead)
	if got.Status == application.StatusClosed || !got.Hitl() {
		t.Fatalf("expected %s open and hitl, got status %q labels %v", bead, got.Status, got.Labels)
	}
	var kept bool
	for _, c := range a.tracker.Comments(bead) {
		kept = kept || strings.Contains(c, "health url said 502")
	}
	if !kept {
		t.Errorf("expected the output commented on the bead, got %q", a.tracker.Comments(bead))
	}
	raw, _ := a.tracker.Note(context.Background(), application.HandsRanKey(bead, "backend-4c9db71"))
	if !strings.Contains(raw, `"exit":1`) {
		t.Errorf("expected the run recorded as exit 1, got %q", raw)
	}
}

// A step that fails without putting anything back does not say it did.
func TestAFailedSwapThatPutNothingBackDoesNotSayItDid(t *testing.T) {
	a := anAutoRig(t, application.HandsOutcome{Exit: 1, Output: "backend 4c9db71 answers 'h' but its check failed: leaving it live\n"})
	a.stage.Landed(context.Background(), presenceLanding())

	a.stage.Pending(context.Background())

	said := a.said.Sent()
	if len(said) != 1 || strings.Contains(said[0].Text, "put back") || !strings.Contains(said[0].Text, "leaving it live") {
		t.Fatalf("expected the failure said as it was, got %+v", said)
	}
}

// AC4: the same staged commit is never run twice, however many ticks come, or
// however many times its landing is staged again.
func TestTheSameStagedCommitIsNeverRunTwice(t *testing.T) {
	a := anAutoRig(t, application.HandsOutcome{Exit: 0, Output: swapAnswered})
	a.stage.Landed(context.Background(), presenceLanding())

	a.stage.Pending(context.Background())
	if said := a.stage.Pending(context.Background()); len(said) != 0 {
		t.Fatalf("expected a second tick to do nothing, got %q", said)
	}
	if len(a.runner.Jobs()) != 1 || len(a.said.Sent()) != 1 {
		t.Fatalf("expected one run and one message, got %d and %d", len(a.runner.Jobs()), len(a.said.Sent()))
	}
}

// A step whose run was begun, by this tick or a tap, is not begun again.
func TestAStepThatAlreadyStartedIsNotRunAgain(t *testing.T) {
	a := anAutoRig(t, application.HandsOutcome{Exit: 0, Output: swapAnswered})
	a.stage.Landed(context.Background(), presenceLanding())
	bead := a.stagedBead(t)
	if err := a.tracker.SetNote(context.Background(), application.HandsRanKey(bead, "backend-4c9db71"), `{"at":"x","exit":-1,"host":"laptop"}`); err != nil {
		t.Fatal(err)
	}

	a.stage.Pending(context.Background())

	if len(a.runner.Jobs()) != 0 {
		t.Fatalf("expected no run, got %+v", a.runner.Jobs())
	}
}

// AC5: a rig that keeps the tap files the step as before and never runs it.
func TestARigThatKeepsTheTapNeverRunsItsSwap(t *testing.T) {
	a := anAutoRig(t, application.HandsOutcome{Exit: 0, Output: swapAnswered})
	cfg := a.stage.Settings["postern"]
	cfg.Swap = application.BackendSwapHands
	a.stage.Settings["postern"] = cfg

	a.stage.Landed(context.Background(), presenceLanding())
	bead := a.stagedBead(t)
	a.stage.Pending(context.Background())

	if len(a.runner.Jobs()) != 0 || len(a.said.Sent()) != 0 {
		t.Fatalf("expected the tap kept: nothing run or said, got %+v", a.runner.Jobs())
	}
	if len(handsSteps(t, a.tracker, bead)) != 1 || !a.bead(t, bead).Hitl() || len(a.sender.Sent()) != 1 {
		t.Fatal("expected the hands step filed and pushed as before")
	}
	if left, _ := a.tracker.NotesWithPrefix(context.Background(), application.BackendSwapPrefix); len(left) != 0 {
		t.Fatalf("expected no swap kept for the tick, got %v", left)
	}
}

// A rig switched to the tap after its swap was staged is left to him.
func TestASwapStagedBeforeTheRigTookTheTapIsNotRun(t *testing.T) {
	a := anAutoRig(t, application.HandsOutcome{Exit: 0, Output: swapAnswered})
	a.stage.Landed(context.Background(), presenceLanding())
	cfg := a.stage.Settings["postern"]
	cfg.Swap = application.BackendSwapHands
	a.stage.Settings["postern"] = cfg

	a.stage.Pending(context.Background())

	if len(a.runner.Jobs()) != 0 {
		t.Fatalf("expected nothing run, got %+v", a.runner.Jobs())
	}
}

// The swap restarts the backend the inbox pass reads: it never runs beside one,
// or beside another swap, and runs on the first tick that finds the lock free.
func TestTheSwapWaitsForTheInboxLockAndLetsGoOfIt(t *testing.T) {
	a := anAutoRig(t, application.HandsOutcome{Exit: 0, Output: swapAnswered})
	a.stage.Landed(context.Background(), presenceLanding())

	a.lock.held = true
	a.stage.Pending(context.Background())
	if len(a.runner.Jobs()) != 0 {
		t.Fatal("expected no swap while a pass holds the inbox lock")
	}

	a.lock.held = false
	a.stage.Pending(context.Background())
	if len(a.runner.Jobs()) != 1 {
		t.Fatalf("expected the swap once the lock was free, got %d", len(a.runner.Jobs()))
	}
	if a.lock.held {
		t.Fatal("expected the lock let go after the swap")
	}
}

// A newer staged swap takes the older one's place: the older never runs.
func TestAnOlderStagedSwapThatWasSupersededIsNotRun(t *testing.T) {
	a := anAutoRig(t, application.HandsOutcome{Exit: 0, Output: swapAnswered})
	a.stage.Landed(context.Background(), swapLanding("mw-j0f2d.28", "1111111aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	a.stage.Landed(context.Background(), swapLanding("mw-j0f2d.28", "2222222bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))

	a.stage.Pending(context.Background())

	jobs := a.runner.Jobs()
	if len(jobs) != 1 || jobs[0].Request.ID != "backend-2222222" {
		t.Fatalf("expected only the newer swap run, got %+v", jobs)
	}
}

// A host that is not home runs nothing.
func TestAHostThatIsNotHomeNeverRunsTheSwap(t *testing.T) {
	a := anAutoRig(t, application.HandsOutcome{Exit: 0, Output: swapAnswered})
	a.stage.Landed(context.Background(), presenceLanding())
	a.stage.Host = "desktop"

	a.stage.Pending(context.Background())

	if len(a.runner.Jobs()) != 0 {
		t.Fatalf("expected nothing run away from home, got %+v", a.runner.Jobs())
	}
}

// A good swap that found its binary already live changes nothing and says so in
// the step's own words.
func TestASwapThatChangedNothingSaysWhatTheStepSaid(t *testing.T) {
	a := anAutoRig(t, application.HandsOutcome{Exit: 0, Output: "backend 4c9db71 is already live: nothing was changed by this tap\n"})
	a.stage.Landed(context.Background(), presenceLanding())

	a.stage.Pending(context.Background())

	said := a.said.Sent()
	if len(said) != 1 || !strings.Contains(said[0].Text, "already live") || strings.Contains(said[0].Text, "live and answering") {
		t.Fatalf("expected the step's own words, got %+v", said)
	}
}

// TalkWait keeps what it heard of the Governor's last talk record, for the
// swap to read: a turn opens the Talk the swap waits out.
func TestTalkWaitKeepsTheGovernorsLastTalkRecord(t *testing.T) {
	f := newTalkPosternFixture(t)
	seq := f.addTurn("hello")

	f.run(seq)

	raw, err := f.tracker.Note(context.Background(), application.TalkLastKey)
	if err != nil {
		t.Fatal(err)
	}
	var last application.TalkLast
	if err := json.Unmarshal([]byte(raw), &last); err != nil || last.ID != "talk-7" || last.Role != application.TalkRoleTurn || last.At == 0 {
		t.Fatalf("expected talk-7's turn kept with its time, got %q (%v)", raw, err)
	}
}
