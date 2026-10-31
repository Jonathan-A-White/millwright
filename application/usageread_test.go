package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// usageScreen is what Claude's /usage screen printed, captured by hand
// (mw-gq6.834).
const usageScreen = `
  Settings:  Status   Config   Usage   Stats

  Current session
  ██████                                             13% used
  Resets 9:19pm (America/New_York)

  Current week (all models)
  █████████████████████████████████████████████      90% used
  Resets Oct 30, 8:59am (America/New_York)

  Current week (Fable)
  █████████████████████████                          50% used
  Resets Oct 30, 8:59am (America/New_York)

  Esc to cancel
`

const noUsageTab = `
  Settings:  Status   Config   Stats

  Version: 2.1
  Model: opus
`

func newYork(t *testing.T) *time.Location {
	t.Helper()
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no zone data")
	}
	return zone
}

func TestTheUsageScreenReadsTheWeekAndItsResetAndTheFable(t *testing.T) {
	now := time.Date(2026, 10, 29, 1, 2, 0, 0, time.UTC)
	got, err := application.ParseUsageScreen(usageScreen, now, newYork(t))
	if err != nil {
		t.Fatal(err)
	}
	if got.Percent != 90 {
		t.Errorf("the week's percent = %v, want 90", got.Percent)
	}
	if want := time.Date(2026, 10, 30, 12, 59, 0, 0, time.UTC); !got.Resets.Equal(want) {
		t.Errorf("the reset = %s, want %s", got.Resets.UTC().Format(time.RFC3339), want.Format(time.RFC3339))
	}
	if got.Fable == nil || *got.Fable != 50 {
		t.Errorf("the Fable percent = %v, want 50", got.Fable)
	}
}

func TestTheUsageScreenWithoutAFableStillReadsTheWeek(t *testing.T) {
	screen := strings.Replace(usageScreen, "Current week (Fable)", "Current week (Sonnet)", 1)
	got, err := application.ParseUsageScreen(screen, time.Date(2026, 10, 29, 1, 2, 0, 0, time.UTC), newYork(t))
	if err != nil {
		t.Fatal(err)
	}
	if got.Percent != 90 || got.Fable != nil {
		t.Errorf("read %v with Fable %v, want 90 with none", got.Percent, got.Fable)
	}
}

func TestAScreenWithNoUsageTabIsNotRead(t *testing.T) {
	_, err := application.ParseUsageScreen(noUsageTab, time.Now(), newYork(t))
	if err == nil || !strings.Contains(err.Error(), "Current week (all models)") {
		t.Fatalf("a screen with no Usage tab: %v, want an error naming the week it looked for", err)
	}
}

func TestAWeekWithoutAResetIsNotRead(t *testing.T) {
	screen := "Current week (all models)\n  90% used\n\nCurrent week (Fable)\n  50% used\n  Resets Oct 30, 8:59am (America/New_York)\n"
	if _, err := application.ParseUsageScreen(screen, time.Date(2026, 10, 29, 1, 2, 0, 0, time.UTC), newYork(t)); err == nil {
		t.Fatal("a week with no reset of its own was read from the next section's")
	}
}

func TestAResetWithOnlyATimeIsTheNextTimeItComes(t *testing.T) {
	zone := newYork(t)
	screen := "Current week (all models)\n 10% used\n Resets 9:19pm (America/New_York)\n"
	now := time.Date(2026, 10, 29, 1, 2, 0, 0, time.UTC) // 21:02 on the 28th in New York
	got, err := application.ParseUsageScreen(screen, now, zone)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 10, 29, 1, 19, 0, 0, time.UTC); !got.Resets.Equal(want) {
		t.Errorf("reset = %s, want %s", got.Resets.UTC(), want)
	}
	later := time.Date(2026, 10, 29, 2, 0, 0, 0, time.UTC) // 22:00, 9:19pm has passed
	got, err = application.ParseUsageScreen(screen, later, zone)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 10, 30, 1, 19, 0, 0, time.UTC); !got.Resets.Equal(want) {
		t.Errorf("reset = %s, want %s", got.Resets.UTC(), want)
	}
}

