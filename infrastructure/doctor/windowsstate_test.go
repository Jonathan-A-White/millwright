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
