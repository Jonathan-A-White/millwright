package hostload

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// Uptime reads the host's `uptime` line: how long it has been up, who is on it
// and its load averages.
type Uptime struct {
	// Program is the uptime command; empty is uptime.
	Program string
}

var _ application.Uptimes = Uptime{}

// Uptime implements application.Uptimes.
func (u Uptime) Uptime(ctx context.Context) (string, error) {
	program := u.Program
	if program == "" {
		program = "uptime"
	}
	out, err := exec.CommandContext(ctx, program).Output()
	if err != nil {
		return "", fmt.Errorf("running %s: %w", program, err)
	}
	return strings.TrimSpace(string(out)), nil
}
