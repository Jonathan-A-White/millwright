package application_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// mw land (plans/0117, Land Rush 3): which stories a batch takes and in what
// order. The landing itself, on a real rig and origin, is
// features/batch_land.feature.

func TestSortOldestQueuedPutsTheOldestFirstAndUnrecordedTimesBeforeAll(t *testing.T) {
	at := func(minute int) time.Time { return time.Date(2026, 10, 15, 12, minute, 0, 0, time.UTC) }
	queue := []application.BatchStory{
		{ID: "mw-c", QueuedAt: at(30)},
		{ID: "mw-b", QueuedAt: at(5)},
		{ID: "mw-z"}, // queued by an mw that recorded no time
		{ID: "mw-a", QueuedAt: at(30)},
	}
	application.SortOldestQueued(queue)
	var got []string
	for _, s := range queue {
		got = append(got, s.ID)
	}
	if want := []string{"mw-z", "mw-b", "mw-a", "mw-c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected the order %v, got %v", want, got)
	}
}

func TestConflictLinesAreGitsOwnLines(t *testing.T) {
	said := "git merge --no-edit origin/mw/mw-1 in /tmp/x: exit status 1: Auto-merging a.md\n" +
		"CONFLICT (content): Merge conflict in a.md\nCONFLICT (add/add): Merge conflict in b.md\n" +
		"Automatic merge failed; fix conflicts and then commit the result."
	got := application.ConflictLines(said)
	want := []string{"CONFLICT (content): Merge conflict in a.md", "CONFLICT (add/add): Merge conflict in b.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if got := application.ConflictLines("the merge has conflicts\nmore"); !reflect.DeepEqual(got, []string{"the merge has conflicts"}) {
		t.Fatalf("expected the first line when git named no conflict, got %q", got)
	}
}

// dryLanding answers what a dry run asks of the rig: every branch at the
// origin is there, two commits ahead.
type dryLanding struct {
	application.Landing
}

func (dryLanding) RefHead(_ context.Context, _, ref string) (string, error) {
	return "0123456789abcdef-" + ref, nil
}

func (dryLanding) Ahead(context.Context, string, string, string) (int, error) { return 2, nil }

type fetchOnly struct {
	application.Worktrees
	fetched int
}

func (f *fetchOnly) Fetch(context.Context, string) error { f.fetched++; return nil }

// aBatchRig is a tracker with stories of two rigs, some queued at the given
// minutes past noon, and the BatchLand of millwright over it, in a dry run.
func aBatchRig(t *testing.T) (application.BatchLand, *apptest.FakeTracker, *bytes.Buffer) {
	t.Helper()
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-gq6", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Host: "vps"})
	tracker.AddEpic("lp-1", domain.Path{Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Host: "vps"})
	for _, s := range []struct {
		epic, id string
		minute   int // when it was queued; negative is claimed and not queued
	}{
		{"mw-gq6", "mw-gq6.1", 40},
		{"mw-gq6", "mw-gq6.2", 10},
		{"mw-gq6", "mw-gq6.3", -1},
		{"lp-1", "lp-1.1", 1},
		{"mw-gq6", "mw-gq6.4", 25},
	} {
		tracker.AddStory(s.epic, domain.Story{ID: s.id, Title: "The story " + s.id})
		mustDo(t, tracker.ClaimStory(ctx, s.id))
		if s.minute < 0 {
			continue
		}
		queue(t, tracker, s.id)
		mustDo(t, tracker.SetStoryMetadata(ctx, s.id, map[string]string{
			application.QueuedAtField: time.Date(2026, 10, 15, 12, s.minute, 0, 0, time.UTC).Format(time.RFC3339),
		}))
	}
	var out bytes.Buffer
	land := application.BatchLand{
		Next: application.Next{
			Tracker: tracker, Vault: newFakeVault(), Worktrees: &fetchOnly{}, Landing: dryLanding{},
			Checks: failingChecks{t}, Slot: noSlot{t}, Host: "vps", Seat: "builder",
			Rigs: map[string]string{"millwright": "/rigs/millwright", "lampas": "/rigs/lampas"},
			Out:  &out,
		},
		Records: noRecords{t},
		DryRun:  true,
	}
	return land, tracker, &out
}

