package application_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
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

func (r noRecords) BatchFilePath(string, int, string) string { return "" }

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

// queueReadingTracker is a tracker that counts the work in hand it is asked for
// and reads the queued stories in one cheap read, as the beads gateway does
// over its kept connection.
type queueReadingTracker struct {
	*apptest.FakeTracker
	works  int
	asks   int
	failed error
}

func (q *queueReadingTracker) WorkInHand(ctx context.Context) (application.WorkInHand, error) {
	q.works++
	return q.FakeTracker.WorkInHand(ctx)
}

func (q *queueReadingTracker) QueuedStories(ctx context.Context) ([]application.QueuedStory, error) {
	q.asks++
	if q.failed != nil {
		return nil, q.failed
	}
	work, err := q.FakeTracker.WorkInHand(ctx)
	if err != nil {
		return nil, err
	}
	var queued []application.QueuedStory
	for _, d := range work.Running {
		if !application.QueuedForBatch(d) {
			continue
		}
		path, err := d.Path()
		if err != nil {
			continue
		}
		queued = append(queued, application.QueuedStory{ID: d.Story.ID, Rig: path.Rig, QueuedAt: d.QueuedAt, Status: application.StatusInProgress})
	}
	return queued, nil
}

func (j *aLandingJob) landingsOn(tracker application.BatchQueues) *application.BatchLandings {
	return &application.BatchLandings{
		Rigs: map[string]string{"lampas": "/rigs/lampas"}, Rules: j.rules, Tracker: tracker,
		Home: func(context.Context) (bool, error) { return true, nil },
		Land: j.land, Notes: j.tracker, Host: "desktop",
		Now: func() time.Time { return j.now },
	}
}

// mw-gq6.600, mw-gq6.605: the follower asks the batch-land job every pass
// whether a rig is due, and each ask listed the work in hand: three bd and a bd
// show or more, every few seconds, most of the beads server's connections. A
// tracker that reads the queue in one cheap read never lists the work in hand
// to probe, whether nothing is queued or a story is.
func TestTheBatchLandProbeListsNoWorkInHandWhileTheQueueCanBeRead(t *testing.T) {
	j := newLandingJob(t)
	reader := &queueReadingTracker{FakeTracker: j.tracker}
	job := j.landingsOn(reader).Job()
	for i := 0; i < 50; i++ {
		if reason, wanted := job.Probe(context.Background()); wanted {
			t.Fatalf("probe %d: nothing is queued, yet the job wants to run: %s", i, reason)
		}
	}
	if reader.works != 0 || reader.asks != 50 {
		t.Fatalf("expected 50 cheap reads and no work in hand listed, got %d reads, %d listings", reader.asks, reader.works)
	}

	j.queued("lampas", "lp-1", 2)
	reader.works = 0
	for i := 0; i < 20; i++ {
		if reason, wanted := job.Probe(context.Background()); !wanted || reason != "landing of lampas (1 queued, one at a time)" && reason != "landing of lampas (1 queued)" {
			t.Fatalf("probe %d: expected the queued story to make the rig due, got %q, %v", i, reason, wanted)
		}
	}
	if reader.works != 0 {
		t.Fatalf("expected a probe pass with a story queued to list no work in hand, got %d listings", reader.works)
	}
}

// The queue read hands the rig and the time the queue is judged by: a rig that
// is not on this host is left out, the oldest queued time of a rig is its
// oldest, and a batch is due by BatchDue as it was when the work was listed.
func TestTheBatchLandProbeJudgesTheQueueReadByRigAndOldest(t *testing.T) {
	j := newLandingJob(t)
	reader := &queueReadingTracker{FakeTracker: j.tracker}
	j.queued("millwright", "mw-1", 600) // a rig the landings do not know here
	j.queued("lampas", "lp-1", 2)
	if reason, wanted := j.landingsOn(reader).Job().Probe(context.Background()); !wanted || strings.Contains(reason, "millwright") {
		t.Fatalf("expected only lampas due, got %q, %v", reason, wanted)
	}
	if reader.works != 0 {
		t.Fatalf("expected no work in hand listed, got %d", reader.works)
	}
}

