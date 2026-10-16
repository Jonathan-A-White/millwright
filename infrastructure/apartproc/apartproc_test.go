package apartproc_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/apartproc"
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

// heldStandIn is a standIn that runs until the test releases it by making
// $0.go, a minute at most: so it is still running whatever the host's load
// makes of the test's own steps (mw-gq6.440).
func heldStandIn(t *testing.T) string {
	t.Helper()
	prog := filepath.Join(t.TempDir(), "mw")
	body := "#!/bin/sh\necho \"$@\" >> \"$0.args\"\ni=0\nwhile [ ! -e \"$0.go\" ] && [ $i -lt 3000 ]; do sleep 0.02; i=$((i + 1)); done\nexit 0\n"
	if err := os.WriteFile(prog, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return prog
}

func TestASuiteOutlivesTheContextItWasBegunUnderAndIsWaitedForAnew(t *testing.T) {
	prog := heldStandIn(t)
	p := &apartproc.Processes{Dir: t.TempDir(), Program: prog, Scope: noScope(t), Poll: 20 * time.Millisecond}
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
	if err := os.WriteFile(prog+".go", nil, 0o644); err != nil {
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
	p := &apartproc.Processes{Dir: t.TempDir(), Program: standIn(t, "0.1", 1), Scope: noScope(t), Poll: 20 * time.Millisecond}
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
	p := &apartproc.Processes{Dir: t.TempDir(), Program: prog, Scope: noScope(t), Poll: 20 * time.Millisecond}
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

func TestALandingOutlivesTheFollowerStoppingAndNoSecondIsBegunBesideIt(t *testing.T) {
	prog := heldStandIn(t)
	l := &apartproc.Landings{Dir: t.TempDir(), Program: prog, Scope: noScope(t), Poll: 20 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	if err := l.Begin(ctx, "lampas", false); err != nil {
		t.Fatal(err)
	}
	cancel() // the follower stopping
	if err := l.Wait(ctx, "lampas"); err == nil {
		t.Fatal("expected the stopped follower's wait to end with its context")
	}
	if pending, err := l.Pending(context.Background()); err != nil || len(pending) != 1 || pending[0] != "lampas" {
		t.Fatalf("expected the landing of lampas pending, got %v, %v", pending, err)
	}

	// The follower that comes back, twice over: neither begins another.
	for i := 0; i < 2; i++ {
		if err := l.Begin(context.Background(), "lampas", true); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(prog+".go", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := l.Wait(context.Background(), "lampas"); err != nil {
		t.Fatalf("the adopted landing exited 0, but Wait said: %v", err)
	}
	args, _ := os.ReadFile(prog + ".args")
	if got := strings.Count(string(args), "land lampas"); got != 1 {
		t.Errorf("expected one landing, got %d (%q)", got, args)
	}
	if pending, _ := l.Pending(context.Background()); len(pending) != 0 {
		t.Errorf("expected nothing pending once told, got %v", pending)
	}
}

func TestALandingThatEndedWhileNoFollowerWaitedIsToldOnceAndNotRunAgain(t *testing.T) {
	prog := standIn(t, "0", 1)
	l := &apartproc.Landings{Dir: t.TempDir(), Program: prog, Scope: noScope(t), Poll: 20 * time.Millisecond}
	if err := l.Begin(context.Background(), "lampas", false); err != nil {
		t.Fatal(err)
	}
	// Nobody waits: the follower stopped. It ends meanwhile.
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if pending, _ := l.Pending(context.Background()); len(pending) == 1 {
			if _, err := os.Stat(filepath.Join(l.Dir, "lampas", "status")); err == nil {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("the landing never ended")
		}
	}
	if err := l.Begin(context.Background(), "lampas", false); err != nil {
		t.Fatal(err)
	}
	err := l.Wait(context.Background(), "lampas")
	if err == nil || !strings.Contains(err.Error(), "mw land lampas failed (exit status 1") {
		t.Fatalf("expected the landing's own failure, got %v", err)
	}
	args, _ := os.ReadFile(prog + ".args")
	if got := strings.Count(string(args), "land lampas"); got != 1 {
		t.Errorf("expected the ended landing told, not run again: %d runs", got)
	}
	// Told once: the next Begin is a landing of its own.
	if err := l.Begin(context.Background(), "lampas", false); err != nil {
		t.Fatal(err)
	}
	_ = l.Wait(context.Background(), "lampas")
	args, _ = os.ReadFile(prog + ".args")
	if got := strings.Count(string(args), "land lampas"); got != 2 {
		t.Errorf("expected a second landing once the first was told, got %d", got)
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
