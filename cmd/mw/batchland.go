package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// batchLandSpringJob is the job that runs `mw land <rig>` on each of this
// host's rigs whose queue is due, on the home only (plans/0117, Land Rush 4):
// a rig that lands in batches when it has batch_min queued and the oldest has
// waited batch_every, or batch_max queued; a rig flipped back to landing =
// each with stories still queued, one story at a time (`mw land <rig> --one`)
// until none remains. Each is a run of this binary, its output on out.
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
	landings := &application.BatchLandings{
		Rigs:    rigs,
		Rules:   mwVault(dir, host),
		Tracker: mwGateway(dir, host),
		Home: func(ctx context.Context) (bool, error) {
			return application.IsHome(ctx, mwVault(dir, host), host)
		},
		Land: func(ctx context.Context, rig string, one bool) error {
			args := []string{"land", rig}
			if one {
				args = append(args, "--one")
			}
			cmd := exec.CommandContext(ctx, self, args...)
			cmd.Stdout, cmd.Stderr = out, out
			return cmd.Run()
		},
	}
	return landings.Job(), nil
}
