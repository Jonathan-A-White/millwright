package application_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.1617: the home's nightly suite and nightly timing run wait for the home's
// calm and its one gate lock; while the home is busy and another host has room,
// they run there instead.

func busyHome() application.LoadReading { return application.LoadReading{Load: 26, Cores: 20} }

// aNightlySuite is the home's nightly suite of lampas at 09:00 UTC with a host
// book, the home's load and gate lock, and the tracker its claim is kept in.
type aNightlySuite struct {
	t         *testing.T
	job       application.SpringJob
	elsewhere *apptest.FakeNightlyElsewhere
	load      *apptest.FakeHostLoad
	holder    string
	local     []string
	tracker   *apptest.FakeTracker
	rules     *apptest.FakeEpicRules
	now       time.Time
}

func newNightlySuite(t *testing.T) *aNightlySuite {
	t.Helper()
	a := &aNightlySuite{
		t:         t,
		elsewhere: &apptest.FakeNightlyElsewhere{},
		load:      &apptest.FakeHostLoad{Reading: application.LoadReading{Load: 3, Cores: 20}},
		tracker:   apptest.NewFakeTracker(),
		rules:     apptest.NewFakeEpicRules(),
		now:       time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC),
	}
	a.rules.Require("lampas", domain.EpicRequirements{NightlySuite: true})
	suite := &application.NightlySuite{
		Hour: 8, Rigs: map[string]string{"lampas": "/rigs/lampas"}, Host: "desktop",
		Rules: a.rules, History: &apptest.FakeSuiteRecords{},
		Home: func(context.Context) (bool, error) { return true, nil },
		Run:  func(_ context.Context, rig string) error { a.local = append(a.local, rig); return nil },
		Now:  func() time.Time { return a.now },
		Where: application.NightlyPlace{
			Elsewhere: a.elsewhere, Load: a.load, GateHolder: func() string { return a.holder },
			Host: "desktop", Notes: a.tracker, Now: func() time.Time { return a.now },
		},
	}
	a.job = suite.Job()
	return a
}

func (a *aNightlySuite) run() {
	a.t.Helper()
	mustDo(a.t, a.job.Run(context.Background()))
}

func TestTheNightlySuiteOfABusyHomeRunsOnTheFreestBoxWithRoom(t *testing.T) {
	a := newNightlySuite(t)
	a.load.Reading = busyHome()
	a.elsewhere.Hosts = []application.BatchRoom{free("cloud2", 6), free("cloud14", 0.5)}
	a.run()
	if got, want := a.elsewhere.Ran(), []string{"cloud14 suite lampas"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected the suite run on the freest box, got %v", got)
	}
	if len(a.local) != 0 {
		t.Fatalf("expected the home not to run what cloud14 does, got %v", a.local)
	}
}

func TestTheNightlySuiteOfAHomeHoldingItsGateLockForAnotherRigRunsOnABox(t *testing.T) {
	a := newNightlySuite(t)
	a.holder = "mw-6ww.185.18 check" // a close-out gate, a home under its cores
	a.elsewhere.Hosts = []application.BatchRoom{free("cloud9", 2)}
	a.run()
	if got, want := a.elsewhere.Ran(), []string{"cloud9 suite lampas"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected the suite run on cloud9, got %v", got)
	}
	if len(a.local) != 0 {
		t.Fatalf("expected the home to wait for nothing, got %v", a.local)
	}
}

func TestTheNightlySuiteWithNoBoxWithRoomRunsOnTheHomeAsBefore(t *testing.T) {
	a := newNightlySuite(t)
	a.load.Reading = busyHome()
	a.holder = "mw-6ww.185.18 check"
	a.elsewhere.Hosts = []application.BatchRoom{
		{Host: "cloud2", HasRig: true, Load: application.LoadReading{Load: 9, Cores: 8}},                           // over its cores
		{Host: "cloud9", HasRig: true, GateHeldBy: "mw-x check", Load: application.LoadReading{Load: 1, Cores: 8}}, // gate held
		{Host: "cloud14", Load: application.LoadReading{Load: 1, Cores: 8}},                                        // rig not checked out
		{Host: "cloud3", HasRig: true, Trouble: "ssh: no route", Load: application.LoadReading{Load: 1, Cores: 8}}, // could not be asked
	}
	a.run()
	if got := a.elsewhere.Ran(); len(got) != 0 {
		t.Fatalf("expected nothing run elsewhere, got %v", got)
	}
	if !reflect.DeepEqual(a.local, []string{"lampas"}) {
		t.Fatalf("expected the home to run the suite, got %v", a.local)
	}
}

