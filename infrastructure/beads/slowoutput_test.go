package beads_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// mw-gq6.1746: a bd that exits 0 but leaves a child holding its output for a
// couple of seconds (a loaded host closing the pipe late) did well, and is not
// reported as failed because the output took longer than a second to close.
func TestABdThatExitsZeroWithItsOutputHeldForTwoSecondsSucceeds(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	dir := t.TempDir()
	program := filepath.Join(dir, "bd-slow-output")
	script := "#!/bin/sh\n( sleep 2 ) &\nexit 0\n"
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	gateway := beads.New(t.TempDir(), beads.WithProgram(program))

	if err := gateway.Sync(context.Background()); err != nil {
		t.Fatalf("a bd that exited 0 was reported as failed: %v", err)
	}
}
