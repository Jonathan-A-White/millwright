package main

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/eventlog"
	"github.com/Jonathan-A-White/millwright/infrastructure/grist"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/userunits"
)

// springUnits is the user manager the follower starts its jobs' units in. A
// test replaces it.
var springUnits userUnits = userunits.Systemctl{}

// userUnits is what the follower asks of the user manager.
type userUnits interface {
	Installed(ctx context.Context, unit string) bool
	Start(ctx context.Context, unit string) error
}

// The units the follower's jobs run, each the timer's own service unchanged.
const (
	dispatchUnit     = "mw-dispatch.service"
	millhandTickUnit = "mw-millhand-tick.service"
	mailNotifyUnit   = "mw-mail-notify.service"
)

// homeSpring is the follower's springer for the log in the file path: the
// dispatch pass, the Millhand's tick and the sync job of mail-notify, each
// only when its unit is installed here (a host with none is told on out, and
// left to its timers), the grist job where the config has a [grist] table, the
// chain-stamp job, the To do reminder job, the To do rank job and the weekly digest job, which run in
// the follower itself and so are always there, and the cloud job where the config has a [cloud] table.
func homeSpring(path, host string, out io.Writer) (*application.EventSpring, error) {
	knobs, err := config.Events()
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	runs := func(unit string) func(context.Context) error {
		return func(ctx context.Context) error { return springUnits.Start(ctx, unit) }
	}
	var jobs []application.SpringJob
	for _, j := range []struct {
		unit string
		job  application.SpringJob
	}{
		{dispatchUnit, application.DispatchJob(knobs.Heartbeat, runs(dispatchUnit))},
		{millhandTickUnit, application.MillhandTickJob(knobs.Heartbeat, runs(millhandTickUnit))},
		{mailNotifyUnit, application.ClockJob("mail-notify", knobs.Clock, runs(mailNotifyUnit))},
	} {
		if !springUnits.Installed(ctx, j.unit) {
			fmt.Fprintf(out, "mw events follow: %s is not installed here, so the %s job is not sprung (sh scripts/install-units.sh)\n", j.unit, j.job.Name)
			continue
		}
		jobs = append(jobs, j.job)
	}
	if job, ok, err := gristSpringJob(host, out); err != nil {
		fmt.Fprintf(out, "mw events follow: no grist job: %v\n", err)
	} else if ok {
		jobs = append(jobs, job)
	}
	if job, ok, err := gristLevelSpringJob(host, knobs.Heartbeat, out); err != nil {
		fmt.Fprintf(out, "mw events follow: no grist-level job: %v\n", err)
	} else if ok {
		jobs = append(jobs, job)
	}
	jobs = append(jobs, application.ChainStampJob(chainStampEvery, chainStampRun(out)))
	if job, err := beadsFlushSpringJob(host, out); err != nil {
		fmt.Fprintf(out, "mw events follow: no beads-flush job: %v\n", err)
	} else {
		jobs = append(jobs, job)
	}
	if job, err := todoReminderSpringJob(host, out); err != nil {
		fmt.Fprintf(out, "mw events follow: no To do reminder job: %v\n", err)
	} else {
		jobs = append(jobs, job)
	}
	if job, err := todoRankSpringJob(host, out); err != nil {
		fmt.Fprintf(out, "mw events follow: no To do rank job: %v\n", err)
	} else {
		jobs = append(jobs, job)
	}
	if job, err := nightlySuiteSpringJob(host, knobs.SuiteHour, out); err != nil {
		fmt.Fprintf(out, "mw events follow: no suite job: %v\n", err)
	} else {
		jobs = append(jobs, job)
	}
	if job, err := digestSpringJob(host, out); err != nil {
		fmt.Fprintf(out, "mw events follow: no digest job: %v\n", err)
	} else {
		jobs = append(jobs, job)
	}
	if job, err := sleepSpringJob(host, knobs.SleepHour, knobs.SleepMinute, out); err != nil {
		fmt.Fprintf(out, "mw events follow: no sleep job: %v\n", err)
	} else {
		jobs = append(jobs, job)
	}
	if job, err := batchLandSpringJob(host, out); err != nil {
		fmt.Fprintf(out, "mw events follow: no batch-land job: %v\n", err)
	} else {
		jobs = append(jobs, job)
	}
	if job, err := contributeBackSpringJob(host, knobs.ContributeBackDay); err != nil {
		fmt.Fprintf(out, "mw events follow: no contribute-back job: %v\n", err)
	} else {
		jobs = append(jobs, job)
	}
	if job, ok, err := cloudSpringJob(out); err != nil {
		fmt.Fprintf(out, "mw events follow: no cloud job: %v\n", err)
	} else if ok {
		jobs = append(jobs, job)
	}
	spring := &application.EventSpring{Log: eventlog.New(path), Host: host, Jobs: jobs, Err: out, Settle: 2 * time.Second}
	// The Mayor is mailed of a failed nightly or weekly pass (mw-gq6.531); with
	// no vault configured there is no mailbox and no one is told.
	if dir, err := config.Vault(); err == nil {
		spring.Mailbox = mwGateway(dir, host)
	}
	return spring, nil
}

