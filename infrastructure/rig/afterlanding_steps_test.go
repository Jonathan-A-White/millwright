package rig

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// holdPortEnv, when set, turns the test binary into a stand-in for a shots
// run's preview server: it binds PREVIEW_PORT (else the fallback port the test
// names), says so with a file in the directory holdPortEnv names, and holds the
// port until the test makes the release file there.
const (
	holdPortEnv      = "MW_TEST_HOLD_PORT_DIR"
	holdPortFallback = "MW_TEST_HOLD_PORT_FALLBACK"
)

func TestMain(m *testing.M) {
	if dir := os.Getenv(holdPortEnv); dir != "" {
		os.Exit(holdPort(dir))
	}
	os.Exit(m.Run())
}

// holdPort is the stand-in's whole life. It never waits on a clock for the
// other run: it waits for the release file, which the test makes once both runs
// have bound their ports.
func holdPort(dir string) int {
	port := os.Getenv("PREVIEW_PORT")
	if port == "" {
		port = os.Getenv(holdPortFallback)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		fmt.Printf("Error: http://localhost:%s is already used\n", port)
		return 1
	}
	defer listener.Close()
	if err := os.WriteFile(filepath.Join(dir, "bound-"+port), nil, 0o644); err != nil {
		fmt.Println(err)
		return 1
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "release")); err == nil {
			return 0
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTwoAfterLandingRunsAtOnceOnOneHostNeverCollideOnAPort(t *testing.T) {
	dir := t.TempDir()
	// Without a port of its own each run would take the one port the checkout
	// names, as postern's shots took 5370, and the second would find it used.
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fallback := strconv.Itoa(taken.Addr().(*net.TCPAddr).Port)
	taken.Close()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := fmt.Sprintf("%s=%s %s=%s %q -test.run=^$", holdPortEnv, dir, holdPortFallback, fallback, self)
	after := NewAfterLanding(WithAfterLimit(time.Minute), WithAfterCommands(map[string]string{"postern": command}))

	type result struct {
		ran string
		ok  bool
	}
	done := make(chan result, 2)
	for range 2 {
		go func() {
			ran, err := after.Run(context.Background(), "postern", t.TempDir())
			done <- result{fmt.Sprintf("%+v %v", ran, err), err == nil && ran.Succeeded()}
		}()
	}

	var results []result
	release := func() { _ = os.WriteFile(filepath.Join(dir, "release"), nil, 0o644) }
	defer release()
	for deadline := time.Now().Add(30 * time.Second); len(results) == 0; {
		bound, _ := filepath.Glob(filepath.Join(dir, "bound-*"))
		if len(bound) == 2 {
			break
		}
		select {
		case r := <-done:
			results = append(results, r)
		case <-time.After(10 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatal("the two runs never both bound a port")
		}
	}
	release()
	for len(results) < 2 {
		results = append(results, <-done)
	}
	for _, r := range results {
		if !r.ok {
			t.Errorf("expected both runs at once to pass the port step, one said: %s", r.ran)
		}
	}
}

func TestTheAfterLandingCommandIsGivenAFreePortOfItsOwn(t *testing.T) {
	after := NewAfterLanding(WithAfterCommands(map[string]string{"postern": `echo "port=$PREVIEW_PORT"`}))

	ran, err := after.Run(context.Background(), "postern", t.TempDir())
	if err != nil || !ran.Succeeded() {
		t.Fatalf("running the command: %+v, %v", ran, err)
	}
	said := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ran.Output), "port="))
	port, err := strconv.Atoi(said)
	if err != nil || port <= 0 || port > 65535 {
		t.Fatalf("expected PREVIEW_PORT to name a port, got %q", ran.Output)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:"+said)
	if err != nil {
		t.Errorf("expected the port it was given to be free once it ended: %v", err)
	} else {
		listener.Close()
	}
}

func TestAnAfterLandingCommandIsReadAsTheStepsItsAndsJoin(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    []string
	}{
		{"make build", []string{"make build"}},
		{"npm ci && npm run build", []string{"npm ci", "npm run build"}},
		{
			`npm ci && (cd server && go test ./...) && ssh -o BatchMode=yes root@h 'rsync -a a/ b/ && true' && echo "x && y" && npm run shots:publish -- latest`,
			[]string{"npm ci", "(cd server && go test ./...)", "ssh -o BatchMode=yes root@h 'rsync -a a/ b/ && true'", `echo "x && y"`, "npm run shots:publish -- latest"},
		},
		{`a \&\& b && c`, []string{`a \&\& b`, "c"}},
		{"a && b | tee log", []string{"a", "b | tee log"}},
		{"{ a && b; } && c", []string{"{ a && b; }", "c"}},
		{"a && $(b && c) && d", []string{"a", "$(b && c)", "d"}},
		{"a && `b && c` && d", []string{"a", "`b && c`", "d"}},
		{"a 2>&1 && b >&2 && c &>log", []string{"a 2>&1", "b >&2", "c &>log"}},
		// Anything but a plain chain of &&s is one step, read as it is.
		{"a; b && c", []string{"a; b && c"}},
		{"a || b && c", []string{"a || b && c"}},
		{"a & b && c", []string{"a & b && c"}},
		{"a && b\nc", []string{"a && b\nc"}},
		{"a && 'b", []string{"a && 'b"}},
		{"a && && b", []string{"a && && b"}},
	} {
		if got := commandSteps(tc.command); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("commandSteps(%q) = %q, want %q", tc.command, got, tc.want)
		}
	}
}

func TestAnAfterLandingCommandOfStepsSaysWhichStepWasRunningWhenItEnded(t *testing.T) {
	command := `echo "it's built" && sh -c 'echo "it is broken"; exit 3' && echo never`
	after := NewAfterLanding(WithAfterCommands(map[string]string{"postern": command}))

	ran, err := after.Run(context.Background(), "postern", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if ran.Status != 3 || ran.Step != 2 || len(ran.Steps) != 3 || ran.Steps[1] != `sh -c 'echo "it is broken"; exit 3'` {
		t.Errorf("expected step 2 of 3 to have failed with exit status 3, got %+v", ran)
	}
	if ran.Command != command || strings.Contains(ran.Output, "never") || !strings.Contains(ran.Output, "it's built") || !strings.Contains(ran.Output, "it is broken") {
		t.Errorf("expected the command as the host names it and the output of the steps that ran, got %+v", ran)
	}

	ran, err = after.Run(context.Background(), "postern", t.TempDir())
	if err == nil && ran.Step != 2 {
		t.Errorf("expected the same step every time, got %+v", ran)
	}

	ok := NewAfterLanding(WithAfterCommands(map[string]string{"postern": "echo a && echo b"}))
	ran, err = ok.Run(context.Background(), "postern", t.TempDir())
	if err != nil || !ran.Succeeded() || ran.Step != 2 || !strings.Contains(ran.Output, "a\n") {
		t.Errorf("expected a chain that passed to have run to its last step, got %+v, %v", ran, err)
	}
}
