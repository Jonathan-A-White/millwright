package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// homeIs is a vault whose home file names host as home.
func homeIs(host string) *apptest.FakeHomeFile {
	return &apptest.FakeHomeFile{Text: host + " 2026-09-29T12:00:00Z mayor@laptop\n"}
}

// autoSync is a sync on the laptop with beads_sync = auto, the home file as
// given, and no backup interval said.
func autoSync(t *testing.T, home application.HomeFile) (application.Sync, *apptest.FakeTracker) {
	t.Helper()
	sync, _, tracker := syncing(t)
	sync.Host = "laptop"
	sync.Mode = application.BeadsSyncAuto
	sync.Home = home
	return sync, tracker
}

func TestParseBeadsSyncModeKnowsAuto(t *testing.T) {
	got, err := application.ParseBeadsSyncMode("auto")
	if err != nil || got != application.BeadsSyncAuto {
		t.Fatalf("ParseBeadsSyncMode(auto) = %q, %v", got, err)
	}
	_, err = application.ParseBeadsSyncMode("server")
	if err == nil || !strings.Contains(err.Error(), "auto") {
		t.Fatalf("expected the refusal to name auto among the choices, got %v", err)
	}
}

func TestAutoOnTheHomeActsAsBackupEveryFiveMinutesWhenNothingSaysOtherwise(t *testing.T) {
	sync, tracker := autoSync(t, homeIs("laptop"))

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if report.Mode != application.BeadsSyncBackup || !report.BackedUp {
		t.Fatalf("expected the home to act as backup and back up on its first sync, got %+v", report)
	}
	if got := tracker.Syncs(); got != 1 {
		t.Fatalf("expected one remote cycle on the first sync, got %d", got)
	}
	if !strings.Contains(report.String(), "auto: home") {
		t.Errorf("expected the report to say why, got %q", report.String())
	}

	// Two minutes on: inside the five, so no remote cycle.
	sync.Now = func() time.Time { return level.Add(2 * time.Minute) }
	report, err = sync.Run(context.Background())
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if report.BackedUp || tracker.Syncs() != 1 {
		t.Fatalf("expected no remote cycle inside 5 minutes, got %d cycles, %+v", tracker.Syncs(), report)
	}

	// Six minutes on: past the five, so one more.
	sync.Now = func() time.Time { return level.Add(6 * time.Minute) }
	if _, err = sync.Run(context.Background()); err != nil {
		t.Fatalf("third sync: %v", err)
	}
	if tracker.Syncs() != 2 {
		t.Fatalf("expected the second remote cycle once 5 minutes had passed, got %d", tracker.Syncs())
	}
}

func TestAutoOnTheHomeKeepsABackupIntervalThatIsSaid(t *testing.T) {
	sync, tracker := autoSync(t, homeIs("laptop"))
	sync.BackupInterval = 20 * time.Minute

	if _, err := sync.Run(context.Background()); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	sync.Now = func() time.Time { return level.Add(10 * time.Minute) }
	if _, err := sync.Run(context.Background()); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if tracker.Syncs() != 1 {
		t.Fatalf("expected a said interval of 20 minutes to hold, got %d cycles", tracker.Syncs())
	}
}

func TestAutoOnABoostActsAsSharedWithNoRemoteCycle(t *testing.T) {
	sync, tracker := autoSync(t, homeIs("desktop"))

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("syncing: %v", err)
	}
	if report.Mode != application.BeadsSyncShared {
		t.Fatalf("expected shared on a boost, got %+v", report)
	}
	if tracker.Syncs() != 0 {
		t.Fatalf("expected no remote cycle on a boost, got %d", tracker.Syncs())
	}
	if !strings.Contains(report.String(), "auto: boost of desktop") {
		t.Errorf("expected the report to say why, got %q", report.String())
	}
	if note(t, tracker, application.LastSyncKey("laptop")) == "" {
		t.Error("expected the note of when this host was level, written straight into the one database")
	}
}

func TestAutoWithNoHomeFileRefusesAndDoesNothing(t *testing.T) {
	sync, files, tracker := syncing(t)
	sync.Host = "laptop"
	sync.Mode = application.BeadsSyncAuto
	sync.Home = &apptest.FakeHomeFile{Missing: true}
	files.Incoming = 1

	_, err := sync.Run(context.Background())
	if err == nil {
		t.Fatal("expected auto with no home file to refuse")
	}
	if !errors.Is(err, application.ErrNoHomeFile) {
		t.Errorf("expected the refusal to carry why, got %v", err)
	}
	for _, want := range []string{"auto", "home"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected %q in the refusal, got %q", want, err)
		}
	}
	if marks, pulls, pushes := files.Moves(); tracker.Syncs() != 0 || marks != 0 || pulls != 0 || pushes != 0 {
		t.Errorf("expected nothing done: %d cycles, %d marks, %d pulls, %d pushes", tracker.Syncs(), marks, pulls, pushes)
	}
	if note(t, tracker, application.LastSyncKey("laptop")) != "" {
		t.Error("expected no note written")
	}
}