// A tracker that cannot read the queue does not hide it: the work in hand is
// read, as it was.
func TestTheBatchLandProbeListsTheWorkWhenTheQueueReadFails(t *testing.T) {
	j := newLandingJob(t)
	reader := &queueReadingTracker{FakeTracker: j.tracker, failed: fmt.Errorf("no kept connection")}
	j.queued("lampas", "lp-1", 2)
	if reason, wanted := j.landingsOn(reader).Job().Probe(context.Background()); !wanted || reader.works != 1 {
		t.Fatalf("expected the queue found by the work in hand, got %q, %v after %d listings", reason, wanted, reader.works)
	}
}

// mw-gq6.803: a landing that goes on apart (lampas' post-landing grist smoke
// ran for an hour) held back every other rig's due batch on the host.

// twoRigLandings is BatchLandings over lampas and millwright whose Land is as
// given; both rigs' queues are old and hold two stories, so both are due.
func twoRigLandings(t *testing.T, land func(ctx context.Context, rig string, one bool) error) (*aLandingJob, *application.BatchLandings) {
	t.Helper()
	j := newLandingJob(t)
	for _, rig := range []string{"lampas", "millwright"} {
		j.rule(rig, domain.LandingBatch)
		j.queued(rig, rig+"-1", 600)
		j.queued(rig, rig+"-2", 600)
	}
	return j, &application.BatchLandings{
		Rigs:  map[string]string{"lampas": "/rigs/lampas", "millwright": "/rigs/millwright"},
		Rules: j.rules, Tracker: j.tracker,
		Home: func(context.Context) (bool, error) { return true, nil },
		Land: land, Notes: j.tracker, Host: "desktop",
		Now: func() time.Time { return j.now },
	}
}

func TestALandingLeftRunningApartDoesNotStopTheNextRigsLanding(t *testing.T) {
	var mu sync.Mutex
	var asked []string
	land := func(_ context.Context, rig string, _ bool) error {
		mu.Lock()
		defer mu.Unlock()
		asked = append(asked, rig)
		if rig == "lampas" {
			return fmt.Errorf("%w: the wait ended", application.ErrLeftRunning)
		}
		return nil
	}
	_, landings := twoRigLandings(t, land)
	err := landings.Job().Run(context.Background())
	if !errors.Is(err, application.ErrLeftRunning) {
		t.Fatalf("expected the pass to tell lampas went on apart, got %v", err)
	}
	sort.Strings(asked)
	if want := []string{"lampas", "millwright"}; !reflect.DeepEqual(asked, want) {
		t.Fatalf("expected both rigs landed in the pass, got %v", asked)
	}
}

func TestARigsLandingStillRunningDoesNotHoldBackAnotherRigsLanding(t *testing.T) {
	release := make(chan struct{})
	millwrightAsked := make(chan struct{})
	land := func(ctx context.Context, rig string, _ bool) error {
		if rig == "lampas" {
			<-release // the smoke goes on
			return nil
		}
		close(millwrightAsked)
		return nil
	}
	_, landings := twoRigLandings(t, land)
	done := make(chan error, 1)
	go func() { done <- landings.Job().Run(context.Background()) }()
	select {
	case <-millwrightAsked:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("millwright was not landed while the landing of lampas was still running")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("expected the pass to end well, got %v", err)
	}
}

func TestARigStillDueWhileItsOwnLandingRunsIsNotLandedAgainInThePass(t *testing.T) {
	release := make(chan struct{})
	var mu sync.Mutex
	asked := map[string]int{}
	land := func(ctx context.Context, rig string, _ bool) error {
		mu.Lock()
		asked[rig]++
		mu.Unlock()
		if rig == "lampas" {
			<-release // lampas's batch goes on, and its queue is still due at every look
		}
		return nil
	}
	_, landings := twoRigLandings(t, land)
	landings.Recheck = 5 * time.Millisecond
	done := make(chan error, 1)
	go func() { done <- landings.Job().Run(context.Background()) }()
	time.Sleep(100 * time.Millisecond) // many rechecks while lampas lands
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("expected the pass to end well, got %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := (map[string]int{"lampas": 1, "millwright": 1}); !reflect.DeepEqual(asked, want) {
		t.Fatalf("expected one landing for each rig in the pass, got %v", asked)
	}
}

