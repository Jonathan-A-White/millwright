// Package bdprogram runs the real bd program for application.BdRetry. It is the
// adapter behind application.BdProgram: it starts bd with the arguments given,
// in the environment of the session that asked, and hands back what bd printed
// and the status it left with.
package bdprogram

import (
	"bytes"
	"context"
	"errors"
	"os/exec"

	"github.com/Jonathan-A-White/millwright/application"
)

// Program runs the bd at Path.
type Program struct{ Path string }

// Run implements application.BdProgram. A bd that ran and failed is a BdRun
// with its status; only one that could not be started is an error.
func (p Program) Run(ctx context.Context, args []string) (application.BdRun, error) {
	cmd := exec.CommandContext(ctx, p.Path, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	run := application.BdRun{Stdout: stdout.String(), Stderr: stderr.String()}
	var exited *exec.ExitError
	switch {
	case err == nil:
		return run, nil
	case errors.As(err, &exited) && ctx.Err() == nil:
		run.Exit = exited.ExitCode()
		if run.Exit <= 0 {
			run.Exit = 1
		}
		return run, nil
	default:
		return run, err
	}
}