type failingChecks struct{ t *testing.T }

func (f failingChecks) Run(context.Context, string, string) (application.Checked, error) {
	f.t.Fatalf("a dry run ran the rig's tests")
	return application.Checked{}, nil
}

type noSlot struct{ t *testing.T }

func (s noSlot) Take(context.Context, string, string) (application.Holding, error) {
	s.t.Fatalf("a dry run took the merge slot")
	return nil, nil
}

type noRecords struct{ t *testing.T }

func (r noRecords) NewBatch(context.Context, string) (int, error) {
	r.t.Fatalf("a dry run began a batch's records")
	return 0, nil
}

func (r noRecords) PutBatchFile(context.Context, string, int, string, string) (string, error) {
	r.t.Fatalf("a dry run wrote a batch's records")
	return "", nil
}

func TestADryRunTakesOnlyTheRigsQueuedStoriesOldestFirstAndChangesNothing(t *testing.T) {
	land, tracker, out := aBatchRig(t)
	asked := len(tracker.Asked())

	report, err := land.Run(context.Background(), "millwright")
	if err != nil {
		t.Fatalf("a dry run: %v", err)
	}
	var got []string
	for _, s := range report.Queue {
		got = append(got, s.ID)
	}
	if want := []string{"mw-gq6.2", "mw-gq6.4", "mw-gq6.1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected the batch %v, oldest queued first and none of another rig or not queued, got %v", want, got)
	}
	if report.Queue[0].Branch != "origin/mw/mw-gq6.2" || report.Queue[0].Commits != 2 {
		t.Fatalf("expected each story's branch as the origin has it, with its commits, got %+v", report.Queue[0])
	}
	for _, call := range tracker.Asked()[asked:] {
		if call != "WorkInHand" {
			t.Fatalf("expected a dry run only to read the queue, got %v", tracker.Asked()[asked:])
		}
	}
	printed := out.String()
	if !strings.Contains(printed, "dry run") || strings.Index(printed, "mw-gq6.2") > strings.Index(printed, "mw-gq6.1") {
		t.Fatalf("expected the dry run printed in merge order, got:\n%s", printed)
	}
}

func TestANothingQueuedRigLandsNothing(t *testing.T) {
	land, _, out := aBatchRig(t)
	land.DryRun = false
	land.Next.Rigs["quiet"] = "/rigs/quiet"

	report, err := land.Run(context.Background(), "quiet")
	if err != nil {
		t.Fatalf("landing an empty queue: %v", err)
	}
	if len(report.Queue) != 0 || report.Landed || report.Held != "" {
		t.Fatalf("expected nothing taken, landed or held, got %+v", report)
	}
	if !strings.Contains(out.String(), "nothing is queued") {
		t.Fatalf("expected it to say nothing is queued, got:\n%s", out.String())
	}
}

func TestLandingARigThisHostHasNotCheckedOutIsRefused(t *testing.T) {
	land, _, _ := aBatchRig(t)
	if _, err := land.Run(context.Background(), "elsewhere"); err == nil || !strings.Contains(err.Error(), "not checked out") {
		t.Fatalf("expected a rig that is not checked out refused, got %v", err)
	}
}

// The follower's batch-land job (plans/0117, Land Rush 4): when a rig's queue
// is due, mw land is run on it, on the home only.

func TestBatchDueFollowsTheRigsKeys(t *testing.T) {
	settings := domain.LandingSettings{BatchEvery: 30 * time.Minute, BatchMin: 2, BatchMax: 4}
	now := time.Date(2026, 10, 15, 13, 0, 0, 0, time.UTC)
	ago := func(minutes int) time.Time { return now.Add(-time.Duration(minutes) * time.Minute) }
	for _, c := range []struct {
		name   string
		queued int
		oldest time.Time
		want   bool
	}{
		{"nothing queued is never due", 0, ago(600), false},
		{"below the minimum and young is not due", 1, ago(5), false},
		{"below the minimum and old is not due", 1, ago(600), false},
		{"the minimum reached but young is not due", 2, ago(29), false},
		{"the minimum reached and old enough is due", 2, ago(30), true},
		{"the maximum reached is due at once", 4, ago(0), true},
		{"past the maximum is due", 5, ago(1), true},
		{"a queue whose time was never recorded is old", 2, time.Time{}, true},
	} {
		if got := application.BatchDue(settings, c.queued, c.oldest, now); got != c.want {
			t.Errorf("%s: expected due=%v, got %v", c.name, c.want, got)
		}
	}
}

