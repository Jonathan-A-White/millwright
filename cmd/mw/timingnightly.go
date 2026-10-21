package main

import (
	"context"
	"io"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostload"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// nightlyTimingSpringJob is the job that runs the timing tests of the rigs
// whose file says timing_tests (docs/timing-tests.md): from suiteHour (UTC)
// each day, on the home only, after the night's suite, for each of this host's
// rigs where a file a tagged test covers has changed since the last night they
// passed. It runs in the follower itself, under the host's gate lock and after
// the host has calmed; the result is kept under runs/timing/<rig>/ in the vault
// and each failing file is filed as one held [bug] under the rig's suite_epic.
func nightlyTimingSpringJob(host string, suiteHour int, out io.Writer) (application.SpringJob, error) {
	dir, err := config.Vault()
	if err != nil {
		return application.SpringJob{}, err
	}
	rigs, err := config.Rigs()
	if err != nil {
		return application.SpringJob{}, err
	}
	tests, err := config.Tests()
	if err != nil {
		return application.SpringJob{}, err
	}
	nice, err := config.BuilderNice()
	if err != nil {
		return application.SpringJob{}, err
	}
	jobs, err := config.BuilderJobs()
	if err != nil {
		return application.SpringJob{}, err
	}
	files := vault.New(dir)
	worktrees := rig.New()
	gateway := mwGateway(dir, host)
	timing := &application.NightlyTiming{
		Hour: suiteHour, Rigs: rigs, Tests: tests,
		Rules: files, Suites: files, History: files, Records: files, Files: mwVault(dir, host),
		Source: worktrees, Trees: worktrees, Heads: worktrees,
		Runner: rig.NewChecks(rig.WithNice(nice), rig.WithJobs(jobs)),
		Uptime: hostload.Uptime{},
		Filer:  application.TimingFiler{Tracker: gateway, Rules: files},
		Load:   hostload.Proc{}, GateLock: hostGateLock(),
		Home: func(ctx context.Context) (bool, error) {
			return application.IsHome(ctx, mwVault(dir, host), host)
		},
		Host: host, Out: out,
	}
	return timing.Job(), nil
}
