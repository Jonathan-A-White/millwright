package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

var netNow = time.Date(2026, 10, 6, 0, 20, 0, 0, time.UTC)

// aNetwork is the network reader on auto, with Windows saying what cost, the
// clock pinned and a log for the events it writes.
func aNetwork(cost string) (*application.Network, *apptest.FakeNetworkProbe, *apptest.FakeNetworkStore, *apptest.FakeEventLog, *time.Time) {
	probe := &apptest.FakeNetworkProbe{Condition: application.NetworkCondition{Profile: "Whitehouse", Cost: cost}}
	store, log, now := &apptest.FakeNetworkStore{}, &apptest.FakeEventLog{}, netNow
	return &application.Network{
		Setting: application.MeteredAuto, Probe: probe, Store: store, Events: log, Host: "laptop",
		Now: func() time.Time { return now },
	}, probe, store, log, &now
}

func TestWindowsFixedAndVariableCostsAreMeteredTheRestAreNot(t *testing.T) {
	for cost, metered := range map[string]bool{
		"Fixed": true, "Variable": true,
		"Unrestricted": false, "Unknown": false, "": false,
	} {
		network, _, _, _, _ := aNetwork(cost)
		got := network.Read(context.Background())
		if got.Metered != metered {
			t.Errorf("cost %q read as metered=%v, want %v", cost, got.Metered, metered)
		}
		if got.Profile != "Whitehouse" || got.Cost != cost || got.Source != application.NetworkWindows {
			t.Errorf("cost %q read as %+v, want Windows' profile and cost kept", cost, got)
		}
	}
}

func TestNoConnectionProfileOrAFailedOrTimedOutCallIsNotMeteredAndSaysWhy(t *testing.T) {
	network, probe, _, _, _ := aNetwork("")
	probe.Condition = application.NetworkCondition{}
	if got := network.Read(context.Background()); got.Metered || got.Reason == "" {
		t.Fatalf("no profile read as %+v, want unmetered with a reason", got)
	}

	for _, failure := range []error{errors.New("exec: powershell.exe: not found"), context.DeadlineExceeded} {
		network, probe, _, _, _ := aNetwork("Fixed")
		probe.Err = failure
		got := network.Read(context.Background())
		if got.Metered {
			t.Errorf("a probe that failed with %v read as metered", failure)
		}
		if !strings.Contains(got.Reason, failure.Error()) {
			t.Errorf("the reason %q does not say %q", got.Reason, failure)
		}
	}
}

func TestAHostWithNoWindowsToAskIsNotMetered(t *testing.T) {
	got := (&application.Network{Setting: application.MeteredAuto}).Read(context.Background())
	if got.Metered || got.Reason == "" {
		t.Fatalf("read as %+v, want unmetered with a reason", got)
	}
}

func TestTheConfigOverrideDecidesWithoutAskingWindows(t *testing.T) {
	for setting, metered := range map[application.MeteredSetting]bool{application.MeteredYes: true, application.MeteredNo: false} {
		network, probe, store, log, _ := aNetwork("Unrestricted")
		network.Setting = setting
		if metered {
			probe.Condition.Cost = "Unrestricted"
		} else {
			probe.Condition.Cost = "Fixed"
		}
		got := network.Read(context.Background())
		if got.Metered != metered || got.Source != application.NetworkConfig {
			t.Errorf("metered = %q read as %+v", setting, got)
		}
		if probe.Probes != 0 || store.Saves != 0 {
			t.Errorf("metered = %q asked Windows %d times and saved %d", setting, probe.Probes, store.Saves)
		}
		if head, _ := log.Head(context.Background()); head != 0 {
			t.Errorf("metered = %q wrote %d events", setting, head)
		}
	}
}

func TestParseMeteredSetting(t *testing.T) {
	for said, want := range map[string]application.MeteredSetting{
		"": application.MeteredAuto, "auto": application.MeteredAuto, "yes": application.MeteredYes, "no": application.MeteredNo,
	} {
		if got, err := application.ParseMeteredSetting(said); err != nil || got != want {
			t.Errorf("%q parsed as %q, %v; want %q", said, got, err, want)
		}
	}
	if _, err := application.ParseMeteredSetting("maybe"); err == nil || !strings.Contains(err.Error(), "auto") {
		t.Errorf("maybe parsed with %v, want a refusal naming auto, yes and no", err)
	}
}

