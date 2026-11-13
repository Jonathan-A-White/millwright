package application_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// A command that ran past its limit and finished of itself is not said to be
// stopped or half-deployed; one stopped at the stretched limit is (mw-gq6.1268).
func TestALateCommandIsSaidToHaveFinishedByItselfAndNotToHaveBeenStopped(t *testing.T) {
	late := application.Ran{Command: "npm run lxx:ship", Limit: 5 * time.Minute, Took: 7 * time.Minute}
	if !late.Succeeded() || !late.Late() {
		t.Fatalf("expected a late pass, got %+v", late)
	}
	want := "after landing: npm run lxx:ship: ok, but it ran 7m0s, past its limit of 5m0s, and finished by itself"
	if got := late.Line(); got != want {
		t.Errorf("expected %q, got %q", want, got)
	}

	onTime := application.Ran{Command: "make build", Limit: 5 * time.Minute, Took: 4 * time.Minute}
	if onTime.Late() || onTime.Line() != "after landing: make build: ok" {
		t.Errorf("expected a run inside its limit to read as it always did, got %q", onTime.Line())
	}

	failed := application.Ran{Command: "make build", Status: 2, Output: "boom\n", Limit: time.Minute, Took: 2 * time.Minute}
	if line := failed.Line(); !strings.Contains(line, "exit status 2 (it ran 2m0s, past its limit of 1m0s, and finished by itself): boom") {
		t.Errorf("expected a late failure to say so, got %q", line)
	}

	hung := application.Ran{Command: "make build", Status: -1, TimedOut: 10 * time.Minute, Limit: 5 * time.Minute, Took: 10 * time.Minute}
	if hung.Late() || hung.Succeeded() || !strings.Contains(hung.Line(), "stopped after 10m0s, still running") {
		t.Errorf("expected a hung command to be stopped and said so, got %q", hung.Line())
	}
}