// fakeUsageTerminal drives a screen that prints what it is told.
type fakeUsageTerminal struct {
	screens []string // what each capture prints, the last repeated
	opened  []string
	typed   []string
	pressed []string
	closed  []string
	taken   int
	openErr error
	// failAt names the call that fails: type, press, capture.
	failAt string
	// probes are the sessions the server has that the sweep may see.
	probes   []application.UsageProbe
	probeErr error
	// onCapture, when set, runs on each capture (a cancel, say).
	onCapture func()
}

func (f *fakeUsageTerminal) fails(call string) error {
	if f.failAt == call {
		return errors.New(call + " failed")
	}
	return nil
}

func (f *fakeUsageTerminal) Probes(context.Context) ([]application.UsageProbe, error) {
	return f.probes, f.probeErr
}

func (f *fakeUsageTerminal) Open(_ context.Context, name, dir string, cols, rows int) error {
	f.opened = append(f.opened, name+" "+dir)
	return f.openErr
}
func (f *fakeUsageTerminal) Type(_ context.Context, name, text string) error {
	f.typed = append(f.typed, text)
	return f.fails("type")
}
func (f *fakeUsageTerminal) Press(_ context.Context, name, key string) error {
	f.pressed = append(f.pressed, key)
	return f.fails("press")
}
func (f *fakeUsageTerminal) Capture(context.Context, string) (string, error) {
	i := f.taken
	if i >= len(f.screens) {
		i = len(f.screens) - 1
	}
	f.taken++
	if f.onCapture != nil {
		f.onCapture()
	}
	return f.screens[i], f.fails("capture")
}
func (f *fakeUsageTerminal) Close(ctx context.Context, name string) error {
	if ctx.Err() != nil {
		return ctx.Err() // a close under a cancelled context does nothing
	}
	f.closed = append(f.closed, name)
	return nil
}

type fakeUsageLock struct {
	held  bool
	taken int
	freed int
}

func (l *fakeUsageLock) TryTake(context.Context) (func(), bool, error) {
	if l.held {
		return nil, false, nil
	}
	l.taken++
	return func() { l.freed++ }, true, nil
}
func (l *fakeUsageLock) Held(context.Context) (bool, error) { return l.held, nil }

type usageReadRig struct {
	term   *fakeUsageTerminal
	lock   *fakeUsageLock
	notes  *apptest.FakeTracker
	kept   []string
	out    bytes.Buffer
	read   application.UsageRead
	paused []string
}

func newUsageReadRig(t *testing.T, screens ...string) *usageReadRig {
	t.Helper()
	r := &usageReadRig{term: &fakeUsageTerminal{screens: screens}, lock: &fakeUsageLock{}, notes: apptest.NewFakeTracker()}
	now := time.Date(2026, 10, 29, 1, 2, 0, 0, time.UTC)
	zone := newYork(t)
	r.read = application.UsageRead{
		Terminal: r.term, Lock: r.lock, Dir: "/vault", Zone: zone,
		Keep: func(text string) (string, error) {
			r.kept = append(r.kept, text)
			return "/state/usage-read-capture.txt", nil
		},
		Wait: func(context.Context, time.Duration) error { return nil },
		Name: func() string { return "mw-usage-probe-1" },
		Now:  func() time.Time { return now },
		Set: application.UsageSet{
			Readings: application.UsageReadings{Notes: r.notes}, Floors: application.UsageFloors{Notes: r.notes}, Zone: zone,
			Now:   func() time.Time { return now },
			Pause: func(_ context.Context, reason string) error { r.paused = append(r.paused, reason); return nil },
		},
	}
	r.read.Out = &r.out
	r.read.Set.Out = &r.out
	return r
}