func TestTheNightlySuiteOfACalmHomeRunsOnTheHomeWhateverTheBoxesSay(t *testing.T) {
	a := newNightlySuite(t)
	a.elsewhere.Hosts = []application.BatchRoom{free("cloud9", 0.1)}
	a.run()
	if got := a.elsewhere.Ran(); len(got) != 0 {
		t.Fatalf("expected a calm home to run its own suite, got %v", got)
	}
	if !reflect.DeepEqual(a.local, []string{"lampas"}) {
		t.Fatalf("expected the home to run the suite, got %v", a.local)
	}
}

func TestTheNightlySuiteIsNeverHandedToABoxWhenTheHostBookCannotBeRead(t *testing.T) {
	a := newNightlySuite(t)
	a.load.Reading = busyHome()
	a.elsewhere.Err = context.DeadlineExceeded
	a.run()
	if !reflect.DeepEqual(a.local, []string{"lampas"}) {
		t.Fatalf("expected the home to run the suite, got %v", a.local)
	}
}

func TestASuiteHostKeepsItsOwnSuiteWhileItIsBusy(t *testing.T) {
	a := newNightlySuite(t)
	a.rules.Require("lampas", domain.EpicRequirements{NightlySuite: true, SuiteEvery: time.Hour, SuiteHost: "desktop"})
	a.load.Reading = busyHome()
	a.elsewhere.Hosts = []application.BatchRoom{free("cloud9", 0.1)}
	a.run()
	if got := a.elsewhere.Ran(); len(got) != 0 {
		t.Fatalf("a suite host's suite is its own, but %v ran elsewhere", got)
	}
	if !reflect.DeepEqual(a.local, []string{"lampas"}) {
		t.Fatalf("expected the suite host to run its suite, got %v", a.local)
	}
}

func TestAnAwayNightlySuiteIsClaimedOnItsBoxWhileItRunsAndGivenBackAfter(t *testing.T) {
	a := newNightlySuite(t)
	a.load.Reading = busyHome()
	a.elsewhere.Hosts = []application.BatchRoom{free("cloud9", 2)}
	var during string
	a.elsewhere.OnRun = func(host, kind, rig string) {
		during, _ = application.NightlyRunsOn(context.Background(), a.tracker, kind, rig, a.now)
	}
	a.run()
	if during != "cloud9" {
		t.Fatalf("expected the claim to name cloud9 while the suite ran, got %q", during)
	}
	if host, running := application.NightlyRunsOn(context.Background(), a.tracker, application.NightlySuiteKind, "lampas", a.now); running {
		t.Fatalf("expected the claim given back, but the suite runs on %q", host)
	}
}

func TestAClaimThatRanOutIsNoRunOnTheBox(t *testing.T) {
	a := newNightlySuite(t)
	a.load.Reading = busyHome()
	a.elsewhere.Hosts = []application.BatchRoom{free("cloud9", 2)}
	a.elsewhere.OnRun = func(string, string, string) {} // the claim stays, as a follower killed in the run leaves it
	a.run()
	_ = a.tracker.SetNote(context.Background(), application.NightlyRunKey("suite", "lampas"),
		application.CrossSlotLease{Holder: "x", Host: "cloud9", Token: "t", Taken: a.now, Expires: a.now.Add(5 * time.Minute)}.Note())
	if _, running := application.NightlyRunsOn(context.Background(), a.tracker, "suite", "lampas", a.now.Add(6*time.Minute)); running {
		t.Fatal("a lease past its expiry was taken for a run")
	}
}

func TestAStatusReportSaysWhereTheNightlyRunsAre(t *testing.T) {
	report := application.StatusReport{Host: "desktop", NightlyOn: []application.NightlyRun{
		{Kind: application.NightlySuiteKind, Rig: "lampas", Host: "cloud9"},
		{Kind: application.NightlyTimingKind, Rig: "lampas", Host: "cloud14"},
	}}
	text := report.String()
	for _, want := range []string{"suite lampas on cloud9", "timing lampas on cloud14"} {
		if !strings.Contains(text, want) {
			t.Errorf("the report does not say %q:\n%s", want, text)
		}
	}
}

// the nightly timing run

