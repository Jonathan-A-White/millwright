package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// [verify] realtime = false gives neither route a hook: the --apply pass and the
// follower's start no match (mw-4xdnis.6); the default gives both one.
func TestVerifyHookHonoursRealtimeOnBothRoutes(t *testing.T) {
	for _, tc := range []struct {
		name, table string
		hook        bool
	}{
		{"left out", "", true},
		{"on", "[verify]\nrealtime = true\n", true},
		{"off", "[verify]\nrealtime = false\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			writeConfig(t, home, tc.table)
			for _, who := range []string{"", "mw postern inbox --apply"} {
				out := &bytes.Buffer{}
				if got := verifyHook(out, who) != nil; got != tc.hook {
					t.Errorf("who %q: hook present = %v, want %v (said %q)", who, got, tc.hook, out.String())
				}
			}
		})
	}
}

// A [verify] table that cannot be read gives no hook and says so under the name
// of the command that asked.
func TestVerifyHookSaysWhoFoundTheTableUnreadable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeConfig(t, home, "[verify]\nrealtime = maybe\n")
	out := &bytes.Buffer{}
	if verifyHook(out, "mw postern inbox --apply") != nil || !strings.HasPrefix(out.String(), "mw postern inbox --apply: no real-time verify match: ") {
		t.Errorf("expected no hook and the apply route named, got %q", out.String())
	}
}

func writeConfig(t *testing.T, home, text string) {
	t.Helper()
	path := filepath.Join(home, config.File)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}