func TestARigThatFallsDueWhileAnotherLandingRunsIsLandedInThePass(t *testing.T) {
	release := make(chan struct{})
	millwrightAsked := make(chan struct{})
	var j *aLandingJob
	land := func(ctx context.Context, rig string, _ bool) error {
		if rig == "lampas" {
			<-release
			return nil
		}
		close(millwrightAsked)
		return nil
	}
	var landings *application.BatchLandings
	j, landings = twoRigLandings(t, land)
	// millwright's queue is young when the pass begins: not due.
	j.rule("millwright", domain.LandingBatch)
	for _, id := range []string{"millwright-1", "millwright-2"} {
		mustDo(t, j.tracker.SetStoryMetadata(context.Background(), id, map[string]string{
			application.QueuedAtField: j.now.Add(-time.Minute).Format(time.RFC3339),
		}))
	}
	landings.Recheck = 10 * time.Millisecond
	var clockMu sync.Mutex
	now := j.now
	landings.Now = func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return now }
	done := make(chan error, 1)
	go func() { done <- landings.Job().Run(context.Background()) }()
	time.Sleep(50 * time.Millisecond)
	select {
	case <-millwrightAsked:
		close(release)
		t.Fatal("millwright is not due yet, but was landed")
	default:
	}
	clockMu.Lock()
	now = now.Add(40 * time.Minute) // now it is
	clockMu.Unlock()
	select {
	case <-millwrightAsked:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("millwright fell due while lampas was landing, but was not landed in the pass")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("expected the pass to end well, got %v", err)
	}
}

// staticQueue is a tracker whose cheap queue read says what a test gives it.
type staticQueue struct {
	*apptest.FakeTracker
	stories []application.QueuedStory
	works   int
}

func (s *staticQueue) WorkInHand(ctx context.Context) (application.WorkInHand, error) {
	s.works++
	return s.FakeTracker.WorkInHand(ctx)
}

func (s *staticQueue) QueuedStories(context.Context) ([]application.QueuedStory, error) {
	return s.stories, nil
}

// mw-gq6.824: the follower's probe counted every open or in-progress story
// labelled queued, and mw land takes only those it can (WorkInHand.Queued): a
// story open, unclaimed and blocked is left alone by mw land, which ended
// "nothing is queued" at once, and the probe sprang the job again a few
// seconds later, for ever. The two agree on what is queued now.
func TestTheBatchLandProbeDoesNotCountAQueuedStoryMwLandWouldNotTake(t *testing.T) {
	j := newLandingJob(t)
	j.rule("lampas", domain.LandingEach)
	reader := &staticQueue{FakeTracker: j.tracker, stories: []application.QueuedStory{
		{ID: "lp-1", Rig: "lampas", Status: application.StatusOpen, Blocked: true},
		{ID: "lp-2", Rig: "lampas", Status: application.StatusOpen, Blocked: true},
	}}
	job := j.landingsOn(reader).Job()
	if reason, wanted := job.Probe(context.Background()); wanted {
		t.Fatalf("the only queued stories are open, unclaimed and blocked, yet the job wants to run: %s", reason)
	}
	for name, taken := range map[string]application.QueuedStory{
		"in progress":     {ID: "lp-3", Rig: "lampas", Status: application.StatusInProgress},
		"open, claimed":   {ID: "lp-3", Rig: "lampas", Status: application.StatusOpen, Claimed: true, Blocked: true},
		"open, unblocked": {ID: "lp-3", Rig: "lampas", Status: application.StatusOpen},
	} {
		reader.stories = []application.QueuedStory{reader.stories[0], taken}
		if reason, wanted := job.Probe(context.Background()); !wanted || !strings.Contains(reason, "1 queued") {
			t.Errorf("%s: expected the story counted, one queued, got %q, %v", name, reason, wanted)
		}
	}
}

