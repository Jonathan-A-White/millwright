package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/eventlog"
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
// chain-stamp job, which runs in the follower itself and so is always there,
// and the cloud job where the config has a [cloud] table.
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
	if job, ok, err := cloudSpringJob(out); err != nil {
		fmt.Fprintf(out, "mw events follow: no cloud job: %v\n", err)
	} else if ok {
		jobs = append(jobs, job)
	}
	return &application.EventSpring{Log: eventlog.New(path), Host: host, Jobs: jobs, Err: out, Settle: 2 * time.Second}, nil
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
	watch := &application.GristWatch{Postern: backend, Keys: keys, Err: out}
	job := application.GristJob(watch, func(ctx context.Context) error {
		if at, err := home(ctx); err != nil || !at {
			return err
		}
		mill, err := newMill(out)
		if err != nil {
			return err
		}
		_, err = mill.Run(ctx)
		return err
	})
	look := job.Probe
	job.Probe = func(ctx context.Context) (string, bool) {
		if at, err := home(ctx); err != nil || !at {
			return "", false
		}
		return look(ctx)
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
			Smoke: hostGristSmoke(gateway, host, rigs, worktrees),
			Home: func(ctx context.Context) (bool, error) {
				return application.IsHome(ctx, mwVault(dir, host), host)
			},
		}.Run(ctx)
	}), true, nil
}
