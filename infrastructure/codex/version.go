package codex

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrNotInstalled is what Version returns when the Codex program is not
// on this host. It is not a fault of the harness: a host that never starts a
// session with it does not need it.
var ErrNotInstalled = errors.New("codex is not installed")

// Version is the version Codex reports for itself (`codex --version`),
// as one line. The flags a session is started with are Codex's own, so which
// Codex a session will run is worth being able to see. It starts no session
// and spends no fuel.
func (h *Harness) Version(ctx context.Context) (string, error) {
	if _, err := exec.LookPath(h.program); err != nil {
		return "", ErrNotInstalled
	}

	printed, err := exec.CommandContext(ctx, h.program, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("running %s --version: %w", h.program, err)
	}
	version := strings.TrimSpace(string(printed))
	if version == "" {
		return "", fmt.Errorf("%s --version printed nothing", h.program)
	}
	return version, nil
}