// aLandingJob is the follower's batch-land job over a tracker whose stories
// are queued as a test says, each rig's file as given. Land lands the oldest
// queued story of the rig when it is asked for one, and every queued story of
// it otherwise, and notes what it was asked.
type aLandingJob struct {
	t       *testing.T
	tracker *apptest.FakeTracker
	rules   *apptest.FakeEpicRules
	home    bool
	now     time.Time
	asked   []string
	failing map[string]error
	job     application.SpringJob
}

func newLandingJob(t *testing.T) *aLandingJob {
	t.Helper()
	j := &aLandingJob{
		t:       t,
		tracker: apptest.NewFakeTracker(),
		rules:   apptest.NewFakeEpicRules(),
		home:    true,
		now:     time.Date(2026, 10, 15, 13, 0, 0, 0, time.UTC),
		failing: map[string]error{},
	}
	for _, rig := range []string{"lampas", "millwright"} {
		j.tracker.AddEpic("e-"+rig, domain.Path{Rig: rig, Branch: "main", Harness: domain.HarnessClaude,
			Model: domain.ModelOpus, Effort: domain.EffortHigh, Host: "desktop"})
	}
	landings := &application.BatchLandings{
		Rigs:    map[string]string{"lampas": "/rigs/lampas", "millwright": "/rigs/millwright"},
		Rules:   j.rules,
		Tracker: j.tracker,
		Home:    func(context.Context) (bool, error) { return j.home, nil },
		Land:    j.land,
		Notes:   j.tracker,
		Host:    "desktop",
		Now:     func() time.Time { return j.now },
	}
	j.job = landings.Job()
	return j
}

// queued puts a story of rig in the queue, queued the given minutes ago.
func (j *aLandingJob) queued(rig, id string, minutesAgo int) {
	j.t.Helper()
	ctx := context.Background()
	j.tracker.AddStory("e-"+rig, domain.Story{ID: id, Title: "The story " + id})
	mustDo(j.t, j.tracker.ClaimStory(ctx, id))
	queue(j.t, j.tracker, id)
	mustDo(j.t, j.tracker.SetStoryMetadata(ctx, id, map[string]string{
		application.QueuedAtField: j.now.Add(-time.Duration(minutesAgo) * time.Minute).Format(time.RFC3339),
	}))
}

func (j *aLandingJob) land(ctx context.Context, rig string, one bool) error {
	j.asked = append(j.asked, fmt.Sprintf("%s one=%v", rig, one))
	if err := j.failing[rig]; err != nil {
		return err
	}
	work, err := j.tracker.WorkInHand(ctx)
	mustDo(j.t, err)
	for _, d := range work.Running {
		path, _ := d.Path()
		if path.Rig != rig || !application.QueuedForBatch(d) {
			continue
		}
		mustDo(j.t, j.tracker.SetStoryState(ctx, d.Story.ID, application.RunState, application.RunLanded, "landed"))
		if one {
			break
		}
	}
	return nil
}

func (j *aLandingJob) rule(rig string, mode domain.LandingMode) {
	j.rules.Require(rig, domain.EpicRequirements{Landing: domain.LandingSettings{Mode: mode}})
}

func (j *aLandingJob) probe() (string, bool) { return j.job.Probe(context.Background()) }

