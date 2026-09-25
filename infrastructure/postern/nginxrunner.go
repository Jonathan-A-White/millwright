package postern

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"

	"github.com/Jonathan-A-White/millwright/application"
)

// NginxRunner is the real application.PosternNginxRunner: `nginx -t` and
// `systemctl reload nginx`, run on this host.
type NginxRunner struct {
	// NginxProgram and SystemctlProgram name the programs Test and Reload
	// run. Empty reads "nginx" and "systemctl" off PATH; a test points them
	// at a stand-in script instead.
	NginxProgram     string
	SystemctlProgram string
}

var _ application.PosternNginxRunner = (*NginxRunner)(nil)

// NewNginxRunner returns the real nginx runner, reading nginx and systemctl
// off PATH.
func NewNginxRunner() *NginxRunner { return &NginxRunner{} }

// Test implements application.PosternNginxRunner.
func (r *NginxRunner) Test(ctx context.Context) (string, bool, error) {
	return posternRun(ctx, r.nginxProgram(), "-t")
}

// Reload implements application.PosternNginxRunner.
func (r *NginxRunner) Reload(ctx context.Context) (string, error) {
	out, ok, err := posternRun(ctx, r.systemctlProgram(), "reload", "nginx")
	if err != nil {
		return out, err
	}
	if !ok {
		return out, fmt.Errorf("systemctl reload nginx failed:\n%s", out)
	}
	return out, nil
}

func (r *NginxRunner) nginxProgram() string {
	if r.NginxProgram != "" {
		return r.NginxProgram
	}
	return "nginx"
}

func (r *NginxRunner) systemctlProgram() string {
	if r.SystemctlProgram != "" {
		return r.SystemctlProgram
	}
	return "systemctl"
}

// posternRun runs one command to completion, reporting its combined output
// and whether it exited zero. A command that ran and exited nonzero is an
// answer, not an error — the error is a command that could not be run at
// all, such as nginx or systemctl not being on this host's PATH.
func posternRun(ctx context.Context, name string, args ...string) (string, bool, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if err == nil {
		return out.String(), true, nil
	}
	var exited *exec.ExitError
	if errors.As(err, &exited) {
		return out.String(), false, nil
	}
	return out.String(), false, fmt.Errorf("running %s: %w", name, err)
}
