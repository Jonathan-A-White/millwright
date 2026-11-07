// Package gittest keeps the host's own git config out of the tests that run
// git. A host carries a global and a system git config (url insteadOf
// rewrites, a user, a hooks path, signing), and a test that ran git under it
// would get that host's verdict instead of its own (mw-gq6.1017, mw-gq6.1025).
//
// A test package calls Isolate from its TestMain, which covers every git the
// binary runs, the adapters under test as well as the fixtures; a fixture that
// builds its own command uses Command or Env. Standard library only, and it
// is for _test.go files: nothing that ships imports it.
package gittest

import (
	"context"
	"os"
	"os/exec"
)

// Env is the process environment with the host's git config shut out: no
// global file (GIT_CONFIG_GLOBAL=/dev/null, which also turns off the
// ~/.config/git one) and no system file (GIT_CONFIG_NOSYSTEM=1). A later entry
// wins, so these settings stand even where the shell set its own.
func Env() []string {
	return append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
}

// Isolate shuts the host's git config out of the whole test binary. Call it
// first in TestMain.
func Isolate() {
	os.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

// Command is exec.Command with Env: the one way a fixture builds a git (or
// any other) command that must not read the host's git config.
func Command(program string, args ...string) *exec.Cmd {
	cmd := exec.Command(program, args...)
	cmd.Env = Env()
	return cmd
}

// CommandContext is Command with a context, as exec.CommandContext is.
func CommandContext(ctx context.Context, program string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Env = Env()
	return cmd
}