func TestTheBatchLandJobLandsABatchRigWhenItsQueueIsDue(t *testing.T) {
	j := newLandingJob(t)
	j.rule("lampas", domain.LandingBatch)
	j.queued("lampas", "lp-1", 5)
	if reason, ok := j.probe(); ok {
		t.Fatalf("one young story is not a batch, but the job was sprung: %q", reason)
	}
	j.queued("lampas", "lp-2", 4)
	if reason, ok := j.probe(); ok {
		t.Fatalf("two young stories are not due, but the job was sprung: %q", reason)
	}
	j.now = j.now.Add(30 * time.Minute)
	reason, ok := j.probe()
	if !ok {
		t.Fatal("the oldest has waited batch_every with batch_min queued, but the job was not sprung")
	}
	if !strings.Contains(reason, "lampas") || !strings.Contains(reason, "2 queued") {
		t.Fatalf("expected the reason to name the rig and the count, got %q", reason)
	}
	mustDo(t, j.job.Run(context.Background()))
	if got, want := j.asked, []string{"lampas one=false"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected one mw land of the whole queue, got %v", got)
	}
	if reason, ok := j.probe(); ok {
		t.Fatalf("the queue is empty, but the job was sprung: %q", reason)
	}
}

func TestTheBatchLandJobLandsAtOnceWhenTheQueueReachesBatchMax(t *testing.T) {
	j := newLandingJob(t)
	j.rule("lampas", domain.LandingBatch)
	for i := 0; i < domain.DefaultBatchMax; i++ {
		j.queued("lampas", fmt.Sprintf("lp-%d", i), 0)
	}
	reason, ok := j.probe()
	if !ok || !strings.Contains(reason, "6 queued") {
		t.Fatalf("expected the job sprung for 6 queued, got %q, %v", reason, ok)
	}
}

func TestTheBatchLandJobLandsAnEachRigsLeftoversOneAtATime(t *testing.T) {
	j := newLandingJob(t)
	j.rule("lampas", domain.LandingEach)
	j.queued("lampas", "lp-1", 2)
	j.queued("lampas", "lp-2", 1)
	// A story of another rig that lands in batches, young, is left alone.
	j.rule("millwright", domain.LandingBatch)
	j.queued("millwright", "mw-1", 0)

	reason, ok := j.probe()
	if !ok || !strings.Contains(reason, "lampas") || !strings.Contains(reason, "2 queued") || strings.Contains(reason, "millwright") {
		t.Fatalf("expected the reason to name lampas and 2 queued only, got %q, %v", reason, ok)
	}
	mustDo(t, j.job.Run(context.Background()))
	if got, want := j.asked, []string{"lampas one=true", "lampas one=true"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected the two leftovers landed singly, got %v", got)
	}
	if _, ok := j.probe(); ok {
		t.Fatal("no leftover remains, but the job was sprung")
	}
}

func TestTheBatchLandJobDoesNotRunOnTheBoost(t *testing.T) {
	j := newLandingJob(t)
	j.rule("lampas", domain.LandingBatch)
	j.queued("lampas", "lp-1", 600)
	j.queued("lampas", "lp-2", 600)
	j.home = false
	if reason, ok := j.probe(); ok {
		t.Fatalf("the job was sprung on the boost: %q", reason)
	}
	mustDo(t, j.job.Run(context.Background()))
	if len(j.asked) != 0 {
		t.Fatalf("mw land was run on the boost: %v", j.asked)
	}
}

func TestTheBatchLandJobLeavesAHeldBatchAloneUntilBatchEveryHasPassed(t *testing.T) {
	j := newLandingJob(t)
	j.rule("lampas", domain.LandingBatch)
	j.queued("lampas", "lp-1", 600)
	j.queued("lampas", "lp-2", 600)
	j.failing["lampas"] = errors.New("batch 1 of lampas was held: red")
	err := j.job.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "lampas") {
		t.Fatalf("expected the failure named, got %v", err)
	}
	if reason, ok := j.probe(); ok {
		t.Fatalf("a held batch was tried again at once: %q", reason)
	}
	j.now = j.now.Add(domain.DefaultBatchEvery)
	if _, ok := j.probe(); !ok {
		t.Fatal("the held batch was not tried again after batch_every")
	}
}