// mw-gq6.824: a land that finds nothing to land ends clean, so it never
// counted as failed, and the follower began it again at once. It waits
// batch_every like a failed one, unless the queue changes.
func TestARigWhoseLandFoundNothingIsNotLandedAgainBeforeBatchEveryUnlessItsQueueChanged(t *testing.T) {
	j := newLandingJob(t)
	j.rule("lampas", domain.LandingBatch)
	j.queued("lampas", "lp-1", 600)
	j.queued("lampas", "lp-2", 600)
	asked := 0
	landings := j.landingsOn(j.tracker)
	landings.Land = func(context.Context, string, bool) error {
		asked++
		return nil // "queue: nothing is queued for batch landing"
	}
	job := landings.Job()
	mustDo(t, job.Run(context.Background()))
	if asked != 1 {
		t.Fatalf("expected one land, got %d", asked)
	}
	if reason, wanted := job.Probe(context.Background()); wanted {
		t.Fatalf("a land that found nothing was due again at once: %s", reason)
	}
	mustDo(t, job.Run(context.Background()))
	if asked != 1 {
		t.Fatalf("expected no second land before batch_every, got %d", asked)
	}
	j.now = j.now.Add(domain.DefaultBatchEvery / 2)
	j.queued("lampas", "lp-3", 600)
	if _, wanted := job.Probe(context.Background()); !wanted {
		t.Fatal("a story joined the queue, but the rig was not due")
	}
	mustDo(t, job.Run(context.Background()))
	if asked != 2 {
		t.Fatalf("expected a land for the changed queue, got %d", asked)
	}
	if _, wanted := job.Probe(context.Background()); wanted {
		t.Fatal("the land found nothing again, yet the rig was due at once")
	}
	j.now = j.now.Add(domain.DefaultBatchEvery)
	if _, wanted := job.Probe(context.Background()); !wanted {
		t.Fatal("batch_every has passed, but the rig was not due again")
	}
}

// A batch landing springs the Tester as a single landing does (mw-gq6.961): one
// Tester story for each landed story of a rig the trial names whose closing
// comment tells the Governor how to check it, at the batch's commit.

const aBatchTesterSteps = "HOW TO CHECK IT, for the Governor:\n1. Open the page.\n2. Press \"Save\". It reads \"Saved\".\n"

// aBatchLandedWithTesters records as landed the stories of lampas epic mw-e a
// batch merged, and returns what it said.
func aBatchLandedWithTesters(t *testing.T, trial application.TesterTrial, tracker application.WorkTracker, rules application.EpicRules, landed ...string) *application.NextReport {
	t.Helper()
	ctx := context.Background()
	at := time.Date(2026, 11, 4, 16, 0, 0, 0, time.UTC)
	path := domain.Path{Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude, Model: domain.ModelOpus, Effort: domain.EffortHigh}
	report := &application.BatchReport{Rig: "lampas", Number: 379, Target: "main", Base: "origin/main", Commit: "abc1234def5678"}
	for _, id := range landed {
		mustDo(t, tracker.ClaimStory(ctx, id))
		detail, err := tracker.ShowStory(ctx, id)
		mustDo(t, err)
		report.Merged = append(report.Merged, application.MergedStory(id, "merge"+id, detail, path))
	}
	lands := &fakeRetryLanding{}
	var out bytes.Buffer
	land := application.BatchLand{Next: application.Next{
		Tracker: tracker, Vault: newFakeVault(), Worktrees: lands, Landing: lands, Rules: rules,
		Tester: trial, Now: func() time.Time { return at },
		Seat: "builder", Host: "laptop", Out: &out, Err: &out,
		Rigs: map[string]string{"lampas": "/rigs/lampas"},
	}}
	nr := &application.NextReport{}
	land.RecordLanded(ctx, "/rigs/lampas", report, nr)
	return nr
}

