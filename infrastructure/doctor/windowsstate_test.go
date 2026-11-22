package doctor_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

var windowsStateNow = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

const windowsStateScriptHash = "ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789"

// goodWindowsState is a state file the Windows task wrote a minute ago, with
// nothing wrong in it. A test changes one line of it.
func goodWindowsState() map[string]string {
	return map[string]string{
		"time":                  windowsStateNow.Add(-time.Minute).Format("2006-01-02T15:04:05Z"),
		"script_sha256":         windowsStateScriptHash,
		"boot_time":             "2026-11-20T08:00:00Z",
		"restart_pending":       "no",
		"restart_pending_since": "none",
		"vhdx_drive":            "D 536870912000 107374182400",
		"c_free":                "53687091200",
		"vhdx_size":             "214748364800",
		"memory_total":          "34359738368",
		"memory_free":           "8589934592",
		"ac_standby_timeout":    "0",
		"wsl_version":           "2.6.1.0",
		"task1":                 "Start WSL (millwright)|Ready|0|Interactive",
		"task2":                 "millwright-wsl-flag-watch|Running|267009|Interactive",
		"disk1":                 "Samsung SSD 980|SSD|Healthy",
		"wslconfig_memory":      "16GB",
		"wslconfig_processors":  "8",
		"wslconfig_swap":        "unset",
	}
}

// stateText renders the map as the task writes it: 'key value' lines, the
// task and disk lines repeated.
func stateText(m map[string]string) string {
	order := []string{"time", "script_sha256", "boot_time", "restart_pending", "restart_pending_since", "vhdx_drive", "c_free", "vhdx_size",
		"memory_total", "memory_free", "ac_standby_timeout", "wsl_version", "task1", "task2", "disk1", "wslconfig_memory", "wslconfig_processors", "wslconfig_swap"}
	var b strings.Builder
	for _, k := range order {
		v, ok := m[k]
		if !ok {
			continue
		}
		key := strings.TrimRight(k, "0123456789")
		if strings.HasPrefix(k, "wslconfig") || strings.HasPrefix(k, "restart") || strings.HasPrefix(k, "memory") || strings.HasPrefix(k, "script") {
			key = k
		}
		b.WriteString(key + " " + v + "\n")
	}
	return b.String()
}

func windowsStateCheck(t *testing.T, virt string, m map[string]string) *doctor.WindowsState {
	t.Helper()
	path := filepath.Join(t.TempDir(), "windows-state")
	if m != nil {
		if err := os.WriteFile(path, []byte(stateText(m)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &doctor.WindowsState{Path: path, ScriptSHA256: windowsStateScriptHash, DetectVirt: detectVirt(virt), Now: func() time.Time { return windowsStateNow }}
}

func TestWindowsStateIsOkWhenNothingIsWrong(t *testing.T) {
	verdict, reason := windowsStateCheck(t, "wsl", goodWindowsState()).Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %v %q", verdict, reason)
	}
}

func TestWindowsStateIsOkNotWSLOffWSL(t *testing.T) {
	verdict, reason := windowsStateCheck(t, "none", nil).Probe(context.Background())
	if verdict != application.DoctorOK || !strings.Contains(reason, "not WSL") {
		t.Fatalf("expected ok, 'not WSL', got %v %q", verdict, reason)
	}
}

func TestWindowsStateIsFaultyWithNoFileAndNamesTheInstallLine(t *testing.T) {
	verdict, reason := windowsStateCheck(t, "wsl", nil).Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "windows-state") || !strings.Contains(reason, "mw host wsl-flag --install-line") {
		t.Fatalf("expected faulty naming the file and the install line, got %v %q", verdict, reason)
	}
}

func TestWindowsStateIsFaultyWhenTheFileIsOverTenMinutesOld(t *testing.T) {
	m := goodWindowsState()
	m["time"] = windowsStateNow.Add(-11 * time.Minute).Format("2006-01-02T15:04:05Z")
	verdict, reason := windowsStateCheck(t, "wsl", m).Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "11 minutes") {
		t.Fatalf("expected faulty saying the state is 11 minutes old, got %v %q", verdict, reason)
	}
	m["time"] = windowsStateNow.Add(-9 * time.Minute).Format("2006-01-02T15:04:05Z")
	if verdict, reason := windowsStateCheck(t, "wsl", m).Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok at 9 minutes, got %v %q", verdict, reason)
	}
}

func TestWindowsStateIsFaultyWhenTheACStandbyTimeoutIsNotZero(t *testing.T) {
	m := goodWindowsState()
	m["ac_standby_timeout"] = "1800"
	verdict, reason := windowsStateCheck(t, "wsl", m).Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "standby") || !strings.Contains(reason, "1800") {
		t.Fatalf("expected faulty naming the standby timeout, got %v %q", verdict, reason)
	}
}