func TestTheAnswerIsKeptAboutAMinute(t *testing.T) {
	network, probe, _, _, now := aNetwork("Fixed")
	network.Read(context.Background())
	*now = now.Add(59 * time.Second)
	if got := network.Read(context.Background()); !got.Metered {
		t.Fatalf("the kept answer read as %+v", got)
	}
	if probe.Probes != 1 {
		t.Fatalf("Windows was asked %d times within a minute, want 1", probe.Probes)
	}
	*now = now.Add(2 * time.Second)
	probe.Condition.Cost = "Unrestricted"
	if got := network.Read(context.Background()); got.Metered {
		t.Fatalf("a minute on, the network still read as %+v", got)
	}
	if probe.Probes != 2 {
		t.Fatalf("Windows was asked %d times in all, want 2", probe.Probes)
	}
}

func TestTheStateChangeIsOneEventNotOnePerTick(t *testing.T) {
	ctx := context.Background()
	network, probe, _, log, now := aNetwork("Unrestricted")
	tick := func(cost string) {
		probe.Condition.Cost = cost
		*now = now.Add(2 * time.Minute)
		network.Read(ctx)
	}
	count := func() uint64 { head, _ := log.Head(ctx); return head }

	network.Read(ctx)
	tick("Unrestricted")
	if count() != 0 {
		t.Fatalf("an unmetered network that stayed so wrote %d events", count())
	}

	tick("Fixed")
	if count() != 1 {
		t.Fatalf("going metered wrote %d events, want 1", count())
	}
	tick("Fixed")
	tick("Variable")
	if count() != 1 {
		t.Fatalf("ticks on a metered network wrote %d events in all, want 1", count())
	}
	alarm, _ := log.Since(ctx, 0)
	if e := alarm[0]; e.Kind != events.KindJob || e.Actor != "network@laptop" || e.To != events.JobFailed || e.Lane != events.LaneEmergency ||
		!strings.Contains(e.Detail, "metered") || !strings.Contains(e.Detail, "Whitehouse") {
		t.Fatalf("the metered event is %+v", e)
	}

	tick("Unrestricted")
	tick("Unrestricted")
	if count() != 2 {
		t.Fatalf("coming back wrote %d events in all, want 2", count())
	}
	back, _ := log.Since(ctx, 1)
	if e := back[0]; e.To != events.JobDone || e.Lane != events.LaneNormal || e.Clears != 1 {
		t.Fatalf("the unmetered event is %+v, want a done job clearing seq 1", e)
	}
}

func TestACallThatFailsIsNotAChangeOfState(t *testing.T) {
	ctx := context.Background()
	network, probe, _, log, now := aNetwork("Fixed")
	network.Read(ctx)
	probe.Err = errors.New("timed out")
	*now = now.Add(2 * time.Minute)
	if got := network.Read(ctx); got.Metered {
		t.Fatalf("a failed call read as metered: %+v", got)
	}
	probe.Err = nil
	*now = now.Add(2 * time.Minute)
	network.Read(ctx)
	if head, _ := log.Head(ctx); head != 1 {
		t.Fatalf("a failed call in between wrote events: %d in all, want only the first metered one", head)
	}
}

func TestAnEventThatCouldNotBeWrittenIsTriedAgainOnTheNextReading(t *testing.T) {
	ctx := context.Background()
	network, _, _, log, now := aNetwork("Fixed")
	log.FailNext(errors.New("disk full"))
	network.Read(ctx)
	*now = now.Add(2 * time.Minute)
	network.Read(ctx)
	if head, _ := log.Head(ctx); head != 1 {
		t.Fatalf("%d events after a refused write and a retry, want 1", head)
	}
}

func TestAQuietReaderWritesNothing(t *testing.T) {
	ctx := context.Background()
	network, _, store, log, _ := aNetwork("Fixed")
	network.Quiet = true
	if got := network.Read(ctx); !got.Metered {
		t.Fatalf("read as %+v", got)
	}
	if head, _ := log.Head(ctx); head != 0 || store.Saves != 0 {
		t.Fatalf("a quiet read wrote %d events and saved %d times", head, store.Saves)
	}
}

