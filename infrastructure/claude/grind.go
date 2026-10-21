package claude

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
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

// APIKeyEnv is the variable Claude Code reads an Anthropic API key from: when
// it is set the session is billed to that key, else to the login.
const APIKeyEnv = "ANTHROPIC_API_KEY"

// CacheTTLEnv is the variable Claude Code reads its prompt cache's life from:
// a talk's session sets it to application.GristTalkCacheTTL.
const CacheTTLEnv = "CLAUDE_CODE_PROMPT_CACHE_TTL"

// GrindEnv is the environment a grind's session runs in: environ without any
// ANTHROPIC_API_KEY, so the subscription login is used, and with the call's
// key when it carries one. The key travels here and nowhere else: never in the
// argument list. A talk's session (a call with a SessionID or a Resume) runs
// with the one-hour prompt cache, CacheTTLEnv in place of any environ had.
func GrindEnv(environ []string, call application.GrindCall) []string {
	talk := call.SessionID != "" || call.Resume != ""
	env := make([]string, 0, len(environ)+2)
	for _, entry := range environ {
		if strings.HasPrefix(entry, APIKeyEnv+"=") || talk && strings.HasPrefix(entry, CacheTTLEnv+"=") {
			continue
		}
		env = append(env, entry)
	}
	if call.APIKey != "" {
		env = append(env, APIKeyEnv+"="+call.APIKey)
	}
	if talk {
		env = append(env, CacheTTLEnv+"="+application.GristTalkCacheTTL)
	}
	return env
}

// SystemPromptPath is where the grind's instructions are kept for claude to read
// with --system-prompt-file: in the grind's own directory, under a name that
// holds a digest of the text. A talk's grinds share one directory, so two kinds
// (or two versions of a grind) with different instructions never meet in one
// file, and one claude never reads another's text half written.
func SystemPromptPath(call application.GrindCall) string {
	sum := sha256.Sum256([]byte(call.System))
	return filepath.Join(call.Dir, "system-prompt-"+hex.EncodeToString(sum[:8])+".md")
}

// WriteSystemPrompt writes call.System to SystemPromptPath, for the command
// line GrindArgs and WarmArgs give: whole or not at all, and readable by the
// owner only.
func WriteSystemPrompt(call application.GrindCall) error {
	if call.Dir == "" {
		return errors.New("the grind has no directory to keep its instructions in")
	}
	fail := func(err error) error {
		return fmt.Errorf("the grind's instructions could not be written to %s: %w", call.Dir, err)
	}
	tmp, err := os.CreateTemp(call.Dir, ".system-prompt-")
	if err != nil {
		return fail(err)
	}
	_, err = tmp.WriteString(call.System)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), SystemPromptPath(call))
	}
	if err != nil {
		os.Remove(tmp.Name())
		return fail(err)
	}
	removeOldSystemPrompts(call.Dir, SystemPromptPath(call), time.Now())
	return nil
}

// OldSystemPromptAge is how long the instructions of an older digest are kept:
// a grind or warm still running on them has read its file long before.
const OldSystemPromptAge = 24 * time.Hour

// removeOldSystemPrompts removes the system-prompt-<digest>.md files in dir other
// than current whose last write is over OldSystemPromptAge before now, so a
// directory does not gain a file for every change of instructions for good
// (mw-gq6.691). A failure is logged, never returned: the new instructions are
// already written.
func removeOldSystemPrompts(dir, current string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("the old instructions in %s could not be listed: %v", dir, err)
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "system-prompt-") || !strings.HasSuffix(name, ".md") {
			continue
		}
		path := filepath.Join(dir, name)
		if path == current {
			continue
		}
		info, err := entry.Info()
		if err != nil || now.Sub(info.ModTime()) <= OldSystemPromptAge {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("the old instructions %s could not be removed: %v", path, err)
		}
	}
}

// PromptArgMax is the longest prompt a grind carries as its last argument. One
// argument may be 131,072 bytes on Linux (MAX_ARG_STRLEN), and execve fails
// outright past it; a longer prompt (a talk's history, a long input) is sent on
// the session's standard input instead, which claude --print reads as the prompt
// (mw-gq6.690).
const PromptArgMax = 64 << 10

// PromptOnStdin says a grind's prompt goes on the session's standard input, as
// it is too long for an argument.
func PromptOnStdin(call application.GrindCall) bool { return len(call.Prompt) > PromptArgMax }

// GrindArgs is a grind's command line after the program, one argument each,
// never read by a shell: the prompt is the last, unless PromptOnStdin. The instructions are named by
// file (WriteSystemPrompt writes it), never carried in an argument. A talk's session is kept:
// --session-id starts it, --resume carries it on, each in the place
// --no-session-persistence has otherwise, and every other flag the same, so
// the resumed turn's prompt cache matches the first's.
func GrindArgs(call application.GrindCall) ([]string, error) {
	schema, err := GrindSchema(call.Schema)
	if err != nil {
		return nil, err
	}
	session := []string{"--no-session-persistence"}
	switch {
	case call.Resume != "":
		session = []string{"--resume", call.Resume}
	case call.SessionID != "":
		session = []string{"--session-id", call.SessionID}
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
	}
	args = append(args, session...)
	args = append(args,
		"--disable-slash-commands",
		// --safe-mode is claude's documented switch (claude --help: "all
		// customizations (CLAUDE.md, skills, plugins, hooks, MCP servers ...)
		// disabled"). --restricted ignores settings files but its help does
		// not promise it stops CLAUDE.md discovery in the directory's
		// ancestors or ~/.claude. --bare would too, but reads no OAuth login.
		"--safe-mode",
		// A file, never the text: one argv string is at most 131,072 bytes on
		// Linux, and a grind's instructions can pass it (mw-gq6.690).
		"--system-prompt-file", SystemPromptPath(call),
		"--json-schema", schema,
	)
	// The one limit a grind file may set. Nothing else is forbidden here: no
	// flag turns subagents off.
	if call.MaxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(call.MaxTurns))
	}
	if PromptOnStdin(call) {
		return args, nil
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
// standard input (the null device: claude otherwise waits for it) but a prompt
// too long for an argument (PromptOnStdin), runs in a
// process group of its own, and is killed with the whole group at the
// call's timeout. What it prints is its result JSON, read even when it
// exits non-zero: an error result says why.
func (g *Grinder) Grind(ctx context.Context, call application.GrindCall) (application.SessionResult, error) {
	args, err := GrindArgs(call)
	if err != nil {
		return application.SessionResult{}, err
	}
	if err := WriteSystemPrompt(call); err != nil {
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
	cmd.Env = GrindEnv(os.Environ(), call)
	cmd.Stdin = null
	if PromptOnStdin(call) {
		cmd.Stdin = strings.NewReader(call.Prompt)
	}
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
	if err := WriteSystemPrompt(call); err != nil {
		return nil, err
	}
	cmd := exec.Command(g.program, args...)
	cmd.Dir = call.Dir
	cmd.Env = GrindEnv(os.Environ(), call)
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