// A batch held for its fixer's harness stop waits for the stop's time, kept in
// the host's note, even once batch_every has passed (mw-gq6.472): a landing
// before then would start no fixer and be held again.
func TestTheBatchLandJobLeavesABatchHeldForTheHarnessAloneUntilTheResetHasCome(t *testing.T) {
	j := newLandingJob(t)
	j.rule("lampas", domain.LandingBatch)
	j.queued("lampas", "lp-1", 600)
	j.queued("lampas", "lp-2", 600)
	j.failing["lampas"] = errors.New("batch 1 of lampas was held: the harness stopped its fixer")
	reset := j.now.Add(2 * time.Hour)
	mustDo(t, j.tracker.SetNote(context.Background(), application.HarnessHoldKey("desktop"),
		reset.Format(time.RFC3339)+" usage-limit You've hit your weekly limit"))
	if err := j.job.Run(context.Background()); err == nil {
		t.Fatal("expected the held landing named")
	}
	j.now = j.now.Add(domain.DefaultBatchEvery)
	if reason, ok := j.probe(); ok {
		t.Fatalf("a batch held for the harness was tried again before the reset at %s: %q", reset, reason)
	}
	j.now = reset
	if _, ok := j.probe(); !ok {
		t.Fatal("the batch held for the harness was not tried again once the reset had come")
	}
}

func TestTheBatchLandJobStopsDrainingWhenALandingLeavesTheQueueAsItWas(t *testing.T) {
	j := newLandingJob(t)
	j.rule("lampas", domain.LandingEach)
	j.queued("lampas", "lp-1", 2)
	j.queued("lampas", "lp-2", 1)
	landings := &application.BatchLandings{
		Rigs: map[string]string{"lampas": "/rigs/lampas"}, Rules: j.rules, Tracker: j.tracker,
		Home: func(context.Context) (bool, error) { return true, nil },
		Land: func(_ context.Context, rig string, one bool) error {
			j.asked = append(j.asked, rig)
			return nil // lands nothing
		},
		Now: func() time.Time { return j.now },
	}
	err := landings.Job().Run(context.Background())
	if err == nil || len(j.asked) != 1 {
		t.Fatalf("expected one try and an error for a landing that landed nothing, got %d tries, %v", len(j.asked), err)
	}
}

// mw-gq6.470: the follower restarts after every landing of the factory rig.
// A landing in flight then runs on apart from it; the follower that comes back
// waits for that landing, begins no second one beside it however often the
// queue says it is due, and the log tells one outcome, not a pass cut short.
func TestABatchLandInFlightAcrossAFollowerRestartIsWaitedForAndToldOnce(t *testing.T) {
	j := newLandingJob(t)
	j.rule("lampas", domain.LandingBatch)
	j.queued("lampas", "lp-1", 600)
	j.queued("lampas", "lp-2", 600)
	log, procs := &apptest.FakeEventLog{}, &apptest.FakeLandProcesses{}
	follower := func() *application.EventSpring {
		detached := application.DetachedLand{Processes: procs}
		landings := &application.BatchLandings{
			Rigs: map[string]string{"lampas": "/rigs/lampas"}, Rules: j.rules, Tracker: j.tracker,
			Home: func(context.Context) (bool, error) { return true, nil },
			Land: detached.Land, Pending: detached.Pending,
			Now: func() time.Time { return j.now },
		}
		return &application.EventSpring{Log: log, Host: "laptop", Jobs: []application.SpringJob{landings.Job()},
			Now: func() time.Time { return j.now }}
	}
	waiting := func() {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); !procs.Waiting("lampas"); time.Sleep(5 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatal("nothing waited on the landing of lampas")
			}
		}
	}

	ctx, stop := context.WithCancel(context.Background())
	first := follower()
	mustDo(t, first.Spring(ctx)) // its bearings
	mustDo(t, first.Spring(ctx)) // the queue is due
	waiting()
	stop() // a millwright landing restarts the follower
	first.Wait()

	second := follower()
	mustDo(t, second.Spring(context.Background()))
	waiting()
	mustDo(t, second.Spring(context.Background())) // the queue still reads due
	if got := procs.Begun("lampas"); got != 1 {
		t.Fatalf("expected the one landing waited for across the restart, got %d begun", got)
	}
	for _, id := range []string{"lp-1", "lp-2"} { // what mw land does
		mustDo(t, j.tracker.SetStoryState(context.Background(), id, application.RunState, application.RunLanded, "landed"))
	}
	mustDo(t, procs.End("lampas", nil))
	second.Wait()

	var told []string
	for _, e := range log.All() {
		if e.Kind != events.KindJob || e.Actor != "batch-land@laptop" {
			continue
		}
		if e.Detail == application.SpringInterruptedDetail || e.To == events.JobFailed {
			t.Errorf("the log says the landing was %s: %q", e.To, e.Detail)
		}
		told = append(told, e.To+" "+e.Detail)
	}
	want := []string{
		"scheduled landing of lampas (2 queued)", "running ", "done " + application.SpringLeftRunningDetail,
		"scheduled " + application.SpringRestartedReason, "running ", "done ",
	}
	if len(told) < len(want) || !reflect.DeepEqual(told[:len(want)], want) {
		t.Fatalf("expected the log to tell\n%q\nfirst, got\n%q", want, told)
	}
	if got := procs.Begun("lampas"); got != 1 {
		t.Fatalf("expected one landing, got %d begun", got)
	}
}

