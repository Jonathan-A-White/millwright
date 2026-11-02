package tmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every tmux (and ps) command this package starts goes through runBounded, so
// every one has a deadline (mw-gq6.884). A second exec.Command in a file here
// would be a call a stuck server could hold for as long as it liked.
func TestEveryTmuxCommandIsStartedUnderTheDeadline(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if strings.Contains(line, "exec.Command(") || (strings.Contains(line, "exec.CommandContext(") && file != "tmux.go") {
				t.Errorf("%s:%d starts a process outside runBounded: %s", file, i+1, strings.TrimSpace(line))
			}
		}
	}
	src, _ := os.ReadFile("tmux.go")
	if n := strings.Count(string(src), "exec.CommandContext("); n != 1 {
		t.Errorf("tmux.go starts %d processes with exec.CommandContext, want the one in runBounded", n)
	}
}
