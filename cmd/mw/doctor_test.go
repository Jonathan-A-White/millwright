package main

import (
	"bytes"
	"fmt"
	"net"
	"strings"
	"testing"
)

// closedDoctorPort is a host:port nothing listens on, refusing every
// connection at once rather than timing out: standing in for a dead
// internet without a real network call.
func closedDoctorPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a closed port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

// mw doctor's table is wired here, and nothing else says what order it
// runs in: this is the one place that order can regress unnoticed.
// Everything in this scenario reads cannot-tell — no real systemctl, netsh
// or ssh is reached for real — which is enough to see the table order
// without touching this host's own units or the network.
func TestDoctorTableRunsWifiBeforeTunnel(t *testing.T) {
	closed := closedDoctorPort(t)
	vault := t.TempDir()
	mwConfig(t, fmt.Sprintf("vault = %q\nhost = \"laptop\"\n\n[doctor]\nreach = [%q]\n", vault, closed))

	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"doctor", "--dry-run"})
	_ = root.Execute()

	report := out.String()
	wifiAt := strings.Index(report, "wifi:")
	tunnelAt := strings.Index(report, "tunnel:")
	if wifiAt < 0 || tunnelAt < 0 {
		t.Fatalf("expected both wifi and tunnel in the report, got:\n%s", report)
	}
	if wifiAt > tunnelAt {
		t.Fatalf("expected wifi to run before tunnel, got:\n%s", report)
	}
}