func aBatchEpicToTest(t *testing.T) (*apptest.FakeTracker, *apptest.FakeEpicRules) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-e", domain.Path{Rig: "lampas", Branch: "main"})
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.1", Title: "Build the page"})
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.2", Title: "Tidy the code"})
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.3", Title: "Test: Earlier page"})
	mustDo(t, tracker.SetLabels("mw-e.3", application.LabelTester))
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.4", Title: "Demo: the page"})
	mustDo(t, tracker.SetLabels("mw-e.4", application.LabelDemo))
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.5", Title: "Build the other page"})
	ctx := context.Background()
	mustDo(t, tracker.CommentOnStory(ctx, "mw-e.1", "Done.\n"+aBatchTesterSteps))
	mustDo(t, tracker.CommentOnStory(ctx, "mw-e.2", "Done.\nHOW TO CHECK IT, for the Governor:\nInternal: nothing for the Governor to look at.\n"))
	mustDo(t, tracker.CommentOnStory(ctx, "mw-e.3", "Done.\n"+aBatchTesterSteps))
	mustDo(t, tracker.CommentOnStory(ctx, "mw-e.4", "Done.\n"+aBatchTesterSteps))
	mustDo(t, tracker.CommentOnStory(ctx, "mw-e.5", "Done.\n"+aBatchTesterSteps))
	rules := apptest.NewFakeEpicRules()
	rules.Require("lampas", domain.EpicRequirements{LastStoryLabels: []string{"demo"}})
	return tracker, rules
}

func theTestersFiled(t *testing.T, tracker *apptest.FakeTracker, before []string) []application.StoryDetail {
	t.Helper()
	var filed []application.StoryDetail
	for _, id := range tracker.Stories() {
		if slices.Contains(before, id) {
			continue
		}
		detail, err := tracker.ShowStory(context.Background(), id)
		mustDo(t, err)
		filed = append(filed, detail)
	}
	return filed
}

func TestABatchLandingOnATrialRigFilesOneTesterForEachStoryWithStepsAtTheBatchCommit(t *testing.T) {
	tracker, rules := aBatchEpicToTest(t)
	before := tracker.Stories()
	trial := application.TesterTrial{Rigs: []string{"lampas"}, Until: time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC),
		Model: domain.ModelSonnet, Effort: domain.EffortHigh}

	aBatchLandedWithTesters(t, trial, tracker, rules, "mw-e.1", "mw-e.2", "mw-e.3", "mw-e.5")

	filed := theTestersFiled(t, tracker, before)
	if len(filed) != 2 {
		t.Fatalf("expected a Tester for mw-e.1 and mw-e.5 only (not an internal, Tester or demo story), got %d: %+v", len(filed), filed)
	}
	for i, id := range []string{"mw-e.1", "mw-e.5"} {
		d := filed[i]
		if !application.IsTesterStory(d) || application.TesterLanded(d) != id {
			t.Errorf("expected a Tester story for %s, got %q: %s", id, d.Story.Title, d.Description)
		}
		if !strings.Contains(d.Description, "abc1234") {
			t.Errorf("expected the Tester to drive the batch's commit abc1234, got %s", d.Description)
		}
		if d.Story.Overrides.Model != domain.ModelSonnet || d.Story.Overrides.Formula != application.TesterFormula {
			t.Errorf("expected the trial's model and the tester formula, got %+v", d.Story.Overrides)
		}
		if d.Status != apptest.StatusOpen {
			t.Errorf("expected the Tester story released, got status %q", d.Status)
		}
	}
	// The epic's demo waits on the Testers, as after a single landing.
	if needs := needsOf(t, tracker, "mw-e.4"); !slices.Contains(needs, filed[0].Story.ID) {
		t.Errorf("expected the demo to wait on the Tester %s, got %q", filed[0].Story.ID, needs)
	}
}

func TestABatchLandingOutsideTheTrialOrWithTheTrialOverFilesNoTester(t *testing.T) {
	until := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	for name, trial := range map[string]application.TesterTrial{
		"no trial":       {},
		"another rig":    {Rigs: []string{"millwright"}, Until: until},
		"the trial over": {Rigs: []string{"lampas"}, Until: time.Date(2026, 11, 4, 16, 0, 0, 0, time.UTC)},
	} {
		tracker, rules := aBatchEpicToTest(t)
		before := tracker.Stories()
		aBatchLandedWithTesters(t, trial, tracker, rules, "mw-e.1", "mw-e.5")
		if filed := theTestersFiled(t, tracker, before); len(filed) != 0 {
			t.Errorf("%s: expected no Tester, got %+v", name, filed)
		}
	}
}