func TestAutoWithAHomeFileNobodyCanReadRefuses(t *testing.T) {
	sync, tracker := autoSync(t, &apptest.FakeHomeFile{Text: "the vps\n"})
	if _, err := sync.Run(context.Background()); err == nil {
		t.Fatal("expected a home file that is not understood to refuse")
	}
	if tracker.Syncs() != 0 {
		t.Fatal("expected nothing done")
	}
}

func TestAutoWithNoWayToReadTheHomeRefuses(t *testing.T) {
	sync, _ := autoSync(t, nil)
	if _, err := sync.Run(context.Background()); err == nil {
		t.Fatal("expected auto with no home file reader to refuse")
	}
}

func TestResolveBeadsSyncLeavesTheThreeAlone(t *testing.T) {
	for _, mode := range []application.BeadsSyncMode{application.BeadsSyncRemote, application.BeadsSyncBackup, application.BeadsSyncShared} {
		got, err := application.ResolveBeadsSync(context.Background(), &apptest.FakeHomeFile{Missing: true}, "laptop", mode)
		if err != nil || got.Mode != mode || got.Why != "" {
			t.Errorf("mode %s: got %+v, %v", mode, got, err)
		}
	}
}

func TestResolveBeadsSyncReadsAutoFromTheHome(t *testing.T) {
	got, err := application.ResolveBeadsSync(context.Background(), homeIs("laptop"), "laptop", application.BeadsSyncAuto)
	if err != nil || got.Mode != application.BeadsSyncBackup || got.Why != "auto: home" || got.Home != "laptop" {
		t.Errorf("on the home: got %+v, %v", got, err)
	}
	got, err = application.ResolveBeadsSync(context.Background(), homeIs("laptop"), "desktop", application.BeadsSyncAuto)
	if err != nil || got.Mode != application.BeadsSyncShared || got.Why != "auto: boost of laptop" || got.Home != "laptop" {
		t.Errorf("on the boost: got %+v, %v", got, err)
	}
	_, err = application.ResolveBeadsSync(context.Background(), &apptest.FakeHomeFile{Missing: true}, "desktop", application.BeadsSyncAuto)
	if _, ok := application.HomeUnknownIn(err); !ok {
		t.Errorf("expected a home that cannot be told, got %v", err)
	}
}

func TestBoostServerHostIsTheHomesNameOnWireGuardUnlessSaid(t *testing.T) {
	if got := application.BoostServerHost("laptop", ""); got != "laptop.mw" {
		t.Errorf("expected laptop.mw, got %q", got)
	}
	if got := application.BoostServerHost("laptop", "10.8.0.2"); got != "10.8.0.2" {
		t.Errorf("expected the override, got %q", got)
	}
}

func TestStatusNamesTheResolvedModeOfAuto(t *testing.T) {
	for _, tc := range []struct {
		host, home string
		want       string
	}{
		{"laptop", "laptop", "BEADS SYNC backup · never backed up · auto: home"},
		{"laptop", "desktop", "BEADS SYNC shared · auto: boost of desktop"},
	} {
		printed := autoStatus(t, homeIs(tc.home), tc.host).String()
		if !strings.Contains(printed, tc.want) {
			t.Errorf("home %s: expected %q, got:\n%s", tc.home, tc.want, printed)
		}
	}
}

func TestStatusSaysWhenAutoCannotTellTheHome(t *testing.T) {
	report := autoStatus(t, &apptest.FakeHomeFile{Missing: true}, "laptop")
	printed := report.String()
	if !strings.Contains(printed, "BEADS SYNC auto · home unknown") {
		t.Errorf("expected auto to say the home is unknown, got:\n%s", printed)
	}
	for _, line := range strings.Split(printed, "\n") {
		if n := utf8.RuneCountInString(line); n > application.Width {
			t.Errorf("%d columns in %q", n, line)
		}
	}
}

func autoStatus(t *testing.T, home application.HomeFile, host string) application.StatusReport {
	t.Helper()
	tracker := aTrackerPathedToVPS(t)
	report, err := application.Status{
		Tracker:     tracker,
		Notes:       tracker,
		Host:        host,
		Seat:        "builder",
		SyncMode:    application.BeadsSyncAuto,
		Home:        home,
		HostSilence: 2 * time.Hour,
		Now:         func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	return report
}
