// Package codex turns a launch into the command line and environment that
// start a Codex session. It is the adapter behind application.Harness for a
// Path whose harness is codex, beside infrastructure/claude.
//
// It assembles and nothing else: it starts no session, reaches no network and
// spends no fuel. What it returns is an application.SessionSpec, which a Runner
// starts. The result file, the heartbeat, the close-out and the retrying bd are
// the shell line every harness shares (infrastructure/sessionshell).
package codex

import (
	"fmt"
	"strconv"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/sessionshell"
)

// Program is the command this adapter builds a line for, and Shell is what
// reads that line. The line is read by a shell because the session's result is
// a redirection: `codex exec --json` prints its event stream on stdout, and the
// boot text reaches it on stdin.
const (
	Program = "codex"
	Shell   = "/bin/sh"
)

// DefaultTimeLimit is how long a session may run before it is ended. Codex has
// no turn cap, so the wall clock is the cap (application.CodexTimeoutExit).
const DefaultTimeLimit = 90 * time.Minute

// LastMessageFile is where, inside the worktree, Codex leaves its last message
// (`codex exec -o`).
const LastMessageFile = ".mw-last.md"

// Harness assembles Codex sessions.
type Harness struct {
	program    string
	shell      string
	timeLimit  time.Duration
	envFile    string
	serverHost string
	nice       int
	jobs       int
	timing     map[string][]string
}

// Harness satisfies the port.
var _ application.Harness = (*Harness)(nil)

// Option is a setting of a Harness, given to New.
type Option func(*Harness)

// WithProgram names the Codex command to run, for a host that keeps it
// somewhere unusual — and for tests, which put a harmless command in its place.
func WithProgram(name string) Option {
	return func(h *Harness) { h.program = name }
}

// WithShell names the shell that reads the session's command line.
func WithShell(path string) Option {
	return func(h *Harness) { h.shell = path }
}

// WithTimeLimit sets how long a session may run, rounded up to whole minutes.
// A limit of nothing or less leaves DefaultTimeLimit.
func WithTimeLimit(d time.Duration) Option {
	return func(h *Harness) {
		if d > 0 {
			h.timeLimit = d
		}
	}
}

// WithEnvFile names a file of shell assignments, such as ~/.config/mw/beads.env,
// for a session to read before anything else it runs. The command line holds
// the file's path and never anything in it. An empty path does nothing.
func WithEnvFile(path string) Option {
	return func(h *Harness) { h.envFile = path }
}

// WithBeadsServerHost names the host a session's bd reaches the beads database
// server on: exported as BEADS_DOLT_SERVER_HOST after the env file is read. An
// empty host does nothing.
func WithBeadsServerHost(host string) Option {
	return func(h *Harness) { h.serverHost = host }
}

// WithNice starts the harness under `nice -n <n>`, so a Builder's session and
// the tests it runs yield the CPU to the mill's own work. 0 starts it as it was.
func WithNice(n int) Option {
	return func(h *Harness) { h.nice = n }
}

// WithJobs gives the session JOBS, GOMAXPROCS and VITEST_MAX_WORKERS as n
// (application.WorkerLimitEnv). 0 adds nothing.
func WithJobs(n int) Option {
	return func(h *Harness) { h.jobs = n }
}

// WithTimingTests gives a session working a rig MW_TIMING=skip and the rig's
// timing_tests globs (application.TimingEnv), by rig name.
func WithTimingTests(timing map[string][]string) Option {
	return func(h *Harness) { h.timing = timing }
}

// New returns a Harness that runs Codex as this host has it, unless an option
// says otherwise.
func New(opts ...Option) *Harness {
	h := &Harness{program: Program, shell: Shell, timeLimit: DefaultTimeLimit}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Name implements application.Harness.
func (h *Harness) Name() domain.Harness { return domain.HarnessCodex }

// Session implements application.Harness: the session that works one launch.
//
// It is a headless `codex exec --json`, so that what it cost and how it ended
// can be read back from the event stream. The sandbox is workspace-write with
// the network on (bd and git push need it) and nothing asks: a command the
// sandbox blocks fails. The boot file is piped in as the prompt's extra
// context, since Codex has no system-prompt flag; the kickoff is the prompt.
// The session is run under `timeout`, and an exit of 124 is read as its cap.
func (h *Harness) Session(l application.Launch) (application.SessionSpec, error) {
	if err := l.Validate(); err != nil {
		return application.SessionSpec{}, err
	}
	if l.Path.Harness != domain.HarnessCodex {
		return application.SessionSpec{}, fmt.Errorf("launching %s: this harness is %s, not %s", l.StoryID, domain.HarnessCodex, l.Path.Harness)
	}
	// A close-out alone runs After in the environment it runs in after a
	// harness, and nothing else.
	if l.CloseOutOnly {
		return h.spec(l, sessionshell.AfterLine(l)), nil
	}

	argv := []string{
		"timeout", strconv.Itoa(minutes(h.timeLimit)) + "m",
		h.program, "exec", "--json",
		"-m", string(l.Path.Model),
		"-c", "model_reasoning_effort=" + string(l.Path.Effort),
		"--sandbox", "workspace-write",
		"-c", "sandbox_workspace_write.network_access=true",
		"-c", "shell_environment_policy.inherit=all",
		"-a", "never",
		"-C", l.Dir,
		"-o", l.Dir + "/" + LastMessageFile,
		l.Kickoff,
	}
	if h.nice > 0 {
		argv = append([]string{"nice", "-n", strconv.Itoa(h.nice)}, argv...)
	}

	// The boot text is stdin of the harness's command, between its argv and the
	// result redirect; the rest is the same for every harness.
	line := sessionshell.ResultRedirect(argv, l.ResultFile)
	at := len(sessionshell.ShellLine(argv))
	line = line[:at] + " < " + sessionshell.ShellQuote(l.BootFile) + line[at:]
	line = sessionshell.WithBdShim(line, l.BdRetry)
	line = sessionshell.WithHeartbeat(line, l)
	line = sessionshell.WithAfter(line, l)
	return h.spec(l, line), nil
}

// minutes is d in whole minutes, rounded up, and at least one.
func minutes(d time.Duration) int {
	m := int((d + time.Minute - 1) / time.Minute)
	if m < 1 {
		return 1
	}
	return m
}

// spec is the session that runs line for the launch: the host's beads
// environment read ahead of it, and the seat's identity and the story in its
// environment.
func (h *Harness) spec(l application.Launch, line string) application.SessionSpec {
	line = sessionshell.Prelude(line, sessionshell.Env{File: h.envFile, ServerHost: h.serverHost})

	identity := application.SeatIdentity(l.Seat, l.Host)
	env := map[string]string{
		// Who beads records as the actor, and who the seat is for mail.
		"BEADS_ACTOR": identity,
		"MW_SEAT":     identity,
		"MW_STORY":    l.StoryID,
	}
	for name, value := range application.WorkerLimitEnv(h.jobs) {
		env[name] = value
	}
	for name, value := range application.TimingEnv(application.TimingSkip, h.timing[l.Path.Rig]) {
		env[name] = value
	}
	return application.SessionSpec{
		Name:    application.SessionName(l.StoryID),
		Dir:     l.Dir,
		Env:     env,
		Command: []string{h.shell, "-c", line},
	}
}
