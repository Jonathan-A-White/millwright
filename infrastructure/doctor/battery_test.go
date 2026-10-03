package doctor_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// batteryHost is a fake /sys/class/power_supply with a Mains supply (AC1) and
// the helpers to put a battery in it. Nothing here reads the real one.
type batteryHost struct {
	t   *testing.T
	dir string
}

func newBatteryHost(t *testing.T) *batteryHost {
	t.Helper()
	dir := t.TempDir()
	h := &batteryHost{t: t, dir: dir}
	h.write("AC1", "type", "Mains")
	return h
}

func (h *batteryHost) write(supply, file, content string) {
	h.t.Helper()
	path := filepath.Join(h.dir, supply, file)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content+"\n"), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

// battery sets BAT1 to the status and capacity given.
func (h *batteryHost) battery(status, capacity string) {
	h.t.Helper()
	h.write("BAT1", "type", "Battery")
	h.write("BAT1", "status", status)
	h.write("BAT1", "capacity", capacity)
}

// batteryClears are the clears the battery check sent, over every run of a test.
var batteryClears []clearCall

// runBatteryDoctor runs mw doctor's battery check once, over a real Store in
// a temp dir, and returns the alarms it sent.
func runBatteryDoctor(t *testing.T, host *batteryHost, store *doctor.Store, now time.Time, alarms *[]string) application.DoctorReport {
	t.Helper()
	check := doctor.NewBattery(host.dir, store)
	check.Alarm = func(_ context.Context, text string) (uint64, error) {
		*alarms = append(*alarms, text)
		return uint64(100 + len(*alarms)), nil
	}
	check.Clear = func(_ context.Context, text string, clears uint64) error {
		batteryClears = append(batteryClears, clearCall{text, clears})
		return nil
	}
	report, err := application.Doctor{
		Checks: application.DoctorChecks{check},
		State:  store,
		Log:    store,
		Now:    func() time.Time { return now },
	}.Run(context.Background(), "", false)
	if err != nil {
		t.Fatalf("doctor run: %v", err)
	}
	return report
}

func TestBatteryAlarmsOncePerFallBelowEachThreshold(t *testing.T) {
	host := newBatteryHost(t)
	store := doctor.New(t.TempDir())
	var alarms []string
	now := time.Date(2026, 10, 1, 18, 0, 0, 0, time.UTC)

	host.battery("Discharging", "24")
	runBatteryDoctor(t, host, store, now, &alarms)
	if len(alarms) != 1 {
		t.Fatalf("expected one alarm at 24%% discharging, got %v", alarms)
	}
	if want := "Laptop battery 24%, discharging: plug it in or it sleeps"; alarms[0] != want {
		t.Fatalf("expected %q, got %q", want, alarms[0])
	}

	now = now.Add(5 * time.Minute)
	host.battery("Discharging", "23")
	runBatteryDoctor(t, host, store, now, &alarms)
	if len(alarms) != 1 {
		t.Fatalf("expected no new alarm on the next run, got %v", alarms)
	}

	now = now.Add(5 * time.Minute)
	host.battery("Discharging", "9")
	runBatteryDoctor(t, host, store, now, &alarms)
	if len(alarms) != 2 || alarms[1] != "Laptop battery 9%, discharging: plug it in or it sleeps" {
		t.Fatalf("expected one more alarm at 9%%, got %v", alarms)
	}

	now = now.Add(5 * time.Minute)
	host.battery("Discharging", "8")
	runBatteryDoctor(t, host, store, now, &alarms)
	if len(alarms) != 2 {
		t.Fatalf("expected no alarm for the same band, got %v", alarms)
	}
}

func TestBatteryAlarmsAgainAfterACharge(t *testing.T) {
	host := newBatteryHost(t)
	store := doctor.New(t.TempDir())
	var alarms []string
	now := time.Date(2026, 10, 1, 18, 0, 0, 0, time.UTC)

	host.battery("Discharging", "20")
	runBatteryDoctor(t, host, store, now, &alarms)
	now = now.Add(5 * time.Minute)
	host.battery("Charging", "22")
	runBatteryDoctor(t, host, store, now, &alarms)
	now = now.Add(5 * time.Minute)
	host.battery("Discharging", "24")
	runBatteryDoctor(t, host, store, now, &alarms)

	if len(alarms) != 2 {
		t.Fatalf("expected a fresh alarm for a second fall below 25%%, got %v", alarms)
	}
}

func TestBatteryAlarmsAtTenWhenFirstSeenThere(t *testing.T) {
	host := newBatteryHost(t)
	store := doctor.New(t.TempDir())
	var alarms []string
	now := time.Date(2026, 10, 1, 18, 0, 0, 0, time.UTC)

	host.battery("Discharging", "10")
	runBatteryDoctor(t, host, store, now, &alarms)
	now = now.Add(5 * time.Minute)
	host.battery("Discharging", "9")
	runBatteryDoctor(t, host, store, now, &alarms)
	if len(alarms) != 1 {
		t.Fatalf("expected one alarm for a fall straight to 10%%, got %v", alarms)
	}
}

func TestBatterySaysNothingWhileChargingOrFullOrAboveTheLine(t *testing.T) {
	for _, tc := range []struct{ status, capacity string }{
		{"Charging", "5"},
		{"Full", "100"},
		{"Not charging", "20"},
		{"Discharging", "26"},
		{"Discharging", "80"},
	} {
		host := newBatteryHost(t)
		store := doctor.New(t.TempDir())
		var alarms []string
		host.battery(tc.status, tc.capacity)

		report := runBatteryDoctor(t, host, store, time.Date(2026, 10, 1, 18, 0, 0, 0, time.UTC), &alarms)
		if len(alarms) != 0 {
			t.Fatalf("%s %s%%: expected no alarm, got %v", tc.status, tc.capacity, alarms)
		}
		if report.Faulty() || report.Results[0].Verdict != "ok" {
			t.Fatalf("%s %s%%: expected ok, got %s", tc.status, tc.capacity, report)
		}
	}
}