// The batch's fixer (plans/0117, Land Rush 5): what it is booted with and how
// its word is read back. The fixer started, waited for and judged by mw's own
// run of the tests is features/batch_land.feature.

// aBatchToFix is a red batch of millwright: two stories merged, a third set
// aside for its conflict.
func aBatchToFix() application.BatchFixBrief {
	return application.BatchFixBrief{
		Rig: "millwright", Target: "main", Number: 4, Base: "base0123",
		Stories: []application.BatchStory{
			{ID: "mw-gq6.1", Title: "The first", Branch: "origin/mw/mw-gq6.1", Merge: "merge111"},
			{ID: "mw-gq6.2", Title: "The second", Branch: "origin/mw/mw-gq6.2", Merge: "merge222"},
			{ID: "mw-gq6.3", Title: "The third", Branch: "origin/mw/mw-gq6.3",
				Why: "it conflicts with the batch", Conflicts: []string{"CONFLICT (content): Merge conflict in a.md"}},
		},
		Tested: true,
		Gate:   application.Checked{Command: "make test", Output: "ok one\nFAIL two: the batch is red\n"},
		Settings: domain.EpicRequirements{Landing: domain.LandingSettings{
			FixerModel: "haiku", FixerEffort: "medium", FixerTurns: 12,
		}}.LandingSettings(),
	}
}

func TestBatchFixBootsOneBuilderInTheLandingWorktreeOnTheRigsFixerKeys(t *testing.T) {
	files, harness := newFakeVault(), &fakeHarness{}
	boot := application.SeatBoot{Vault: files, Harness: harness, Seat: "builder", Host: "vps",
		After: []string{"mw", "next"}, Heartbeat: []string{"mw", "next", "--heartbeat"}}

	spec, err := boot.BatchFix(context.Background(), aBatchToFix(), "/rigs/millwright/.landing-1")
	if err != nil {
		t.Fatalf("booting the fixer: %v", err)
	}
	l := harness.launch
	if spec.Name != "batch-millwright-4" || l.StoryID != "batch-millwright-4" || l.Dir != "/rigs/millwright/.landing-1" {
		t.Fatalf("expected the fixer batch-millwright-4 in the landing worktree, got %+v / %+v", spec, l)
	}
	if l.Path.Model != domain.ModelHaiku || l.Path.Effort != domain.EffortMedium || l.MaxTurns != 12 || l.Path.Rig != "millwright" {
		t.Fatalf("expected the rig's fixer keys, haiku/medium and 12 turns, got %+v, %d turns", l.Path, l.MaxTurns)
	}
	if len(l.After) != 0 || len(l.Heartbeat) != 0 {
		t.Fatalf("expected nothing after or beside the fixer, since mw land waits for it, got %v and %v", l.After, l.Heartbeat)
	}
	if l.Seat != "builder" || l.ResultFile != "/vault/runs/batch-millwright-4/result.json" {
		t.Fatalf("expected a Builder with its result under runs/batch-millwright-4, got %+v", l)
	}
	if !strings.Contains(l.Kickoff, "fixer of batch 4 of millwright") || !strings.Contains(l.Kickoff, "Do not push") {
		t.Fatalf("expected the kickoff to name the batch and forbid a push, got %q", l.Kickoff)
	}
	if _, written := files.written["batch-millwright-4/boot.md"]; !written {
		t.Fatalf("expected the boot file written under runs/batch-millwright-4, got %v", files.written)
	}
}

