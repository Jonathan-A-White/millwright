package reaper_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/reaper"
)

// standIn is a program that stands in for mw: it records the arguments it was
// started with, the session it runs in and its own pid, and then exits.
func standIn(t *testing.T) (program, said string) {
	t.Helper()
	dir := t.TempDir()
	program = filepath.Join(dir, "mw")
	said = filepath.Join(dir, "said")
	script := "#!/bin/sh\n" +
		"{ for arg in \"$@\"; do echo \"arg=$arg\"; done; echo \"pid=$$\"; echo \"sid=$(ps -o sid= -p $$ | tr -d ' ')\"; } > " + said + ".part\n" +
		"mv " + said + ".part " + said + "\n"
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return program, said
}

func waitForFile(t *testing.T, path string) string {
	t.Helper()
	for until := time.Now().Add(10 * time.Second); time.Now().Before(until); time.Sleep(20 * time.Millisecond) {
		if raw, err := os.ReadFile(path); err == nil {
			return string(raw)
		}
	}
	t.Fatalf("waited for %s and it never appeared", path)
	return ""
}

func TestArmStartsSeatReapOnTheWindowInASessionOfItsOwn(t *testing.T) {
	program, said := standIn(t)

	err := reaper.New(program).Arm(context.Background(), application.ReapArming{Seat: "mayor", Window: "@3", Mode: application.ReapSuccessor})
	if err != nil {
		t.Fatalf("arming: %v", err)
	}

	got := waitForFile(t, said)
	for _, want := range []string{"arg=seat", "arg=reap", "arg=mayor", "arg=--window", "arg=@3"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected the reaper to be started with %q, it said:\n%s", want, got)
		}
	}
	if strings.Contains(got, "--when-idle") {
		t.Errorf("expected successor mode to be the default, it said:\n%s", got)
	}

	// It is the leader of a session of its own: closing the window it watches
	// does not take it with it.
	var pid, sid int
	for _, line := range strings.Split(got, "\n") {
		if v, ok := strings.CutPrefix(line, "pid="); ok {
			pid, _ = strconv.Atoi(v)
		}
		if v, ok := strings.CutPrefix(line, "sid="); ok {
			sid, _ = strconv.Atoi(v)
		}
	}
	if pid == 0 || pid != sid {
		t.Errorf("expected the reaper to lead a session of its own, pid %d in session %d", pid, sid)
	}
}

func TestArmPassesWhenIdleForIdleMode(t *testing.T) {
	program, said := standIn(t)
	err := reaper.New(program).Arm(context.Background(), application.ReapArming{Seat: "millhand", Window: "@7", Mode: application.ReapWhenIdle})
	if err != nil {
		t.Fatalf("arming: %v", err)
	}
	if got := waitForFile(t, said); !strings.Contains(got, "arg=--when-idle") {
		t.Errorf("expected --when-idle, it said:\n%s", got)
	}
}

func TestArmRefusesWhatIsNotAWatch(t *testing.T) {
	for name, arming := range map[string]application.ReapArming{
		"no seat":   {Window: "@3", Mode: application.ReapSuccessor},
		"no window": {Seat: "mayor", Mode: application.ReapSuccessor},
		"no mode":   {Seat: "mayor", Window: "@3"},
	} {
		if err := reaper.New("/bin/true").Arm(context.Background(), arming); err == nil {
			t.Errorf("%s: expected arming to be refused", name)
		}
	}
	if err := reaper.New("/no/such/mw").Arm(context.Background(), application.ReapArming{Seat: "mayor", Window: "@3", Mode: application.ReapSuccessor}); err == nil {
		t.Errorf("expected a program that cannot be started to be an error")
	}
}
