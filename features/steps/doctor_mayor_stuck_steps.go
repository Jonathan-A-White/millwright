package steps

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"

	"github.com/cucumber/godog"
)

// mayorStuckProxyNames are the settings the cure may drop, the order the
// steps list them in.
var mayorStuckProxyNames = []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"}

// mayorStuckFixture is the real mayor-stuck check over a stand-in tmux and
// bin/mayor-up, a transcript written to a temp projects directory, and an
// alarm that only writes down what it was told. Nothing here reads this
// host's own transcripts, environment or tmux server.
type mayorStuckFixture struct {
	vault      string
	projects   string
	tmuxLog    string
	tmuxEnv    string
	upEnv      string
	acting     string
	environ    []string
	alarms     []string
	listener   net.Listener
	proxyValue string
	check      *doctor.MayorStuck
}

// registerMayorStuckSteps registers the steps of the mayor-stuck scenarios.
func (c *doctorContext) registerMayorStuckSteps(ctx *godog.ScenarioContext) {
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.stuck != nil && c.stuck.listener != nil {
			c.stuck.listener.Close()
		}
		return ctx, nil
	})

	ctx.Given(`^a stand-in tmux listing that Mayor window with a live claude process$`, c.aStandInTmuxForTheMayorWindow)
	ctx.Given(`^a stand-in bin/mayor-up that starts a Mayor in window "([^"]*)"$`, c.aStandInMayorUpThatRecordsItsEnvironment)
	ctx.Given(`^a transcript whose replies in the last 10 minutes were all API Errors$`, c.aTranscriptOfAPIErrors)
	ctx.Given(`^a transcript whose replies in the last 10 minutes were API Errors and one that succeeded$`, c.aTranscriptOfAPIErrorsAndASuccess)
	ctx.Given(`^a transcript whose only replies were API Errors (\d+) minutes ago$`, c.aTranscriptOfOldAPIErrors)
	ctx.Given(`^a transcript with no replies$`, c.aTranscriptWithNoReplies)
	ctx.Given(`^this host is not home$`, c.thisHostIsNotHome)
	ctx.Given(`^the environment sets HTTPS_PROXY and https_proxy to a proxy whose port refuses connections$`, c.theEnvironmentSetsADeadProxy)
	ctx.Given(`^the tmux server's global environment sets HTTPS_PROXY, https_proxy and ALL_PROXY to that same proxy$`, c.theTmuxEnvironmentSetsTheSameProxy)
	ctx.Given(`^the environment sets HTTPS_PROXY to a proxy that answers$`, c.theEnvironmentSetsALiveProxy)

	ctx.When(`^mw doctor's mayor-stuck check runs$`, func() error { return c.run(false) })

	ctx.Then(`^the Mayor's window was closed$`, c.theMayorsWindowWasClosed)
	ctx.Then(`^the Mayor's window was not closed$`, c.theMayorsWindowWasNotClosed)
	ctx.Then(`^no handover was written$`, c.noHandoverWasWritten)
	ctx.Then(`^no alarm was sent$`, func() error { return c.alarmsSent(0) })
	ctx.Then(`^exactly (\d+) alarms? (?:was|were) sent$`, c.alarmsSent)
	ctx.Then(`^the alarm says "([^"]*)"$`, c.theAlarmSays)
	ctx.Then(`^the successor's environment has no proxy setting$`, c.theSuccessorHasNoProxy)
	ctx.Then(`^the successor's environment keeps HTTPS_PROXY$`, c.theSuccessorKeepsHTTPSProxy)
	ctx.Then(`^tmux was told to unset HTTPS_PROXY, https_proxy and ALL_PROXY in its global environment$`, c.tmuxWasToldToUnsetTheProxies)
	ctx.Then(`^tmux was not told to unset anything$`, c.tmuxWasNotToldToUnset)
}

// stuckFixture builds the check on the first call of a scenario, over the
// vault the .mayor-acting step made.
func (c *doctorContext) stuckFixture() (*mayorStuckFixture, error) {
	if c.stuck != nil {
		return c.stuck, nil
	}
	vault, err := c.ensureMayorGoneVault()
	if err != nil {
		return nil, err
	}
	root, err := os.MkdirTemp("", "mw-doctor-mayor-stuck")
	if err != nil {
		return nil, err
	}
	f := &mayorStuckFixture{
		vault:    vault,
		projects: filepath.Join(root, "projects"),
		tmuxLog:  filepath.Join(root, "tmux-calls"),
		tmuxEnv:  filepath.Join(root, "tmux-env"),
		upEnv:    filepath.Join(root, "mayor-up-env"),
		acting:   filepath.Join(vault, application.ActingFileName("mayor")),
		environ:  []string{"PATH=" + os.Getenv("PATH"), "HOME=/nonexistent"},
	}
	f.check = doctor.NewMayorStuck(vault, c.state)
	f.check.Replies = claude.NewTranscripts(f.projects)
	f.check.Host = seatUpHost
	f.check.Now = func() time.Time { return c.now }
	f.check.Settle = time.Millisecond
	f.check.Environ = func() []string { return append([]string(nil), f.environ...) }
	f.check.Alarm = func(_ context.Context, text string) (uint64, error) {
		f.alarms = append(f.alarms, text)
		return uint64(len(f.alarms)), nil
	}
	f.check.Clear = func(context.Context, string, uint64) error { return nil }
	c.stuck = f
	c.real = f.check
	return f, nil
}

