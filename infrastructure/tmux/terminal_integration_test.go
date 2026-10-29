package tmux_test

// This test drives a real tmux on a server of its own, as tmux_integration_test.go
// does, and never touches tmux's default server.

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/tmux"
)

func TestATerminalIsTypedIntoReadAndClosed(t *testing.T) {
	if !tmux.Available() {
		t.Skipf("%s is not on PATH", tmux.Program)
	}
	socket := fmt.Sprintf("mw-test-%d-%d", os.Getpid(), socketCount.Add(1))
	t.Cleanup(func() {
		_ = exec.Command(tmux.Program, "-L", socket, "kill-server").Run()
		_ = os.Remove(socketPath(socket))
	})
	term := tmux.NewTerminal(tmux.WithSocket(socket))
	ctx := t.Context()
	name := "mw-usage-probe-test"
	if err := term.Open(ctx, name, t.TempDir(), 120, 40); err != nil {
		t.Fatal(err)
	}
	if err := term.Type(ctx, name, "echo usage-probe-$((6*7))"); err != nil {
		t.Fatal(err)
	}
	if err := term.Press(ctx, name, "Enter"); err != nil {
		t.Fatal(err)
	}
	var screen string
	for range 50 {
		var err error
		if screen, err = term.Capture(ctx, name); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(screen, "usage-probe-42") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(screen, "usage-probe-42") {
		t.Fatalf("the typed command's output is not on the screen:\n%s", screen)
	}
	if err := term.Close(ctx, name); err != nil {
		t.Fatal(err)
	}
	if err := term.Close(ctx, name); err != nil {
		t.Fatalf("closing a terminal already gone: %v", err)
	}
}
