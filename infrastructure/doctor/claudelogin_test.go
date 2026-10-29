package doctor_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

var claudeLoginNow = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

// claudeEnvAged writes a claude.env whose mtime is the given days before
// claudeLoginNow and returns its path.
func claudeEnvAged(t *testing.T, days int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "claude.env")
	if err := os.WriteFile(path, []byte("CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	made := claudeLoginNow.AddDate(0, 0, -days)
	if err := os.Chtimes(path, made, made); err != nil {
		t.Fatal(err)
	}
	return path
}

// runClaudeLogin runs the claude-login check once and returns the report and
// the alarms it sent.
func runClaudeLogin(t *testing.T, check *doctor.ClaudeLogin) (application.DoctorReport, []string) {
	t.Helper()
	var alarms []string
	check.Now = func() time.Time { return claudeLoginNow }
	check.Alarm = func(_ context.Context, text string) (uint64, error) {
		alarms = append(alarms, text)
		return 1, nil
	}
	store := doctor.New(t.TempDir())
	report, err := application.Doctor{
		Checks: application.DoctorChecks{check},
		State:  store,
		Log:    store,
		Now:    func() time.Time { return claudeLoginNow },
	}.Run(context.Background(), "", false)
	if err != nil {
		t.Fatalf("doctor run: %v", err)
	}
	return report, alarms
}

func loggedIn(context.Context) (bool, error) { return true, nil }

func TestClaudeLoginAlarmsWhenTheTokenIs336DaysOld(t *testing.T) {
	check := doctor.NewClaudeLogin(claudeEnvAged(t, 336))
	check.AuthStatus = loggedIn
	_, alarms := runClaudeLogin(t, check)
	if len(alarms) != 1 {
		t.Fatalf("a token 336 days old: want one alarm, got %q", alarms)
	}
	for _, want := range []string{"336 days", "claude setup-token", "printf 'CLAUDE_CODE_OAUTH_TOKEN=%s\\n' \"$token\" > ~/.config/mw/claude.env", "docs/secrets.md"} {
		if !strings.Contains(alarms[0], want) {
			t.Errorf("the alarm should say %q, got %q", want, alarms[0])
		}
	}
}

func TestClaudeLoginAlarmsAtExactly335DaysAndIsQuietAt334(t *testing.T) {
	for days, wantAlarm := range map[int]bool{334: false, 335: true} {
		check := doctor.NewClaudeLogin(claudeEnvAged(t, days))
		check.AuthStatus = loggedIn
		if _, alarms := runClaudeLogin(t, check); (len(alarms) == 1) != wantAlarm {
			t.Errorf("token %d days old: alarms %q, want alarm %v", days, alarms, wantAlarm)
		}
	}
}

func TestClaudeLoginIsQuietForATokenThatIs300DaysOld(t *testing.T) {
	check := doctor.NewClaudeLogin(claudeEnvAged(t, 300))
	check.AuthStatus = loggedIn
	check.Screens = func(context.Context) ([]string, error) { return nil, nil }
	report, alarms := runClaudeLogin(t, check)
	if len(alarms) != 0 {
		t.Fatalf("a token 300 days old should be quiet, got %q", alarms)
	}
	if got := report.Results[0].Verdict; got != "ok" {
		t.Fatalf("verdict = %q, want ok", got)
	}
}

func TestClaudeLoginAlarmsWhenAuthStatusSaysNotLoggedIn(t *testing.T) {
	check := doctor.NewClaudeLogin(claudeEnvAged(t, 10))
	check.AuthStatus = func(context.Context) (bool, error) { return false, nil }
	_, alarms := runClaudeLogin(t, check)
	if len(alarms) != 1 {
		t.Fatalf("not logged in: want one alarm, got %q", alarms)
	}
	for _, want := range []string{"claude auth status", "not logged in", "claude setup-token", "docs/secrets.md"} {
		if !strings.Contains(alarms[0], want) {
			t.Errorf("the alarm should say %q, got %q", want, alarms[0])
		}
	}
}

func TestClaudeLoginAlarmsWhenASeatsPaneShowsNotLoggedIn(t *testing.T) {
	check := doctor.NewClaudeLogin(claudeEnvAged(t, 10))
	check.AuthStatus = loggedIn
	check.Screens = func(context.Context) ([]string, error) { return []string{"mayor-2026-10-28-01"}, nil }
	_, alarms := runClaudeLogin(t, check)
	if len(alarms) != 1 || !strings.Contains(alarms[0], "mayor-2026-10-28-01") || !strings.Contains(alarms[0], "Not logged in") {
		t.Fatalf("a pane showing Not logged in should alarm naming the window, got %q", alarms)
	}
}

func TestClaudeLoginWithNoEnvFileJudgesOnlyTheLogin(t *testing.T) {
	check := doctor.NewClaudeLogin(filepath.Join(t.TempDir(), "claude.env"))
	check.AuthStatus = loggedIn
	report, alarms := runClaudeLogin(t, check)
	if len(alarms) != 0 || report.Results[0].Verdict != "ok" {
		t.Fatalf("no env file and a good login should be ok and quiet, got %q %q", report.Results[0].Verdict, alarms)
	}
}

func TestClaudeLoginOnAHostWithNoClaudeIsOkNotApplicable(t *testing.T) {
	check := doctor.NewClaudeLogin(filepath.Join(t.TempDir(), "claude.env"))
	check.AuthStatus = func(context.Context) (bool, error) { return false, exec.ErrNotFound }
	report, alarms := runClaudeLogin(t, check)
	if len(alarms) != 0 || report.Results[0].Verdict != "ok" {
		t.Fatalf("no claude program should be ok and quiet, got %q %q", report.Results[0].Verdict, alarms)
	}
}

func TestClaudeLoginCannotTellWhenAuthStatusFailsOddly(t *testing.T) {
	check := doctor.NewClaudeLogin(claudeEnvAged(t, 10))
	check.AuthStatus = func(context.Context) (bool, error) { return false, errors.New("claude auth status: gave no JSON") }
	report, alarms := runClaudeLogin(t, check)
	if len(alarms) != 0 || report.Results[0].Verdict != "cannot-tell" {
		t.Fatalf("an unreadable status should be cannot-tell and quiet, got %q %q", report.Results[0].Verdict, alarms)
	}
}

func TestClaudeLoginAuthStatusReadsTheProgramsJSON(t *testing.T) {
	dir := t.TempDir()
	for body, want := range map[string]bool{
		`{"loggedIn": true, "authMethod": "claude.ai"}`: true,
		`{"loggedIn": false}`:                           false,
	} {
		program := filepath.Join(dir, "claude")
		script := "#!/bin/sh\ncat <<'EOF'\n" + body + "\nEOF\n"
		if !want {
			script += "exit 1\n"
		}
		if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		got, err := doctor.ClaudeAuthStatus(program, "")(context.Background())
		if err != nil || got != want {
			t.Errorf("status %s: got %v, %v; want %v", body, got, err, want)
		}
	}
}
