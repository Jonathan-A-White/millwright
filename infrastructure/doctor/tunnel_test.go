package doctor_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// tunnelListeningFixture and tunnelNotListeningFixture are `ss -ltn`'s own
// shape: a header line always printed, and, only when something is
// listening, a LISTEN line under it.
const tunnelListeningFixture = `State  Recv-Q Send-Q Local Address:Port Peer Address:Port
LISTEN 0      128        127.0.0.1:2222      0.0.0.0:*
`
const tunnelNotListeningFixture = `State  Recv-Q Send-Q Local Address:Port Peer Address:Port
`

// fakeTunnelSSH writes a stand-in for ssh that prints output and leaves with
// status, logging every call (one line per call, arguments space-joined) to
// a file this test can read back.
func fakeTunnelSSH(t *testing.T, output string, status int) (program, callLog string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for ssh is a shell script")
	}
	dir := t.TempDir()
	program = filepath.Join(dir, "ssh-stand-in")
	callLog = filepath.Join(dir, "calls")

	fixture := filepath.Join(dir, "output.txt")
	if err := os.WriteFile(fixture, []byte(output), 0o644); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}

	script := fmt.Sprintf(`#!/bin/sh
echo "$*" >>%q
cat %q
exit %d
`, callLog, fixture, status)
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return program, callLog
}

// fakeTunnelSystemctl writes a stand-in for systemctl that always succeeds,
// logging every call to a file this test can read back.
func fakeTunnelSystemctl(t *testing.T) (program, callLog string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for systemctl is a shell script")
	}
	dir := t.TempDir()
	program = filepath.Join(dir, "systemctl-stand-in")
	callLog = filepath.Join(dir, "calls")

	script := fmt.Sprintf(`#!/bin/sh
echo "$*" >>%q
exit 0
`, callLog)
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return program, callLog
}

func TestTheTunnelProbeIsCannotTellWhenTheInternetIsUnreachable(t *testing.T) {
	program, calls := fakeTunnelSSH(t, tunnelListeningFixture, 0)
	check := &doctor.Tunnel{Reach: []string{unreachableHost(t)}, Host: "vps", SSH: program}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, "internet unreachable") {
		t.Errorf("expected the reason to say so, got %q", reason)
	}
	if got := callLines(t, calls); len(got) != 0 {
		t.Fatalf("expected nothing exec'd, got %v", got)
	}
}

func TestTheTunnelProbeIsOKWhenTheListenerIsPresent(t *testing.T) {
	program, calls := fakeTunnelSSH(t, tunnelListeningFixture, 0)
	check := &doctor.Tunnel{Reach: []string{localListener(t)}, Host: "vps", SSH: program}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}

	got := callLines(t, calls)
	want := "-o BatchMode=yes -o ConnectTimeout=10 vps ss -ltn sport = :2222"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("expected ssh to be run with exactly %q, got %v", want, got)
	}
}

func TestTheTunnelProbeIsFaultyWhenTheListenerIsAbsent(t *testing.T) {
	program, _ := fakeTunnelSSH(t, tunnelNotListeningFixture, 0)
	check := &doctor.Tunnel{Reach: []string{localListener(t)}, Host: "vps", SSH: program}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, "vps") {
		t.Errorf("expected the reason to name the host, got %q", reason)
	}
}

func TestTheTunnelProbeIsCannotTellWhenSSHFails(t *testing.T) {
	program, calls := fakeTunnelSSH(t, "", 255)
	check := &doctor.Tunnel{Reach: []string{localListener(t)}, Host: "vps", SSH: program}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s (%s)", verdict, reason)
	}
	if got := callLines(t, calls); len(got) != 1 {
		t.Fatalf("expected ssh to have been tried once, got %v", got)
	}
}

func TestTheTunnelCureRestartsTheUnitExactlyThenReProbes(t *testing.T) {
	sshProgram, sshCalls := fakeTunnelSSH(t, tunnelNotListeningFixture, 0)
	systemctlProgram, systemctlCalls := fakeTunnelSystemctl(t)
	check := &doctor.Tunnel{
		Reach: []string{localListener(t)}, Host: "vps",
		SSH: sshProgram, Systemctl: systemctlProgram, Sleep: noSleep,
	}

	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("curing: %v", err)
	}

	got := callLines(t, systemctlCalls)
	want := []string{"--user restart reverse-tunnel.service"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("expected systemctl to have run exactly %v, got %v", want, got)
	}

	sshGot := callLines(t, sshCalls)
	if len(sshGot) != 1 {
		t.Fatalf("expected the cure to re-probe over ssh exactly once, got %v", sshGot)
	}
}

func TestTheTunnelHonoursCommandAndUnitOverrides(t *testing.T) {
	sshProgram, sshCalls := fakeTunnelSSH(t, tunnelNotListeningFixture, 0)
	systemctlProgram, systemctlCalls := fakeTunnelSystemctl(t)
	check := &doctor.Tunnel{
		Reach: []string{localListener(t)}, Host: "vps",
		Command: "ss -ltn", Unit: "other-tunnel.service",
		SSH: sshProgram, Systemctl: systemctlProgram, Sleep: noSleep,
	}

	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("curing: %v", err)
	}

	systemctlGot := callLines(t, systemctlCalls)
	if len(systemctlGot) != 1 || systemctlGot[0] != "--user restart other-tunnel.service" {
		t.Fatalf("expected the overridden unit to have been restarted, got %v", systemctlGot)
	}

	sshGot := callLines(t, sshCalls)
	want := "-o BatchMode=yes -o ConnectTimeout=10 vps ss -ltn"
	if len(sshGot) != 1 || sshGot[0] != want {
		t.Fatalf("expected the overridden command to have been run, got %v", sshGot)
	}

	if way := check.WayBack(); way != "systemctl --user stop other-tunnel.service" {
		t.Fatalf("expected the way back to name the overridden unit, got %q", way)
	}
}

func TestTheTunnelDamperIsFifteenMinutesCapThree(t *testing.T) {
	check := &doctor.Tunnel{}
	wait, capPerEpisode := check.Damper()
	if wait != doctor.TunnelDamperWait || capPerEpisode != doctor.TunnelDamperCap {
		t.Fatalf("expected %s/%d, got %s/%d", doctor.TunnelDamperWait, doctor.TunnelDamperCap, wait, capPerEpisode)
	}
}

func TestTheTunnelWayBackNamesTheUnit(t *testing.T) {
	check := &doctor.Tunnel{}
	if way := check.WayBack(); way != "systemctl --user stop reverse-tunnel.service" {
		t.Fatalf("expected the default unit's way back, got %q", way)
	}
}