func TestTheNetworkLineSaysWhatWindowsSaid(t *testing.T) {
	for _, c := range []struct {
		reading application.NetworkReading
		want    string
	}{
		{application.NetworkReading{Metered: true, Source: application.NetworkWindows, Profile: "Whitehouse", Cost: "Fixed"}, "NETWORK metered (Windows: Whitehouse, cost Fixed)"},
		{application.NetworkReading{Source: application.NetworkWindows, Profile: "Whitehouse", Cost: "Unrestricted"}, "NETWORK unmetered"},
		{application.NetworkReading{Metered: true, Source: application.NetworkConfig}, "NETWORK metered (config: metered = \"yes\")"},
		{application.NetworkReading{Reason: "Windows could not be asked: timed out"}, "NETWORK unmetered (Windows could not be asked: timed out)"},
	} {
		if got := c.reading.Line(); got != c.want {
			t.Errorf("line %q, want %q", got, c.want)
		}
	}
}

func TestMwStatusSaysNetworkMeteredOrUnmetered(t *testing.T) {
	for network, want := range map[*apptest.FakeNetwork]string{
		apptest.Metered():   "NETWORK metered (Windows: Whitehouse, cost Fixed)\n",
		apptest.Unmetered(): "NETWORK unmetered\n",
	} {
		tracker := apptest.NewFakeTracker()
		report, err := application.Status{Tracker: tracker, Notes: tracker, Host: "laptop", Seat: "builder", Network: network}.Run(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(report.String(), want) {
			t.Errorf("status says %q, want it to say %q", report.String(), want)
		}
	}
	tracker := apptest.NewFakeTracker()
	report, _ := application.Status{Tracker: tracker, Notes: tracker, Host: "laptop", Seat: "builder"}.Run(context.Background())
	if strings.Contains(report.String(), "NETWORK") {
		t.Errorf("a status with no network reader says %q", report.String())
	}
}

func TestSyncSkipsTheBackupOnAMeteredNetworkAndSaysSo(t *testing.T) {
	sync, _, tracker := keeping(t)
	sync.Network = apptest.Metered()
	backedUpAgo(t, tracker, 31*time.Minute)
	before := note(t, tracker, application.LastBackupKey("vps"))

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("syncing: %v", err)
	}
	if tracker.Syncs() != 0 || report.BackedUp || !report.BackupMetered {
		t.Fatalf("a metered sync ran %d remote cycles: %+v", tracker.Syncs(), report)
	}
	if got := report.String(); !strings.Contains(got, "backup skipped: metered network") {
		t.Fatalf("the sync says %q", got)
	}
	if got := note(t, tracker, application.LastBackupKey("vps")); got != before {
		t.Fatalf("the backup note moved from %q to %q: it must stay due", before, got)
	}
	if note(t, tracker, application.LastSyncKey("vps")) == "" {
		t.Fatal("the rest of the sync did not run: no note of when it was level")
	}
}

func TestSyncBacksUpOnAnUnmeteredNetwork(t *testing.T) {
	sync, _, tracker := keeping(t)
	sync.Network = apptest.Unmetered()
	backedUpAgo(t, tracker, 31*time.Minute)
	report, err := sync.Run(context.Background())
	if err != nil || tracker.Syncs() != 1 || !report.BackedUp || report.BackupMetered {
		t.Fatalf("an unmetered sync: %d cycles, %+v, %v", tracker.Syncs(), report, err)
	}
}

func TestSyncDoesNotAskTheNetworkWhileNoBackupIsDue(t *testing.T) {
	sync, _, tracker := keeping(t)
	network := apptest.Metered()
	sync.Network = network
	backedUpAgo(t, tracker, 10*time.Minute)
	report, err := sync.Run(context.Background())
	if err != nil || network.Reads != 0 || report.BackupMetered {
		t.Fatalf("a sync with no backup due asked the network %d times: %+v, %v", network.Reads, report, err)
	}
}

func TestMeteredLeavesAHostThatSyncsRemoteAlone(t *testing.T) {
	sync, _, tracker := syncing(t)
	sync.Network = apptest.Metered()
	if _, err := sync.Run(context.Background()); err != nil || tracker.Syncs() != 1 {
		t.Fatalf("a remote-mode sync on a metered network ran %d cycles, %v", tracker.Syncs(), err)
	}
}
