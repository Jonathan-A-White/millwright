package main

import (
	"context"
	"io"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// storyReach is the ssh line to each host a story may be worked on, for the
// commands that go to that host (mw pause, mw peek, mw retry): [hands_hosts],
// and each cloud box the book holds that it has no entry for, at its WireGuard
// address. A home with no [cloud] table reaches [hands_hosts] alone. gaps are
// the boxes left out, each with why its address could not be read.
func storyReach(ctx context.Context, out io.Writer) (reach, gaps map[string]string, err error) {
	return storyReachOf(ctx, pauseCloud(ctx, out))
}

// reachCloud is the cloud as the reach sees it: the boxes up, and where each
// is reached from the home.
type reachCloud interface {
	Boxes(ctx context.Context) ([]string, error)
	Address(ctx context.Context, box string) (application.CloudAddress, error)
}

// storyReachOf is storyReach for a cloud the caller already made, so mw pause
// reaches boxes through the very cloud it destroys them with.
func storyReachOf(ctx context.Context, cloud reachCloud) (reach, gaps map[string]string, err error) {
	hands, err := config.HandsHosts()
	if err != nil {
		return nil, nil, err
	}
	return reachWithCloud(ctx, hands, cloud)
}

// reachWithCloud copies hands and adds each box cloud holds at ssh
// <user>@<address>, the address read off the WireGuard hub (the home resolves
// no name for a box), when hands has no entry for it: a cloud box comes and
// goes, so a hand entry would go stale, and one that is there wins. A box
// whose address cannot be read is left out of the reach and is in gaps with
// the reason.
func reachWithCloud(ctx context.Context, hands map[string]string, cloud reachCloud) (reach, gaps map[string]string, err error) {
	reach, gaps = map[string]string{}, map[string]string{}
	for k, v := range hands {
		reach[k] = v
	}
	boxes, err := cloud.Boxes(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, box := range boxes {
		if reach[box] != "" {
			continue
		}
		at, err := cloud.Address(ctx, box)
		if err != nil {
			gaps[box] = err.Error()
			continue
		}
		reach[box] = "ssh " + at.String()
	}
	return reach, gaps, nil
}
