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
// the host has calmed (on another host with room while the home is busy); the result is kept under runs/timing/<rig>/ in the vault
// and each failing file is filed as one held [bug] under the rig's suite_epic.
func nightlyTimingSpringJob(host string, suiteHour int, out io.Writer) (application.SpringJob, error) {
	timing, err := newNightlyTiming(host, suiteHour, out)
	if err != nil {
		return application.SpringJob{}, err
	}
	dir, err := config.Vault()
	if err != nil {
		return application.SpringJob{}, err
	}
	// While the home is busy, or holds its gate lock for a gate, a rig's run is made
	// by mw timing <rig> on another host with room (mw-gq6.1617).
	if timing.Where, err = nightlyPlace(host, dir, out); err != nil {
		return application.SpringJob{}, err
	}
	return timing.Job(), nil
}

// newNightlyTiming is the timing run as this host makes it, with no place to
// hand a run to: the follower's job adds one, and mw timing <rig> never does.
func newNightlyTiming(host string, suiteHour int, out io.Writer) (*application.NightlyTiming, error) {
	dir, err := config.Vault()
	if err != nil {
		return nil, err
	}
	rigs, err := config.Rigs()
	if err != nil {
		return nil, err
	}
	tests, err := config.Tests()
	if err != nil {
		return nil, err
	}
	nice, err := config.BuilderNice()
	if err != nil {
		return nil, err
	}
	jobs, err := config.BuilderJobs()
	if err != nil {
		return nil, err
	}
	testRuns, err := hostTestRuns()
	if err != nil {
		return nil, err
	}
	files := vault.New(dir)
	worktrees := rig.New()
	gateway := mwGateway(dir, host)
	return &application.NightlyTiming{
		Hour: suiteHour, Rigs: rigs, Tests: tests,
		ReadConfig: func() (map[string]string, map[string]string, error) {
			rigs, err := config.Rigs()
			if err != nil {
				return nil, nil, err
			}
			tests, err := config.Tests()
			return rigs, tests, err
		},
		Rules: files, Suites: files, History: files, Records: files, Files: mwVault(dir, host),
		Source: worktrees, Trees: worktrees, Heads: worktrees,
		Runner: rig.NewChecks(rig.WithNice(nice), rig.WithJobs(jobs), rig.WithTestRuns(testRuns)),
		Uptime: hostload.Uptime{},
		Filer:  application.TimingFiler{Tracker: gateway, Rules: files},
		Load:   hostload.Proc{}, GateLock: hostGateLock(),
		Home: func(ctx context.Context) (bool, error) {
			return application.IsHome(ctx, mwVault(dir, host), host)
		},
		Host: host, Out: out,
	}, nil
}