func (f *timingNight) place(elsewhere *apptest.FakeNightlyElsewhere, holder func() string) {
	f.night.Where = application.NightlyPlace{
		Elsewhere: elsewhere, Load: f.load, GateHolder: holder, Host: "desktop",
		Notes: f.tracker, Now: f.night.Now, Out: f.out,
	}
}

func TestTheNightlyTimingRunOfABusyHomeRunsOnAFreeBox(t *testing.T) {
	f := newTimingNight(t)
	elsewhere := &apptest.FakeNightlyElsewhere{Hosts: []application.BatchRoom{free("cloud2", 6), free("cloud9", 1)}}
	f.load.Reading = busyHome()
	f.place(elsewhere, func() string { return "" })
	mustDo(t, f.night.Job().Run(context.Background()))
	if got, want := elsewhere.Ran(), []string{"cloud9 timing lampas"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected the timing run on cloud9, got %v", got)
	}
	if ran := f.runner.Ran(); len(ran) != 0 {
		t.Fatalf("expected the home to run no tests, got %+v", ran)
	}
	if got := f.due(t); len(got) != 0 {
		t.Fatalf("expected the rig tried for the day, but it is due again: %v", got)
	}
}

func TestTheNightlyTimingRunOfAHomeHoldingItsGateLockRunsOnAFreeBox(t *testing.T) {
	f := newTimingNight(t)
	elsewhere := &apptest.FakeNightlyElsewhere{Hosts: []application.BatchRoom{free("cloud9", 1)}}
	f.place(elsewhere, func() string { return "suite lampas" })
	mustDo(t, f.night.Job().Run(context.Background()))
	if got, want := elsewhere.Ran(), []string{"cloud9 timing lampas"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected the timing run on cloud9, got %v", got)
	}
}

func TestTheNightlyTimingRunWithNoBoxWithRoomRunsOnTheHomeAsBefore(t *testing.T) {
	f := newTimingNight(t)
	elsewhere := &apptest.FakeNightlyElsewhere{Hosts: []application.BatchRoom{
		{Host: "cloud2", HasRig: true, Load: application.LoadReading{Load: 9, Cores: 8}},
	}}
	f.place(elsewhere, func() string { return "mw-x check" })
	mustDo(t, f.night.Job().Run(context.Background()))
	if got := elsewhere.Ran(); len(got) != 0 {
		t.Fatalf("expected nothing run elsewhere, got %v", got)
	}
	if ran := f.runner.Ran(); len(ran) != 1 {
		t.Fatalf("expected the home to run the tests once, got %+v", ran)
	}
}

func TestTheNightlyTimingRunOfACalmHomeRunsOnTheHome(t *testing.T) {
	f := newTimingNight(t)
	elsewhere := &apptest.FakeNightlyElsewhere{Hosts: []application.BatchRoom{free("cloud9", 0.1)}}
	f.place(elsewhere, func() string { return "" })
	mustDo(t, f.night.Job().Run(context.Background()))
	if got := elsewhere.Ran(); len(got) != 0 {
		t.Fatalf("expected a calm home to run its own tests, got %v", got)
	}
	if ran := f.runner.Ran(); len(ran) != 1 {
		t.Fatalf("expected the home to run the tests once, got %+v", ran)
	}
}

func TestARunOnABoxThatFailsFailsTheTimingPassNamingTheRig(t *testing.T) {
	f := newTimingNight(t)
	elsewhere := &apptest.FakeNightlyElsewhere{Hosts: []application.BatchRoom{free("cloud9", 1)}, RunErr: context.DeadlineExceeded}
	f.place(elsewhere, func() string { return "mw-x check" })
	err := f.night.Job().Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "lampas") || !strings.Contains(err.Error(), "cloud9") {
		t.Fatalf("expected a failure naming lampas and cloud9, got %v", err)
	}
}

// mw timing <rig>, which a busy home runs on the host with room, makes the run
// there and then, and hands it to no one: not when it is not the home, not when
// the host is busy, not when the rig's last night passed at this commit.
func TestAnExplicitTimingRunRunsHereWhateverTheHomeAndTheDayAndTheLoadSay(t *testing.T) {
	f := newTimingNight(t)
	f.home = false
	f.passedAt(t, "2026-10-21", "bbb") // today's passing night, at main's head: not due
	elsewhere := &apptest.FakeNightlyElsewhere{Hosts: []application.BatchRoom{free("cloud2", 0.1)}}
	f.place(elsewhere, func() string { return "mw-x check" }) // a busy home: its gate lock is held
	if _, err := f.night.RunNow(context.Background(), "lampas"); err != nil {
		t.Fatal(err)
	}
	if got := elsewhere.Ran(); len(got) != 0 {
		t.Fatalf("expected the run made here, but %v ran elsewhere", got)
	}
	if ran := f.runner.Ran(); len(ran) == 0 {
		t.Fatal("expected the timing tests run on this host")
	}
}

