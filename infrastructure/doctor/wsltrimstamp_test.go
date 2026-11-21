package doctor_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// fakeSystemd255 is a stand-in for systemctl as systemd 255 answers on the
// desktop, the Laptop and cloud9: it ignores --timestamp=unix in `systemctl
// show` and prints LastTriggerUSec as 'Mon 2026-11-16 19:19:56 UTC' in the zone
// $TZ names (the desktop's own zone, EST, when no TZ is given).
func fakeSystemd255(t *testing.T, ranAt time.Time) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for systemctl is a shell script")
	}
	if _, err := exec.LookPath("date"); err != nil {
		t.Skip("the stand-in needs date")
	}
	program := filepath.Join(t.TempDir(), "systemctl-255")
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = show ] && [ "$2" = fstrim.timer ]; then
  zone="${TZ:-America/New_York}"
  echo "LastTriggerUSec=$(TZ="$zone" date -d @%d '+%%a %%Y-%%m-%%d %%H:%%M:%%S %%Z')"
  echo "LoadState=loaded"
  exit 0
fi
exit 1
`, ranAt.Unix())
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return program
}

// mw-gq6.1527: the doctor said 'fstrim.timer has never run on this WSL host'
// for days after the trim was installed on the desktop (2026-11-17 09:12 EST),
// because systemd 255 printed the formatted time the check did not read.
func TestWSLTrimReadsTheTimeSystemd255PrintsForTheLastTrim(t *testing.T) {
	for _, tt := range []struct {
		ago     time.Duration
		verdict application.Verdict
		want    string
	}{
		{2 * 24 * time.Hour, application.DoctorOK, "last ran 2 days ago"},
		{9 * 24 * time.Hour, application.DoctorFaulty, "last ran 9 days ago"},
	} {
		program := fakeSystemd255(t, trimNow.Add(-tt.ago))
		check := &doctor.WSLTrim{Program: program, DetectVirt: detectVirt("wsl"), DropIns: []string{}, Now: func() time.Time { return trimNow }}
		verdict, reason := check.Probe(context.Background())
		if verdict != tt.verdict || !strings.Contains(reason, tt.want) {
			t.Errorf("a trim %v ago: got %v %q, want %v naming %q", tt.ago, verdict, reason, tt.verdict, tt.want)
		}
	}
}

// mw-gq6.1527 sweep: on the Laptop the drop-ins were in, but fstrim.timer had
// never run (LAST "-"): restarting a Persistent timer with no stamp runs
// nothing until its next calendar time, so the root script runs one trim at
// once, which stamps the timer.
func TestWSLTrimRootScriptRunsOneTrimAtOnce(t *testing.T) {
	script := doctor.WSLTrimRootScript()
	if !strings.HasSuffix(script, "systemctl restart fstrim.timer && systemctl start fstrim.service") {
		t.Errorf("expected the script to end by starting fstrim.service once, got %q", script)
	}
}
