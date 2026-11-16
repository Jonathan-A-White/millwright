package application_test

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// followerRun is a follower of the home, springing the sleep job on the Sunday
// fixture of newSleepFixture, with its model runs marked in marks. The agent of
// the pass is held until the test lets it end.
type followerRun struct {
	f      *sleepFixture
	marks  *apptest.FakeAgentRunMarks
	spring *application.EventSpring
	log    *apptest.FakeEventLog
	cuts   *sleepVault
	cancel context.CancelFunc
	done   chan error
}

func newFollowerRun(t *testing.T) *followerRun {
	t.Helper()
	r := &followerRun{
		f: newSleepFixture(), marks: &apptest.FakeAgentRunMarks{}, log: &apptest.FakeEventLog{},
		cuts: newSleepVault(), done: make(chan error, 1),
	}
	r.f.onSunday()
	r.f.grinder.Entered = make(chan struct{}, 1)
	r.f.grinder.Hold = make(chan struct{})
	// An agent the follower's stop ends does not answer, as a killed one does not.
	r.f.pass.Grinder = killedWithTheFollower{r.f.grinder}
	now := time.Date(2026, 10, 18, 7, 30, 11, 0, time.UTC)
	nightly := &application.SleepNightly{
		Hour: 7, Minute: 30, Notes: &sleepNotes{kv: map[string]string{}},
		Run: func(ctx context.Context) error {
			_, err := r.f.pass.Run(ctx, application.SleepOptions{})
			return err
		},
		Home: func(context.Context) (bool, error) { return true, nil },
		Now:  func() time.Time { return now },
	}
	r.spring = &application.EventSpring{
		Log: r.log, Host: "laptop", Jobs: []application.SpringJob{nightly.Job()},
		Now: func() time.Time { return now }, Runs: r.marks, Cuts: r.cuts,
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	t.Cleanup(cancel)
	if err := r.spring.Spring(ctx); err != nil { // takes its bearings
		t.Fatal(err)
	}
	if err := r.spring.Spring(ctx); err != nil { // the pass is due
		t.Fatal(err)
	}
	select {
	case <-r.f.grinder.Entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the sleep pass never started its agent")
	}
	return r
}

// guarded is the restarter a landing's after-landing step uses on this host.
func (r *followerRun) guarded(units application.UnitRestarter) *application.RunGuardedRestarter {
	return &application.RunGuardedRestarter{Units: units, Runs: r.marks, Poll: time.Millisecond, Limit: 10 * time.Second}
}

// mw-gq6.1339: a landing of the factory rig restarted the follower while its
// sleep pass's agent ran, and the pass was cut at 12m30s of 12m30s or so.
func TestTheRestartAfterALandingWaitsForTheSleepPassAndTheFollowerIsOnTheNewBuildAfter(t *testing.T) {
	r := newFollowerRun(t)
	units := &apptest.FakeUnitRestarter{}
	restart := r.guarded(units)

	var notes []string
	restarted := make(chan struct{})
	go func() {
		defer close(restarted)
		notes, _ = application.RestartFactoryUnits(context.Background(), restart, application.FactoryRig)
	}()

	// While the agent runs, the follower is left alone, however long the restart waits.
	time.Sleep(50 * time.Millisecond)
	select {
	case <-restarted:
		t.Fatalf("the follower was restarted while its sleep agent ran: %v", units.Events)
	default:
	}
	if len(units.Asked) != 0 {
		t.Fatalf("units asked to restart under a run: %v", units.Asked)
	}

	// The agent answers; the pass writes its files; then the follower restarts.
	close(r.f.grinder.Hold)
	r.spring.Wait()
	<-restarted
	if got := r.f.vault.written(); !strings.Contains(strings.Join(got, ","), "sleep-2026-10-18/recommendations.json") ||
		!strings.Contains(strings.Join(got, ","), "sleep/2026-10-18.json") {
		t.Fatalf("the pass's answer was not written: %v", got)
	}
	if len(units.Asked) != 1 || units.Asked[0] != "mw-view-follow.service" {
		t.Fatalf("the follower was not restarted on the new build after the run: %v", units.Asked)
	}
	said := strings.Join(notes, "\n")
	if !strings.Contains(said, "restarted mw-view-follow.service on the new build") || !strings.Contains(said, "waited") || !strings.Contains(said, "sleep") {
		t.Errorf("the notes do not say the restart waited for the sleep pass: %q", said)
	}
	if live, _, _ := r.marks.Open(context.Background()); len(live) != 0 {
		t.Errorf("a mark of the ended run is left: %v", live)
	}
}

// The limit passes: the restart goes ahead, says a run was cut and which job,
// and the follower, stopped under the run, writes down what it cut.
func TestAtTheLimitTheRestartGoesAheadAndSaysWhichRunWasCut(t *testing.T) {
	r := newFollowerRun(t)
	units := &apptest.FakeUnitRestarter{}
	clock := time.Date(2026, 10, 18, 7, 31, 0, 0, time.UTC)
	restart := r.guarded(units)
	restart.Limit = 30 * time.Minute
	restart.Now = func() time.Time { return clock }
	restart.Wait = func(_ context.Context, d time.Duration) error { clock = clock.Add(d * 600); return nil }

	notes, failed := application.RestartFactoryUnits(context.Background(), restart, application.FactoryRig)
	if len(failed) != 0 || len(units.Asked) != 1 {
		t.Fatalf("restart at the limit: failed %v, asked %v", failed, units.Asked)
	}
	said := strings.Join(notes, "\n")
	for _, want := range []string{"restarted mw-view-follow.service on the new build", "a run was cut", "sleep"} {
		if !strings.Contains(said, want) {
			t.Errorf("the notes lack %q: %q", want, said)
		}
	}

	// The restart stops the follower: the pass is cut, and written down.
	r.cancel()
	r.spring.Wait()
	cut := r.cuts.cutRuns()
	if len(cut) != 1 {
		t.Fatalf("a cut run left %d records, want 1: %v", len(cut), r.cuts.written())
	}
	for _, want := range []string{`"job":"sleep"`, `"began":"2026-10-18T07:30:11Z"`, `"ran":`, "stopped"} {
		if !strings.Contains(cut[0], want) {
			t.Errorf("the record of the cut run lacks %q: %s", want, cut[0])
		}
	}
	if live, _, _ := r.marks.Open(context.Background()); len(live) != 0 {
		t.Errorf("a mark of the cut run is left: %v", live)
	}
}

// A run a crash or a host restart cut leaves its mark with no process: the
// follower that comes back writes it down and takes the mark away.
func TestACrashedRunsMarkIsWrittenDownByTheFollowerThatComesBack(t *testing.T) {
	marks := &apptest.FakeAgentRunMarks{Gone: []application.AgentRun{{Job: "sleep", Host: "laptop", Pid: 4242, Began: time.Date(2026, 10, 18, 7, 34, 50, 0, time.UTC)}}}
	cuts := newSleepVault()
	now := time.Date(2026, 10, 18, 7, 47, 20, 0, time.UTC)
	spring := &application.EventSpring{
		Log: &apptest.FakeEventLog{}, Host: "laptop", Now: func() time.Time { return now }, Runs: marks, Cuts: cuts,
	}
	if err := spring.Spring(context.Background()); err != nil {
		t.Fatal(err)
	}
	spring.Wait()
	cut := cuts.cutRuns()
	if len(cut) != 1 || !strings.Contains(cut[0], `"job":"sleep"`) || !strings.Contains(cut[0], `"ran":"12m30s"`) {
		t.Fatalf("the crashed run was not written down with its time: %v", cut)
	}
	if _, gone, _ := marks.Open(context.Background()); len(gone) != 0 {
		t.Errorf("the mark of the written-down run is left: %v", gone)
	}
}

// With no model run under way the restart is as it was: one note, no wait.
func TestWithNoRunUnderWayTheRestartIsAsItWasWithNoWait(t *testing.T) {
	units := &apptest.FakeUnitRestarter{}
	waits := 0
	restart := &application.RunGuardedRestarter{
		Units: units, Runs: &apptest.FakeAgentRunMarks{},
		Wait: func(context.Context, time.Duration) error { waits++; return nil },
	}
	notes, failed := application.RestartFactoryUnits(context.Background(), restart, application.FactoryRig)
	if len(failed) != 0 || waits != 0 || len(units.Asked) != 1 {
		t.Fatalf("failed %v, waits %d, asked %v", failed, waits, units.Asked)
	}
	if len(notes) != 1 || notes[0] != "restarted mw-view-follow.service on the new build" {
		t.Fatalf("notes = %q, want the one plain line", notes)
	}
}

// A job that starts no agent is not marked, so a restart never waits for it.
func TestAJobThatRunsNoAgentIsNotMarked(t *testing.T) {
	marks := &apptest.FakeAgentRunMarks{}
	log := &apptest.FakeEventLog{}
	inside := make(chan []application.AgentRun, 1)
	job := application.SpringJob{Name: "plain", Reason: application.ClockReason, Every: time.Nanosecond, Run: func(ctx context.Context) error {
		live, _, _ := marks.Open(ctx)
		inside <- live
		return nil
	}}
	spring := &application.EventSpring{Log: log, Host: "laptop", Jobs: []application.SpringJob{job}, Runs: marks}
	if err := spring.Spring(context.Background()); err != nil {
		t.Fatal(err)
	}
	spring.Wait()
	select {
	case live := <-inside:
		if len(live) != 0 {
			t.Fatalf("a job with no agent was marked: %v", live)
		}
	default:
	}
	for _, e := range log.All() {
		if e.Kind == events.KindJob && e.Detail == application.SpringInterruptedDetail {
			t.Fatalf("unexpected interruption: %v", e)
		}
	}
}

// cutRuns is the records of cut runs the vault was given, oldest first.
func (v *sleepVault) cutRuns() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	var names, out []string
	for name := range v.runs {
		if strings.HasPrefix(name, application.CutRunDir+"/") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		out = append(out, v.runs[name])
	}
	return out
}