func TestAnExplicitTimingRunOfARigWithNoTimingTestsIsRefused(t *testing.T) {
	f := newTimingNight(t)
	f.rules.Require("lampas", domain.EpicRequirements{SuiteEpic: "mw-suite"})
	if _, err := f.night.RunNow(context.Background(), "lampas"); err == nil || !strings.Contains(err.Error(), "timing_tests") {
		t.Fatalf("expected a refusal naming timing_tests, got %v", err)
	}
	if _, err := f.night.RunNow(context.Background(), "nowhere"); err == nil {
		t.Fatal("expected a rig this host does not have to be refused")
	}
}

// mw-gq6.1621: a box whose mw is older than the home's has no mw timing, so it says
// no "timing: yes" line to mw land --room, and a timing run is never handed to it.
func TestATimingRunIsHandedOnlyToABoxThatHasMwTiming(t *testing.T) {
	older := func(host string, load float64) application.BatchRoom {
		room := free(host, load)
		room.HasTiming = false // what an mw from before mw timing says: no timing line
		return room
	}
	t.Run("the freest box is passed over when its mw is older", func(t *testing.T) {
		f := newTimingNight(t)
		elsewhere := &apptest.FakeNightlyElsewhere{Hosts: []application.BatchRoom{older("cloud2", 0.1), free("cloud9", 4)}}
		f.place(elsewhere, func() string { return "mw-x check" })
		mustDo(t, f.night.Job().Run(context.Background()))
		if got, want := elsewhere.Ran(), []string{"cloud9 timing lampas"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("expected the timing run on cloud9, which has mw timing, got %v", got)
		}
	})
	t.Run("with only older boxes the home runs it as before", func(t *testing.T) {
		f := newTimingNight(t)
		elsewhere := &apptest.FakeNightlyElsewhere{Hosts: []application.BatchRoom{older("cloud2", 0.1)}}
		f.place(elsewhere, func() string { return "mw-x check" })
		mustDo(t, f.night.Job().Run(context.Background()))
		if got := elsewhere.Ran(); len(got) != 0 {
			t.Fatalf("expected nothing handed to a box with no mw timing, got %v", got)
		}
		if ran := f.runner.Ran(); len(ran) != 1 {
			t.Fatalf("expected the home to run the tests once, got %+v", ran)
		}
	})
	t.Run("the suite still goes to an older box, which has mw suite --file", func(t *testing.T) {
		a := newNightlySuite(t)
		a.load.Reading = busyHome()
		room := free("cloud2", 0.1)
		room.HasTiming = false
		a.elsewhere.Hosts = []application.BatchRoom{room}
		a.run()
		if got, want := a.elsewhere.Ran(), []string{"cloud2 suite lampas"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("expected the suite run on cloud2, got %v", got)
		}
	})
}

// What mw land --room prints says whether the box has mw timing; an mw that
// predates the line prints none, and is read as a box without it.
func TestAnOlderBoxsRoomSaysNoTiming(t *testing.T) {
	said := application.BatchRoom{HasRig: true, HasTiming: true, Load: application.LoadReading{Load: 1, Cores: 8}}.String()
	got, err := application.ParseBatchRoom("cloud9", said)
	if err != nil || !got.HasTiming {
		t.Fatalf("expected a box that says timing: yes to have mw timing, got %+v, %v from %q", got, err, said)
	}
	older := "rig: yes\ngate: free\nload: 1 8 0\n"
	got, err = application.ParseBatchRoom("cloud2", older)
	if err != nil || got.HasTiming || !got.HasRig {
		t.Fatalf("expected an older box's room read, without mw timing, got %+v, %v", got, err)
	}
}

