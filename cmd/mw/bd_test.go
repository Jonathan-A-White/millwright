package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.568: mw bd waits for its turn and for quiet before it runs bd.

func TestBdWaitFlagAndEnvironment(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		env     string
		want    time.Duration
		left    []string
		wantErr bool
	}{
		{args: []string{"list"}, left: []string{"list"}},
		{args: []string{"--mw-wait", "30s", "list", "-n", "5"}, want: 30 * time.Second, left: []string{"list", "-n", "5"}},
		{args: []string{"--mw-wait=5s", "list"}, want: 5 * time.Second, left: []string{"list"}},
		{args: []string{"--mw-wait=0", "list"}, want: -1, left: []string{"list"}},
		{args: []string{"list"}, env: "45s", want: 45 * time.Second, left: []string{"list"}},
		{args: []string{"--mw-wait=9s", "list"}, env: "45s", want: 9 * time.Second, left: []string{"list"}},
		// Only the first argument is mw bd's: later ones belong to bd.
		{args: []string{"list", "--mw-wait", "30s"}, left: []string{"list", "--mw-wait", "30s"}},
		{args: []string{"--mw-wait=soon", "list"}, wantErr: true},
		{args: []string{"--mw-wait"}, wantErr: true},
		{args: []string{"list"}, env: "-3s", wantErr: true},
	} {
		got, left, err := bdWaitFrom(tc.args, tc.env)
		if (err != nil) != tc.wantErr || (err == nil && (got != tc.want || strings.Join(left, " ") != strings.Join(tc.left, " "))) {
			t.Errorf("%v env %q: got %s %v err %v, want %s %v (error %v)", tc.args, tc.env, got, left, err, tc.want, tc.left, tc.wantErr)
		}
	}
}

// fakeBdScript writes a bd that notes when it starts and ends, prints its
// arguments and a variable of its environment, and leaves with the status
// MW_FAKE_EXIT names.
func fakeBdScript(t *testing.T, dir string) (script, log string) {
	t.Helper()
	log = filepath.Join(dir, "log")
	script = filepath.Join(dir, "realbd")
	body := "#!/bin/sh\n" +
		"echo \"start $1\" >> " + log + "\n" +
		"sleep 0.3\n" +
		"echo \"end $1\" >> " + log + "\n" +
		"echo \"args:$* seen:$MW_FAKE_SEEN\"\n" +
		"exit ${MW_FAKE_EXIT:-0}\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script, log
}

func bdWorld(t *testing.T) (log string, procs *apptest.FakeUserProcesses) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	script, log := fakeBdScript(t, t.TempDir())
	t.Setenv(bdRealEnv, script)
	procs = apptest.NewFakeUserProcesses()
	real := newBdQuiet
	newBdQuiet = func() application.UserProcesses { return procs }
	t.Cleanup(func() { newBdQuiet = real })
	return log, procs
}

func TestTwoMwBdCommandsAtOnceRunOneAfterTheOther(t *testing.T) {
	log, _ := bdWorld(t)
	var wg sync.WaitGroup
	for _, id := range []string{"a", "b", "c"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if out, err := runMw(t, "bd", id); err != nil {
				t.Errorf("mw bd %s: %v\n%s", id, err, out)
			}
		}()
	}
	wg.Wait()
	said, _ := os.ReadFile(log)
	entries := strings.Split(strings.TrimSpace(string(said)), "\n")
	if len(entries) != 6 {
		t.Fatalf("want three starts and three ends, got %q", said)
	}
	// Each start is followed at once by the end of the same bd.
	for i := 0; i < len(entries); i += 2 {
		id := strings.TrimPrefix(entries[i], "start ")
		if entries[i] != "start "+id || entries[i+1] != "end "+id {
			t.Fatalf("bd ran together: %q", said)
		}
	}
}

func TestMwBdRunsBdWithTheEnvironmentAndExitStatusOfTheCaller(t *testing.T) {
	bdWorld(t)
	t.Setenv("MW_FAKE_SEEN", "yes")
	t.Setenv("MW_FAKE_EXIT", "3")
	out, err := runMw(t, "bd", "show", "x-1")
	if !strings.Contains(out, "args:show x-1 seen:yes") {
		t.Errorf("bd did not see its arguments and environment: %q", out)
	}
	exit, ok := err.(*handedOffExit)
	if !ok || exit.code != 3 {
		t.Errorf("want bd's status 3 back, got %v", err)
	}
}

func TestMwBdThatCannotGetQuietExitsNonZeroNamingTheBlocker(t *testing.T) {
	log, procs := bdWorld(t)
	procs.Start(application.Process{Pid: 4242, Argv: []string{"/usr/bin/mw", "sync"}})
	out, err := runMw(t, "bd", "--mw-wait", "1s", "list")
	if err == nil {
		t.Fatalf("mw bd ran beside a sync and left with 0:\n%s", out)
	}
	if !strings.Contains(out, "4242") || !strings.Contains(out, "mw sync") {
		t.Errorf("the failure should name the blocker, it said %q", out)
	}
	if _, statErr := os.Stat(log); statErr == nil {
		t.Errorf("bd was run beside the sync")
	}
	// mw-gq6.1021: it says which command to run again, and leaves with 1.
	if !strings.Contains(out, "run again: mw bd list") {
		t.Errorf("the failure should say which command to run again, it said %q", out)
	}
	if exit, ok := err.(*handedOffExit); !ok || exit.code != 1 {
		t.Errorf("want exit status 1, got %v", err)
	}
}