// failingToFile is a tracker that cannot file a story.
type failingToFile struct{ *apptest.FakeTracker }

func (failingToFile) CreateStory(context.Context, application.NewStory) (string, error) {
	return "", errors.New("the tracker is away")
}

func TestATesterThatCannotBeFiledForABatchIsANoteAndTheStoriesAreStillClosed(t *testing.T) {
	tracker, rules := aBatchEpicToTest(t)
	before := tracker.Stories()
	trial := application.TesterTrial{Rigs: []string{"lampas"}, Until: time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)}

	nr := aBatchLandedWithTesters(t, trial, failingToFile{tracker}, rules, "mw-e.1", "mw-e.2", "mw-e.5")

	notes := strings.Join(nr.Notes, "\n")
	if !strings.Contains(notes, "no Tester story was filed for mw-e.1") || !strings.Contains(notes, "mw-e.5") {
		t.Errorf("expected a note for each Tester that could not be filed, got %q", notes)
	}
	if filed := theTestersFiled(t, tracker, before); len(filed) != 0 {
		t.Errorf("expected nothing filed, got %+v", filed)
	}
	for _, id := range []string{"mw-e.1", "mw-e.2", "mw-e.5"} {
		if d, _ := tracker.ShowStory(context.Background(), id); d.Status != apptest.StatusClosed {
			t.Errorf("expected %s closed in spite of the Tester, got %q", id, d.Status)
		}
	}
}

func TestABatchLandingThatMergedADemoFilesNoTesterForIt(t *testing.T) {
	tracker, rules := aBatchEpicToTest(t)
	before := tracker.Stories()
	trial := application.TesterTrial{Rigs: []string{"lampas"}, Until: time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)}

	aBatchLandedWithTesters(t, trial, tracker, rules, "mw-e.4")

	if filed := theTestersFiled(t, tracker, before); len(filed) != 0 {
		t.Errorf("expected no Tester for a demo, got %+v", filed)
	}
}

// A batch landing says what a single landing says of each story it lands
// (mw-gq6.963): the files the story's branch changed that its files key does not
// name, as a comment, and the backend the story changed, staged with its hands
// step. Both are read against the batch's base, since by the time the batch is
// recorded the target branch holds the stories.

// batchChanges is the files each branch of a batch changed, by branch, and the
// ranges ChangedFiles was asked about.
type batchChanges struct {
	files map[string][]string
	asked *[]string
}

func (b batchChanges) ChangedFiles(_ context.Context, _, base, branch string) ([]string, error) {
	*b.asked = append(*b.asked, base+".."+branch)
	return b.files[branch], nil
}

func (batchChanges) AddedFiles(context.Context, string, string, string) ([]application.AddedFile, error) {
	return nil, nil
}

func (batchChanges) DiffLines(context.Context, string, string, string, string) ([]application.DiffLine, error) {
	return nil, nil
}

