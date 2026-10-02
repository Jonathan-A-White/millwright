package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/eventlog"
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
// left to its timers).
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
	return &application.EventSpring{Log: eventlog.New(path), Host: host, Jobs: jobs, Err: out, Settle: 2 * time.Second}, nil
}
