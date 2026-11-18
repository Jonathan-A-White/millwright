package doctor_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
}

// An antivirus took the base64 form of this paste for a threat and quarantined
// the Governor's PowerShell history (mw-gq6.1431): the drop-in text is in the
// script as written, and nothing here decodes it.
func TestWSLTrimRootScriptWritesPlainText(t *testing.T) {
	script := doctor.WSLTrimRootScript()
	for _, banned := range []string{"base64", "encodedcommand"} {
		if strings.Contains(strings.ToLower(script), banned) {
			t.Errorf("expected plain text, the script holds %q: %q", banned, script)
		}
	}
	for _, path := range doctor.WSLTrimDropIns {
		if !strings.Contains(script, "> "+path) {
			t.Errorf("expected the script to write %s with a redirect, got %q", path, script)
		}
	}
	// two printfs, each of the drop-in's own words: the [Unit] section and an
	// empty ConditionVirtualization=
	if got := strings.Count(script, `printf \[Unit]\\nConditionVirtualization=\\n > `); got != 2 {
		t.Errorf("expected both drop-ins printed as written, got %d in %q", got, script)
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

// driveFreeCheck is the check over a report file in a temp dir holding text
// (nothing when text is empty and missing is true).
func driveFreeCheck(t *testing.T, virt string, text string, missing bool) *doctor.WSLDriveFree {
	t.Helper()
	path := filepath.Join(t.TempDir(), "windows-drive-free")
	if !missing {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatalf("writing the report: %v", err)
		}
	}
	return &doctor.WSLDriveFree{Path: path, DetectVirt: detectVirt(virt), Now: func() time.Time { return trimNow }}
}

// report is a line the Windows task writes: the drive, its free bytes, and the
// unix second it was read.
func report(drive string, freeGB float64, ago time.Duration) string {
	return fmt.Sprintf("%s %d %d\n", drive, int64(freeGB*(1<<30)), trimNow.Add(-ago).Unix())
}

// The desktop's D: sat at 0.01 GB free with nothing in the doctor able to see
// it (mw-gq6.1366): a free figure under 5 GB is the alarm.
func TestWSLDriveFreeIsFaultyUnderFiveGigabytes(t *testing.T) {
	check := driveFreeCheck(t, "wsl", report("D:", 0.01, time.Minute), false)
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty for 0.01 GB free, got %v %q", verdict, reason)
	}
	for _, want := range []string{"D:", "0.0 GB free", "5 GB", "wsl --shutdown"} {
		if !strings.Contains(reason, want) {
			t.Errorf("expected the reason to hold %q, got %q", want, reason)
		}
	}
}

func TestWSLDriveFreeIsOkWhenWellOverTheFloorsAndSaysTheFigure(t *testing.T) {
	check := driveFreeCheck(t, "wsl", report("D:", 42, time.Minute), false)
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK || !strings.Contains(reason, "D:") || !strings.Contains(reason, "42 GB free") {
		t.Fatalf("expected ok saying D: has 42 GB free, got %v %q", verdict, reason)
	}
	// At exactly 5 GB it is no longer the full drive's alarm: dispatch is
	// refusing, said as a warning (mw-gq6.1428).
	check = driveFreeCheck(t, "wsl", report("C:", 5, time.Minute), false)
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorFaulty || strings.Contains(reason, "wsl --shutdown") || !strings.Contains(reason, "refusing now") {
		t.Fatalf("expected the warning that dispatch is refusing at exactly 5 GB, got %v %q", verdict, reason)
	}
}

func TestWSLDriveFreeIsInertOnAHostThatIsNotWSL(t *testing.T) {
	check := driveFreeCheck(t, "none", "", true)
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK || !strings.HasPrefix(reason, "n/a") {
		t.Fatalf("expected an n/a ok off WSL, got %v %q", verdict, reason)
	}
}

// A WSL host with no report is a host whose Windows drive nothing can see: a
// named alarm (the Laptop had none, mw-gq6.1428), with the line that installs
// the task that writes it.
func TestWSLDriveFreeIsFaultyWithoutAReportAndNamesTheInstallLine(t *testing.T) {
	check := driveFreeCheck(t, "wsl", "", true)
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty with no report, got %v %q", verdict, reason)
	}
	if !strings.Contains(reason, "mw host wsl-flag --install-line") {
		t.Errorf("expected the reason to name the install line, got %q", reason)
	}
}

