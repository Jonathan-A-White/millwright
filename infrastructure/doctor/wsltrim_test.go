package doctor_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// fakeTrimSystemctl writes a stand-in for systemctl that answers
// `show fstrim.timer -p LoadState -p LastTriggerUSec --timestamp=unix` the way
// systemd 255 does: LastTriggerUSec empty for a timer that never ran, '@<unix
// seconds>' for one that did, and logs every call to a file this test reads
// back. loadState is what the timer's unit file says (loaded, not-found).
func fakeTrimSystemctl(t *testing.T, loadState, lastTrigger string) (program, callLog string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for systemctl is a shell script")
	}
	dir := t.TempDir()
	program = filepath.Join(dir, "systemctl-stand-in")
	callLog = filepath.Join(dir, "calls")
	script := fmt.Sprintf(`#!/bin/sh
echo "$*" >>%q
if [ "$1" = show ] && [ "$2" = fstrim.timer ]; then
  echo "LastTriggerUSec=%s"
  echo "LoadState=%s"
  exit 0
fi
exit 1
`, callLog, lastTrigger, loadState)
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return program, callLog
}

func detectVirt(kind string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return kind, nil }
}

var trimNow = time.Date(2026, 11, 6, 20, 0, 0, 0, time.UTC)

func trimCheck(t *testing.T, virt, loadState, lastTrigger string) *doctor.WSLTrim {
	t.Helper()
	program, _ := fakeTrimSystemctl(t, loadState, lastTrigger)
	return &doctor.WSLTrim{Program: program, DetectVirt: detectVirt(virt), Now: func() time.Time { return trimNow }}
}

func unixAt(ago time.Duration) string { return fmt.Sprintf("@%d", trimNow.Add(-ago).Unix()) }

// A WSL host whose fstrim.timer never ran is the bug: the unit's
// ConditionVirtualization=!container stops it on WSL.
func TestWSLTrimIsFaultyWhenFstrimTimerNeverRan(t *testing.T) {
	check := trimCheck(t, "wsl", "loaded", "")
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty for a timer that never ran, got %v %q", verdict, reason)
	}
	for _, want := range []string{"never", "fstrim.timer", "mw host trim-setup"} {
		if !strings.Contains(reason, want) {
			t.Errorf("expected the reason to name %q, got %q", want, reason)
		}
	}
}

func TestWSLTrimIsFaultyWhenFstrimTimerLastRanOverEightDaysAgo(t *testing.T) {
	check := trimCheck(t, "wsl", "loaded", unixAt(8*24*time.Hour+time.Minute))
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty for a trim over 8 days old, got %v %q", verdict, reason)
	}
	for _, want := range []string{"8 days ago", "mw host trim-setup"} {
		if !strings.Contains(reason, want) {
			t.Errorf("expected the reason to name %q, got %q", want, reason)
		}
	}
}

func TestWSLTrimIsOkWhenFstrimTimerRanInTheLastEightDays(t *testing.T) {
	check := trimCheck(t, "wsl", "loaded", unixAt(7*24*time.Hour))
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok for a trim 7 days ago, got %v %q", verdict, reason)
	}
}

// A host that is not WSL trims by systemd's own timer; the check asks nothing
// of systemctl there.
func TestWSLTrimIsInertOnAHostThatIsNotWSL(t *testing.T) {
	program, callLog := fakeTrimSystemctl(t, "loaded", "")
	check := &doctor.WSLTrim{Program: program, DetectVirt: detectVirt("none"), Now: func() time.Time { return trimNow }}
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK || !strings.HasPrefix(reason, "n/a") {
		t.Fatalf("expected an n/a ok off WSL, got %v %q", verdict, reason)
	}
	if calls, _ := os.ReadFile(callLog); len(calls) != 0 {
		t.Errorf("expected systemctl not asked off WSL, got %q", calls)
	}
}

func TestWSLTrimCannotTellWithoutAnFstrimTimerUnit(t *testing.T) {
	check := trimCheck(t, "wsl", "not-found", "")
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorCannotTell || !strings.Contains(reason, "fstrim.timer") {
		t.Fatalf("expected cannot-tell naming fstrim.timer, got %v %q", verdict, reason)
	}
}