func TestBatchFixRefusesFixerKeysThatMakeNoPath(t *testing.T) {
	brief := aBatchToFix()
	brief.Settings.FixerModel = "gpt"
	boot := application.SeatBoot{Vault: newFakeVault(), Harness: &fakeHarness{}, Seat: "builder", Host: "vps"}
	if _, err := boot.BatchFix(context.Background(), brief, "/rigs/millwright/.landing-1"); err == nil || !strings.Contains(err.Error(), "batch_fixer") {
		t.Fatalf("expected a fixer model nobody knows refused, naming the keys, got %v", err)
	}
}

func TestTheFixersBootFileNamesEveryBranchItsConflictTheTestsTailAndTheRules(t *testing.T) {
	seat := application.Seat{Name: "builder", Rig: "millwright", Charter: "the builder's charter", Memory: "what the builder knows"}
	prompt := application.BatchFixBootPrompt(seat, aBatchToFix())
	for _, want := range []string{
		"the builder's charter", "what the builder knows",
		"# Your batch: batch 4 of millwright",
		"1. mw-gq6.1, origin/mw/mw-gq6.1: merged at merge111",
		"2. mw-gq6.2, origin/mw/mw-gq6.2: merged at merge222",
		"3. mw-gq6.3, origin/mw/mw-gq6.3: SET ASIDE, git said: CONFLICT (content): Merge conflict in a.md",
		"`make test` did not pass", "FAIL two: the batch is red",
		"You may not push", "revert a story's branch", "drop a story's change",
		"delete, skip or loosen a test without saying so on that story",
		"comment on each story you touched", "TOUCHED <story>", "CAUSE <story>", "no second fixer",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("expected the fixer's boot file to hold %q, got:\n%s", want, prompt)
		}
	}

	brief := aBatchToFix()
	brief.Tested = false
	if prompt := application.BatchFixBootPrompt(seat, brief); !strings.Contains(prompt, "Not run yet") {
		t.Errorf("expected a fixer started before the tests ran to be told so, got:\n%s", prompt)
	}
}

func TestReadFixerWordReadsTouchedAndCauseLinesAndNothingElse(t *testing.T) {
	said := "Mended it.\nTOUCHED mw-gq6.2: src/a.ts, src/a.test.ts\n- `TOUCHED mw-gq6.3: b.md`\n" +
		"CAUSE mw-gq6.2: it renamed a field mw-gq6.1 reads\nTOUCHED: nobody\nTOUCHED two words: x\nCAUSED nothing"
	touched, causes := application.ReadFixerWord(said)
	wantTouched := []application.FixerWord{{Story: "mw-gq6.2", Said: "src/a.ts, src/a.test.ts"}, {Story: "mw-gq6.3", Said: "b.md"}}
	if !reflect.DeepEqual(touched, wantTouched) {
		t.Fatalf("expected %+v touched, got %+v", wantTouched, touched)
	}
	wantCauses := []application.FixerWord{{Story: "mw-gq6.2", Said: "it renamed a field mw-gq6.1 reads"}}
	if !reflect.DeepEqual(causes, wantCauses) {
		t.Fatalf("expected %+v as its word on the cause, got %+v", wantCauses, causes)
	}
}

