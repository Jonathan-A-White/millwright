package shellcmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRunKeepsWhatTheCommandPrintedOutAndErrTogether(t *testing.T) {
	dir := t.TempDir()
	out, err := New(0).Run(context.Background(), dir, "pwd; echo to-err >&2; printf 'a\\nb\\n'")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.HasPrefix(out, dir+"\n") || !strings.Contains(out, "to-err\n") || !strings.HasSuffix(out, "a\nb\n") {
		t.Errorf("printed %q", out)
	}
	out, err = New(0).Run(context.Background(), dir, "echo partial; exit 3")
	if err == nil || !strings.Contains(err.Error(), "exit status 3") || out != "partial\n" {
		t.Errorf("a failing command: %q, %v", out, err)
	}
}

func TestRunGivesUpAfterTheTimeoutAndSaysSo(t *testing.T) {
	start := time.Now()
	out, err := New(200*time.Millisecond).Run(context.Background(), t.TempDir(), "echo before; sleep 30; echo after")
	if err == nil || !strings.Contains(err.Error(), "gave up after 200ms") {
		t.Errorf("error %v, want one saying it gave up", err)
	}
	if out != "before\n" {
		t.Errorf("printed %q before giving up", out)
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("giving up took %s", took)
	}
}

func TestRunStopsWhatTheCommandStartedWhenItGivesUp(t *testing.T) {
	// A sleep length no other process has: the marker pgrep looks for.
	marker := fmt.Sprintf("sleep 40%d", os.Getpid())
	t.Cleanup(func() { _ = exec.Command("pkill", "-f", marker).Run() })
	_, err := New(time.Second).Run(context.Background(), t.TempDir(), `sh -c "`+marker+` & wait"`)
	if err == nil || !strings.Contains(err.Error(), "gave up after 1s") {
		t.Fatalf("error %v, want one saying it gave up", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if exec.Command("pgrep", "-f", marker).Run() != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%q still runs after the command gave up", marker)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
