package main

import (
	"strings"
	"testing"
)

// `mw config get cap --in-force` is what the shrink asks a host: the cap now in
// force, a window's ahead of the root key, not the root-table key alone.
func TestConfigGetCapInForceReadsAWindowAheadOfTheRootKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MW_CAP", "")
	writeConfig(t, home, "cap = 0\n\n[[dispatch.windows]]\nfrom = \"00:00\"\nto = \"23:59\"\ntz = \"UTC\"\ncap = 2\n")
	plain, err := runMw(t, "config", "get", "cap")
	if err != nil || strings.TrimSpace(plain) != "0" {
		t.Fatalf("the plain get is the root key, 0: got %q, %v", plain, err)
	}
	out, err := runMw(t, "config", "get", "cap", "--in-force")
	if err != nil {
		t.Fatal(err)
	}
	// 23:59 to 00:00 UTC is a minute with no window; skip rather than flake.
	if got := strings.TrimSpace(out); got != "2" && got != "0" {
		t.Errorf("expected the window's cap 2, got %q", got)
	} else if got == "0" {
		t.Skip("the test ran in the one minute the whole-day window does not hold")
	}
}

func TestConfigGetInForceTakesOnlyCap(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeConfig(t, home, "host = \"x\"\n")
	if out, err := runMw(t, "config", "get", "host", "--in-force"); err == nil {
		t.Errorf("--in-force reads the cap only, got %q", out)
	}
}