func TestUsageReadOpensASessionReadsTheScreenAndSetsTheUsage(t *testing.T) {
	r := newUsageReadRig(t, usageScreen)
	if err := r.read.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.term.opened) != 1 || r.term.opened[0] != "mw-usage-probe-1 /vault" {
		t.Errorf("opened %v", r.term.opened)
	}
	if got := strings.Join(r.term.typed, "|"); got != "claude|/usage" {
		t.Errorf("typed %q, want claude then /usage", got)
	}
	if len(r.term.closed) != 1 || r.term.closed[0] != "mw-usage-probe-1" {
		t.Errorf("closed %v, want the probe session closed once", r.term.closed)
	}
	all, err := application.UsageReadings{Notes: r.notes}.All(context.Background())
	if err != nil || len(all) != 1 {
		t.Fatalf("readings %v, %v", all, err)
	}
	if all[0].Percent != 90 || all[0].Fable == nil || *all[0].Fable != 50 ||
		!all[0].Resets.Equal(time.Date(2026, 10, 30, 12, 59, 0, 0, time.UTC)) {
		t.Errorf("reading kept = %+v", all[0])
	}
	if len(r.paused) != 0 {
		t.Errorf("90%% with a day and a half left is under the reserve line, but paused %v", r.paused)
	}
	if !strings.Contains(r.out.String(), "Fable 50.0%") || !strings.Contains(r.out.String(), "is under it") {
		t.Errorf("set did not report as mw usage set does:\n%s", r.out.String())
	}
	if r.lock.taken != 1 || r.lock.freed != 1 {
		t.Errorf("lock taken %d freed %d, want 1 and 1", r.lock.taken, r.lock.freed)
	}
	if len(r.kept) != 1 {
		t.Errorf("the capture was kept %d times, want once", len(r.kept))
	}
}

func TestUsageShowPrintsTheFableBesideTheReading(t *testing.T) {
	r := newUsageReadRig(t, usageScreen)
	if err := r.read.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	show := application.UsageShow{Readings: r.read.Set.Readings, Floors: r.read.Set.Floors, Out: &out,
		Now: func() time.Time { return time.Date(2026, 10, 29, 1, 5, 0, 0, time.UTC) }}
	if err := show.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Fable 50.0%") {
		t.Errorf("mw usage show did not print the Fable:\n%s", out.String())
	}
}

func TestUsageReadOnAScreenItCannotReadChangesNothingAndKeepsTheCapture(t *testing.T) {
	r := newUsageReadRig(t, noUsageTab)
	err := r.read.Run(context.Background())
	if err == nil {
		t.Fatal("an unreadable screen was not an error")
	}
	if !strings.Contains(err.Error(), "/state/usage-read-capture.txt") {
		t.Errorf("the error %q does not say where the capture is", err)
	}
	if len(r.kept) != 1 || r.kept[0] != noUsageTab {
		t.Errorf("kept %q, want the screen as captured", r.kept)
	}
	if all, _ := (application.UsageReadings{Notes: r.notes}).All(context.Background()); len(all) != 0 {
		t.Errorf("readings %v were kept from a screen that was not read", all)
	}
	if len(r.paused) != 0 {
		t.Errorf("paused on a screen that was not read")
	}
	if len(r.term.closed) != 1 {
		t.Errorf("the probe session was closed %d times, want once", len(r.term.closed))
	}
}

func TestUsageReadLooksAgainWhenTheScreenIsNotUpYet(t *testing.T) {
	r := newUsageReadRig(t, "Loading...", usageScreen)
	if err := r.read.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.term.taken != 2 {
		t.Errorf("captured %d times, want 2", r.term.taken)
	}
}

func TestUsageReadNeverRunsTwoAtOnce(t *testing.T) {
	r := newUsageReadRig(t, usageScreen)
	r.lock.held = true
	if err := r.read.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.term.opened) != 0 {
		t.Errorf("opened a session %v while another read held the lock", r.term.opened)
	}
	if !strings.Contains(r.out.String(), "under way") {
		t.Errorf("said %q, want it to say another read is under way", r.out.String())
	}
}

func TestUsageReadClosesTheSessionWhenItCannotBeDriven(t *testing.T) {
	r := newUsageReadRig(t, usageScreen)
	r.term.openErr = errors.New("no tmux")
	if err := r.read.Run(context.Background()); err == nil {
		t.Fatal("a session that did not open was not an error")
	}
	if len(r.term.closed) != 1 {
		t.Errorf("closed %v, want a close even so", r.term.closed)
	}
}

// requireProbeClosed fails unless the run left exactly its own probe closed.
func requireProbeClosed(t *testing.T, r *usageReadRig) {
	t.Helper()
	if len(r.term.opened) != 1 {
		t.Fatalf("opened %v, want one probe", r.term.opened)
	}
	if len(r.term.closed) != 1 || r.term.closed[0] != "mw-usage-probe-1" {
		t.Errorf("closed %v, want the probe mw-usage-probe-1 killed before read returned", r.term.closed)
	}
}

