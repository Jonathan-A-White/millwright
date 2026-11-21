package application_test

import (
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
)

// mw-gq6.1616: a batch is due and the home is busy or holds its gate lock for
// another rig's check: another host that has the rig, whose gate is free and
// whose load is under its cores, lands it over ssh instead of the home waiting.

// aBatchWithRoomElsewhere is the batch-land job on the home (desktop) with a due
// batch of lampas, a host book, and the home's own load and gate lock.
type aBatchWithRoomElsewhere struct {
	t         *testing.T
	job       application.SpringJob
	elsewhere *apptest.FakeBatchElsewhere
	load      *apptest.FakeHostLoad
	holder    string
	local     []string
	tracker   *apptest.FakeTracker
	landings  *application.BatchLandings
	now       time.Time
}

func newBatchWithRoomElsewhere(t *testing.T) *aBatchWithRoomElsewhere {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	rules := apptest.NewFakeEpicRules()
	now := time.Date(2026, 11, 21, 21, 0, 0, 0, time.UTC)
	tracker.AddEpic("e-lampas", domain.Path{Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Host: "desktop"})
	rules.Require("lampas", domain.EpicRequirements{Landing: domain.LandingSettings{Mode: domain.LandingBatch}})
	ctx := context.Background()
	for _, id := range []string{"lp-1", "lp-2"} {
		tracker.AddStory("e-lampas", domain.Story{ID: id, Title: "The story " + id})
		mustDo(t, tracker.ClaimStory(ctx, id))
		queue(t, tracker, id)
		mustDo(t, tracker.SetStoryMetadata(ctx, id, map[string]string{
			application.QueuedAtField: now.Add(-2 * time.Hour).Format(time.RFC3339),
		}))
	}
	a := &aBatchWithRoomElsewhere{
		t:         t,
		elsewhere: &apptest.FakeBatchElsewhere{},
		load:      &apptest.FakeHostLoad{Reading: application.LoadReading{Load: 3, Cores: 20}},
		tracker:   tracker,
		now:       now,
	}
	landings := &application.BatchLandings{
		Rigs:    map[string]string{"lampas": "/rigs/lampas"},
		Rules:   rules,
		Tracker: tracker,
		Home:    func(context.Context) (bool, error) { return true, nil },
		Land: func(_ context.Context, rig string, one bool) error {
			a.local = append(a.local, rig)
			return nil
		},
		Notes:      tracker,
		Host:       "desktop",
		Now:        func() time.Time { return now },
		Elsewhere:  a.elsewhere,
		Load:       a.load,
		GateHolder: func() string { return a.holder },
	}
	a.landings = landings
	a.job = landings.Job()
	return a
}

func (a *aBatchWithRoomElsewhere) run() {
	a.t.Helper()
	mustDo(a.t, a.job.Run(context.Background()))
}

func free(host string, load float64) application.BatchRoom {
	return application.BatchRoom{Host: host, HasRig: true, Load: application.LoadReading{Load: load, Cores: 8}}
}

func TestADueBatchOfABusyHomeLandsOnAFreeHost(t *testing.T) {
	a := newBatchWithRoomElsewhere(t)
	a.load.Reading = application.LoadReading{Load: 26, Cores: 20} // the Laptop at 21-31 of 20
	a.elsewhere.Hosts = []application.BatchRoom{free("cloud2", 6), free("cloud14", 0.5)}
	a.run()
	if got, want := a.elsewhere.Landed(), []string{"cloud14 lampas"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected the batch landed on the freest host, got %v", got)
	}
	if len(a.local) != 0 {
		t.Fatalf("expected the home not to land what cloud14 does, got %v", a.local)
	}
}

func TestADueBatchOfAHomeHoldingItsGateLockForAnotherRigLandsOnAFreeHost(t *testing.T) {
	a := newBatchWithRoomElsewhere(t)
	a.holder = "mw-gq6.1612 check" // a millwright single-story check, a calm host
	a.elsewhere.Hosts = []application.BatchRoom{free("cloud9", 2)}
	a.run()
	if got, want := a.elsewhere.Landed(), []string{"cloud9 lampas"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected the batch landed on cloud9, got %v", got)
	}
	if len(a.local) != 0 {
		t.Fatalf("expected the home to wait for nothing, got %v", a.local)
	}
}