func TestWindowsStateIsFaultyForADisabledTaskAndAFailedTask(t *testing.T) {
	m := goodWindowsState()
	m["task1"] = "Start WSL (millwright)|Disabled|0|Interactive"
	verdict, reason := windowsStateCheck(t, "wsl", m).Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "Start WSL (millwright)") || !strings.Contains(reason, "disabled") {
		t.Fatalf("expected faulty naming the disabled task, got %v %q", verdict, reason)
	}
	m = goodWindowsState()
	m["task2"] = "millwright-wsl-flag-watch|Ready|2147942402|Interactive"
	verdict, reason = windowsStateCheck(t, "wsl", m).Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "millwright-wsl-flag-watch") || !strings.Contains(reason, "2147942402") {
		t.Fatalf("expected faulty naming the failed task and its result, got %v %q", verdict, reason)
	}
	// 'running' (267009) and 'has not yet run' (267011) are not failures
	m = goodWindowsState()
	m["task1"] = "Start WSL (millwright)|Ready|267011|Interactive"
	if verdict, reason := windowsStateCheck(t, "wsl", m).Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected a task that has not yet run to be ok, got %v %q", verdict, reason)
	}
}

func TestWindowsStateIsFaultyForADiskThatIsNotHealthy(t *testing.T) {
	m := goodWindowsState()
	m["disk1"] = "Samsung SSD 980|SSD|Warning"
	verdict, reason := windowsStateCheck(t, "wsl", m).Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "Samsung SSD 980") || !strings.Contains(reason, "Warning") {
		t.Fatalf("expected faulty naming the disk and its health, got %v %q", verdict, reason)
	}
}

func TestWindowsStateIsFaultyForAnInstalledScriptThatIsNotThisMws(t *testing.T) {
	m := goodWindowsState()
	m["script_sha256"] = "0000000000000000000000000000000000000000000000000000000000000000"
	verdict, reason := windowsStateCheck(t, "wsl", m).Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "mw host wsl-flag --install-line") {
		t.Fatalf("expected faulty with the cure: paste the install line again, got %v %q", verdict, reason)
	}
	// the hash is compared whatever its case
	m["script_sha256"] = strings.ToLower(windowsStateScriptHash)
	if verdict, reason := windowsStateCheck(t, "wsl", m).Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected a hash in lower case to match, got %v %q", verdict, reason)
	}
}

func TestWindowsStateNamesEachFaultAtOnce(t *testing.T) {
	m := goodWindowsState()
	m["ac_standby_timeout"] = "900"
	m["disk1"] = "Samsung SSD 980|SSD|Unhealthy"
	m["task1"] = "Start WSL (millwright)|Disabled|0|Interactive"
	verdict, reason := windowsStateCheck(t, "wsl", m).Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %v %q", verdict, reason)
	}
	for _, want := range []string{"standby", "Samsung SSD 980", "Start WSL (millwright)"} {
		if !strings.Contains(reason, want) {
			t.Errorf("expected every fault named, %q missing from %q", want, reason)
		}
	}
}

func TestWindowsStateSaysARestartPendingForOverThreeDaysInItsOkLine(t *testing.T) {
	m := goodWindowsState()
	m["restart_pending"] = "yes"
	m["restart_pending_since"] = windowsStateNow.Add(-4 * 24 * time.Hour).Format("2006-01-02T15:04:05Z")
	verdict, reason := windowsStateCheck(t, "wsl", m).Probe(context.Background())
	if verdict != application.DoctorOK || !strings.Contains(reason, "restart pending") || !strings.Contains(reason, "4 days") {
		t.Fatalf("expected ok saying a restart has been pending 4 days, got %v %q", verdict, reason)
	}
	m["restart_pending_since"] = windowsStateNow.Add(-2 * 24 * time.Hour).Format("2006-01-02T15:04:05Z")
	verdict, reason = windowsStateCheck(t, "wsl", m).Probe(context.Background())
	if verdict != application.DoctorOK || strings.Contains(reason, "restart pending") {
		t.Fatalf("expected a restart pending 2 days to go unsaid, got %v %q", verdict, reason)
	}
}

func TestWindowsStateCannotTellFromAFileItCannotRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "windows-state")
	if err := os.WriteFile(path, []byte("garbage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	check := &doctor.WindowsState{Path: path, DetectVirt: detectVirt("wsl"), Now: func() time.Time { return windowsStateNow }}
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot tell, got %v %q", verdict, reason)
	}
}

// The cure is one note to the Mayor naming the faults: nothing on Windows can
// be changed from WSL.
func TestWindowsStateCureWritesOneNoteNamingTheFaults(t *testing.T) {
	m := goodWindowsState()
	m["ac_standby_timeout"] = "900"
	check := windowsStateCheck(t, "wsl", m)
	var notes []string
	check.Note = func(_ context.Context, text string) error { notes = append(notes, text); return nil }
	if verdict, _ := check.Probe(context.Background()); verdict != application.DoctorFaulty {
		t.Fatal("expected faulty")
	}
	if err := check.Cure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "standby") || !strings.Contains(notes[0], "windows-state") {
		t.Errorf("expected one note naming the standby fault, got %q", notes)
	}
	if check.Name() != "windows-state" {
		t.Errorf("expected the name windows-state, got %q", check.Name())
	}
}

