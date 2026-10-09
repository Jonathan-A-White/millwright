// Package cloud makes and destroys the cloud boxes through the provider's
// wrapper: contrib/vultr-boost, run as a program, so that terraform, the
// tokens and the hub's peer stay the wrapper's business.
package cloud

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// DefaultLimit is how long one up or down may run before it is stopped: a
// box made from a snapshot is up in a few minutes, one bootstrapped in full in
// some more.
const DefaultLimit = 20 * time.Minute

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
	// Limit is how long an up or down may run; zero is DefaultLimit.
	Limit time.Duration
}

var _ application.CloudProvider = Vultr{}

// Up implements application.CloudProvider.
func (v Vultr) Up(ctx context.Context, name string) error {
	args := []string{"up", "--yes", "--name", name}
	if v.Snapshot != "" {
		args = append(args, "--snapshot", v.Snapshot)
	}
	_, err := v.run(ctx, v.limit(), args...)
	return err
}

// Down implements application.CloudProvider.
func (v Vultr) Down(ctx context.Context, name string) error {
	_, err := v.run(ctx, v.limit(), "down", "--yes", "--name", name)
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

func (v Vultr) limit() time.Duration {
	if v.Limit > 0 {
		return v.Limit
	}
	return DefaultLimit
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
			return "", fmt.Errorf("%s %s ran past %s", v.Command, args[0], limit)
		}
		last := lastLine(stderr.String())
		if last == "" {
			last = lastLine(stdout.String())
		}
		if last != "" {
			return "", fmt.Errorf("%s %s: %v: %s", v.Command, args[0], err, last)
		}
		return "", fmt.Errorf("%s %s: %w", v.Command, args[0], err)
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
