package doctor_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// standIn writes a program that prints text and exits with status, and notes
// its arguments in args.
func standIn(t *testing.T, name, text string, status int) (program, args string) {
	t.Helper()
	dir := t.TempDir()
	args = filepath.Join(dir, "args")
	program = filepath.Join(dir, name)
	script := "#!/bin/sh\necho \"$*\" >> " + args + "\ncat <<'EOF'\n" + text + "\nEOF\nexit " + strconv.Itoa(status) + "\n"
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return program, args
}

func TestUserUnitsRunByTimerReadsTheTimersLoadState(t *testing.T) {
	for _, tc := range []struct {
		state, unit string
		want        bool
	}{
		{"loaded", "mw-dispatch.service", true},
		{"not-found", "mw-report.service", false},
		{"loaded", "mw-dispatch.timer", false},
	} {
		program, args := standIn(t, "systemctl", tc.state, 0)
		got, err := doctor.UserUnits{Systemctl: program}.RunByTimer(context.Background(), tc.unit)
		if err != nil || got != tc.want {
			t.Errorf("%s with the timer %s: got %v, %v; want %v", tc.unit, tc.state, got, err, tc.want)
		}
		if data, _ := os.ReadFile(args); strings.HasSuffix(tc.unit, ".service") &&
			!strings.Contains(string(data), "--user show "+strings.TrimSuffix(tc.unit, ".service")+".timer") {
			t.Errorf("systemctl was run with %q", data)
		}
	}
}

func TestUserUnitsJournalTailAsksForTheLastLines(t *testing.T) {
	program, args := standIn(t, "journalctl", "line one\nline two", 0)
	got, err := doctor.UserUnits{Journalctl: program}.JournalTail(context.Background(), "mw-dispatch.service")
	if err != nil || got != "line one\nline two" {
		t.Fatalf("got %q, %v", got, err)
	}
	if data, _ := os.ReadFile(args); !strings.Contains(string(data), "--user -u mw-dispatch.service -n 20") {
		t.Errorf("journalctl was run with %q", data)
	}
}

func TestUserUnitsRanCleanSinceFindsSystemdsFinishedLine(t *testing.T) {
	since := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		journal string
		want    bool
	}{
		{"mw-dispatch.service: Failed with result 'exit-code'.\nFailed to start mw-dispatch.service - Dispatch.", false},
		{"started mw-gq6.716\nFinished mw-dispatch.service - Dispatch ready stories.", true},
		{"mw-dispatch.service: Deactivated successfully.", true},
		{"mw-dispatch.service: Succeeded.", true},
		{"Finished mw-doctor.service - Cure faults.", false},
		{"", false},
	} {
		program, args := standIn(t, "journalctl", tc.journal, 0)
		got, err := doctor.UserUnits{Journalctl: program}.RanCleanSince(context.Background(), "mw-dispatch.service", since)
		if err != nil || got != tc.want {
			t.Errorf("journal %q: got %v, %v; want %v", tc.journal, got, err, tc.want)
		}
		if data, _ := os.ReadFile(args); !strings.Contains(string(data), "--since @"+"1792756800") {
			t.Errorf("journalctl was run with %q", data)
		}
	}
}

func TestUnitFailureFilesKeepOneFailurePerUnit(t *testing.T) {
	files := doctor.UnitFailureFiles{Dir: filepath.Join(t.TempDir(), "unit-failed")}
	ctx := context.Background()
	if _, ok, err := files.Load(ctx, "mw-dispatch.service"); ok || err != nil {
		t.Fatalf("an empty directory: ok=%v err=%v", ok, err)
	}
	failure := application.UnitFailure{At: time.Date(2026, 10, 23, 12, 0, 5, 0, time.UTC), Journal: "a\nb"}
	if err := files.Save(ctx, "mw-dispatch.service", failure); err != nil {
		t.Fatal(err)
	}
	got, ok, err := files.Load(ctx, "mw-dispatch.service")
	if err != nil || !ok || !got.At.Equal(failure.At) || got.Journal != "a\nb" {
		t.Fatalf("got %+v, %v, %v", got, ok, err)
	}
	if _, ok, _ := files.Load(ctx, "mw-doctor.service"); ok {
		t.Error("another unit's failure was read")
	}
	if err := files.Clear(ctx, "mw-dispatch.service"); err != nil {
		t.Fatal(err)
	}
	if err := files.Clear(ctx, "mw-dispatch.service"); err != nil {
		t.Errorf("clearing none: %v", err)
	}
	if _, ok, _ := files.Load(ctx, "mw-dispatch.service"); ok {
		t.Error("the failure is still kept after Clear")
	}
}