func TestADueBatchWithNoFreeHostKeepsTodaysWaitOnTheHome(t *testing.T) {
	a := newBatchWithRoomElsewhere(t)
	a.load.Reading = application.LoadReading{Load: 26, Cores: 20}
	a.holder = "mw-gq6.1612 check"
	a.elsewhere.Hosts = []application.BatchRoom{
		{Host: "cloud2", HasRig: true, Load: application.LoadReading{Load: 9, Cores: 8}},                              // over its cores
		{Host: "cloud9", HasRig: true, GateHeldBy: "mw-x.1 check", Load: application.LoadReading{Load: 1, Cores: 8}},  // its gate lock held
		{Host: "cloud14", HasRig: false, Load: application.LoadReading{Load: 0.2, Cores: 8}},                          // no checkout of lampas
		{Host: "boost1", HasRig: true, Trouble: "ssh: connection timed out", Load: application.LoadReading{Cores: 8}}, // could not be asked
		{Host: "cloud3", HasRig: true, Load: application.LoadReading{Cores: 0}},                                       // load not read
	}
	a.run()
	if got := a.elsewhere.Landed(); len(got) != 0 {
		t.Fatalf("expected no landing on a host with no room, got %v", got)
	}
	if want := []string{"lampas"}; !reflect.DeepEqual(a.local, want) {
		t.Fatalf("expected the home to land it as before (its wait is in mw land's gate), got %v", a.local)
	}
}

func TestADueBatchOnACalmHomeLandsThereAndAsksNoOtherHost(t *testing.T) {
	a := newBatchWithRoomElsewhere(t)
	a.elsewhere.Hosts = []application.BatchRoom{free("cloud14", 0)}
	a.run()
	if want := []string{"lampas"}; !reflect.DeepEqual(a.local, want) {
		t.Fatalf("expected the home to land it, got %v", a.local)
	}
	if len(a.elsewhere.Asked()) != 0 || len(a.elsewhere.Landed()) != 0 {
		t.Fatalf("expected no other host asked of while the home is calm, got %v", a.elsewhere.Asked())
	}
}

func TestADueBatchWhoseHostBookCannotBeReadKeepsTodaysWaitOnTheHome(t *testing.T) {
	a := newBatchWithRoomElsewhere(t)
	a.load.Reading = application.LoadReading{Load: 26, Cores: 20}
	a.elsewhere.Err = errors.New("no [cloud] table")
	a.run()
	if want := []string{"lampas"}; !reflect.DeepEqual(a.local, want) {
		t.Fatalf("expected the home to land it, got %v", a.local)
	}
}

// The batch's own landing on the other host failing is a failed landing like any
// other: the pass says so, and the rig waits batch_every before another try.
func TestABatchThatFailsOnAnotherHostIsAFailedLanding(t *testing.T) {
	a := newBatchWithRoomElsewhere(t)
	a.load.Reading = application.LoadReading{Load: 26, Cores: 20}
	a.elsewhere.Hosts = []application.BatchRoom{free("cloud14", 0.5)}
	a.elsewhere.LandErr = errors.New("mw land lampas failed (exit status 1)")
	err := a.job.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "landing lampas") {
		t.Fatalf("expected the failure said, got %v", err)
	}
	if len(a.local) != 0 {
		t.Fatalf("expected no second landing at home after a failure on cloud14, got %v", a.local)
	}
}

func TestABatchRoomIsFreeOnlyWithTheRigAGateAndLoadUnderItsCores(t *testing.T) {
	cases := []struct {
		name string
		room application.BatchRoom
		want bool
	}{
		{"free", free("cloud14", 7.9), true},
		{"at its cores", free("cloud14", 8), false},
		{"no rig", application.BatchRoom{Host: "x", Load: application.LoadReading{Load: 1, Cores: 8}}, false},
		{"gate held", application.BatchRoom{Host: "x", HasRig: true, GateHeldBy: "y", Load: application.LoadReading{Load: 1, Cores: 8}}, false},
		{"not asked", application.BatchRoom{Host: "x", HasRig: true, Trouble: "ssh failed", Load: application.LoadReading{Load: 1, Cores: 8}}, false},
		{"no cores read", application.BatchRoom{Host: "x", HasRig: true}, false},
	}
	for _, c := range cases {
		if got := c.room.Free(); got != c.want {
			t.Errorf("%s: Free() = %v, want %v", c.name, got, c.want)
		}
	}
}

