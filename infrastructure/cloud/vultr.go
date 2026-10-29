// Package cloud makes and destroys the cloud boxes through the provider's
// wrapper: contrib/vultr-boost, run as a program, so that terraform, the
// tokens and the hub's peer stay the wrapper's business.
package cloud

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
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

// downLimit is how long one down or reap may run before it is stopped. The
// limit of an up is the config's ([cloud] up_minutes, Vultr.UpLimit): a
// snapshot restore took about 50 minutes on 2026-10-15, where 20 were
// allowed, so it is never a constant here.
const downLimit = 20 * time.Minute

// setCapLimit is how long setting a box's cap may take: two ssh calls, the hub's
// and the box's, each with a connect timeout of 15 seconds.
const setCapLimit = 2 * time.Minute

// errRanPast is a run stopped at its limit.
var errRanPast = errors.New("ran past its limit")

// billedLimit is how long asking the billing may take.
const billedLimit = time.Minute

// Unknown is what `vultr-boost spend` prints when Vultr's billing could not
// be read.
const Unknown = "unknown"

// Vultr is application.CloudProvider over contrib/vultr-boost: `up --yes
// --name <box> [--snapshot <id>]`, `down --yes --name <box>` and `spend
// --name <box>...`.
type Vultr struct {
	// Command is the wrapper's full path.
	Command string
	// Snapshot is the snapshot a box is made from; empty bootstraps it from
	// Ubuntu.
	Snapshot string
	// Out is where the wrapper's output is copied. Nil drops it.
	Out io.Writer
	// UpLimit is how long an up may run, [cloud] up_minutes. It is the limit
	// of the process Begin starts, never of a caller. Zero is a fault: an up
	// is refused rather than given a limit of the adapter's own.
	UpLimit time.Duration
	// Self is the mw binary Begin runs as `mw cloud make <name>`; MakeLog is
	// the file that process appends its output to. NoScope keeps it out of a
	// systemd scope of its own, which it is put in where systemd-run and a
	// user manager are there.
	Self    string
	MakeLog string
	NoScope bool
}

var _ application.CloudProvider = Vultr{}

// Begin implements application.CloudProvider: it starts `mw cloud make <name>`
// in a session of its own (and a systemd scope, where there is a user
// manager, so that stopping the caller's unit does not take it), not tied to
// ctx or to the caller's process group. A caller that is killed 10 seconds
// into the apply leaves the apply running.
func (v Vultr) Begin(_ context.Context, name string) error {
	if v.Self == "" {
		return fmt.Errorf("no mw to make the box with")
	}
	args := []string{v.Self, "cloud", "make", name}
	if scope := v.scope(); scope != "" {
		args = append([]string{scope, "--user", "--scope", "--collect", "--quiet"}, args...)
	}
	// Not CommandContext: the make is meant to outlive the context it was
	// begun in.
	cmd := exec.Command(args[0], args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if v.MakeLog != "" {
		if err := os.MkdirAll(filepath.Dir(v.MakeLog), 0o755); err != nil {
			return fmt.Errorf("making the directory of the log %s: %w", v.MakeLog, err)
		}
		log, err := os.OpenFile(v.MakeLog, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
		if err != nil {
			return fmt.Errorf("opening the log %s: %w", v.MakeLog, err)
		}
		defer log.Close()
		cmd.Stdout, cmd.Stderr = log, log
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting the make of %s: %w", name, err)
	}
	// Nobody waits for it: let go rather than leave a zombie of this process.
	return cmd.Process.Release()
}

// scope is the systemd-run to put the make in a scope with, or "".
func (v Vultr) scope() string {
	if v.NoScope {
		return ""
	}
	path, err := exec.LookPath("systemd-run")
	if err != nil {
		return ""
	}
	// A host without a user manager has systemd-run and nothing for it to ask.
	if out, err := exec.Command("systemctl", "--user", "is-system-running").Output(); err != nil && len(out) == 0 {
		return ""
	}
	return path
}

// Up implements application.CloudProvider.
func (v Vultr) Up(ctx context.Context, name string) error {
	if v.UpLimit <= 0 {
		return fmt.Errorf("no up limit: set up_minutes in the [cloud] table")
	}
	args := []string{"up", "--yes", "--name", name}
	if v.Snapshot != "" {
		args = append(args, "--snapshot", v.Snapshot)
	}
	_, err := v.run(ctx, v.UpLimit, args...)
	if errors.Is(err, errRanPast) {
		// What became of the box is Vultr's to say, not this exit's.
		return fmt.Errorf("%w: %v", application.ErrCloudUpDeadline, err)
	}
	return err
}

// Down implements application.CloudProvider.
func (v Vultr) Down(ctx context.Context, name string) error {
	_, err := v.run(ctx, downLimit, "down", "--yes", "--name", name)
	return err
}

// Billed implements application.CloudProvider: what `spend` prints, a number
// of dollars, or Unknown when Vultr's billing could not be read.
func (v Vultr) Billed(ctx context.Context, names []string) (float64, bool, error) {
	args := []string{"spend"}
	for _, name := range names {
		args = append(args, "--name", name)
	}
	out, err := v.run(ctx, billedLimit, args...)
	if err != nil {
		return 0, false, err
	}
	said := strings.TrimSpace(lastLine(out))
	if said == Unknown {
		return 0, false, nil
	}
	usd, err := strconv.ParseFloat(said, 64)
	if err != nil {
		return 0, false, fmt.Errorf("%s spend printed %q, not a number of dollars", v.Command, said)
	}
	return usd, true, nil
}

// Instances implements application.CloudProvider: `list` prints a line for each
// instance Vultr holds, its id, label, status and server status separated by tabs; a line
// without a label is not a box of ours and is dropped.
func (v Vultr) Instances(ctx context.Context) ([]application.CloudInstance, error) {
	out, err := v.run(ctx, billedLimit, "list")
	if err != nil {
		return nil, err
	}
	var found []application.CloudInstance
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), "\t")
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		if len(fields) < 2 || fields[0] == "" || fields[1] == "" {
			continue
		}
		inst := application.CloudInstance{ID: fields[0], Label: fields[1]}
		if len(fields) > 2 {
			inst.Status = fields[2]
		}
		if len(fields) > 3 {
			inst.Server = fields[3]
		}
		found = append(found, inst)
	}
	return found, nil
}

