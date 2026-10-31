package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/apartproc"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// batchLandSpringJob is the job that runs `mw land <rig>` on each of this
// host's rigs whose queue is due, on the home only (plans/0117, Land Rush 4):
// a rig that lands in batches when it has batch_min queued and the oldest has
// waited batch_every, or batch_max queued; a rig flipped back to landing =
// each with stories still queued, one story at a time (`mw land <rig> --one`)
// until none remains. Each is a process of this binary of its own, apart from
// the follower (in a systemd scope where there is one): a follower restarted
// after a landing of the factory rig leaves a landing running, and the one that
// comes back waits for it and tells its real outcome (mw-gq6.470). Its output
// goes to a log beside its state, under ~/.local/state/mw/land.
func batchLandSpringJob(host string, out io.Writer) (application.SpringJob, error) {
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
		return application.SpringJob{}, fmt.Errorf("finding the mw binary to land with: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return application.SpringJob{}, fmt.Errorf("finding the home directory to keep the landings' state in: %w", err)
	}
	detached := batchLandApart(self, landStateDir(home), "")
	landings := &application.BatchLandings{
		Rigs:    rigs,
		Rules:   mwVault(dir, host),
		Tracker: mwGateway(dir, host),
		Home: func(ctx context.Context) (bool, error) {
			return application.IsHome(ctx, mwVault(dir, host), host)
		},
		Land:    detached.Land,
		Pending: detached.Pending,
		Notes:   mwGateway(dir, host),
		Host:    host,
	}
	return landings.Job(), nil
}

// batchLandApart is how the job runs `mw land <rig>` with the binary self:
// apart from the follower, its state in stateDir, in a scope of scope's making
// (empty: systemd-run, where there is a user manager).
func batchLandApart(self, stateDir, scope string) application.DetachedLand {
	return application.DetachedLand{Processes: &apartproc.Landings{Dir: stateDir, Program: self, Scope: scope}}
}