// windowsActsCheck is the check over a state file with a windows-acts file
// beside it holding acts (nil: no such file).
func windowsActsCheck(t *testing.T, acts []string) *doctor.WindowsState {
	t.Helper()
	w := windowsStateCheck(t, "wsl", goodWindowsState())
	if acts != nil {
		if err := os.WriteFile(filepath.Join(filepath.Dir(w.Path), "windows-acts"), []byte(strings.Join(acts, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

func actAt(ago time.Duration, text string) string {
	return windowsStateNow.Add(-ago).Format("2006-01-02T15:04:05Z") + " " + text
}

func TestWindowsStateNamesTheNewestActOfTheLast24HoursInItsOkLine(t *testing.T) {
	w := windowsActsCheck(t, []string{
		actAt(30*time.Hour, "hung: restarted"),
		actAt(5*time.Hour, "not running: started"),
		actAt(2*time.Hour, "hung: restarted"),
	})
	verdict, reason := w.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %v %q", verdict, reason)
	}
	if !strings.Contains(reason, "hung: restarted") || !strings.Contains(reason, "2026-11-21T10:00:00Z") {
		t.Errorf("expected the newest act (hung: restarted at 10:00) named, got %q", reason)
	}
	if strings.Contains(reason, "not running: started") {
		t.Errorf("only the newest act is named, got %q", reason)
	}
}

func TestWindowsStateNamesNoActOlderThan24Hours(t *testing.T) {
	w := windowsActsCheck(t, []string{actAt(25*time.Hour, "hung: restarted")})
	verdict, reason := w.Probe(context.Background())
	if verdict != application.DoctorOK || strings.Contains(reason, "hung: restarted") {
		t.Fatalf("expected ok naming no act older than 24 hours, got %v %q", verdict, reason)
	}
}

func TestWindowsStateIsOkWithNoActsFileAndSkipsGarbageLines(t *testing.T) {
	if verdict, reason := windowsActsCheck(t, nil).Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok with no windows-acts, got %v %q", verdict, reason)
	}
	w := windowsActsCheck(t, []string{"garbage", actAt(time.Hour, "down: started again"), "not-a-time something"})
	verdict, reason := w.Probe(context.Background())
	if verdict != application.DoctorOK || !strings.Contains(reason, "down: started again") {
		t.Fatalf("expected the one good act named, got %v %q", verdict, reason)
	}
}

func TestWindowsStateActsLineComesEvenWhenAnActIsInTheFuture(t *testing.T) {
	// a clock a little ahead of the writer's is not a reason to hide the act
	w := windowsActsCheck(t, []string{actAt(-2*time.Minute, "not running: started")})
	if _, reason := w.Probe(context.Background()); !strings.Contains(reason, "not running: started") {
		t.Fatalf("expected the act named, got %q", reason)
	}
}

// On the desktop (2026-11-22) a WSL host with no windows-state file was reported
// 'cured', way back 'nothing was changed on the host', while the file was still
// absent: the cure is a note, and a note puts nothing right. Run through the
// doctor with its cure, the check is faulty (the note written) and then damped,
// never cured, and the other hosts read it faulty on every tick.
func TestWindowsStateWithNoFileIsNeverReportedCuredByTheDoctor(t *testing.T) {
	check := windowsStateCheck(t, "wsl", nil)
	notes := 0
	check.Note = func(context.Context, string) error { notes++; return nil }
	now := windowsStateNow
	verdicts := apptest.NewFakeTracker()
	runner := application.Doctor{
		Checks: application.DoctorChecks{check},
		State:  apptest.NewFakeDoctorState(), Log: &apptest.FakeDoctorLog{}, Verdicts: verdicts,
		Host: "desktop", Now: func() time.Time { return now },
	}
	for _, at := range []time.Duration{0, 5 * time.Minute, 7 * time.Hour} {
		now = windowsStateNow.Add(at)
		report, _ := runner.Run(context.Background(), "", false)
		if len(report.Results) != 1 {
			t.Fatalf("at +%s: expected one result, got %+v", at, report.Results)
		}
		got := report.Results[0]
		if got.Verdict != "faulty" && got.Verdict != "damped" {
			t.Errorf("at +%s: expected faulty or damped, got %q (%s)", at, got.Verdict, got.String())
		}
		if strings.Contains(got.String(), "cured") {
			t.Errorf("at +%s: the report says cured: %s", at, got.String())
		}
		said, _ := verdicts.Note(context.Background(), application.DoctorVerdictKey("desktop"))
		if verdict, ok := application.ParseDoctorVerdict(said); !ok || len(verdict.Faulty) != 1 || verdict.Faulty[0].Check != doctor.WindowsStateName {
			t.Errorf("at +%s: expected the verdict to hold windows-state faulty, got %q", at, said)
		}
	}
	if notes != 2 {
		t.Errorf("expected one note at the start and one after the 6 hour damper, got %d", notes)
	}
}
