package application_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

const diskGB = int64(1) << 30

// aDisk is the host's disk as the guard reads it, and what freeing does to it.
type aDisk struct {
	reading application.LoadReading
	// gain is what one FreeUp gives back, in bytes.
	gain  int64
	freed int
}

func (d *aDisk) Load(context.Context) (application.LoadReading, error) { return d.reading, nil }

func (d *aDisk) FreeUp(context.Context) []string {
	d.freed++
	d.reading.RootFreeBytes += d.gain
	return []string{"host sweep: removed 2 items", "go-cache: removed 40 files", "tmp: nothing"}
}

func (d *aDisk) Largest(context.Context, int) ([]application.DiskHolder, error) {
	return []application.DiskHolder{
		{Path: "/home/mw/.cache", Bytes: 40 * diskGB}, {Path: "/home/mw/work", Bytes: 30 * diskGB},
		{Path: "/home/mw/.mw-worktrees", Bytes: 20 * diskGB}, {Path: "/home/mw/go", Bytes: 10 * diskGB},
		{Path: "/home/mw/vault", Bytes: 5 * diskGB},
	}, nil
}

// aGuardedHost is aGiveWayWorld with a disk that has rootFree bytes free, a mailbox
// and the stop floor at 5 GB.
func aGuardedHost(rootFree int64) (application.DiskGuard, *apptest.FakeGiveWayHost, *aDisk, *apptest.FakeMailbox, *time.Time) {
	way, host, _, now := aGiveWayWorld()
	way.Retry = &apptest.FakeGiveWayRetry{Host: host}
	disk := &aDisk{reading: application.LoadReading{RootKnown: true, RootFreeBytes: rootFree}}
	mailbox := apptest.NewFakeMailbox()
	return application.DiskGuard{
		Way: way, Load: disk, Free: disk, Holders: disk, Mailbox: mailbox,
		StopFreeGB: 5, Host: "desktop", Now: func() time.Time { return *now },
	}, host, disk, mailbox, now
}

func mayorsMail(t *testing.T, mailbox *apptest.FakeMailbox) []application.Message {
	t.Helper()
	inbox, err := mailbox.Inbox(context.Background(), application.MayorMailbox)
	if err != nil {
		t.Fatal(err)
	}
	return inbox
}

func guardCalls(calls []string, call string) int {
	n := 0
	for _, c := range calls {
		if c == call {
			n++
		}
	}
	return n
}

func TestDiskGuardUnderTheFloorFreezesFreesAndMailsOnce(t *testing.T) {
	guard, host, disk, mailbox, now := aGuardedHost(3 * diskGB)
	got, err := guard.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Frozen {
		t.Fatalf("result = %+v, want Frozen", got)
	}
	if want := []int{201, 203, 301, 400, 401}; !slices.Equal(host.Stopped(), want) {
		t.Fatalf("stopped %v, want %v: every Builder process but the heartbeats", host.Stopped(), want)
	}
	if host.Marker == nil || host.Marker.Disk == nil {
		t.Fatalf("marker = %+v, want one the disk guard keeps", host.Marker)
	}
	if disk.freed != 1 {
		t.Fatalf("freed %d times, want once", disk.freed)
	}
	mails := mayorsMail(t, mailbox)
	if len(mails) != 1 {
		t.Fatalf("%d mails to the Mayor, want one", len(mails))
	}
	body := mails[0].Subject + "\n" + mails[0].Body
	for _, want := range []string{"desktop", "3.0 GB", "5 GB", "host sweep: removed 2 items", "stays frozen", "/home/mw/.cache", "40.0 GB", "/home/mw/vault"} {
		if !strings.Contains(body, want) {
			t.Errorf("the mail does not say %q:\n%s", want, body)
		}
	}

	// A second tick, a minute on and still under the floor: nothing is stopped
	// twice, nothing more is freed, nothing more is mailed, and the marker is renewed.
	*now = now.Add(time.Minute)
	if got, err = guard.Run(context.Background()); err != nil || !got.Frozen {
		t.Fatalf("second tick: %+v, %v", got, err)
	}
	for _, pid := range []string{"201", "203", "301", "400", "401"} {
		if n := guardCalls(host.Calls, "stop "+pid); n != 1 {
			t.Errorf("process %s stopped %d times, want once", pid, n)
		}
	}
	if disk.freed != 1 {
		t.Errorf("freed %d times after the second tick, want still once", disk.freed)
	}
	if len(mayorsMail(t, mailbox)) != 1 {
		t.Errorf("a second tick mailed again")
	}
	if !host.Marker.Seen.Equal(*now) {
		t.Errorf("marker seen %v, want renewed to %v", host.Marker.Seen, *now)
	}
}

func TestDiskGuardLiftsBackOverTheFloorPlusFiveAndHoldsBetween(t *testing.T) {
	guard, host, disk, _, now := aGuardedHost(3 * diskGB)
	ctx := context.Background()
	if _, err := guard.Run(ctx); err != nil {
		t.Fatal(err)
	}

	// Over the floor and under floor plus 5: still frozen.
	disk.reading.RootFreeBytes = 7 * diskGB
	*now = now.Add(time.Minute)
	got, err := guard.Run(ctx)
	if err != nil || !got.Frozen || host.Marker == nil || len(host.Stopped()) != 5 {
		t.Fatalf("at 7 GB: %+v, %v, marker %+v, stopped %v: want still frozen", got, err, host.Marker, host.Stopped())
	}

	// A session died while it was frozen.
	host.PaneList = []application.GiveWayPane{{Session: "mw-b_2", Pid: 300}, {Session: "0", Pid: 600}}

	disk.reading.RootFreeBytes = 10 * diskGB
	*now = now.Add(time.Minute)
	got, err = guard.Run(ctx)
	if err != nil || got.Frozen || !got.Lifted {
		t.Fatalf("at 10 GB: %+v, %v: want lifted", got, err)
	}
	if host.Marker != nil || len(host.Stopped()) != 0 {
		t.Fatalf("marker %+v, stopped %v: want both gone", host.Marker, host.Stopped())
	}
	for _, line := range host.Order {
		if line == "retry mw-a.1" {
			return
		}
	}
	t.Fatalf("order %q: the session that died while frozen was not retried", host.Order)
}

