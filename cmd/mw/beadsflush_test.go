package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// flushWorld sets a vault whose home file names home, a bd on PATH that
// records its calls, and the flush file in a temp dir. It gives the job
// homeSpring made for host.
func flushWorld(t *testing.T, home, host string) (job application.SpringJob, calls, flushFile string) {
	t.Helper()
	vault := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "home"), []byte(home+" 2026-09-29T12:00:00Z mayor\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mwConfig(t, fmt.Sprintf("host = %q\nvault = %q\n", host, vault))
	bin := t.TempDir()
	calls = filepath.Join(bin, "calls")
	if err := os.WriteFile(filepath.Join(bin, "bd"), []byte("#!/bin/sh\necho \"$*\" >> "+calls+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	flushFile = filepath.Join(t.TempDir(), "beads-flush")
	t.Setenv("MW_BEADS_FLUSH_FILE", flushFile)

	defer func(was userUnits) { springUnits = was }(springUnits)
	springUnits = &fakeUnits{installed: map[string]bool{}}
	spring, err := homeSpring(t.TempDir()+"/log.jsonl", host, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range spring.Jobs {
		if j.Name == "beads-flush" {
			return j, calls, flushFile
		}
	}
	t.Fatalf("no beads-flush job among %+v", spring.Jobs)
	return application.SpringJob{}, "", ""
}

func TestHomeSpringFlushesTheBeadsEveryFifteenMinutesOnTheFollowersClock(t *testing.T) {
	job, calls, flushFile := flushWorld(t, "laptop", "laptop")
	if job.Every != 15*60*1e9 || job.Reason != application.ClockReason {
		t.Fatalf("the job is %+v, want a 15 minute clock job", job)
	}

	if err := job.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	if said, _ := os.ReadFile(calls); !strings.Contains(string(said), "dolt commit -m mw flush ") {
		t.Fatalf("bd was run as %q, want a dolt commit", said)
	}
	if record, _ := os.ReadFile(flushFile); !strings.HasPrefix(string(record), "ok ") {
		t.Fatalf("the flush was recorded as %q", record)
	}
}

func TestTheBeadsFlushJobDoesNothingOnAHostThatIsNotHome(t *testing.T) {
	job, calls, flushFile := flushWorld(t, "desktop", "laptop")

	if err := job.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(calls); err == nil {
		t.Fatal("bd was run on a host that is not home")
	}
	if _, err := os.Stat(flushFile); err == nil {
		t.Fatal("a flush was recorded on a host that is not home")
	}
}
