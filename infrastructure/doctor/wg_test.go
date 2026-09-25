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

// wg0AddrFixture and wg0NoDeviceFixture are `ip -4 -o addr show dev wg0`'s
// own shape: one line naming the interface's address when it exists, and,
// when it does not, nothing on stdout and a message on stderr with a
// non-zero exit.
const wg0AddrFixture = `4: wg0    inet 10.88.0.5/24 scope global wg0\       valid_lft forever preferred_lft forever
`

// fakeIP writes a stand-in for `ip` that answers `-4 -o addr show dev wg0`
// with the given fixture and status, logging every call to a file this test
// can read back.
func fakeIP(t *testing.T, output string, status int) (program, callLog string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for ip is a shell script")
	}
	dir := t.TempDir()
	program = filepath.Join(dir, "ip-stand-in")
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

// fakeSudo writes a stand-in for sudo that either succeeds or refuses the way
// `sudo -n` does when it has no cached credential and cannot prompt: exit 1,
// "a password is required" on stderr. It logs every call to a file this test
// can read back.
func fakeSudo(t *testing.T, refuse bool) (program, callLog string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for sudo is a shell script")
	}
	dir := t.TempDir()
	program = filepath.Join(dir, "sudo-stand-in")
	callLog = filepath.Join(dir, "calls")

	body := "exit 0\n"
	if refuse {
		body = "echo 'sudo: a password is required' 1>&2\nexit 1\n"
	}
	script := fmt.Sprintf(`#!/bin/sh
echo "$*" >>%q
%s`, callLog, body)
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return program, callLog
}

func alwaysDial() func(context.Context, string) bool {
	return func(context.Context, string) bool { return true }
}

func neverDial() func(context.Context, string) bool {
	return func(context.Context, string) bool { return false }
}

func TestTheWgProbeIsOKWhenTheHubAnswers(t *testing.T) {
	ip, _ := fakeIP(t, wg0AddrFixture, 0)
	check := &doctor.Wg{
		Reach: []string{localListener(t)}, Hub: "10.88.0.1:22",
		IP: ip, State: doctor.New(t.TempDir()), Dial: alwaysDial(),
	}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
}

func TestTheWgProbeIsCannotTellWhenTheInternetIsUnreachable(t *testing.T) {
	ip, calls := fakeIP(t, wg0AddrFixture, 0)
	check := &doctor.Wg{
		Reach: []string{unreachableHost(t)}, Hub: "10.88.0.1:22",
		IP: ip, State: doctor.New(t.TempDir()), Dial: neverDial(),
	}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, "internet unreachable") {
		t.Errorf("expected the reason to say so, got %q", reason)
	}
	if got := callLines(t, calls); len(got) != 0 {
		t.Fatalf("expected ip never run, got %v", got)
	}
}

func TestTheWgProbeIsCannotTellWhenThereIsNoWg0Interface(t *testing.T) {
	ip, _ := fakeIP(t, "", 1)
	check := &doctor.Wg{
		Reach: []string{localListener(t)}, Hub: "10.88.0.1:22",
		IP: ip, State: doctor.New(t.TempDir()), Dial: alwaysDial(),
	}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, "no wg0 interface") {
		t.Errorf("expected the reason to say so, got %q", reason)
	}
}

func TestTheWgProbeIsCannotTellWhenThisHostIsTheHub(t *testing.T) {
	ip, _ := fakeIP(t, wg0AddrFixture, 0) // wg0's address is 10.88.0.5
	check := &doctor.Wg{
		Reach: []string{localListener(t)}, Hub: "10.88.0.5:22",
		IP: ip, State: doctor.New(t.TempDir()), Dial: alwaysDial(),
	}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, "this host is the hub") {
		t.Errorf("expected the reason to say so, got %q", reason)
	}
}

