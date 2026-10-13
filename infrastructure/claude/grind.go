package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// DefaultGrindTimeout is how long a grind may run when its call says
// nothing: config [grist] timeout's own default.
const DefaultGrindTimeout = application.DefaultGristTimeout

// GrindTools is the one tool a grind may use: Read, confined by
// --restricted to the grind's own private directory.
const GrindTools = "Read"

// Grinder runs a grind as one Claude Code session, synchronously, with no
// seat and no tmux: the adapter behind application.Grinder. The session
// runs in the grind's private directory, may Read the photos there and
// nothing else, runs no command, loads no MCP server, skill, settings file,
// CLAUDE.md or saved session, asks nobody anything, and answers only through a
// structured output held to the grind's schema.
type Grinder struct {
	program string
}

// Grinder satisfies the ports.
var (
	_ application.Grinder     = (*Grinder)(nil)
	_ application.WarmGrinder = (*Grinder)(nil)
)

// GrinderOption is a setting of a Grinder.
type GrinderOption func(*Grinder)

// WithGrindProgram names the claude command to run: a test's stand-in.
func WithGrindProgram(program string) GrinderOption {
	return func(g *Grinder) { g.program = program }
}

// NewGrinder is the grinder that runs Program.
func NewGrinder(opts ...GrinderOption) *Grinder {
	g := &Grinder{program: Program}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// GrindArgs is a grind's command line after the program, one argument each,
// never read by a shell: the prompt is the last.
func GrindArgs(call application.GrindCall) ([]string, error) {
	schema, err := GrindSchema(call.Schema)
	if err != nil {
		return nil, err
	}
	args := []string{
		"--print",
		"--output-format", "json",
		"--model", call.Model,
		"--effort", call.Effort,
		"--restricted",
		"--tools", GrindTools,
		"--strict-mcp-config",
		"--permission-prompts", "none",
		"--no-session-persistence",
		"--disable-slash-commands",
		// --safe-mode is claude's documented switch (claude --help: "all
		// customizations (CLAUDE.md, skills, plugins, hooks, MCP servers ...)
		// disabled"). --restricted ignores settings files but its help does
		// not promise it stops CLAUDE.md discovery in the directory's
		// ancestors or ~/.claude. --bare would too, but reads no OAuth login.
		"--safe-mode",
		"--system-prompt", call.System,
		"--json-schema", schema,
	}
	// The one limit a grind file may set. Nothing else is forbidden here: no
	// flag turns subagents off.
	if call.MaxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(call.MaxTurns))
	}
	return append(args, call.Prompt), nil
}

// GrindSchema is the grind's answer schema as --json-schema takes it: its
// top-level $schema and $id left out, since claude refuses a schema that
// names a draft by $schema ("no schema with key or ref"), and compact. Its
// $defs and every $ref are kept.
func GrindSchema(schema string) (string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(schema), &fields); err != nil || fields == nil {
		return "", fmt.Errorf("the grind's answer schema is not a JSON object: %v", err)
	}
	delete(fields, "$schema")
	delete(fields, "$id")
	encoded, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, encoded); err != nil {
		return "", err
	}
	return compact.String(), nil
}

// Grind implements application.Grinder. The session reads nothing from its
// standard input (the null device: claude otherwise waits for it), runs in a
// process group of its own, and is killed with the whole group at the
// call's timeout. What it prints is its result JSON, read even when it
// exits non-zero: an error result says why.
func (g *Grinder) Grind(ctx context.Context, call application.GrindCall) (application.SessionResult, error) {
	args, err := GrindArgs(call)
	if err != nil {
		return application.SessionResult{}, err
	}
	timeout := call.Timeout
	if timeout <= 0 {
		timeout = DefaultGrindTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	null, err := os.Open(os.DevNull)
	if err != nil {
		return application.SessionResult{}, err
	}
	defer null.Close()

	cmd := exec.CommandContext(ctx, g.program, args...)
	cmd.Dir = call.Dir
	cmd.Stdin = null
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	runErr := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return application.SessionResult{}, fmt.Errorf("%w: %s gave up after %s", application.ErrGrindTimedOut, g.program, timeout)
	}
	if ctx.Err() != nil {
		return application.SessionResult{}, fmt.Errorf("%s: %w", g.program, ctx.Err())
	}
	result, readErr := application.ReadSessionResult(out.String())
	if readErr != nil {
		if runErr != nil {
			return application.SessionResult{}, fmt.Errorf("%s: %w%s", g.program, runErr, lastWords(errs.String()))
		}
		return application.SessionResult{}, fmt.Errorf("%s: %w", g.program, readErr)
	}
	if runErr != nil {
		result.IsError = true
	}
	return result, nil
}

// lastWords is the last line a command wrote to its standard error, set
// off for an error message, or nothing.
func lastWords(said string) string {
	lines := strings.Split(strings.TrimSpace(said), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1]); last != "" {
		return ": " + last
	}
	return ""
}

