package shellcmd

import (
	"context"
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
