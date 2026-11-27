package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-gq6.1546: the commands a timer runs built their Gateway with mwGateway and
// never kept a beads connection, so each tracker read in a run started its own
// bd, and each bd costs the server several new connections (the beads-churn of
// mw-gq6.1513 and mw-gq6.1514). Each reads over one connection kept open for
// the run, as mw next and mw millhand tick do.

// aTimerRun runs mw with args in a vault and home of its own, against a bd that
// records what it is asked and a server to reach, and returns what it said.
// home is the host the vault names as home, and toml is what follows the vault and host lines of the config.
func aTimerRun(t *testing.T, home, toml string, args ...string) (said string, err error, asked string, server *aStandInBeadsServer) {
	t.Helper()
	asked = aBdThatRecords(t)
	server = aServerToReach(t)
	vaultDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(vaultDir, "home"), []byte(home+" 2026-01-01T00:00:00Z mw\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mwConfig(t, "vault = \""+vaultDir+"\"\nhost = \"laptop\"\n"+toml)
	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), err, asked, server
}

// startsNoBd fails the test when the run started a bd to read, and when the
// server it could reach was asked nothing.
func startsNoBd(t *testing.T, said string, asked string, server *aStandInBeadsServer) {
	t.Helper()
	if got := bdsAsked(t, asked); got != 0 {
		t.Errorf("expected no bd started while the kept connection answers, got %d:\n%s\nthe run said:\n%s", got, readAsked(t, asked), said)
	}
	if got := server.queries(""); got == 0 {
		t.Errorf("expected the reads to go over the kept connection, the server was asked nothing; the run said:\n%s", said)
	}
}

func aRigWithAStoryBranch(t *testing.T) string {
	t.Helper()
	rig := filepath.Join(t.TempDir(), "rig")
	if err := os.MkdirAll(rig, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "first"},
		{"branch", "mw/e-1.1"},
	} {
		cmd := exec.Command("git", append([]string{"-C", rig}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return rig
}

func TestKeptHostSweepTimerReadsItsStoriesOverTheKeptConnection(t *testing.T) {
	rig := aRigWithAStoryBranch(t)
	said, err, asked, server := aTimerRun(t, "laptop", "[rigs]\nr = \""+rig+"\"\n", "host", "sweep", "--timer")
	if err != nil {
		t.Fatalf("expected the sweep to complete, got %v\n%s", err, said)
	}
	startsNoBd(t, said, asked, server)
}

func TestKeptNeedsSweepReadsOverTheKeptConnection(t *testing.T) {
	said, err, asked, server := aTimerRun(t, "laptop", "", "needs", "sweep")
	if err != nil {
		t.Fatalf("expected the sweep to complete, got %v\n%s", err, said)
	}
	startsNoBd(t, said, asked, server)
}

func TestKeptVerifyMatchReadsOverTheKeptConnection(t *testing.T) {
	said, err, asked, server := aTimerRun(t, "laptop", "", "verify", "match", "--dry-run")
	if err != nil {
		t.Fatalf("expected the match to complete, got %v\n%s", err, said)
	}
	startsNoBd(t, said, asked, server)
}

func TestKeptReportSendReadsOverTheKeptConnection(t *testing.T) {
	said, err, asked, server := aTimerRun(t, "laptop", "", "report", "send", "--dry-run")
	if err != nil {
		t.Fatalf("expected the report to be made, got %v\n%s", err, said)
	}
	startsNoBd(t, said, asked, server)
}

// aUsageScreen is the /usage screen a probe terminal shows. Its resets are two
// days after the clock, built from it, so they stay in the future whatever day
// the test runs (mw-gq6.1774).
func aUsageScreen(now time.Time) string {
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		zone = time.UTC
	}
	reset := now.In(zone).AddDate(0, 0, 2).Format("Jan 2, 3:04pm")
	return `
  Settings:  Status   Config   Usage   Stats

  Current week (all models)
  ██████                                             13% used
  Resets ` + reset + ` (America/New_York)

  Current week (Fable)
  █████████████████████████                          50% used
  Resets ` + reset + ` (America/New_York)
`
}

type aUsageProbe struct{}

func (aUsageProbe) Open(context.Context, string, string, int, int) error { return nil }
func (aUsageProbe) Type(context.Context, string, string) error           { return nil }
func (aUsageProbe) Press(context.Context, string, string) error          { return nil }
func (aUsageProbe) Capture(context.Context, string) (string, error) {
	return aUsageScreen(time.Now()), nil
}
func (aUsageProbe) Close(context.Context, string) error { return nil }
func (aUsageProbe) Probes(context.Context) ([]application.UsageProbe, error) {
	return nil, nil
}

func TestKeptUsageReadSetsTheUsageOverTheKeptConnection(t *testing.T) {
	was, wasWait := usageReadTerminal, usageReadWait
	usageReadTerminal = func() application.UsageTerminal { return aUsageProbe{} }
	usageReadWait = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { usageReadTerminal, usageReadWait = was, wasWait })

	said, err, asked, server := aTimerRun(t, "laptop", "", "usage", "read")
	if err != nil {
		t.Fatalf("expected the read to complete, got %v\n%s", err, said)
	}
	for _, line := range strings.Split(strings.TrimSpace(readAsked(t, asked)), "\n") {
		if line != "" && !strings.Contains(line, " kv set ") {
			t.Errorf("expected no bd but a write of the reading, got %q", line)
		}
	}
	if got := server.queries(""); got == 0 {
		t.Errorf("expected the reads to go over the kept connection, the server was asked nothing; the run said:\n%s", said)
	}
}

// mw postern mirror reads no tracker: it copies the backend's files, and on a
// host that is not home it does nothing. It starts no bd, kept connection or
// not; this keeps it so if a tracker read is ever added to it.
func TestKeptPosternMirrorStartsNoBd(t *testing.T) {
	said, err, asked, _ := aTimerRun(t, "desktop", "postern_data = \""+t.TempDir()+"\"\n[hands_hosts]\ndesktop = \"ssh desktop\"\n", "postern", "mirror")
	if err != nil {
		t.Fatalf("expected the mirror to complete, got %v\n%s", err, said)
	}
	if got := bdsAsked(t, asked); got != 0 {
		t.Errorf("expected no bd started, got %d:\n%s", got, readAsked(t, asked))
	}
}