func TestDiskGuardLiftsAtOnceWhenFreeingIsEnough(t *testing.T) {
	guard, host, disk, mailbox, _ := aGuardedHost(3 * diskGB)
	disk.gain = 20 * diskGB
	got, err := guard.Run(context.Background())
	if err != nil || got.Frozen || !got.Lifted {
		t.Fatalf("%+v, %v: want frozen and lifted in the one tick", got, err)
	}
	if host.Marker != nil || guardCalls(host.Calls, "stop 201") != 1 || guardCalls(host.Calls, "cont 201") != 1 {
		t.Fatalf("marker %+v, calls %v", host.Marker, host.Calls)
	}
	mails := mayorsMail(t, mailbox)
	if len(mails) != 1 || strings.Contains(mails[0].Body, "stays frozen") || strings.Contains(mails[0].Body, "/home/mw/.cache") {
		t.Fatalf("mails %+v: want one that says it was freed and names no holders", mails)
	}
}

func TestDiskGuardLeavesAPersonsGivingWayToHer(t *testing.T) {
	guard, host, disk, mailbox, now := aGuardedHost(3 * diskGB)
	ctx := context.Background()
	hers := application.GiveWayMarker{Since: *now, By: "mw@desktop", Seen: *now, Stopped: []application.GiveWayStopped{{Pid: 201, Start: "t201"}}}
	host.Marker = &hers
	if _, err := guard.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if host.Marker == nil || host.Marker.Disk != nil || host.Marker.Since != hers.Since || len(host.Calls) != 0 || disk.freed != 0 || len(mayorsMail(t, mailbox)) != 0 {
		t.Fatalf("under the floor with her marker: marker %+v, calls %v, freed %d: want it all untouched", host.Marker, host.Calls, disk.freed)
	}
	disk.reading.RootFreeBytes = 50 * diskGB
	if _, err := guard.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if host.Marker == nil || len(host.Calls) != 0 {
		t.Fatalf("back over the floor: marker %+v, calls %v: the guard lifted what is hers", host.Marker, host.Calls)
	}
}

func TestDiskGuardDoesNotLiftWhatAPersonTookOver(t *testing.T) {
	guard, host, disk, _, now := aGuardedHost(3 * diskGB)
	ctx := context.Background()
	if _, err := guard.Run(ctx); err != nil {
		t.Fatal(err)
	}
	// She gives way on top of the guard's freeze.
	way := guard.Way
	way.By = "mw@desktop"
	if err := way.On(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	if host.Marker == nil || host.Marker.Disk != nil {
		t.Fatalf("marker %+v: want hers, with no disk episode", host.Marker)
	}
	disk.reading.RootFreeBytes = 50 * diskGB
	*now = now.Add(time.Minute)
	if _, err := guard.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if host.Marker == nil || len(host.Stopped()) != 5 {
		t.Fatalf("marker %+v, stopped %v: the guard lifted a freeze she took over", host.Marker, host.Stopped())
	}
}

func TestDiskGuardReadsTheWindowsDriveOfAWSLHost(t *testing.T) {
	guard, host, disk, _, _ := aGuardedHost(100 * diskGB)
	disk.reading.WSL, disk.reading.DriveKnown, disk.reading.DriveName, disk.reading.DriveFreeBytes = true, true, "D:", 2*diskGB
	got, err := guard.Run(context.Background())
	if err != nil || !got.Frozen || host.Marker == nil {
		t.Fatalf("%+v, %v, marker %+v: want frozen for D: at 2 GB", got, err, host.Marker)
	}
}

func TestDiskGuardDoesNothingWhenOffOrBlind(t *testing.T) {
	for name, tc := range map[string]struct {
		floor int64
		read  application.LoadReading
	}{
		"stop_free_gb = 0": {0, application.LoadReading{RootKnown: true, RootFreeBytes: diskGB}},
		"nothing read":     {5, application.LoadReading{}},
	} {
		guard, host, disk, mailbox, _ := aGuardedHost(0)
		guard.StopFreeGB = tc.floor
		disk.reading = tc.read
		got, err := guard.Run(context.Background())
		if err != nil || got.Frozen || host.Marker != nil || len(host.Calls) != 0 || len(mayorsMail(t, mailbox)) != 0 {
			t.Errorf("%s: %+v, %v, marker %+v, calls %v: want nothing done", name, got, err, host.Marker, host.Calls)
		}
	}
}

func TestGiveWayStatusSaysFrozenForItsDisk(t *testing.T) {
	guard, host, _, _, _ := aGuardedHost(3 * diskGB)
	if _, err := guard.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	way := guard.Way
	way.Out = &out
	if err := way.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "frozen for its disk since 2026-11-13T16:40:00Z") || strings.Contains(out.String(), "gives way since") {
		t.Fatalf("status says %q, want 'frozen for its disk since <time>' and not 'gives way since'", out.String())
	}

	person := application.GiveWayMarker{Since: guard.Now(), By: "mw@desktop", Seen: guard.Now()}
	host.Marker = &person
	out.Reset()
	if err := way.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "gives way since") || strings.Contains(out.String(), "frozen for its disk") {
		t.Fatalf("status says %q for a person's marker, want 'gives way since' and no disk", out.String())
	}
}