func TestUsageReadKillsTheProbeOnEveryWayOut(t *testing.T) {
	cases := []struct {
		name    string
		failAt  string
		screens []string
		wantErr bool
		cancel  bool
	}{
		{name: "success", screens: []string{usageScreen}},
		{name: "unreadable screen", screens: []string{noUsageTab}, wantErr: true},
		{name: "typing fails", failAt: "type", screens: []string{usageScreen}, wantErr: true},
		{name: "a key press fails", failAt: "press", screens: []string{usageScreen}, wantErr: true},
		{name: "capture fails", failAt: "capture", screens: []string{usageScreen}, wantErr: true},
		{name: "context cancelled", screens: []string{"Loading..."}, wantErr: true, cancel: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newUsageReadRig(t, c.screens...)
			r.term.failAt = c.failAt
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if c.cancel {
				r.read.Wait = func(ctx context.Context, _ time.Duration) error {
					cancel()
					return ctx.Err()
				}
				r.term.onCapture = nil
			}
			err := r.read.Run(ctx)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			requireProbeClosed(t, r)
		})
	}
}

func TestUsageReadKillsTheProbeOnATimeout(t *testing.T) {
	r := newUsageReadRig(t, usageScreen)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	r.read.Wait = func(ctx context.Context, _ time.Duration) error {
		<-ctx.Done()
		return ctx.Err()
	}
	if err := r.read.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the deadline", err)
	}
	requireProbeClosed(t, r)
}

// The pause a full reading runs can wait for minutes, and a kill in that wait
// skips every defer: the probe must be gone before the reading is set.
func TestUsageReadKillsTheProbeBeforeItSetsTheUsage(t *testing.T) {
	r := newUsageReadRig(t, usageScreen)
	var closedWhenSet []string
	now := time.Date(2026, 10, 29, 1, 2, 0, 0, time.UTC)
	r.read.Set.Now = func() time.Time {
		closedWhenSet = append([]string(nil), r.term.closed...)
		return now
	}
	if err := r.read.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(closedWhenSet) != 1 {
		t.Errorf("the probe was closed %v at the time the usage was set, want it closed already", closedWhenSet)
	}
	requireProbeClosed(t, r)
}

var errNoSweep = errors.New("no server running")

func TestUsageReadClosesStaleProbesBeforeOpeningItsOwn(t *testing.T) {
	r := newUsageReadRig(t, usageScreen)
	now := time.Date(2026, 10, 29, 1, 2, 0, 0, time.UTC)
	r.read.Alive = func(pid int) bool { return pid == 200 }
	r.term.probes = []application.UsageProbe{
		{Name: "mw-usage-probe-100-1", Created: now.Add(-3 * time.Hour)},     // dead pid, old: closed
		{Name: "mw-usage-probe-200-2", Created: now.Add(-3 * time.Hour)},     // live mw: kept
		{Name: "mw-usage-probe-300-3", Created: now.Add(-2 * time.Minute)},   // dead pid but young: kept
		{Name: "mw-usage-probe-400-4", Created: now.Add(-11 * time.Minute)},  // dead pid, over 10 minutes: closed
		{Name: "mw-seat-mayor", Created: now.Add(-9 * time.Hour)},            // not a probe: kept
		{Name: "mw-usage-probe-notapid-5", Created: now.Add(-9 * time.Hour)}, // no pid in the name: kept
	}
	if err := r.read.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"mw-usage-probe-100-1", "mw-usage-probe-400-4", "mw-usage-probe-1"}
	if strings.Join(r.term.closed, ",") != strings.Join(want, ",") {
		t.Errorf("closed %v, want %v", r.term.closed, want)
	}
	if !strings.Contains(r.out.String(), "closed 2 stale usage probes") {
		t.Errorf("did not say how many it closed:\n%s", r.out.String())
	}
}

func TestUsageReadSaysNothingWhenNoProbeIsStale(t *testing.T) {
	r := newUsageReadRig(t, usageScreen)
	r.read.Alive = func(int) bool { return false }
	if err := r.read.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.out.String(), "stale") {
		t.Errorf("said something of stale probes when there were none:\n%s", r.out.String())
	}
}

func TestUsageReadGoesOnWhenTheSweepCannotListTheProbes(t *testing.T) {
	r := newUsageReadRig(t, usageScreen)
	r.term.probeErr = errNoSweep
	if err := r.read.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireProbeClosed(t, r)
}
