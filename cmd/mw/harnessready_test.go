package main

import (
	"os"
	"strings"
	"testing"
)

// mw-gq6.1590 (folds mw-gq6.1359, .1360, .1361): a session the commands start
// waits for claude --version, through the one constructor harnessReadyRunner. A
// command that hands a use case a raw tmux.New() starts its sessions without the
// wait. A line that does not reach Runner.Start says so with a //mw:statusonly
// comment.
func TestNoCommandHandsARawTmuxRunnerToASessionStart(t *testing.T) {
	for _, file := range []string{"dispatch.go", "next.go", "retry.go", "land.go"} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if strings.Contains(line, "tmux.New()") && !strings.Contains(line, "//mw:statusonly") {
				t.Errorf("%s:%d hands a raw tmux.New() on: %s\nuse harnessReadyRunner, which waits for claude before a session starts", file, i+1, strings.TrimSpace(line))
			}
		}
		if file != "dispatch.go" && !strings.Contains(string(src), "harnessReadyRunner(") {
			t.Errorf("%s starts no session through harnessReadyRunner", file)
		}
	}
}
