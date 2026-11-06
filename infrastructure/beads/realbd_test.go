package beads_test

import (
	"errors"
	"testing"
)

// mw-gq6.1023: the real-bd cases run the bd a Builder's session names in
// MW_BD_REAL, not the mw bd script ahead of it on PATH.
func TestTheRealBdCasesRunTheBdThatMwBdRuns(t *testing.T) {
	onPath := map[string]string{"bd": "/tmp/tmp.wrap/bd", "/home/jwhite/.local/bin/bd": "/home/jwhite/.local/bin/bd"}
	lookPath := func(name string) (string, error) {
		if found, ok := onPath[name]; ok {
			return found, nil
		}
		return "", errors.New(name + " not found")
	}
	env := func(named string) func(string) string {
		return func(key string) string {
			if key == bdRealEnv {
				return named
			}
			return ""
		}
	}
	for _, c := range []struct {
		named, want string
	}{
		{"/home/jwhite/.local/bin/bd", "/home/jwhite/.local/bin/bd"},
		{"", "/tmp/tmp.wrap/bd"},
	} {
		got, err := realBdFor(env(c.named), lookPath)
		if err != nil || got != c.want {
			t.Errorf("with %s=%q the cases ran %q (%v), want %q", bdRealEnv, c.named, got, err, c.want)
		}
	}
	if _, err := realBdFor(env("/nowhere/bd"), lookPath); err == nil {
		t.Errorf("a %s that names no bd to run must be an error, not the bd on PATH", bdRealEnv)
	}
}