// killedWithTheFollower is a Grinder that fails when its context ends before it
// answers, as the agent child of a follower that is stopped does.
type killedWithTheFollower struct{ inner application.Grinder }

func (k killedWithTheFollower) Grind(ctx context.Context, call application.GrindCall) (application.SessionResult, error) {
	result, err := k.inner.Grind(ctx, call)
	if err == nil && ctx.Err() != nil {
		return application.SessionResult{}, ctx.Err()
	}
	return result, err
}

// mw fuel says a cut run: the job, when it began, how long it ran.
func TestFuelSaysACutRunWithItsJobItsStartAndHowLongItRan(t *testing.T) {
	cut := application.CutRun{
		Job: "sleep", Host: "laptop", Began: time.Date(2026, 11, 15, 7, 34, 50, 0, time.UTC),
		Ran: 12*time.Minute + 30*time.Second, Why: "the follower was stopped under it",
	}
	vault := newSleepVault()
	if err := application.WriteCutRun(context.Background(), vault, cut); err != nil {
		t.Fatal(err)
	}
	runs := fuelRuns{application.CutRunDir: {}}
	for name, body := range vault.runs {
		runs[application.CutRunDir][strings.TrimPrefix(name, application.CutRunDir+"/")] = body
	}
	var out strings.Builder
	err := application.FuelByKind{Runs: runs, Days: 7, Now: func() time.Time { return time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC) }, Out: &out}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"cut run", "sleep began 2026-11-15 07:34:50Z", "ran 12m30s"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("mw fuel lacks %q:\n%s", want, out.String())
		}
	}
}