// What mw land --room prints on the far end, and what the home reads of it.
func TestABatchRoomIsReadBackFromWhatTheFarEndPrints(t *testing.T) {
	said := application.BatchRoom{HasRig: true, GateHeldBy: "mw-x.1 check", Load: application.LoadReading{Load: 3.25, Cores: 8, PartWorkers: 1}}.String()
	got, err := application.ParseBatchRoom("cloud14", said)
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "cloud14" || !got.HasRig || got.GateHeldBy != "mw-x.1 check" || got.Load.Load != 3.25 || got.Load.Cores != 8 || got.Load.PartWorkers != 1 {
		t.Fatalf("expected the room read back, got %+v from %q", got, said)
	}
	if _, err := application.ParseBatchRoom("cloud14", "Welcome to Ubuntu\n"); err == nil {
		t.Fatal("expected a banner that says nothing of a room to be an error, not a free host")
	}
}

// mw-gq6.1618: a drain (landing = each, mw land <rig> --one) and a held batch
// landed again in parts (mw land <rig> --only <ids>) are handed to a host with
// room as a whole-queue batch is, each landing of them on its own.

// landsThe is what mw land does on the far end: the stories its scope takes are
// landed, the oldest alone for --one.
func (a *aBatchWithRoomElsewhere) landsThe(scope application.LandScope) {
	ctx := context.Background()
	work, err := a.tracker.WorkInHand(ctx)
	mustDo(a.t, err)
	for _, d := range work.Running {
		if !application.QueuedForBatch(d) {
			continue
		}
		if len(scope.Only) > 0 && !containsString(scope.Only, d.Story.ID) {
			continue
		}
		mustDo(a.t, a.tracker.SetStoryState(ctx, d.Story.ID, application.RunState, application.RunLanded, "landed"))
		if scope.One {
			return
		}
	}
}

// aDrainOnABusyHome is a follower of lampas, which lands each story by itself,
// with lp-1 and lp-2 queued: the home is the Laptop at load 26 of 20.
func aDrainOnABusyHome(t *testing.T) *aBatchWithRoomElsewhere {
	a := newBatchWithRoomElsewhere(t)
	rules := apptest.NewFakeEpicRules()
	rules.Require("lampas", domain.EpicRequirements{Landing: domain.LandingSettings{Mode: domain.LandingEach}})
	a.landings.Rules = rules
	a.landings.Land = func(ctx context.Context, rig string, one bool) error {
		a.local = append(a.local, fmt.Sprintf("%s one=%v", rig, one))
		a.landsThe(application.LandScope{One: one})
		return nil
	}
	a.job = a.landings.Job()
	a.load.Reading = application.LoadReading{Load: 26, Cores: 20}
	a.elsewhere.OnLand = func(_, _ string, scope application.LandScope) { a.landsThe(scope) }
	return a
}

func TestBatchRoomADrainOfABusyHomeLandsEachStoryOnAFreeHost(t *testing.T) {
	a := aDrainOnABusyHome(t)
	a.elsewhere.Hosts = []application.BatchRoom{free("cloud14", 0.5)}
	a.run()
	if got, want := a.elsewhere.Landed(), []string{"cloud14 lampas --one", "cloud14 lampas --one"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected each story of the drain landed by itself on cloud14, got %v", got)
	}
	if len(a.local) != 0 {
		t.Fatalf("expected the home to land none of them, got %v", a.local)
	}
}

// The home may calm down between two landings of a drain: the next is its own.
func TestBatchRoomADrainWhoseHomeCalmsDownLandsTheRestAtHome(t *testing.T) {
	a := aDrainOnABusyHome(t)
	a.elsewhere.Hosts = []application.BatchRoom{free("cloud14", 0.5)}
	a.elsewhere.OnLand = func(_, _ string, scope application.LandScope) {
		a.landsThe(scope)
		a.load.Reading = application.LoadReading{Load: 3, Cores: 20}
	}
	a.run()
	if got, want := a.elsewhere.Landed(), []string{"cloud14 lampas --one"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected the first story landed on cloud14, got %v", got)
	}
	if want := []string{"lampas one=true"}; !reflect.DeepEqual(a.local, want) {
		t.Fatalf("expected the second landed at home, got %v", a.local)
	}
}

