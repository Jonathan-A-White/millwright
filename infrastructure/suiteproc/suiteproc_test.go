package suiteproc_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/suiteproc"
)

// standIn writes a program that stands for mw: it sleeps, then exits with code.
func standIn(t *testing.T, delay string, code int) string {
	t.Helper()
	prog := filepath.Join(t.TempDir(), "mw")
	body := "#!/bin/sh\necho \"$@\" >> \"$0.args\"\nsleep " + delay + "\nexit " + string(rune('0'+code)) + "\n"
	if err := os.WriteFile(prog, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return prog
}

func TestASuiteOutlivesTheContextItWasBegunUnderAndIsWaitedForAnew(t *testing.T) {
	prog := standIn(t, "0.5", 0)
	p := &suiteproc.Processes{Dir: t.TempDir(), Program: prog, Scope: noScope(t), Poll: 20 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	if err := p.Begin(ctx, "lampas"); err != nil {
		t.Fatal(err)
	}
	waited, stop := context.WithTimeout(ctx, 100*time.Millisecond)
	defer stop()
	if err := p.Wait(waited, "lampas"); err == nil {
		t.Fatal("expected the wait to be cut short by its context")
	}
	cancel() // the follower stopping

	// A follower that comes back begins the suite again: it finds this one.
	if err := p.Begin(context.Background(), "lampas"); err != nil {
		t.Fatal(err)
	}
	if err := p.Wait(context.Background(), "lampas"); err != nil {
		t.Fatalf("the adopted suite passed, but Wait said: %v", err)
	}
	args, _ := os.ReadFile(prog + ".args")
	if got := strings.Count(string(args), "suite lampas --file"); got != 1 {
		t.Errorf("expected the suite run once, got %d (%q)", got, args)
	}
}

func TestASuiteThatFailedIsToldAsFailed(t *testing.T) {
	p := &suiteproc.Processes{Dir: t.TempDir(), Program: standIn(t, "0.1", 1), Scope: noScope(t), Poll: 20 * time.Millisecond}
	if err := p.Begin(context.Background(), "lampas"); err != nil {
		t.Fatal(err)
	}
	err := p.Wait(context.Background(), "lampas")
	if err == nil || !strings.Contains(err.Error(), "exit status 1") {
		t.Fatalf("expected the failure with its exit status, got %v", err)
	}
}

func TestAFinishedSuiteIsRunAgainByTheNextBegin(t *testing.T) {
	prog := standIn(t, "0.05", 0)
	p := &suiteproc.Processes{Dir: t.TempDir(), Program: prog, Scope: noScope(t), Poll: 20 * time.Millisecond}
	for i := 0; i < 2; i++ {
		if err := p.Begin(context.Background(), "lampas"); err != nil {
			t.Fatal(err)
		}
		if err := p.Wait(context.Background(), "lampas"); err != nil {
			t.Fatal(err)
		}
	}
	args, _ := os.ReadFile(prog + ".args")
	if got := strings.Count(string(args), "suite lampas --file"); got != 2 {
		t.Errorf("expected two runs, got %d", got)
	}
}

// noScope is a systemd-run that is not one: it runs the rest of its arguments,
// so that a test needs no user manager.
func noScope(t *testing.T) string {
	t.Helper()
	prog := filepath.Join(t.TempDir(), "systemd-run")
	body := "#!/bin/sh\nwhile [ \"$1\" != sh ]; do shift; done\nexec \"$@\"\n"
	if err := os.WriteFile(prog, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return prog
}
