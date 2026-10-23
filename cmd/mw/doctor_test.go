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

	"github.com/Jonathan-A-White/millwright/application"
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
	seq, _ := emitDoctorEmergency(context.Background(), log, func() time.Time { return at }, "laptop", pane, &said)
	got, _ := log.Since(context.Background(), 0)
	if len(got) != 1 || got[0].Lane != events.LaneEmergency || got[0].Validate() != nil {
		t.Fatalf("log holds %+v, want one valid emergency event", got)
	}
	if seq != got[0].Seq || seq == 0 {
		t.Fatalf("emit returned seq %d, want the event's %d", seq, got[0].Seq)
	}
	if said.Len() != 0 {
		t.Fatalf("a good emit said %q", said.String())
	}

	// A host that makes the actor unwritable by the log stands in for a refusal.
	if seq, _ := emitDoctorEmergency(context.Background(), &failingEventLog{}, func() time.Time { return at }, "laptop", "text", &said); seq != 0 {
		t.Fatalf("a failed emit returned seq %d, want 0", seq)
	}
	if !strings.Contains(said.String(), "emergency event: not written") || !strings.Contains(said.String(), "disk full") {
		t.Fatalf("a failed emit said %q, want it logged", said.String())
	}
}

// The doctor's way back is one event in the normal lane, a job going running to
// done, that names the emergency it ends in `clears`: not a second emergency.
func TestEmitDoctorClearIsANormalLaneEventNamingTheEmergency(t *testing.T) {
	log := &apptest.FakeEventLog{}
	at := time.Date(2026, 10, 3, 12, 17, 0, 0, time.UTC)
	var said bytes.Buffer
	emergency, _ := emitDoctorEmergency(context.Background(), log, func() time.Time { return at }, "laptop", "down", &said)
	if err := emitDoctorClear(context.Background(), log, func() time.Time { return at }, "laptop", "answers again", emergency, &said); err != nil {
		t.Fatal(err)
	}
	got, _ := log.Since(context.Background(), 0)
	if len(got) != 2 {
		t.Fatalf("log holds %+v, want the emergency and its clear", got)
	}
	clear := got[1]
	if clear.Lane != events.LaneNormal || clear.Clears != emergency || clear.Detail != "answers again" || clear.Actor != "doctor@laptop" || clear.Validate() != nil {
		t.Fatalf("clear is %+v, want a valid normal-lane event clearing %d", clear, emergency)
	}
	if clear.From == events.JobRunning && clear.To == events.JobFailed {
		t.Fatalf("clear is running->failed, which reads as a fault")
	}
	if said.Len() != 0 {
		t.Fatalf("a good emit said %q", said.String())
	}

	if err := emitDoctorClear(context.Background(), &failingEventLog{}, func() time.Time { return at }, "laptop", "x", 0, &said); err == nil || !strings.Contains(said.String(), "clearing event: not written") {
		t.Fatalf("a failed clear returned %v and said %q, want it said", err, said.String())
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

// A Mayor stuck on API errors is in the table, ahead of mayor-stale: on a host
// whose vault holds no .mayor-acting there is no seat held, which reads ok.
func TestDoctorTableHasTheMayorStuckCheck(t *testing.T) {
	closed := closedDoctorPort(t)
	vault := t.TempDir()
	mwConfig(t, fmt.Sprintf("vault = %q\nhost = \"laptop\"\n\n[doctor]\nreach = [%q]\n", vault, closed))

	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"doctor", "--dry-run", "mayor-stuck"})
	if err := root.Execute(); err != nil {
		t.Fatalf("mw doctor mayor-stuck: %v\n%s", err, out)
	}
	if report := out.String(); !strings.HasPrefix(report, "mayor-stuck: ok") {
		t.Fatalf("expected the mayor-stuck check to run and read ok, got:\n%s", report)
	}
}

// fakePostern writes a stand-in postern that records its arguments beside
// itself, then prints stdout and stderr and leaves with code.
func fakePostern(t *testing.T, stdout, stderr string, code int) (bin, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	outFile := filepath.Join(dir, "stdout")
	errFile := filepath.Join(dir, "stderr")
	for file, text := range map[string]string{outFile: stdout, errFile: stderr} {
		if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	bin = filepath.Join(dir, "postern")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" > '" + argsFile + "'\ncat '" + outFile + "'\ncat '" + errFile + "' >&2\nexit " + fmt.Sprint(code) + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin, argsFile
}

func runDoctorLicencesWith(t *testing.T, posternBin string, extra ...string) (string, error) {
	t.Helper()
	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"doctor", "licences", "--postern", posternBin}, extra...))
	err := root.Execute()
	return out.String(), err
}

const cleanRightsJSON = `{"collections":[
 {"collection":"postern","kind":"cockpit","routes":["GET /api/view"]},
 {"collection":"cairn","kind":"app","app":"cairn","routes":["POST /api/blobs"]}
]}`