// Reap implements application.CloudProvider: `down --yes --name <label>
// --instance <id>`, which destroys what the box's state holds, if it holds
// anything, and the instance by its id, and takes the peer off the hub.
func (v Vultr) Reap(ctx context.Context, instance application.CloudInstance) error {
	_, err := v.run(ctx, downLimit, "down", "--yes", "--name", instance.Label, "--instance", instance.ID)
	return err
}

// Adopt implements application.CloudProvider: `adopt --yes --name <box>
// --instance <id>` imports the instance into the box's terraform state, so
// that down finds it.
func (v Vultr) Adopt(ctx context.Context, name string, instance application.CloudInstance) error {
	_, err := v.run(ctx, downLimit, "adopt", "--yes", "--name", name, "--instance", instance.ID)
	return err
}

// SetCap implements application.CloudProvider: `set-cap --name <box> --cap
// <n>`, which reads the box's WireGuard address off the hub and writes the cap
// into the config of the box over ssh.
func (v Vultr) SetCap(ctx context.Context, name string, cap int) error {
	_, err := v.run(ctx, setCapLimit, "set-cap", "--name", name, "--cap", strconv.Itoa(cap))
	return err
}

// addressLimit is how long reading a box's address may take: one ssh to the hub,
// with a connect timeout of 15 seconds.
const addressLimit = time.Minute

// Address implements application.CloudProvider: `address --name <box>`, which
// reads the box's WireGuard address off the hub and prints it, and then the
// user the box dispatches as (mw when it prints none).
func (v Vultr) Address(ctx context.Context, name string) (application.CloudAddress, error) {
	// What it prints is the answer, not progress to be copied to Out.
	v.Out = nil
	out, err := v.run(ctx, addressLimit, "address", "--name", name)
	if err != nil {
		return application.CloudAddress{}, err
	}
	lines := strings.Fields(out)
	if len(lines) == 0 || net.ParseIP(lines[0]) == nil {
		return application.CloudAddress{}, fmt.Errorf("%s address printed %q, not an address", v.Command, strings.TrimSpace(out))
	}
	user := "mw"
	if len(lines) > 1 {
		user = lines[1]
	}
	return application.CloudAddress{User: user, Host: lines[0]}, nil
}

// run runs the wrapper, copying its output to Out, and returns its standard
// output; a failure names the last line of its standard error, where the
// wrapper says why it stopped, else of its output.
func (v Vultr) run(ctx context.Context, limit time.Duration, args ...string) (string, error) {
	if v.Command == "" {
		return "", fmt.Errorf("no cloud command: set command in the [cloud] table")
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, v.Command, args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if v.Out != nil {
		// The two pipes are copied in goroutines of their own: one lock keeps
		// their lines whole in Out.
		out := &lockedWriter{w: v.Out}
		cmd.Stdout, cmd.Stderr = io.MultiWriter(&stdout, out), io.MultiWriter(&stderr, out)
	}
	cmd.WaitDelay = 10 * time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("%s %s %w (%s)", v.Command, args[0], errRanPast, limit)
		}
		last := lastLine(stderr.String())
		if last == "" {
			last = lastLine(stdout.String())
		}
		failure := fmt.Errorf("%s %s: %w", v.Command, args[0], err)
		if last != "" {
			failure = fmt.Errorf("%s %s: %v: %s", v.Command, args[0], err, last)
		}
		// Terraform says what Vultr refused on standard error, a few lines
		// above the wrapper's last: a refusal no retry mends is named so.
		return "", application.ClassifyCloudError(failure, stderr.String())
	}
	return stdout.String(), nil
}

// lockedWriter is a writer two goroutines may share.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