func TestABatchLineCountsTheFixerItsFuelAndBothGateRuns(t *testing.T) {
	red := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	report := application.BatchReport{
		Rig: "lampas", Number: 2, Landed: true, Tested: true, GateRuns: 2, GateTook: 30 * time.Minute, RedAt: red,
		Queue:  []application.BatchStory{{ID: "lp-1"}, {ID: "lp-2"}},
		Merged: []application.BatchStory{{ID: "lp-1"}, {ID: "lp-2"}},
		Fixer: &application.BatchFixing{Started: true, ResultRead: true,
			Result: application.SessionResult{Fuel: application.Fuel{Input: 1000, Output: 500}}},
	}
	m := report.Metric(red.Add(20 * time.Minute))
	if !m.Fixer || m.FixerTokens != 1500 || m.GateRuns != 2 || m.GateMinutes != 30 || m.Outcome != application.BatchFixed || m.RedMinutes != 20 {
		t.Fatalf("expected a fixer of 1500 tokens, 2 gate runs over 30 minutes, outcome fixed and 20 red minutes, got %+v", m)
	}
	report.Landed = false
	if m := report.Metric(red); m.Outcome != application.BatchHeld || !m.Fixer {
		t.Fatalf("expected a batch held after its fixer to say held, with a fixer, got %+v", m)
	}
}

// quietQueueTracker is a tracker that counts the work in hand it is asked for
// and answers, in one cheap read, whether any story is queued for batch landing.
type quietQueueTracker struct {
	*apptest.FakeTracker
	works  int
	asks   int
	failed error
}

func (q *quietQueueTracker) WorkInHand(ctx context.Context) (application.WorkInHand, error) {
	q.works++
	return q.FakeTracker.WorkInHand(ctx)
}

func (q *quietQueueTracker) AnyQueuedForBatch(ctx context.Context) (bool, error) {
	q.asks++
	if q.failed != nil {
		return false, q.failed
	}
	work, err := q.FakeTracker.WorkInHand(ctx)
	if err != nil {
		return false, err
	}
	for _, d := range work.Running {
		if application.QueuedForBatch(d) {
			return true, nil
		}
	}
	return false, nil
}

// mw-gq6.600: the follower asks the batch-land job every pass whether a rig is
// due, and each ask listed the work in hand: three bd and a bd show or more,
// every few seconds, most of the beads server's connections. A tracker that can
// say in one read that nothing is queued spares it the listing.
func TestTheBatchLandProbeListsNoWorkWhileNothingIsQueued(t *testing.T) {
	j := newLandingJob(t)
	quiet := &quietQueueTracker{FakeTracker: j.tracker}
	landings := &application.BatchLandings{
		Rigs: map[string]string{"lampas": "/rigs/lampas"}, Rules: j.rules, Tracker: quiet,
		Home: func(context.Context) (bool, error) { return true, nil },
		Land: j.land, Notes: j.tracker, Host: "desktop",
		Now: func() time.Time { return j.now },
	}
	job := landings.Job()
	for i := 0; i < 50; i++ {
		if reason, wanted := job.Probe(context.Background()); wanted {
			t.Fatalf("probe %d: nothing is queued, yet the job wants to run: %s", i, reason)
		}
	}
	if quiet.works != 0 || quiet.asks != 50 {
		t.Fatalf("expected 50 cheap asks and no work in hand listed, got %d asks, %d listings", quiet.asks, quiet.works)
	}

	j.queued("lampas", "lp-1", 2)
	if reason, wanted := job.Probe(context.Background()); !wanted || quiet.works != 1 {
		t.Fatalf("expected a queued story to be read and the rig due, got %q, %v after %d listings", reason, wanted, quiet.works)
	}
}

// A tracker that cannot answer the cheap ask does not hide a queue: the work in
// hand is read, as it was.
func TestTheBatchLandProbeListsTheWorkWhenTheCheapAskFails(t *testing.T) {
	j := newLandingJob(t)
	quiet := &quietQueueTracker{FakeTracker: j.tracker, failed: fmt.Errorf("no kept connection")}
	landings := &application.BatchLandings{
		Rigs: map[string]string{"lampas": "/rigs/lampas"}, Rules: j.rules, Tracker: quiet,
		Home: func(context.Context) (bool, error) { return true, nil },
		Land: j.land, Notes: j.tracker, Host: "desktop",
		Now: func() time.Time { return j.now },
	}
	j.queued("lampas", "lp-1", 2)
	if reason, wanted := landings.Job().Probe(context.Background()); !wanted || quiet.works != 1 {
		t.Fatalf("expected the queue found by the work in hand, got %q, %v after %d listings", reason, wanted, quiet.works)
	}
}