func TestBatchRoomADrainOfABusyHomeWithNoFreeHostKeepsTodaysWaitOnTheHome(t *testing.T) {
	a := aDrainOnABusyHome(t)
	a.elsewhere.Hosts = []application.BatchRoom{free("cloud14", 9)} // over its cores
	a.run()
	if got := a.elsewhere.Landed(); len(got) != 0 {
		t.Fatalf("expected no landing on a host with no room, got %v", got)
	}
	if want := []string{"lampas one=true", "lampas one=true"}; !reflect.DeepEqual(a.local, want) {
		t.Fatalf("expected the home to land each story itself, got %v", a.local)
	}
}

// aHeldBatchOnABusyHome is a follower of lampas with lp-1 to lp-4 queued whose
// batch 600 was held red with lp-3 as its suspect; the home is busy.
func aHeldBatchOnABusyHome(t *testing.T) (*aBatchWithRoomElsewhere, *[]string) {
	a := newBatchWithRoomElsewhere(t)
	ids := []string{"lp-1", "lp-2", "lp-3", "lp-4"}
	for _, id := range ids[2:] {
		a.tracker.AddStory("e-lampas", domain.Story{ID: id, Title: "The story " + id})
		mustDo(t, a.tracker.ClaimStory(context.Background(), id))
		queue(t, a.tracker, id)
		mustDo(t, a.tracker.SetStoryMetadata(context.Background(), id, map[string]string{
			application.QueuedAtField: a.now.Add(-2 * time.Hour).Format(time.RFC3339),
		}))
	}
	history := &apptest.FakeBatchMetrics{}
	line := heldBatch(a.now.Add(-10*time.Minute), 600, ids...)
	line.Suspect = "lp-3"
	mustDo(t, history.AppendBatch(context.Background(), line))
	a.landings.History = history
	var home []string
	a.landings.LandScoped = func(_ context.Context, rig string, scope application.LandScope) error {
		home = append(home, fmt.Sprintf("%s only=%s", rig, strings.Join(scope.Only, ",")))
		a.landsThe(scope)
		return nil
	}
	a.job = a.landings.Job()
	a.load.Reading = application.LoadReading{Load: 26, Cores: 20}
	a.elsewhere.OnLand = func(_, _ string, scope application.LandScope) { a.landsThe(scope) }
	return a, &home
}

func TestBatchRoomAHeldBatchInPartsOfABusyHomeLandsEachPartOnAFreeHost(t *testing.T) {
	a, home := aHeldBatchOnABusyHome(t)
	a.elsewhere.Hosts = []application.BatchRoom{free("cloud14", 0.5)}
	a.run()
	want := []string{"cloud14 lampas --only lp-1,lp-2,lp-4", "cloud14 lampas --only lp-3"}
	if got := a.elsewhere.Landed(); !reflect.DeepEqual(got, want) {
		t.Fatalf("expected the batch of three and then the suspect landed on cloud14, got %v", got)
	}
	if len(*home) != 0 {
		t.Fatalf("expected the home to land none of them, got %v", *home)
	}
}

func TestBatchRoomAHeldBatchInPartsOfABusyHomeWithNoFreeHostKeepsTodaysWaitOnTheHome(t *testing.T) {
	a, home := aHeldBatchOnABusyHome(t)
	a.elsewhere.Hosts = []application.BatchRoom{{Host: "cloud14", HasRig: true, GateHeldBy: "mw-x.1 check", Load: application.LoadReading{Load: 1, Cores: 8}}}
	a.run()
	if got := a.elsewhere.Landed(); len(got) != 0 {
		t.Fatalf("expected no landing on a host with no room, got %v", got)
	}
	if want := []string{"lampas only=lp-1,lp-2,lp-4", "lampas only=lp-3"}; !reflect.DeepEqual(*home, want) {
		t.Fatalf("expected the home to land the parts, got %v", *home)
	}
}