// aBatchLandedOnPostern records as landed a batch of two stories of the postern
// rig of epic mw-j0f2d, mw-j0f2d.28 (files key server/a.go) and mw-j0f2d.29
// (files key server/b.go), each branch having changed what changes says, and
// returns the rig as the backend tests build it and what the record said.
func aBatchLandedOnPostern(t *testing.T, changes map[string][]string, backendChanged bool) (backendRig, *application.NextReport, []string) {
	t.Helper()
	ctx := context.Background()
	r := aBackendRig(t, "laptop", "laptop")
	r.builds.Changes = backendChanged
	r.tracker.AddStory("mw-j0f2d", domain.Story{ID: "mw-j0f2d.29", Title: "Show the Governor's presence"})
	report := &application.BatchReport{Rig: "postern", Number: 7, Target: "main", Base: "origin/main", Commit: landedCommit}
	for id, key := range map[string]string{"mw-j0f2d.28": "server/a.go", "mw-j0f2d.29": "server/b.go"} {
		mustDo(t, r.tracker.ClaimStory(ctx, id))
		detail, err := r.tracker.ShowStory(ctx, id)
		mustDo(t, err)
		path := domain.Path{Rig: "postern", Branch: "main", Harness: domain.HarnessClaude, Model: domain.ModelOpus, Effort: domain.EffortHigh, Files: key}
		report.Merged = append(report.Merged, application.MergedStory(id, "merge"+id, detail, path))
	}
	slices.SortFunc(report.Merged, func(a, b application.BatchStory) int { return strings.Compare(a.ID, b.ID) })
	lands := &fakeRetryLanding{}
	var out bytes.Buffer
	var asked []string
	land := application.BatchLand{Next: application.Next{
		Tracker: r.tracker, Vault: newFakeVault(), Worktrees: lands, Landing: lands,
		Rules:   apptest.NewFakeEpicRules(),
		Changes: batchChanges{files: changes, asked: &asked},
		Backend: r.stage,
		Now:     func() time.Time { return time.Date(2026, 11, 4, 16, 0, 0, 0, time.UTC) },
		Seat:    "builder", Host: "laptop", Out: &out, Err: &out,
		Rigs: map[string]string{"postern": "/rigs/postern"},
	}}
	nr := &application.NextReport{}
	land.RecordLanded(ctx, "/rigs/postern", report, nr)
	return r, nr, asked
}

// swapBeadsFiled is the beads filed under the epic besides the batch's two stories.
func swapBeadsFiled(t *testing.T, r backendRig) []application.StoryDetail {
	t.Helper()
	var filed []application.StoryDetail
	for _, s := range filedUnder(t, r, "mw-j0f2d") {
		if s.Story.ID != "mw-j0f2d.29" {
			filed = append(filed, s)
		}
	}
	return filed
}

func TestABatchLandingCommentsOnEachStoryTheFilesItsBranchChangedOutsideItsKey(t *testing.T) {
	r, _, asked := aBatchLandedOnPostern(t, map[string][]string{
		"origin/mw/mw-j0f2d.28": {"server/a.go", "docs/notes.md"},
		"origin/mw/mw-j0f2d.29": {"server/b.go"},
	}, false)

	said := strings.Join(r.tracker.Comments("mw-j0f2d.28"), "\n")
	if !strings.Contains(said, application.FilesOutsideTheKey+" docs/notes.md") || strings.Contains(said, "server/a.go,") {
		t.Errorf("expected mw-j0f2d.28 to be told of docs/notes.md alone as outside its key, got %q", said)
	}
	if said := strings.Join(r.tracker.Comments("mw-j0f2d.29"), "\n"); strings.Contains(said, application.FilesOutsideTheKey) {
		t.Errorf("expected nothing said of mw-j0f2d.29, whose branch kept to its key, got %q", said)
	}
	if !slices.Contains(asked, "origin/main..origin/mw/mw-j0f2d.28") {
		t.Errorf("expected the branch read against the batch's base, not the target it now holds, got %q", asked)
	}
}

func TestABatchLandingStagesTheBackendAStoryChangedAtTheBatchCommitWithItsHandsStep(t *testing.T) {
	r, nr, _ := aBatchLandedOnPostern(t, nil, true)

	if len(r.builds.Built) == 0 || r.builds.Built[0].Commit != landedCommit {
		t.Fatalf("expected the backend built at the batch's commit, got %+v (notes %q)", r.builds.Built, nr.Notes)
	}
	if !slices.ContainsFunc(r.builds.Asked, func(a string) bool { return strings.Contains(a, "origin/main...origin/mw/mw-j0f2d.28 server") }) {
		t.Errorf("expected the story's own changes under server asked against the batch's base, got %q", r.builds.Asked)
	}
	filed := swapBeadsFiled(t, r)
	if len(filed) != 1 {
		t.Fatalf("expected one swap bead for the one commit, though two stories changed the backend, got %+v", filed)
	}
	steps := handsSteps(t, r.tracker, filed[0].Story.ID)
	if len(steps) != 1 || steps[0].ID != "backend-4c9db71" {
		t.Fatalf("expected the hands step backend-4c9db71 on %s, got %+v", filed[0].Story.ID, steps)
	}
}