// mw status reads the claim a busy home wrote when it handed a nightly run to a
// box, and says where the suite and the timing run are (mw-gq6.1617).
func TestStatusSaysWhereTheNightlySuiteAndTimingRunOfARigAre(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("ep", domain.Path{Rig: "lampas", Branch: "main", Host: "desktop"})
	rules := apptest.NewFakeEpicRules()
	rules.Require("lampas", domain.EpicRequirements{NightlySuite: true, TimingTests: []string{"tests/timing/**"}})
	rules.Require("lampshade", domain.EpicRequirements{NightlySuite: true}) // no claim: it runs on the home
	now := time.Date(2026, 11, 21, 9, 30, 0, 0, time.UTC)
	claim := func(kind, host string, expires time.Time) {
		mustDo(t, tracker.SetNote(context.Background(), application.NightlyRunKey(kind, "lampas"),
			application.CrossSlotLease{Holder: "the nightly " + kind, Host: host, Token: "t", Taken: now, Expires: expires}.Note()))
	}
	claim(application.NightlySuiteKind, "cloud9", now.Add(4*time.Minute))
	claim(application.NightlyTimingKind, "cloud14", now.Add(-time.Minute)) // ran out: its host went away
	report, err := application.Status{
		Tracker: tracker, Notes: tracker, Host: "desktop", Seat: "builder",
		Rigs: map[string]string{"lampas": "/rigs/lampas", "lampshade": "/rigs/lampshade"}, Rules: rules, Suites: &apptest.FakeSuiteRecords{},
		Now: func() time.Time { return now },
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []application.NightlyRun{{Kind: "suite", Rig: "lampas", Host: "cloud9"}}; !reflect.DeepEqual(report.NightlyOn, want) {
		t.Fatalf("expected only the suite on cloud9, got %+v", report.NightlyOn)
	}
	if text := report.String(); !strings.Contains(text, "suite lampas on cloud9") || strings.Contains(text, "timing lampas on") {
		t.Fatalf("expected the report to say the suite is on cloud9 and no timing run is away:\n%s", text)
	}
}

// mw-gq6.1639: a result the box's state still holds from the night before is not
// tonight's: the run begun elsewhere is told the slot it must have begun in.
func TestTheNightlySuiteHandedToABoxIsToldTheSlotAnOwedResultMustHaveBegunIn(t *testing.T) {
	a := newNightlySuite(t)
	a.holder = "mw-6ww.185.18 check"
	a.elsewhere.Hosts = []application.BatchRoom{free("cloud19", 2)}
	a.run()
	if got, want := a.elsewhere.Ran(), []string{"cloud19 suite lampas"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected the suite run on cloud19, got %v", got)
	}
	want := []time.Time{time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)}
	if got := a.elsewhere.NotBefore(); !reflect.DeepEqual(got, want) {
		t.Fatalf("expected the run told to take no result of a run begun before %v, got %v", want, got)
	}
}

func TestTheNightlySuiteRunOnTheHomeIsToldTheSlotAnOwedResultMustHaveBegunIn(t *testing.T) {
	var got time.Time
	rules := apptest.NewFakeEpicRules()
	rules.Require("lampas", domain.EpicRequirements{NightlySuite: true})
	suite := &application.NightlySuite{
		Hour: 8, Rigs: map[string]string{"lampas": "/rigs/lampas"}, Host: "desktop",
		Rules: rules, History: &apptest.FakeSuiteRecords{},
		Home: func(context.Context) (bool, error) { return true, nil },
		Run:  func(ctx context.Context, _ string) error { got, _ = application.RunNotBefore(ctx); return nil },
		Now:  func() time.Time { return time.Date(2026, 11, 22, 8, 0, 6, 0, time.UTC) },
	}
	mustDo(t, suite.Job().Run(context.Background()))
	if want := time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("expected the run told the slot began at %v, got %v", want, got)
	}
}

func TestTheNightlyTimingRunHandedToABoxIsToldTheDayAnOwedResultMustHaveBegunIn(t *testing.T) {
	f := newTimingNight(t)
	elsewhere := &apptest.FakeNightlyElsewhere{Hosts: []application.BatchRoom{free("cloud9", 1)}}
	f.load.Reading = busyHome()
	f.place(elsewhere, func() string { return "" })
	mustDo(t, f.night.Job().Run(context.Background()))
	if got := elsewhere.Ran(); len(got) != 1 {
		t.Fatalf("expected the timing run on cloud9, got %v", got)
	}
	got := elsewhere.NotBefore()
	if len(got) != 1 || got[0].IsZero() || got[0].Hour() != 0 || got[0].After(f.night.Now()) {
		t.Fatalf("expected the timing run told the start of its day, got %v", got)
	}
}
