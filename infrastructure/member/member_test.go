package member

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// script writes a stand-in program and returns its path.
func script(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWriteFileMakesDirectoriesWithTheModeAndNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".config", "mw", "beads.env")
	h := &Host{}
	if err := h.WriteFile(path, "one\n", 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("beads.env = %v, %v; want mode 600", info, err)
	}
	if err := h.WriteFile(path, "two\n", 0o600); err == nil {
		t.Fatal("a second WriteFile overwrote the file")
	}
	if got, _ := os.ReadFile(path); string(got) != "one\n" {
		t.Errorf("the file holds %q after a refused write, want the first text", got)
	}
}

func TestEnableAndDisableRunSystemctlOnTheUserManager(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls")
	h := &Host{Systemctl: script(t, "systemctl", `echo "$@" >> `+log)}
	if err := h.EnableUnit(context.Background(), "dolt-beads.service"); err != nil {
		t.Fatal(err)
	}
	if err := h.DisableUnit(context.Background(), "dolt-beads.service"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(log)
	want := "--user daemon-reload\n--user enable --now dolt-beads.service\n--user disable --now dolt-beads.service\n"
	if string(got) != want {
		t.Errorf("systemctl ran:\n%s\nwant:\n%s", got, want)
	}
}

func TestEnableSaysWhatSystemctlSaidWhenItFails(t *testing.T) {
	h := &Host{Systemctl: script(t, "systemctl", `echo "Failed to connect to bus" >&2; exit 1`)}
	err := h.EnableUnit(context.Background(), "dolt-beads.service")
	if err == nil || !strings.Contains(err.Error(), "Failed to connect to bus") {
		t.Fatalf("EnableUnit = %v, want systemctl's own words", err)
	}
}

func TestLingerReadsLoginctl(t *testing.T) {
	for _, tc := range []struct {
		said string
		want bool
		err  bool
	}{{"Linger=yes", true, false}, {"Linger=no", false, false}, {"nonsense", false, true}} {
		h := &Host{Loginctl: script(t, "loginctl", `echo "`+tc.said+`"`)}
		got, err := h.LingerEnabled(context.Background())
		if got != tc.want || (err != nil) != tc.err {
			t.Errorf("loginctl says %q: LingerEnabled = %v, %v", tc.said, got, err)
		}
	}
}

func TestPortFreeAndServerAnswersSeeAListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	h := &Host{Wait: time.Second, Poll: 10 * time.Millisecond}
	if err := h.PortFree(context.Background(), port); err == nil {
		t.Error("PortFree is nil for a port with a listener")
	}
	if err := h.ServerAnswers(context.Background(), port); err != nil {
		t.Errorf("ServerAnswers = %v for a port with a listener", err)
	}
	listener.Close()
	if err := h.PortFree(context.Background(), port); err != nil {
		t.Errorf("PortFree = %v once the listener is gone", err)
	}
	if err := h.ServerAnswers(context.Background(), port); err == nil {
		t.Error("ServerAnswers is nil for a port nothing listens on")
	}
}

func TestDoltProgramIsAFullPathOrASayingHowToNameOne(t *testing.T) {
	dolt := script(t, "dolt", "exit 0")
	got, err := (&Host{}).DoltProgram(dolt)
	if err != nil || got != dolt {
		t.Errorf("DoltProgram(%q) = %q, %v", dolt, got, err)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := (&Host{}).DoltProgram(""); err == nil || !strings.Contains(err.Error(), "--dolt") {
		t.Errorf("DoltProgram with no dolt = %v, want it to say --dolt names one", err)
	}
}

func TestNewPasswordIsLongAndNeverTheSameTwice(t *testing.T) {
	h := &Host{}
	a, err := h.NewPassword()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := h.NewPassword()
	if len(a) < 32 || a == b {
		t.Errorf("passwords %q and %q", a, b)
	}
}

func TestInitBeadsPassesTheConnectionInTheEnvironmentAndNotInAnArgument(t *testing.T) {
	out := filepath.Join(t.TempDir(), "seen")
	t.Setenv("BEADS_DOLT_SERVER_PORT", "3307")
	h := &Host{Bd: script(t, "bd", `echo "$@" > `+out+`; env | grep -E '^BEADS_' >> `+out)}
	if err := h.InitBeads(context.Background(), t.TempDir(), "luke", 3308, "beads", "s3cret"); err != nil {
		t.Fatal(err)
	}
	seen, _ := os.ReadFile(out)
	args, env, _ := strings.Cut(string(seen), "\n")
	if strings.Contains(args, "s3cret") || !strings.Contains(args, "--server-port 3308") || strings.Contains(args, "migrate") {
		t.Errorf("bd was run with %q", args)
	}
	for _, want := range []string{"BEADS_DOLT_PASSWORD=s3cret", "BEADS_DOLT_SERVER_PORT=3308", "BEADS_DOLT_SERVER_MODE=1"} {
		if !strings.Contains(env, want) {
			t.Errorf("bd's environment lacks %s:\n%s", want, env)
		}
	}
	if strings.Contains(env, "=3307") {
		t.Errorf("bd saw the ambient port:\n%s", env)
	}
}

func TestInitBeadsStrikesThePasswordOutOfWhatBdSays(t *testing.T) {
	h := &Host{Bd: script(t, "bd", `echo "bad login s3cret" >&2; exit 1`)}
	err := h.InitBeads(context.Background(), t.TempDir(), "luke", 3308, "beads", "s3cret")
	if err == nil || strings.Contains(err.Error(), "s3cret") || !strings.Contains(err.Error(), "<password>") {
		t.Errorf("InitBeads = %v, want the password struck out", err)
	}
}