func TestABatchLandingThatChangedNoBackendStagesNothing(t *testing.T) {
	r, _, _ := aBatchLandedOnPostern(t, nil, false)
	if len(r.builds.Built) != 0 || len(swapBeadsFiled(t, r)) != 0 {
		t.Errorf("expected nothing built or filed, got %+v", r.builds.Built)
	}
}

// A story a batch landing lands is given what a single landing gives it after
// the push (mw-gq6.966): the benchmark of the gate it landed under, in its result
// file, and its formula closed, root and steps, with nothing left open.

// aBatchLandedAfterItsGate runs the batch's gate, which takes five minutes and
// ten seconds on the close-out's clock, and then records as landed the story
// mw-x.1, claimed and given a poured formula whose steps are all still open and
// a result file; it returns the vault, the tracker and the formula's root.
func aBatchLandedAfterItsGate(t *testing.T) (*fakeVault, *apptest.FakeTracker, string) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 11, 4, 16, 0, 0, 0, time.UTC)
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature", application.FormulaStep{Title: "Understand {{story}}"}, application.FormulaStep{Title: "Commit"})
	tracker.AddEpic("mw-x", domain.Path{Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude, Model: domain.ModelSonnet, Effort: domain.EffortHigh})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	mustDo(t, tracker.ClaimAs("mw-x.1", "mw@laptop", now.Add(time.Hour)))
	mustDo(t, tracker.SetStarted("mw-x.1", now.Add(-30*time.Minute)))
	poured, err := tracker.PourFormula(ctx, "tdd-feature", "mw-x.1", "A story", false)
	mustDo(t, err)
	mustDo(t, tracker.SetStoryMetadata(ctx, "mw-x.1", map[string]string{application.MoleculeField: poured.RootID}))
	detail, err := tracker.ShowStory(ctx, "mw-x.1")
	mustDo(t, err)

	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success","session_id":"s1"}`
	lands := &fakeRetryLanding{}
	var out bytes.Buffer
	land := application.BatchLand{Next: application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands, Landing: lands,
		Checks: &timedChecks{passed: true, output: "ok\n", took: 310 * time.Second, clock: &now},
		Load:   &apptest.FakeHostLoad{Reading: application.LoadReading{Load: 3.5, Cores: 20}},
		Now:    func() time.Time { return now },
		Seat:   "builder", Host: "laptop", Out: &out, Err: &out,
		Rigs: map[string]string{"lampas": "/rigs/lampas"},
	}}
	path := domain.Path{Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude, Model: domain.ModelSonnet, Effort: domain.EffortHigh}
	report := &application.BatchReport{Rig: "lampas", Number: 380, Target: "main", Base: "origin/main", Commit: "abc1234def5678",
		Merged: []application.BatchStory{application.MergedStory("mw-x.1", "merge1", detail, path)}}
	if gate := land.GateTheBatch(ctx, t.TempDir(), report); !gate.Passed {
		t.Fatalf("expected the batch's gate to pass, got %+v", gate)
	}
	land.RecordLanded(ctx, "/rigs/lampas", report, &application.NextReport{})
	return vault, tracker, poured.RootID
}

func TestAStoryABatchLandsHasTheBenchmarkOfTheBatchesGateInItsResultFile(t *testing.T) {
	vault, _, _ := aBatchLandedAfterItsGate(t)
	result := vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)]
	for _, want := range []string{`"gate_seconds":310`, `"load_at_gate":3.5`, `"cores":20`, `"landed":true`,
		`"kind":"lampas/feature/sonnet"`, `"session_id":"s1"`} {
		if !strings.Contains(result, want) {
			t.Errorf("expected the result of a batch-landed story to carry %s, got %s", want, result)
		}
	}
}

func TestAStoryABatchLandsHasItsFormulaClosedRootAndSteps(t *testing.T) {
	_, tracker, root := aBatchLandedAfterItsGate(t)
	open, err := tracker.OpenMolecule(context.Background(), root)
	mustDo(t, err)
	if open.RootID != "" || len(open.Steps) != 0 {
		t.Errorf("expected the formula %s of a batch-landed story closed, got %d steps open and root %q", root, len(open.Steps), open.RootID)
	}
}
