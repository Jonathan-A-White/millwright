package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"
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

// A host whose beads live in another host's database has its link to it in
// the table: beads-server dials BEADS_DOLT_SERVER_HOST on its port, and a
// database that does not answer is faulty. Every other host reads it ok.
func TestDoctorTableHasTheBeadsServerCheck(t *testing.T) {
	closed := closedDoctorPort(t)
	host, port, err := net.SplitHostPort(closed)
	if err != nil {
		t.Fatal(err)
	}
	vault := t.TempDir()
	mwConfig(t, fmt.Sprintf("vault = %q\nhost = \"laptop\"\nbeads_sync = \"shared\"\n\n[doctor]\nreach = [%q]\n", vault, closed))
	t.Setenv("MW_BEADS_SYNC", "")
	t.Setenv("BEADS_DOLT_SERVER_HOST", host)
	t.Setenv("BEADS_DOLT_SERVER_PORT", port)

	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"doctor", "--dry-run", "beads-server"})
	_ = root.Execute()

	if report := out.String(); !strings.Contains(report, "beads-server") || !strings.Contains(report, closed) {
		t.Fatalf("expected beads-server to name the database that does not answer, got:\n%s", report)
	}
}

// The home dials its own dolt-beads server: a home (beads_sync backup) whose
// server is stopped is faulty, and the dry run says it is this host's own.
func TestDoctorBeadsServerIsFaultyOnAHomeWhoseServerIsStopped(t *testing.T) {
	closed := closedDoctorPort(t)
	host, port, err := net.SplitHostPort(closed)
	if err != nil {
		t.Fatal(err)
	}
	vault := t.TempDir()
	mwConfig(t, fmt.Sprintf("vault = %q\nhost = \"desktop\"\nbeads_sync = \"backup\"\n\n[doctor]\nreach = [%q]\n", vault, closed))
	t.Setenv("MW_BEADS_SYNC", "")
	t.Setenv("BEADS_DOLT_SERVER_HOST", host)
	t.Setenv("BEADS_DOLT_SERVER_PORT", port)

	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"doctor", "--dry-run", "beads-server"})
	_ = root.Execute()

	report := out.String()
	for _, want := range []string{"beads-server", closed, "dolt-beads", "this host"} {
		if !strings.Contains(report, want) {
			t.Fatalf("expected the report to name %q, got:\n%s", want, report)
		}
	}
}

// The beads-stores check is in the table: a vault holding both .beads/dolt and
// .beads/embeddeddolt is faulty, and the dry run names the stray store.
func TestDoctorTableHasTheBeadsStoresCheck(t *testing.T) {
	vault := t.TempDir()
	for _, store := range []string{"dolt", "embeddeddolt"} {
		if err := os.MkdirAll(filepath.Join(vault, ".beads", store), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mwConfig(t, fmt.Sprintf("vault = %q\nhost = \"laptop\"\n", vault))

	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"doctor", "--dry-run", "beads-stores"})
	_ = root.Execute()

	if report := out.String(); !strings.Contains(report, "beads-stores") || !strings.Contains(report, "embeddeddolt") {
		t.Fatalf("expected beads-stores to name the stray store, got:\n%s", report)
	}
}

// The doctor's emergency copy of an alarm cuts a long pane to what an
// emergency event may carry, and a refused or failed write is said, not
// discarded.
func TestEmitDoctorEmergencyCutsAPaneAndSaysARefusal(t *testing.T) {
	log := &apptest.FakeEventLog{}
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	pane := strings.Repeat(strings.Repeat("x", 200)+"\n", 12)
	var said bytes.Buffer
	emitDoctorEmergency(context.Background(), log, func() time.Time { return at }, "laptop", pane, &said)
	got, _ := log.Since(context.Background(), 0)
	if len(got) != 1 || got[0].Lane != events.LaneEmergency || got[0].Validate() != nil {
		t.Fatalf("log holds %+v, want one valid emergency event", got)
	}
	if said.Len() != 0 {
		t.Fatalf("a good emit said %q", said.String())
	}

	// A host that makes the actor unwritable by the log stands in for a refusal.
	emitDoctorEmergency(context.Background(), &failingEventLog{}, func() time.Time { return at }, "laptop", "text", &said)
	if !strings.Contains(said.String(), "emergency event: not written") || !strings.Contains(said.String(), "disk full") {
		t.Fatalf("a failed emit said %q, want it logged", said.String())
	}
}

type failingEventLog struct{ apptest.FakeEventLog }

func (*failingEventLog) Append(context.Context, []events.Event) (uint64, error) {
	return 0, errors.New("disk full")
}

// The home's battery is in the table: a host with no battery reads ok, naming
// that there is none, so this runs the same on the VPS.
func TestDoctorTableHasTheBatteryCheck(t *testing.T) {
	closed := closedDoctorPort(t)
	vault := t.TempDir()
	mwConfig(t, fmt.Sprintf("vault = %q\nhost = \"laptop\"\n\n[doctor]\nreach = [%q]\n", vault, closed))

	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"doctor", "--dry-run", "battery"})
	if err := root.Execute(); err != nil {
		t.Fatalf("mw doctor battery: %v\n%s", err, out)
	}
	if report := out.String(); !strings.HasPrefix(report, "battery: ok") {
		t.Fatalf("expected the battery check to run and read ok, got:\n%s", report)
	}
}
