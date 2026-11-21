package application_test

import (
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