// WarmArgs is a warm session's command line after the program: GrindArgs'
// flags, every one and with the same system prompt and schema, but no prompt.
// The session takes its one message on its standard input as a stream-json
// line, which claude allows only with stream-json output (--verbose with it),
// and prints its result as the last line of that stream.
func WarmArgs(call application.GrindCall) ([]string, error) {
	call.Prompt = ""
	cold, err := GrindArgs(call)
	if err != nil {
		return nil, err
	}
	cold = cold[:len(cold)-1]
	args := make([]string, 0, len(cold)+4)
	for i := 0; i < len(cold); i++ {
		if cold[i] == "--output-format" && i+1 < len(cold) {
			args = append(args, "--output-format", "stream-json", "--input-format", "stream-json", "--verbose")
			i++
			continue
		}
		args = append(args, cold[i])
	}
	return args, nil
}

// Warm implements application.WarmGrinder. The session is started at once, in
// call.Dir and a process group of its own, and waits on its standard input:
// claude loads everything it loads before it reads its message there, and
// asks the model nothing until it has one. Its input closed with no message,
// it ends having asked nothing.
func (g *Grinder) Warm(call application.GrindCall) (application.WarmSession, error) {
	args, err := WarmArgs(call)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(g.program, args...)
	cmd.Dir = call.Dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	s := &warmSession{program: g.program, cmd: cmd, stdin: stdin, done: make(chan struct{})}
	cmd.Stdout, cmd.Stderr = &s.out, &s.errs
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%s: %w", g.program, err)
	}
	go func() {
		s.runErr = cmd.Wait()
		close(s.done)
	}()
	return s, nil
}

// warmSession is one warm grind session: a claude process waiting for its one
// message.
type warmSession struct {
	program string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	out     resultLine
	errs    bytes.Buffer
	// done is closed once the process has ended and runErr is set.
	done   chan struct{}
	runErr error
	given  sync.Once
}

// streamMessage is the one user message a warm session is given, as claude's
// --input-format stream-json reads it.
type streamMessage struct {
	Type    string `json:"type"`
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
}

// Answer implements application.WarmSession: it writes the prompt as one
// stream-json user message and closes the session's input, so that it ends
// once it has answered, then waits for it to end. It is killed, with
// everything it started, at timeout or when ctx ends. What it printed last
// that is a result is read, even when it exits non-zero: an error result says
// why.
func (s *warmSession) Answer(ctx context.Context, prompt string, timeout time.Duration) (application.SessionResult, error) {
	if timeout <= 0 {
		timeout = DefaultGrindTimeout
	}
	var message streamMessage
	message.Type, message.Message.Role, message.Message.Content = "user", "user", prompt
	line, err := json.Marshal(message)
	if err != nil {
		s.Retire()
		return application.SessionResult{}, err
	}
	// The write may block on a session that reads nothing; it ends when the
	// session is killed, and an error is the session having ended already.
	s.given.Do(func() {
		go func() {
			s.stdin.Write(append(line, '\n'))
			s.stdin.Close()
		}()
	})
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-s.done:
	case <-timer.C:
		s.kill()
		return application.SessionResult{}, fmt.Errorf("%w: %s gave up after %s", application.ErrGrindTimedOut, s.program, timeout)
	case <-ctx.Done():
		s.kill()
		return application.SessionResult{}, fmt.Errorf("%s: %w", s.program, ctx.Err())
	}
	result, readErr := application.ReadSessionResult(s.out.last())
	if readErr != nil {
		if s.runErr != nil {
			return application.SessionResult{}, fmt.Errorf("%s: %w%s", s.program, s.runErr, lastWords(s.errs.String()))
		}
		return application.SessionResult{}, fmt.Errorf("%s: %w", s.program, readErr)
	}
	if s.runErr != nil {
		result.IsError = true
	}
	return result, nil
}

// Exited implements application.WarmSession.
func (s *warmSession) Exited() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

// Retire implements application.WarmSession: the session is killed with
// everything it started, and has been given nothing.
func (s *warmSession) Retire() {
	s.given.Do(func() { s.stdin.Close() })
	s.kill()
}

// kill ends the session's whole process group and waits for it to be gone.
// A session already ended is not signalled: its group's number may be
// another's by now.
func (s *warmSession) kill() {
	if s.Exited() {
		return
	}
	syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
	<-s.done
}

// resultLine keeps, of everything a stream-json session prints, only the last
// whole line that is its result: the rest (each turn, every photo it read)
// may run to megabytes and is not wanted.
type resultLine struct {
	mu      sync.Mutex
	partial []byte
	result  []byte
}

// Write implements io.Writer.
func (r *resultLine) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.partial = append(r.partial, p...)
	for {
		end := bytes.IndexByte(r.partial, '\n')
		if end < 0 {
			break
		}
		r.keep(r.partial[:end])
		r.partial = r.partial[end+1:]
	}
	if len(r.partial) == 0 {
		r.partial = nil
	}
	return len(p), nil
}

// keep keeps line when it is a result. r.mu is held.
func (r *resultLine) keep(line []byte) {
	if !bytes.Contains(line, []byte(`"result"`)) {
		return
	}
	var head struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(line, &head) == nil && head.Type == "result" {
		r.result = append(r.result[:0], line...)
	}
}

// last is the last result line printed, the unfinished last line counted.
func (r *resultLine) last() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.partial) > 0 {
		r.keep(r.partial)
		r.partial = nil
	}
	return string(r.result)
}
