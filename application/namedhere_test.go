package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestErrorLineOfIsTheMessageUnderTheFailedTest(t *testing.T) {
	out := "=== RUN   TestRunBook\n--- FAIL: TestRunBook (0.00s)\n    runbook_test.go:41: no ffmpeg\nFAIL\nFAIL\texample.com/rig/runbook\t0.01s\n"
	if got := application.ErrorLineOf(out); got != "runbook_test.go:41: no ffmpeg" {
		t.Errorf("ErrorLineOf = %q", got)
	}
	if got := application.ErrorLineOf("boom\n\nexit status 1\n"); got != "boom" {
		t.Errorf("with no failed-test header ErrorLineOf = %q, want the last meaningful line", got)
	}
}

func TestNamedHereVerdictsReadBack(t *testing.T) {
	for text, want := range map[string]application.NamedRun{
		"named: passed\ncommand: npm test\nok\n":       {Ran: true, Checked: application.Checked{Passed: true, Command: "npm test", Output: "ok\n"}},
		"named: failed\ncommand: c\n--- FAIL: TestX\n": {Ran: true, Checked: application.Checked{Command: "c", Output: "--- FAIL: TestX\n"}},
		"named: not run\ncommand: c\nno such file\n":   {Ran: true, Checked: application.Checked{NotRun: true, Command: "c", Output: "no such file\n"}},
		"named: no command\ncommand: \n":               {},
	} {
		got, err := application.ParseNamedHere("Welcome to the box\n" + text)
		if err != nil || got != want {
			t.Errorf("ParseNamedHere(%q) = %+v, %v; want %+v", text, got, err, want)
		}
	}
	if _, err := application.ParseNamedHere("Welcome to the box\n"); err == nil {
		t.Error("text with no verdict was read as a pass")
	}
}

type fakeLanding struct {
	application.Landing
	opened, closed string
}

func (f *fakeLanding) OpenLanding(_ context.Context, _, base string) (string, error) {
	f.opened = base
	return "/tmp/landing", nil
}
func (f *fakeLanding) CloseLanding(_ context.Context, _, dir string) error {
	f.closed = dir
	return nil
}

type fakeFetch struct{ application.Worktrees }

func (fakeFetch) Fetch(context.Context, string) error { return nil }

type ranNamed struct{ run application.NamedRun }

func (r ranNamed) RunNamed(context.Context, string, string, []string) (application.NamedRun, error) {
	return r.run, nil
}

func TestNamedHerePrintsHowTheTestsCameOutAndTakesTheLandingAway(t *testing.T) {
	var out bytes.Buffer
	landing := &fakeLanding{}
	here := application.NamedHere{
		Worktrees: fakeFetch{}, Landing: landing, Rigs: map[string]string{"r": "/rigs/r"}, Out: &out,
		NamedChecks: ranNamed{application.NamedRun{Ran: true, Checked: application.Checked{Passed: true, Command: "go test -run X", Output: "ok\n"}}},
	}
	if err := here.Run(context.Background(), "r", "origin/main", []string{"TestX"}); err != nil {
		t.Fatal(err)
	}
	if landing.opened != "origin/main" || landing.closed != "/tmp/landing" {
		t.Errorf("landing opened at %q, closed %q", landing.opened, landing.closed)
	}
	got, err := application.ParseNamedHere(out.String())
	if err != nil || !got.Passed || got.Command != "go test -run X" || !strings.Contains(got.Output, "ok") {
		t.Errorf("read back %+v, %v from %q", got, err, out.String())
	}
	if err := here.Run(context.Background(), "other", "origin/main", []string{"TestX"}); err == nil {
		t.Error("a rig not checked out here was run")
	}
}
