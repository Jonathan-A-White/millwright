package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// heldLand writes a program that stands for mw: it notes its arguments, runs
// until the test releases it by making $0.go (a minute at most), then notes
// that it finished and exits 0.
func heldLand(t *testing.T) string {
	t.Helper()
	prog := filepath.Join(t.TempDir(), "mw")
	body := "#!/bin/sh\necho \"$@\" >> \"$0.args\"\ni=0\nwhile [ ! -e \"$0.go\" ] && [ $i -lt 3000 ]; do sleep 0.02; i=$((i + 1)); done\necho landed >> \"$0.done\"\nexit 0\n"
	if err := os.WriteFile(prog, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return prog
}

// waitFor polls until the file at path exists, failing the test after 10 s.
func waitFor(t *testing.T, path string) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(path); err == nil {
			return
		}
	}
	t.Fatalf("%s never appeared", path)
}

func lines(t *testing.T, path string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return strings.Count(string(raw), "\n")
}

// mw-gq6.470: the follower restarts after every landing of the factory rig,
// and a batch-land in flight was killed with it, so a lampas batch whose gate
// outran the gap between two millwright landings never landed. A land begun by
// one follower outlives that follower stopping, and the follower that comes
// back waits for that same land rather than beginning another.
func TestABatchLandInFlightOutlivesTheFollowerStoppingAndIsWaitedForOnce(t *testing.T) {
	self := heldLand(t)
	land := testBatchLandRun(t, self)

	first, stop := context.WithCancel(context.Background())
	ended := make(chan error, 1)
	go func() { ended <- land(first, "lampas", false) }()
	waitFor(t, self+".args")
	stop() // the follower stopping, for a landing of the factory rig
	select {
	case <-ended:
	case <-time.After(10 * time.Second):
		t.Fatal("the stopped follower's wait did not end with its context")
	}

	// The follower that comes back runs the pass again.
	again := make(chan error, 1)
	go func() { again <- land(context.Background(), "lampas", false) }()
	if err := os.WriteFile(self+".go", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-again:
		if err != nil {
			t.Fatalf("the land finished well, but the follower that came back was told %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the follower that came back never heard the land end")
	}
	if got := lines(t, self+".args"); got != 1 {
		t.Errorf("expected mw land lampas begun once across the restart, got %d", got)
	}
	if got := lines(t, self+".done"); got != 1 {
		t.Errorf("expected the land begun before the restart to finish, got %d finished (it was killed with the follower)", got)
	}
}

func testBatchLandRun(t *testing.T, self string) func(context.Context, string, bool) error {
	t.Helper()
	return batchLandApart(self, t.TempDir(), noScope(t)).Land
}

// noScope is a systemd-run that is not one: it runs the rest of its arguments,
// so that a test needs no user manager.
func noScope(t *testing.T) string {
	t.Helper()
	prog := filepath.Join(t.TempDir(), "systemd-run")
	body := "#!/bin/sh\nwhile [ \"$1\" != sh ]; do shift; done\nexec \"$@\"\n"
	if err := os.WriteFile(prog, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return prog
}