// gristSpringJob is the job that runs one pass of the mill (mw grist grind)
// when a new grist record reaches the postern backend, so a grist is answered
// within seconds and not on the next dispatch tick. It is there only where the
// config has a [grist] table, as the tick's own pass is; and it looks and runs
// only while this host is home, for the home is the one host that holds the
// mill key. The pass is the mill's own, under its own pass lock: one sprung
// while another runs finds it busy and does nothing.
func gristSpringJob(host string, out io.Writer) (application.SpringJob, bool, error) {
	configured, err := config.GristConfigured()
	if err != nil || !configured {
		return application.SpringJob{}, false, err
	}
	keys, err := gristKeys()
	if err != nil {
		return application.SpringJob{}, false, err
	}
	backend, err := posternBackend(keys)
	if err != nil {
		return application.SpringJob{}, false, err
	}
	home := func(ctx context.Context) (bool, error) {
		dir, err := config.Vault()
		if err != nil {
			return false, err
		}
		return application.IsHome(ctx, mwVault(dir, host), host)
	}
	notices := application.NewGristNotices()
	// The stream is the mill key's own; until the backend lets that key onto
	// GET /api/events the watch reads four times a second instead and says so
	// once. The watch reads on a loop of its own (job.Watch below), not once
	// in each cycle of the follower, and only while this host is home.
	watch := &application.GristWatch{Postern: backend, Stream: backend, Keys: keys, Notices: notices, Err: out}
	watch.Gate = func(ctx context.Context) bool {
		at, err := home(ctx)
		return err == nil && at
	}
	// The warm pool is the follower's, kept from one pass to the next: a pass
	// that mw grist grind or the dispatch tick runs has none, and grinds cold.
	stateDir, err := config.GristStateDir()
	if err != nil {
		return application.SpringJob{}, false, err
	}
	pool := &application.GristPool{Warmer: claude.NewGrinder(), Board: grist.New(stateDir), Err: out}
	job := application.GristJob(watch, func(ctx context.Context) error {
		if at, err := home(ctx); err != nil || !at {
			return err
		}
		mill, err := newMill(out)
		if err != nil {
			return err
		}
		mill.Notices = notices
		// pool_quiet = "0s" keeps no warm pool.
		if settings, err := config.Grist(); err == nil && settings.PoolQuiet > 0 {
			pool.Configure(settings.Concurrency, settings.PoolQuiet, settings.PoolMaxAge)
			pool.UseCredits(mill.Credits)
			mill.Pool = pool
		} else {
			pool.Close()
		}
		_, err = mill.Run(ctx)
		return err
	})
	job.Watch = watch.Watch
	var closing sync.Once
	job.Probe = func(ctx context.Context) (string, bool) {
		// The follower stopping ends every warm session; so does this host
		// no longer being home, for the mill is the home's alone.
		closing.Do(func() {
			go func() {
				<-ctx.Done()
				pool.Close()
			}()
		})
		if at, err := home(ctx); err != nil || !at {
			pool.Close()
			return "", false
		}
		pool.Tick(ctx)
		// The grist is the watch's to find, on its own pace.
		return "", false
	}
	return job, true, nil
}

// gristLevelSpringJob is the job that keeps the home's checkout of each
// [grist-apps] rig level with the rig's main when a story lands from any host,
// and makes the grist smoke of what that changed (application.GristLevel). It
// is there only where the config has a [grist-apps] table, and works only while
// this host is home, for the home is where the mill reads the grinds.
func gristLevelSpringJob(host string, heartbeat time.Duration, out io.Writer) (application.SpringJob, bool, error) {
	apps, err := config.GristApps()
	if err != nil || len(apps) == 0 {
		return application.SpringJob{}, false, err
	}
	return application.GristLevelJob(heartbeat, func(ctx context.Context) error {
		dir, err := config.Vault()
		if err != nil {
			return err
		}
		rigs, err := config.Rigs()
		if err != nil {
			return err
		}
		gateway := mwGateway(dir, host)
		worktrees := rig.New()
		return application.GristLevel{
			Apps: apps, Rigs: rigs, Checkout: worktrees, Mailbox: gateway, Host: host, Out: out,
			Book:  gristSmokeBook(gateway, host),
			Smoke: hostGristSmoke(gateway, host, rigs, worktrees, nil, ""),
			Home: func(ctx context.Context) (bool, error) {
				return application.IsHome(ctx, mwVault(dir, host), host)
			},
		}.Run(ctx)
	}), true, nil
}
