package doctor_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// wifiInterfacesFixture is the "SSID"/"BSSID" lines of a real `netsh wlan show
// interfaces`, as the Millhand mailed them, with the leading whitespace real
// Windows output has: BSSID before SSID, so a parser that takes the first
// line starting "SSID" without checking the key exactly would take the wrong
// one.
const wifiInterfacesFixture = `
    Name                   : Wi-Fi
    State                  : connected
    BSSID                  : 00:11:22:33:44:55
    SSID                   : Whitehouse
    Signal                 : 100%
`

// wifiInterfacesFixtureNoSSID is the same shape with no network associated:
// no SSID line at all.
const wifiInterfacesFixtureNoSSID = `
    Name                   : Wi-Fi
    State                  : disconnected
`

// fakeNetsh writes a stand-in for netsh.exe that answers `wlan show
// interfaces` with the given fixture, and `wlan disconnect` / `wlan connect
// name=...` with success, logging every call (one line per call, arguments
// space-joined) to a file this test can read back.
func fakeNetsh(t *testing.T, showInterfacesOutput string) (program, callLog string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for netsh is a shell script")
	}
	dir := t.TempDir()
	program = filepath.Join(dir, "netsh-stand-in")
	callLog = filepath.Join(dir, "calls")

	fixture := filepath.Join(dir, "show-interfaces.txt")
	if err := os.WriteFile(fixture, []byte(showInterfacesOutput), 0o644); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}

	script := fmt.Sprintf(`#!/bin/sh
echo "$*" >>%q
if [ "$1" = wlan ] && [ "$2" = show ] && [ "$3" = interfaces ]; then
  cat %q
  exit 0
fi
if [ "$1" = wlan ] && [ "$2" = disconnect ]; then
  exit 0
fi
if [ "$1" = wlan ] && [ "$2" = connect ]; then
  exit 0
fi
exit 1
`, callLog, fixture)
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return program, callLog
}

// callLines reads back fakeNetsh's call log, one entry per call, in order.
func callLines(t *testing.T, callLog string) []string {
	t.Helper()
	data, err := os.ReadFile(callLog)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("reading the call log: %v", err)
	}
	text := strings.TrimRight(string(data), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

// existingFile is a path that exists, standing in for powershell.exe on a
// Windows-backed host.
func existingFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "powershell.exe")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("writing a stand-in powershell.exe: %v", err)
	}
	return path
}

// missingFile is a path that does not exist, standing in for a host that is
// not Windows-backed.
func missingFile(t *testing.T) string {
	return filepath.Join(t.TempDir(), "no-such-powershell.exe")
}

// localListener is a TCP listener a probe can reach, standing in for a reach
// host that is up. It closes itself when the test ends.
func localListener(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("starting a local listener: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln.Addr().String()
}

// unreachableHost is a host:port nothing listens on, refusing every
// connection immediately rather than timing out: fast to fail in a test.
func unreachableHost(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a closed port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

func noSleep(time.Duration) {}

func TestTheWifiProbeIsOKWhenAReachHostConnects(t *testing.T) {
	check := &doctor.Wifi{
		Reach:      []string{localListener(t)},
		Powershell: existingFile(t),
		State:      doctor.New(t.TempDir()),
	}
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
}

func TestTheWifiProbeWaitsFiveMinutesBeforeItCallsAnOutageFaulty(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	check := &doctor.Wifi{
		Reach:      []string{unreachableHost(t)},
		Powershell: existingFile(t),
		State:      doctor.New(t.TempDir()),
		Now:        func() time.Time { return now },
	}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell || !strings.Contains(reason, "waiting 5m") {
		t.Fatalf("expected cannot-tell waiting 5m, got %s (%s)", verdict, reason)
	}

	now = now.Add(4 * time.Minute)
	verdict, reason = check.Probe(context.Background())
	if verdict != application.DoctorCannotTell || !strings.Contains(reason, "waiting 5m") {
		t.Fatalf("expected still cannot-tell waiting 5m at 4 minutes, got %s (%s)", verdict, reason)
	}

	now = now.Add(1 * time.Minute)
	verdict, reason = check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty at 5 minutes, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, "internet unreachable since") || !strings.Contains(reason, "2026-09-23T12:00:00Z") {
		t.Fatalf("expected the reason to carry the state's firstFaulty, got %q", reason)
	}
}

func TestTheWifiProbeIsCannotTellWhenPowershellIsAbsent(t *testing.T) {
	check := &doctor.Wifi{
		Reach:      []string{unreachableHost(t)},
		Powershell: missingFile(t),
		State:      doctor.New(t.TempDir()),
	}
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, "not Windows-backed") {
		t.Errorf("expected the reason to explain why, got %q", reason)
	}
}

func TestTheWifiCuresExecSequenceIsShowDisconnectConnect(t *testing.T) {
	program, calls := fakeNetsh(t, wifiInterfacesFixture)
	check := &doctor.Wifi{Netsh: program, Reach: []string{localListener(t)}, Sleep: noSleep}

	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("curing: %v", err)
	}

	got := callLines(t, calls)
	want := []string{"wlan show interfaces", "wlan disconnect", "wlan connect name=Whitehouse"}
	if len(got) != len(want) {
		t.Fatalf("expected exactly %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected call %d to be %q, got %q (all calls: %v)", i, want[i], got[i], got)
		}
	}
}

func TestTheWifiCureTakesSSIDNotBSSID(t *testing.T) {
	program, calls := fakeNetsh(t, wifiInterfacesFixture)
	check := &doctor.Wifi{Netsh: program, Reach: []string{localListener(t)}, Sleep: noSleep}

	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("curing: %v", err)
	}
	got := callLines(t, calls)
	if got[len(got)-1] != "wlan connect name=Whitehouse" {
		t.Fatalf("expected the connect call to name the SSID (Whitehouse), not the BSSID, got %q", got[len(got)-1])
	}
}

func TestTheWifiCureRefusesWhenTheFixtureHasNoSSID(t *testing.T) {
	program, calls := fakeNetsh(t, wifiInterfacesFixtureNoSSID)
	check := &doctor.Wifi{Netsh: program, Sleep: noSleep}

	err := check.Cure(context.Background())
	if err == nil {
		t.Fatal("expected the cure to refuse with no SSID to rejoin")
	}

	got := callLines(t, calls)
	if len(got) != 1 || got[0] != "wlan show interfaces" {
		t.Fatalf("expected nothing exec'd after the show, got %v", got)
	}
}

func TestTheWifiDamperIsThirtyMinutesCapThree(t *testing.T) {
	check := &doctor.Wifi{}
	wait, capPerEpisode := check.Damper()
	if wait != doctor.WifiDamperWait || capPerEpisode != doctor.WifiDamperCap {
		t.Fatalf("expected %s/%d, got %s/%d", doctor.WifiDamperWait, doctor.WifiDamperCap, wait, capPerEpisode)
	}
}

func TestTheWifiWayBackNamesTheSSIDReadFresh(t *testing.T) {
	program, _ := fakeNetsh(t, wifiInterfacesFixture)
	check := &doctor.Wifi{Netsh: program}

	way := check.WayBack()
	if way != "netsh wlan connect name=Whitehouse" {
		t.Fatalf("expected the way back to name the SSID, got %q", way)
	}
}