func TestBatterySaysNothingOnAHostWithNoBattery(t *testing.T) {
	host := newBatteryHost(t)
	store := doctor.New(t.TempDir())
	var alarms []string

	report := runBatteryDoctor(t, host, store, time.Date(2026, 10, 1, 18, 0, 0, 0, time.UTC), &alarms)
	if len(alarms) != 0 {
		t.Fatalf("expected no alarm with no battery, got %v", alarms)
	}
	if got := report.Results[0]; got.Verdict != "ok" || !strings.Contains(got.Reason, "no battery") {
		t.Fatalf("expected ok naming that there is no battery, got %s", report)
	}

	missing := doctor.NewBattery(filepath.Join(t.TempDir(), "absent"), store)
	if verdict, _ := missing.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok when there is no power_supply directory at all, got %s", verdict)
	}
}

func TestBatteryHonoursItsOwnThresholds(t *testing.T) {
	host := newBatteryHost(t)
	store := doctor.New(t.TempDir())
	host.battery("Discharging", "40")

	check := doctor.NewBattery(host.dir, store)
	check.Low, check.Critical = 50, 20
	if verdict, _ := check.Probe(context.Background()); verdict != application.DoctorFaulty {
		t.Fatalf("expected 40%% to be faulty under a 50%% line, got %s", verdict)
	}
}

func TestBatteryRetriesWhenTheAlarmCouldNotBeSent(t *testing.T) {
	host := newBatteryHost(t)
	store := doctor.New(t.TempDir())
	host.battery("Discharging", "20")

	check := doctor.NewBattery(host.dir, store)
	check.Alarm = func(context.Context, string) (uint64, error) { return 0, os.ErrPermission }
	if err := check.Cure(context.Background()); err == nil {
		t.Fatalf("expected the failed alarm to fail the cure")
	}
	if verdict, _ := check.Probe(context.Background()); verdict != application.DoctorFaulty {
		t.Fatalf("expected the band to stay unalarmed, got %s", verdict)
	}
}

func TestBatteryRecoveryClearsTheSeqOfEachAlarmOfTheFall(t *testing.T) {
	batteryClears = nil
	host := newBatteryHost(t)
	store := doctor.New(t.TempDir())
	var alarms []string
	now := time.Date(2026, 10, 3, 18, 0, 0, 0, time.UTC)

	host.battery("Discharging", "20")
	runBatteryDoctor(t, host, store, now, &alarms)
	now = now.Add(5 * time.Minute)
	host.battery("Discharging", "9")
	runBatteryDoctor(t, host, store, now, &alarms)
	if len(alarms) != 2 || len(batteryClears) != 0 {
		t.Fatalf("expected two alarms and no clear yet, got %q and %+v", alarms, batteryClears)
	}

	now = now.Add(5 * time.Minute)
	host.battery("Charging", "11")
	runBatteryDoctor(t, host, store, now, &alarms)
	if len(batteryClears) != 2 || batteryClears[0].clears != 101 || batteryClears[1].clears != 102 || !strings.Contains(batteryClears[1].text, "11%") {
		t.Fatalf("expected a clear for each of the fall's alarms, 101 and 102, got %+v", batteryClears)
	}

	now = now.Add(5 * time.Minute)
	runBatteryDoctor(t, host, store, now, &alarms)
	if len(batteryClears) != 2 || len(alarms) != 2 {
		t.Errorf("the fall is over: expected nothing more, got alarms %q, clears %+v", alarms, batteryClears)
	}
}

func TestBatteryThatNeverAlarmedClearsNothing(t *testing.T) {
	batteryClears = nil
	host := newBatteryHost(t)
	store := doctor.New(t.TempDir())
	var alarms []string
	now := time.Date(2026, 10, 3, 18, 0, 0, 0, time.UTC)

	host.battery("Discharging", "80")
	runBatteryDoctor(t, host, store, now, &alarms)
	host.battery("Charging", "81")
	runBatteryDoctor(t, host, store, now.Add(time.Minute), &alarms)
	if len(batteryClears) != 0 {
		t.Fatalf("expected no clear without an alarm, got %+v", batteryClears)
	}
}

func TestBatteryAlarmWithNoSeqClearsNothingWhenItEnds(t *testing.T) {
	batteryClears = nil
	host := newBatteryHost(t)
	store := doctor.New(t.TempDir())
	now := time.Date(2026, 10, 3, 18, 0, 0, 0, time.UTC)

	host.battery("Discharging", "20")
	check := doctor.NewBattery(host.dir, store)
	check.Alarm = func(context.Context, string) (uint64, error) { return 0, nil }
	check.Clear = func(_ context.Context, text string, clears uint64) error {
		batteryClears = append(batteryClears, clearCall{text, clears})
		return nil
	}
	doc := application.Doctor{Checks: application.DoctorChecks{check}, State: store, Log: store, Now: func() time.Time { return now }}
	if _, err := doc.Run(context.Background(), "", false); err != nil {
		t.Fatal(err)
	}
	host.battery("Charging", "21")
	if _, err := doc.Run(context.Background(), "", false); err != nil {
		t.Fatal(err)
	}
	if len(batteryClears) != 0 {
		t.Fatalf("an emergency that was never written has nothing to clear, got %+v", batteryClears)
	}
}
