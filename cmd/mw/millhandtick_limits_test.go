package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// The Millhand's self-update builds mw through the same after-landing port every
// landing uses, so it runs under the host's builder_nice and builder_jobs as a
// landing's command does (mw-gq6.1308): an unniced, unlimited `make build` on a
// loaded host was the load mw-gq6.1294 set out to cap.
func TestTheMillhandsSelfUpdateRunsUnderTheHostsNiceAndWorkerLimit(t *testing.T) {
	checkout := t.TempDir()
	out := filepath.Join(t.TempDir(), "seen.txt")
	command := "echo \"nice=$(nice) JOBS=$JOBS GOMAXPROCS=$GOMAXPROCS\" > " + out
	rigs := map[string]string{"millwright": checkout}
	afterLanding := map[string]string{"millwright": command}

	update := hostSelfUpdate(nil, rigs, afterLanding, nil, rig.WithAfterDeploySteps(nil), 7, 3, t.TempDir(), nil, "laptop")
	if _, err := update.After.Run(t.Context(), "millwright", checkout); err != nil {
		t.Fatalf("running the self-update's command: %v", err)
	}
	seen, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("the command left no record: %v", err)
	}
	// nice adds to the niceness the test was started at, up to 19.
	base, err := exec.Command("nice").Output()
	if err != nil {
		t.Fatalf("reading this process's niceness: %v", err)
	}
	baseNice, err := strconv.Atoi(strings.TrimSpace(string(base)))
	if err != nil {
		t.Fatalf("reading this process's niceness: %v", err)
	}
	wantNice := min(baseNice+7, 19)
	for _, want := range []string{"nice=" + strconv.Itoa(wantNice) + " ", "JOBS=3 ", "GOMAXPROCS=3"} {
		if !strings.Contains(string(seen), want) {
			t.Errorf("expected the self-update's command to see %q, it saw %q", want, strings.TrimSpace(string(seen)))
		}
	}
}
