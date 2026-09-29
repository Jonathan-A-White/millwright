package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
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

// Grinder satisfies the port.
var _ application.Grinder = (*Grinder)(nil)

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
	return []string{
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
		call.Prompt,
	}, nil
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
