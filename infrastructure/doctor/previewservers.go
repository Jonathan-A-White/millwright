package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// PreviewServers is the real application.PreviewServers: it finds the
// `vite preview` and `npm run preview` processes in /proc, with the directory
// each runs in (mw-i80dx.12).
type PreviewServers struct {
	// ProcDir is the directory the processes are read from; empty reads "/proc".
	ProcDir string
	// Signal sends a signal to a pid; nil is syscall.Kill.
	Signal func(pid int, sig syscall.Signal) error
	// Patience is how long a stopped process is given to go before it is killed
	// outright; zero is three seconds.
	Patience time.Duration
}

var _ application.PreviewServers = PreviewServers{}

func (p PreviewServers) proc() string {
	if p.ProcDir != "" {
		return p.ProcDir
	}
	return "/proc"
}

// List implements application.PreviewServers.
func (p PreviewServers) List(context.Context) ([]application.PreviewServer, error) {
	entries, err := os.ReadDir(p.proc())
	if err != nil {
		return nil, err
	}
	var servers []application.PreviewServer
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if server, ok := p.read(pid); ok {
			servers = append(servers, server)
		}
	}
	return servers, nil
}

// read says the process pid as a preview server; ok is false when it is not
// one, has gone, or its directory cannot be read (another user's).
func (p PreviewServers) read(pid int) (application.PreviewServer, bool) {
	dir := filepath.Join(p.proc(), strconv.Itoa(pid))
	raw, err := os.ReadFile(filepath.Join(dir, "cmdline"))
	if err != nil || len(raw) == 0 {
		return application.PreviewServer{}, false
	}
	args := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
	tokens, ok := previewTokens(args)
	if !ok {
		return application.PreviewServer{}, false
	}
	cwd, err := os.Readlink(filepath.Join(dir, "cwd"))
	if err != nil {
		return application.PreviewServer{}, false
	}
	deleted := strings.HasSuffix(cwd, " (deleted)")
	return application.PreviewServer{
		Pid:        pid,
		Port:       portOf(tokens),
		Cwd:        strings.TrimSuffix(cwd, " (deleted)"),
		CwdDeleted: deleted,
		Cmd:        strings.Join(args, " "),
	}, true
}

// previewTokens says whether a command line starts a preview server, and gives
// the words it is made of. Those are: vite run with the word preview, and a
// package manager run with the word preview (`npm run preview`), each also as
// the string a shell was given with -c. A word of prose in a longer argument
// is not a command, so only a shell's -c string is split into words.
func previewTokens(args []string) ([]string, bool) {
	tokens := args
	if len(args) > 2 && isOneOf(filepath.Base(args[0]), "sh", "bash", "dash") && args[1] == "-c" {
		tokens = append(append([]string{}, args[:2]...), strings.Fields(args[2])...)
		tokens = append(tokens, args[3:]...)
	}
	preview := false
	for _, t := range tokens[1:] {
		if t == "preview" {
			preview = true
		}
	}
	if !preview {
		return nil, false
	}
	manager := false
	for _, t := range tokens {
		switch filepath.Base(t) {
		case "vite", "vite.js":
			return tokens, true
		case "npm", "npx", "pnpm", "yarn":
			manager = true
		}
	}
	return tokens, manager && containsWord(tokens, "run", "exec")
}

func isOneOf(s string, options ...string) bool { return containsWord(options, s) }

func containsWord(words []string, wants ...string) bool {
	for _, w := range words {
		for _, want := range wants {
			if w == want {
				return true
			}
		}
	}
	return false
}

// portOf reads the port from `--port N` or `--port=N`; 0 when none is named.
func portOf(tokens []string) int {
	for i, t := range tokens {
		value := ""
		switch {
		case t == "--port" && i+1 < len(tokens):
			value = tokens[i+1]
		case strings.HasPrefix(t, "--port="):
			value = strings.TrimPrefix(t, "--port=")
		default:
			continue
		}
		if port, err := strconv.Atoi(value); err == nil {
			return port
		}
	}
	return 0
}

// Stop implements application.PreviewServers: it stops the process only while
// it is still the preview server it was listed as (a pid is reused), asks it to
// end and, when it has not gone in the time given, kills it.
func (p PreviewServers) Stop(_ context.Context, server application.PreviewServer) error {
	now, ok := p.read(server.Pid)
	if !ok || now.Cmd != server.Cmd || now.Cwd != server.Cwd {
		return nil
	}
	signal := p.Signal
	if signal == nil {
		signal = func(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) }
	}
	patience := p.Patience
	if patience == 0 {
		patience = 3 * time.Second
	}
	if err := signal(server.Pid, syscall.SIGTERM); err != nil {
		return stopError(server.Pid, err)
	}
	deadline := time.Now().Add(patience)
	for time.Now().Before(deadline) {
		if p.gone(server) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	if p.gone(server) {
		return nil
	}
	return stopError(server.Pid, signal(server.Pid, syscall.SIGKILL))
}

// gone says the process is no longer the preview server it was.
func (p PreviewServers) gone(server application.PreviewServer) bool {
	now, ok := p.read(server.Pid)
	return !ok || now.Cmd != server.Cmd
}

// stopError reads a failed signal: a process already gone is a stop done.
func stopError(pid int, err error) error {
	if err == nil || err == syscall.ESRCH {
		return nil
	}
	return fmt.Errorf("stopping %d: %w", pid, err)
}
