package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/apartproc"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostload"
	"github.com/Jonathan-A-White/millwright/infrastructure/peekremote"
)

// nightlyStateDir is where the follower keeps the state of the nightly runs it
// begins apart from itself, under the state dir of the mw of this user:
// ~/.local/state/mw/<kind>.
func nightlyStateDir(home, kind string) string {
	return filepath.Join(home, ".local", "state", "mw", kind)
}

// nightlyApart is the processes the nightly kind of run is begun and waited for
// through, here and on another host: the suite's, in the state dir of the suite, and
// the timing run's, in a dir of its own (mw-gq6.1617).
func nightlyApart(self, home, kind string) *apartproc.Processes {
	p := &apartproc.Processes{Dir: nightlyStateDir(home, kind), Program: self}
	if kind == application.NightlyTimingKind {
		p.Label = "the timing run of "
	}
	return p
}

// sshNightlyHosts is application.NightlyElsewhere over ssh: it asks each other
// host what room it has as a batch landing does (mw land <rig> --room), and runs mw
// suite <rig> --file or mw timing <rig> on the one with room, apart from the
// follower in the state of the run here (apartproc.Processes.BeginOn).
type sshNightlyHosts struct {
	hosts sshBatchHosts
	runs  map[string]*apartproc.Processes // kind -> the processes of that kind
}

var _ application.NightlyElsewhere = sshNightlyHosts{}

func newSSHNightlyHosts(host string, suites, timings *apartproc.Processes) sshNightlyHosts {
	return sshNightlyHosts{
		hosts: newSSHBatchHosts(host, nil),
		runs:  map[string]*apartproc.Processes{application.NightlySuiteKind: suites, application.NightlyTimingKind: timings},
	}
}

// Rooms implements application.NightlyElsewhere.
func (h sshNightlyHosts) Rooms(ctx context.Context, rig string) ([]application.BatchRoom, error) {
	return h.hosts.Rooms(ctx, rig)
}

// Run implements application.NightlyElsewhere.
func (h sshNightlyHosts) Run(ctx context.Context, host, kind, rig string) error {
	processes := h.runs[kind]
	if processes == nil {
		return fmt.Errorf("no nightly run of the kind %q to run on %s", kind, host)
	}
	reach, gaps, err := h.hosts.reach(ctx)
	if err != nil {
		return application.LeftRunning(ctx, fmt.Errorf("reading where %s is reached: %w", host, err))
	}
	argv, err := peekremote.Remote{Reach: reach, Gaps: gaps}.NightlyArgv(host, kind, rig)
	if err != nil {
		return application.LeftRunning(ctx, err)
	}
	if err := processes.BeginOn(ctx, rig, host, argv); err != nil {
		return application.LeftRunning(ctx, fmt.Errorf("beginning the nightly %s of %s on %s: %w", kind, rig, host, err))
	}
	return application.LeftRunning(ctx, processes.Wait(ctx, rig))
}

// nightlyPlace is where the home's nightly kind of run is made: on the home, or,
// while the home is busy (its load at its cores, or its gate lock held by a gate)
// and another host has room, on that host (mw-gq6.1617).
func nightlyPlace(host, dir string, out io.Writer) (application.NightlyPlace, error) {
	self, err := os.Executable()
	if err != nil {
		return application.NightlyPlace{}, fmt.Errorf("finding the mw binary to run the nightly runs with: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return application.NightlyPlace{}, fmt.Errorf("finding the home directory to keep the nightly runs' state in: %w", err)
	}
	return application.NightlyPlace{
		Elsewhere: newSSHNightlyHosts(host,
			nightlyApart(self, home, application.NightlySuiteKind), nightlyApart(self, home, application.NightlyTimingKind)),
		Load:       hostload.Proc{}, // the load alone: no swap is sampled for a look at the home
		GateHolder: hostGateHolder(),
		Host:       host,
		Notes:      mwGateway(dir, host),
		Out:        out,
	}, nil
}
