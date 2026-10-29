package main

import (
	"context"
	"io"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// storyReach is the ssh line to each host a story may be worked on, for the
// commands that go to that host (mw pause, mw peek, mw retry): [hands_hosts],
// and each cloud box the book holds that it has no entry for. A home with no
// [cloud] table reaches [hands_hosts] alone.
func storyReach(ctx context.Context, out io.Writer) (map[string]string, error) {
	return storyReachOf(ctx, pauseCloud(ctx, out))
}

// storyReachOf is storyReach for a cloud the caller already made, so mw pause
// reaches boxes through the very cloud it destroys them with.
func storyReachOf(ctx context.Context, cloud application.PauseCloud) (map[string]string, error) {
	hands, err := config.HandsHosts()
	if err != nil {
		return nil, err
	}
	return reachWithCloud(ctx, hands, cloud)
}

// reachWithCloud copies hands and adds each box cloud holds at ssh mw@<box>.mw
// when hands has no entry for it: a cloud box comes and goes, so a hand entry
// would go stale, and one that is there wins.
func reachWithCloud(ctx context.Context, hands map[string]string, cloud application.PauseCloud) (map[string]string, error) {
	reach := map[string]string{}
	for k, v := range hands {
		reach[k] = v
	}
	boxes, err := cloud.Boxes(ctx)
	if err != nil {
		return nil, err
	}
	for _, box := range boxes {
		if reach[box] == "" {
			reach[box] = "ssh mw@" + box + ".mw"
		}
	}
	return reach, nil
}