func TestWSLTrimCannotTellWhenSystemctlCannotBeRun(t *testing.T) {
	check := &doctor.WSLTrim{Program: "/no/such/systemctl", DetectVirt: detectVirt("wsl"), Now: func() time.Time { return trimNow }}
	if verdict, _ := check.Probe(context.Background()); verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %v", verdict)
	}
}

// The cure needs root, which a Builder or the doctor has not: it is one note
// to the Mayor naming the command that files the Governor's step.
func TestWSLTrimCureWritesOneNoteNamingTheCommandAndChangesNothingElse(t *testing.T) {
	check := trimCheck(t, "wsl", "loaded", "")
	var notes []string
	check.Note = func(_ context.Context, text string) error { notes = append(notes, text); return nil }
	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("cure: %v", err)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "mw host trim-setup") || !strings.Contains(notes[0], "fstrim.timer") {
		t.Fatalf("expected one note naming the command, got %q", notes)
	}
	wait, cap := check.Damper()
	if wait < 24*time.Hour || cap != 0 {
		t.Errorf("expected a day's wait and no cap for a note-only cure, got %v %d", wait, cap)
	}
	if check.Name() != "wsl-trim" || check.WayBack() == "" {
		t.Errorf("unexpected name %q or empty way back", check.Name())
	}
}

// The root script is the one thing run as root on the host, by the hands step
// and inside the PowerShell line; it holds neither quote, so the line survives
// PowerShell and wsl.exe.
func TestWSLTrimRootScriptWritesBothDropInsAndRestartsTheTimer(t *testing.T) {
	script := doctor.WSLTrimRootScript()
	if strings.ContainsAny(script, "'\"`$") {
		t.Errorf("expected no quote, backtick or dollar in the script, got %q", script)
	}
	for _, want := range []string{
		"/etc/systemd/system/fstrim.timer.d/wsl.conf", "/etc/systemd/system/fstrim.service.d/wsl.conf",
		"systemctl daemon-reload", "systemctl restart fstrim.timer",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("expected the script to hold %q, got %q", want, script)
		}
	}
	encoded := regexp.MustCompile(`echo ([A-Za-z0-9+/=]+) \| base64 -d`).FindAllStringSubmatch(script, -1)
	if len(encoded) != 2 {
		t.Fatalf("expected two encoded drop-ins, got %q", script)
	}
	for _, m := range encoded {
		raw, err := base64.StdEncoding.DecodeString(m[1])
		if err != nil || string(raw) != "[Unit]\nConditionVirtualization=\n" {
			t.Errorf("expected the drop-in [Unit] then an empty ConditionVirtualization=, got %q (%v)", raw, err)
		}
	}
}

func TestWSLTrimPowerShellLineRunsTheScriptAsRootInUbuntu(t *testing.T) {
	line := doctor.WSLTrimPowerShellLine()
	prefix := "wsl -d Ubuntu-24.04 -u root -- sh -c '"
	if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, "'") {
		t.Fatalf("expected the line to be %s<script>', got %q", prefix, line)
	}
	if got := strings.TrimSuffix(strings.TrimPrefix(line, prefix), "'"); got != doctor.WSLTrimRootScript() {
		t.Errorf("expected the line to carry the root script, got %q", got)
	}
	if strings.Contains(line, "\n") || strings.HasPrefix(line, "PS ") {
		t.Errorf("expected one line with no prompt, got %q", line)
	}
}

func TestWSLTrimDropInsAreWhereTheProbeLooks(t *testing.T) {
	for _, path := range doctor.WSLTrimDropIns {
		if !strings.Contains(doctor.WSLTrimRootScript(), path) {
			t.Errorf("expected the script to write %s", path)
		}
	}
}

func TestWSLTrimProbeScriptSaysWSLAndDropIn(t *testing.T) {
	script := doctor.WSLTrimProbeScript()
	for _, want := range []string{"systemd-detect-virt", doctor.WSLTrimDropIns[0], doctor.WSLTrimDropIns[1],
		doctor.WSLTrimNotWSL, doctor.WSLTrimHasDropIn, doctor.WSLTrimLacksDropIn} {
		if !strings.Contains(script, want) {
			t.Errorf("expected the probe to hold %q", want)
		}
	}
}
