package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/suiteproc"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// nightlySuiteSpringJob is the job that runs the nightly whole suite: from
// suiteHour (UTC) each day, on the home only, `mw suite <rig> --file` for each
// of this host's rigs whose file says nightly_suite = true, one at a time. Each
// is a process of this binary of its own, apart from the follower (in a systemd
// scope where there is one): a follower restarted by a self-update leaves a suite
// running, and the one that comes back adopts it and tells its real outcome
// (mw-gq6.395). Its output goes to a log beside its state, under
// ~/.local/state/mw/suite.
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
	home, err := os.UserHomeDir()
	if err != nil {
		return application.SpringJob{}, fmt.Errorf("finding the home directory to keep the suite's state in: %w", err)
	}
	detached := application.DetachedSuite{Processes: &suiteproc.Processes{
		Dir: filepath.Join(home, ".local", "state", "mw", "suite"), Program: self,
	}}
	files := vault.New(dir)
	suite := &application.NightlySuite{
		Hour: suiteHour, Rigs: rigs, Rules: files, History: files,
		Home: func(ctx context.Context) (bool, error) {
			return application.IsHome(ctx, mwVault(dir, host), host)
		},
		Run: detached.Run,
	}
	return suite.Job(), nil
}