func (c *doctorContext) aStandInTmuxForTheMayorWindow() error {
	f, err := c.stuckFixture()
	if err != nil {
		return err
	}
	dir := filepath.Dir(f.tmuxLog)
	program := filepath.Join(dir, "tmux-stand-in")
	script := fmt.Sprintf(`#!/bin/sh
echo "$*" >>%q
case "$1" in
list-windows) printf '@7|mayor-2026-10-06-210\n' ;;
list-panes) printf '0 claude\n' ;;
show-environment) cat %q 2>/dev/null ;;
esac
exit 0
`, f.tmuxLog, f.tmuxEnv)
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		return err
	}
	f.check.Tmux = program
	return nil
}

func (c *doctorContext) aStandInMayorUpThatRecordsItsEnvironment(windowID string) error {
	f, err := c.stuckFixture()
	if err != nil {
		return err
	}
	binDir := filepath.Join(f.vault, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	c.mayorUpCalls = filepath.Join(f.vault, "mayor-up-calls")
	script := fmt.Sprintf(`#!/bin/sh
echo call >>%q
env >%q
echo %s
exit 0
`, c.mayorUpCalls, f.upEnv, windowID)
	return os.WriteFile(filepath.Join(binDir, "mayor-up"), []byte(script), 0o755)
}

// writeStuckTranscript writes the newest session's transcript for the vault, one
// line per reply the given number of minutes before now.
func (c *doctorContext) writeStuckTranscript(replies []stuckReply) error {
	f, err := c.stuckFixture()
	if err != nil {
		return err
	}
	dir := claude.ProjectDir(f.projects, f.vault)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	lines := []string{`{"type":"user","timestamp":"2026-10-06T00:00:00Z","message":{"role":"user","content":"begin"}}`}
	for _, r := range replies {
		at := c.now.Add(-r.ago).UTC().Format(time.RFC3339Nano)
		if r.apiError {
			lines = append(lines, fmt.Sprintf(`{"type":"assistant","timestamp":%q,"isApiErrorMessage":true,"message":{"role":"assistant","content":[{"type":"text","text":"API Error: Connection lost mid-response"}]}}`, at))
		} else {
			lines = append(lines, fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"role":"assistant","content":[{"type":"text","text":"Filed it."}],"usage":{"input_tokens":10}}}`, at))
		}
	}
	// A subagent's reply is another conversation, and a line cut short is
	// the one still being written: neither is the Mayor's own reply.
	lines = append(lines,
		fmt.Sprintf(`{"isSidechain":true,"type":"assistant","timestamp":%q,"message":{"role":"assistant","content":[{"type":"text","text":"subagent fine"}]}}`, c.now.Add(-time.Minute).UTC().Format(time.RFC3339Nano)),
		`{"type":"assistant","timestamp":"2026-10-0`)
	return os.WriteFile(filepath.Join(dir, "session-210.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

type stuckReply struct {
	ago      time.Duration
	apiError bool
}

func (c *doctorContext) aTranscriptOfAPIErrors() error {
	return c.writeStuckTranscript([]stuckReply{
		{9 * time.Minute, true}, {7 * time.Minute, true}, {4 * time.Minute, true}, {time.Minute, true},
	})
}

func (c *doctorContext) aTranscriptOfAPIErrorsAndASuccess() error {
	return c.writeStuckTranscript([]stuckReply{
		{9 * time.Minute, true}, {6 * time.Minute, false}, {4 * time.Minute, true}, {time.Minute, true},
	})
}

func (c *doctorContext) aTranscriptOfOldAPIErrors(minutes int) error {
	ago := time.Duration(minutes) * time.Minute
	return c.writeStuckTranscript([]stuckReply{{ago + time.Minute, true}, {ago, true}})
}

func (c *doctorContext) aTranscriptWithNoReplies() error {
	return c.writeStuckTranscript(nil)
}

func (c *doctorContext) thisHostIsNotHome() error {
	f, err := c.stuckFixture()
	if err != nil {
		return err
	}
	f.check.Home = &apptest.FakeHomeFile{Text: "desktop 2026-09-29T00:10:00Z mw@desktop"}
	return nil
}

// refusedProxy is the address of a port nothing listens on: one that was
// listening a moment ago.
func refusedProxy() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := l.Addr().String()
	return "http://" + addr, l.Close()
}

func (c *doctorContext) theEnvironmentSetsADeadProxy() error {
	f, err := c.stuckFixture()
	if err != nil {
		return err
	}
	if f.proxyValue, err = refusedProxy(); err != nil {
		return err
	}
	f.environ = append(f.environ, "HTTPS_PROXY="+f.proxyValue, "https_proxy="+f.proxyValue)
	return nil
}

func (c *doctorContext) theTmuxEnvironmentSetsTheSameProxy() error {
	f, err := c.stuckFixture()
	if err != nil {
		return err
	}
	// show-environment -g prints a variable the server holds as NAME=value
	// and one it has marked removed as -NAME.
	return os.WriteFile(f.tmuxEnv, []byte("DISPLAY=:0\nHTTPS_PROXY="+f.proxyValue+"\nhttps_proxy="+f.proxyValue+"\nALL_PROXY="+f.proxyValue+"\n-LESS\n"), 0o644)
}

func (c *doctorContext) theEnvironmentSetsALiveProxy() error {
	f, err := c.stuckFixture()
	if err != nil {
		return err
	}
	if f.listener, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
		return err
	}
	f.proxyValue = "http://" + f.listener.Addr().String()
	f.environ = append(f.environ, "HTTPS_PROXY="+f.proxyValue)
	return nil
}

func (f *mayorStuckFixture) read(path string) string {
	data, _ := os.ReadFile(path)
	return string(data)
}

func (c *doctorContext) theMayorsWindowWasClosed() error {
	f := c.stuck
	if !strings.Contains(f.read(f.tmuxLog), "kill-window -t @7") {
		return fmt.Errorf("expected tmux to be told to close window @7, its calls were:\n%s", f.read(f.tmuxLog))
	}
	return nil
}

func (c *doctorContext) theMayorsWindowWasNotClosed() error {
	f := c.stuck
	if strings.Contains(f.read(f.tmuxLog), "kill-window") {
		return fmt.Errorf("expected no window closed, tmux's calls were:\n%s", f.read(f.tmuxLog))
	}
	return nil
}

// noHandoverWasWritten: the successor takes the seat on its own, so the
// acting file is not rewritten by the cure.
func (c *doctorContext) noHandoverWasWritten() error {
	f := c.stuck
	if got, want := f.read(f.acting), "acting in window mayor-2026-10-06-210\n"; got != want {
		return fmt.Errorf("expected .mayor-acting untouched (%q), it holds %q", want, got)
	}
	return nil
}

func (c *doctorContext) alarmsSent(want int) error {
	if got := len(c.stuck.alarms); got != want {
		return fmt.Errorf("expected %d alarm(s) sent, got %d: %v", want, got, c.stuck.alarms)
	}
	return nil
}

func (c *doctorContext) theAlarmSays(substr string) error {
	for _, alarm := range c.stuck.alarms {
		if strings.Contains(alarm, substr) {
			return nil
		}
	}
	return fmt.Errorf("expected an alarm saying %q, got %v", substr, c.stuck.alarms)
}

func (c *doctorContext) theSuccessorHasNoProxy() error {
	f := c.stuck
	env := f.read(f.upEnv)
	if env == "" {
		return fmt.Errorf("bin/mayor-up recorded no environment: it did not run")
	}
	for _, name := range mayorStuckProxyNames {
		if strings.Contains(env, name+"=") {
			return fmt.Errorf("expected the successor's environment to hold no %s, it holds:\n%s", name, env)
		}
	}
	return nil
}

func (c *doctorContext) theSuccessorKeepsHTTPSProxy() error {
	f := c.stuck
	if want := "HTTPS_PROXY=" + f.proxyValue; !strings.Contains(f.read(f.upEnv), want) {
		return fmt.Errorf("expected the successor's environment to hold %s, it holds:\n%s", want, f.read(f.upEnv))
	}
	return nil
}

func (c *doctorContext) tmuxWasToldToUnsetTheProxies() error {
	f := c.stuck
	calls := f.read(f.tmuxLog)
	for _, name := range []string{"HTTPS_PROXY", "https_proxy", "ALL_PROXY"} {
		if !strings.Contains(calls, "set-environment -g -u "+name+"\n") {
			return fmt.Errorf("expected tmux to be told `set-environment -g -u %s`, its calls were:\n%s", name, calls)
		}
	}
	return nil
}

func (c *doctorContext) tmuxWasNotToldToUnset() error {
	f := c.stuck
	if calls := f.read(f.tmuxLog); strings.Contains(calls, "set-environment") {
		return fmt.Errorf("expected tmux told to unset nothing, its calls were:\n%s", calls)
	}
	return nil
}
