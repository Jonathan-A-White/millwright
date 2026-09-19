// Package watch is the adapter mw watch reaches the world through: an HTTP
// reply from a URL, a `cat ~/.mw-health` over ssh, and the memory and log it
// keeps in a directory of its own on this host. Everything it does to the
// world is a read.
package watch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.WatchProbes = (*Probes)(nil)

// Timeout is how long any one probe may take: a URL to answer, ssh to print a
// file. A host that does not answer inside it is a host that does not answer.
const Timeout = 10 * time.Second

// The files mw watch keeps in its directory.
const (
	MemoryFile = "memory"
	LogFile    = "log"
)

// sshFailed is the status ssh itself leaves with when it could not do what it
// was asked: no connection, no authentication. The remote command's own status
// is any other number.
const sshFailed = 255

// Probes is the adapter. The zero value of everything but Dir is ready to use.
type Probes struct {
	// Dir is where the memory and the log are kept. It is made when first written
	// to.
	Dir string
	// Client makes the HTTP requests. Nil reads a client that does not follow
	// redirects, so that the first reply is the one that counts.
	Client *http.Client
	// SSH is the program run for ssh. Empty reads "ssh".
	SSH string
}

// New is the adapter keeping its memory and its log in dir.
func New(dir string) *Probes { return &Probes{Dir: dir} }

// Reach implements application.WatchProbes: a GET, of which only whether any
// reply came back is looked at, and nothing of the body is read.
func (p *Probes) Reach(ctx context.Context, url string) bool {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	reply, err := p.client().Do(request)
	if err != nil {
		return false
	}
	reply.Body.Close()
	return true
}

func (p *Probes) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// ReadHealth implements application.WatchProbes: exactly `cat ~/.mw-health` on
// the host, with BatchMode so that nothing is ever asked of a person, and
// Timeout to answer in. Only ssh failing is an error; a file that is missing
// there is the empty text, since ssh got through to say so.
func (p *Probes) ReadHealth(ctx context.Context, host string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	program := p.SSH
	if program == "" {
		program = "ssh"
	}
	out, err := exec.CommandContext(ctx, program,
		"-o", "BatchMode=yes",
		"-o", fmt.Sprintf("ConnectTimeout=%d", int(Timeout/time.Second)),
		"--", host,
		"cat "+application.WatchHealthFile).Output()
	if err == nil {
		return string(out), nil
	}

	var exited *exec.ExitError
	if ctx.Err() == nil && errors.As(err, &exited) && exited.ExitCode() != sshFailed && exited.ExitCode() > 0 {
		return "", nil
	}
	return "", fmt.Errorf("ssh %s: %w", host, err)
}

// LoadMemory implements application.WatchProbes. A memory that was never
// written, or was left empty, or holds something that is not a time, is the
// zero value: mw watch starts counting again, which costs it a few minutes and
// nothing else.
func (p *Probes) LoadMemory(context.Context) (application.WatchMemory, error) {
	said, err := os.ReadFile(filepath.Join(p.Dir, MemoryFile))
	if os.IsNotExist(err) {
		return application.WatchMemory{}, nil
	}
	if err != nil {
		return application.WatchMemory{}, fmt.Errorf("reading the watch memory: %w", err)
	}
	at, err := time.Parse(time.RFC3339, strings.TrimSpace(string(said)))
	if err != nil {
		return application.WatchMemory{}, nil
	}
	return application.WatchMemory{FirstFailure: at}, nil
}

// SaveMemory implements application.WatchProbes. Forgetting is removing the
// file.
func (p *Probes) SaveMemory(_ context.Context, memory application.WatchMemory) error {
	path := filepath.Join(p.Dir, MemoryFile)
	if memory.FirstFailure.IsZero() {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("forgetting in %s: %w", path, err)
		}
		return nil
	}
	if err := os.MkdirAll(p.Dir, 0o755); err != nil {
		return fmt.Errorf("making %s: %w", p.Dir, err)
	}
	// Made whole under another name and moved into place, so that a reader never
	// sees half a time.
	whole := path + ".new"
	if err := os.WriteFile(whole, []byte(memory.FirstFailure.UTC().Format(time.RFC3339)+"\n"), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", whole, err)
	}
	if err := os.Rename(whole, path); err != nil {
		return fmt.Errorf("moving %s into place: %w", whole, err)
	}
	return nil
}

// AppendLog implements application.WatchProbes: one line, added to the end.
func (p *Probes) AppendLog(_ context.Context, line string) error {
	if err := os.MkdirAll(p.Dir, 0o755); err != nil {
		return fmt.Errorf("making %s: %w", p.Dir, err)
	}
	path := filepath.Join(p.Dir, LogFile)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	if _, err := fmt.Fprintln(file, line); err != nil {
		file.Close()
		return fmt.Errorf("appending to %s: %w", path, err)
	}
	return file.Close()
}
