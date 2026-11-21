package main

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/apartproc"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostload"
	"github.com/Jonathan-A-White/millwright/infrastructure/peekremote"
)

// runLandRoom is mw land <rig> --room: it prints whether this host could land the
// rig's batch now, in the lines application.ParseBatchRoom reads, and lands
// nothing. It is what the home asks over ssh of each other host when it is busy
// and a batch is due (mw-gq6.1616).
func runLandRoom(ctx context.Context, rigName string, out io.Writer) error {
	rigs, err := config.Rigs()
	if err != nil {
		return err
	}
	_, has := rigs[rigName]
	room := application.BatchRoom{HasRig: has}
	// A load that cannot be read leaves the cores at 0, which is no room.
	if reading, err := (hostload.Proc{}).Load(ctx); err == nil {
		room.Load = reading
	}
	if holder := hostGateHolder(); holder != nil {
		room.GateHeldBy = holder()
	}
	_, err = fmt.Fprint(out, room.String())
	return err
}

// landRoomLimit is how long the home waits on one host's answer to mw land --room.
const landRoomLimit = 30 * time.Second

// sshBatchHosts is application.BatchElsewhere over ssh: the hosts it asks are the
// ones the story reach names ([hands_hosts] and each cloud box the book holds at
// its WireGuard address), but this one; a batch is landed on one by running mw
// land there, apart from the follower in the state of the rig's landing here
// (apartproc.Landings.BeginOn).
type sshBatchHosts struct {
	host     string
	landings *apartproc.Landings
	// reach reads where each other host is reached from here; the real one is
	// storyReach, a test names its own.
	reach func(ctx context.Context) (reach, gaps map[string]string, err error)
}

var _ application.BatchElsewhere = sshBatchHosts{}

func newSSHBatchHosts(host string, landings *apartproc.Landings) sshBatchHosts {
	return sshBatchHosts{host: host, landings: landings, reach: func(ctx context.Context) (map[string]string, map[string]string, error) {
		return storyReachOf(ctx, pauseCloud(ctx, io.Discard), true)
	}}
}

// Rooms implements application.BatchElsewhere: every other host is asked at once,
// and one that cannot be asked is a room with its trouble.
func (h sshBatchHosts) Rooms(ctx context.Context, rig string) ([]application.BatchRoom, error) {
	reach, gaps, err := h.reach(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading where the other hosts are reached: %w", err)
	}
	remote := peekremote.Remote{Reach: reach, Gaps: gaps}
	var hosts []string
	for name := range reach {
		if name != h.host {
			hosts = append(hosts, name)
		}
	}
	sort.Strings(hosts)
	rooms := make([]application.BatchRoom, len(hosts))
	var wg sync.WaitGroup
	for i, name := range hosts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			asked, cancel := context.WithTimeout(ctx, landRoomLimit)
			defer cancel()
			room, err := remote.LandRoom(asked, name, rig)
			if err != nil {
				room = application.BatchRoom{Host: name, Trouble: err.Error()}
			}
			rooms[i] = room
		}()
	}
	wg.Wait()
	return rooms, nil
}

// Land implements application.BatchElsewhere: mw land <rig> on host, begun apart
// from the follower and waited for as a landing here is.
func (h sshBatchHosts) Land(ctx context.Context, host, rig string, scope application.LandScope) error {
	reach, gaps, err := h.reach(ctx)
	if err != nil {
		return application.LeftRunning(ctx, fmt.Errorf("reading where %s is reached: %w", host, err))
	}
	argv, err := peekremote.Remote{Reach: reach, Gaps: gaps}.LandArgv(host, rig, scope)
	if err != nil {
		return application.LeftRunning(ctx, err)
	}
	if err := h.landings.BeginOn(ctx, rig, host, argv); err != nil {
		return application.LeftRunning(ctx, fmt.Errorf("beginning mw land %s on %s: %w", rig, host, err))
	}
	return application.LeftRunning(ctx, h.landings.Wait(ctx, rig))
}
