package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// nightlySuiteSpringJob is the job that runs the nightly whole suite: from
// suiteHour (UTC) each day, on the home only, `mw suite <rig> --file` for each
// of this host's rigs whose file says nightly_suite = true, one at a time. A rig
// whose file names suite_every and suite_host is run on that host whenever main
// has moved and suite_every has passed, and by the home only when its newest
// result is over 26 hours old; while the home is busy, or holds its gate lock for a
// gate, the home's own nightly runs on another host with room instead
// (mw-gq6.1617). Each suite is a process of this binary of its
// own, apart from the follower (in a systemd scope where there is one): a follower restarted by a self-update leaves a suite
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
	detached := application.DetachedSuite{Processes: nightlyApart(self, home, application.NightlySuiteKind)}
	where, err := nightlyPlace(host, dir, out)
	if err != nil {
		return application.SpringJob{}, err
	}
	files := vault.New(dir)
	suite := &application.NightlySuite{
		Hour: suiteHour, Rigs: rigs, ReadRigs: config.Rigs, Rules: files, History: files,
		Host: host, Heads: rig.New(),
		Home: func(ctx context.Context) (bool, error) {
			return application.IsHome(ctx, mwVault(dir, host), host)
		},
		Run: detached.Run, Where: where,
	}
	return suite.Job(), nil
}