func TestDoctorLicencesReadsPosternRightsAndLeavesCleanOnAGoodMap(t *testing.T) {
	bin, argsFile := fakePostern(t, cleanRightsJSON, "", 0)
	envFile := filepath.Join(t.TempDir(), "env")
	report, err := runDoctorLicencesWith(t, bin, "--env-file", envFile)
	if err != nil {
		t.Fatalf("expected a clean map to leave ok, got %v\n%s", err, report)
	}
	for _, want := range []string{"postern -> cockpit: a key holding it opens GET /api/view", "cairn -> app cairn:", "ok: every licence collection opens what it should"} {
		if !strings.Contains(report, want) {
			t.Errorf("expected %q in the report, got:\n%s", want, report)
		}
	}
	args, _ := os.ReadFile(argsFile)
	if got := strings.TrimSpace(string(args)); got != "rights --json --env-file "+envFile {
		t.Errorf("expected postern to be asked for its rights as JSON from the env file, got %q", got)
	}
}

func TestDoctorLicencesLeavesFaultyOnAWarning(t *testing.T) {
	bin, _ := fakePostern(t, `{"collections":[
 {"collection":"postern","kind":"cockpit","routes":["GET /api/view"]},
 {"collection":"spell","kind":"app","app":"spellforge","routes":["GET /api/me"]},
 {"collection":"spell","kind":"app","app":"spellforge","routes":["GET /api/me"]}
]}`, "", 0)
	report, err := runDoctorLicencesWith(t, bin)
	if _, fault := application.DoctorFaults(err); !fault {
		t.Fatalf("expected a doctor fault, got %v\n%s", err, report)
	}
	if !strings.Contains(report, "WARNING: spell is a cockpit collection") {
		t.Errorf("expected the warning, got:\n%s", report)
	}
}

func TestDoctorLicencesSaysPlainlyWhenPosternIsMissing(t *testing.T) {
	report, err := runDoctorLicencesWith(t, filepath.Join(t.TempDir(), "no-such-postern"))
	if err == nil {
		t.Fatalf("expected a failure, got none\n%s", report)
	}
	if _, fault := application.DoctorFaults(err); fault {
		t.Errorf("a missing postern is a failure, not a warning: %v", err)
	}
	if !strings.Contains(err.Error(), "postern") || strings.Contains(err.Error(), "\n") || strings.Contains(report, "->") {
		t.Errorf("expected one plain line and no report, got %q and %q", err, report)
	}
}

func TestDoctorLicencesSaysPlainlyWhenPosternHasNoRightsSubcommand(t *testing.T) {
	// An older postern does not know rights; it prints something that is not
	// the map. Nothing else stands in for it.
	for name, run := range map[string]struct {
		stdout, stderr string
		code           int
	}{
		"output that is not the map": {stdout: "postern server listening on 127.0.0.1:8787\n", code: 0},
		"a refusal":                  {stderr: "loading config: POSTERN_ANCHOR is required\n", code: 1},
	} {
		bin, _ := fakePostern(t, run.stdout, run.stderr, run.code)
		report, err := runDoctorLicencesWith(t, bin)
		if err == nil {
			t.Fatalf("%s: expected a failure\n%s", name, report)
		}
		if _, fault := application.DoctorFaults(err); fault {
			t.Errorf("%s: expected a failure, not a warning: %v", name, err)
		}
		if !strings.Contains(err.Error(), "postern rights --json") || strings.Contains(err.Error(), "\n") {
			t.Errorf("%s: expected one line naming the command, got %q", name, err)
		}
		if strings.Contains(report, "->") || strings.Contains(report, "ok:") {
			t.Errorf("%s: expected no report from a failed read, got %q", name, report)
		}
	}
}

func TestDoctorLicencesKeepsPosternsRefusalInItsLine(t *testing.T) {
	bin, _ := fakePostern(t, "", "postern rights: loading config: POSTERN_APPS: \"spell\" is one of POSTERN_COLLECTIONS\n", 1)
	_, err := runDoctorLicencesWith(t, bin)
	if err == nil || !strings.Contains(err.Error(), `"spell" is one of POSTERN_COLLECTIONS`) {
		t.Fatalf("expected postern's own words in the failure, got %v", err)
	}
}

// A push that failed after the emergency event was written must not make the
// check write a second emergency at its next pass, leaving the first without
// a clear: the seq is kept and the failure only said.
func TestAlarmSeqKeepsTheEmergencyWhenOnlyThePushFailed(t *testing.T) {
	var said bytes.Buffer
	seq, err := alarmSeq(7, errors.New("backend down"), &said)
	if seq != 7 || err != nil || !strings.Contains(said.String(), "backend down") {
		t.Errorf("got seq %d, err %v, said %q: want 7, no error, the push failure said", seq, err, said.String())
	}
	if _, err := alarmSeq(0, errors.New("backend down"), &said); err == nil {
		t.Error("with no emergency event written the push failure is still the error, to be tried again")
	}
	if seq, err := alarmSeq(7, nil, &said); seq != 7 || err != nil {
		t.Errorf("a push that worked: got %d, %v", seq, err)
	}
}