func TestTheWgProbeWaitsThreeMinutesBeforeItCallsTheHubFaulty(t *testing.T) {
	ip, _ := fakeIP(t, wg0AddrFixture, 0)
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	check := &doctor.Wg{
		Reach: []string{localListener(t)}, Hub: "10.88.0.1:22",
		IP: ip, State: doctor.New(t.TempDir()), Dial: neverDial(),
		Now: func() time.Time { return now },
	}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell || !strings.Contains(reason, "waiting 3m") {
		t.Fatalf("expected cannot-tell waiting 3m, got %s (%s)", verdict, reason)
	}

	now = now.Add(2 * time.Minute)
	verdict, reason = check.Probe(context.Background())
	if verdict != application.DoctorCannotTell || !strings.Contains(reason, "waiting 3m") {
		t.Fatalf("expected still cannot-tell waiting 3m at 2 minutes, got %s (%s)", verdict, reason)
	}

	now = now.Add(1 * time.Minute)
	verdict, reason = check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty at 3 minutes, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, "hub unreachable since") || !strings.Contains(reason, "2026-09-25T12:00:00Z") {
		t.Fatalf("expected the reason to carry the state's firstFaulty, got %q", reason)
	}
}

func TestTheWgCureRestartsTheUnitExactlyThenReProbes(t *testing.T) {
	ip, _ := fakeIP(t, wg0AddrFixture, 0)
	sudo, sudoCalls := fakeSudo(t, false)
	check := &doctor.Wg{
		Reach: []string{localListener(t)}, Hub: "10.88.0.1:22", Unit: "wg-quick@wg0",
		IP: ip, Sudo: sudo, State: doctor.New(t.TempDir()), Dial: alwaysDial(), Sleep: noSleep,
	}

	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("curing: %v", err)
	}

	got := callLines(t, sudoCalls)
	want := "-n systemctl restart wg-quick@wg0"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("expected sudo to have run exactly %q, got %v", want, got)
	}
}

func TestTheWgCureRefusesNamingTheMissingSudoersLineWhenSudoNeedsAPassword(t *testing.T) {
	sudo, sudoCalls := fakeSudo(t, true)
	check := &doctor.Wg{Unit: "wg-quick@wg0", Sudo: sudo, Sleep: noSleep}

	err := check.Cure(context.Background())
	if err == nil {
		t.Fatal("expected the cure to refuse")
	}
	if !strings.Contains(err.Error(), "sudoers") || !strings.Contains(err.Error(), "NOPASSWD") {
		t.Fatalf("expected the error to name the missing sudoers line, got %q", err)
	}
	if !strings.Contains(err.Error(), "systemctl restart wg-quick@wg0") {
		t.Fatalf("expected the error to name the unit, got %q", err)
	}

	got := callLines(t, sudoCalls)
	want := "-n systemctl restart wg-quick@wg0"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("expected sudo to have been tried once, got %v", got)
	}
}

func TestTheWgDamperIsFifteenMinutesCapThree(t *testing.T) {
	check := &doctor.Wg{}
	wait, capPerEpisode := check.Damper()
	if wait != doctor.WgDamperWait || capPerEpisode != doctor.WgDamperCap {
		t.Fatalf("expected %s/%d, got %s/%d", doctor.WgDamperWait, doctor.WgDamperCap, wait, capPerEpisode)
	}
}

func TestTheWgWayBackNamesTheUnitAndIsNeverRun(t *testing.T) {
	sudo, sudoCalls := fakeSudo(t, false)
	check := &doctor.Wg{Sudo: sudo}

	if way := check.WayBack(); way != "sudo systemctl stop wg-quick@wg0" {
		t.Fatalf("expected the default unit's way back, got %q", way)
	}
	if got := callLines(t, sudoCalls); len(got) != 0 {
		t.Fatalf("expected WayBack to run nothing, got %v", got)
	}
}

func TestTheWgHonoursUnitOverride(t *testing.T) {
	ip, _ := fakeIP(t, wg0AddrFixture, 0)
	sudo, sudoCalls := fakeSudo(t, false)
	check := &doctor.Wg{
		Reach: []string{localListener(t)}, Hub: "10.88.0.1:22", Unit: "other-wg.service",
		IP: ip, Sudo: sudo, State: doctor.New(t.TempDir()), Dial: alwaysDial(), Sleep: noSleep,
	}

	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("curing: %v", err)
	}

	got := callLines(t, sudoCalls)
	if len(got) != 1 || got[0] != "-n systemctl restart other-wg.service" {
		t.Fatalf("expected the overridden unit to have been restarted, got %v", got)
	}
	if way := check.WayBack(); way != "sudo systemctl stop other-wg.service" {
		t.Fatalf("expected the way back to name the overridden unit, got %q", way)
	}
}
