package beads_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

const breakerWords = "dolt circuit breaker is open: server appears down, failing fast (cooldown 5s)"

// breakingBd is a stand-in bd that says words and exits 1 the first fails times
// it is run, and prints "ok" after that. The gateway's waits are recorded, not
// taken.
func breakingBd(t *testing.T, words string, fails int) (*beads.Gateway, *[]time.Duration, func() int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	dir := t.TempDir()
	counter := filepath.Join(dir, "runs")
	program := filepath.Join(dir, "bd-breaking")
	script := fmt.Sprintf("#!/bin/sh\necho x >> %q\nruns=$(wc -l < %q)\n"+
		"if [ \"$runs\" -le %d ]; then echo '%s' >&2; exit 1; fi\necho ok\n", counter, counter, fails, words)
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	var waits []time.Duration
	gateway := beads.New(t.TempDir(), beads.WithProgram(program), beads.WithSleep(func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}))
	runs := func() int {
		data, _ := os.ReadFile(counter)
		return strings.Count(string(data), "x")
	}
	return gateway, &waits, runs
}

func TestABdWhoseCircuitBreakerIsOpenIsTriedAgainAfterTheCooldownAndSucceeds(t *testing.T) {
	gateway, waits, runs := breakingBd(t, breakerWords, 2)

	err := gateway.SetStoryMetadata(context.Background(), "mw-x.1", map[string]string{"k": "v"})

	if err != nil {
		t.Fatalf("expected the third try to succeed, got %v", err)
	}
	if runs() != 3 {
		t.Fatalf("expected three runs of bd, got %d", runs())
	}
	if len(*waits) != 2 {
		t.Fatalf("expected two waits on the fake clock, got %v", *waits)
	}
	for _, wait := range *waits {
		if wait < 5*time.Second {
			t.Errorf("expected each wait to outlast the 5s cooldown, got %v", wait)
		}
	}
}

func TestABdWhoseCircuitBreakerStaysOpenGivesUpAfterItsBoundedTries(t *testing.T) {
	gateway, waits, runs := breakingBd(t, breakerWords, 100)

	err := gateway.SetStoryMetadata(context.Background(), "mw-x.1", map[string]string{"k": "v"})

	if err == nil || !strings.Contains(err.Error(), "circuit breaker is open") {
		t.Fatalf("expected bd's own words in the error, got %v", err)
	}
	if runs() != 3 || len(*waits) != 2 {
		t.Fatalf("expected three runs and two waits, got %d runs and waits %v", runs(), *waits)
	}
	var total time.Duration
	for _, wait := range *waits {
		total += wait
	}
	if total > 30*time.Second {
		t.Errorf("expected the waits to total about 20s, got %v", total)
	}
}

func TestABdThatFailsForAnotherReasonIsNotTriedAgain(t *testing.T) {
	for _, words := range []string{`no issue found matching "mw-x.1"`, "permission denied: bd is read-only"} {
		gateway, waits, runs := breakingBd(t, words, 100)

		err := gateway.SetStoryMetadata(context.Background(), "mw-x.1", map[string]string{"k": "v"})

		if err == nil {
			t.Fatalf("%q: expected bd's failure", words)
		}
		if runs() != 1 || len(*waits) != 0 {
			t.Errorf("%q: expected one run and no wait, got %d runs and waits %v", words, runs(), *waits)
		}
	}
}

func TestABdWhoseServerRefusesTheConnectionIsTriedAgain(t *testing.T) {
	gateway, _, runs := breakingBd(t, "dial tcp 127.0.0.1:3307: connect: connection refused", 1)

	if err := gateway.SetStoryMetadata(context.Background(), "mw-x.1", map[string]string{"k": "v"}); err != nil {
		t.Fatalf("expected the second try to succeed, got %v", err)
	}
	if runs() != 2 {
		t.Fatalf("expected two runs, got %d", runs())
	}
}
