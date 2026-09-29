package main

import (
	"os"
	"strings"
	"testing"
)

// TestMain clears bd's Dolt server settings before any test runs. A session on
// a host whose bd runs in server mode carries them (beads.env), and the real
// `bd init` that mw init's tests run would then make its throwaway database on
// the host's shared server instead of in the temp directory. A test that wants
// one of them sets it with t.Setenv.
func TestMain(m *testing.M) {
	// A test that runs on a host where a service started this session must not
	// find mw postern inbox --apply handing its pass to a real systemd-run.
	if selfPath := os.Getenv("MW_TEST_HANDOFF"); selfPath != "" {
		handOffAsTheServiceStartedIt(selfPath)
	}
	os.Unsetenv("INVOCATION_ID")
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "BEADS_DOLT_") {
			os.Unsetenv(name)
		}
	}
	os.Exit(m.Run())
}
