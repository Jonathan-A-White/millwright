package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// nightlySuiteSpringJob is the job that runs the nightly whole suite: from
// suiteHour (UTC) each day, on the home only, `mw suite <rig> --file` for each
// of this host's rigs whose file says nightly_suite = true, one at a time. Each
// is a process of its own, this binary, so a suite that hangs or is killed
// takes only itself with it; its output goes where the follower's does.
func nightlySuiteSpringJob(host string, suiteHour int, out io.Writer) (application.SpringJob, error) {
	dir, err := config.Vault()
	if err != nil {
		return application.SpringJob{}, err
	}
	rigs, err := config.Rigs()
	if err != nil {
		return application.SpringJob{}, err
	}
	self, err := os.Executable()
	if err != nil {
		return application.SpringJob{}, fmt.Errorf("finding the mw binary to run the suite with: %w", err)
	}
	files := vault.New(dir)
	suite := &application.NightlySuite{
		Hour: suiteHour, Rigs: rigs, Rules: files, History: files,
		Home: func(ctx context.Context) (bool, error) {
			return application.IsHome(ctx, mwVault(dir, host), host)
		},
		Run: func(ctx context.Context, rig string) error {
			cmd := exec.CommandContext(ctx, self, "suite", rig, "--file")
			cmd.Stdout, cmd.Stderr = out, out
			return cmd.Run()
		},
	}
	return suite.Job(), nil
}