func TestWSLDriveFreeCannotTellFromAReportItCannotReadOrOneThatIsStale(t *testing.T) {
	for name, text := range map[string]string{
		"garbage":       "not a report\n",
		"no number":     "D: lots " + fmt.Sprint(trimNow.Unix()) + "\n",
		"stale by days": report("D:", 100, 3*24*time.Hour),
	} {
		verdict, reason := driveFreeCheck(t, "wsl", text, false).Probe(context.Background())
		if verdict != application.DoctorCannotTell {
			t.Errorf("%s: expected cannot-tell, got %v %q", name, verdict, reason)
		}
	}
}

// A stale report does not hide a full drive: what it last said is still said.
func TestWSLDriveFreeStaleReportStillSaysAFullDrive(t *testing.T) {
	verdict, reason := driveFreeCheck(t, "wsl", report("D:", 0.01, 3*24*time.Hour), false).Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "3 days old") {
		t.Fatalf("expected faulty naming the report's age, got %v %q", verdict, reason)
	}
}

func TestWSLDriveFreeCureWritesOneNoteAndChangesNothingElse(t *testing.T) {
	check := driveFreeCheck(t, "wsl", report("D:", 0.01, time.Minute), false)
	var notes []string
	check.Note = func(_ context.Context, text string) error { notes = append(notes, text); return nil }
	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("cure: %v", err)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "5 GB") {
		t.Fatalf("expected one note naming the threshold, got %q", notes)
	}
	if wait, cap := check.Damper(); wait <= 0 || cap != 0 {
		t.Errorf("expected a wait and no cap for a note-only cure, got %v %d", wait, cap)
	}
	if check.Name() != "wsl-drive-free" || check.WayBack() == "" {
		t.Errorf("unexpected name %q or empty way back", check.Name())
	}
}

// mw-gq6.1428: a second, higher floor (30 GB) warns before dispatch's own
// (15 GB) stops it, and says so; the 5 GB faulty floor is as it was.
func TestWSLDriveFreeWarnsUnderTheHigherFloor(t *testing.T) {
	check := driveFreeCheck(t, "wsl", report("D:", 25, time.Minute), false)
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected a warning (faulty) for 25 GB free, got %v %q", verdict, reason)
	}
	for _, want := range []string{"D:", "25.0 GB", "30 GB", "low, not yet refusing", "15 GB"} {
		if !strings.Contains(reason, want) {
			t.Errorf("expected the warning to hold %q, got %q", want, reason)
		}
	}
	if strings.Contains(reason, "wsl --shutdown") {
		t.Errorf("a warning is not the full drive's alarm, got %q", reason)
	}
	// The cure's note says it is a low warning, not the drive full.
	var note string
	check.Note = func(_ context.Context, text string) error { note = text; return nil }
	if err := check.Cure(context.Background()); err != nil || !strings.Contains(note, "not yet refusing") {
		t.Errorf("expected the cure to note a low, not-yet-refusing drive, got %q (%v)", note, err)
	}

	check = driveFreeCheck(t, "wsl", report("D:", 40, time.Minute), false)
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK || !strings.Contains(reason, "40 GB free") {
		t.Errorf("expected ok for 40 GB free, got %v %q", verdict, reason)
	}

	check = driveFreeCheck(t, "wsl", report("D:", 3, time.Minute), false)
	verdict, reason = check.Probe(context.Background())
	if verdict != application.DoctorFaulty || strings.Contains(reason, "not yet refusing") || !strings.Contains(reason, "wsl --shutdown") {
		t.Errorf("expected the faulty alarm for 3 GB free, got %v %q", verdict, reason)
	}

	// The warning floor is the config's: 50 GB warns 40 GB free.
	check = driveFreeCheck(t, "wsl", report("D:", 40, time.Minute), false)
	check.WarnGB = 50
	if verdict, _ := check.Probe(context.Background()); verdict != application.DoctorFaulty {
		t.Errorf("expected a warning at 40 GB free under a 50 GB warning floor, got %v", verdict)
	}
}
